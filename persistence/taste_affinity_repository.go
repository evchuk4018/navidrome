package persistence

import (
	"database/sql"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/navidrome/navidrome/core/recommendations"
	"github.com/navidrome/navidrome/model"
)

const (
	tasteHalfLife        = 120 * 24 * time.Hour
	tasteRefreshInterval = 5 * time.Minute
)

var tasteRebuildMu sync.Mutex

type tasteAffinityRepository struct {
	db *sql.DB
}

func newTasteAffinityRepository(db *sql.DB) recommendations.TasteAffinityRepository {
	return &tasteAffinityRepository{db: db}
}

func (r *tasteAffinityRepository) EnsureFresh(userID string, now time.Time) error {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	tasteRebuildMu.Lock()
	defer tasteRebuildMu.Unlock()
	var updated nullableSQLiteTime
	err := r.db.QueryRow(`select max(updated_at) from recommendation_taste_affinity where user_id = ?`, userID).Scan(&updated)
	if err != nil {
		return err
	}
	if updated.Valid && now.Sub(updated.Time.UTC()) < tasteRefreshInterval {
		return nil
	}
	return r.rebuild(userID, now.UTC())
}

func (r *tasteAffinityRepository) Rebuild(userID string, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	tasteRebuildMu.Lock()
	defer tasteRebuildMu.Unlock()
	return r.rebuild(strings.TrimSpace(userID), now.UTC())
}

func (r *tasteAffinityRepository) rebuild(userID string, now time.Time) error {
	if userID == "" {
		return nil
	}
	accumulators, err := r.buildAccumulators(userID, now)
	if err != nil {
		return err
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`delete from recommendation_taste_affinity where user_id = ?`, userID); err != nil {
		return err
	}
	statement := `insert into recommendation_taste_affinity
		(user_id, entity_type, entity_key, score, positive_weight, negative_weight, last_evidence_at, updated_at)
		values (?, ?, ?, ?, ?, ?, ?, ?)`
	keys := make([]string, 0, len(accumulators))
	for key := range accumulators {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		entry := accumulators[key]
		if _, err := tx.Exec(statement, userID, entry.entityType, entry.entityKey,
			entry.score(), entry.positive, entry.negative, nullableTime(entry.lastEvidence), now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *tasteAffinityRepository) AffinityForCandidates(userID string, candidates []recommendations.TasteCandidateIdentity) (map[string]recommendations.TasteAffinity, error) {
	result := make(map[string]recommendations.TasteAffinity, len(candidates))
	if len(candidates) == 0 {
		return result, nil
	}
	now := time.Now().UTC()
	if err := r.EnsureFresh(userID, now); err != nil {
		return nil, err
	}

	keysByType := map[string]map[string]struct{}{
		"track":  {},
		"artist": {},
		"genre":  {},
		"album":  {},
	}
	for _, candidate := range candidates {
		if candidate.TrackKey != "" {
			keysByType["track"][candidate.TrackKey] = struct{}{}
		}
		for _, key := range candidate.ArtistKeys {
			if key != "" {
				keysByType["artist"][key] = struct{}{}
			}
		}
		for _, key := range candidate.GenreKeys {
			if key != "" {
				keysByType["genre"][key] = struct{}{}
			}
		}
		if candidate.AlbumKey != "" {
			keysByType["album"][candidate.AlbumKey] = struct{}{}
		}
	}

	values := map[string]float64{}
	for _, entityType := range []string{"track", "artist", "genre", "album"} {
		keys := sortedKeys(keysByType[entityType])
		if len(keys) == 0 {
			continue
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")
		args := make([]any, 0, len(keys)+2)
		args = append(args, userID, entityType)
		for _, key := range keys {
			args = append(args, key)
		}
		rows, err := r.db.Query(fmt.Sprintf(`select entity_key, score
			from recommendation_taste_affinity
			where user_id = ? and entity_type = ? and entity_key in (%s)`, placeholders), args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var key string
			var score float64
			if err := rows.Scan(&key, &score); err != nil {
				_ = rows.Close()
				return nil, err
			}
			values[entityType+"\x00"+key] = clampTaste(score)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}

	for _, candidate := range candidates {
		artist := maxTaste(values, "artist", candidate.ArtistKeys)
		genre := maxTaste(values, "genre", candidate.GenreKeys)
		track := clampTaste(values["track\x00"+candidate.TrackKey])
		album := clampTaste(values["album\x00"+candidate.AlbumKey])
		result[candidate.Key] = recommendations.ComposeTasteAffinity(track, artist, genre, album)
	}
	return result, nil
}

type tasteEvidence struct {
	entityType   string
	entityKey    string
	positive     float64
	negative     float64
	lastEvidence time.Time
}

func (e *tasteEvidence) score() float64 {
	if e.positive <= 0 {
		return 0
	}
	return clampTaste(e.positive / (e.positive + e.negative + 2))
}

type tasteMedia struct {
	file      model.MediaFile
	identity  recommendations.TasteCandidateIdentity
	playCount int64
	playDate  time.Time
	starred   bool
	starredAt time.Time
}

func (r *tasteAffinityRepository) buildAccumulators(userID string, now time.Time) (map[string]*tasteEvidence, error) {
	media, byMBID, err := r.loadTasteMedia(userID)
	if err != nil {
		return nil, err
	}
	accumulators := map[string]*tasteEvidence{}
	for _, item := range media {
		when := item.playDate
		if when.IsZero() {
			when = now
		}
		if item.playCount > 0 {
			addMediaEvidence(accumulators, item.identity, float64(minTasteInt64(item.playCount, 100))*0.35, 0, when, now)
		}
		if item.starred {
			starredAt := item.starredAt
			if starredAt.IsZero() {
				starredAt = now
			}
			addMediaEvidence(accumulators, item.identity, 3, 0, starredAt, now)
		}
	}

	rows, err := r.db.Query(`select media_file_id, submission_time from scrobbles
		where user_id = ? order by submission_time asc, id asc`, userID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var mediaFileID string
		var submissionTime int64
		if err := rows.Scan(&mediaFileID, &submissionTime); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if item, ok := media[mediaFileID]; ok {
			when := time.Unix(submissionTime, 0).UTC()
			addMediaEvidence(accumulators, item.identity, 1, 0, when, now)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()

	feedbackRows, err := r.db.Query(`select recording_mbid, positive_count, completed_count,
		neutral_skip_count, early_skip_count, last_early_skip_at, updated_at
		from radio_track_feedback where user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	for feedbackRows.Next() {
		var mbid string
		var positive, completed, neutral, early int
		var lastEarly nullableSQLiteTime
		var updated nullableSQLiteTime
		if err := feedbackRows.Scan(&mbid, &positive, &completed, &neutral, &early, &lastEarly, &updated); err != nil {
			_ = feedbackRows.Close()
			return nil, err
		}
		when := updated.Time
		if !updated.Valid {
			when = now
		}
		positiveWeight := float64(positive) + 1.5*float64(completed)
		negativeWeight := 1.5*float64(early) + 0.25*float64(neutral)
		trackKey := model.RadioTrackKey(mbid, "")
		addEvidence(accumulators, "track", trackKey, positiveWeight, negativeWeight, when, now)
		for _, item := range byMBID[model.NormalizeRecordingMBID(mbid)] {
			addMediaEvidence(accumulators, item.identity, positiveWeight, negativeWeight, when, now)
		}
		if lastEarly.Valid && lastEarly.Time.After(when) {
			entry := accumulators["track\x00"+trackKey]
			if entry != nil && lastEarly.Time.After(entry.lastEvidence) {
				entry.lastEvidence = lastEarly.Time
			}
		}
	}
	if err := feedbackRows.Err(); err != nil {
		_ = feedbackRows.Close()
		return nil, err
	}
	_ = feedbackRows.Close()
	return accumulators, nil
}

func (r *tasteAffinityRepository) loadTasteMedia(userID string) (map[string]*tasteMedia, map[string][]*tasteMedia, error) {
	rows, err := r.db.Query(`select mf.id, coalesce(mf.mbz_recording_id, ''),
		coalesce(mf.artist_id, ''), coalesce(mf.artist, ''), coalesce(mf.album_id, ''),
		coalesce(mf.album, ''), coalesce(mf.genre, ''), coalesce(a.play_count, 0),
		a.play_date, coalesce(a.starred, 0), a.starred_at
		from media_file mf left join annotation a on a.user_id = ?
		and a.item_id = mf.id and a.item_type = 'media_file'`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	media := map[string]*tasteMedia{}
	byMBID := map[string][]*tasteMedia{}
	for rows.Next() {
		item := &tasteMedia{}
		var id, mbid, artistID, artist, albumID, album, genre string
		var playDate, starredAt nullableSQLiteTime
		if err := rows.Scan(&id, &mbid, &artistID, &artist, &albumID, &album, &genre,
			&item.playCount, &playDate, &item.starred, &starredAt); err != nil {
			return nil, nil, err
		}
		item.playDate = playDate.Time
		if !playDate.Valid {
			item.playDate = time.Time{}
		}
		item.starredAt = starredAt.Time
		if !starredAt.Valid {
			item.starredAt = time.Time{}
		}
		item.file = model.MediaFile{ID: id, MbzRecordingID: mbid, ArtistID: artistID, Artist: artist, AlbumID: albumID, Album: album, Genre: genre}
		item.identity = recommendations.TasteIdentityForMediaFile("", item.file)
		media[id] = item
		if normalized := model.NormalizeRecordingMBID(mbid); normalized != "" {
			byMBID[normalized] = append(byMBID[normalized], item)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return media, byMBID, nil
}

func addMediaEvidence(accumulators map[string]*tasteEvidence, identity recommendations.TasteCandidateIdentity, positive, negative float64, when, now time.Time) {
	addEvidence(accumulators, "track", identity.TrackKey, positive, negative, when, now)
	for _, key := range identity.ArtistKeys {
		addEvidence(accumulators, "artist", key, positive, negative, when, now)
	}
	for _, key := range identity.GenreKeys {
		addEvidence(accumulators, "genre", key, positive, negative, when, now)
	}
	addEvidence(accumulators, "album", identity.AlbumKey, positive, negative, when, now)
}

func addEvidence(accumulators map[string]*tasteEvidence, entityType, entityKey string, positive, negative float64, when, now time.Time) {
	entityType = strings.TrimSpace(entityType)
	entityKey = strings.TrimSpace(entityKey)
	if entityType == "" || entityKey == "" || positive <= 0 && negative <= 0 {
		return
	}
	if when.IsZero() {
		when = now
	}
	age := now.Sub(when.UTC())
	if age < 0 {
		age = 0
	}
	decay := math.Exp(-float64(age) / float64(tasteHalfLife))
	key := entityType + "\x00" + entityKey
	entry := accumulators[key]
	if entry == nil {
		entry = &tasteEvidence{entityType: entityType, entityKey: entityKey}
		accumulators[key] = entry
	}
	entry.positive += maxFloat(positive, 0) * decay
	entry.negative += maxFloat(negative, 0) * decay
	if when.After(entry.lastEvidence) {
		entry.lastEvidence = when.UTC()
	}
}

func maxTaste(values map[string]float64, entityType string, keys []string) float64 {
	var result float64
	for _, key := range keys {
		result = max(result, values[entityType+"\x00"+key])
	}
	return clampTaste(result)
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func clampTaste(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return min(1, max(0, value))
}

func maxFloat(value, lower float64) float64 {
	if value < lower || math.IsNaN(value) || math.IsInf(value, 0) {
		return lower
	}
	return value
}

func minTasteInt64(value, upper int64) int64 {
	if value < upper {
		return value
	}
	return upper
}

var _ recommendations.TasteAffinityRepository = (*tasteAffinityRepository)(nil)
