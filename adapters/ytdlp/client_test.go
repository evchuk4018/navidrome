package ytdlp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	for _, expected := range []string{"--ignore-config", "--no-playlist", "--extract-audio", "--audio-format", "mp3", "--audio-quality", "320K"} {
		if !slices.Contains(runner.args, expected) {
			t.Fatalf("expected %q in command args %v", expected, runner.args)
		}
	}
	if !slices.Contains(runner.args, "ytsearch1:Artist - Song Album") {
		t.Fatalf("expected source search query in command args %v", runner.args)
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
	args  [][]string
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
