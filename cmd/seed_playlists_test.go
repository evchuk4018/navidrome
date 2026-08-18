package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/navidrome/navidrome/model"
)

func TestLoadSeedPlaylistsParsesDefinition(t *testing.T) {
	def := seedPlaylistsFile{Playlists: []seedPlaylist{{
		Name:  "Test Playlist",
		Songs: []seedSongRef{{Artist: "Artist", Title: "Title"}, {Artist: "Other", Title: "Another"}},
	}}}
	data, err := json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "playlists.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}

	parsed, err := loadSeedPlaylists(path)
	if err != nil {
		t.Fatalf("loadSeedPlaylists returned error: %v", err)
	}
	if len(parsed.Playlists) != 1 || parsed.Playlists[0].Name != "Test Playlist" || len(parsed.Playlists[0].Songs) != 2 {
		t.Fatalf("unexpected parsed definition: %+v", parsed)
	}
}

func TestLoadSeedPlaylistsRejectsMissingName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "playlists.json")
	if err := os.WriteFile(path, []byte(`{"playlists":[{"name":"  ","songs":[]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSeedPlaylists(path); err == nil {
		t.Fatal("expected error for playlist with blank name")
	}
}

func TestLoadSeedPlaylistsRejectsInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "playlists.json")
	if err := os.WriteFile(path, []byte(`{"playlists":[`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSeedPlaylists(path); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDedupeSeedSongsDeduplicatesAcrossPlaylists(t *testing.T) {
	def := seedPlaylistsFile{Playlists: []seedPlaylist{
		{Name: "A", Songs: []seedSongRef{{Artist: "Artist", Title: "Song"}, {Artist: "Artist", Title: "Other"}}},
		{Name: "B", Songs: []seedSongRef{{Artist: "artist", Title: "song"}, {Artist: "Other", Title: "Third"}}},
	}}
	unique := dedupeSeedSongs(def)
	if len(unique) != 3 {
		t.Fatalf("expected 3 unique songs, got %d: %+v", len(unique), unique)
	}
}

func TestSeedSongKeyNormalizes(t *testing.T) {
	key := seedSongKey(seedSongRef{Artist: "Artist - Topic", Title: "Song (Official Video)"})
	want := seedSongKey(seedSongRef{Artist: "artist", Title: "song"})
	if key == "" || key != want {
		t.Fatalf("unexpected normalized key %q (want %q)", key, want)
	}
}

func TestSeedTrackMatchesTitlesAndArtists(t *testing.T) {
	cases := []struct {
		name  string
		ref   seedSongRef
		track model.ExternalTrack
		want  bool
	}{
		{
			name:  "exact match",
			ref:   seedSongRef{Artist: "Radiohead", Title: "Karma Police"},
			track: model.ExternalTrack{ArtistName: "Radiohead", Title: "Karma Police"},
			want:  true,
		},
		{
			name:  "case and suffix normalized",
			ref:   seedSongRef{Artist: "Bella Poarch", Title: "Build a Bitch"},
			track: model.ExternalTrack{ArtistName: "Bella Poarch", Title: "Build a Bitch (Official Music Video)"},
			want:  true,
		},
		{
			name:  "feat clause in reference omitted in track",
			ref:   seedSongRef{Artist: "Cardi B", Title: "WAP (feat. Megan Thee Stallion)"},
			track: model.ExternalTrack{ArtistName: "Cardi B", Title: "WAP"},
			want:  true,
		},
		{
			name:  "remix suffix in reference",
			ref:   seedSongRef{Artist: "Charli XCX", Title: "Guess (Remix)"},
			track: model.ExternalTrack{ArtistName: "Charli XCX", Title: "Guess"},
			want:  true,
		},
		{
			name:  "feat clause in track title too",
			ref:   seedSongRef{Artist: "Drake", Title: "One Dance (feat. Wizkid & Kyla)"},
			track: model.ExternalTrack{ArtistName: "Drake", Title: "One Dance (feat. Wizkid & Kyla)"},
			want:  true,
		},
		{
			name:  "wrong artist rejected",
			ref:   seedSongRef{Artist: "Mitski", Title: "Nobody"},
			track: model.ExternalTrack{ArtistName: "Katy Perry", Title: "Nobody"},
			want:  false,
		},
		{
			name:  "wrong title rejected",
			ref:   seedSongRef{Artist: "Drake", Title: "Hotline Bling"},
			track: model.ExternalTrack{ArtistName: "Drake", Title: "Hotling Bing"},
			want:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := seedTrackMatches(tc.ref, tc.track); got != tc.want {
				t.Fatalf("seedTrackMatches(%+v, %+v) = %v, want %v", tc.ref, tc.track, got, tc.want)
			}
		})
	}
}

func TestStripSeedTitleSuffix(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"WAP (feat. Megan Thee Stallion)", "WAP"},
		{"Boy's a Liar Pt. 2 (feat. Ice Spice)", "Boy's a Liar Pt. 2"},
		{"Guess (Remix)", "Guess"},
		{"Bubble Pop Electric (Remastered 2019)", "Bubble Pop Electric"},
		{"Karma Police", "Karma Police"},
		{"Decode (Acoustic)", "Decode"},
	}
	for _, tc := range cases {
		if got := stripSeedTitleSuffix(tc.in); got != tc.want {
			t.Fatalf("stripSeedTitleSuffix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStripSeedCreditsKeepsVersionMarkers(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"WAP (feat. Megan Thee Stallion)", "WAP"},
		{"One Dance (feat. Wizkid & Kyla)", "One Dance"},
		{"Guess (Remix)", "Guess (Remix)"},
		{"Snooze (Remix)", "Snooze (Remix)"},
		{"That's What You Get (Acoustic)", "That's What You Get (Acoustic)"},
		{"Karma Police", "Karma Police"},
	}
	for _, tc := range cases {
		if got := stripSeedCredits(tc.in); got != tc.want {
			t.Fatalf("stripSeedCredits(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSeedSearchQueryBuildsStructuredLucene(t *testing.T) {
	ref := seedSongRef{Artist: `Kid LAROI & Justin Bieber`, Title: `STAY (feat. Someone)`}
	query := seedSearchQuery(ref)
	if !strings.Contains(query, `artist:"Kid LAROI & Justin Bieber"`) {
		t.Fatalf("query missing artist clause: %q", query)
	}
	if !strings.Contains(query, `recording:"STAY"`) {
		t.Fatalf("query missing stripped title clause: %q", query)
	}
	if strings.Contains(query, "(feat.") {
		t.Fatalf("query should strip credit clauses: %q", query)
	}
}

func TestSeedEscapeLucene(t *testing.T) {
	if got := seedEscapeLucene(`a "quoted" \ title`); got != `a \"quoted\" \\ title` {
		t.Fatalf("unexpected escaping: %q", got)
	}
}

func TestSeedTitleMatchesToleratesVersionSuffix(t *testing.T) {
	if !seedTitleMatches("Snooze (Remix)", "Snooze") {
		t.Fatal("expected Snooze (Remix) reference to match Snooze")
	}
	if !seedTitleMatches("Guess (Remix)", "Guess") {
		t.Fatal("expected Guess (Remix) reference to match Guess")
	}
	if seedTitleMatches("Red Wine Supernova", "Supernova") {
		t.Fatal("did not expect partial title to match")
	}
}

func TestSeedLibraryRowStripsCredits(t *testing.T) {
	row := seedLibraryRow(seedSongRef{Artist: "Cardi B", Title: "WAP (feat. Megan Thee Stallion)"})
	if row.Title != "WAP" || row.Artist != "Cardi B" {
		t.Fatalf("unexpected seed library row: %+v", row)
	}
}

func TestPrintSeedSummaryReportsCounts(t *testing.T) {
	summary := &seedSummary{
		Defined:       4,
		UniqueSongs:   3,
		InLibrary:     1,
		AlreadyQueued: 1,
		Queued:        1,
		ResolveFailed: []string{"Artist - Missing: not found"},
		QueueFailed:   []string{"Artist - Broken: validation failed"},
		Playlists:     []seedPlaylistResult{{Name: "Pop", PlaylistID: "pls-1", Matched: 2, Total: 3}},
	}
	var output bytes.Buffer
	printSeedSummary(&output, summary)
	text := output.String()
	for _, fragment := range []string{
		"defined=4 unique=3 in_library=1 queued=1 already_queued=1",
		"resolve failure: Artist - Missing: not found",
		"queue failure: Artist - Broken: validation failed",
		`playlist "Pop" (pls-1): matched 2/3 songs`,
	} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("summary output missing %q:\n%s", fragment, text)
		}
	}
}
