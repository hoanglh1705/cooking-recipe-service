package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/cooking-recipe/cooking-recipe-service/internal/clients/llm"
	"github.com/cooking-recipe/cooking-recipe-service/internal/clients/tiktok"
	"github.com/cooking-recipe/cooking-recipe-service/internal/clients/whisper"
	"github.com/cooking-recipe/cooking-recipe-service/internal/clients/youtube"
	"github.com/cooking-recipe/cooking-recipe-service/internal/config"
	"github.com/cooking-recipe/cooking-recipe-service/internal/db"
	"github.com/cooking-recipe/cooking-recipe-service/internal/pipeline"
	"github.com/cooking-recipe/cooking-recipe-service/internal/queue"

	"github.com/hibiken/asynq"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("open db", "err", err)
		os.Exit(1)
	}
	defer database.Close()
	if err := database.Migrate(ctx); err != nil {
		logger.Error("migrate", "err", err)
		os.Exit(1)
	}

	redisOpt := asynq.RedisClientOpt{
		Addr: cfg.Redis.Addr, Password: cfg.Redis.Password, DB: cfg.Redis.DB,
	}
	qClient := queue.NewClient(redisOpt)
	defer qClient.Close()

	deps := &pipeline.Deps{
		Q:        db.NewQueries(database),
		Client:   qClient,
		YouTube:  youtube.New(cfg.YouTubeAPIKey),
		TikTok:   tiktok.New(),
		LLM:      llm.New(cfg.LLM.Provider, cfg.LLM.AnthropicKey, cfg.LLM.AnthropicMdl, cfg.LLM.OpenAIKey, cfg.LLM.OpenAIMdl),
		Whisper:  whisper.New(cfg.Whisper.Provider, cfg.Whisper.OpenAIKey, cfg.Whisper.OpenAIModel),
		YTDLP:    cfg.YTDLPPath,
		AudioDir: cfg.AudioTmpDir,
		Logger:   logger,
		PublishHook: pipeline.RevalidateNext(cfg.NextRevalidateURL, cfg.NextRevalidateAuth),
	}

	srv := asynq.NewServer(redisOpt, asynq.Config{
		Concurrency: 4,
		Queues: map[string]int{
			queue.QueueDefault: 6,
			queue.QueueAI:      3,
		},
		Logger: asynqSlog{logger},
	})

	mux := asynq.NewServeMux()
	deps.Register(mux)

	go func() {
		<-ctx.Done()
		logger.Info("worker: shutting down")
		srv.Shutdown()
	}()

	logger.Info("worker started", "redis", cfg.Redis.Addr, "llm", cfg.LLM.Provider, "whisper", cfg.Whisper.Provider)
	if err := srv.Run(mux); err != nil {
		logger.Error("worker run", "err", err)
		os.Exit(1)
	}
}

// asynqSlog adapts asynq.Logger to slog so the worker has structured logs
// that match the API server.
type asynqSlog struct{ *slog.Logger }

func (a asynqSlog) Debug(args ...any) { a.Logger.Debug(sprintArgs(args)) }
func (a asynqSlog) Info(args ...any)  { a.Logger.Info(sprintArgs(args)) }
func (a asynqSlog) Warn(args ...any)  { a.Logger.Warn(sprintArgs(args)) }
func (a asynqSlog) Error(args ...any) { a.Logger.Error(sprintArgs(args)) }
func (a asynqSlog) Fatal(args ...any) {
	a.Logger.Error(sprintArgs(args))
	os.Exit(1)
}

func sprintArgs(args []any) string {
	return fmt.Sprint(args...)
}
