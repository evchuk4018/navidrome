package ytdlp

import (
	"context"
	"fmt"
	"net/url"
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

	query := strings.TrimSpace(track.ArtistName + " - " + track.Title)
	if track.AlbumTitle != "" {
		query += " " + track.AlbumTitle
	}
	return c.download(ctx, "ytsearch1:"+query, directory)
}

// DownloadURL downloads a specific HTTP(S) source URL instead of performing a metadata search.
// It is used by administrative imports where the source URL is part of the user's input.
func (c *Client) DownloadURL(ctx context.Context, sourceURL, directory string) (string, error) {
	sourceURL = strings.TrimSpace(sourceURL)
	parsed, err := url.ParseRequestURI(sourceURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("invalid download URL %q", sourceURL)
	}
	path, err := c.download(ctx, sourceURL, directory)
	if err == nil {
		return path, nil
	}

	// YouTube increasingly serves some videos only through a client-specific API.
	// Retry with the Android client when the default web extraction cannot obtain media.
	fallbackPath, fallbackErr := c.download(ctx, sourceURL, directory,
		"--extractor-args", "youtube:player_client=android")
	if fallbackErr == nil {
		return fallbackPath, nil
	}
	return "", fmt.Errorf("default download: %w; android fallback: %v", err, fallbackErr)
}

func (c *Client) download(ctx context.Context, source string, directory string, extraArgs ...string) (string, error) {
	if strings.TrimSpace(source) == "" {
		return "", fmt.Errorf("download source is required")
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
	args := []string{
		"--ignore-config",
		"--no-playlist",
		"--no-progress",
		"--newline",
		"--extract-audio",
		"--audio-format", "mp3",
		"--audio-quality", "320K",
		"--embed-metadata",
		"--parse-metadata", "%(webpage_url)s:%(meta_comment)s",
		"--output", filepath.Join(directory, "%(id)s.%(ext)s"),
		"--print", "after_move:filepath",
	}
	args = append(args, extraArgs...)
	args = append(args, source)
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

// SourceURL extracts the first trusted YouTube URL from imported metadata such as an audio
// comment. yt-dlp stores webpage_url in the comment field when metadata is embedded.
func (c *Client) SourceURL(metadata string) (string, bool) {
	for _, field := range strings.Fields(metadata) {
		candidate := strings.Trim(field, `<>[](){}"'.,;`)
		if isYouTubeURL(candidate) {
			return candidate, true
		}
	}
	return "", false
}

// Thumbnail resolves the thumbnail for a specific YouTube source without downloading media.
func (c *Client) Thumbnail(ctx context.Context, source string) (*url.URL, error) {
	if !isYouTubeURL(source) {
		return nil, model.ErrNotFound
	}
	return c.thumbnail(ctx, source)
}

// SearchThumbnail resolves one YouTube result for legacy tracks that lost their source URL.
func (c *Client) SearchThumbnail(ctx context.Context, query string) (*url.URL, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, model.ErrNotFound
	}
	return c.thumbnail(ctx, "ytsearch1:"+query)
}

func (c *Client) thumbnail(ctx context.Context, source string) (*url.URL, error) {
	if c.runner == nil {
		return nil, fmt.Errorf("yt-dlp runner is not configured")
	}
	args := []string{
		"--ignore-config",
		"--no-playlist",
		"--no-progress",
		"--no-warnings",
		"--skip-download",
		"--print", "after_filter:%(thumbnail)s",
		source,
	}
	output, err := c.runner.Run(ctx, c.executable, args...)
	if err != nil {
		return nil, fmt.Errorf("yt-dlp thumbnail lookup failed: %s", commandOutput(output, err))
	}
	for _, line := range strings.Split(string(output), "\n") {
		candidate := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		parsed, parseErr := url.ParseRequestURI(candidate)
		if parseErr == nil && isTrustedThumbnailURL(parsed) {
			return parsed, nil
		}
	}
	return nil, model.ErrNotFound
}

func isYouTubeURL(source string) bool {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(source))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "youtu.be" || host == "youtube.com" || strings.HasSuffix(host, ".youtube.com")
}

func isTrustedThumbnailURL(imageURL *url.URL) bool {
	if imageURL == nil || imageURL.Host == "" || (imageURL.Scheme != "http" && imageURL.Scheme != "https") {
		return false
	}
	host := strings.ToLower(imageURL.Hostname())
	return host == "ytimg.com" || strings.HasSuffix(host, ".ytimg.com")
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
