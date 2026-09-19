package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"ceksepet-api/internal/config"
	"ceksepet-api/internal/httpapi"
	"ceksepet-api/internal/store"
)

// Run starts the API using the same configuration for both entry points.
func Run() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	cfg, err := config.Load()
	if err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	dataStore, err := store.Open(cfg.MongoURI, cfg.MongoDatabase)
	if err != nil {
		logger.Error("database startup failed", "error", err)
		os.Exit(1)
	}
	defer dataStore.Close()

	var notifier httpapi.OTPNotifier = httpapi.DevelopmentNotifier{Logger: logger}
	if cfg.OTPWebhookURL != "" {
		notifier = httpapi.NewWebhookNotifier(cfg.OTPWebhookURL, cfg.OTPWebhookToken)
	}
	api, err := httpapi.New(cfg, dataStore, notifier, logger)
	if err != nil {
		logger.Error("API startup failed", "error", err)
		os.Exit(1)
	}
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownSignals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go purgeExpiredSessions(shutdownSignals, dataStore, logger)
	go func() {
		<-shutdownSignals.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
		}
	}()

	logger.Info("ÇekSepet API started", "address", cfg.HTTPAddr, "environment", cfg.Environment,
		"swagger", cfg.PublicBaseURL+"/swagger/index.html")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("HTTP server stopped unexpectedly", "error", err)
		os.Exit(1)
	}
}

func purgeExpiredSessions(ctx context.Context, dataStore store.DataStore, logger *slog.Logger) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		purgeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := dataStore.PurgeExpiredSessions(purgeCtx, time.Now().UTC())
		cancel()
		if err != nil {
			logger.Error("expired session cleanup failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
