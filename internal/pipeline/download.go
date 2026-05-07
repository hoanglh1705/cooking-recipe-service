package pipeline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// DownloadAudio invokes yt-dlp to fetch audio-only mp3 for the given URL,
// writing it under audioDir. Returns the resulting file path.
//
// We pass quality "9" because speech-to-text doesn't benefit from a higher
// bitrate, and a smaller file means a faster/cheaper Whisper call.
func DownloadAudio(ctx context.Context, ytdlpPath, audioDir string, videoID int64, url string) (string, error) {
	if ytdlpPath == "" {
		ytdlpPath = "yt-dlp"
	}
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir audio dir: %w", err)
	}

	out := filepath.Join(audioDir, fmt.Sprintf("video_%d.%%(ext)s", videoID))
	expected := filepath.Join(audioDir, fmt.Sprintf("video_%d.mp3", videoID))

	if _, err := exec.LookPath(ytdlpPath); err != nil {
		return "", fmt.Errorf("yt-dlp binary not found at %q: %w", ytdlpPath, err)
	}

	cmd := exec.CommandContext(ctx, ytdlpPath,
		"-x",
		"--audio-format", "mp3",
		"--audio-quality", "9",
		"-o", out,
		url,
	)
	if combined, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("yt-dlp: %w\n%s", err, string(combined))
	}

	if _, err := os.Stat(expected); err != nil {
		return "", fmt.Errorf("expected audio file missing: %s", expected)
	}
	return expected, nil
}
