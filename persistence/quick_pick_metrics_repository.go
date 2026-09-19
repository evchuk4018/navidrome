package persistence

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/navidrome/navidrome/core/recommendations"
	"github.com/navidrome/navidrome/model"
)

type quickPickMetricsRepository struct {
	db *sql.DB
}

const quickPickExposureBatchSize = 400

// nullableSQLiteTime accepts both time.Time values and the timestamp strings
// returned by SQLite for aggregate expressions such as max(played_at).
type nullableSQLiteTime struct {
	time.Time
	Valid bool
}

func (t *nullableSQLiteTime) Scan(value any) error {
	if value == nil {
		t.Time = time.Time{}
		t.Valid = false
		return nil
	}

	switch value := value.(type) {
	case time.Time:
		t.Time = value
		t.Valid = true
		return nil
	case int64:
		t.Time = time.Unix(value, 0).UTC()
		t.Valid = true
		return nil
	case string:
		return t.scanString(value)
	case []byte:
		return t.scanString(string(value))
	default:
		return fmt.Errorf("unsupported SQLite time value %T", value)
	}
}

func (t *nullableSQLiteTime) scanString(value string) error {
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02T15:04:05.999999999-07:00",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	var err error
	for _, layout := range layouts {
		var parsed time.Time
		parsed, err = time.Parse(layout, value)
		if err == nil {
			t.Time = parsed
			t.Valid = true
			return nil
		}
	}
	return fmt.Errorf("unable to parse SQLite time %q: %w", value, err)
}

func NewQuickPickMetricsRepository(db *sql.DB) model.QuickPickMetricsRepository {
	return &quickPickMetricsRepository{db: db}
}

// The metrics repository is already a shared, user-scoped SQL dependency for
// Quick Pick. Expose the optional taste provider on the concrete type so the
// service can consume persistent personalization without changing the public
// constructor or lightweight test doubles.
func (r *quickPickMetricsRepository) Rebuild(userID string, now time.Time) error {
	return newTasteAffinityRepository(r.db).Rebuild(userID, now)
}

func (r *quickPickMetricsRepository) EnsureFresh(userID string, now time.Time) error {
	return newTasteAffinityRepository(r.db).EnsureFresh(userID, now)
}

func (r *quickPickMetricsRepository) AffinityForCandidates(userID string, candidates []recommendations.TasteCandidateIdentity) (map[string]recommendations.TasteAffinity, error) {
	return newTasteAffinityRepository(r.db).AffinityForCandidates(userID, candidates)
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
		var last nullableSQLiteTime
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

func (r *quickPickMetricsRepository) ExposureMetrics(userID string, itemKeys []string) (map[string]model.QuickPickExposureMetric, error) {
	keys := uniqueQuickPickExposureKeys(itemKeys)
	result := make(map[string]model.QuickPickExposureMetric, len(keys))
	for start := 0; start < len(keys); start += quickPickExposureBatchSize {
		end := min(start+quickPickExposureBatchSize, len(keys))
		batch := keys[start:end]
		placeholders := strings.TrimRight(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, 0, len(batch)+1)
		args = append(args, userID)
		for _, key := range batch {
			args = append(args, key)
		}
		rows, err := r.db.Query(`
			select item_key, show_count, last_shown_at
			from quick_pick_exposure
			where user_id = ? and item_key in (`+placeholders+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var metric model.QuickPickExposureMetric
			var lastShown nullableSQLiteTime
			if err := rows.Scan(&metric.ItemKey, &metric.ShowCount, &lastShown); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if lastShown.Valid {
				metric.LastShownAt = lastShown.Time
			}
			result[metric.ItemKey] = metric
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (r *quickPickMetricsRepository) RecordExposures(userID string, itemKeys []string, shownAt time.Time) error {
	keys := uniqueQuickPickExposureKeys(itemKeys)
	if len(keys) == 0 {
		return nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := recordQuickPickExposureAggregates(tx, userID, keys, shownAt); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordImpressions records only the first impression for each item in a
// response view. The detail table is the idempotency boundary; aggregate
// exposure counters are updated in the same transaction so retries can never
// increment one without the other.
func (r *quickPickMetricsRepository) RecordImpressions(userID, viewID string, itemKeys []string, shownAt time.Time) error {
	userID = strings.TrimSpace(userID)
	viewID = strings.TrimSpace(viewID)
	keys := uniqueQuickPickExposureKeys(itemKeys)
	if userID == "" || viewID == "" || len(keys) == 0 {
		return nil
	}
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	newKeys := make([]string, 0, len(keys))
	for _, key := range keys {
		result, err := tx.Exec(`
			insert into quick_pick_impression (user_id, view_id, item_key, shown_at)
			values (?, ?, ?, ?)
			on conflict (user_id, view_id, item_key) do nothing`,
			userID, viewID, key, shownAt.UTC())
		if err != nil {
			return err
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if inserted > 0 {
			newKeys = append(newKeys, key)
		}
	}
	if err := recordQuickPickExposureAggregates(tx, userID, newKeys, shownAt); err != nil {
		return err
	}
	return tx.Commit()
}

// PlaylistTrackAffinities returns the strongest song affinity for each
// playlist in one bounded query. Quick Pick uses it after cheap playlist
// shortlisting so a 100-row playlist recall does not turn into 100 track-load
// queries.
func (r *quickPickMetricsRepository) PlaylistTrackAffinities(playlistIDs []string, songScores map[string]float64) (map[string]float64, error) {
	ids := uniqueQuickPickExposureKeys(playlistIDs)
	result := make(map[string]float64, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := r.db.Query(`
		select playlist_id, media_file_id
		from playlist_tracks
		where playlist_id in (`+placeholders+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var playlistID, mediaFileID string
		if err := rows.Scan(&playlistID, &mediaFileID); err != nil {
			return nil, err
		}
		result[playlistID] = math.Max(result[playlistID], songScores[mediaFileID])
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func recordQuickPickExposureAggregates(tx *sql.Tx, userID string, keys []string, shownAt time.Time) error {
	for _, key := range keys {
		if _, err := tx.Exec(`
			insert into quick_pick_exposure
				(user_id, item_key, show_count, last_shown_at)
			values (?, ?, 1, ?)
			on conflict (user_id, item_key) do update set
				show_count = quick_pick_exposure.show_count + 1,
				last_shown_at = excluded.last_shown_at`,
			userID, key, shownAt.UTC()); err != nil {
			return err
		}
	}
	return nil
}

func uniqueQuickPickExposureKeys(keys []string) []string {
	seen := make(map[string]struct{}, len(keys))
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, key)
	}
	return result
}

var _ model.QuickPickMetricsRepository = (*quickPickMetricsRepository)(nil)
