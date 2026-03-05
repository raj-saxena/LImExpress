package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/limexpress/gateway/internal/config"
	"github.com/limexpress/gateway/internal/db"
	"go.uber.org/zap"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	logger, err := cfg.NewLogger()
	if err != nil {
		log.Fatalf("failed to initialize logger: %v", err)
	}
	defer logger.Sync() //nolint:errcheck

	cfg.LogSummary(logger)

	// Initialize DB pool
	pool, err := db.NewPool(ctx, cfg.DB.DSN)
	if err != nil {
		logger.Fatal("failed to connect to database", zap.Error(err))
	}
	defer pool.Close()

	logger.Info("foundation established; ready for gateway components")
}
