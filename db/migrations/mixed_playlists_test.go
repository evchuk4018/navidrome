package migrations

import (
	"context"
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestMixedPlaylistMigration(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE playlist_tracks(id INTEGER,playlist_id TEXT,media_file_id TEXT NOT NULL);CREATE TABLE annotation(item_type TEXT);INSERT INTO playlist_tracks VALUES(1,'playlist','song');`)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = Up20261006120000(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var source, id string
	if err = db.QueryRow("SELECT source,media_file_id FROM playlist_tracks").Scan(&source, &id); err != nil {
		t.Fatal(err)
	}
	if source != "music" || id != "song" {
		t.Fatalf("migration changed legacy entry: %s %s", source, id)
	}
	if _, err = db.Exec("INSERT INTO playlist_tracks(id,playlist_id,media_file_id,source,video_id) VALUES(2,'playlist','','hometube','video')"); err != nil {
		t.Fatal(err)
	}
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = Down20261006120000(context.Background(), tx); err == nil {
		t.Fatal("rollback must refuse to discard videos")
	}
	_ = tx.Rollback()
}
