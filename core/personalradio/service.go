package personalradio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Masterminds/squirrel"
	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/core/matcher"
	musicservice "github.com/navidrome/navidrome/core/music"
	"github.com/navidrome/navidrome/core/recommendations"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
)

const (
	discoveryCandidateLimit  = 40
	transitionCandidateLimit = 50
	// readyLowWatermark controls uninterrupted local playback. totalQueueTarget
	// controls how many ready/held/downloading items the planner keeps ahead.
	readyLowWatermark          = 3
	totalQueueTarget           = 10
	queueLowWatermark          = totalQueueTarget // legacy name used by tests/callers
	maxConcurrentDownloads     = 2
	discoveryTTL               = 7 * 24 * time.Hour
	localFallbackPageSize      = 500
	radioFeedbackBatchSize     = 500
	providerPlanningTimeout    = 4 * time.Second
	initialLocalCandidateLimit = 96
)

type SimilarityProvider interface {
	GetSimilarSongsByTrackAll(context.Context, string, string, string, string, int) ([]agents.Song, error)
}

type Service interface {
	Start(context.Context)
	Create(context.Context, string, model.CreatePersonalRadioRequest) (*model.PersonalRadioSessionResponse, error)
	Refill(context.Context, string, string, model.RefillPersonalRadioRequest) (*model.PersonalRadioSessionResponse, error)
	Feedback(context.Context, string, string, model.PersonalRadioFeedbackRequest) error
	End(context.Context, string, string, model.EndPersonalRadioRequest) error
}

type service struct {
	ds              model.DataStore
	repo            model.PersonalRadioRepository
	agents          SimilarityProvider
	matcher         *matcher.Matcher
	music           musicservice.Service
	scanner         model.Scanner
	startOnce       sync.Once
	planningMu      sync.Mutex
	planning        map[string]bool
	planningStatus  map[string]string
	requestMu       sync.Mutex
	requestSessions map[string]string
	fallbackMu      sync.Mutex
	fallbackPool    model.MediaFiles
	fallbackLoaded  time.Time
}

func New(ds model.DataStore, repo model.PersonalRadioRepository, ag *agents.Agents, songMatcher *matcher.Matcher, music musicservice.Service, scanner model.Scanner) Service {
	return &service{
		ds:              ds,
		repo:            repo,
		agents:          ag,
		matcher:         songMatcher,
		music:           music,
		scanner:         scanner,
		planning:        map[string]bool{},
		planningStatus:  map[string]string{},
		requestSessions: map[string]string{},
	}
}

func (s *service) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.startOnce.Do(func() { go s.cleanupLoop(ctx) })
}

func (s *service) Create(ctx context.Context, userID string, request model.CreatePersonalRadioRequest) (*model.PersonalRadioSessionResponse, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	clientRequestID := strings.TrimSpace(request.ClientRequestID)
	requestKey := userID + "\x00" + clientRequestID
	if clientRequestID != "" {
		if existing := s.lookupInFlightRequest(requestKey); existing != "" {
			if response, err := s.responseForSession(ctx, userID, existing); err == nil {
				return response, nil
			}
		}
		if lookup, ok := s.repo.(model.PersonalRadioSessionLookup); ok {
			if existing, err := lookup.GetSessionByClientRequest(userID, clientRequestID); err == nil && existing != nil {
				s.rememberRequest(requestKey, existing.ID)
				return s.responseForSession(ctx, userID, existing.ID)
			}
		}
	}
	seed, seedIDs, seedWeights, sourceType, sourceID, err := s.resolveCreateSource(ctx, request)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	session := model.PersonalRadioSession{
		ID: id.NewRandom(), UserID: userID, SeedMediaFileID: seed.ID,
		SourceType: sourceType, SourceID: sourceID, ClientRequestID: clientRequestID,
		SourcePlaylistID: strings.TrimSpace(request.SourcePlaylistID), SeedMediaFileIDs: seedIDs, SeedMediaFileWeights: seedWeights,
		Mode: model.NormalizeRadioMode(string(request.Mode)), Status: model.PersonalRadioActive,
		Revision: 1, Autoplay: true, CreatedAt: now, UpdatedAt: now,
	}
	items := []model.PersonalRadioItem{{ID: id.NewRandom(), SessionID: session.ID, Position: 0, ItemType: model.RadioItemSeed, Status: model.RadioItemReady, MediaFileID: seed.ID, RecordingMBID: seed.MbzRecordingID, Song: seed, CreatedAt: now, UpdatedAt: now}}
	if err := s.repo.CreateSession(&session, items); err != nil {
		// A concurrent request with the same idempotency key may win the unique
		// insert. Return that authoritative session instead of creating another
		// active queue.
		if clientRequestID != "" {
			if lookup, ok := s.repo.(model.PersonalRadioSessionLookup); ok {
				if existing, lookupErr := lookup.GetSessionByClientRequest(userID, clientRequestID); lookupErr == nil && existing != nil {
					s.rememberRequest(requestKey, existing.ID)
					return s.responseForSession(ctx, userID, existing.ID)
				}
			}
		}
		return nil, err
	}
	if clientRequestID != "" {
		s.rememberRequest(requestKey, session.ID)
	}
	if err := s.repo.EndActiveSessions(userID, session.ID); err != nil {
		return nil, err
	}
	// Keep the first response useful even when providers or download services
	// are slow: local successors are selected synchronously and external work
	// starts only after this durable two-track buffer exists.
	if err := s.planInitialReady(ctx, &session, seed, items); err != nil {
		log.Debug(ctx, "Personal radio initial local buffer unavailable", "sessionID", session.ID, "error", err)
	}
	s.setPlanningStatus(session.ID, model.RadioPlanningSelecting)
	log.Info(ctx, "Personal radio session created",
		"sessionID", session.ID,
		"userID", userID,
		"seedID", seed.ID,
		"seedTitle", seed.Title,
		"seedArtist", seed.Artist,
		"seedRecordingMBID", seed.MbzRecordingID)
	if refreshed, refreshErr := s.repo.GetItems(session.ID); refreshErr == nil {
		items = refreshed
	}
	if refreshed, refreshErr := s.repo.GetSessionForUser(session.ID, userID); refreshErr == nil {
		session = *refreshed
	}
	s.schedulePlan(context.WithoutCancel(ctx), session, seed)
	return newPersonalRadioResponse(session, items, true, model.RadioPlanningSelecting), nil
}

func (s *service) lookupInFlightRequest(key string) string {
	s.requestMu.Lock()
	defer s.requestMu.Unlock()
	return s.requestSessions[key]
}

func (s *service) rememberRequest(key, sessionID string) {
	if key == "" || sessionID == "" {
		return
	}
	s.requestMu.Lock()
	if s.requestSessions == nil {
		s.requestSessions = map[string]string{}
	}
	s.requestSessions[key] = sessionID
	s.requestMu.Unlock()
}

func (s *service) responseForSession(ctx context.Context, userID, sessionID string) (*model.PersonalRadioSessionResponse, error) {
	session, err := s.repo.GetSessionForUser(sessionID, userID)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.GetItems(sessionID)
	if err != nil {
		return nil, err
	}
	if err := s.promoteHeldDiscoveries(ctx, session, items); err != nil {
		log.Debug(ctx, "Personal radio could not promote held discoveries", "sessionID", sessionID, "error", err)
	}
	if refreshed, refreshErr := s.repo.GetItems(sessionID); refreshErr == nil {
		items = refreshed
	}
	status := s.getPlanningStatus(sessionID)
	if status == "" {
		status = statusForReadyItems(items)
	}
	return newPersonalRadioResponse(*session, items, status != model.RadioPlanningReady, status), nil
}

func (s *service) resolveCreateSource(ctx context.Context, request model.CreatePersonalRadioRequest) (*model.MediaFile, []string, []float64, string, string, error) {
	if strings.TrimSpace(request.SeedMediaFileID) != "" {
		seed, err := s.ds.MediaFile(ctx).GetWithParticipants(strings.TrimSpace(request.SeedMediaFileID))
		if err != nil {
			return nil, nil, nil, "", "", err
		}
		return seed, []string{seed.ID}, []float64{1}, model.RadioSourceSong, seed.ID, nil
	}
	playlistID := strings.TrimSpace(request.SourcePlaylistID)
	playlist, err := s.ds.Playlist(ctx).GetWithTracks(playlistID, false, false)
	if err != nil {
		return nil, nil, nil, "", "", err
	}
	if playlist == nil || len(playlist.Tracks) == 0 {
		return nil, nil, nil, "", "", model.ErrNotFound
	}
	// Select at most five weighted/diverse seeds. The representative first
	// track is stable; subsequent tracks prefer new artists/albums, then fill
	// remaining slots in playlist order. Persisting this bounded set prevents a
	// long playlist from turning every refill into an N+1 scan.
	var tracks []model.MediaFile
	for _, track := range playlist.Tracks {
		if strings.TrimSpace(track.MediaFileID) == "" {
			continue
		}
		file := track.MediaFile
		if file.ID == "" {
			filePtr, getErr := s.ds.MediaFile(ctx).GetWithParticipants(track.MediaFileID)
			if getErr != nil || filePtr == nil {
				continue
			}
			file = *filePtr
		}
		if file.Missing || file.ID == "" {
			continue
		}
		tracks = append(tracks, file)
	}
	if len(tracks) == 0 {
		return nil, nil, nil, "", "", model.ErrNotFound
	}
	selected := make([]model.MediaFile, 0, min(5, len(tracks)))
	artists, albums := map[string]bool{}, map[string]bool{}
	for _, file := range tracks {
		artist := strings.ToLower(strings.TrimSpace(file.ArtistID + "\x00" + file.Artist))
		album := strings.ToLower(strings.TrimSpace(file.AlbumID + "\x00" + file.Album))
		if len(selected) > 0 && (artists[artist] || albums[album]) {
			continue
		}
		selected = append(selected, file)
		artists[artist], albums[album] = true, true
		if len(selected) == 5 {
			break
		}
	}
	for _, file := range tracks {
		if len(selected) == 5 {
			break
		}
		seen := false
		for _, current := range selected {
			if current.ID == file.ID {
				seen = true
				break
			}
		}
		if !seen {
			selected = append(selected, file)
		}
	}
	seed := selected[0]
	seedIDs := make([]string, 0, len(selected))
	seedWeights := make([]float64, 0, len(selected))
	for _, file := range selected {
		seedIDs = append(seedIDs, file.ID)
		seedWeights = append(seedWeights, 1/float64(len(seedWeights)+1))
	}
	return &seed, seedIDs, seedWeights, model.RadioSourcePlaylist, playlistID, nil
}

func (s *service) planInitialReady(ctx context.Context, session *model.PersonalRadioSession, seed *model.MediaFile, existing []model.PersonalRadioItem) error {
	if s.ds == nil || s.repo == nil || seed == nil {
		return nil
	}
	ready := readyPlayableRadioItems(existing)
	if ready >= 2 {
		return nil
	}
	files, err := s.ds.MediaFile(ctx).GetAll(model.QueryOptions{Sort: "id", Order: "asc", Max: initialLocalCandidateLimit})
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	seenRecordings := map[string]bool{}
	position := 1
	for _, item := range existing {
		seen[item.MediaFileID] = true
		if key := normalizeRecordingMBID(item.RecordingMBID); key != "" {
			seenRecordings[key] = true
		}
		if item.Position >= position {
			position = item.Position + 1
		}
	}
	candidates := make([]model.MediaFile, 0, len(files))
	for _, file := range files {
		if seen[file.ID] || file.ID == "" || file.Missing {
			continue
		}
		if mbid := normalizeRecordingMBID(file.MbzRecordingID); mbid != "" && seenRecordings[mbid] {
			continue
		}
		candidates = append(candidates, file)
	}
	// Rank only the bounded shortlist; filesystem checks run only on tracks we
	// are about to append, keeping startup independent of library size.
	sort.SliceStable(candidates, func(left, right int) bool {
		leftScore := localSeedAffinity(seed, candidates[left])
		rightScore := localSeedAffinity(seed, candidates[right])
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		return candidates[left].ID < candidates[right].ID
	})
	newItems := make([]model.PersonalRadioItem, 0, 2-ready)
	for _, file := range candidates {
		if len(newItems)+ready >= 2 {
			break
		}
		if !isPlayableLocalFile(file) {
			continue
		}
		copy := file
		newItems = append(newItems, model.PersonalRadioItem{
			ID: id.NewRandom(), SessionID: session.ID, Position: position,
			ItemType: model.RadioItemLibrary, Status: model.RadioItemReady,
			MediaFileID: file.ID, RecordingMBID: file.MbzRecordingID, Song: &copy,
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		})
		position++
	}
	if len(newItems) == 0 {
		return nil
	}
	if err := s.repo.AppendItems(session.ID, newItems); err != nil {
		return err
	}
	session.Revision += 1
	return nil
}

func newPersonalRadioResponse(session model.PersonalRadioSession, items []model.PersonalRadioItem, pending bool, status string) *model.PersonalRadioSessionResponse {
	upNext := make([]model.PersonalRadioItem, 0, len(items))
	pendingItems := make([]model.PersonalRadioItem, 0)
	for _, item := range items {
		if item.Status == model.RadioItemHeld || item.Status == model.RadioItemDownloading {
			pendingItems = append(pendingItems, item)
			continue
		}
		if item.ItemType != model.RadioItemSeed && item.Status == model.RadioItemReady && item.MediaFileID != "" {
			upNext = append(upNext, item)
		}
	}
	if session.Revision <= 0 {
		session.Revision = 1
	}
	return &model.PersonalRadioSessionResponse{
		Session: session, Items: items, Revision: session.Revision,
		UpNext: upNext, PendingItems: pendingItems, Pending: pending || len(pendingItems) > 0,
		PlanningStatus: status,
	}
}

func (s *service) Refill(ctx context.Context, userID, sessionID string, request model.RefillPersonalRadioRequest) (*model.PersonalRadioSessionResponse, error) {
	start := time.Now()
	session, err := s.repo.GetSessionForUser(sessionID, userID)
	if err != nil {
		return nil, err
	}
	if request.Mode != "" {
		mode := model.NormalizeRadioMode(string(request.Mode))
		if session.Mode != mode {
			session.Mode = mode
			if err := s.repo.UpdateSession(session); err != nil {
				return nil, err
			}
		}
	} else {
		mode := model.NormalizeRadioMode(string(session.Mode))
		if session.Mode != mode {
			session.Mode = mode
			if err := s.repo.UpdateSession(session); err != nil {
				return nil, err
			}
		}
	}
	items, err := s.repo.GetItems(sessionID)
	if err != nil {
		return nil, err
	}
	log.Debug(ctx, "Personal radio refill started",
		"sessionID", sessionID,
		"userID", userID,
		"itemCount", len(items),
		"itemStatuses", radioItemStatusCounts(items))
	pending := false
	waitingForScan := false
	downloadFailed := false
	for i := range items {
		item := &items[i]
		if item.Status != model.RadioItemDownloading {
			continue
		}
		log.Debug(ctx, "Personal radio checking download item",
			"sessionID", sessionID,
			"userID", userID,
			"itemID", item.ID,
			"position", item.Position,
			"recordingMBID", item.RecordingMBID,
			"downloadJobID", item.DownloadJobID)
		if s.music == nil {
			item.Status = model.RadioItemFailed
			if err := s.updateRadioItem(ctx, item, "marking item failed because download service is unavailable"); err != nil {
				return nil, err
			}
			downloadFailed = true
			continue
		}
		job, getErr := s.music.GetDownload(ctx, userID, item.DownloadJobID)
		if getErr != nil {
			log.Warn(ctx, "Personal radio could not read download job",
				"sessionID", sessionID,
				"userID", userID,
				"itemID", item.ID,
				"downloadJobID", item.DownloadJobID,
				"notFound", errors.Is(getErr, model.ErrNotFound),
				"error", getErr)
			if errors.Is(getErr, model.ErrNotFound) {
				item.Status = model.RadioItemFailed
				if err := s.updateRadioItem(ctx, item, "marking item failed because download job was not found"); err != nil {
					return nil, err
				}
				downloadFailed = true
				continue
			}
			pending = true
			continue
		}
		if job == nil {
			log.Error(ctx, "Personal radio download service returned a nil job",
				"sessionID", sessionID,
				"userID", userID,
				"itemID", item.ID,
				"downloadJobID", item.DownloadJobID)
			item.Status = model.RadioItemFailed
			if err := s.updateRadioItem(ctx, item, "marking item failed because download job was nil"); err != nil {
				return nil, err
			}
			downloadFailed = true
			continue
		}
		// Expose the recommendation metadata while the download is in flight so
		// the queue can show what is being fetched before the track is ready.
		// The ready path below overwrites the stub with the imported file.
		if item.Song == nil && job.Title != "" {
			item.Song = &model.MediaFile{
				Title:  job.Title,
				Artist: job.Artist,
				Album:  job.Album,
			}
		}
		switch job.Status {
		case model.MusicDownloadSuccess:
			log.Debug(ctx, "Personal radio download completed; resolving imported track",
				"sessionID", sessionID,
				"userID", userID,
				"itemID", item.ID,
				"downloadJobID", job.ID,
				"recordingMBID", item.RecordingMBID,
				"downloadTitle", job.Title,
				"downloadArtist", job.Artist,
				"downloadAlbum", job.Album)
			if s.matcher == nil {
				log.Error(ctx, "Personal radio cannot resolve completed download because matcher is unavailable",
					"sessionID", sessionID,
					"itemID", item.ID,
					"downloadJobID", job.ID)
				item.Status = model.RadioItemFailed
				if err := s.updateRadioItem(ctx, item, "marking item failed because matcher is unavailable"); err != nil {
					return nil, err
				}
				downloadFailed = true
				continue
			}
			file, matched, matchErr := s.resolveDownloadedItem(ctx, item, job)
			if matchErr != nil {
				log.Warn(ctx, "Personal radio matcher failed for completed download; will retry resolution",
					"sessionID", sessionID,
					"userID", userID,
					"itemID", item.ID,
					"downloadJobID", job.ID,
					"recordingMBID", item.RecordingMBID,
					"error", matchErr)
				waitingForScan = true
				pending = true
				continue
			}
			if matched {
				item.MediaFileID = file.ID
				item.Song = &file
				item.Status = model.RadioItemReady
				if err := s.updateRadioItem(ctx, item, "marking completed download ready"); err != nil {
					return nil, err
				}
				expires := time.Now().UTC().Add(discoveryTTL)
				if err := s.repo.UpsertDiscovery(&model.DiscoveryTrack{ID: id.NewRandom(), UserID: userID, RecordingMBID: item.RecordingMBID, MediaFileID: file.ID, State: model.DiscoveryTemporary, ExpiresAt: &expires, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}); err != nil {
					log.Warn(ctx, "Personal radio could not persist resolved discovery",
						"sessionID", sessionID,
						"itemID", item.ID,
						"recordingMBID", item.RecordingMBID,
						"mediaFileID", file.ID,
						"error", err)
				}
				log.Info(ctx, "Personal radio download resolved",
					"sessionID", sessionID,
					"userID", userID,
					"itemID", item.ID,
					"downloadJobID", job.ID,
					"recordingMBID", item.RecordingMBID,
					"mediaFileID", file.ID,
					"title", file.Title,
					"artist", file.Artist)
				continue
			}
			log.Warn(ctx, "Personal radio download succeeded but imported track did not match",
				"sessionID", sessionID,
				"userID", userID,
				"itemID", item.ID,
				"downloadJobID", job.ID,
				"recordingMBID", item.RecordingMBID,
				"downloadTitle", job.Title,
				"downloadArtist", job.Artist,
				"downloadAlbum", job.Album)
			item.Status = model.RadioItemFailed
			if err := s.updateRadioItem(ctx, item, "marking unmatched completed download failed"); err != nil {
				return nil, err
			}
			downloadFailed = true
		case model.MusicDownloadFailed:
			log.Warn(ctx, "Personal radio download job failed",
				"sessionID", sessionID,
				"userID", userID,
				"itemID", item.ID,
				"downloadJobID", job.ID,
				"recordingMBID", item.RecordingMBID,
				"message", job.Message,
				"jobError", job.Error)
			item.Status = model.RadioItemFailed
			if err := s.updateRadioItem(ctx, item, "marking failed download item failed"); err != nil {
				return nil, err
			}
			downloadFailed = true
		default:
			log.Debug(ctx, "Personal radio download is still pending",
				"sessionID", sessionID,
				"userID", userID,
				"itemID", item.ID,
				"downloadJobID", job.ID,
				"recordingMBID", item.RecordingMBID,
				"jobStatus", job.Status,
				"message", job.Message,
				"completed", job.Completed,
				"total", job.Total)
			pending = true
		}
	}

	for i := range items {
		if items[i].Status == model.RadioItemReady || items[i].Status == model.RadioItemPlayed {
			if items[i].Song == nil {
				items[i].Song, _ = s.ds.MediaFile(ctx).GetWithParticipants(items[i].MediaFileID)
			}
		}
	}
	if downloadFailed {
		s.setPlanningStatus(session.ID, model.RadioPlanningRetrying)
	}
	if session.Status == model.PersonalRadioActive {
		radioContext, contextErr := s.buildRadioContext(ctx, *session, items, request)
		if contextErr != nil {
			log.Warn(ctx, "Personal radio could not build session context",
				"sessionID", session.ID,
				"userID", userID,
				"error", contextErr)
		} else if radioOutstandingItems(items, radioContext) < totalQueueTarget || readyPlayableRadioItemsForContext(items, radioContext) < readyLowWatermark {
			s.schedulePlanForContext(context.WithoutCancel(ctx), *session, radioContext)
			pending = true
		}
	}

	status := s.getPlanningStatus(session.ID)
	if waitingForScan {
		status = model.RadioPlanningWaitingForScan
	} else if downloadFailed && (pending || s.isPlanning(session.ID)) {
		status = model.RadioPlanningRetrying
	} else if hasDownloadingItems(items) {
		status = model.RadioPlanningDownloading
	} else if pending {
		if status == "" {
			status = model.RadioPlanningSelecting
		}
	} else if !s.isPlanning(session.ID) && isPendingPlanningStatus(status) {
		status = statusForReadyItems(items)
		s.setPlanningStatus(session.ID, status)
	}
	if status == "" {
		status = model.RadioPlanningReady
	}
	log.Debug(ctx, "Personal radio refill completed",
		"sessionID", sessionID,
		"userID", userID,
		"status", status,
		"pending", pending || isPendingPlanningStatus(status),
		"waitingForScan", waitingForScan,
		"downloadFailed", downloadFailed,
		"itemCount", len(items),
		"itemStatuses", radioItemStatusCounts(items),
		"elapsed", time.Since(start))
	if refreshed, refreshErr := s.repo.GetSessionForUser(sessionID, userID); refreshErr == nil {
		session = refreshed
	}
	return newPersonalRadioResponse(*session, items, pending || isPendingPlanningStatus(status), status), nil
}

func (s *service) End(ctx context.Context, userID, sessionID string, request model.EndPersonalRadioRequest) error {
	disableAutoplay := request.DisableAutoplay
	if request.Autoplay != nil {
		disableAutoplay = !*request.Autoplay
	}
	if lookup, ok := s.repo.(model.PersonalRadioSessionLookup); ok {
		if err := lookup.EndSession(sessionID, userID, disableAutoplay); err != nil {
			return err
		}
	} else {
		session, err := s.repo.GetSessionForUser(sessionID, userID)
		if err != nil {
			return err
		}
		session.Status = model.PersonalRadioEnded
		session.Autoplay = !disableAutoplay
		if err := s.repo.UpdateSession(session); err != nil {
			return err
		}
	}
	s.setPlanningStatus(sessionID, model.RadioPlanningReady)
	return nil
}

func (s *service) Feedback(ctx context.Context, userID, sessionID string, req model.PersonalRadioFeedbackRequest) error {
	if _, err := s.repo.GetSessionForUser(sessionID, userID); err != nil {
		return err
	}
	itemForSignal, itemErr := s.repo.GetItemForUser(req.ItemID, userID)
	if itemErr != nil {
		return itemErr
	}
	trackIdentity := feedbackIdentity(*itemForSignal)
	if req.Event == model.RadioFeedbackUnplayable {
		if trackIdentity != "" {
			if err := s.repo.RecordFeedback(userID, trackIdentity, model.RadioFeedbackUnplayable, time.Now().UTC()); err != nil {
				return err
			}
		}
		itemForSignal.Status = model.RadioItemFailed
		itemForSignal.PlaybackOutcome = ""
		return s.repo.UpdateItem(itemForSignal)
	}
	if req.Event == model.RadioFeedbackDislike && trackIdentity != "" {
		if err := s.repo.RecordFeedback(userID, trackIdentity, model.RadioFeedbackDislike, time.Now().UTC()); err != nil {
			return err
		}
	}
	feedback, err := s.repo.RecordPlaybackFeedback(userID, sessionID, req, time.Now().UTC())
	if err != nil {
		return err
	}
	item := &feedback.Item
	if trackIdentity != "" && feedback.Applied {
		switch item.PlaybackOutcome {
		case model.RadioPlaybackAccepted:
			if err := s.repo.RecordFeedback(userID, trackIdentity, model.RadioFeedbackThresholdReached, time.Now().UTC()); err != nil {
				return err
			}
		case model.RadioPlaybackCompleted:
			if err := s.repo.RecordFeedback(userID, trackIdentity, model.RadioFeedbackCompleted, time.Now().UTC()); err != nil {
				return err
			}
		case model.RadioPlaybackEarlySkip:
			if err := s.repo.RecordFeedback(userID, trackIdentity, model.RadioFeedbackManualSkip, time.Now().UTC()); err != nil {
				return err
			}
		case model.RadioPlaybackLateSkip:
			if err := s.repo.RecordFeedback(userID, trackIdentity, "neutral", time.Now().UTC()); err != nil {
				return err
			}
		case model.RadioPlaybackKeep:
			if err := s.repo.RecordFeedback(userID, trackIdentity, model.RadioFeedbackKeep, time.Now().UTC()); err != nil {
				return err
			}
		}
	}
	if item.ItemType != model.RadioItemDiscovery || item.RecordingMBID == "" {
		return nil
	}
	discovery, err := s.repo.GetDiscoveryByRecording(userID, model.NormalizeRecordingMBID(item.RecordingMBID))
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	switch req.Event {
	case model.RadioFeedbackStarted:
		// Keep counting starts for the discovery lifecycle, including a second
		// start of the same item. The generic transition attempt remains
		// idempotent in the repository.
		discovery.PlayStarts++
		if discovery.PlayStarts > 1 {
			discovery.State, discovery.ExpiresAt = model.DiscoveryKept, nil
			if err := s.repo.RecordFeedback(userID, model.NormalizeRecordingMBID(item.RecordingMBID), model.RadioFeedbackKeep, now); err != nil {
				return err
			}
		}
	case model.RadioFeedbackThresholdReached, model.RadioFeedbackCompleted, model.RadioFeedbackKeep:
		if !feedback.Applied {
			return nil
		}
		discovery.State, discovery.ExpiresAt = model.DiscoveryKept, nil
	case model.RadioFeedbackManualSkip:
		if !feedback.Applied {
			return nil
		}
		if item.PlaybackOutcome == model.RadioPlaybackEarlySkip {
			discovery.State = model.DiscoveryDeletePending
			if err := s.repo.UpdateDiscovery(discovery); err != nil {
				return err
			}
			go func() {
				timer := time.NewTimer(2 * time.Second)
				defer timer.Stop()
				<-timer.C
				s.deleteDiscovery(context.WithoutCancel(ctx), *discovery)
			}()
			return nil
		}
		discovery.State, discovery.ExpiresAt = model.DiscoveryKept, nil
	case model.RadioFeedbackDislike:
		discovery.State = model.DiscoveryDeletePending
	}
	return s.repo.UpdateDiscovery(discovery)
}

func feedbackIdentity(item model.PersonalRadioItem) string {
	if mbid := model.NormalizeRecordingMBID(item.RecordingMBID); mbid != "" {
		return mbid
	}
	return model.RadioTrackKey("", item.MediaFileID)
}

func (s *service) planningSeed(ctx context.Context, session model.PersonalRadioSession) (*model.MediaFile, error) {
	items, err := s.repo.GetRecentAcceptedItems(session.ID, 1)
	if err == nil && len(items) > 0 {
		for _, item := range items {
			if item.MediaFileID == "" {
				continue
			}
			if file, getErr := s.ds.MediaFile(ctx).GetWithParticipants(item.MediaFileID); getErr == nil {
				return file, nil
			}
		}
	}
	return s.ds.MediaFile(ctx).GetWithParticipants(session.SeedMediaFileID)
}

func earlySkipThresholdMS(durationMS int64) int64 {
	if durationMS <= 0 {
		return 30000
	}
	return min(int64(30000), durationMS/5)
}

func (s *service) schedulePlan(ctx context.Context, session model.PersonalRadioSession, seed *model.MediaFile) {
	radioContext := radioContextFromSeed(seed)
	if len(session.SeedMediaFileIDs) > 1 && s.ds != nil {
		inputs := make([]radioSeedInput, 0, len(session.SeedMediaFileIDs))
		inputs = append(inputs, radioSeedInput{file: seed, weight: 0.55, role: "original"})
		for index, seedID := range session.SeedMediaFileIDs {
			if strings.TrimSpace(seedID) == "" || seedID == seed.ID {
				continue
			}
			file, err := s.ds.MediaFile(ctx).GetWithParticipants(seedID)
			if err != nil || file == nil {
				continue
			}
			weight := 0.25 / float64(index+1)
			if index < len(session.SeedMediaFileWeights) && session.SeedMediaFileWeights[index] > 0 {
				weight = session.SeedMediaFileWeights[index]
			}
			inputs = append(inputs, radioSeedInput{file: file, weight: weight, role: "playlist_seed"})
		}
		if seeds := weightedRadioSeeds(inputs); len(seeds) > 0 {
			radioContext.Seeds = seeds
		}
	}
	s.schedulePlanForContext(ctx, session, radioContext)
}

func (s *service) schedulePlanForContext(ctx context.Context, session model.PersonalRadioSession, radioContext *radioContext) {
	if radioContext == nil {
		log.Warn(ctx, "Personal radio planning skipped because session context is nil",
			"sessionID", session.ID, "userID", session.UserID)
		return
	}
	seed := radioContext.OriginalSeed
	if seed == nil && len(radioContext.Seeds) > 0 {
		seed = radioContext.Seeds[0].File
	}
	if seed == nil {
		log.Warn(ctx, "Personal radio planning skipped because session context has no seed",
			"sessionID", session.ID, "userID", session.UserID)
		return
	}
	s.planningMu.Lock()
	if s.planning == nil {
		s.planning = map[string]bool{}
	}
	if s.planningStatus == nil {
		s.planningStatus = map[string]string{}
	}
	if s.planning[session.ID] {
		s.planningMu.Unlock()
		return
	}
	s.planning[session.ID] = true
	if s.planningStatus[session.ID] != model.RadioPlanningRetrying {
		s.planningStatus[session.ID] = model.RadioPlanningSelecting
	}
	s.planningMu.Unlock()
	log.Debug(ctx, "Personal radio planning scheduled",
		"sessionID", session.ID,
		"userID", session.UserID,
		"seedID", seed.ID,
		"seedTitle", seed.Title,
		"seedArtist", seed.Artist)
	go func() {
		start := time.Now()
		defer func() {
			s.planningMu.Lock()
			delete(s.planning, session.ID)
			s.planningMu.Unlock()
			log.Debug(ctx, "Personal radio planning worker finished",
				"sessionID", session.ID,
				"userID", session.UserID,
				"elapsed", time.Since(start))
		}()
		if err := s.planWithContext(ctx, session, radioContext); err != nil {
			s.setPlanningStatus(session.ID, model.RadioPlanningError)
			log.Warn(ctx, "Unable to extend personal radio queue",
				"sessionID", session.ID,
				"userID", session.UserID,
				"seedID", seed.ID,
				"error", err)
		}
	}()
}

func (s *service) plan(ctx context.Context, session model.PersonalRadioSession, seed *model.MediaFile) error {
	return s.planWithContext(ctx, session, radioContextFromSeed(seed))
}

func (s *service) planWithContext(ctx context.Context, session model.PersonalRadioSession, radioContext *radioContext) error {
	if radioContext == nil {
		return fmt.Errorf("radio session has no context")
	}
	seed := radioContext.OriginalSeed
	if seed == nil && len(radioContext.Seeds) > 0 {
		seed = radioContext.Seeds[0].File
	}
	if seed == nil {
		return fmt.Errorf("radio session has no usable planning seed")
	}
	items, err := s.repo.GetItems(session.ID)
	if err != nil {
		return fmt.Errorf("load personal radio items: %w", err)
	}
	if s.ds != nil {
		for i := range items {
			if items[i].Song != nil || items[i].MediaFileID == "" || items[i].Status == model.RadioItemFailed {
				continue
			}
			items[i].Song, _ = s.ds.MediaFile(ctx).GetWithParticipants(items[i].MediaFileID)
		}
	}
	outstanding := radioOutstandingItems(items, radioContext)
	readyCount := readyPlayableRadioItemsForContext(items, radioContext)
	if outstanding >= totalQueueTarget && readyCount >= readyLowWatermark {
		log.Debug(ctx, "Personal radio planning skipped because queue is full",
			"sessionID", session.ID,
			"userID", session.UserID,
			"seedID", seed.ID,
			"itemCount", len(items),
			"outstanding", outstanding,
			"queueLowWatermark", queueLowWatermark)
		return nil
	}
	seen := map[string]bool{}
	seenRecordings := map[string]bool{}
	position := 0
	for _, item := range items {
		if item.MediaFileID != "" {
			seen[item.MediaFileID] = true
		}
		if item.RecordingMBID != "" {
			seenRecordings[normalizeRecordingMBID(item.RecordingMBID)] = true
		}
		position = max(position, item.Position+1)
	}

	slotsToAdd := totalQueueTarget - outstanding
	// The total target is a hard cap for planned rows. A queue can be below
	// the ready watermark while held/downloading rows occupy all ten slots;
	// wait for those rows to resolve rather than appending an unbounded second
	// batch that would make the client reconcile duplicate future items.
	if slotsToAdd < 0 {
		slotsToAdd = 0
	}
	if slotsToAdd == 0 {
		return nil
	}
	log.Debug(ctx, "Personal radio planning started",
		"sessionID", session.ID,
		"userID", session.UserID,
		"seedID", seed.ID,
		"seedTitle", seed.Title,
		"seedArtist", seed.Artist,
		"itemCount", len(items),
		"outstanding", outstanding,
		"slotsToAdd", slotsToAdd,
		"seenMediaFiles", len(seen),
		"seenRecordings", len(seenRecordings))
	pools, err := s.recommendationPoolsForContext(ctx, session, radioContext, seen, seenRecordings, slotsToAdd)
	if err != nil {
		return fmt.Errorf("build personal radio recommendation pools: %w", err)
	}
	log.Info(ctx, "Personal radio recommendation pools built",
		"sessionID", session.ID,
		"userID", session.UserID,
		"seedID", seed.ID,
		"rankedCandidates", len(pools.ranked),
		"slotsToAdd", slotsToAdd)
	if len(pools.ranked) == 0 {
		s.setPlanningStatus(session.ID, model.RadioPlanningExhausted)
		log.Warn(ctx, "Personal radio found no usable candidates",
			"sessionID", session.ID,
			"userID", session.UserID,
			"seedID", seed.ID,
			"seenMediaFiles", len(seen),
			"seenRecordings", len(seenRecordings))
		return nil
	}

	now := time.Now().UTC()
	active := activeRadioItems(items, radioContext)
	selected := composeRadioCandidates(pools.ranked, radioCompositionOptions{
		Mode:           string(session.Mode),
		Slots:          slotsToAdd,
		Active:         active,
		HasDownloading: hasDownloadingItems(items),
	})
	activeKnown, activeDiscovery := radioCompositionTypeCounts(active)
	selectedKnown, selectedDiscovery := radioCompositionTypeCountsFromCandidates(selected)
	selectedKeys := make([]string, 0, len(selected))
	selectedScores := make([]float64, 0, len(selected))
	selectedFallbackStages := map[string]int{}
	for _, candidate := range selected {
		selectedKeys = append(selectedKeys, candidate.candidate.Key)
		selectedScores = append(selectedScores, candidate.ranked.Score)
		if candidate.source != "" {
			selectedFallbackStages[candidate.source]++
		}
	}
	log.Debug(ctx, "Personal radio composition selected candidates",
		"sessionID", session.ID,
		"userID", session.UserID,
		"mode", model.NormalizeRadioMode(string(session.Mode)),
		"activeKnown", activeKnown,
		"activeDiscovery", activeDiscovery,
		"targetDiscoveryRatio", discoveryRatioForRadioMode(string(session.Mode)),
		"requestedSlots", slotsToAdd,
		"selectedKnown", selectedKnown,
		"selectedDiscovery", selectedDiscovery,
		"selectedFallbackStages", selectedFallbackStages,
		"readyLibraryBuffer", readyLibraryBufferCount(active),
		"selectedKeys", selectedKeys,
		"selectedScores", selectedScores)
	newItems := make([]model.PersonalRadioItem, 0, len(selected))
	selectedKeysSet := make(map[string]bool, len(selected))
	appendLocalItem := func(candidate rankedRadioCandidate) bool {
		if candidate.local == nil || !isPlayableLocalFile(*candidate.local) {
			return false
		}
		file := *candidate.local
		newItems = append(newItems, model.PersonalRadioItem{
			ID:            id.NewRandom(),
			SessionID:     session.ID,
			Position:      position,
			ItemType:      model.RadioItemLibrary,
			Status:        model.RadioItemReady,
			MediaFileID:   file.ID,
			RecordingMBID: file.MbzRecordingID,
			Song:          &file,
			CreatedAt:     now,
			UpdatedAt:     now,
		})
		position++
		return true
	}
	downloadsInFlight := countDownloadingRadioItems(items)
	for _, candidate := range selected {
		selectedKeysSet[candidate.candidate.Key] = true
		if candidate.isDiscovery {
			if downloadsInFlight >= maxConcurrentDownloads {
				newItems = append(newItems, heldDiscoveryItem(session, candidate.discovery, position, now))
			} else {
				item, ok := s.queueDiscovery(ctx, session, candidate.discovery, position, now)
				if !ok {
					continue
				}
				newItems = append(newItems, item)
				downloadsInFlight++
			}
			position++
			continue
		}
		appendLocalItem(candidate)
	}
	// Discovery quotas are preferences. If queueing a discovery failed, or a
	// selected local file disappeared between scanning and append, consume the
	// next ready local candidate in rank order before giving up on this refill.
	if len(newItems) < slotsToAdd {
		for _, candidate := range pools.ranked {
			if len(newItems) >= slotsToAdd || candidate.isDiscovery || selectedKeysSet[candidate.candidate.Key] {
				continue
			}
			if appendLocalItem(candidate) {
				selectedKeysSet[candidate.candidate.Key] = true
			}
		}
	}
	if len(newItems) == 0 {
		s.setPlanningStatus(session.ID, model.RadioPlanningError)
		log.Warn(ctx, "Personal radio could not queue any planned candidates",
			"sessionID", session.ID,
			"userID", session.UserID,
			"seedID", seed.ID,
			"rankedCandidates", len(pools.ranked),
			"slotsToAdd", slotsToAdd)
		return nil
	}
	if err := s.repo.AppendItems(session.ID, newItems); err != nil {
		return fmt.Errorf("append personal radio items: %w", err)
	}
	localItems, discoveryItems := 0, 0
	for _, item := range newItems {
		if item.ItemType == model.RadioItemDiscovery {
			discoveryItems++
		} else if item.ItemType == model.RadioItemLibrary {
			localItems++
		}
	}
	log.Info(ctx, "Personal radio plan appended",
		"sessionID", session.ID,
		"userID", session.UserID,
		"seedID", seed.ID,
		"itemsAdded", len(newItems),
		"libraryItems", localItems,
		"discoveryItems", discoveryItems,
		"itemPositions", plannedPositions(newItems))
	if hasDownloadingItems(newItems) || hasHeldDiscoveryItems(newItems) {
		s.setPlanningStatus(session.ID, model.RadioPlanningDownloading)
	} else {
		s.setPlanningStatus(session.ID, model.RadioPlanningReady)
	}
	return nil
}

type candidatePools struct {
	local      model.MediaFiles
	discovery  []agents.Song
	ranked     []rankedRadioCandidate
	candidates []rankedRadioCandidate
	fatigue    map[string]float64
}

type rankedRadioCandidate struct {
	candidate   recommendations.Candidate
	ranked      recommendations.RankedCandidate
	local       *model.MediaFile
	discovery   agents.Song
	isDiscovery bool
	injected    bool
	source      string
}

func (s *service) recommendationPools(ctx context.Context, session model.PersonalRadioSession, seed *model.MediaFile, seen map[string]bool, seenRecordings map[string]bool, count int) (candidatePools, error) {
	return s.recommendationPoolsWithLimit(ctx, session, seed, seen, seenRecordings, count, discoveryCandidateLimit, 1)
}

func (s *service) recommendationPoolsWithLimit(ctx context.Context, session model.PersonalRadioSession, seed *model.MediaFile, seen map[string]bool, seenRecordings map[string]bool, count, providerLimit int, seedWeight float64) (candidatePools, error) {
	providerCtx, cancel := context.WithTimeout(ctx, providerPlanningTimeout)
	defer cancel()
	return s.recommendationPoolsWithLimitContext(ctx, providerCtx, session, seed, seen, seenRecordings, count, providerLimit, seedWeight, true)
}

func (s *service) recommendationPoolsWithLimitContext(ctx, providerCtx context.Context, session model.PersonalRadioSession, seed *model.MediaFile, seen map[string]bool, seenRecordings map[string]bool, count, providerLimit int, seedWeight float64, loadFeedback bool) (candidatePools, error) {
	var pools candidatePools
	localAdded := map[string]bool{}
	localAddedKeys := map[string]bool{}
	localAddedRecordings := make(map[string]bool, len(seenRecordings))
	normalizedSeenRecordings := make(map[string]bool, len(seenRecordings))
	for recordingMBID := range seenRecordings {
		if normalized := normalizeRecordingMBID(recordingMBID); normalized != "" {
			normalizedSeenRecordings[normalized] = true
			localAddedRecordings[normalized] = true
		}
	}
	seenRecordings = normalizedSeenRecordings
	rankedCandidates := make([]rankedRadioCandidate, 0, discoveryCandidateLimit+count)
	fatigue := map[string]float64{}
	now := time.Now().UTC()
	stats := map[string]int{}
	if s.agents == nil || s.matcher == nil {
		log.Warn(ctx, "Personal radio external recommendation path is unavailable",
			"sessionID", session.ID,
			"userID", session.UserID,
			"seedID", seed.ID,
			"agentsConfigured", s.agents != nil,
			"matcherConfigured", s.matcher != nil)
	}
	if s.agents != nil && s.matcher != nil {
		providerRecommendations, recErr := s.agents.GetSimilarSongsByTrackAll(providerCtx, seed.ID, seed.Title, seed.Artist, seed.MbzRecordingID, providerLimit)
		if recErr != nil {
			log.Debug(ctx, "Personal radio similarity provider returned no candidates",
				"sessionID", session.ID,
				"userID", session.UserID,
				"seedID", seed.ID,
				"error", recErr)
		} else if len(providerRecommendations) > 0 {
			stats["providerCandidates"] = len(providerRecommendations)
			matches, matchErr := s.matcher.MatchSongsIndexed(providerCtx, providerRecommendations)
			if matchErr != nil {
				log.Warn(ctx, "Unable to compare personal radio recommendations with the library",
					"sessionID", session.ID,
					"userID", session.UserID,
					"seedID", seed.ID,
					"candidateCount", len(providerRecommendations),
					"error", matchErr)
			} else {
				for i, song := range providerRecommendations {
					recordingMBID := normalizeRecordingMBID(song.MBID)
					song.MBID = recordingMBID
					candidateFields := []any{
						"sessionID", session.ID,
						"userID", session.UserID,
						"seedID", seed.ID,
						"candidateIndex", i,
						"candidateID", song.ID,
						"candidateMBID", song.MBID,
						"candidateTitle", song.Name,
						"candidateAlbum", song.Album,
					}
					if local, ok := matches[i]; ok {
						localKey := radioMediaFileCandidateKey(local)
						localRecordingMBID := model.NormalizeRecordingMBID(local.MbzRecordingID)
						if !local.Missing && local.ID != "" && !seen[local.ID] &&
							(localRecordingMBID == "" || !localAddedRecordings[localRecordingMBID]) &&
							!localAdded[local.ID] && !localAddedKeys[localKey] {
							localAdded[local.ID] = true
							localAddedKeys[localKey] = true
							if localRecordingMBID != "" {
								localAddedRecordings[localRecordingMBID] = true
							}
							key := localKey
							localCopy := local
							rankedCandidates = append(rankedCandidates, rankedRadioCandidate{
								candidate: recommendations.Candidate{
									Key:              key,
									SeedAffinity:     localSeedAffinity(seed, local),
									SessionAffinity:  seedWeight * localSeedAffinity(seed, local),
									MediaFile:        local,
									SimilarityScores: song.SimilarityScores,
								},
								local: &localCopy,
							})
							stats["matchedLocal"]++
							traceRadioCandidate(ctx, "Personal radio candidate accepted from library",
								append(candidateFields,
									"decision", "library",
									"mediaFileID", local.ID,
									"matchedTitle", local.Title,
									"matchedArtist", local.Artist))
						} else if local.Missing {
							stats["matchedMissing"]++
							traceRadioCandidate(ctx, "Personal radio candidate rejected because matched library file is missing",
								append(candidateFields, "decision", "matched_missing", "mediaFileID", local.ID))
						} else if seen[local.ID] {
							stats["matchedSeen"]++
							traceRadioCandidate(ctx, "Personal radio candidate rejected because library file was already seen",
								append(candidateFields, "decision", "matched_seen", "mediaFileID", local.ID))
						} else {
							stats["matchedDuplicate"]++
							traceRadioCandidate(ctx, "Personal radio candidate rejected because library file was already added to this plan",
								append(candidateFields, "decision", "matched_duplicate", "mediaFileID", local.ID))
						}
						continue
					}
					if recordingMBID == "" || seenRecordings[recordingMBID] {
						if recordingMBID == "" {
							stats["missingMBID"]++
							traceRadioCandidate(ctx, "Personal radio candidate rejected because it has no recording MBID",
								append(candidateFields, "decision", "missing_mbid"))
						} else {
							stats["seenRecording"]++
							traceRadioCandidate(ctx, "Personal radio candidate rejected because recording was already seen",
								append(candidateFields, "decision", "seen_recording"))
						}
						continue
					}
					key := radioDiscoveryCandidateKey(song)
					rankedCandidates = append(rankedCandidates, rankedRadioCandidate{
						candidate: recommendations.Candidate{
							Key:             key,
							SessionAffinity: seedWeight,
							MediaFile: model.MediaFile{
								ID:             key,
								Title:          song.Name,
								Artist:         firstSongArtist(song),
								Album:          song.Album,
								MbzRecordingID: song.MBID,
							},
							SimilarityScores: song.SimilarityScores,
						},
						discovery:   song,
						isDiscovery: true,
					})
					stats["acceptedDiscovery"]++
					traceRadioCandidate(ctx, "Personal radio candidate accepted for discovery download",
						append(candidateFields, "decision", "discovery"))
				}
			}
		} else {
			log.Debug(ctx, "Personal radio similarity provider returned an empty candidate list",
				"sessionID", session.ID,
				"userID", session.UserID,
				"seedID", seed.ID)
		}
	}

	metadataSeen := make(map[string]bool, len(seen)+len(localAdded))
	for mediaFileID := range seen {
		metadataSeen[mediaFileID] = true
	}
	for mediaFileID := range localAdded {
		metadataSeen[mediaFileID] = true
	}
	appendLocalFallback := func(files model.MediaFiles, stage string) {
		for _, file := range files {
			key := radioMediaFileCandidateKey(file)
			recordingMBID := normalizeRecordingMBID(file.MbzRecordingID)
			if localAdded[file.ID] || localAddedKeys[key] || (recordingMBID != "" && localAddedRecordings[recordingMBID]) {
				continue
			}
			localAdded[file.ID] = true
			localAddedKeys[key] = true
			if recordingMBID != "" {
				localAddedRecordings[recordingMBID] = true
			}
			fileCopy := file
			rankedCandidates = append(rankedCandidates, rankedRadioCandidate{
				candidate: recommendations.Candidate{
					Key:             key,
					SeedAffinity:    localSeedAffinity(seed, file),
					SessionAffinity: seedWeight * localSeedAffinity(seed, file),
					MediaFile:       file,
				},
				local:  &fileCopy,
				source: stage,
			})
			stats[stage]++
		}
	}
	tasteFallback, tasteStats, err := s.boundedLocalCandidateFiles(ctx, seed, metadataSeen, localAddedRecordings, true)
	if err != nil {
		return candidatePools{}, fmt.Errorf("load local fallback candidates: %w", err)
	}
	stats["tasteFallbackPages"] += tasteStats.pages
	stats["tasteFallbackScanned"] += tasteStats.scanned
	stats["tasteFallbackRejected"] += tasteStats.rejected
	appendLocalFallback(tasteFallback, "tasteFallback")
	if len(localAdded) < count {
		broadFallback, broadStats, broadErr := s.boundedLocalCandidateFiles(ctx, seed, metadataSeen, localAddedRecordings, false)
		if broadErr != nil {
			return candidatePools{}, fmt.Errorf("load bounded local fallback candidates: %w", broadErr)
		}
		stats["exhaustiveFallbackPages"] += broadStats.pages
		stats["exhaustiveFallbackScanned"] += broadStats.scanned
		stats["exhaustiveFallbackRejected"] += broadStats.rejected
		appendLocalFallback(broadFallback, "exhaustiveFallback")
	}

	transitionSourceKey := model.RadioTrackKey(seed.MbzRecordingID, seed.ID)
	var learnedTransitions []model.RadioTransitionFeedback
	if transitionSourceKey != "" {
		learnedTransitions, err = s.repo.GetTopTransitions(session.UserID, transitionSourceKey, transitionCandidateLimit)
		if err != nil {
			log.Warn(ctx, "Personal radio could not load learned transitions",
				"sessionID", session.ID, "userID", session.UserID,
				"sourceKey", transitionSourceKey, "error", err)
			learnedTransitions = nil
		}
	}

	candidateKeys := make(map[string]bool, len(rankedCandidates))
	for _, candidate := range rankedCandidates {
		candidateKeys[candidate.candidate.Key] = true
	}
	for _, transition := range learnedTransitions {
		if !strongTransition(transition) || transition.TargetKey == "" || candidateKeys[transition.TargetKey] {
			continue
		}
		if file, ok := s.resolveTransitionMediaFile(ctx, transition); ok {
			recordingMBID := normalizeRecordingMBID(file.MbzRecordingID)
			if file.Missing || seen[file.ID] || (recordingMBID != "" && localAddedRecordings[recordingMBID]) {
				continue
			}
			fileCopy := file
			rankedCandidates = append(rankedCandidates, rankedRadioCandidate{
				candidate: recommendations.Candidate{
					Key:             transition.TargetKey,
					SessionAffinity: seedWeight,
					MediaFile:       file,
				},
				local:    &fileCopy,
				injected: true,
				source:   "learned_transition_local",
			})
			localAdded[file.ID] = true
			localAddedKeys[transition.TargetKey] = true
			if recordingMBID != "" {
				localAddedRecordings[recordingMBID] = true
			}
			candidateKeys[transition.TargetKey] = true
			stats["learnedLocal"]++
			continue
		}
		if mbid := strings.TrimPrefix(transition.TargetKey, "mbid:"); mbid != "" && strings.HasPrefix(transition.TargetKey, "mbid:") {
			rankedCandidates = append(rankedCandidates, rankedRadioCandidate{
				candidate: recommendations.Candidate{
					Key:             transition.TargetKey,
					SessionAffinity: seedWeight,
					MediaFile: model.MediaFile{
						ID:             transition.TargetKey,
						MbzRecordingID: mbid,
					},
				},
				discovery:   agents.Song{MBID: mbid, CandidateID: transition.TargetKey},
				isDiscovery: true,
				injected:    true,
				source:      "learned_transition_discovery",
			})
			candidateKeys[transition.TargetKey] = true
			stats["learnedDiscovery"]++
		}
	}

	transitionKeys := make([]string, 0, len(rankedCandidates))
	for _, candidate := range rankedCandidates {
		if key := model.RadioTrackKey(candidate.candidate.MbzRecordingID, candidate.candidate.MediaFile.ID); key != "" {
			transitionKeys = append(transitionKeys, key)
		}
	}
	transitionFeedback := map[string]model.RadioTransitionFeedback{}
	if transitionSourceKey != "" {
		transitionFeedback, err = s.repo.GetTransitionsForTargets(session.UserID, transitionSourceKey, transitionKeys)
		if err != nil {
			log.Warn(ctx, "Personal radio could not load transition feedback",
				"sessionID", session.ID, "userID", session.UserID,
				"sourceKey", transitionSourceKey, "targetCount", len(transitionKeys), "error", err)
			transitionFeedback = map[string]model.RadioTransitionFeedback{}
		}
	}
	filteredCandidates := make([]rankedRadioCandidate, 0, len(rankedCandidates))
	for _, candidate := range rankedCandidates {
		targetKey := model.RadioTrackKey(candidate.candidate.MbzRecordingID, candidate.candidate.MediaFile.ID)
		if targetKey == "" {
			targetKey = candidate.candidate.Key
		}
		if feedbackForTarget, ok := transitionFeedback[targetKey]; ok {
			if suppressTransition(feedbackForTarget) {
				stats["transitionSuppressed"]++
				// A poor transition is a ranking penalty, not a permanent
				// exclusion. Keeping it in the pool lets local fallback fill a
				// short queue after all stronger candidates are exhausted.
				candidate.candidate.TransitionAffinity = -1
			} else {
				candidate.candidate.TransitionAffinity = recommendations.TransitionAffinity(feedbackForTarget, now)
			}
		}
		filteredCandidates = append(filteredCandidates, candidate)
	}
	rankedCandidates = filteredCandidates
	s.applyTasteAffinities(session.UserID, rankedCandidates)
	if loadFeedback {
		suppressed := s.applyRadioFeedbackFatigue(ctx, session, rankedCandidates, fatigue)
		if len(suppressed) > 0 {
			filtered := rankedCandidates[:0]
			for _, candidate := range rankedCandidates {
				key := model.RadioTrackKey(candidate.candidate.MbzRecordingID, candidate.candidate.MediaFile.ID)
				if key == "" {
					key = candidate.candidate.Key
				}
				if !suppressed[key] && !suppressed[candidate.candidate.Key] {
					filtered = append(filtered, candidate)
				}
			}
			rankedCandidates = filtered
		}
	}
	s.applyLocalFallbackFeatures(rankedCandidates, []radioSeed{{File: seed, Weight: 1}}, session, now, fatigue)
	pools.candidates = append(pools.candidates, rankedCandidates...)
	pools.fatigue = fatigue
	ranked := make([]recommendations.Candidate, 0, len(rankedCandidates))
	sources := make(map[string]rankedRadioCandidate, len(rankedCandidates))
	for _, candidate := range rankedCandidates {
		ranked = append(ranked, candidate.candidate)
		sources[candidate.candidate.Key] = candidate
	}
	for _, candidate := range recommendations.Rank(ranked, recommendations.Options{
		Now:     now,
		Fatigue: fatigue,
	}) {
		source, ok := sources[candidate.Key]
		if !ok {
			continue
		}
		source.ranked = candidate
		pools.ranked = append(pools.ranked, source)
		transitionKey := model.RadioTrackKey(candidate.MbzRecordingID, candidate.MediaFile.ID)
		transition := transitionFeedback[transitionKey]
		traceRadioCandidate(ctx, "Personal radio candidate ranked", []any{
			"sessionID", session.ID,
			"userID", session.UserID,
			"sourceContextKey", transitionSourceKey,
			"targetKey", transitionKey,
			"attemptCount", transition.AttemptCount,
			"acceptedCount", transition.AcceptedCount,
			"completedCount", transition.CompletedCount,
			"earlySkipCount", transition.EarlySkipCount,
			"neutralSkipCount", transition.NeutralSkipCount,
			"transitionAffinity", candidate.TransitionAffinity,
			"transitionScoreContribution", candidate.Breakdown.TransitionAffinity,
			"finalScore", candidate.Score,
			"candidateSource", radioCandidateSource(source),
		})
		if source.isDiscovery {
			pools.discovery = append(pools.discovery, source.discovery)
		} else if source.local != nil {
			pools.local = append(pools.local, *source.local)
		}
	}
	log.Debug(ctx, "Personal radio candidate filtering completed",
		"sessionID", session.ID,
		"userID", session.UserID,
		"seedID", seed.ID,
		"candidateStats", stats,
		"localPool", len(pools.local),
		"discoveryPool", len(pools.discovery),
		"fallbackRequested", count)
	return pools, nil
}

func (s *service) applyTasteAffinities(userID string, candidates []rankedRadioCandidate) {
	taste, ok := s.repo.(recommendations.TasteAffinityRepository)
	if !ok || len(candidates) == 0 {
		return
	}
	identities := make([]recommendations.TasteCandidateIdentity, 0, len(candidates))
	for _, candidate := range candidates {
		identities = append(identities, recommendations.TasteIdentityForMediaFile(candidate.candidate.Key, candidate.candidate.MediaFile))
	}
	affinities, err := taste.AffinityForCandidates(userID, identities)
	if err != nil {
		log.Debug("Personal radio taste affinity unavailable", "userID", userID, "error", err)
		return
	}
	for i := range candidates {
		value := affinities[candidates[i].candidate.Key]
		candidates[i].candidate.TasteAffinity = value.Score
		candidates[i].candidate.TasteDetails = value
	}
}

func (s *service) applyRadioFeedbackFatigue(ctx context.Context, session model.PersonalRadioSession, candidates []rankedRadioCandidate, fatigue map[string]float64) map[string]bool {
	suppressed := map[string]bool{}
	if s.repo == nil || len(candidates) == 0 {
		return suppressed
	}
	identitySet := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		if identity := model.RadioTrackKey(candidate.candidate.MediaFile.MbzRecordingID, candidate.candidate.MediaFile.ID); identity != "" {
			identitySet[identity] = true
		}
	}
	if len(identitySet) == 0 {
		return suppressed
	}
	identities := make([]string, 0, len(identitySet))
	for identity := range identitySet {
		identities = append(identities, identity)
	}
	feedback, err := s.loadRadioTrackFeedback(session.UserID, identities)
	if err != nil {
		log.Warn(ctx, "Personal radio could not load local candidate feedback",
			"sessionID", session.ID, "userID", session.UserID, "trackCount", len(identities), "error", err)
	}
	now := time.Now().UTC()
	for _, candidate := range candidates {
		identity := model.RadioTrackKey(candidate.candidate.MediaFile.MbzRecordingID, candidate.candidate.MediaFile.ID)
		value := radioFeedbackFatigue(feedback[identity])
		if value == 0 && candidate.candidate.MediaFile.MbzRecordingID != "" {
			value = radioFeedbackFatigue(feedback[normalizeRecordingMBID(candidate.candidate.MediaFile.MbzRecordingID)])
		}
		if f := feedback[identity]; f.SuppressedUntil != nil && f.SuppressedUntil.After(now) {
			suppressed[identity] = true
			suppressed[candidate.candidate.Key] = true
			continue
		}
		if value <= 0 {
			continue
		}
		if existing := fatigue[candidate.candidate.Key]; value > existing {
			fatigue[candidate.candidate.Key] = value
		}
		if candidate.candidate.MediaFile.ID != "" {
			if existing := fatigue[candidate.candidate.MediaFile.ID]; value > existing {
				fatigue[candidate.candidate.MediaFile.ID] = value
			}
		}
	}
	return suppressed
}

func (s *service) loadRadioTrackFeedback(userID string, recordingMBIDs []string) (map[string]model.RadioTrackFeedback, error) {
	result := map[string]model.RadioTrackFeedback{}
	if s.repo == nil || len(recordingMBIDs) == 0 {
		return result, nil
	}
	unique := make(map[string]bool, len(recordingMBIDs))
	for _, recordingMBID := range recordingMBIDs {
		if normalized := normalizeRecordingMBID(recordingMBID); normalized != "" {
			unique[normalized] = true
		}
	}
	mbids := make([]string, 0, len(unique))
	for recordingMBID := range unique {
		mbids = append(mbids, recordingMBID)
	}
	for start := 0; start < len(mbids); start += radioFeedbackBatchSize {
		end := min(start+radioFeedbackBatchSize, len(mbids))
		feedback, err := s.repo.GetFeedback(userID, mbids[start:end])
		if err != nil {
			return result, err
		}
		for recordingMBID, value := range feedback {
			result[recordingMBID] = value
			if value.TrackKey != "" {
				result[value.TrackKey] = value
			}
			if canonical := model.RadioTrackKey(recordingMBID, ""); canonical != "" {
				result[canonical] = value
			}
		}
	}
	return result, nil
}

func (s *service) applyLocalFallbackFeatures(candidates []rankedRadioCandidate, seeds []radioSeed, session model.PersonalRadioSession, now time.Time, fatigue map[string]float64) {
	for i := range candidates {
		if !isLocalFallbackSource(candidates[i].source) || candidates[i].local == nil {
			continue
		}
		candidateFatigue := fatigue[candidates[i].candidate.Key]
		if value := fatigue[candidates[i].candidate.MediaFile.ID]; value > candidateFatigue {
			candidateFatigue = value
		}
		features := buildLocalFallbackFeatures(candidates[i].candidate, seeds, string(session.Mode), now, candidateFatigue)
		candidates[i].candidate.LocalFallback = &features
	}
}

func (s *service) recommendationPoolsForContext(ctx context.Context, session model.PersonalRadioSession, radioContext *radioContext, seen map[string]bool, seenRecordings map[string]bool, count int) (candidatePools, error) {
	if radioContext == nil || len(radioContext.Seeds) == 0 {
		return candidatePools{}, fmt.Errorf("radio context has no seeds")
	}
	var all []rankedRadioCandidate
	fatigue := map[string]float64{}
	workingSeen := cloneRadioBoolMap(seen)
	workingRecordings := cloneRadioBoolMap(seenRecordings)
	providerCtx, cancelProvider := context.WithTimeout(ctx, providerPlanningTimeout)
	defer cancelProvider()
	for _, seed := range radioContext.Seeds {
		if seed.File == nil {
			continue
		}
		seedPools, err := s.recommendationPoolsWithLimitContext(ctx, providerCtx, session, seed.File, workingSeen, workingRecordings, count, providerLimitForRadioSeed(seed), seed.Weight, false)
		if err != nil {
			return candidatePools{}, err
		}
		all = append(all, seedPools.candidates...)
		for key, value := range seedPools.fatigue {
			if value > fatigue[key] {
				fatigue[key] = value
			}
		}
	}
	if len(all) == 0 {
		return candidatePools{fatigue: fatigue}, nil
	}
	result := candidatePools{candidates: all, fatigue: fatigue}
	now := time.Now().UTC()
	suppressed := s.applyRadioFeedbackFatigue(ctx, session, all, fatigue)
	if len(suppressed) > 0 {
		filtered := all[:0]
		for _, candidate := range all {
			key := model.RadioTrackKey(candidate.candidate.MediaFile.MbzRecordingID, candidate.candidate.MediaFile.ID)
			if key == "" {
				key = candidate.candidate.Key
			}
			if !suppressed[key] && !suppressed[candidate.candidate.Key] {
				filtered = append(filtered, candidate)
			}
		}
		all = filtered
		result.candidates = filtered
	}
	s.applyLocalFallbackFeatures(all, radioContext.Seeds, session, now, fatigue)
	ranked := make([]recommendations.Candidate, 0, len(all))
	sources := make(map[string]rankedRadioCandidate, len(all))
	for _, candidate := range all {
		key := candidate.candidate.Key
		if key == "" {
			continue
		}
		ranked = append(ranked, candidate.candidate)
		if existing, ok := sources[key]; !ok || (existing.isDiscovery && !candidate.isDiscovery) {
			sources[key] = candidate
		}
	}
	for _, candidate := range recommendations.Rank(ranked, recommendations.Options{Now: now, Fatigue: fatigue}) {
		source, ok := sources[candidate.Key]
		if !ok {
			continue
		}
		source.ranked = candidate
		result.ranked = append(result.ranked, source)
		if source.isDiscovery {
			result.discovery = append(result.discovery, source.discovery)
		} else if source.local != nil {
			result.local = append(result.local, *source.local)
		}
	}
	return result, nil
}

func providerLimitForRadioSeed(seed radioSeed) int {
	switch seed.Role {
	case "accepted_recent_1":
		return 30
	case "accepted_recent_2":
		return 20
	case "accepted_recent_3":
		return 15
	default:
		return discoveryCandidateLimit
	}
}

func cloneRadioBoolMap(values map[string]bool) map[string]bool {
	clone := make(map[string]bool, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func radioLocalCandidateKey(mediaFileID string) string {
	return model.RadioTrackKey("", mediaFileID)
}

func radioMediaFileCandidateKey(file model.MediaFile) string {
	return model.RadioTrackKey(file.MbzRecordingID, file.ID)
}

func normalizeRecordingMBID(value string) string {
	return model.NormalizeRecordingMBID(value)
}

func radioDiscoveryCandidateKey(song agents.Song) string {
	return model.RadioTrackKey(song.MBID, "")
}

func radioCandidateSource(candidate rankedRadioCandidate) string {
	if candidate.isDiscovery {
		if candidate.injected {
			return "learned_transition_injection"
		}
		return "external_provider"
	}
	if candidate.injected {
		return "learned_transition_injection"
	}
	if candidate.local != nil {
		if len(candidate.candidate.SimilarityScores) > 0 {
			return "external_provider_or_local_match"
		}
		return "local_fallback"
	}
	return "unknown"
}

func radioFeedbackFatigue(feedback model.RadioTrackFeedback) float64 {
	negativeFeedback := feedback.EarlySkipCount + feedback.NeutralSkipCount
	if negativeFeedback <= 0 {
		return 0
	}
	return min(1, float64(negativeFeedback)/5)
}

func strongTransition(feedback model.RadioTransitionFeedback) bool {
	if feedback.AttemptCount < 3 || feedback.EarlySkipCount >= 2 {
		return false
	}
	positive := feedback.AcceptedCount + feedback.CompletedCount + feedback.KeepCount
	return float64(positive)/float64(feedback.AttemptCount) >= 0.75
}

func suppressTransition(feedback model.RadioTransitionFeedback) bool {
	if feedback.AttemptCount < 3 || feedback.EarlySkipCount < 2 {
		return false
	}
	positive := feedback.AcceptedCount + feedback.CompletedCount + feedback.KeepCount
	return float64(positive)/float64(feedback.AttemptCount) < 0.20
}

func (s *service) resolveTransitionMediaFile(ctx context.Context, transition model.RadioTransitionFeedback) (model.MediaFile, bool) {
	if transition.TargetMediaFileID != "" {
		file, err := s.ds.MediaFile(ctx).GetWithParticipants(transition.TargetMediaFileID)
		if err == nil && file != nil && !file.Missing {
			return *file, true
		}
	}
	if !strings.HasPrefix(transition.TargetKey, "mbid:") {
		return model.MediaFile{}, false
	}
	mbid := strings.TrimPrefix(transition.TargetKey, "mbid:")
	files, err := s.ds.MediaFile(ctx).GetAll(model.QueryOptions{
		Filters: squirrel.Eq{"mbz_recording_id": mbid},
		Max:     1,
	})
	if err != nil || len(files) == 0 || files[0].Missing {
		return model.MediaFile{}, false
	}
	return files[0], true
}

func (s *service) queueDiscovery(ctx context.Context, session model.PersonalRadioSession, discovery agents.Song, position int, now time.Time) (model.PersonalRadioItem, bool) {
	item := model.PersonalRadioItem{
		ID:            id.NewRandom(),
		SessionID:     session.ID,
		Position:      position,
		ItemType:      model.RadioItemDiscovery,
		Status:        model.RadioItemDownloading,
		RecordingMBID: discovery.MBID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if s.music == nil {
		log.Warn(ctx, "Personal radio download service is unavailable",
			"sessionID", session.ID,
			"userID", session.UserID,
			"itemID", item.ID,
			"position", position,
			"recordingMBID", discovery.MBID,
			"title", discovery.Name)
		return model.PersonalRadioItem{}, false
	}
	job, err := s.music.CreateDownload(ctx, session.UserID, model.ExternalDownloadRequest{
		Kind:        model.MusicDownloadSong,
		ID:          discovery.MBID,
		Origin:      model.MusicDownloadOriginRadio,
		Priority:    100,
		RadioItemID: item.ID,
		Title:       discovery.Name,
		Artist:      firstSongArtist(discovery),
		Album:       discovery.Album,
	})
	if err != nil {
		log.Warn(ctx, "Unable to queue personal radio discovery",
			"sessionID", session.ID,
			"userID", session.UserID,
			"itemID", item.ID,
			"position", position,
			"recordingMBID", discovery.MBID,
			"title", discovery.Name,
			"artist", firstSongArtist(discovery),
			"error", err)
		return model.PersonalRadioItem{}, false
	}
	if job == nil || job.ID == "" {
		log.Warn(ctx, "Personal radio download service returned an empty job",
			"sessionID", session.ID,
			"userID", session.UserID,
			"itemID", item.ID,
			"position", position,
			"recordingMBID", discovery.MBID,
			"title", discovery.Name,
			"artist", firstSongArtist(discovery))
		return model.PersonalRadioItem{}, false
	}
	item.DownloadJobID = job.ID
	log.Info(ctx, "Personal radio discovery download queued",
		"sessionID", session.ID,
		"userID", session.UserID,
		"itemID", item.ID,
		"position", position,
		"downloadJobID", job.ID,
		"recordingMBID", discovery.MBID,
		"title", discovery.Name,
		"artist", firstSongArtist(discovery),
		"album", discovery.Album)
	return item, true
}

func heldDiscoveryItem(session model.PersonalRadioSession, discovery agents.Song, position int, now time.Time) model.PersonalRadioItem {
	return model.PersonalRadioItem{
		ID: id.NewRandom(), SessionID: session.ID, Position: position,
		ItemType: model.RadioItemDiscovery, Status: model.RadioItemHeld,
		RecordingMBID: discovery.MBID,
		Song:          &model.MediaFile{Title: discovery.Name, Artist: firstSongArtist(discovery), Album: discovery.Album},
		CreatedAt:     now, UpdatedAt: now,
	}
}

func (s *service) promoteHeldDiscoveries(ctx context.Context, session *model.PersonalRadioSession, items []model.PersonalRadioItem) error {
	if s.music == nil || session == nil {
		return nil
	}
	inFlight := countDownloadingRadioItems(items)
	for index := range items {
		if inFlight >= maxConcurrentDownloads {
			break
		}
		item := &items[index]
		if item.ItemType != model.RadioItemDiscovery || item.Status != model.RadioItemHeld || item.RecordingMBID == "" {
			continue
		}
		job, err := s.music.CreateDownload(ctx, session.UserID, model.ExternalDownloadRequest{
			Kind: model.MusicDownloadSong, ID: item.RecordingMBID,
			Origin: model.MusicDownloadOriginRadio, Priority: 100,
			RadioItemID: item.ID, Title: mediaFileTitle(item.Song),
			Artist: mediaFileArtist(item.Song), Album: mediaFileAlbum(item.Song),
		})
		if err != nil || job == nil || job.ID == "" {
			item.Status = model.RadioItemFailed
			if updateErr := s.updateRadioItem(ctx, item, "marking held discovery failed"); updateErr != nil {
				return updateErr
			}
			continue
		}
		item.Status = model.RadioItemDownloading
		item.DownloadJobID = job.ID
		if err := s.updateRadioItem(ctx, item, "promoting held discovery"); err != nil {
			return err
		}
		inFlight++
	}
	return nil
}

func mediaFileTitle(file *model.MediaFile) string {
	if file == nil {
		return ""
	}
	return file.Title
}

func mediaFileArtist(file *model.MediaFile) string {
	if file == nil {
		return ""
	}
	return file.Artist
}

func mediaFileAlbum(file *model.MediaFile) string {
	if file == nil {
		return ""
	}
	return file.Album
}

func (s *service) updateRadioItem(ctx context.Context, item *model.PersonalRadioItem, reason string) error {
	if err := s.repo.UpdateItem(item); err != nil {
		return fmt.Errorf("%s for session item %s: %w", reason, item.ID, err)
	}
	return nil
}

func (s *service) resolveDownloadedItem(ctx context.Context, item *model.PersonalRadioItem, job *model.MusicDownloadJob) (model.MediaFile, bool, error) {
	song := agents.Song{
		Name:  job.Title,
		MBID:  item.RecordingMBID,
		Album: job.Album,
	}
	if job.Artist != "" {
		song.Artists = []agents.Artist{{Name: job.Artist}}
	}
	matched, err := s.matcher.MatchSongsIndexed(ctx, []agents.Song{song})
	if err != nil {
		return model.MediaFile{}, false, fmt.Errorf("match imported recording %q: %w", item.RecordingMBID, err)
	}
	file, ok := matched[0]
	if !ok {
		return model.MediaFile{}, false, nil
	}
	return file, true, nil
}

func firstSongArtist(song agents.Song) string {
	if len(song.Artists) == 0 {
		return ""
	}
	return song.Artists[0].Name
}

func traceRadioCandidate(ctx context.Context, message string, fields []any) {
	log.Trace(append([]any{ctx, message}, fields...)...)
}

func (s *service) localCandidates(ctx context.Context, seed *model.MediaFile, seen map[string]bool, count int) (model.MediaFiles, error) {
	if count <= 0 {
		return nil, nil
	}
	files, err := s.localCandidateFiles(ctx, seed, seen)
	if err != nil {
		return nil, err
	}
	candidates := make([]recommendations.Candidate, 0, len(files))
	for _, file := range files {
		candidates = append(candidates, recommendations.Candidate{
			Key:          radioMediaFileCandidateKey(file),
			SeedAffinity: localSeedAffinity(seed, file),
			MediaFile:    file,
		})
	}
	ranked := recommendations.Rank(candidates, recommendations.Options{
		Now:   time.Now().UTC(),
		Limit: count,
	})
	result := make(model.MediaFiles, 0, len(ranked))
	for _, candidate := range ranked {
		result = append(result, candidate.MediaFile)
	}
	return result, nil
}

func (s *service) localCandidateFiles(ctx context.Context, seed *model.MediaFile, seen map[string]bool) (model.MediaFiles, error) {
	candidates, _, err := s.localCandidateFilesForFallback(ctx, seed, seen, nil, true)
	return candidates, err
}

type localFallbackStats struct {
	pages    int
	scanned  int
	rejected int
}

// boundedLocalCandidateFiles loads one reusable metadata pool per planning
// batch. Contextual seeds can then filter/rank the same pool without walking
// the library repeatedly. Filesystem validation is intentionally deferred to
// appendLocalItem, where only the shortlist entering the queue is checked.
func (s *service) boundedLocalCandidateFiles(ctx context.Context, seed *model.MediaFile, seen, seenRecordings map[string]bool, requireAffinity bool) (model.MediaFiles, localFallbackStats, error) {
	if s.ds == nil {
		return nil, localFallbackStats{}, fmt.Errorf("media datastore is unavailable")
	}
	s.fallbackMu.Lock()
	pool := append(model.MediaFiles(nil), s.fallbackPool...)
	loaded := !s.fallbackLoaded.IsZero()
	s.fallbackMu.Unlock()
	stats := localFallbackStats{pages: 1}
	if !loaded {
		files, err := s.ds.MediaFile(ctx).GetAll(model.QueryOptions{Sort: "id", Order: "asc", Max: localFallbackPageSize})
		if err != nil {
			return nil, stats, err
		}
		pool = append(model.MediaFiles(nil), files...)
		s.fallbackMu.Lock()
		if s.fallbackLoaded.IsZero() {
			s.fallbackPool = append(model.MediaFiles(nil), files...)
			s.fallbackLoaded = time.Now().UTC()
		}
		s.fallbackMu.Unlock()
	}
	stats.scanned = len(pool)
	known := make(map[string]bool, len(pool))
	candidates := make(model.MediaFiles, 0, len(pool))
	for _, file := range pool {
		if file.ID == "" || file.Missing || known[file.ID] || seen[file.ID] {
			stats.rejected++
			continue
		}
		known[file.ID] = true
		recording := normalizeRecordingMBID(file.MbzRecordingID)
		if recording != "" && seenRecordings[recording] {
			stats.rejected++
			continue
		}
		if requireAffinity && !localFallbackHasAffinity(seed, file) {
			stats.rejected++
			continue
		}
		candidates = append(candidates, file)
	}
	return candidates, stats, nil
}

// localCandidateFilesForFallback walks the library in stable pages. The
// recommendation pools use the affinity pass first, then call it again with
// requireAffinity=false so an unrelated but playable local track can still
// keep radio alive. The latter is deliberately exhaustive rather than another
// popularity sample: a track beyond the first page is still an eligible
// fallback.
func (s *service) localCandidateFilesForFallback(ctx context.Context, seed *model.MediaFile, seen, seenRecordings map[string]bool, requireAffinity bool) (model.MediaFiles, localFallbackStats, error) {
	if s.ds == nil {
		return nil, localFallbackStats{}, fmt.Errorf("media datastore is unavailable")
	}
	repo := s.ds.MediaFile(ctx)
	candidates := make(model.MediaFiles, 0)
	stats := localFallbackStats{}
	known := make(map[string]bool)
	for offset := 0; ; offset += localFallbackPageSize {
		page, err := repo.GetAll(model.QueryOptions{
			Sort:   "id",
			Order:  "asc",
			Max:    localFallbackPageSize,
			Offset: offset,
		})
		if err != nil {
			return nil, stats, err
		}
		if len(page) == 0 {
			break
		}
		stats.pages++
		newRows := 0
		for _, file := range page {
			if strings.TrimSpace(file.ID) == "" {
				stats.scanned++
				stats.rejected++
				continue
			}
			if file.ID != "" && known[file.ID] {
				continue
			}
			if file.ID != "" {
				known[file.ID] = true
			}
			newRows++
			stats.scanned++
			recordingMBID := normalizeRecordingMBID(file.MbzRecordingID)
			if seen[file.ID] || (recordingMBID != "" && seenRecordings[recordingMBID]) || !isPlayableLocalFile(file) {
				stats.rejected++
				continue
			}
			if requireAffinity && !localFallbackHasAffinity(seed, file) {
				stats.rejected++
				continue
			}
			candidates = append(candidates, file)
		}
		// Some light-weight test repositories ignore Offset. Stop when a page
		// makes no progress so an unhealthy repository cannot loop forever.
		if len(page) < localFallbackPageSize || newRows == 0 {
			break
		}
	}
	return candidates, stats, nil
}

func isPlayableLocalFile(file model.MediaFile) bool {
	if file.ID == "" || file.Missing {
		return false
	}
	// Metadata-only MediaFiles are used by repository/unit tests and by a few
	// callers before scanner paths have been hydrated. Production library rows
	// have both fields. A half-populated path is unusable and must not enter a
	// fallback queue.
	libraryPath := strings.TrimSpace(file.LibraryPath)
	relativePath := strings.TrimSpace(file.Path)
	if libraryPath == "" && relativePath == "" {
		return true
	}
	if libraryPath == "" || relativePath == "" {
		return false
	}
	info, err := os.Stat(file.AbsolutePath())
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return false
	}
	f, err := os.Open(file.AbsolutePath())
	if err != nil {
		return false
	}
	defer f.Close()
	var one [1]byte
	_, err = io.ReadFull(f, one[:])
	return err == nil
}

func localSeedAffinity(seed *model.MediaFile, file model.MediaFile) float64 {
	if seed == nil {
		return 0
	}
	genreMatches := genreAffinity(genreSet(*seed), genreSet(file))
	artistMatch := strings.EqualFold(file.Artist, seed.Artist) || (file.ArtistID != "" && file.ArtistID == seed.ArtistID)
	compatibility := genreMatches * 12
	if artistMatch {
		compatibility += 16
	}
	return compatibility / 52
}

func genreSet(file model.MediaFile) map[string]bool {
	result := map[string]bool{}
	if genre := strings.ToLower(strings.TrimSpace(file.Genre)); genre != "" {
		result[genre] = true
	}
	for _, genre := range file.Genres {
		if name := strings.ToLower(strings.TrimSpace(genre.Name)); name != "" {
			result[name] = true
		}
	}
	return result
}

func genreAffinity(seed, candidate map[string]bool) float64 {
	var score float64
	for seedGenre := range seed {
		for candidateGenre := range candidate {
			score = math.Max(score, genreSimilarity(seedGenre, candidateGenre))
		}
	}
	return score
}

func genreSimilarity(a, b string) float64 {
	a = normalizeGenre(a)
	b = normalizeGenre(b)
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 3
	}
	aTerms := strings.Fields(a)
	bTerms := strings.Fields(b)
	for _, aTerm := range aTerms {
		for _, bTerm := range bTerms {
			if aTerm == bTerm {
				return 2
			}
		}
	}
	if (len(a) >= 4 && strings.Contains(b, a)) || (len(b) >= 4 && strings.Contains(a, b)) {
		return 1
	}
	if family := genreFamily(a); family != "" && family == genreFamily(b) {
		return 1
	}
	return 0
}

func normalizeGenre(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("&", " and ", "/", " ", "-", " ", "_", " ").Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

func genreFamily(value string) string {
	switch {
	case strings.Contains(value, "hip hop"), value == "rap":
		return "hip-hop"
	case strings.Contains(value, "r and b"), strings.Contains(value, "rnb"), value == "soul":
		return "rnb"
	case strings.Contains(value, "electronic"), value == "edm", strings.Contains(value, "house"), strings.Contains(value, "techno"):
		return "electronic"
	case strings.Contains(value, "rock"):
		return "rock"
	case strings.Contains(value, "metal"):
		return "metal"
	case strings.Contains(value, "pop"), value == "indie":
		return "pop"
	case strings.Contains(value, "jazz"):
		return "jazz"
	case strings.Contains(value, "classical"), strings.Contains(value, "orchestral"):
		return "classical"
	default:
		return ""
	}
}

func hasDownloadingItems(items []model.PersonalRadioItem) bool {
	for _, item := range items {
		if item.Status == model.RadioItemDownloading {
			return true
		}
	}
	return false
}

func countDownloadingRadioItems(items []model.PersonalRadioItem) int {
	count := 0
	for _, item := range items {
		if item.ItemType == model.RadioItemDiscovery && item.Status == model.RadioItemDownloading {
			count++
		}
	}
	return count
}

func hasHeldDiscoveryItems(items []model.PersonalRadioItem) bool {
	for _, item := range items {
		if item.ItemType == model.RadioItemDiscovery && item.Status == model.RadioItemHeld {
			return true
		}
	}
	return false
}

func readyPlayableRadioItems(items []model.PersonalRadioItem) int {
	count := 0
	for _, item := range items {
		if item.ItemType != model.RadioItemSeed && item.MediaFileID != "" &&
			item.Status == model.RadioItemReady {
			count++
		}
	}
	return count
}

func readyPlayableRadioItemsForContext(items []model.PersonalRadioItem, radioContext *radioContext) int {
	if radioContext == nil || !radioContext.ClientQueueProvided {
		return readyPlayableRadioItems(items)
	}
	queued := radioContext.QueuedItemIDs
	count := 0
	for _, item := range items {
		if item.ID == radioContext.CurrentItemID || !queued[item.ID] || item.ItemType == model.RadioItemSeed ||
			item.Status != model.RadioItemReady || item.MediaFileID == "" {
			continue
		}
		count++
	}
	return count
}

func pendingRadioItems(items []model.PersonalRadioItem) int {
	count := 0
	for _, item := range items {
		if item.Status == model.RadioItemHeld || item.Status == model.RadioItemDownloading {
			count++
		}
	}
	return count
}

func radioItemTrackKey(item model.PersonalRadioItem) string {
	return model.RadioTrackKey(item.RecordingMBID, item.MediaFileID)
}

func radioItemStatusCounts(items []model.PersonalRadioItem) map[string]int {
	counts := make(map[string]int)
	for _, item := range items {
		counts[item.Status]++
	}
	return counts
}

func plannedPositions(items []model.PersonalRadioItem) []int {
	positions := make([]int, 0, len(items))
	for _, item := range items {
		positions = append(positions, item.Position)
	}
	return positions
}

func outstandingRadioItems(items []model.PersonalRadioItem) int {
	count := 0
	for _, item := range items {
		if item.ItemType == model.RadioItemSeed {
			continue
		}
		switch item.Status {
		case model.RadioItemReady, model.RadioItemHeld, model.RadioItemDownloading:
			count++
		}
	}
	return count
}

func lastPlannedItem(items []model.PersonalRadioItem) (model.PersonalRadioItem, bool) {
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].ItemType != model.RadioItemSeed {
			return items[i], true
		}
	}
	return model.PersonalRadioItem{}, false
}

func isPendingPlanningStatus(status string) bool {
	switch status {
	case "", model.RadioPlanningSelecting, model.RadioPlanningDownloading, model.RadioPlanningWaitingForScan, model.RadioPlanningRetrying:
		return true
	default:
		return false
	}
}

func statusForReadyItems(items []model.PersonalRadioItem) string {
	for _, item := range items {
		if (item.ItemType == model.RadioItemDiscovery || item.ItemType == model.RadioItemLibrary) && item.Status == model.RadioItemReady {
			return model.RadioPlanningReady
		}
	}
	return model.RadioPlanningExhausted
}

func hasDiscoveryItems(items []model.PersonalRadioItem) bool {
	for _, item := range items {
		if item.ItemType == model.RadioItemDiscovery {
			return true
		}
	}
	return false
}

func (s *service) setPlanningStatus(sessionID, status string) {
	s.planningMu.Lock()
	defer s.planningMu.Unlock()
	if s.planningStatus == nil {
		s.planningStatus = map[string]string{}
	}
	s.planningStatus[sessionID] = status
}

func (s *service) getPlanningStatus(sessionID string) string {
	s.planningMu.Lock()
	defer s.planningMu.Unlock()
	return s.planningStatus[sessionID]
}

func (s *service) isPlanning(sessionID string) bool {
	s.planningMu.Lock()
	defer s.planningMu.Unlock()
	return s.planning[sessionID]
}

func (s *service) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	s.cleanupExpired(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.cleanupExpired(ctx)
		}
	}
}

func (s *service) cleanupExpired(ctx context.Context) {
	tracks, err := s.repo.ListExpiredDiscoveries(time.Now().UTC(), 100)
	if err != nil {
		log.Warn(ctx, "Unable to list expired discovery tracks", err)
		return
	}
	for _, track := range tracks {
		s.deleteDiscovery(ctx, track)
	}
}

func (s *service) deleteDiscovery(ctx context.Context, track model.DiscoveryTrack) {
	protected, err := s.repo.IsMediaFileProtected(track.MediaFileID)
	if err != nil {
		log.Warn(ctx, "Unable to check discovery track protection", "mediaFileID", track.MediaFileID, err)
		return
	}
	if protected {
		track.State, track.ExpiresAt = model.DiscoveryKept, nil
		_ = s.repo.UpdateDiscovery(&track)
		return
	}
	file, err := s.ds.MediaFile(ctx).Get(track.MediaFileID)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return
	}
	if file != nil {
		root, rootErr := s.ds.Library(ctx).GetPath(file.LibraryID)
		if rootErr != nil {
			return
		}
		target, absErr := filepath.Abs(filepath.Join(root, file.Path))
		absRoot, rootAbsErr := filepath.Abs(root)
		if absErr != nil || rootAbsErr != nil {
			return
		}
		rel, relErr := filepath.Rel(absRoot, target)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			log.Error(ctx, "Refusing to delete discovery track outside its library", "path", target)
			return
		}
		if removeErr := os.Remove(target); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			log.Warn(ctx, "Unable to delete rejected discovery track", "path", target, removeErr)
			return
		}
		_, _ = s.scanner.ScanFolders(ctx, false, []model.ScanTarget{{LibraryID: file.LibraryID, FolderPath: filepath.Dir(file.Path)}})
	}
	track.State, track.ExpiresAt = model.DiscoveryDeleted, nil
	_ = s.repo.UpdateDiscovery(&track)
}
