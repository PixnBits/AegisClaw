package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/store"
	"go.uber.org/zap"
)

// store-vm is the minimal binary that will run inside the Store microVM.
// Phase 2.7 scaffold: basic structure + store initialization.
// Future: vsock server, health endpoint, graceful shutdown.

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "Path to config file (optional)")
	flag.Parse()

	logger, err := zap.NewProduction()
	if err != nil {
		log.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Sync()

	logger.Info("Store VM starting (Phase 2.7 scaffold)")

	cfg, err := config.Load(logger)
	if err != nil {
		logger.Fatal("failed to load config", zap.Error(err))
	}

	// Initialize stores via the same NewStoreVM logic (or direct for now)
	// In later phases this will be the server that listens on vsock.
	svm, err := store.NewStoreVM(cfg, logger)
	if err != nil {
		logger.Fatal("failed to create StoreVM inside store-vm binary", zap.Error(err))
	}

	if err := svm.Start(context.Background()); err != nil {
		logger.Fatal("StoreVM start failed", zap.Error(err))
	}

	logger.Info("Store VM initialized successfully (in-process mode for scaffold)")

	// TODO (Phase 2.8+): Start vsock listener here
	// Example: go startVsockServer(svm, logger)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("Store VM shutting down...")
	_ = svm.Stop(context.Background())
	logger.Info("Store VM stopped cleanly")
}
