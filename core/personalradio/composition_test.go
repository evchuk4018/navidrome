package personalradio

import (
	"fmt"
	"testing"

	"github.com/navidrome/navidrome/core/recommendations"
	"github.com/navidrome/navidrome/model"
)

func TestComposeRadioCandidatesUsesPersistentModeQuotas(t *testing.T) {
	candidates := make([]rankedRadioCandidate, 0, 20)
	for index := 0; index < 10; index++ {
		candidates = append(candidates,
			newCompositionCandidate(fmt.Sprintf("local-%d", index), false, float64(20-index)),
			newCompositionCandidate(fmt.Sprintf("discovery-%d", index), true, float64(19-index)),
		)
	}
	for _, test := range []struct {
		mode            string
		wantDiscoveries int
	}{
		{mode: model.RadioModeFamiliar, wantDiscoveries: 2},
		{mode: model.RadioModeBalanced, wantDiscoveries: 4},
		{mode: model.RadioModeDiscover, wantDiscoveries: 7},
	} {
		selected := composeRadioCandidates(candidates, radioCompositionOptions{Mode: test.mode, Slots: 10})
		if got := countCompositionDiscoveries(selected); got != test.wantDiscoveries {
			t.Errorf("mode %q selected %d discoveries, want %d", test.mode, got, test.wantDiscoveries)
		}
	}
}

func TestComposeRadioCandidatesUsesActiveQueueForQuota(t *testing.T) {
	active := make([]model.PersonalRadioItem, 0, 8)
	for index := 0; index < 8; index++ {
		active = append(active, model.PersonalRadioItem{
			ID:       fmt.Sprintf("active-%d", index),
			ItemType: model.RadioItemLibrary,
			Status:   model.RadioItemReady,
		})
	}
	candidates := []rankedRadioCandidate{
		newCompositionCandidate("local", false, 10),
		newCompositionCandidate("discovery-1", true, 1),
		newCompositionCandidate("discovery-2", true, 0.5),
	}
	selected := composeRadioCandidates(candidates, radioCompositionOptions{
		Mode:   model.RadioModeBalanced,
		Slots:  2,
		Active: active,
	})
	if got := countCompositionDiscoveries(selected); got != 2 {
		t.Fatalf("active known queue selected %d discoveries, want 2", got)
	}
}

func TestComposeRadioCandidatesPenalizesRepeatedArtistsButPreservesStrongTransitions(t *testing.T) {
	active := []model.PersonalRadioItem{{
		ItemType: model.RadioItemLibrary,
		Status:   model.RadioItemReady,
		Song:     &model.MediaFile{Artist: "Artist A", Album: "Album A"},
	}}
	topSameArtist := newCompositionCandidate("same-artist", false, 10)
	topSameArtist.candidate.MediaFile.Artist = "Artist A"
	topSameArtist.candidate.MediaFile.Album = "Album A"
	otherArtist := newCompositionCandidate("other-artist", false, 9.5)
	otherArtist.candidate.MediaFile.Artist = "Artist B"
	otherArtist.candidate.MediaFile.Album = "Album B"
	selected := composeRadioCandidates([]rankedRadioCandidate{topSameArtist, otherArtist}, radioCompositionOptions{
		Mode:   model.RadioModeFamiliar,
		Slots:  1,
		Active: active,
	})
	if len(selected) != 1 || selected[0].candidate.Key != "other-artist" {
		t.Fatalf("diversity selection = %#v, want other-artist", selected)
	}

	topSameArtist.ranked.Breakdown.TransitionAffinity = 0.9
	selected = composeRadioCandidates([]rankedRadioCandidate{topSameArtist, otherArtist}, radioCompositionOptions{
		Mode:   model.RadioModeFamiliar,
		Slots:  1,
		Active: active,
	})
	if len(selected) != 1 || selected[0].candidate.Key != "same-artist" {
		t.Fatalf("strong transition selection = %#v, want same-artist", selected)
	}
}

func newCompositionCandidate(key string, discovery bool, score float64) rankedRadioCandidate {
	file := model.MediaFile{ID: key, Artist: key, Album: key}
	candidate := recommendations.Candidate{Key: key, MediaFile: file}
	return rankedRadioCandidate{
		candidate:   candidate,
		ranked:      recommendations.RankedCandidate{Candidate: candidate, Score: score},
		isDiscovery: discovery,
	}
}

func countCompositionDiscoveries(candidates []rankedRadioCandidate) int {
	count := 0
	for _, candidate := range candidates {
		if candidate.isDiscovery {
			count++
		}
	}
	return count
}
