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
	"github.com/mdlayher/vsock"
	"go.uber.org/zap"
)

// store-vm guest binary for real Firecracker Store VM.
// Now with functional request routing to actual stores.

func main() {
	var dataDir string
	flag.StringVar(&dataDir, "data-dir", "/data", "Persistent data directory")
	flag.Parse()

	logger, _ := zap.NewProduction()
	defer logger.Sync()

	logger.Info("Store VM guest starting", zap.String("dataDir", dataDir))

	cfg := &config.Config{} // TODO: load real config in guest
	svm, err := store.NewStoreVM(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to initialize stores", zap.Error(err))
	}
	_ = svm.Start(context.Background())

	// Listen on vsock
	listener, err := vsock.Listen(3, 9999)
	if err != nil {
		logger.Fatal("vsock.Listen failed", zap.Error(err))
	}
	defer listener.Close()

	logger.Info("Store VM listening on vsock CID=3 port=9999")

	go acceptLoop(listener, svm, logger)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	logger.Info("Shutting down Store VM")
	_ = svm.Stop(context.Background())
}

func acceptLoop(listener net.Listener, svm store.StoreVM, logger *zap.Logger) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go handleConnection(conn, svm, logger)
	}
}

func handleConnection(conn net.Conn, svm store.StoreVM, logger *zap.Logger) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	var req struct {
		ID      string      `json:"id"`
		Op      string      `json:"op"`
		Payload json.RawMessage `json:"payload"`
	}

	if err := decoder.Decode(&req); err != nil {
		encoder.Encode(map[string]string{"error": "bad request: " + err.Error()})
		return
	}

	var resp interface{}
	switch req.Op {
	case "proposal.create":
		var p struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
			json.Unmarshal(req.Payload, &p)
			err := svm.Store().Proposals().Create(context.Background(), &struct{ ID, Title string }{ID: p.ID, Title: p.Title})
			resp = map[string]interface{}{"status": "ok", "error": err}

	case "proposal.get":
		var q struct{ ID string `json:"id"` }
		json.Unmarshal(req.Payload, &q)
		result, err := svm.Store().Proposals().Get(context.Background(), q.ID)
		resp = map[string]interface{}{"result": result, "error": err}

	case "memory.store":
		// Simplified - real payload would be memory.Entry
		resp = map[string]string{"status": "memory store received (stub)"}

	default:
		resp = map[string]string{"error": "unknown op: " + req.Op}
	}

	encoder.Encode(resp)
}
