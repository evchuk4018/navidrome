package agents

import (
	"context"
	"errors"
	"strings"

	"github.com/gohugoio/hashstructure"
	"github.com/navidrome/navidrome/model"
)

type Constructor func(ds model.DataStore) Interface

type Interface interface {
	AgentName() string
}

// AlbumInfo contains album metadata (no images)
type AlbumInfo struct {
	Name        string
	MBID        string
	Description string
	URL         string
}

type Artist struct {
	ID   string
	Name string
	MBID string
}

type ExternalImage struct {
	URL  string
	Size int
}

// SimilarityScore contains a provider's raw similarity score and its normalized
// value, which is suitable for comparing results from different providers.
type SimilarityScore struct {
	Provider string
	// Score is the raw score returned by Provider.
	Score float64
	// NormalizedScore is the provider score mapped to a [0,1] range.
	NormalizedScore float64
}

type Song struct {
	ID        string
	Name      string
	MBID      string
	ISRC      string
	Artists   []Artist
	Album     string
	AlbumMBID string
	Duration  uint32 // Duration in milliseconds, 0 means unknown
	// CandidateID is the stable identity used to deduplicate recommendation candidates.
	CandidateID string
	// SimilarityScores contains recommendation metadata and is ignored by Equals.
	SimilarityScores []SimilarityScore
}

// Equals reports strict equality of song metadata, used to dedup identical input songs.
// Recommendation metadata is ignored so matcher semantics remain intact. It hashes rather
// than comparing with ==, which the Artists slice makes illegal.
func (s Song) Equals(other Song) bool {
	s.CandidateID = ""
	s.SimilarityScores = nil
	other.CandidateID = ""
	other.SimilarityScores = nil
	h1, _ := hashstructure.Hash(s, nil)
	h2, _ := hashstructure.Hash(other, nil)
	return h1 == h2
}

// CandidateID returns a stable identity for a recommendation candidate. A normalized
// recording MBID is preferred; when it is unavailable, normalized title and first-artist
// text provide the fallback identity.
func CandidateID(song Song) string {
	if mbid := normalizeCandidateText(song.MBID); mbid != "" {
		return "mbid:" + mbid
	}

	artist := ""
	if len(song.Artists) > 0 {
		artist = song.Artists[0].Name
	}
	return "title:" + normalizeCandidateText(song.Name) + "|artist:" + normalizeCandidateText(artist)
}

func normalizeCandidateText(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

var (
	ErrNotFound = errors.New("not found")
)

// AlbumInfoRetriever provides album info (no images)
type AlbumInfoRetriever interface {
	GetAlbumInfo(ctx context.Context, name, artist, mbid string) (*AlbumInfo, error)
}

// AlbumImageRetriever provides album images
type AlbumImageRetriever interface {
	GetAlbumImages(ctx context.Context, name, artist, mbid string) ([]ExternalImage, error)
}

type ArtistMBIDRetriever interface {
	GetArtistMBID(ctx context.Context, id string, name string) (string, error)
}

type ArtistURLRetriever interface {
	GetArtistURL(ctx context.Context, id, name, mbid string) (string, error)
}

type ArtistBiographyRetriever interface {
	GetArtistBiography(ctx context.Context, id, name, mbid string) (string, error)
}

type ArtistSimilarRetriever interface {
	GetSimilarArtists(ctx context.Context, id, name, mbid string, limit int) ([]Artist, error)
}

type ArtistImageRetriever interface {
	GetArtistImages(ctx context.Context, id, name, mbid string) ([]ExternalImage, error)
}

type ArtistTopSongsRetriever interface {
	GetArtistTopSongs(ctx context.Context, id, artistName, mbid string, count int) ([]Song, error)
}

// SimilarSongsByTrackRetriever provides similar songs based on a specific track
type SimilarSongsByTrackRetriever interface {
	// GetSimilarSongsByTrack returns songs similar to the given track.
	// Parameters:
	//   - id: local mediafile ID
	//   - name: track title
	//   - artist: artist name
	//   - mbid: MusicBrainz recording ID (may be empty)
	//   - count: maximum number of results
	GetSimilarSongsByTrack(ctx context.Context, id, name, artist, mbid string, count int) ([]Song, error)
}

// SimilarSongsByAlbumRetriever provides similar songs based on an album
type SimilarSongsByAlbumRetriever interface {
	// GetSimilarSongsByAlbum returns songs similar to tracks on the given album.
	// Parameters:
	//   - id: local album ID
	//   - name: album name
	//   - artist: album artist name
	//   - mbid: MusicBrainz release ID (may be empty)
	//   - count: maximum number of results
	GetSimilarSongsByAlbum(ctx context.Context, id, name, artist, mbid string, count int) ([]Song, error)
}

// SimilarSongsByArtistRetriever provides similar songs based on an artist
type SimilarSongsByArtistRetriever interface {
	// GetSimilarSongsByArtist returns songs similar to the artist's catalog.
	// Parameters:
	//   - id: local artist ID
	//   - name: artist name
	//   - mbid: MusicBrainz artist ID (may be empty)
	//   - count: maximum number of results
	GetSimilarSongsByArtist(ctx context.Context, id, name, mbid string, count int) ([]Song, error)
}

var Map map[string]Constructor

func Register(name string, init Constructor) {
	if Map == nil {
		Map = make(map[string]Constructor)
	}
	Map[name] = init
}
