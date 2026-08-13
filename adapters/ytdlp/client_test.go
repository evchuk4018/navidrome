package ytdlp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/navidrome/navidrome/model"
)

type recordingRunner struct {
	args []string
}

func (r *recordingRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.args = args
	outputDir := ""
	for index, arg := range args {
		if arg == "--output" && index+1 < len(args) {
			outputDir = filepath.Dir(args[index+1])
		}
	}
	path := filepath.Join(outputDir, "source.mp3")
	if err := os.WriteFile(path, []byte("audio"), 0600); err != nil {
		return nil, err
	}
	return []byte(path + "\n"), nil
}

func TestDownloadBuildsSafeAudioCommand(t *testing.T) {
	runner := &recordingRunner{}
	client := NewWithRunner("yt-dlp", runner)
	path, err := client.Download(context.Background(), model.ExternalTrack{
		Title:      "Song",
		ArtistName: "Artist",
		AlbumTitle: "Album",
	}, t.TempDir())
	if err != nil {
		t.Fatalf("Download returned error: %v", err)
	}
	if filepath.Ext(path) != ".mp3" {
		t.Fatalf("expected mp3 output, got %q", path)
	}
	for _, expected := range []string{"--ignore-config", "--js-runtimes", "node", "--no-playlist", "--extract-audio", "--audio-format", "mp3", "--audio-quality", "320K", "%(webpage_url)s:%(meta_comment)s"} {
		if !slices.Contains(runner.args, expected) {
			t.Fatalf("expected %q in command args %v", expected, runner.args)
		}
	}
	if !slices.Contains(runner.args, "ytsearch1:Artist - Song Album") {
		t.Fatalf("expected source search query in command args %v", runner.args)
	}
}

type outputRunner struct {
	output []byte
	err    error
	args   []string
}

func (r *outputRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.args = args
	return r.output, r.err
}

func TestSourceURLExtractsTrustedYouTubeHosts(t *testing.T) {
	client := NewWithRunner("yt-dlp", &outputRunner{})
	for _, metadata := range []string{
		"https://www.youtube.com/watch?v=abc123",
		"source: <https://youtu.be/abc123>, imported",
		"https://music.youtube.com/watch?v=abc123",
	} {
		if got, ok := client.SourceURL(metadata); !ok || got == "" {
			t.Fatalf("expected source URL from %q, got %q, %v", metadata, got, ok)
		}
	}
	if _, ok := client.SourceURL("https://example.com/watch?v=abc123"); ok {
		t.Fatal("expected non-YouTube URL to be rejected")
	}
}

func TestThumbnailUsesMetadataOnlyCommandAndTrustedImageHost(t *testing.T) {
	runner := &outputRunner{output: []byte("https://i.ytimg.com/vi/abc123/maxresdefault.jpg\n")}
	client := NewWithRunner("yt-dlp", runner)
	imageURL, err := client.Thumbnail(context.Background(), "https://www.youtube.com/watch?v=abc123")
	if err != nil {
		t.Fatalf("Thumbnail returned error: %v", err)
	}
	if imageURL.String() != "https://i.ytimg.com/vi/abc123/maxresdefault.jpg" {
		t.Fatalf("unexpected thumbnail URL %q", imageURL)
	}
	for _, expected := range []string{"--js-runtimes", "node", "--skip-download", "after_filter:%(thumbnail)s"} {
		if !slices.Contains(runner.args, expected) {
			t.Fatalf("expected %q in command args %v", expected, runner.args)
		}
	}
}

func TestThumbnailRejectsUntrustedImageHost(t *testing.T) {
	client := NewWithRunner("yt-dlp", &outputRunner{output: []byte("https://example.com/cover.jpg\n")})
	if _, err := client.Thumbnail(context.Background(), "https://youtu.be/abc123"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("expected not found for untrusted thumbnail, got %v", err)
	}
}

func TestSearchThumbnailUsesSingleYouTubeResult(t *testing.T) {
	runner := &outputRunner{output: []byte("https://i.ytimg.com/vi/abc123/hqdefault.jpg\n")}
	client := NewWithRunner("yt-dlp", runner)
	if _, err := client.SearchThumbnail(context.Background(), "Artist - Song"); err != nil {
		t.Fatalf("SearchThumbnail returned error: %v", err)
	}
	if !slices.Contains(runner.args, "ytsearch1:Artist - Song") {
		t.Fatalf("expected one-result search in args %v", runner.args)
	}
}

func TestDownloadURLUsesExactSource(t *testing.T) {
	runner := &recordingRunner{}
	client := NewWithRunner("yt-dlp", runner)
	const sourceURL = "https://www.youtube.com/watch?v=abc123"

	if _, err := client.DownloadURL(context.Background(), sourceURL, t.TempDir()); err != nil {
		t.Fatalf("DownloadURL returned error: %v", err)
	}
	if !slices.Contains(runner.args, sourceURL) {
		t.Fatalf("expected exact source URL in command args %v", runner.args)
	}
}

func TestDownloadURLRejectsNonHTTPSource(t *testing.T) {
	client := NewWithRunner("yt-dlp", &recordingRunner{})
	if _, err := client.DownloadURL(context.Background(), "ytsearch1:Artist - Song", t.TempDir()); err == nil {
		t.Fatal("expected invalid source URL error")
	}
}

type fallbackRunner struct {
	args   [][]string
	called int
}

func (r *fallbackRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.called++
	r.args = append(r.args, args)
	if r.called == 1 {
		return []byte("403 Forbidden"), fmt.Errorf("download failed")
	}
	outputDir := ""
	for index, arg := range args {
		if arg == "--output" && index+1 < len(args) {
			outputDir = filepath.Dir(args[index+1])
		}
	}
	path := filepath.Join(outputDir, "fallback.mp3")
	if err := os.WriteFile(path, []byte("audio"), 0600); err != nil {
		return nil, err
	}
	return []byte(path + "\n"), nil
}

func TestDownloadURLRetriesWithAndroidClient(t *testing.T) {
	runner := &fallbackRunner{}
	client := NewWithRunner("yt-dlp", runner)
	if _, err := client.DownloadURL(context.Background(), "https://www.youtube.com/watch?v=abc123", t.TempDir()); err != nil {
		t.Fatalf("DownloadURL returned error: %v", err)
	}
	if runner.called != 2 {
		t.Fatalf("expected default and fallback attempts, got %d", runner.called)
	}
	if !slices.Contains(runner.args[1], "youtube:player_client=android") {
		t.Fatalf("expected Android fallback args, got %v", runner.args[1])
	}
}

func TestDownloadSearchRetriesWithAndroidClient(t *testing.T) {
	runner := &fallbackRunner{}
	client := NewWithRunner("yt-dlp", runner)
	track := model.ExternalTrack{ArtistName: "Cavetown", Title: "Devil Town"}

	if _, err := client.Download(context.Background(), track, t.TempDir()); err != nil {
		t.Fatalf("Download returned error: %v", err)
	}
	if runner.called != 2 {
		t.Fatalf("expected default and fallback search attempts, got %d", runner.called)
	}
	if !slices.Contains(runner.args[1], "ytsearch1:Cavetown - Devil Town") {
		t.Fatalf("expected the search source in fallback args, got %v", runner.args[1])
	}
	if !slices.Contains(runner.args[1], "youtube:player_client=android") {
		t.Fatalf("expected Android fallback args, got %v", runner.args[1])
	}
}

type failingRunner struct {
	called int
}

func (r *failingRunner) Run(_ context.Context, _ string, _ ...string) ([]byte, error) {
	r.called++
	if r.called == 1 {
		return []byte("HTTP Error 403: Forbidden"), fmt.Errorf("download failed")
	}
	return []byte("video unavailable"), fmt.Errorf("fallback failed")
}

func TestDownloadReportsBothYouTubeAttempts(t *testing.T) {
	runner := &failingRunner{}
	client := NewWithRunner("yt-dlp", runner)
	_, err := client.Download(context.Background(), model.ExternalTrack{
		ArtistName: "Cavetown",
		Title:      "Devil Town",
	}, t.TempDir())
	if err == nil {
		t.Fatal("expected both download attempts to fail")
	}
	for _, detail := range []string{"default download", "403: Forbidden", "android fallback", "video unavailable"} {
		if !strings.Contains(err.Error(), detail) {
			t.Fatalf("expected error %q to contain %q", err, detail)
		}
	}
}
