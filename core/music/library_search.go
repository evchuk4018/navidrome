package music

import (
	"context"
	"math"
	"strings"

	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/model"
)

// mergeLibrarySearch keeps a catalog recording and its library copy in one row.
// Library-only songs enter the same ranking pass as catalog results.
func (s *service) mergeLibrarySearch(ctx context.Context, query string, limit int, result model.ExternalMusicSearch) (model.ExternalMusicSearch, error) {
	for i := range result.Songs {
		result.Songs[i].Source = "catalog"
	}
	local, err := s.ds.MediaFile(ctx).Search(query, model.QueryOptions{Max: min(limit*2, 100)})
	if err != nil {
		return result, err
	}
	matched, matchErr := s.matchCatalogTracks(ctx, result.Songs)
	matchedIDs := make(map[string]bool, len(matched))
	for i, file := range matched {
		result.Songs[i].LocalMediaFileID = file.ID
		matchedIDs[file.ID] = true
	}
	for _, file := range local {
		if matchedIDs[file.ID] {
			continue
		}
		represented := false
		for i := range result.Songs {
			if result.Songs[i].Source != "catalog" || !sameLibrarySong(result.Songs[i], file) {
				continue
			}
			if result.Songs[i].LocalMediaFileID == "" {
				result.Songs[i].LocalMediaFileID = file.ID
			}
			represented = true
			break
		}
		if represented {
			continue
		}
		result.Songs = append(result.Songs, localSearchTrack(file))
	}
	return result, matchErr
}

func localSearchTrack(file model.MediaFile) model.ExternalTrack {
	version := ""
	if subtitles := file.Tags.Values(model.TagSubtitle); len(subtitles) > 0 {
		version = versionMarker(subtitles[0])
	}
	return model.ExternalTrack{
		ID:               file.ID,
		Source:           "library",
		LocalMediaFileID: file.ID,
		Title:            file.Title,
		ArtistName:       file.Artist,
		AlbumTitle:       file.Album,
		Year:             file.Year,
		Duration:         int(math.Round(float64(file.Duration))),
		TrackNumber:      file.TrackNumber,
		DiscNumber:       file.DiscNumber,
		Genre:            file.Genre,
		ISRCs:            append([]string(nil), file.Tags.Values(model.TagISRC)...),
		Version:          version,
	}
}

// matchCatalogTracks uses the existing batched matcher for candidate lookup,
// then applies a stricter text fallback for the user-visible ownership label.
func (s *service) matchCatalogTracks(ctx context.Context, tracks []model.ExternalTrack) (map[int]model.MediaFile, error) {
	result := make(map[int]model.MediaFile)
	if len(tracks) == 0 || s.matcher == nil {
		return result, nil
	}
	queries := make([]agents.Song, 0, len(tracks))
	owners := make([]int, 0, len(tracks))
	for i, track := range tracks {
		isrcs := track.ISRCs
		if len(isrcs) == 0 {
			isrcs = []string{""}
		}
		for _, isrc := range isrcs {
			query := agents.Song{
				Name:      track.Title,
				MBID:      track.ID,
				ISRC:      isrc,
				Album:     track.AlbumTitle,
				AlbumMBID: track.AlbumID,
				Duration:  uint32(max(track.Duration, 0)) * 1000,
			}
			if track.ArtistName != "" || track.ArtistID != "" {
				query.Artists = []agents.Artist{{Name: track.ArtistName, MBID: track.ArtistID}}
			}
			queries = append(queries, query)
			owners = append(owners, i)
		}
	}
	matches, err := s.matcher.MatchSongsIndexed(ctx, queries)
	if err != nil {
		return result, err
	}
	for queryIndex, owner := range owners {
		if _, found := result[owner]; found {
			continue
		}
		if file, found := matches[queryIndex]; found && sameLibrarySong(tracks[owner], file) {
			result[owner] = file
		}
	}
	return result, nil
}

func sameLibrarySong(track model.ExternalTrack, file model.MediaFile) bool {
	if file.ID == "" || file.Missing {
		return false
	}
	if songVersion(track) != songVersion(localSearchTrack(file)) {
		return false
	}
	if track.ID != "" && file.MbzRecordingID != "" && strings.EqualFold(track.ID, file.MbzRecordingID) {
		return true
	}
	for _, externalISRC := range track.ISRCs {
		for _, localISRC := range file.Tags.Values(model.TagISRC) {
			if externalISRC != "" && strings.EqualFold(externalISRC, localISRC) {
				return true
			}
		}
	}
	if normalizeText(track.Title) == "" || normalizeText(track.ArtistName) == "" ||
		normalizeText(track.Title) != normalizeText(file.Title) ||
		normalizeText(track.ArtistName) != normalizeText(file.Artist) {
		return false
	}
	return track.Duration <= 0 || file.Duration <= 0 ||
		math.Abs(float64(track.Duration)-float64(file.Duration)) <= 5
}
