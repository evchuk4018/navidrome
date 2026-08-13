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
	initialLocalTracks      = 2
	localTracksPerPlan      = 2
	discoveryTracksPerPlan  = 2
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
	local, err := s.localCandidates(ctx, seed, map[string]bool{seed.ID: true}, initialLocalTracks)
	if err != nil {
		return nil, err
	}
	items = append(items, makeLibraryItems(session.ID, 1, local, now)...)
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
	for i := range items {
		item := &items[i]
		if item.Status != model.RadioItemDownloading {
			continue
		}
		job, getErr := s.music.GetDownload(ctx, userID, item.DownloadJobID)
		if getErr != nil {
			if errors.Is(getErr, model.ErrNotFound) {
				item.Status = model.RadioItemFailed
				_ = s.repo.UpdateItem(item)
				continue
			}
			pending = true
			continue
		}
		switch job.Status {
		case model.MusicDownloadSuccess:
			matched, matchErr := s.matcher.MatchSongsIndexed(ctx, []agents.Song{{MBID: item.RecordingMBID}})
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
		default:
			pending = true
		}
	}

	ready := 0
	seen := map[string]bool{}
	for i := range items {
		if items[i].MediaFileID != "" {
			seen[items[i].MediaFileID] = true
		}
		if items[i].Status == model.RadioItemReady {
			ready++
		}
		if items[i].Status == model.RadioItemReady || items[i].Status == model.RadioItemPlayed {
			if items[i].Song == nil {
				items[i].Song, _ = s.ds.MediaFile(ctx).GetWithParticipants(items[i].MediaFileID)
			}
		}
	}
	if session.Status == model.PersonalRadioActive && ready < queueLowWatermark {
		seed, seedErr := s.planningSeed(ctx, *session)
		if seedErr == nil {
			s.schedulePlan(context.WithoutCancel(ctx), *session, seed)
			pending = true
		}
	}

	status := s.getPlanningStatus(session.ID)
	if waitingForScan {
		status = model.RadioPlanningWaitingForScan
	} else if hasDownloadingItems(items) {
		status = model.RadioPlanningDownloading
	} else if pending {
		if status == "" {
			status = model.RadioPlanningSelecting
		}
	} else if !s.isPlanning(session.ID) && (status == "" || status == model.RadioPlanningSelecting || status == model.RadioPlanningDownloading || status == model.RadioPlanningWaitingForScan) {
		status = statusForReadyItems(items)
		s.setPlanningStatus(session.ID, status)
	}
	if status == "" {
		status = model.RadioPlanningReady
	}
	return &model.PersonalRadioSessionResponse{
		Session:        *session,
		Items:          items,
		Pending:        pending || status == model.RadioPlanningSelecting || status == model.RadioPlanningDownloading || status == model.RadioPlanningWaitingForScan,
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
	s.planningStatus[session.ID] = model.RadioPlanningSelecting
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
	seen := map[string]bool{}
	seenRecordings := map[string]bool{}
	position := 0
	for _, item := range items {
		seen[item.MediaFileID] = item.MediaFileID != ""
		if item.RecordingMBID != "" {
			seenRecordings[item.RecordingMBID] = true
		}
		position = max(position, item.Position+1)
	}

	now := time.Now().UTC()
	newItems := make([]model.PersonalRadioItem, 0, localTracksPerPlan+discoveryTracksPerPlan)
	discoveries := s.discoveryCandidates(ctx, session, seed, seen, seenRecordings, discoveryTracksPerPlan)
	if len(discoveries) > 0 {
		s.setPlanningStatus(session.ID, model.RadioPlanningDownloading)
	}
	for _, discovery := range discoveries {
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
			continue
		}
		job, createErr := s.music.CreateDownload(ctx, session.UserID, model.ExternalDownloadRequest{
			Kind:        model.MusicDownloadSong,
			ID:          discovery.MBID,
			Origin:      model.MusicDownloadOriginRadio,
			Priority:    100,
			RadioItemID: item.ID,
		})
		if createErr != nil {
			log.Warn(ctx, "Unable to queue personal radio discovery", "recordingMBID", discovery.MBID, createErr)
			continue
		}
		if job == nil || job.ID == "" {
			log.Warn(ctx, "Personal radio download service returned an empty job", "recordingMBID", discovery.MBID)
			continue
		}
		item.DownloadJobID = job.ID
		newItems = append(newItems, item)
		seenRecordings[discovery.MBID] = true
		position++
	}

	local, err := s.localCandidates(ctx, seed, seen, localTracksPerPlan)
	if err != nil {
		return err
	}
	newItems = append(newItems, makeLibraryItems(session.ID, position, local, now)...)
	if len(newItems) == 0 {
		s.setPlanningStatus(session.ID, model.RadioPlanningNoDiscovery)
		return nil
	}
	if err := s.repo.AppendItems(session.ID, newItems); err != nil {
		return err
	}
	if len(discoveries) == 0 || !hasDiscoveryItems(newItems) {
		s.setPlanningStatus(session.ID, model.RadioPlanningNoDiscovery)
	}
	return nil
}

func (s *service) discoveryCandidates(ctx context.Context, session model.PersonalRadioSession, seed *model.MediaFile, seen map[string]bool, seenRecordings map[string]bool, count int) []agents.Song {
	if count <= 0 || s.agents == nil || s.matcher == nil {
		s.setPlanningStatus(session.ID, model.RadioPlanningNoDiscovery)
		return nil
	}
	recommendations, recErr := s.agents.GetSimilarSongsByTrackAll(ctx, seed.ID, seed.Title, seed.Artist, seed.MbzRecordingID, discoveryCandidateLimit)
	if recErr != nil || len(recommendations) == 0 {
		log.Debug(ctx, "No personal radio discovery candidates", "sessionID", session.ID, "error", recErr)
		s.setPlanningStatus(session.ID, model.RadioPlanningNoDiscovery)
		return nil
	}

	matches, matchErr := s.matcher.MatchSongsIndexed(ctx, recommendations)
	if matchErr != nil {
		log.Warn(ctx, "Unable to compare personal radio discoveries with the library", "sessionID", session.ID, matchErr)
		s.setPlanningStatus(session.ID, model.RadioPlanningNoDiscovery)
		return nil
	}
	mbids := make([]string, 0, len(recommendations))
	for _, song := range recommendations {
		if song.MBID != "" {
			mbids = append(mbids, song.MBID)
		}
	}
	feedback, _ := s.repo.GetFeedback(session.UserID, mbids)
	result := make([]agents.Song, 0, count)
	for i := range recommendations {
		song := recommendations[i]
		if song.MBID == "" || seenRecordings[song.MBID] {
			continue
		}
		if local, ok := matches[i]; ok {
			seen[local.ID] = true
			log.Debug(ctx, "Skipping personal radio candidate already in library", "recordingMBID", song.MBID, "mediaFileID", local.ID)
			continue
		}
		f := feedback[song.MBID]
		cooldownDays := min(365, 30*(f.EarlySkipCount+1))
		if f.LastEarlySkipAt != nil && time.Since(*f.LastEarlySkipAt) < time.Duration(cooldownDays)*24*time.Hour {
			continue
		}
		result = append(result, song)
		if len(result) == count {
			break
		}
	}
	if len(result) == 0 {
		s.setPlanningStatus(session.ID, model.RadioPlanningNoDiscovery)
	}
	return result
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
		if compatibility == 0 && len(seedGenres) > 0 {
			continue
		}
		score := compatibility + 0.8*math.Log1p(float64(file.PlayCount)) + localRecencyScore(file)
		if file.Starred {
			score += 4
		}
		candidates = append(candidates, scoredFile{file: file, score: score})
	}
	// Sparse metadata should still produce an uninterrupted queue, but an
	// unrelated song receives a strong penalty so it cannot displace a
	// compatible candidate merely because it has a large play count.
	if len(candidates) < count {
		for _, file := range files {
			if seen[file.ID] || file.Missing {
				continue
			}
			if genreAffinity(seedGenres, genreSet(file)) > 0 || strings.EqualFold(file.Artist, seed.Artist) {
				continue
			}
			fallbackScore := -40 + 0.25*math.Log1p(float64(file.PlayCount)) + localRecencyScore(file)
			candidates = append(candidates, scoredFile{file: file, score: fallbackScore})
		}
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

func makeLibraryItems(sessionID string, position int, files model.MediaFiles, now time.Time) []model.PersonalRadioItem {
	items := make([]model.PersonalRadioItem, 0, len(files))
	for i := range files {
		file := files[i]
		items = append(items, model.PersonalRadioItem{ID: id.NewRandom(), SessionID: sessionID, Position: position + i, ItemType: model.RadioItemLibrary, Status: model.RadioItemReady, MediaFileID: file.ID, RecordingMBID: file.MbzRecordingID, Song: &file, CreatedAt: now, UpdatedAt: now})
	}
	return items
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
