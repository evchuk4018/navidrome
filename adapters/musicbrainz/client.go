package musicbrainz

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/navidrome/navidrome/adapters/listenbrainz"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

const (
	defaultBaseURL       = "https://musicbrainz.org/ws/2"
	cacheTTL             = 10 * time.Minute
	maxResponse          = 8 << 20
	recordingSearchLimit = 100
)

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type popularityProvider interface {
	GetRecordingPopularity(context.Context, []string) (map[string]model.RecordingPopularity, error)
}

type Client struct {
	baseURL    string
	http       httpDoer
	popularity popularityProvider

	rateMu      sync.Mutex
	lastRequest time.Time

	cacheMu sync.RWMutex
	cache   map[string]cacheEntry
}

type cacheEntry struct {
	body      []byte
	expiresAt time.Time
}

func New() *Client {
	return newWithPopularity(defaultBaseURL, http.DefaultClient, listenbrainz.NewPopularityClient())
}

func NewWithClient(baseURL string, client httpDoer) *Client {
	return newWithPopularity(baseURL, client, nil)
}

func newWithPopularity(baseURL string, client httpDoer, popularity popularityProvider) *Client {
	if strings.TrimSpace(baseURL) == "" {
		baseURL = defaultBaseURL
	}
	if client == nil {
		client = http.DefaultClient
	}
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		http:       client,
		popularity: popularity,
		cache:      make(map[string]cacheEntry),
	}
}

func (c *Client) Search(ctx context.Context, query string) (model.ExternalMusicSearch, error) {
	var artists mbArtistSearchResponse
	if err := c.get(ctx, "/artist", queryValues(query), &artists); err != nil {
		return model.ExternalMusicSearch{}, fmt.Errorf("search artists: %w", err)
	}

	var groups mbReleaseGroupSearchResponse
	if err := c.get(ctx, "/release-group", queryValues(query), &groups); err != nil {
		return model.ExternalMusicSearch{}, fmt.Errorf("search albums: %w", err)
	}

	var recordings mbRecordingSearchResponse
	if err := c.get(ctx, "/recording", recordingQueryValues(query), &recordings); err != nil {
		return model.ExternalMusicSearch{}, fmt.Errorf("search songs: %w", err)
	}
	popularity := map[string]model.RecordingPopularity(nil)
	if c.popularity != nil && len(recordings.Recordings) > 0 {
		var err error
		popularity, err = c.popularity.GetRecordingPopularity(ctx, recordingIDs(recordings.Recordings))
		if err != nil {
			log.Warn(ctx, "ListenBrainz popularity lookup failed; using MusicBrainz ordering", err)
			popularity = nil
		}
	}
	sortRecordingsByRelevance(recordings.Recordings, query, popularity)
	var genres mbTagSearchResponse
	if err := c.get(ctx, "/tag", queryValues(query), &genres); err != nil {
		return model.ExternalMusicSearch{}, fmt.Errorf("search genres: %w", err)
	}

	result := model.ExternalMusicSearch{
		Artists: make([]model.ExternalArtist, 0, len(artists.Artists)),
		Albums:  make([]model.ExternalAlbum, 0, len(groups.ReleaseGroups)),
		Songs:   make([]model.ExternalTrack, 0, len(recordings.Recordings)),
		Genres:  make([]model.ExternalGenre, 0, len(genres.Tags)),
	}
	tags := make(map[string]struct{})
	for _, artist := range artists.Artists {
		result.Artists = append(result.Artists, externalArtist(artist))
		collectTags(tags, artist.Tags, query)
	}
	for _, group := range groups.ReleaseGroups {
		result.Albums = append(result.Albums, externalAlbum(group))
		collectTags(tags, group.Tags, query)
	}
	for _, recording := range recordings.Recordings {
		result.Songs = append(result.Songs, externalTrack(recording))
		collectTags(tags, recording.Tags, query)
	}
	for _, genre := range genres.Tags {
		collectTags(tags, []mbTag{genre}, query)
	}
	for tag := range tags {
		result.Genres = append(result.Genres, model.ExternalGenre{Name: tag})
	}
	sort.Slice(result.Genres, func(i, j int) bool { return result.Genres[i].Name < result.Genres[j].Name })
	return result, nil
}

func (c *Client) Artist(ctx context.Context, artistID string) (model.ExternalArtistDetails, error) {
	if err := validateID(artistID); err != nil {
		return model.ExternalArtistDetails{}, err
	}

	var artist mbArtist
	if err := c.get(ctx, "/artist/"+artistID, values("inc", "tags"), &artist); err != nil {
		return model.ExternalArtistDetails{}, fmt.Errorf("get artist: %w", err)
	}
	params := url.Values{}
	params.Set("artist", artistID)
	params.Set("fmt", "json")
	params.Set("limit", "100")
	params.Set("inc", "artist-credits tags")
	var groups mbReleaseGroupBrowseResponse
	if err := c.get(ctx, "/release-group", params, &groups); err != nil {
		return model.ExternalArtistDetails{}, fmt.Errorf("get artist albums: %w", err)
	}

	albums := make([]model.ExternalAlbum, 0, len(groups.ReleaseGroups))
	for _, group := range groups.ReleaseGroups {
		albums = append(albums, externalAlbum(group))
	}
	sort.SliceStable(albums, func(i, j int) bool {
		if albums[i].Year == albums[j].Year {
			return albums[i].Title < albums[j].Title
		}
		return albums[i].Year < albums[j].Year
	})
	return model.ExternalArtistDetails{
		Artist: externalArtist(artist),
		Albums: albums,
	}, nil
}

func (c *Client) Album(ctx context.Context, albumID string) (model.ExternalAlbumDetails, error) {
	if err := validateID(albumID); err != nil {
		return model.ExternalAlbumDetails{}, err
	}

	var group mbReleaseGroup
	if err := c.get(ctx, "/release-group/"+albumID, values("inc", "releases artist-credits"), &group); err != nil {
		return model.ExternalAlbumDetails{}, fmt.Errorf("get album: %w", err)
	}
	if len(group.Releases) == 0 {
		return model.ExternalAlbumDetails{
			Album:  externalAlbum(group),
			Tracks: []model.ExternalTrack{},
		}, nil
	}

	releaseID := group.Releases[0].ID
	var release mbRelease
	if err := c.get(ctx, "/release/"+releaseID, values("inc", "recordings artist-credits"), &release); err != nil {
		return model.ExternalAlbumDetails{}, fmt.Errorf("get album tracks: %w", err)
	}

	album := externalAlbum(group)
	if album.ArtistName == "" {
		album.ArtistName = artistCreditName(release.ArtistCredit)
	}
	tracks := make([]model.ExternalTrack, 0)
	for _, medium := range release.Media {
		for _, track := range medium.Tracks {
			external := externalTrack(track.Recording)
			external.AlbumID = group.ID
			external.AlbumTitle = group.Title
			external.ArtistName = firstNonEmpty(external.ArtistName, album.ArtistName)
			external.ImageURL = coverArtURL(group.ID)
			external.ReleaseDate = firstNonEmpty(group.FirstReleaseDate, release.Date)
			external.Year = yearFromDate(external.ReleaseDate)
			external.TrackNumber = track.Position
			external.DiscNumber = medium.Position
			tracks = append(tracks, external)
		}
	}
	album.TrackCount = len(tracks)
	return model.ExternalAlbumDetails{Album: album, Tracks: tracks}, nil
}

func (c *Client) Recording(ctx context.Context, recordingID string) (model.ExternalTrack, error) {
	if err := validateID(recordingID); err != nil {
		return model.ExternalTrack{}, err
	}
	var recording mbRecording
	if err := c.get(ctx, "/recording/"+recordingID, values("inc", "releases artist-credits tags"), &recording); err != nil {
		return model.ExternalTrack{}, fmt.Errorf("get recording: %w", err)
	}
	return externalTrack(recording), nil
}

// SearchSongs searches the recording catalog and returns the matches ordered
// by relevance (title match first, then ListenBrainz popularity).
func (c *Client) SearchSongs(ctx context.Context, query string) ([]model.ExternalTrack, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	var recordings mbRecordingSearchResponse
	if err := c.get(ctx, "/recording", recordingQueryValues(query), &recordings); err != nil {
		return nil, fmt.Errorf("search songs: %w", err)
	}
	popularity := map[string]model.RecordingPopularity(nil)
	if c.popularity != nil && len(recordings.Recordings) > 0 {
		var err error
		popularity, err = c.popularity.GetRecordingPopularity(ctx, recordingIDs(recordings.Recordings))
		if err != nil {
			log.Warn(ctx, "ListenBrainz popularity lookup failed; using MusicBrainz ordering", err)
			popularity = nil
		}
	}
	sortRecordingsByRelevance(recordings.Recordings, query, popularity)
	songs := make([]model.ExternalTrack, 0, len(recordings.Recordings))
	for _, recording := range recordings.Recordings {
		songs = append(songs, externalTrack(recording))
	}
	return songs, nil
}

func (c *Client) get(ctx context.Context, path string, params url.Values, target ...any) error {
	params = cloneValues(params)
	params.Set("fmt", "json")
	if params.Get("limit") == "" {
		params.Set("limit", "20")
	}

	parsed, err := url.Parse(c.baseURL + path)
	if err != nil {
		return err
	}
	parsed.RawQuery = params.Encode()
	cacheKey := parsed.String()
	if body, ok := c.cached(cacheKey); ok {
		return json.Unmarshal(body, targetValue(target))
	}

	var lastErr error
	for attempt := 1; attempt <= transientAttempts; attempt++ {
		if err := c.waitForRateLimit(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Navidrome/2 (https://github.com/navidrome/navidrome)")
		resp, err := c.http.Do(req)
		if err != nil {
			// A canceled or expired context is not transient; everything else is.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return err
			}
			lastErr = err
			if attempt < transientAttempts {
				if err := sleep(ctx, transientBackoff(attempt, 0)); err != nil {
					return err
				}
				continue
			}
			return err
		}
		if resp.StatusCode == http.StatusNotFound {
			_ = resp.Body.Close()
			return model.ErrNotFound
		}
		if isTransientStatus(resp.StatusCode) && attempt < transientAttempts {
			wait := retryAfter(resp)
			if wait <= 0 {
				wait = transientBackoff(attempt, 0)
			}
			lastErr = fmt.Errorf("musicbrainz returned HTTP %d", resp.StatusCode)
			_ = resp.Body.Close()
			if err := sleep(ctx, wait); err != nil {
				return err
			}
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
		closeErr := resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
			return fmt.Errorf("musicbrainz returned HTTP %d", resp.StatusCode)
		}
		if len(body) > maxResponse {
			return fmt.Errorf("musicbrainz response exceeds size limit")
		}
		c.store(cacheKey, body)
		if err := json.Unmarshal(body, targetValue(target)); err != nil {
			return fmt.Errorf("decode musicbrainz response: %w", err)
		}
		return nil
	}
	return lastErr
}

const transientAttempts = 3

func isTransientStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// retryAfter returns the number of seconds to wait before retrying, honoring
// the Retry-After header when present.
func retryAfter(resp *http.Response) time.Duration {
	if raw := strings.TrimSpace(resp.Header.Get("Retry-After")); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return 0
}

func transientBackoff(attempt int, jitter time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	wait := time.Duration(attempt) * time.Second
	if jitter > 0 {
		wait += time.Duration(rand.Int63n(int64(jitter)))
	}
	return wait
}

func sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) waitForRateLimit(ctx context.Context) error {
	c.rateMu.Lock()
	now := time.Now()
	nextRequest := now
	if c.lastRequest.After(nextRequest) {
		nextRequest = c.lastRequest
	}
	wait := nextRequest.Sub(now)
	c.lastRequest = nextRequest.Add(time.Second)
	c.rateMu.Unlock()
	if wait == 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) cached(key string) ([]byte, bool) {
	c.cacheMu.RLock()
	entry, ok := c.cache[key]
	c.cacheMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return bytes.Clone(entry.body), true
}

func (c *Client) store(key string, body []byte) {
	c.cacheMu.Lock()
	c.cache[key] = cacheEntry{body: bytes.Clone(body), expiresAt: time.Now().Add(cacheTTL)}
	c.cacheMu.Unlock()
}

func targetValue(target []any) any {
	if len(target) == 0 || target[0] == nil {
		return &struct{}{}
	}
	return target[0]
}

func queryValues(query string) url.Values {
	params := url.Values{}
	params.Set("query", strings.TrimSpace(query))
	params.Set("limit", "20")
	params.Set("inc", "tags artist-credits")
	return params
}

func recordingQueryValues(query string) url.Values {
	params := queryValues(query)
	params.Set("limit", fmt.Sprintf("%d", recordingSearchLimit))
	return params
}

func recordingIDs(recordings []mbRecording) []string {
	ids := make([]string, 0, len(recordings))
	for _, recording := range recordings {
		if recording.ID != "" {
			ids = append(ids, recording.ID)
		}
	}
	return ids
}

func values(key, value string) url.Values {
	params := url.Values{}
	params.Set(key, value)
	return params
}

func cloneValues(values url.Values) url.Values {
	cloned := url.Values{}
	for key, values := range values {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func validateID(value string) error {
	value = strings.TrimSpace(value)
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return fmt.Errorf("%w: invalid MusicBrainz ID", model.ErrValidation)
	}
	for index, char := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
			return fmt.Errorf("%w: invalid MusicBrainz ID", model.ErrValidation)
		}
	}
	if strings.ContainsAny(value, `/\\?#`) {
		return fmt.Errorf("%w: invalid MusicBrainz ID", model.ErrValidation)
	}
	return nil
}

func externalArtist(artist mbArtist) model.ExternalArtist {
	return model.ExternalArtist{
		ID:             artist.ID,
		Name:           firstNonEmpty(artist.Name, artist.SortName),
		SortName:       artist.SortName,
		Country:        artist.Country,
		Disambiguation: artist.Disambiguation,
		Type:           artist.Type,
	}
}

func externalAlbum(group mbReleaseGroup) model.ExternalAlbum {
	return model.ExternalAlbum{
		ID:          group.ID,
		Title:       group.Title,
		ArtistID:    artistCreditID(group.ArtistCredit),
		ArtistName:  artistCreditName(group.ArtistCredit),
		ReleaseDate: group.FirstReleaseDate,
		Year:        yearFromDate(group.FirstReleaseDate),
		Type:        group.PrimaryType,
		TrackCount:  0,
		ImageURL:    coverArtURL(group.ID),
	}
}

func externalRecording(recording mbRecording) model.ExternalTrack {
	return model.ExternalTrack{
		ID:         recording.ID,
		Title:      recording.Title,
		ArtistID:   artistCreditID(recording.ArtistCredit),
		ArtistName: artistCreditName(recording.ArtistCredit),
		Duration:   recording.Length / 1000,
		Genre:      firstTag(recording.Tags),
	}
}

func externalTrack(recording mbRecording) model.ExternalTrack {
	track := externalRecording(recording)
	for _, release := range recording.Releases {
		if release.ReleaseGroup.ID == "" {
			continue
		}
		track.AlbumID = release.ReleaseGroup.ID
		track.AlbumTitle = firstNonEmpty(release.ReleaseGroup.Title, release.Title)
		track.ReleaseDate = firstNonEmpty(release.ReleaseGroup.FirstReleaseDate, release.Date)
		track.Year = yearFromDate(track.ReleaseDate)
		track.ImageURL = coverArtURL(release.ReleaseGroup.ID)
		break
	}
	return track
}

func artistCreditName(credits []mbArtistCredit) string {
	if len(credits) == 0 {
		return ""
	}
	names := make([]string, 0, len(credits))
	for _, credit := range credits {
		name := firstNonEmpty(credit.Name, credit.Artist.Name)
		if name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, ", ")
}

func artistCreditID(credits []mbArtistCredit) string {
	if len(credits) == 0 {
		return ""
	}
	return credits[0].Artist.ID
}

func collectTags(destination map[string]struct{}, tags []mbTag, query string) {
	query = strings.ToLower(strings.TrimSpace(query))
	for _, tag := range tags {
		name := strings.TrimSpace(tag.Name)
		if name != "" && (query == "" || strings.Contains(strings.ToLower(name), query)) {
			destination[name] = struct{}{}
		}
	}
}

func firstTag(tags []mbTag) string {
	if len(tags) == 0 {
		return ""
	}
	return tags[0].Name
}

func coverArtURL(releaseGroupID string) string {
	if releaseGroupID == "" {
		return ""
	}
	return "https://coverartarchive.org/release-group/" + releaseGroupID + "/front-250"
}

func yearFromDate(value string) int {
	if len(value) < 4 {
		return 0
	}
	var year int
	if _, err := fmt.Sscanf(value[:4], "%d", &year); err != nil {
		return 0
	}
	return year
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

type mbTag struct {
	Name string `json:"name"`
}

type mbArtist struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	SortName       string  `json:"sort-name"`
	Country        string  `json:"country"`
	Disambiguation string  `json:"disambiguation"`
	Type           string  `json:"type"`
	Tags           []mbTag `json:"tags"`
}

type mbArtistCredit struct {
	Name   string   `json:"name"`
	Artist mbArtist `json:"artist"`
}

type mbReleaseGroup struct {
	ID               string             `json:"id"`
	Title            string             `json:"title"`
	FirstReleaseDate string             `json:"first-release-date"`
	PrimaryType      string             `json:"primary-type"`
	ReleaseCount     int                `json:"release-count"`
	ArtistCredit     []mbArtistCredit   `json:"artist-credit"`
	Tags             []mbTag            `json:"tags"`
	Releases         []mbReleaseSummary `json:"releases"`
}

type mbReleaseSummary struct {
	ID   string `json:"id"`
	Date string `json:"date"`
}

type mbRelease struct {
	ID           string           `json:"id"`
	Date         string           `json:"date"`
	ArtistCredit []mbArtistCredit `json:"artist-credit"`
	Media        []mbMedium       `json:"media"`
}

type mbMedium struct {
	Position int       `json:"position"`
	Tracks   []mbTrack `json:"tracks"`
}

type mbTrack struct {
	Position  int         `json:"position"`
	Recording mbRecording `json:"recording"`
}

type mbRecording struct {
	ID           string               `json:"id"`
	Title        string               `json:"title"`
	Score        int                  `json:"score"`
	Length       int                  `json:"length"`
	ArtistCredit []mbArtistCredit     `json:"artist-credit"`
	Tags         []mbTag              `json:"tags"`
	Releases     []mbRecordingRelease `json:"releases"`
}

type mbRecordingRelease struct {
	Date         string         `json:"date"`
	Title        string         `json:"title"`
	ReleaseGroup mbReleaseGroup `json:"release-group"`
}

type mbArtistSearchResponse struct {
	Artists []mbArtist `json:"artists"`
}

type mbReleaseGroupSearchResponse struct {
	ReleaseGroups []mbReleaseGroup `json:"release-groups"`
}

type mbReleaseGroupBrowseResponse struct {
	ReleaseGroups []mbReleaseGroup `json:"release-groups"`
}

type mbRecordingSearchResponse struct {
	Recordings []mbRecording `json:"recordings"`
}

type mbTagSearchResponse struct {
	Tags []mbTag `json:"tags"`
}
