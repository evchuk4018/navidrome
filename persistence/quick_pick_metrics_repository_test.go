package persistence

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

func TestQuickPickMetricsParsesSQLiteTextTimestamps(t *testing.T) {
	database, err := sql.Open("sqlite3", "file:quick-pick-metrics-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })

	_, err = database.Exec(`
		create table playlist_play_history (
			user_id text not null,
			playlist_id text not null,
			played_at datetime not null
		);
		insert into playlist_play_history (user_id, playlist_id, played_at)
		values ('user', 'playlist', '2026-08-13 21:53:18.123456789+00:00');
	`)
	if err != nil {
		t.Fatal(err)
	}

	repository := &quickPickMetricsRepository{db: database}
	metrics, err := repository.PlaylistMetrics("user", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	lastPlayed := metrics["playlist"].LastPlayed
	if lastPlayed == nil {
		t.Fatal("expected last played timestamp")
	}
	want := time.Date(2026, 8, 13, 21, 53, 18, 123456789, time.UTC)
	if !lastPlayed.Equal(want) {
		t.Fatalf("got last played %s, want %s", lastPlayed, want)
	}
}
