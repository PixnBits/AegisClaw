package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/store"
	"github.com/mdlayher/vsock" // vsock support
	"go.uber.org/zap"
)

// store-vm: Real guest binary for the Store microVM (Firecracker).
// Listens on vsock and serves Store operations.

func main() {
	var dataDir string
	flag.StringVar(&dataDir, "data-dir", "/data", "Directory for persistent stores")
	flag.Parse()

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	logger.Info("Store VM guest starting (real Firecracker mode)", zap.String("dataDir", dataDir))

	cfg := &config.Config{} // minimal config for guest
	// In real impl, load from mounted config or env

	svm, err := store.NewStoreVM(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to init stores", zap.Error(err))
	}
	_ = svm.Start(context.Background())

	// Start vsock server
	listener, err := vsock.Listen(3, 9999) // CID 3, port 9999
	if err != nil {
		logger.Fatal("vsock listen failed", zap.Error(err))
	}
	defer listener.Close()

	logger.Info("Store VM listening on vsock :3:9999")

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				continue
			}
			go handleConnection(conn, svm, logger)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("Store VM shutting down")
	_ = svm.Stop(context.Background())
}

func handleConnection(conn net.Conn, svm store.StoreVM, logger *zap.Logger) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	var req struct {
		Op      string      `json:"op"`
		Payload interface{} `json:"payload"`
	}

	if err := decoder.Decode(&req); err != nil {
		encoder.Encode(map[string]string{"error": err.Error()})
		return
	}

	// TODO: Route req.Op to actual store methods
	// For now, echo back
	resp := map[string]interface{}{
		"op":     req.Op,
		"status":  "received",
		"payload": req.Payload,
	}
	encoder.Encode(resp)
}
