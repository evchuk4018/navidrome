package migrations

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
)

func init() { goose.AddMigrationContext(Up20261006120000, Down20261006120000) }

func Up20261006120000(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE hometube_video (
 id TEXT PRIMARY KEY, title TEXT NOT NULL, channel_id TEXT NOT NULL,
 channel_name TEXT NOT NULL, thumbnail_url TEXT NOT NULL, duration_seconds REAL NOT NULL DEFAULT 0
);
ALTER TABLE playlist_tracks ADD COLUMN source TEXT NOT NULL DEFAULT 'music' CHECK(source IN ('music','hometube'));
ALTER TABLE playlist_tracks ADD COLUMN video_id TEXT NOT NULL DEFAULT '';
CREATE INDEX playlist_tracks_video ON playlist_tracks(video_id);
`)
	return err
}

func Down20261006120000(ctx context.Context, tx *sql.Tx) error {
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM playlist_tracks WHERE source = 'hometube'").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("mixed playlists exist; restore the database backup to roll back without losing videos")
	}
	_, err := tx.ExecContext(ctx, `DROP INDEX playlist_tracks_video; ALTER TABLE playlist_tracks DROP COLUMN video_id; ALTER TABLE playlist_tracks DROP COLUMN source; DROP TABLE hometube_video; DELETE FROM annotation WHERE item_type = 'hometube_video';`)
	return err
}
