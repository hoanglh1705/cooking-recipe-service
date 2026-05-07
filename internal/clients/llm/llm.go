// Package llm provides a small interface over Claude / OpenAI that returns
// strict JSON. We only need a single "compose recipe" call, so the surface
// area stays minimal and we can swap providers via config.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Composer turns a JSON-output prompt into raw JSON bytes.
type Composer interface {
	ComposeJSON(ctx context.Context, system, user string) ([]byte, error)
}

// New returns a Composer for the configured provider, or a stub if no key is set.
func New(provider, anthropicKey, anthropicModel, openaiKey, openaiModel string) Composer {
	switch provider {
	case "anthropic":
		if anthropicKey == "" {
			return Stub{}
		}
		return &Anthropic{APIKey: anthropicKey, Model: anthropicModel, HTTP: defaultHTTP()}
	case "openai":
		if openaiKey == "" {
			return Stub{}
		}
		return &OpenAI{APIKey: openaiKey, Model: openaiModel, HTTP: defaultHTTP()}
	default:
		return Stub{}
	}
}

func defaultHTTP() *http.Client {
	return &http.Client{Timeout: 90 * time.Second}
}

// ---------- Anthropic ----------

type Anthropic struct {
	APIKey string
	Model  string
	HTTP   *http.Client
}

type anthropicReq struct {
	Model     string                  `json:"model"`
	MaxTokens int                     `json:"max_tokens"`
	System    string                  `json:"system,omitempty"`
	Messages  []anthropicMessage      `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResp struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (a *Anthropic) ComposeJSON(ctx context.Context, system, user string) ([]byte, error) {
	body, _ := json.Marshal(anthropicReq{
		Model:     a.Model,
		MaxTokens: 4096,
		System:    system,
		Messages: []anthropicMessage{
			{Role: "user", Content: user},
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic %d: %s", resp.StatusCode, string(raw))
	}
	var parsed anthropicResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("anthropic decode: %w", err)
	}
	if parsed.Error != nil {
		return nil, errors.New(parsed.Error.Message)
	}
	if len(parsed.Content) == 0 {
		return nil, errors.New("anthropic: empty content")
	}
	return []byte(parsed.Content[0].Text), nil
}

// ---------- OpenAI ----------

type OpenAI struct {
	APIKey string
	Model  string
	HTTP   *http.Client
}

type openaiReq struct {
	Model          string         `json:"model"`
	Messages       []openaiMsg    `json:"messages"`
	ResponseFormat map[string]any `json:"response_format,omitempty"`
}

type openaiMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResp struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (o *OpenAI) ComposeJSON(ctx context.Context, system, user string) ([]byte, error) {
	body, _ := json.Marshal(openaiReq{
		Model: o.Model,
		Messages: []openaiMsg{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		ResponseFormat: map[string]any{"type": "json_object"},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.APIKey)

	resp, err := o.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai %d: %s", resp.StatusCode, string(raw))
	}
	var parsed openaiResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("openai decode: %w", err)
	}
	if parsed.Error != nil {
		return nil, errors.New(parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return nil, errors.New("openai: no choices")
	}
	return []byte(parsed.Choices[0].Message.Content), nil
}

// ---------- Stub ----------

// Stub returns a deterministic minimal recipe so the pipeline is testable
// end-to-end without external API credentials.
type Stub struct{}

func (Stub) ComposeJSON(_ context.Context, _, _ string) ([]byte, error) {
	return []byte(`{"title":"(stub) Công thức mẫu","description":"Đây là công thức mẫu được sinh khi LLM provider chưa cấu hình.","intro_md":"","prep_time_min":10,"cook_time_min":20,"total_time_min":30,"servings":2,"difficulty":"easy","calories_per_serving":null,"ingredients":[{"name":"Nguyên liệu mẫu","quantity":1,"unit":"phần","note":""}],"steps":[{"instruction":"Bước mẫu — vui lòng cấu hình API key để có công thức thật.","duration_s":300}],"tips":[],"variations":[],"faqs":[],"seo_title":"Công thức mẫu","seo_keywords":[]}`), nil
}
