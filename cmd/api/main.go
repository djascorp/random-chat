// Command api runs the random video chat signaling server: REST API, WebSocket
// signaling, matching engine, moderation and metrics.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"random-chat/internal/config"
	"random-chat/internal/httpapi"
	"random-chat/internal/matching"
	"random-chat/internal/metrics"
	"random-chat/internal/moderation"
	"random-chat/internal/sessions"
	"random-chat/internal/signaling"
	"random-chat/internal/storage"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store, err := storage.Open(ctx, cfg.DatabaseURL, cfg.MigrationsDir, cfg.AutoMigrate, logger)
	if err != nil {
		logger.Error("storage initialisation failed", "err", err)
		os.Exit(1)
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Warn("store close failed", "err", err)
		}
	}()

	registry := metrics.New()
	sessionManager := sessions.NewManager(store, logger)
	moderationService := moderation.NewService(ctx, store, moderation.Options{
		AutoSuspendAfter: cfg.AutoSuspendAfter,
		SuspendDuration:  cfg.SuspendDuration,
		ViolationLimit:   cfg.ViolationLimit,
	}, logger)
	go moderationService.Run(ctx)

	matcher := matching.NewMatcher(matching.Options{
		QueueTimeout: cfg.QueueTimeout,
		Sessions:     sessionManager,
		Blocks:       moderationService,
		Metrics:      signaling.NewMatcherMetrics(registry),
		Logger:       logger,
	})
	go matcher.Run(ctx)

	hub := signaling.NewHub(signaling.HubOptions{
		Config:     cfg,
		Store:      store,
		Sessions:   sessionManager,
		Matcher:    matcher,
		Moderation: moderationService,
		Metrics:    registry,
		Logger:     logger,
	})
	go hub.Run(ctx)

	router := httpapi.NewRouter(httpapi.Options{
		Config:     cfg,
		Hub:        hub,
		Store:      store,
		Sessions:   sessionManager,
		Moderation: moderationService,
		Metrics:    registry,
		Logger:     logger,
	})

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: WebSocket connections are long lived and the
		// per-message deadlines in the write pump already bound slow clients.
		ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelWarn),
	}

	go func() {
		logger.Info("signaling server listening",
			"addr", cfg.Addr,
			"protocol_version", config.ProtocolVersion,
			"turn_configured", len(cfg.IceServers) > 1,
			"admin_api", cfg.AdminToken != "")
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server stopped", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down", "grace", cfg.ShutdownGrace.String())

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer cancel()

	sessionManager.EndAll("server_shutdown")
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Warn("graceful shutdown incomplete", "err", err)
		_ = server.Close()
	}
	logger.Info("shutdown complete")
}
