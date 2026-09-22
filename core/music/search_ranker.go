package music

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/navidrome/navidrome/core/recommendations"
	"github.com/navidrome/navidrome/model"
)

const (
	kindArtist = "artist"
	kindAlbum  = "album"
	kindSong   = "song"
	kindGenre  = "genre"
)

type searchCandidate struct {
	hit        model.ExternalSearchHit
	kind       string
	id         string
	primary    string
	context    string
	secondary  string
	provider   float64
	popularity float64
	affinity   float64
	tier       int
	score      float64
}

// rankExternalSearch performs all cross-entity ordering on the server. Match
// tier is the primary sort key, so popularity can never displace a better text
// match.
func rankExternalSearch(query string, result model.ExternalMusicSearch, limit int, affinities map[string]recommendations.TasteAffinity) model.ExternalMusicSearch {
	if limit < 1 {
		limit = 30
	}
	if limit > 50 {
		limit = 50
	}
	candidates := normalizeCandidates(result)
	candidates = deduplicateCandidates(candidates)
	normalizePopularity(candidates)
	for i := range candidates {
		candidate := &candidates[i]
		candidate.tier = matchTier(query, *candidate)
		candidate.affinity = affinities[candidateKey(*candidate)].Score
		coverage := tokenCoverage(query, candidate.primary+" "+candidate.context+" "+candidate.secondary)
		intent := intentFit(query, candidate.kind)
		quality := metadataQuality(candidate.hit)
		candidate.score = .40*clamp01(candidate.provider) + .35*clamp01(candidate.popularity) +
			.10*coverage + .07*clamp01(candidate.affinity) + .05*intent + .03*quality
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.tier != right.tier {
			return left.tier < right.tier
		}
		if left.score != right.score {
			return left.score > right.score
		}
		if left.kind != right.kind {
			return left.kind < right.kind
		}
		return left.id < right.id
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	result.Results = make([]model.ExternalSearchHit, 0, len(candidates))
	for _, candidate := range candidates {
		result.Results = append(result.Results, candidate.hit)
	}
	result.DeriveCompatibilityArrays()
	return result
}

func searchTasteCandidates(result model.ExternalMusicSearch) []recommendations.TasteCandidateIdentity {
	candidates := deduplicateCandidates(normalizeCandidates(result))
	identities := make([]recommendations.TasteCandidateIdentity, 0, len(candidates))
	for _, candidate := range candidates {
		identity := recommendations.TasteCandidateIdentity{Key: candidateKey(candidate)}
		switch {
		case candidate.hit.Song != nil:
			identity.TrackKey = candidate.hit.Song.ID
			identity.ArtistKeys = []string{candidate.hit.Song.ArtistID, candidate.hit.Song.ArtistName}
			identity.AlbumKey = candidate.hit.Song.AlbumID
			if candidate.hit.Song.Genre != "" {
				identity.GenreKeys = []string{candidate.hit.Song.Genre}
			}
		case candidate.hit.Artist != nil:
			identity.ArtistKeys = []string{candidate.hit.Artist.ID, candidate.hit.Artist.Name}
		case candidate.hit.Album != nil:
			identity.AlbumKey = candidate.hit.Album.ID
			identity.ArtistKeys = []string{candidate.hit.Album.ArtistID, candidate.hit.Album.ArtistName}
		case candidate.hit.Genre != nil:
			identity.GenreKeys = []string{candidate.hit.Genre.Name}
		}
		identities = append(identities, identity)
	}
	return identities
}

func normalizeCandidates(result model.ExternalMusicSearch) []searchCandidate {
	all := make([]searchCandidate, 0, len(result.Artists)+len(result.Albums)+len(result.Songs)+len(result.Genres))
	for i := range result.Artists {
		entity := result.Artists[i]
		all = append(all, searchCandidate{hit: model.ExternalSearchHit{Kind: kindArtist, Artist: &entity}, kind: kindArtist, id: entity.ID,
			primary: entity.Name, secondary: strings.Join(append([]string{entity.Disambiguation}, entity.Aliases...), " "), provider: entity.ProviderScore, popularity: entity.Popularity})
	}
	for i := range result.Albums {
		entity := result.Albums[i]
		all = append(all, searchCandidate{hit: model.ExternalSearchHit{Kind: kindAlbum, Album: &entity}, kind: kindAlbum, id: entity.ID,
			primary: entity.Title, context: entity.ArtistName, secondary: entity.Type, provider: entity.ProviderScore, popularity: entity.Popularity})
	}
	for i := range result.Songs {
		entity := result.Songs[i]
		all = append(all, searchCandidate{hit: model.ExternalSearchHit{Kind: kindSong, Song: &entity}, kind: kindSong, id: entity.ID,
			primary: entity.Title, context: entity.ArtistName + " " + entity.AlbumTitle, secondary: entity.Genre, provider: entity.ProviderScore, popularity: entity.Popularity})
	}
	for i := range result.Genres {
		entity := result.Genres[i]
		all = append(all, searchCandidate{hit: model.ExternalSearchHit{Kind: kindGenre, Genre: &entity}, kind: kindGenre, id: normalizeText(entity.Name), primary: entity.Name, provider: entity.ProviderScore})
	}
	return all
}

func deduplicateCandidates(input []searchCandidate) []searchCandidate {
	output := make([]searchCandidate, 0, len(input))
	byKey := map[string]int{}
	for _, candidate := range input {
		key := dedupeKey(candidate)
		if candidate.kind == kindSong {
			if equivalent := equivalentSongIndex(output, candidate); equivalent >= 0 {
				mergeCandidate(&output[equivalent], candidate)
				continue
			}
		}
		if index, exists := byKey[key]; exists {
			mergeCandidate(&output[index], candidate)
			continue
		}
		byKey[key] = len(output)
		output = append(output, candidate)
	}
	return output
}

func dedupeKey(candidate searchCandidate) string {
	if candidate.kind != kindSong {
		return candidate.kind + ":" + candidate.id
	}
	song := candidate.hit.Song
	artistTitle := normalizeText(song.ArtistName) + "\x00" + normalizeText(song.Title)
	if len(song.ISRCs) > 0 {
		return "song:isrc:" + strings.ToUpper(song.ISRCs[0]) + "\x00" + artistTitle
	}
	return "song:" + artistTitle + "\x00" + songVersion(*song) + "\x00" + strconv.Itoa(song.Duration/2)
}

func equivalentSongIndex(existing []searchCandidate, candidate searchCandidate) int {
	right := candidate.hit.Song
	if right == nil {
		return -1
	}
	for index := range existing {
		left := existing[index].hit.Song
		if left == nil || songVersion(*left) != songVersion(*right) {
			continue
		}
		if normalizeText(left.ArtistName) != normalizeText(right.ArtistName) || normalizeText(left.Title) != normalizeText(right.Title) {
			continue
		}
		if sharedISRC(left.ISRCs, right.ISRCs) || (len(left.ISRCs) == 0 && len(right.ISRCs) == 0 && abs(left.Duration-right.Duration) <= 2) {
			return index
		}
	}
	return -1
}

func mergeCandidate(destination *searchCandidate, source searchCandidate) {
	if source.popularity > destination.popularity {
		destination.popularity = source.popularity
	}
	if source.provider > destination.provider {
		destination.provider = source.provider
	}
	// Prefer canonical non-video recordings, then the earliest release.
	if destination.hit.Song != nil && source.hit.Song != nil {
		left, right := destination.hit.Song, source.hit.Song
		if (left.Video && !right.Video) || (!right.Video && earlierDate(right.ReleaseDate, left.ReleaseDate)) {
			right.Popularity = destination.popularity
			destination.hit.Song = right
			destination.id = right.ID
		}
	}
}

func matchTier(query string, candidate searchCandidate) int {
	q, primary := normalizeText(query), normalizeText(candidate.primary)
	contextText := normalizeText(candidate.context)
	if q == "" {
		return 6
	}
	if strings.EqualFold(strings.TrimSpace(query), candidate.id) {
		return 1
	}
	if candidate.kind == kindSong && candidate.hit.Song != nil {
		artist := normalizeText(candidate.hit.Song.ArtistName)
		if q == strings.TrimSpace(artist+" "+primary) || q == strings.TrimSpace(primary+" "+artist) {
			return 1
		}
	}
	if q == primary {
		return 2
	}
	if strings.HasPrefix(primary, q) || strings.Contains(primary, q) {
		return 3
	}
	if tokenCoverage(q, primary+" "+contextText) == 1 {
		return 4
	}
	if boundedDistance(q, primary) {
		return 5
	}
	return 6
}

func normalizePopularity(candidates []searchCandidate) {
	byKind := map[string][]float64{}
	for _, candidate := range candidates {
		if candidate.popularity > 0 {
			byKind[candidate.kind] = append(byKind[candidate.kind], math.Log1p(candidate.popularity))
		}
	}
	for kind := range byKind {
		sort.Float64s(byKind[kind])
	}
	for i := range candidates {
		values := byKind[candidates[i].kind]
		if len(values) == 0 || candidates[i].popularity <= 0 {
			candidates[i].popularity = 0
			continue
		}
		value := math.Log1p(candidates[i].popularity)
		position := sort.SearchFloat64s(values, value)
		candidates[i].popularity = float64(position+1) / float64(len(values))
	}
}

func tokenCoverage(query, value string) float64 {
	tokens := strings.Fields(normalizeText(query))
	if len(tokens) == 0 {
		return 0
	}
	haystack := " " + normalizeText(value) + " "
	matched := 0
	for _, token := range tokens {
		if strings.Contains(haystack, " "+token+" ") {
			matched++
		}
	}
	return float64(matched) / float64(len(tokens))
}

func intentFit(query, kind string) float64 {
	q := normalizeText(query)
	if strings.Contains(q, kind) || (kind == kindSong && strings.Contains(q, "track")) {
		return 1
	}
	return .5
}

func metadataQuality(hit model.ExternalSearchHit) float64 {
	fields, present := 2.0, 0.0
	switch {
	case hit.Artist != nil:
		if hit.Artist.Name != "" {
			present++
		}
		if hit.Artist.Country != "" || hit.Artist.Type != "" {
			present++
		}
	case hit.Album != nil:
		fields = 4
		if hit.Album.Title != "" {
			present++
		}
		if hit.Album.ArtistName != "" {
			present++
		}
		if hit.Album.ReleaseDate != "" {
			present++
		}
		if len(hit.Album.ArtworkURLs) > 0 {
			present++
		}
	case hit.Song != nil:
		fields = 5
		if hit.Song.Title != "" {
			present++
		}
		if hit.Song.ArtistName != "" {
			present++
		}
		if hit.Song.AlbumTitle != "" {
			present++
		}
		if hit.Song.Duration > 0 {
			present++
		}
		if len(hit.Song.ArtworkURLs) > 0 {
			present++
		}
	case hit.Genre != nil:
		fields = 1
		if hit.Genre.Name != "" {
			present++
		}
	}
	return present / fields
}

func normalizeText(value string) string {
	var builder strings.Builder
	space := false
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			if space && builder.Len() > 0 {
				builder.WriteByte(' ')
			}
			builder.WriteRune(r)
			space = false
		} else {
			space = true
		}
	}
	return builder.String()
}

func boundedDistance(left, right string) bool {
	if left == "" || right == "" || abs(len([]rune(left))-len([]rune(right))) > 2 {
		return false
	}
	a, b := []rune(left), []rune(right)
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		rowMin := current[0]
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			current[j] = min(current[j-1]+1, previous[j]+1, previous[j-1]+cost)
			if current[j] < rowMin {
				rowMin = current[j]
			}
		}
		if rowMin > 2 {
			return false
		}
		previous = current
	}
	return previous[len(b)] <= 2
}

func songVersion(song model.ExternalTrack) string {
	if song.Version != "" {
		return normalizeText(song.Version)
	}
	text := normalizeText(song.Title)
	for _, marker := range []string{"live", "remix", "acoustic", "instrumental", "radio edit", "karaoke", "video"} {
		if strings.Contains(text, marker) {
			return marker
		}
	}
	return "original"
}

func candidateKey(candidate searchCandidate) string { return candidate.kind + ":" + candidate.id }
func sharedISRC(left, right []string) bool {
	for _, a := range left {
		for _, b := range right {
			if strings.EqualFold(a, b) {
				return true
			}
		}
	}
	return false
}
func earlierDate(left, right string) bool { return left != "" && (right == "" || left < right) }
func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
