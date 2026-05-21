package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/PixnBits/AegisClaw/internal/config"
	"github.com/PixnBits/AegisClaw/internal/proposal"
	"github.com/PixnBits/AegisClaw/internal/store"
	"github.com/PixnBits/AegisClaw/internal/store/remote"
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
			// If the listener was closed intentionally (e.g. during shutdown) that
			// is a clean stop; any other error is unexpected – log it and stop to
			// avoid a hot spin on a broken listener.
			if errors.Is(err, net.ErrClosed) {
				logger.Info("acceptLoop: listener closed, stopping")
			} else {
				logger.Error("acceptLoop: accept error, stopping", zap.Error(err))
			}
			return
		}
		go handleConnection(conn, svm, logger)
	}
}

func handleConnection(conn net.Conn, svm store.StoreVM, logger *zap.Logger) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	for {
		var req remote.Request
		if err := decoder.Decode(&req); err != nil {
			if err == io.EOF {
				return
			}
			resp := remote.Response{ID: req.ID, Success: false, Error: fmt.Sprintf("decode request: %v", err)}
			if err := encoder.Encode(resp); err != nil {
				logger.Error("failed to send error response", zap.Error(err))
			}
			return
		}

		var respData interface{}
		var err error

		switch req.Op {
		case "proposal.list":
			summaries, e := svm.Store().Proposals().List()
			if e != nil {
				err = fmt.Errorf("proposal list failed: %w", e)
			} else {
				respData = summaries
			}
		case "proposal.get":
			var idReq struct{ ID string `json:"id"` }
			if e := json.Unmarshal(req.Payload, &idReq); e != nil {
				err = fmt.Errorf("invalid payload for proposal.get: %w", e)
			} else {
				p, e := svm.Store().Proposals().Get(idReq.ID)
				if e != nil {
					err = e
				} else {
					respData = p
				}
			}
		case "proposal.create":
			var p proposal.Proposal
			if e := json.Unmarshal(req.Payload, &p); e != nil {
				err = fmt.Errorf("invalid payload for proposal.create: %w", e)
			} else {
				e := svm.Store().Proposals().Create(&p)
				if e != nil {
					err = e
				} else {
					respData = p
				}
			}
		case "proposal.update":
			var p proposal.Proposal
			if e := json.Unmarshal(req.Payload, &p); e != nil {
				err = fmt.Errorf("invalid payload for proposal.update: %w", e)
			} else {
				e := svm.Store().Proposals().Update(&p)
				if e != nil {
					err = e
				} else {
					respData = p
				}
			}
		case "proposal.list_by_status":
			var statusReq struct{ Status string `json:"status"` }
			if e := json.Unmarshal(req.Payload, &statusReq); e != nil {
				err = fmt.Errorf("invalid payload for proposal.list_by_status: %w", e)
			} else {
				summaries, e := svm.Store().Proposals().ListByStatus(proposal.Status(statusReq.Status))
				if e != nil {
					err = e
				} else {
					respData = summaries
				}
			}
		case "proposal.resolve_id":
			var prefixReq struct{ Prefix string `json:"prefix"` }
			if e := json.Unmarshal(req.Payload, &prefixReq); e != nil {
				err = fmt.Errorf("invalid payload for proposal.resolve_id: %w", e)
			} else {
				id, e := svm.Store().Proposals().ResolveID(prefixReq.Prefix)
				if e != nil {
					err = e
				} else {
					respData = id
				}
			}
		case "proposal.import":
			var p proposal.Proposal
			if e := json.Unmarshal(req.Payload, &p); e != nil {
				err = fmt.Errorf("invalid payload for proposal.import: %w", e)
			} else {
				e := svm.Store().Proposals().Import(&p)
				if e != nil {
					err = e
				} else {
					respData = p
				}
			}
		default:
			err = fmt.Errorf("unsupported operation: %s", req.Op)
		}

		resp := remote.Response{
			ID:      req.ID,
			Success: err == nil,
			Error:   "",
			Data:    respData,
		}
		if err != nil {
			resp.Error = err.Error()
		}
		if err := encoder.Encode(resp); err != nil {
			logger.Error("failed to send response", zap.Error(err))
			return
		}
	}
}
