package ytdlp

import (
	"context"
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
