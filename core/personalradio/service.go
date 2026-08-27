package personalradio

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
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
	queueLowWatermark        = 10
	discoveryTTL             = 7 * 24 * time.Hour
)

type SimilarityProvider interface {
	GetSimilarSongsByTrackAll(context.Context, string, string, string, string, int) ([]agents.Song, error)
}

type Service interface {
	Start(context.Context)
	Create(context.Context, string, model.CreatePersonalRadioRequest) (*model.PersonalRadioSessionResponse, error)
	Refill(context.Context, string, string, model.RefillPersonalRadioRequest) (*model.PersonalRadioSessionResponse, error)
	Feedback(context.Context, string, string, model.PersonalRadioFeedbackRequest) error
}

type service struct {
	ds             model.DataStore
	repo           model.PersonalRadioRepository
	agents         SimilarityProvider
	matcher        *matcher.Matcher
	music          musicservice.Service
	scanner        model.Scanner
	startOnce      sync.Once
	planningMu     sync.Mutex
	planning       map[string]bool
	planningStatus map[string]string
}

func New(ds model.DataStore, repo model.PersonalRadioRepository, ag *agents.Agents, songMatcher *matcher.Matcher, music musicservice.Service, scanner model.Scanner) Service {
	return &service{
		ds:             ds,
		repo:           repo,
		agents:         ag,
		matcher:        songMatcher,
		music:          music,
		scanner:        scanner,
		planning:       map[string]bool{},
		planningStatus: map[string]string{},
	}
}

func (s *service) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.startOnce.Do(func() { go s.cleanupLoop(ctx) })
}

func (s *service) Create(ctx context.Context, userID string, request model.CreatePersonalRadioRequest) (*model.PersonalRadioSessionResponse, error) {
	seedID := strings.TrimSpace(request.SeedMediaFileID)
	seed, err := s.ds.MediaFile(ctx).GetWithParticipants(seedID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	session := model.PersonalRadioSession{ID: id.NewRandom(), UserID: userID, SeedMediaFileID: seedID, Mode: model.NormalizeRadioMode(string(request.Mode)), Status: model.PersonalRadioActive, CreatedAt: now, UpdatedAt: now}
	items := []model.PersonalRadioItem{{ID: id.NewRandom(), SessionID: session.ID, Position: 0, ItemType: model.RadioItemSeed, Status: model.RadioItemReady, MediaFileID: seed.ID, RecordingMBID: seed.MbzRecordingID, Song: seed, CreatedAt: now, UpdatedAt: now}}
	if err := s.repo.CreateSession(&session, items); err != nil {
		return nil, err
	}
	if err := s.repo.EndActiveSessions(userID, session.ID); err != nil {
		return nil, err
	}
	s.setPlanningStatus(session.ID, model.RadioPlanningSelecting)
	log.Info(ctx, "Personal radio session created",
		"sessionID", session.ID,
		"userID", userID,
		"seedID", seed.ID,
		"seedTitle", seed.Title,
		"seedArtist", seed.Artist,
		"seedRecordingMBID", seed.MbzRecordingID)
	s.schedulePlan(context.WithoutCancel(ctx), session, seed)
	return &model.PersonalRadioSessionResponse{
		Session:        session,
		Items:          items,
		Pending:        true,
		PlanningStatus: model.RadioPlanningSelecting,
	}, nil
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
		} else if radioOutstandingItems(items, radioContext) < queueLowWatermark {
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
	return &model.PersonalRadioSessionResponse{
		Session:        *session,
		Items:          items,
		Pending:        pending || isPendingPlanningStatus(status),
		PlanningStatus: status,
	}, nil
}

func (s *service) Feedback(ctx context.Context, userID, sessionID string, req model.PersonalRadioFeedbackRequest) error {
	if _, err := s.repo.GetSessionForUser(sessionID, userID); err != nil {
		return err
	}
	feedback, err := s.repo.RecordPlaybackFeedback(userID, sessionID, req, time.Now().UTC())
	if err != nil {
		return err
	}
	item := &feedback.Item
	if item.RecordingMBID != "" && feedback.Applied {
		switch item.PlaybackOutcome {
		case model.RadioPlaybackAccepted:
			if err := s.repo.RecordFeedback(userID, model.NormalizeRecordingMBID(item.RecordingMBID), model.RadioFeedbackThresholdReached, time.Now().UTC()); err != nil {
				return err
			}
		case model.RadioPlaybackCompleted:
			if err := s.repo.RecordFeedback(userID, model.NormalizeRecordingMBID(item.RecordingMBID), model.RadioFeedbackCompleted, time.Now().UTC()); err != nil {
				return err
			}
		case model.RadioPlaybackEarlySkip:
			if err := s.repo.RecordFeedback(userID, model.NormalizeRecordingMBID(item.RecordingMBID), model.RadioFeedbackManualSkip, time.Now().UTC()); err != nil {
				return err
			}
		case model.RadioPlaybackLateSkip:
			if err := s.repo.RecordFeedback(userID, model.NormalizeRecordingMBID(item.RecordingMBID), "neutral", time.Now().UTC()); err != nil {
				return err
			}
		case model.RadioPlaybackKeep:
			if err := s.repo.RecordFeedback(userID, model.NormalizeRecordingMBID(item.RecordingMBID), model.RadioFeedbackKeep, time.Now().UTC()); err != nil {
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
	}
	return s.repo.UpdateDiscovery(discovery)
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
	s.schedulePlanForContext(ctx, session, radioContextFromSeed(seed))
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
			s.setPlanningStatus(session.ID, model.RadioPlanningNoDiscovery)
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
	if outstanding >= queueLowWatermark {
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

	slotsToAdd := queueLowWatermark - outstanding
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
		s.setPlanningStatus(session.ID, model.RadioPlanningNoDiscovery)
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
	for _, candidate := range selected {
		selectedKeys = append(selectedKeys, candidate.candidate.Key)
		selectedScores = append(selectedScores, candidate.ranked.Score)
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
		"readyLibraryBuffer", readyLibraryBufferCount(active),
		"selectedKeys", selectedKeys,
		"selectedScores", selectedScores)
	newItems := make([]model.PersonalRadioItem, 0, len(selected))
	for _, candidate := range selected {
		if candidate.isDiscovery {
			item, ok := s.queueDiscovery(ctx, session, candidate.discovery, position, now)
			if !ok {
				continue
			}
			newItems = append(newItems, item)
			position++
			continue
		}
		if candidate.local == nil {
			continue
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
	}
	if len(newItems) == 0 {
		s.setPlanningStatus(session.ID, model.RadioPlanningNoDiscovery)
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
	if hasDiscoveryItems(newItems) {
		s.setPlanningStatus(session.ID, model.RadioPlanningDownloading)
	} else {
		s.setPlanningStatus(session.ID, model.RadioPlanningNoDiscovery)
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
}

func (s *service) recommendationPools(ctx context.Context, session model.PersonalRadioSession, seed *model.MediaFile, seen map[string]bool, seenRecordings map[string]bool, count int) (candidatePools, error) {
	return s.recommendationPoolsWithLimit(ctx, session, seed, seen, seenRecordings, count, discoveryCandidateLimit, 1)
}

func (s *service) recommendationPoolsWithLimit(ctx context.Context, session model.PersonalRadioSession, seed *model.MediaFile, seen map[string]bool, seenRecordings map[string]bool, count, providerLimit int, seedWeight float64) (candidatePools, error) {
	var pools candidatePools
	localAdded := map[string]bool{}
	localAddedKeys := map[string]bool{}
	normalizedSeenRecordings := make(map[string]bool, len(seenRecordings))
	for recordingMBID := range seenRecordings {
		normalizedSeenRecordings[normalizeRecordingMBID(recordingMBID)] = true
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
		providerRecommendations, recErr := s.agents.GetSimilarSongsByTrackAll(ctx, seed.ID, seed.Title, seed.Artist, seed.MbzRecordingID, providerLimit)
		if recErr != nil {
			log.Debug(ctx, "Personal radio similarity provider returned no candidates",
				"sessionID", session.ID,
				"userID", session.UserID,
				"seedID", seed.ID,
				"error", recErr)
		} else if len(providerRecommendations) > 0 {
			stats["providerCandidates"] = len(providerRecommendations)
			matches, matchErr := s.matcher.MatchSongsIndexed(ctx, providerRecommendations)
			if matchErr != nil {
				log.Warn(ctx, "Unable to compare personal radio recommendations with the library",
					"sessionID", session.ID,
					"userID", session.UserID,
					"seedID", seed.ID,
					"candidateCount", len(providerRecommendations),
					"error", matchErr)
			} else {
				mbids := make([]string, 0, len(providerRecommendations))
				for _, song := range providerRecommendations {
					if recordingMBID := normalizeRecordingMBID(song.MBID); recordingMBID != "" {
						mbids = append(mbids, recordingMBID)
					}
				}
				feedback, feedbackErr := s.repo.GetFeedback(session.UserID, mbids)
				if feedbackErr != nil {
					log.Warn(ctx, "Personal radio could not load recommendation feedback",
						"sessionID", session.ID,
						"userID", session.UserID,
						"mbidCount", len(mbids),
						"error", feedbackErr)
					feedback = map[string]model.RadioTrackFeedback{}
				}
				normalizedFeedback := make(map[string]model.RadioTrackFeedback, len(feedback))
				for recordingMBID, value := range feedback {
					normalizedFeedback[normalizeRecordingMBID(recordingMBID)] = value
				}
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
					feedbackForSong := normalizedFeedback[recordingMBID]
					if local, ok := matches[i]; ok {
						localKey := radioMediaFileCandidateKey(local)
						localRecordingMBID := model.NormalizeRecordingMBID(local.MbzRecordingID)
						if !local.Missing && !seen[local.ID] &&
							(localRecordingMBID == "" || !seenRecordings[localRecordingMBID]) &&
							!localAdded[local.ID] && !localAddedKeys[localKey] {
							localAdded[local.ID] = true
							localAddedKeys[localKey] = true
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
							fatigue[local.ID] = radioFeedbackFatigue(feedbackForSong)
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
					cooldownDays := min(365, 30*(feedbackForSong.EarlySkipCount+1))
					if feedbackForSong.LastEarlySkipAt != nil && now.Sub(feedbackForSong.LastEarlySkipAt.UTC()) < time.Duration(cooldownDays)*24*time.Hour {
						stats["feedbackCooldown"]++
						traceRadioCandidate(ctx, "Personal radio candidate rejected by feedback cooldown",
							append(candidateFields,
								"decision", "feedback_cooldown",
								"earlySkipCount", feedbackForSong.EarlySkipCount,
								"cooldownDays", cooldownDays,
								"lastEarlySkipAt", feedbackForSong.LastEarlySkipAt))
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
					fatigue[key] = radioFeedbackFatigue(feedbackForSong)
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
	fallback, err := s.localCandidateFiles(ctx, seed, metadataSeen)
	if err != nil {
		return candidatePools{}, fmt.Errorf("load local fallback candidates: %w", err)
	}
	stats["localFallback"] = len(fallback)
	for _, file := range fallback {
		key := radioMediaFileCandidateKey(file)
		if !localAdded[file.ID] && !localAddedKeys[key] {
			if recordingMBID := model.NormalizeRecordingMBID(file.MbzRecordingID); recordingMBID != "" && seenRecordings[recordingMBID] {
				continue
			}
			localAdded[file.ID] = true
			localAddedKeys[key] = true
			fileCopy := file
			rankedCandidates = append(rankedCandidates, rankedRadioCandidate{
				candidate: recommendations.Candidate{Key: key, SeedAffinity: localSeedAffinity(seed, file), SessionAffinity: seedWeight * localSeedAffinity(seed, file), MediaFile: file},
				local:     &fileCopy,
			})
		}
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
			if file.Missing || seen[file.ID] || (file.MbzRecordingID != "" && seenRecordings[model.NormalizeRecordingMBID(file.MbzRecordingID)]) {
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
			})
			localAdded[file.ID] = true
			localAddedKeys[transition.TargetKey] = true
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
				continue
			}
			candidate.candidate.TransitionAffinity = recommendations.TransitionAffinity(feedbackForTarget, now)
		}
		filteredCandidates = append(filteredCandidates, candidate)
	}
	rankedCandidates = filteredCandidates
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

func (s *service) recommendationPoolsForContext(ctx context.Context, session model.PersonalRadioSession, radioContext *radioContext, seen map[string]bool, seenRecordings map[string]bool, count int) (candidatePools, error) {
	if radioContext == nil || len(radioContext.Seeds) == 0 {
		return candidatePools{}, fmt.Errorf("radio context has no seeds")
	}
	var all []rankedRadioCandidate
	fatigue := map[string]float64{}
	workingSeen := cloneRadioBoolMap(seen)
	workingRecordings := cloneRadioBoolMap(seenRecordings)
	for _, seed := range radioContext.Seeds {
		if seed.File == nil {
			continue
		}
		seedPools, err := s.recommendationPoolsWithLimit(ctx, session, seed.File, workingSeen, workingRecordings, count, providerLimitForRadioSeed(seed), seed.Weight)
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
	for _, candidate := range recommendations.Rank(ranked, recommendations.Options{Now: time.Now().UTC(), Fatigue: fatigue}) {
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
	byID := map[string]model.MediaFile{}
	for _, options := range []model.QueryOptions{
		{Sort: "play_count", Order: "desc", Max: 500},
		{Sort: "play_date", Order: "desc", Max: 500},
		{Sort: "created_at", Order: "desc", Max: 500},
		{Sort: "starred_at", Order: "desc", Max: 250},
	} {
		files, err := s.ds.MediaFile(ctx).GetAll(options)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			byID[file.ID] = file
		}
	}
	files := make([]model.MediaFile, 0, len(byID))
	for _, file := range byID {
		files = append(files, file)
	}
	candidates := make(model.MediaFiles, 0, len(files))
	for _, file := range files {
		if seen[file.ID] || file.Missing {
			continue
		}
		if localSeedAffinity(seed, file) == 0 {
			continue
		}
		candidates = append(candidates, file)
	}
	return candidates, nil
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
		if item.ItemType == model.RadioItemDiscovery && (item.Status == model.RadioItemReady || item.Status == model.RadioItemPlayed) {
			return model.RadioPlanningReady
		}
	}
	return model.RadioPlanningNoDiscovery
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
