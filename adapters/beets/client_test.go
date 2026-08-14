package beets

import (
	"context"
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
	return nil, nil
}

func TestImportCreatesConfigAndSetsMetadata(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "beets.yaml")
	filePath := filepath.Join(directory, "track.mp3")
	if err := os.WriteFile(filePath, []byte("audio"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{}
	client := NewWithRunner("beet", runner, "/music", configPath, "/data/beets.db")
	if err := client.Import(context.Background(), []string{filePath}, model.ExternalTrack{
		Title:      "Song",
		ArtistName: "Artist",
		AlbumTitle: "Album",
		Year:       2024,
	}, true); err != nil {
		t.Fatalf("Import returned error: %v", err)
	}
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(config), "move: yes") {
		t.Fatalf("expected moving importer config, got %s", config)
	}
	if !strings.Contains(string(config), "quiet_fallback: asis") {
		t.Fatalf("expected quiet imports to fall back to as-is, got %s", config)
	}
	if !slices.Contains(runner.args, "-s") || !slices.Contains(runner.args, "--set") {
		t.Fatalf("expected singleton metadata args, got %v", runner.args)
	}
	if !slices.Contains(runner.args, "-A") {
		t.Fatalf("expected importer to preserve supplied metadata without autotagging, got %v", runner.args)
	}
	if !slices.Contains(runner.args, "--quiet-fallback=asis") {
		t.Fatalf("expected quiet fallback override, got %v", runner.args)
	}
}
