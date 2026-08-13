package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/navidrome/navidrome/model"
)

func TestReadLikedMusicRowsDeduplicatesURLs(t *testing.T) {
	delimiter := " " + string(rune(0x2014)) + " "
	content := "Song" + delimiter + "Artist" + delimiter + "https://www.youtube.com/watch?v=one\n" +
		"Duplicate" + delimiter + "Artist" + delimiter + "https://www.youtube.com/watch?v=one\n" +
		"Second" + delimiter + "Artist" + delimiter + "https://youtu.be/two\n"
	path := filepath.Join(t.TempDir(), "liked.txt")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	rows, result, err := readLikedMusicRows(path)
	if err != nil {
		t.Fatalf("readLikedMusicRows returned error: %v", err)
	}
	if result.Rows != 3 || result.UniqueURLs != 2 || result.DuplicateURLs != 1 {
		t.Fatalf("unexpected parse counts: %+v", result)
	}
	if len(rows) != 2 || rows[0].URL == rows[1].URL {
		t.Fatalf("unexpected deduplicated rows: %+v", rows)
	}
}

func TestFindLikedMusicMatchesNormalizesMetadata(t *testing.T) {
	row := likedMusicImportRow{
		Title:  "Song (Official Music Video)",
		Artist: "Artist - Topic",
		URL:    "https://www.youtube.com/watch?v=one",
	}
	files := model.MediaFiles{{ID: "song-1", Title: "Song", Artist: "Artist"}}
	matches := findLikedMusicMatches(files, row)
	if len(matches) != 1 || matches[0].ID != "song-1" {
		t.Fatalf("expected one normalized match, got %+v", matches)
	}
}

func TestFindLikedMusicMatchesRejectsAmbiguousMetadata(t *testing.T) {
	row := likedMusicImportRow{Title: "Song", Artist: "Artist"}
	files := model.MediaFiles{
		{ID: "song-1", Title: "Song", Artist: "Artist"},
		{ID: "song-2", Title: "Song", AlbumArtist: "Artist"},
	}
	if matches := findLikedMusicMatches(files, row); len(matches) != 2 {
		t.Fatalf("expected two ambiguous matches, got %+v", matches)
	}
}

func TestValidateYouTubeImportURL(t *testing.T) {
	valid := []string{
		"https://www.youtube.com/watch?v=abc",
		"https://youtu.be/abc",
	}
	for _, value := range valid {
		if err := validateYouTubeImportURL(value); err != nil {
			t.Errorf("expected %q to be valid: %v", value, err)
		}
	}
	for _, value := range []string{"ytsearch1:Artist - Song", "https://example.com/song"} {
		if err := validateYouTubeImportURL(value); err == nil {
			t.Errorf("expected %q to be rejected", value)
		}
	}
}
