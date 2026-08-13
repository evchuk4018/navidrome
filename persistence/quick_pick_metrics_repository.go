package persistence

import (
	"database/sql"
	"time"

	"github.com/navidrome/navidrome/model"
)

type quickPickMetricsRepository struct {
	db *sql.DB
}

func NewQuickPickMetricsRepository(db *sql.DB) model.QuickPickMetricsRepository {
	return &quickPickMetricsRepository{db: db}
}

func (r *quickPickMetricsRepository) SongRecentPlays(userID string, since time.Time) (map[string]int64, error) {
	rows, err := r.db.Query(`
		select media_file_id, count(*)
		from scrobbles
		where user_id = ? and submission_time >= ?
		group by media_file_id`, userID, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]int64{}
	for rows.Next() {
		var id string
		var count int64
		if err := rows.Scan(&id, &count); err != nil {
			return nil, err
		}
		result[id] = count
	}
	return result, rows.Err()
}

func (r *quickPickMetricsRepository) PlaylistMetrics(userID string, since time.Time) (map[string]model.PlaylistPlayMetric, error) {
	rows, err := r.db.Query(`
		select playlist_id,
		       count(*) as total_starts,
		       sum(case when played_at >= ? then 1 else 0 end) as recent_starts,
		       max(played_at) as last_played
		from playlist_play_history
		where user_id = ?
		group by playlist_id`, since.UTC(), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]model.PlaylistPlayMetric{}
	for rows.Next() {
		var metric model.PlaylistPlayMetric
		var last sql.NullTime
		if err := rows.Scan(&metric.PlaylistID, &metric.TotalStarts, &metric.RecentStarts, &last); err != nil {
			return nil, err
		}
		if last.Valid {
			metric.LastPlayed = &last.Time
		}
		result[metric.PlaylistID] = metric
	}
	return result, rows.Err()
}

func (r *quickPickMetricsRepository) RecordPlaylistPlay(userID, playlistID string, playedAt time.Time) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`insert into playlist_play_history (user_id, playlist_id, played_at) values (?, ?, ?)`,
		userID, playlistID, playedAt.UTC()); err != nil {
		return err
	}
	_, err = tx.Exec(`
		insert into annotation (user_id, item_id, item_type, play_count, play_date)
		values (?, ?, 'playlist', 1, ?)
		on conflict (user_id, item_id, item_type) do update set
			play_count = coalesce(play_count, 0) + 1,
			play_date = max(coalesce(play_date, ''), excluded.play_date)`,
		userID, playlistID, playedAt.UTC())
	if err != nil {
		return err
	}
	return tx.Commit()
}

var _ model.QuickPickMetricsRepository = (*quickPickMetricsRepository)(nil)
