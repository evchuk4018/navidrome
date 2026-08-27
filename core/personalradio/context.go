package personalradio

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/navidrome/navidrome/model"
)

// radioSeed is one weighted source of session context. The weights are
// normalized after duplicate tracks are merged, so the original seed remains
// meaningful even when it is also the current or a recently accepted item.
type radioSeed struct {
	File   *model.MediaFile
	Weight float64
	Role   string
}

type radioContext struct {
	OriginalSeed        *model.MediaFile
	CurrentItemID       string
	QueuedItemIDs       map[string]bool
	ClientQueueProvided bool
	Seeds               []radioSeed
}

var radioSeedWeights = []struct {
	weight float64
	role   string
}{
	{weight: 0.35, role: "original"},
	{weight: 0.30, role: "current"},
	{weight: 0.18, role: "accepted_recent_1"},
	{weight: 0.10, role: "accepted_recent_2"},
	{weight: 0.07, role: "accepted_recent_3"},
}

type radioSeedInput struct {
	file   *model.MediaFile
	weight float64
	role   string
}

func (s *service) buildRadioContext(ctx context.Context, session model.PersonalRadioSession, items []model.PersonalRadioItem, request model.RefillPersonalRadioRequest) (*radioContext, error) {
	original, err := s.ds.MediaFile(ctx).GetWithParticipants(session.SeedMediaFileID)
	if err != nil {
		return nil, fmt.Errorf("load original radio seed %s: %w", session.SeedMediaFileID, err)
	}
	if original == nil {
		return nil, fmt.Errorf("load original radio seed %s: empty result", session.SeedMediaFileID)
	}

	context := &radioContext{
		OriginalSeed:        original,
		CurrentItemID:       strings.TrimSpace(request.CurrentItemID),
		QueuedItemIDs:       map[string]bool{},
		ClientQueueProvided: request.CurrentItemID != "" || request.QueuedItemIDs != nil,
	}
	validItems := make(map[string]model.PersonalRadioItem, len(items))
	for _, item := range items {
		validItems[item.ID] = item
	}
	if current, ok := validItems[context.CurrentItemID]; ok && current.ItemType != model.RadioItemSeed {
		context.QueuedItemIDs[current.ID] = true
	}
	for _, itemID := range request.QueuedItemIDs {
		itemID = strings.TrimSpace(itemID)
		if itemID == "" {
			continue
		}
		if item, ok := validItems[itemID]; ok && item.ItemType != model.RadioItemSeed {
			context.QueuedItemIDs[itemID] = true
		}
	}

	var accepted []model.PersonalRadioItem
	if accepted, err = s.repo.GetRecentAcceptedItems(session.ID, 4); err != nil {
		return nil, fmt.Errorf("load recent accepted radio items: %w", err)
	}
	var serverCurrent *model.PersonalRadioItem
	if context.CurrentItemID == "" && len(accepted) > 0 {
		// A legacy GET refill has no client position. The newest accepted item
		// is the safest server-side approximation of the current anchor.
		context.CurrentItemID = accepted[0].ID
		serverCurrent = &accepted[0]
	}
	inputs := make([]radioSeedInput, 0, len(accepted)+1)
	inputs = append(inputs, radioSeedInput{file: original, weight: radioSeedWeights[0].weight, role: radioSeedWeights[0].role})
	if current, ok := validItems[context.CurrentItemID]; ok && model.IsAcceptedRadioPlaybackOutcome(current.PlaybackOutcome) {
		if file := s.radioItemMediaFile(ctx, current); file != nil {
			inputs = append(inputs, radioSeedInput{file: file, weight: radioSeedWeights[1].weight, role: radioSeedWeights[1].role})
		}
	} else if serverCurrent != nil {
		if file := s.radioItemMediaFile(ctx, *serverCurrent); file != nil {
			inputs = append(inputs, radioSeedInput{file: file, weight: radioSeedWeights[1].weight, role: radioSeedWeights[1].role})
		}
	}
	acceptedIndex := 0
	for _, item := range accepted {
		if item.ID == context.CurrentItemID {
			continue
		}
		weightIndex := acceptedIndex + 2
		if weightIndex >= len(radioSeedWeights) {
			break
		}
		if file := s.radioItemMediaFile(ctx, item); file != nil {
			inputs = append(inputs, radioSeedInput{file: file, weight: radioSeedWeights[weightIndex].weight, role: radioSeedWeights[weightIndex].role})
			acceptedIndex++
		}
	}
	context.Seeds = weightedRadioSeeds(inputs)
	if len(context.Seeds) == 0 {
		return nil, fmt.Errorf("radio context has no usable seeds")
	}
	return context, nil
}

func (s *service) radioItemMediaFile(ctx context.Context, item model.PersonalRadioItem) *model.MediaFile {
	if item.Song != nil {
		file := *item.Song
		return &file
	}
	if item.MediaFileID == "" {
		return nil
	}
	file, err := s.ds.MediaFile(ctx).GetWithParticipants(item.MediaFileID)
	if err != nil || file == nil {
		return nil
	}
	return file
}

func weightedRadioSeeds(inputs []radioSeedInput) []radioSeed {
	if len(inputs) == 0 {
		return nil
	}
	type weighted struct {
		file   *model.MediaFile
		weight float64
		role   string
	}
	merged := make(map[string]*weighted, len(inputs))
	order := make([]string, 0, len(inputs))
	for _, input := range inputs {
		file := input.file
		if file == nil || file.ID == "" || input.weight <= 0 {
			continue
		}
		key := model.RadioTrackKey(file.MbzRecordingID, file.ID)
		if key == "" {
			continue
		}
		entry, ok := merged[key]
		if !ok {
			copy := *file
			entry = &weighted{file: &copy, role: input.role}
			merged[key] = entry
			order = append(order, key)
		}
		entry.weight += input.weight
	}
	var total float64
	for _, entry := range merged {
		total += entry.weight
	}
	if total <= 0 {
		return nil
	}
	result := make([]radioSeed, 0, len(order))
	for _, key := range order {
		entry := merged[key]
		result = append(result, radioSeed{File: entry.file, Weight: entry.weight / total, Role: entry.role})
	}
	return result
}

func radioContextFromSeed(seed *model.MediaFile) *radioContext {
	if seed == nil {
		return &radioContext{}
	}
	copy := *seed
	return &radioContext{
		OriginalSeed:  &copy,
		Seeds:         []radioSeed{{File: &copy, Weight: 1, Role: "original"}},
		QueuedItemIDs: map[string]bool{},
	}
}

func radioOutstandingItems(items []model.PersonalRadioItem, context *radioContext) int {
	if context == nil || !context.ClientQueueProvided {
		return outstandingRadioItems(items)
	}
	byID := make(map[string]model.PersonalRadioItem, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	count := 0
	counted := map[string]bool{}
	for itemID := range context.QueuedItemIDs {
		item, ok := byID[itemID]
		if !ok || item.ItemType == model.RadioItemSeed || item.Status == model.RadioItemFailed {
			continue
		}
		if itemID == context.CurrentItemID || item.Status == model.RadioItemReady || item.Status == model.RadioItemHeld || item.Status == model.RadioItemDownloading {
			count++
			counted[item.ID] = true
		}
	}
	for _, item := range items {
		if item.ItemType != model.RadioItemSeed && item.Status == model.RadioItemDownloading && !counted[item.ID] {
			count++
		}
	}
	return count
}

func activeRadioItems(items []model.PersonalRadioItem, context *radioContext) []model.PersonalRadioItem {
	if context == nil || !context.ClientQueueProvided {
		result := make([]model.PersonalRadioItem, 0, len(items))
		for _, item := range items {
			if item.ItemType != model.RadioItemSeed && item.Status != model.RadioItemFailed && item.Status != model.RadioItemPlayed {
				result = append(result, item)
			}
		}
		return limitActiveRadioItems(result)
	}
	byID := make(map[string]model.PersonalRadioItem, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	result := make([]model.PersonalRadioItem, 0, len(context.QueuedItemIDs))
	for itemID := range context.QueuedItemIDs {
		item, ok := byID[itemID]
		if ok && item.ItemType != model.RadioItemSeed && item.Status != model.RadioItemFailed &&
			(item.ID == context.CurrentItemID || item.Status != model.RadioItemPlayed) {
			result = append(result, item)
		}
	}
	for _, item := range items {
		if item.ItemType == model.RadioItemSeed || item.Status != model.RadioItemDownloading {
			continue
		}
		found := false
		for _, active := range result {
			if active.ID == item.ID {
				found = true
				break
			}
		}
		if !found {
			result = append(result, item)
		}
	}
	return limitActiveRadioItems(result)
}

func limitActiveRadioItems(items []model.PersonalRadioItem) []model.PersonalRadioItem {
	sort.SliceStable(items, func(left, right int) bool {
		return items[left].Position < items[right].Position
	})
	if len(items) > 20 {
		return items[:20]
	}
	return items
}
