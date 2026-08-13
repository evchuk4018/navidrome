package listenbrainz

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/navidrome/navidrome/model"
)

func TestPopularityClientBatchesAndCachesResults(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/1/popularity/recording" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json; charset=UTF-8" {
			t.Errorf("unexpected content type %q", got)
		}

		var request recordingPopularityRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(request.RecordingMBIDs) > popularityBatchSize {
			t.Errorf("request contained %d IDs, want at most %d", len(request.RecordingMBIDs), popularityBatchSize)
		}

		response := make([]recordingPopularityResponse, 0, len(request.RecordingMBIDs))
		for _, recordingMBID := range request.RecordingMBIDs {
			if recordingMBID == "missing" {
				response = append(response, recordingPopularityResponse{RecordingMBID: recordingMBID})
				continue
			}
			listenCount := int64(len(recordingMBID))
			userCount := int64(7)
			response = append(response, recordingPopularityResponse{
				RecordingMBID:    recordingMBID,
				TotalListenCount: &listenCount,
				TotalUserCount:   &userCount,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := newPopularityClient(server.URL+"/1/", server.Client())
	recordingMBIDs := make([]string, 0, popularityBatchSize+2)
	recordingMBIDs = append(recordingMBIDs, " first ", "first")
	for i := 0; i < popularityBatchSize-1; i++ {
		recordingMBIDs = append(recordingMBIDs, "recording-"+string(rune('a'+i%26))+string(rune('0'+i/26)))
	}
	recordingMBIDs = append(recordingMBIDs, "missing")

	popularity, err := client.GetRecordingPopularity(t.Context(), recordingMBIDs)
	if err != nil {
		t.Fatalf("GetRecordingPopularity returned error: %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("expected two batch requests, got %d", requestCount)
	}
	if got := popularity["first"].TotalListenCount; got != int64(len("first")) {
		t.Fatalf("unexpected first listen count %d", got)
	}
	if got := popularity["missing"]; got != (model.RecordingPopularity{}) {
		t.Fatalf("expected missing popularity to be zero, got %#v", got)
	}

	if _, err := client.GetRecordingPopularity(t.Context(), recordingMBIDs); err != nil {
		t.Fatalf("cached GetRecordingPopularity returned error: %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("expected cached lookup to avoid requests, got %d requests", requestCount)
	}
}
