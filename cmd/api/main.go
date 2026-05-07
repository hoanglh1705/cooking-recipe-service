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

	"github.com/cooking-recipe/cooking-recipe-service/internal/admin"
	"github.com/cooking-recipe/cooking-recipe-service/internal/api"
	"github.com/cooking-recipe/cooking-recipe-service/internal/config"
	"github.com/cooking-recipe/cooking-recipe-service/internal/db"
	"github.com/cooking-recipe/cooking-recipe-service/internal/httpmw"
	"github.com/cooking-recipe/cooking-recipe-service/internal/internalapi"
	"github.com/cooking-recipe/cooking-recipe-service/internal/queue"

	"github.com/hibiken/asynq"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "err", err)
		os.Exit(1)
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Open(rootCtx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("open db", "err", err)
		os.Exit(1)
	}
	defer database.Close()
	if err := database.Migrate(rootCtx); err != nil {
		logger.Error("migrate", "err", err)
		os.Exit(1)
	}
	queries := db.NewQueries(database)

	// Asynq client is optional — admin pipeline trigger fails closed if Redis is unreachable.
	var qc *queue.Client
	if cfg.Redis.Addr != "" {
		qc = queue.NewClient(asynq.RedisClientOpt{
			Addr:     cfg.Redis.Addr,
			Password: cfg.Redis.Password,
			DB:       cfg.Redis.DB,
		})
		defer qc.Close()
	}

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Use(middleware.RequestID())
	e.Use(httpmw.Logger(logger))
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: cfg.CORSOrigins,
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowHeaders: []string{"Content-Type", "Authorization"},
	}))

	api.New(queries).Mount(e)
	admin.New(queries, qc, admin.Config{
		AdminToken:       cfg.AdminToken,
		RevalidateURL:    cfg.NextRevalidateURL,
		RevalidateSecret: cfg.NextRevalidateAuth,
	}).Mount(e)
	internalapi.New(queries, cfg.CrawlerSecret).Mount(e)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           e,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("api server listening", "addr", cfg.HTTPAddr, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen", "err", err)
			stop()
		}
	}()

	<-rootCtx.Done()
	logger.Info("api: shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown", "err", err)
	}
}
