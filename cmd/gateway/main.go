package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/limexpress/gateway/internal/config"
	"github.com/limexpress/gateway/internal/db"
	"github.com/limexpress/gateway/internal/metrics"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		panic(fmt.Sprintf("failed to load config: %v", err))
	}

	logger, err := metrics.New(cfg.Log.Level)
	if err != nil {
		panic(fmt.Sprintf("failed to initialise logger: %v", err))
	}
	defer logger.Sync() //nolint:errcheck

	cfg.LogSummary(logger)

	// Initialize DB pool
	pool, err := db.NewPool(ctx, cfg.DB.DSN)
	if err != nil {
		logger.Fatal("failed to connect to database", zap.Error(err))
	}
	defer pool.Close()

	r := chi.NewRouter()
	r.Get("/metrics", promhttp.Handler().ServeHTTP)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	logger.Info("gateway starting",
		zap.String("addr", addr),
		zap.String("log_level", cfg.Log.Level),
	)

	if err := http.ListenAndServe(addr, r); err != nil {
		logger.Fatal("server error", zap.Error(err))
	}
}
