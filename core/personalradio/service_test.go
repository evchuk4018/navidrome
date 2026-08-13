package personalradio

import (
	"context"
	"testing"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/tests"
)

func TestEarlySkipThreshold(t *testing.T) {
	for _, test := range []struct {
		duration int64
		want     int64
	}{{210000, 30000}, {80000, 16000}, {0, 30000}} {
		if got := earlySkipThresholdMS(test.duration); got != test.want {
			t.Fatalf("earlySkipThresholdMS(%d) = %d, want %d", test.duration, got, test.want)
		}
	}
}

func TestLocalCandidatesStayInSeedGenre(t *testing.T) {
	repo := tests.CreateMockMediaFileRepo()
	repo.SetData(model.MediaFiles{
		{ID: "seed", Artist: "21 Savage", Genre: "Rap"},
		{ID: "rap", Artist: "Future", Genre: "Rap", Annotations: model.Annotations{PlayCount: 10}},
		{ID: "rock", Artist: "Queen", Genre: "Rock", Annotations: model.Annotations{PlayCount: 1000}},
	})
	svc := &service{ds: &tests.MockDataStore{MockedMediaFile: repo}}
	result, err := svc.localCandidates(context.Background(), repo.Data["seed"], map[string]bool{"seed": true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].ID != "rap" {
		t.Fatalf("expected genre-compatible track, got %#v", result)
	}
}

func TestGenreSimilarityAllowsGenreFamilies(t *testing.T) {
	tests := []struct {
		seed      string
		candidate string
		want      bool
	}{
		{seed: "Pop", candidate: "Indie Pop", want: true},
		{seed: "Hip-Hop", candidate: "Rap", want: true},
		{seed: "Rock", candidate: "Metal", want: false},
	}
	for _, test := range tests {
		got := genreSimilarity(test.seed, test.candidate) > 0
		if got != test.want {
			t.Fatalf("genreSimilarity(%q, %q) = %v, want %v", test.seed, test.candidate, got, test.want)
		}
	}
}

func TestLocalCandidatesDoNotLetPopularityOverrideGenre(t *testing.T) {
	repo := tests.CreateMockMediaFileRepo()
	repo.SetData(model.MediaFiles{
		{ID: "seed", Artist: "Seed Artist", Genre: "Pop"},
		{ID: "indie", Artist: "New Artist", Genre: "Indie Pop", Annotations: model.Annotations{PlayCount: 2}},
		{ID: "metal", Artist: "Popular Artist", Genre: "Metal", Annotations: model.Annotations{PlayCount: 100000}},
	})
	svc := &service{ds: &tests.MockDataStore{MockedMediaFile: repo}}
	result, err := svc.localCandidates(context.Background(), repo.Data["seed"], map[string]bool{"seed": true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].ID != "indie" {
		t.Fatalf("expected indie-pop candidate, got %#v", result)
	}
}
