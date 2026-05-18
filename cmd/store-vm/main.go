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

// store-vm binary (Phase 2.8 scaffold)
// - Initializes stores
// - TODO: Start vsock server to serve Store interface

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "", "Path to config (optional)")
	flag.Parse()

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	logger.Info("Store VM starting (Phase 2.8 - vsock scaffold)")

	cfg, err := config.Load(logger)
	if err != nil {
		logger.Fatal("config load failed", zap.Error(err))
	}

	svm, err := store.NewStoreVM(cfg, logger)
	if err != nil {
		logger.Fatal("NewStoreVM failed", zap.Error(err))
	}
	_ = svm.Start(context.Background())

	logger.Info("Stores initialized. Starting vsock listener... (stub)")

	// TODO Phase 2.8/2.9: Replace with real vsock listener
	// Example:
	// listener, _ := vsock.Listen(cid, port)
	// for {
	//     conn, _ := listener.Accept()
	//     go handleStoreRequest(conn, svm.Store())
	// }

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("Shutting down Store VM")
	_ = svm.Stop(context.Background())
}
