package personalradio

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/navidrome/navidrome/core/agents"
	"github.com/navidrome/navidrome/core/matcher"
	musicservice "github.com/navidrome/navidrome/core/music"
	"github.com/navidrome/navidrome/log"
	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/id"
)

const (
	discoveryCandidateLimit = 40
	queueLowWatermark       = 6
	discoveryTTL            = 7 * 24 * time.Hour
)

type SimilarityProvider interface {
	GetSimilarSongsByTrackAll(context.Context, string, string, string, string, int) ([]agents.Song, error)
}

type Service interface {
	Start(context.Context)
	Create(context.Context, string, string) (*model.PersonalRadioSessionResponse, error)
	Refill(context.Context, string, string) (*model.PersonalRadioSessionResponse, error)
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

func (s *service) Create(ctx context.Context, userID, seedID string) (*model.PersonalRadioSessionResponse, error) {
	seed, err := s.ds.MediaFile(ctx).GetWithParticipants(seedID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	session := model.PersonalRadioSession{ID: id.NewRandom(), UserID: userID, SeedMediaFileID: seedID, Status: model.PersonalRadioActive, CreatedAt: now, UpdatedAt: now}
	items := []model.PersonalRadioItem{{ID: id.NewRandom(), SessionID: session.ID, Position: 0, ItemType: model.RadioItemSeed, Status: model.RadioItemReady, MediaFileID: seed.ID, RecordingMBID: seed.MbzRecordingID, Song: seed, CreatedAt: now, UpdatedAt: now}}
	if err := s.repo.CreateSession(&session, items); err != nil {
		return nil, err
	}
	if err := s.repo.EndActiveSessions(userID, session.ID); err != nil {
		return nil, err
	}
	s.setPlanningStatus(session.ID, model.RadioPlanningSelecting)
	s.schedulePlan(context.WithoutCancel(ctx), session, seed)
	return &model.PersonalRadioSessionResponse{
		Session:        session,
		Items:          items,
		Pending:        true,
		PlanningStatus: model.RadioPlanningSelecting,
	}, nil
}

func (s *service) Refill(ctx context.Context, userID, sessionID string) (*model.PersonalRadioSessionResponse, error) {
	session, err := s.repo.GetSessionForUser(sessionID, userID)
	if err != nil {
		return nil, err
	}
	items, err := s.repo.GetItems(sessionID)
	if err != nil {
		return nil, err
	}
	pending := false
	waitingForScan := false
	downloadFailed := false
	for i := range items {
		item := &items[i]
		if item.Status != model.RadioItemDownloading {
			continue
		}
		if s.music == nil {
			item.Status = model.RadioItemFailed
			_ = s.repo.UpdateItem(item)
			downloadFailed = true
			continue
		}
		job, getErr := s.music.GetDownload(ctx, userID, item.DownloadJobID)
		if getErr != nil {
			if errors.Is(getErr, model.ErrNotFound) {
				item.Status = model.RadioItemFailed
				_ = s.repo.UpdateItem(item)
				downloadFailed = true
				continue
			}
			pending = true
			continue
		}
		switch job.Status {
		case model.MusicDownloadSuccess:
			var matched map[int]model.MediaFile
			var matchErr error
			if s.matcher != nil {
				matched, matchErr = s.matcher.MatchSongsIndexed(ctx, []agents.Song{{MBID: item.RecordingMBID}})
			}
			if matchErr == nil {
				if file, ok := matched[0]; ok {
					item.MediaFileID = file.ID
					item.Song = &file
					item.Status = model.RadioItemReady
					job.MediaFileID = file.ID
					_ = s.repo.UpdateItem(item)
					expires := time.Now().UTC().Add(discoveryTTL)
					_ = s.repo.UpsertDiscovery(&model.DiscoveryTrack{ID: id.NewRandom(), UserID: userID, RecordingMBID: item.RecordingMBID, MediaFileID: file.ID, State: model.DiscoveryTemporary, ExpiresAt: &expires, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
					continue
				}
			}
			waitingForScan = true // The scanner may still be committing the import.
		case model.MusicDownloadFailed:
			item.Status = model.RadioItemFailed
			_ = s.repo.UpdateItem(item)
			downloadFailed = true
		default:
			pending = true
		}
	}

	for _, index := range releasableHeldItems(items) {
		items[index].Status = model.RadioItemReady
		_ = s.repo.UpdateItem(&items[index])
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
	if session.Status == model.PersonalRadioActive && outstandingRadioItems(items) < queueLowWatermark {
		seed, seedErr := s.planningSeed(ctx, *session)
		if seedErr == nil {
			s.schedulePlan(context.WithoutCancel(ctx), *session, seed)
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
	item, err := s.repo.GetItemForUser(req.ItemID, userID)
	if err != nil {
		return err
	}
	item.Status = model.RadioItemPlayed
	_ = s.repo.UpdateItem(item)
	if item.ItemType != model.RadioItemDiscovery || item.RecordingMBID == "" {
		return nil
	}
	discovery, err := s.repo.GetDiscoveryByRecording(userID, item.RecordingMBID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	thresholdMS := earlySkipThresholdMS(req.DurationMS)
	switch req.Event {
	case model.RadioFeedbackStarted:
		discovery.PlayStarts++
		if discovery.PlayStarts > 1 {
			discovery.State, discovery.ExpiresAt = model.DiscoveryKept, nil
			_ = s.repo.RecordFeedback(userID, item.RecordingMBID, model.RadioFeedbackKeep, now)
		}
	case model.RadioFeedbackThresholdReached, model.RadioFeedbackCompleted, model.RadioFeedbackKeep:
		discovery.State, discovery.ExpiresAt = model.DiscoveryKept, nil
		_ = s.repo.RecordFeedback(userID, item.RecordingMBID, req.Event, now)
	case model.RadioFeedbackManualSkip:
		if req.ListenedMS < thresholdMS {
			discovery.State = model.DiscoveryDeletePending
			_ = s.repo.RecordFeedback(userID, item.RecordingMBID, req.Event, now)
			_ = s.repo.UpdateDiscovery(discovery)
			go func() {
				timer := time.NewTimer(2 * time.Second)
				defer timer.Stop()
				<-timer.C
				s.deleteDiscovery(context.WithoutCancel(ctx), *discovery)
			}()
			return nil
		}
		_ = s.repo.RecordFeedback(userID, item.RecordingMBID, "neutral", now)
	}
	return s.repo.UpdateDiscovery(discovery)
}

func (s *service) planningSeed(ctx context.Context, session model.PersonalRadioSession) (*model.MediaFile, error) {
	ids, err := s.repo.RecentSeedIDs(session.ID, 1)
	if err == nil && len(ids) > 0 {
		if file, getErr := s.ds.MediaFile(ctx).GetWithParticipants(ids[0]); getErr == nil {
			return file, nil
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
	go func() {
		defer func() {
			s.planningMu.Lock()
			delete(s.planning, session.ID)
			s.planningMu.Unlock()
		}()
		if err := s.plan(ctx, session, seed); err != nil {
			s.setPlanningStatus(session.ID, model.RadioPlanningNoDiscovery)
			log.Warn(ctx, "Unable to extend personal radio queue", "sessionID", session.ID, err)
		}
	}()
}

func (s *service) plan(ctx context.Context, session model.PersonalRadioSession, seed *model.MediaFile) error {
	items, err := s.repo.GetItems(session.ID)
	if err != nil {
		return err
	}
	outstanding := outstandingRadioItems(items)
	if outstanding >= queueLowWatermark {
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
			seenRecordings[item.RecordingMBID] = true
		}
		position = max(position, item.Position+1)
	}

	slotsToAdd := queueLowWatermark - outstanding
	// Refill in pairs so a single failed discovery cannot permanently skew the
	// library/discovery ratio. One extra buffered item is preferable to another
	// planning pass on every status poll.
	if slotsToAdd%2 != 0 {
		slotsToAdd++
	}
	pools, err := s.recommendationPools(ctx, session, seed, seen, seenRecordings, slotsToAdd)
	if err != nil {
		return err
	}
	if len(pools.local) == 0 && len(pools.discovery) == 0 {
		s.setPlanningStatus(session.ID, model.RadioPlanningNoDiscovery)
		return nil
	}

	now := time.Now().UTC()
	newItems := make([]model.PersonalRadioItem, 0, slotsToAdd)
	previous, hasPrevious := lastPlannedItem(items)
	blockedByDiscovery := lastDiscoveryIsDownloading(items)
	nextType := model.RadioItemLibrary
	if hasPrevious && previous.ItemType == model.RadioItemLibrary {
		nextType = model.RadioItemDiscovery
	}
	localIndex, discoveryIndex := 0, 0
	for len(newItems) < slotsToAdd {
		added := false
		for attempt := 0; attempt < 2 && !added; attempt++ {
			switch nextType {
			case model.RadioItemLibrary:
				if localIndex < len(pools.local) {
					file := pools.local[localIndex]
					localIndex++
					status := model.RadioItemReady
					if blockedByDiscovery {
						status = model.RadioItemHeld
					}
					item := model.PersonalRadioItem{
						ID:            id.NewRandom(),
						SessionID:     session.ID,
						Position:      position,
						ItemType:      model.RadioItemLibrary,
						Status:        status,
						MediaFileID:   file.ID,
						RecordingMBID: file.MbzRecordingID,
						Song:          &file,
						CreatedAt:     now,
						UpdatedAt:     now,
					}
					newItems = append(newItems, item)
					position++
					nextType = model.RadioItemDiscovery
					added = true
					continue
				}
				nextType = model.RadioItemDiscovery
			case model.RadioItemDiscovery:
				for discoveryIndex < len(pools.discovery) && !added {
					discovery := pools.discovery[discoveryIndex]
					discoveryIndex++
					item, ok := s.queueDiscovery(ctx, session, discovery, position, now)
					if !ok {
						continue
					}
					newItems = append(newItems, item)
					blockedByDiscovery = true
					position++
					nextType = model.RadioItemLibrary
					added = true
				}
				if !added {
					nextType = model.RadioItemLibrary
				}
			}
		}
		if !added {
			break
		}
	}
	if len(newItems) == 0 {
		s.setPlanningStatus(session.ID, model.RadioPlanningNoDiscovery)
		return nil
	}
	if err := s.repo.AppendItems(session.ID, newItems); err != nil {
		return err
	}
	if hasDiscoveryItems(newItems) {
		s.setPlanningStatus(session.ID, model.RadioPlanningDownloading)
	} else {
		s.setPlanningStatus(session.ID, model.RadioPlanningNoDiscovery)
	}
	return nil
}

type candidatePools struct {
	local     model.MediaFiles
	discovery []agents.Song
}

func (s *service) recommendationPools(ctx context.Context, session model.PersonalRadioSession, seed *model.MediaFile, seen map[string]bool, seenRecordings map[string]bool, count int) (candidatePools, error) {
	pools := candidatePools{}
	localAdded := map[string]bool{}
	if s.agents != nil && s.matcher != nil {
		recommendations, recErr := s.agents.GetSimilarSongsByTrackAll(ctx, seed.ID, seed.Title, seed.Artist, seed.MbzRecordingID, discoveryCandidateLimit)
		if recErr != nil {
			log.Debug(ctx, "No personal radio recommendation candidates", "sessionID", session.ID, "error", recErr)
		} else if len(recommendations) > 0 {
			matches, matchErr := s.matcher.MatchSongsIndexed(ctx, recommendations)
			if matchErr != nil {
				log.Warn(ctx, "Unable to compare personal radio recommendations with the library", "sessionID", session.ID, matchErr)
			} else {
				mbids := make([]string, 0, len(recommendations))
				for _, song := range recommendations {
					if song.MBID != "" {
						mbids = append(mbids, song.MBID)
					}
				}
				feedback, _ := s.repo.GetFeedback(session.UserID, mbids)
				for i, song := range recommendations {
					if local, ok := matches[i]; ok {
						if !local.Missing && !seen[local.ID] && !localAdded[local.ID] {
							pools.local = append(pools.local, local)
							localAdded[local.ID] = true
						}
						continue
					}
					if song.MBID == "" || seenRecordings[song.MBID] {
						continue
					}
					f := feedback[song.MBID]
					cooldownDays := min(365, 30*(f.EarlySkipCount+1))
					if f.LastEarlySkipAt != nil && time.Since(*f.LastEarlySkipAt) < time.Duration(cooldownDays)*24*time.Hour {
						continue
					}
					pools.discovery = append(pools.discovery, song)
				}
			}
		}
	}

	metadataSeen := make(map[string]bool, len(seen)+len(localAdded))
	for mediaFileID := range seen {
		metadataSeen[mediaFileID] = true
	}
	for mediaFileID := range localAdded {
		metadataSeen[mediaFileID] = true
	}
	fallback, err := s.localCandidates(ctx, seed, metadataSeen, count)
	if err != nil {
		return candidatePools{}, err
	}
	for _, file := range fallback {
		if !localAdded[file.ID] {
			pools.local = append(pools.local, file)
			localAdded[file.ID] = true
		}
	}
	return pools, nil
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
		log.Warn(ctx, "Personal radio download service is unavailable", "recordingMBID", discovery.MBID)
		return model.PersonalRadioItem{}, false
	}
	job, err := s.music.CreateDownload(ctx, session.UserID, model.ExternalDownloadRequest{
		Kind:        model.MusicDownloadSong,
		ID:          discovery.MBID,
		Origin:      model.MusicDownloadOriginRadio,
		Priority:    100,
		RadioItemID: item.ID,
	})
	if err != nil {
		log.Warn(ctx, "Unable to queue personal radio discovery", "recordingMBID", discovery.MBID, err)
		return model.PersonalRadioItem{}, false
	}
	if job == nil || job.ID == "" {
		log.Warn(ctx, "Personal radio download service returned an empty job", "recordingMBID", discovery.MBID)
		return model.PersonalRadioItem{}, false
	}
	item.DownloadJobID = job.ID
	return item, true
}

type scoredFile struct {
	file  model.MediaFile
	score float64
}

func (s *service) localCandidates(ctx context.Context, seed *model.MediaFile, seen map[string]bool, count int) (model.MediaFiles, error) {
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
	seedGenres := genreSet(*seed)
	candidates := make([]scoredFile, 0, len(files))
	for _, file := range files {
		if seen[file.ID] || file.Missing {
			continue
		}
		genreMatches := genreAffinity(seedGenres, genreSet(file))
		artistMatch := strings.EqualFold(file.Artist, seed.Artist) || (file.ArtistID != "" && file.ArtistID == seed.ArtistID)
		compatibility := genreMatches * 12
		if artistMatch {
			compatibility += 16
		}
		if compatibility == 0 {
			continue
		}
		score := compatibility + 0.8*math.Log1p(float64(file.PlayCount)) + localRecencyScore(file)
		if file.Starred {
			score += 4
		}
		candidates = append(candidates, scoredFile{file: file, score: score})
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	result := make(model.MediaFiles, 0, count)
	added := map[string]bool{}
	for _, candidate := range candidates {
		if added[candidate.file.ID] || seen[candidate.file.ID] {
			continue
		}
		result = append(result, candidate.file)
		added[candidate.file.ID] = true
		if len(result) == count {
			break
		}
	}
	return result, nil
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

func localRecencyScore(file model.MediaFile) float64 {
	now := time.Now().UTC()
	score := 0.0
	if file.PlayDate != nil {
		days := math.Max(0, now.Sub(file.PlayDate.UTC()).Hours()/24)
		score += 3 * math.Exp(-days/30)
	}
	if !file.CreatedAt.IsZero() {
		days := math.Max(0, now.Sub(file.CreatedAt.UTC()).Hours()/24)
		score += 2 * math.Exp(-days/45)
	}
	return score
}

func hasDownloadingItems(items []model.PersonalRadioItem) bool {
	for _, item := range items {
		if item.Status == model.RadioItemDownloading {
			return true
		}
	}
	return false
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

func lastDiscoveryIsDownloading(items []model.PersonalRadioItem) bool {
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].ItemType == model.RadioItemDiscovery {
			return items[i].Status == model.RadioItemDownloading
		}
	}
	return false
}

// releasableHeldItems returns library items whose closest preceding discovery
// has resolved (success or failure). This preserves planned queue order while
// allowing playback to continue immediately after a failed download.
func releasableHeldItems(items []model.PersonalRadioItem) []int {
	result := make([]int, 0)
	for i := range items {
		if items[i].Status != model.RadioItemHeld {
			continue
		}
		blocked := false
		for previous := i - 1; previous >= 0; previous-- {
			if items[previous].ItemType == model.RadioItemDiscovery {
				blocked = items[previous].Status == model.RadioItemDownloading
				break
			}
		}
		if !blocked {
			result = append(result, i)
		}
	}
	return result
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
