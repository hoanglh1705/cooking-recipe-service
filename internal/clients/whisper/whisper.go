// Package whisper transcribes audio. The MVP uses OpenAI's whisper-1
// endpoint (multipart/form-data with a file field). When unconfigured,
// the Stub transcriber returns a deterministic placeholder so the pipeline
// can be wired end-to-end without API credentials.
package whisper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type Transcriber interface {
	Transcribe(ctx context.Context, audioPath string) (string, error)
}

func New(provider, openaiKey, openaiModel string) Transcriber {
	switch provider {
	case "openai":
		if openaiKey == "" {
			return Stub{}
		}
		return &OpenAI{APIKey: openaiKey, Model: openaiModel, HTTP: &http.Client{Timeout: 5 * time.Minute}}
	default:
		return Stub{}
	}
}

type OpenAI struct {
	APIKey string
	Model  string
	HTTP   *http.Client
}

type openaiTranscriptResp struct {
	Text  string `json:"text"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func (o *OpenAI) Transcribe(ctx context.Context, audioPath string) (string, error) {
	f, err := os.Open(audioPath)
	if err != nil {
		return "", fmt.Errorf("open audio: %w", err)
	}
	defer f.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("model", o.Model); err != nil {
		return "", err
	}
	if err := mw.WriteField("response_format", "json"); err != nil {
		return "", err
	}
	if err := mw.WriteField("language", "vi"); err != nil {
		return "", err
	}
	part, err := mw.CreateFormFile("file", filepath.Base(audioPath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, f); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.openai.com/v1/audio/transcriptions", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+o.APIKey)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := o.HTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("whisper: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("whisper %d: %s", resp.StatusCode, string(raw))
	}
	var parsed openaiTranscriptResp
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", err
	}
	if parsed.Error != nil {
		return "", fmt.Errorf("whisper: %s", parsed.Error.Message)
	}
	return parsed.Text, nil
}

type Stub struct{}

func (Stub) Transcribe(_ context.Context, audioPath string) (string, error) {
	return "[stub transcript for " + filepath.Base(audioPath) + "] Cấu hình WHISPER_PROVIDER + OPENAI_API_KEY để có transcript thật.", nil
}
