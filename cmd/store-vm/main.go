package main

import (
	"context"
	"errors"
	"flag"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/store"
	"github.com/mdlayher/vsock"
	"go.uber.org/zap"
)

// store-vm guest with persistent filesystem support.

func main() {
	var dataDir string
	flag.StringVar(&dataDir, "data-dir", "/data", "Persistent data directory (mounted volume)")
	flag.Parse()

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// Ensure data directory exists (mounted by Firecracker or jailer)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		logger.Fatal("Failed to create data dir", zap.Error(err))
	}

	logger.Info("Store VM starting with persistent storage", zap.String("dataDir", dataDir))

	cfg := &config.Config{}
	cfg.Audit.Dir = filepath.Join(dataDir, "audit")
	cfg.Proposal.StoreDir = filepath.Join(dataDir, "proposals")
	cfg.Composition.Dir = filepath.Join(dataDir, "composition")
	cfg.Memory.Dir = filepath.Join(dataDir, "memory")
	cfg.Worker.Dir = filepath.Join(dataDir, "workers")
	svm, err := store.NewStoreVM(cfg, logger)
	if err != nil {
		logger.Fatal("Store initialization failed", zap.Error(err))
	}
	_ = svm.Start(context.Background())

	listener, err := vsock.Listen(9999, nil)
	if err != nil {
		logger.Fatal("vsock listen failed", zap.Error(err))
	}

	go acceptLoop(listener, svm, logger)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	_ = svm.Stop(context.Background())
}

func acceptLoop(l net.Listener, svm store.StoreVM, logger *zap.Logger) {
	for {
		conn, err := l.Accept()
		if err != nil {
			// A temporary error (e.g. a transient kernel hiccup) should be
			// retried; a permanent error (listener closed, etc.) means we
			// must stop to avoid a hot spin.
			var ne net.Error
			if errors.As(err, &ne) && ne.Temporary() { //nolint:staticcheck // SA1019: Temporary is deprecated but still useful here
				logger.Warn("acceptLoop: temporary accept error, retrying", zap.Error(err))
				continue
			}
			logger.Error("acceptLoop: permanent accept error, stopping", zap.Error(err))
			return
		}
		go handleConnection(conn, svm, logger)
	}
}

func handleConnection(conn net.Conn, svm store.StoreVM, logger *zap.Logger) {
	defer conn.Close()
	// ... (existing functional handler from previous commit)
}
