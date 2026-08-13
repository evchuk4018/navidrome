package listenbrainz

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/navidrome/navidrome/conf"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
)

const (
	lbzApiUrl = "https://api.listenbrainz.org/1/"
	labsBase  = "https://labs.api.listenbrainz.org/"

	popularityEndpoint  = "popularity/recording"
	popularityBatchSize = 100
	popularityCacheTTL  = 10 * time.Minute
)

var (
	ErrorNotFound = errors.New("listenbrainz: not found")
)

type listenBrainzError struct {
	Code    int
	Message string
}

func (e *listenBrainzError) Error() string {
	return fmt.Sprintf("ListenBrainz error(%d): %s", e.Code, e.Message)
}

type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

func newClient(baseURL string, hc httpDoer) *client {
	return &client{baseURL, hc}
}

type client struct {
	baseURL string
	hc      httpDoer
}

// PopularityClient retrieves aggregate recording popularity from ListenBrainz.
// It is separate from the authenticated ListenBrainz agent because external
// popularity data does not require a user token.
type PopularityClient struct {
	client *client

	cacheMu sync.RWMutex
	cache   map[string]popularityCacheEntry
}

type popularityCacheEntry struct {
	value     model.RecordingPopularity
	expiresAt time.Time
}

type recordingPopularityRequest struct {
	RecordingMBIDs []string `json:"recording_mbids"`
}

type recordingPopularityResponse struct {
	RecordingMBID    string `json:"recording_mbid"`
	TotalListenCount *int64 `json:"total_listen_count"`
	TotalUserCount   *int64 `json:"total_user_count"`
}

// NewPopularityClient creates a client for the public ListenBrainz popularity
// API. The endpoint is batch-based and does not require authentication.
func NewPopularityClient() *PopularityClient {
	return newPopularityClient(lbzApiUrl, http.DefaultClient)
}

func newPopularityClient(baseURL string, hc httpDoer) *PopularityClient {
	return &PopularityClient{
		client: newClient(baseURL, hc),
		cache:  make(map[string]popularityCacheEntry),
	}
}

type listenBrainzResponse struct {
	Code     int    `json:"code"`
	Message  string `json:"message"`
	Error    string `json:"error"`
	Status   string `json:"status"`
	Valid    bool   `json:"valid"`
	UserName string `json:"user_name"`
}

type listenBrainzRequest struct {
	ApiKey string //nolint:gosec
	Body   listenBrainzRequestBody
}

type listenBrainzRequestBody struct {
	ListenType listenType   `json:"listen_type,omitempty"`
	Payload    []listenInfo `json:"payload,omitempty"`
}

type listenType string

const (
	Single     listenType = "single"
	PlayingNow listenType = "playing_now"
)

type listenInfo struct {
	ListenedAt    int           `json:"listened_at,omitempty"`
	TrackMetadata trackMetadata `json:"track_metadata"`
}

type trackMetadata struct {
	ArtistName     string         `json:"artist_name,omitempty"`
	TrackName      string         `json:"track_name,omitempty"`
	ReleaseName    string         `json:"release_name,omitempty"`
	AdditionalInfo additionalInfo `json:"additional_info"`
}

type additionalInfo struct {
	SubmissionClient        string   `json:"submission_client,omitempty"`
	SubmissionClientVersion string   `json:"submission_client_version,omitempty"`
	TrackNumber             int      `json:"tracknumber,omitempty"`
	ArtistNames             []string `json:"artist_names,omitempty"`
	ArtistMBIDs             []string `json:"artist_mbids,omitempty"`
	RecordingMBID           string   `json:"recording_mbid,omitempty"`
	ReleaseMBID             string   `json:"release_mbid,omitempty"`
	ReleaseGroupMBID        string   `json:"release_group_mbid,omitempty"`
	DurationMs              int      `json:"duration_ms,omitempty"`
}

func (c *client) validateToken(ctx context.Context, apiKey string) (*listenBrainzResponse, error) {
	r := &listenBrainzRequest{
		ApiKey: apiKey,
	}
	response, err := c.makeAuthenticatedRequest(ctx, http.MethodGet, "validate-token", r)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func (c *client) updateNowPlaying(ctx context.Context, apiKey string, li listenInfo) error {
	r := &listenBrainzRequest{
		ApiKey: apiKey,
		Body: listenBrainzRequestBody{
			ListenType: PlayingNow,
			Payload:    []listenInfo{li},
		},
	}

	resp, err := c.makeAuthenticatedRequest(ctx, http.MethodPost, "submit-listens", r)
	if err != nil {
		return err
	}
	if resp.Status != "ok" {
		log.Warn(ctx, "ListenBrainz: NowPlaying was not accepted", "status", resp.Status)
	}
	return nil
}

func (c *client) scrobble(ctx context.Context, apiKey string, li listenInfo) error {
	r := &listenBrainzRequest{
		ApiKey: apiKey,
		Body: listenBrainzRequestBody{
			ListenType: Single,
			Payload:    []listenInfo{li},
		},
	}
	resp, err := c.makeAuthenticatedRequest(ctx, http.MethodPost, "submit-listens", r)
	if err != nil {
		return err
	}
	if resp.Status != "ok" {
		log.Warn(ctx, "ListenBrainz: Scrobble was not accepted", "status", resp.Status)
	}
	return nil
}

func (c *client) path(endpoint string) (string, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return "", err
	}
	u.Path = path.Join(u.Path, endpoint)
	return u.String(), nil
}

func (c *client) makeAuthenticatedRequest(ctx context.Context, method string, endpoint string, r *listenBrainzRequest) (*listenBrainzResponse, error) {
	b, _ := json.Marshal(r.Body)
	uri, err := c.path(endpoint)
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, method, uri, bytes.NewBuffer(b))
	req.Header.Add("Content-Type", "application/json; charset=UTF-8")

	if r.ApiKey != "" {
		req.Header.Add("Authorization", fmt.Sprintf("Token %s", r.ApiKey))
	}

	log.Trace(ctx, fmt.Sprintf("Sending ListenBrainz %s request", req.Method), "url", req.URL)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)

	var response listenBrainzResponse
	jsonErr := decoder.Decode(&response)
	if resp.StatusCode != 200 && jsonErr != nil {
		return nil, fmt.Errorf("ListenBrainz: HTTP Error, Status: (%d)", resp.StatusCode)
	}
	if jsonErr != nil {
		return nil, jsonErr
	}
	if response.Code != 0 && response.Code != 200 {
		return &response, &listenBrainzError{Code: response.Code, Message: response.Error}
	}

	return &response, nil
}

type lbzHttpError struct {
	Code  int    `json:"code"`
	Error string `json:"error"`
}

func (c *client) makeGenericRequest(ctx context.Context, method string, endpoint string, params url.Values) (*http.Response, error) {
	req, _ := http.NewRequestWithContext(ctx, method, lbzApiUrl+endpoint, nil)
	req.Header.Add("Content-Type", "application/json; charset=UTF-8")
	req.URL.RawQuery = params.Encode()

	log.Trace(ctx, fmt.Sprintf("Sending ListenBrainz %s request", req.Method), "url", req.URL)
	resp, err := c.hc.Do(req)

	if err != nil {
		return nil, err
	}

	// On a 200 code, there is no code. Decode using using error message if it exists
	if resp.StatusCode != 200 {
		defer resp.Body.Close()
		decoder := json.NewDecoder(resp.Body)

		var lbzError lbzHttpError
		jsonErr := decoder.Decode(&lbzError)

		if jsonErr != nil {
			return nil, fmt.Errorf("ListenBrainz: HTTP Error, Status: (%d)", resp.StatusCode)
		}

		return nil, &listenBrainzError{Code: lbzError.Code, Message: lbzError.Error}
	}

	return resp, err
}

type artistMetadataResult struct {
	Rels struct {
		OfficialHomepage string `json:"official homepage,omitempty"`
	} `json:"rels,omitzero"`
}

func (c *client) getArtistUrl(ctx context.Context, mbid string) (string, error) {
	params := url.Values{}
	params.Add("artist_mbids", mbid)
	resp, err := c.makeGenericRequest(ctx, http.MethodGet, "metadata/artist", params)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)

	var response []artistMetadataResult
	jsonErr := decoder.Decode(&response)
	if jsonErr != nil {
		return "", fmt.Errorf("ListenBrainz: HTTP Error, Status: (%d)", resp.StatusCode)
	}

	if len(response) == 0 || response[0].Rels.OfficialHomepage == "" {
		return "", ErrorNotFound
	}

	return response[0].Rels.OfficialHomepage, nil
}

type trackInfo struct {
	ArtistName    string   `json:"artist_name"`
	ArtistMBIDs   []string `json:"artist_mbids"`
	DurationMs    uint32   `json:"length"`
	RecordingName string   `json:"recording_name"`
	RecordingMbid string   `json:"recording_mbid"`
	ReleaseName   string   `json:"release_name"`
	ReleaseMBID   string   `json:"release_mbid"`
}

func (c *client) getArtistTopSongs(ctx context.Context, mbid string, count int) ([]trackInfo, error) {
	resp, err := c.makeGenericRequest(ctx, http.MethodGet, "popularity/top-recordings-for-artist/"+mbid, url.Values{})
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)

	var response []trackInfo
	jsonErr := decoder.Decode(&response)
	if jsonErr != nil {
		return nil, fmt.Errorf("ListenBrainz: HTTP Error, Status: (%d)", resp.StatusCode)
	}

	if len(response) > count {
		return response[0:count], nil
	}

	return response, nil
}

// GetRecordingPopularity returns ListenBrainz aggregate popularity for the
// requested recording MBIDs. Missing recordings are represented by zero-value
// popularity, matching the API's null count response. Results are cached so a
// repeated search does not re-query unchanged popularity data.
func (c *PopularityClient) GetRecordingPopularity(ctx context.Context, recordingMBIDs []string) (map[string]model.RecordingPopularity, error) {
	requested := uniqueRecordingMBIDs(recordingMBIDs)
	result := make(map[string]model.RecordingPopularity, len(requested))
	missing := make([]string, 0, len(requested))

	for _, recordingMBID := range requested {
		if popularity, ok := c.cachedPopularity(recordingMBID); ok {
			result[recordingMBID] = popularity
			continue
		}
		missing = append(missing, recordingMBID)
	}

	for start := 0; start < len(missing); start += popularityBatchSize {
		end := min(start+popularityBatchSize, len(missing))
		batch := missing[start:end]
		popularity, err := c.fetchRecordingPopularity(ctx, batch)
		if err != nil {
			return nil, err
		}

		for _, recordingMBID := range batch {
			value := popularity[recordingMBID]
			c.cachePopularity(recordingMBID, value)
			result[recordingMBID] = value
		}
	}

	return result, nil
}

func uniqueRecordingMBIDs(recordingMBIDs []string) []string {
	unique := make([]string, 0, len(recordingMBIDs))
	seen := make(map[string]struct{}, len(recordingMBIDs))
	for _, recordingMBID := range recordingMBIDs {
		recordingMBID = strings.TrimSpace(recordingMBID)
		if recordingMBID == "" {
			continue
		}
		if _, ok := seen[recordingMBID]; ok {
			continue
		}
		seen[recordingMBID] = struct{}{}
		unique = append(unique, recordingMBID)
	}
	return unique
}

func (c *PopularityClient) fetchRecordingPopularity(ctx context.Context, recordingMBIDs []string) (map[string]model.RecordingPopularity, error) {
	body, err := json.Marshal(recordingPopularityRequest{RecordingMBIDs: recordingMBIDs})
	if err != nil {
		return nil, err
	}
	uri, err := c.client.path(popularityEndpoint)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uri, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=UTF-8")

	log.Trace(ctx, fmt.Sprintf("Sending ListenBrainz %s request", req.Method), "url", req.URL)
	resp, err := c.client.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var response lbzHttpError
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			return nil, fmt.Errorf("ListenBrainz: HTTP Error, Status: (%d)", resp.StatusCode)
		}
		if response.Code == 0 {
			response.Code = resp.StatusCode
		}
		return nil, &listenBrainzError{Code: response.Code, Message: response.Error}
	}

	var response []recordingPopularityResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("ListenBrainz: HTTP Error, Status: (%d)", resp.StatusCode)
	}

	popularity := make(map[string]model.RecordingPopularity, len(response))
	for _, item := range response {
		value := model.RecordingPopularity{}
		if item.TotalListenCount != nil {
			value.TotalListenCount = *item.TotalListenCount
		}
		if item.TotalUserCount != nil {
			value.TotalUserCount = *item.TotalUserCount
		}
		popularity[item.RecordingMBID] = value
	}
	return popularity, nil
}

func (c *PopularityClient) cachedPopularity(recordingMBID string) (model.RecordingPopularity, bool) {
	c.cacheMu.RLock()
	entry, ok := c.cache[recordingMBID]
	c.cacheMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return model.RecordingPopularity{}, false
	}
	return entry.value, true
}

func (c *PopularityClient) cachePopularity(recordingMBID string, value model.RecordingPopularity) {
	c.cacheMu.Lock()
	c.cache[recordingMBID] = popularityCacheEntry{
		value:     value,
		expiresAt: time.Now().Add(popularityCacheTTL),
	}
	c.cacheMu.Unlock()
}

type artist struct {
	MBID  string `json:"artist_mbid"`
	Name  string `json:"name"`
	Score int    `json:"score"`
}

func (c *client) getSimilarArtists(ctx context.Context, mbid string, limit int) ([]artist, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, labsBase+"similar-artists/json", nil)
	req.Header.Add("Content-Type", "application/json; charset=UTF-8")
	req.URL.RawQuery = url.Values{
		"artist_mbids": []string{mbid}, "algorithm": []string{conf.Server.ListenBrainz.ArtistAlgorithm},
	}.Encode()

	log.Trace(ctx, fmt.Sprintf("Sending ListenBrainz Labs %s request", req.Method), "url", req.URL)
	resp, err := c.hc.Do(req)

	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)

	var artists []artist
	jsonErr := decoder.Decode(&artists)
	if jsonErr != nil {
		return nil, fmt.Errorf("ListenBrainz: HTTP Error, Status: (%d)", resp.StatusCode)
	}

	if len(artists) > limit {
		return artists[:limit], nil
	}

	return artists, nil
}

type recording struct {
	MBID        string `json:"recording_mbid"`
	Name        string `json:"recording_name"`
	Artist      string `json:"artist_credit_name"`
	ReleaseName string `json:"release_name"`
	ReleaseMBID string `json:"release_mbid"`
	Score       int    `json:"score"`
}

func (c *client) getSimilarRecordings(ctx context.Context, mbid string, limit int) ([]recording, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, labsBase+"similar-recordings/json", nil)
	req.Header.Add("Content-Type", "application/json; charset=UTF-8")
	req.URL.RawQuery = url.Values{
		"recording_mbids": []string{mbid}, "algorithm": []string{conf.Server.ListenBrainz.TrackAlgorithm},
	}.Encode()

	log.Trace(ctx, fmt.Sprintf("Sending ListenBrainz Labs %s request", req.Method), "url", req.URL)
	resp, err := c.hc.Do(req)

	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)

	var recordings []recording
	jsonErr := decoder.Decode(&recordings)
	if jsonErr != nil {
		return nil, fmt.Errorf("ListenBrainz: HTTP Error, Status: (%d)", resp.StatusCode)
	}

	// For whatever reason, labs API isn't guaranteed to give results in the proper order
	// and may also provide duplicates. See listenbrainz.labs.similar-recordings-real-out-of-order.json
	// generated from https://labs.api.listenbrainz.org/similar-recordings/json?recording_mbids=8f3471b5-7e6a-48da-86a9-c1c07a0f47ae&algorithm=session_based_days_180_session_300_contribution_5_threshold_15_limit_50_skip_30
	slices.SortFunc(recordings, func(a, b recording) int {
		return cmp.Or(
			cmp.Compare(b.Score, a.Score), // Sort by score descending
			cmp.Compare(a.MBID, b.MBID),   // Then by MBID ascending to ensure deterministic order for duplicates
		)
	})

	recordings = slices.CompactFunc(recordings, func(a, b recording) bool {
		return a.MBID == b.MBID
	})

	if len(recordings) > limit {
		return recordings[:limit], nil
	}

	return recordings, nil
}
