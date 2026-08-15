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
	calls [][]string
}

func (r *recordingRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string(nil), args...))
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
		ID:         "recording-1",
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
	if len(runner.calls) != 2 {
		t.Fatalf("expected import and tag write calls, got %v", runner.calls)
	}
	importArgs := runner.calls[0]
	if !slices.Contains(importArgs, "-s") || !slices.Contains(importArgs, "--set") {
		t.Fatalf("expected singleton metadata args, got %v", importArgs)
	}
	if !slices.Contains(importArgs, "-A") {
		t.Fatalf("expected importer to preserve supplied metadata without autotagging, got %v", importArgs)
	}
	if !slices.Contains(importArgs, "--quiet-fallback=asis") {
		t.Fatalf("expected quiet fallback override, got %v", importArgs)
	}
	if writeArgs := runner.calls[1]; !slices.Contains(writeArgs, "write") || !slices.Contains(writeArgs, "mb_trackid:recording-1") {
		t.Fatalf("expected tag write by recording ID, got %v", writeArgs)
	}
}

func TestImportWritesAlbumTagsByArtistAndAlbum(t *testing.T) {
	runner := &recordingRunner{}
	client := NewWithRunner("beet", runner, "/music", "/tmp/beets.yaml", "/data/beets.db")
	if err := client.Import(context.Background(), []string{"/tmp/track.mp3"}, model.ExternalTrack{
		ArtistName: "Artist",
		AlbumTitle: "Some Album",
		Year:       2024,
	}, false); err != nil {
		t.Fatalf("Import returned error: %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("expected import and tag write calls, got %v", runner.calls)
	}
	writeArgs := runner.calls[1]
	if !slices.Contains(writeArgs, "write") || !slices.Contains(writeArgs, `albumartist:Artist album:"Some Album"`) {
		t.Fatalf("expected album tag write by artist and album, got %v", writeArgs)
	}
}

func TestImportSkipsTagWriteWithoutMatchableFields(t *testing.T) {
	runner := &recordingRunner{}
	client := NewWithRunner("beet", runner, "/music", "/tmp/beets.yaml", "/data/beets.db")
	if err := client.Import(context.Background(), []string{"/tmp/track.mp3"}, model.ExternalTrack{}, false); err != nil {
		t.Fatalf("Import returned error: %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("expected a single import call when no fields can select the item, got %v", runner.calls)
	}
}
