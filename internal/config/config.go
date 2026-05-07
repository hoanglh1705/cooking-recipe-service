package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr    string
	Env         string
	LogLevel    string
	CORSOrigins   []string
	AdminToken    string
	CrawlerSecret string // shared HMAC secret cho /api/internal/* (xem crawler_architech.md §6.2)

	DatabaseURL string

	Redis RedisConfig

	YouTubeAPIKey      string
	LLM                LLMConfig
	Whisper            WhisperConfig
	YTDLPPath          string
	AudioTmpDir        string
	NextRevalidateURL  string
	NextRevalidateAuth string
}

type RedisConfig struct {
	Addr     string
	Password string
	DB       int
}

type LLMConfig struct {
	Provider     string
	AnthropicKey string
	AnthropicMdl string
	OpenAIKey    string
	OpenAIMdl    string
}

type WhisperConfig struct {
	Provider     string
	OpenAIKey    string
	OpenAIModel  string
}

func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:           getEnv("HTTP_ADDR", ":8080"),
		Env:                getEnv("ENV", "dev"),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
		CORSOrigins:        splitCSV(getEnv("CORS_ORIGINS", "http://localhost:3000")),
		AdminToken:         getEnv("ADMIN_TOKEN", ""),
		CrawlerSecret:      getEnv("CRAWLER_SECRET", ""),
		DatabaseURL:        getEnv("DATABASE_URL", ""),
		YouTubeAPIKey:      getEnv("YOUTUBE_API_KEY", ""),
		YTDLPPath:          getEnv("YT_DLP_PATH", "yt-dlp"),
		AudioTmpDir:        getEnv("AUDIO_TMP_DIR", "/tmp/cooking-recipe-audio"),
		NextRevalidateURL:  getEnv("NEXT_REVALIDATE_URL", ""),
		NextRevalidateAuth: getEnv("NEXT_REVALIDATE_SECRET", ""),
	}

	cfg.Redis = RedisConfig{
		Addr:     getEnv("REDIS_ADDR", "localhost:6379"),
		Password: getEnv("REDIS_PASSWORD", ""),
		DB:       getEnvInt("REDIS_DB", 0),
	}

	cfg.LLM = LLMConfig{
		Provider:     getEnv("LLM_PROVIDER", "anthropic"),
		AnthropicKey: getEnv("ANTHROPIC_API_KEY", ""),
		AnthropicMdl: getEnv("ANTHROPIC_MODEL", "claude-haiku-4-5"),
		OpenAIKey:    getEnv("OPENAI_API_KEY", ""),
		OpenAIMdl:    getEnv("OPENAI_MODEL", "gpt-4o-mini"),
	}

	cfg.Whisper = WhisperConfig{
		Provider:    getEnv("WHISPER_PROVIDER", "openai"),
		OpenAIKey:   getEnv("OPENAI_API_KEY", ""),
		OpenAIModel: getEnv("OPENAI_WHISPER_MODEL", "whisper-1"),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func splitCSV(v string) []string {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
