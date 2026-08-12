package ytdlp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/navidrome/navidrome/model"
)

type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type osRunner struct{}

func (osRunner) Run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, executable, args...) // #nosec G204 -- executable is fixed by server configuration.
	return cmd.CombinedOutput()
}

type Client struct {
	executable string
	runner     Runner
}

func New() *Client {
	return NewWithRunner("yt-dlp", osRunner{})
}

func NewWithRunner(executable string, runner Runner) *Client {
	if strings.TrimSpace(executable) == "" {
		executable = "yt-dlp"
	}
	return &Client{executable: executable, runner: runner}
}

func (c *Client) Download(ctx context.Context, track model.ExternalTrack, directory string) (string, error) {
	if strings.TrimSpace(track.Title) == "" {
		return "", fmt.Errorf("track title is required")
	}
	if strings.TrimSpace(directory) == "" {
		return "", fmt.Errorf("download directory is required")
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return "", fmt.Errorf("create yt-dlp directory: %w", err)
	}
	directory, err := filepath.Abs(directory)
	if err != nil {
		return "", fmt.Errorf("resolve yt-dlp directory: %w", err)
	}
	if c.runner == nil {
		return "", fmt.Errorf("yt-dlp runner is not configured")
	}

	query := strings.TrimSpace(track.ArtistName + " - " + track.Title)
	if track.AlbumTitle != "" {
		query += " " + track.AlbumTitle
	}
	args := []string{
		"--ignore-config",
		"--no-playlist",
		"--no-progress",
		"--newline",
		"--extract-audio",
		"--audio-format", "mp3",
		"--audio-quality", "320K",
		"--embed-metadata",
		"--output", filepath.Join(directory, "%(id)s.%(ext)s"),
		"--print", "after_move:filepath",
		"ytsearch1:" + query,
	}
	output, err := c.runner.Run(ctx, c.executable, args...)
	if err != nil {
		return "", fmt.Errorf("yt-dlp failed: %s", commandOutput(output, err))
	}
	path, err := findAudioFile(directory, output)
	if err != nil {
		return "", err
	}
	return path, nil
}

func findAudioFile(directory string, output []byte) (string, error) {
	for _, line := range strings.Split(string(output), "\n") {
		candidate := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	patterns := []string{"*.mp3", "*.m4a", "*.opus", "*.webm", "*.wav", "*.flac"}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(filepath.Join(directory, pattern))
		if err != nil {
			return "", err
		}
		if len(matches) > 0 {
			return matches[0], nil
		}
	}
	return "", fmt.Errorf("yt-dlp completed without producing an audio file")
}

func commandOutput(output []byte, err error) string {
	message := strings.TrimSpace(string(output))
	if message == "" {
		return err.Error()
	}
	if len(message) > 1000 {
		message = message[len(message)-1000:]
	}
	return message
}

var _ interface {
	Download(context.Context, model.ExternalTrack, string) (string, error)
} = (*Client)(nil)
