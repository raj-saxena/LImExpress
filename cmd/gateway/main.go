package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	// Initialize DB pool.
	pool, err := db.NewPool(ctx, cfg.DB.DSN)
	if err != nil {
		logger.Fatal("failed to connect to database", zap.Error(err))
	}
	defer pool.Close()

	querier := db.New(pool)

	r := chi.NewRouter()
	r.Get("/metrics", promhttp.Handler().ServeHTTP)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	r.Handle("/assets/*", http.StripPrefix("/assets/", http.FileServer(http.Dir("assets"))))

	// UI routes are dynamic:
	//   1) env/runtime settings complete -> full portal + auth routes
	//   2) settings missing -> setup form to persist required values in DB
	r.Mount("/", newUISwitcher(ctx, logger, pool, querier))

	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	logger.Info("gateway starting",
		zap.String("addr", addr),
		zap.String("log_level", cfg.Log.Level),
	)

	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		logger.Info("shutdown signal received, stopping server")
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", zap.Error(err))
		}
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatal("server error", zap.Error(err))
	}
}
