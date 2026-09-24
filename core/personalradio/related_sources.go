package personalradio

import (
	"context"
	"strings"
	"time"

	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/core/recommendations"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

const (
	relatedSourceTTL        = 30 * time.Minute
	relatedSourceTimeout    = 30 * time.Second
	relatedLocalSongLimit   = 100 // Same request size as Instant Mix.
	relatedSimilarArtistMax = 15
	relatedArtistTopSongMax = 20
)

type relatedArtistSong struct {
	song     agents.Song
	affinity float64
}

// relatedLocalSongs uses the same provider and artist-based fallback as
// Instant Mix. Cache its completed selection between one-song radio refills;
// the provider itself only caches its HTTP responses for a few seconds.
func (s *service) relatedLocalSongs(ctx context.Context, seed *model.MediaFile) model.MediaFiles {
	if s.relatedSongs == nil || seed == nil || seed.ID == "" {
		return nil
	}
	load := func(string) (model.MediaFiles, time.Duration, error) {
		providerCtx, cancel := context.WithTimeout(ctx, relatedSourceTimeout)
		defer cancel()
		files, err := s.relatedSongs.SimilarSongs(providerCtx, seed.ID, relatedLocalSongLimit)
		return files, relatedSourceTTL, err
	}
	var files model.MediaFiles
	var err error
	if s.relatedLocal == nil {
		files, _, err = load(seed.ID)
	} else {
		files, err = s.relatedLocal.GetWithLoader(seed.ID, load)
	}
	if err != nil {
		log.Warn(ctx, "Related radio could not load Instant Mix songs", "seedID", seed.ID, "error", err)
		return nil
	}
	return append(model.MediaFiles(nil), files...)
}

func (s *service) invalidateRelatedLocalSongs(seedID string) {
	if s.relatedLocal != nil && seedID != "" {
		s.relatedLocal.Remove(seedID)
	}
}

// relatedArtistSongs supplies download candidates from the same seed/similar
// artists' top songs used by Instant Mix. It is only needed once fresh library
// matches are scarce. A recording MBID is required for a download.
func (s *service) relatedArtistSongs(ctx context.Context, seed *model.MediaFile) []relatedArtistSong {
	if s.artistAgents == nil || seed == nil || seed.ID == "" {
		return nil
	}
	load := func(string) ([]relatedArtistSong, time.Duration, error) {
		providerCtx, cancel := context.WithTimeout(ctx, relatedSourceTimeout)
		defer cancel()
		songs := s.fetchRelatedArtistSongs(providerCtx, seed)
		return songs, relatedSourceTTL, nil
	}
	var songs []relatedArtistSong
	if s.relatedArtists == nil {
		songs, _, _ = load(seed.ID)
	} else {
		songs, _ = s.relatedArtists.GetWithLoader(seed.ID, load)
	}
	return append([]relatedArtistSong(nil), songs...)
}

func (s *service) fetchRelatedArtistSongs(ctx context.Context, seed *model.MediaFile) []relatedArtistSong {
	mainArtist := agents.Artist{ID: seed.ArtistID, Name: seed.Artist, MBID: seed.MbzArtistID}
	if s.ds != nil && seed.ArtistID != "" {
		if artist, err := s.ds.Artist(ctx).Get(seed.ArtistID); err == nil && artist != nil {
			mainArtist.Name = artist.Name
			mainArtist.MBID = artist.MbzArtistID
		}
	}
	if mainArtist.Name == "" {
		return nil
	}
	artists := []agents.Artist{mainArtist}
	if similar, err := s.artistAgents.GetSimilarArtists(ctx, mainArtist.ID, mainArtist.Name, mainArtist.MBID, relatedSimilarArtistMax); err == nil {
		artists = append(artists, similar[:min(len(similar), relatedSimilarArtistMax)]...)
	} else {
		log.Debug(ctx, "Related radio has no similar artists", "seedID", seed.ID, "error", err)
	}
	result := make([]relatedArtistSong, 0, len(artists)*relatedArtistTopSongMax)
	seenArtists := map[string]bool{}
	seenRecordings := map[string]bool{}
	for artistIndex, artist := range artists {
		if ctx.Err() != nil {
			break
		}
		artistKey := strings.ToLower(strings.TrimSpace(artist.MBID))
		if artistKey == "" {
			artistKey = strings.ToLower(strings.TrimSpace(artist.Name))
		}
		if artistKey == "" || seenArtists[artistKey] {
			continue
		}
		seenArtists[artistKey] = true
		topSongs, err := s.artistAgents.GetArtistTopSongs(ctx, artist.ID, artist.Name, artist.MBID, relatedArtistTopSongMax)
		if err != nil {
			continue
		}
		for songIndex, song := range topSongs[:min(len(topSongs), relatedArtistTopSongMax)] {
			song.MBID = model.NormalizeRecordingMBID(song.MBID)
			if song.MBID == "" || strings.TrimSpace(song.Name) == "" || seenRecordings[song.MBID] {
				continue
			}
			seenRecordings[song.MBID] = true
			if len(song.Artists) == 0 {
				song.Artists = []agents.Artist{artist}
			} else if song.Artists[0].Name == "" {
				song.Artists[0].Name = artist.Name
			}
			affinity := max(0.35, 0.9-0.025*float64(artistIndex)-0.005*float64(songIndex))
			result = append(result, relatedArtistSong{song: song, affinity: affinity})
		}
	}
	return result
}

// Reuse only songs whose relationship to the original seed is known. Persisted
// sessions may predate the genre fix, so their old unrelated items cannot be
// assumed to be valid replay candidates.
func (s *service) relatedReplayCandidates(ctx context.Context, items, active []model.PersonalRadioItem, seed *model.MediaFile, fresh []rankedRadioCandidate) []rankedRadioCandidate {
	if seed == nil {
		return nil
	}
	allowedIDs := map[string]bool{}
	allowedMBIDs := map[string]bool{}
	for _, file := range s.relatedLocalSongs(ctx, seed) {
		allowedIDs[file.ID] = true
		if mbid := normalizeRecordingMBID(file.MbzRecordingID); mbid != "" {
			allowedMBIDs[mbid] = true
		}
	}
	for _, artistSong := range s.relatedArtistSongs(ctx, seed) {
		allowedMBIDs[normalizeRecordingMBID(artistSong.song.MBID)] = true
	}
	activeIDs, activeMBIDs := map[string]bool{seed.ID: true}, map[string]bool{}
	if mbid := normalizeRecordingMBID(seed.MbzRecordingID); mbid != "" {
		activeMBIDs[mbid] = true
	}
	for _, item := range active {
		activeIDs[item.MediaFileID] = true
		if mbid := normalizeRecordingMBID(item.RecordingMBID); mbid != "" {
			activeMBIDs[mbid] = true
		}
	}
	for _, candidate := range fresh {
		if candidate.local != nil {
			activeIDs[candidate.local.ID] = true
			if mbid := normalizeRecordingMBID(candidate.local.MbzRecordingID); mbid != "" {
				activeMBIDs[mbid] = true
			}
		}
	}
	latest := map[string]model.PersonalRadioItem{}
	for _, item := range items {
		if item.MediaFileID == "" || item.Song == nil || !isPlayableLocalFile(*item.Song) {
			continue
		}
		identity := normalizeRecordingMBID(item.RecordingMBID)
		if identity == "" {
			identity = normalizeRecordingMBID(item.Song.MbzRecordingID)
		}
		if identity == "" {
			identity = item.MediaFileID
		}
		previous, ok := latest[identity]
		if !ok || replayItemIsNewer(item, previous) {
			latest[identity] = item
		}
	}
	result := make([]rankedRadioCandidate, 0, len(latest))
	for _, item := range latest {
		file := *item.Song
		mbid := normalizeRecordingMBID(file.MbzRecordingID)
		if mbid == "" {
			mbid = normalizeRecordingMBID(item.RecordingMBID)
		}
		if activeIDs[file.ID] || (mbid != "" && activeMBIDs[mbid]) {
			continue
		}
		if !allowedIDs[file.ID] && !allowedMBIDs[mbid] && relatedLocalTier(seed, file) == 0 {
			continue
		}
		key := radioMediaFileCandidateKey(file)
		candidate := recommendations.Candidate{Key: key, MediaFile: file, SeedAffinity: localSeedAffinity(seed, file)}
		result = append(result, rankedRadioCandidate{
			candidate:          candidate,
			ranked:             recommendations.RankedCandidate{Candidate: candidate},
			local:              &file,
			source:             relatedReplay,
			replayPosition:     item.Position,
			replayEarlySkipped: item.PlaybackOutcome == model.RadioPlaybackEarlySkip,
		})
		if item.LastFeedbackAt != nil {
			result[len(result)-1].replayTime = *item.LastFeedbackAt
		}
	}
	return result
}

func replayItemIsNewer(item, previous model.PersonalRadioItem) bool {
	if item.LastFeedbackAt != nil && previous.LastFeedbackAt != nil && !item.LastFeedbackAt.Equal(*previous.LastFeedbackAt) {
		return item.LastFeedbackAt.After(*previous.LastFeedbackAt)
	}
	if item.LastFeedbackAt != nil && previous.LastFeedbackAt == nil {
		return true
	}
	if item.LastFeedbackAt == nil && previous.LastFeedbackAt != nil {
		return false
	}
	return item.Position > previous.Position
}
