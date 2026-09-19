package nativeapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/navidrome/navidrome/model"
	"github.com/navidrome/navidrome/model/request"
)

type quickPickRouteService struct {
	impressions []model.QuickPickImpressionRequest
}

func (s *quickPickRouteService) Get(context.Context, string) (*model.QuickPickResponse, error) {
	return &model.QuickPickResponse{}, nil
}
func (s *quickPickRouteService) RecordPlaylistPlay(context.Context, string, string) error {
	return nil
}
func (s *quickPickRouteService) RecordImpressions(_ context.Context, _ string, viewID string, itemKeys []string) error {
	s.impressions = append(s.impressions, model.QuickPickImpressionRequest{ViewID: viewID, ItemKeys: itemKeys})
	return nil
}

func TestRecordQuickPickImpressionsRequiresViewID(t *testing.T) {
	service := &quickPickRouteService{}
	api := &Router{quickPick: service}
	req := httptest.NewRequest(http.MethodPost, "/quick-pick/impressions", strings.NewReader(`{"itemKeys":["track:a"]}`))
	req = req.WithContext(request.WithUser(req.Context(), model.User{ID: "user"}))
	response := httptest.NewRecorder()
	api.recordQuickPickImpressions(response, req)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if len(service.impressions) != 0 {
		t.Fatalf("impressions = %#v, want none", service.impressions)
	}
}

func TestRecordQuickPickImpressionsPassesViewAndVisibleItems(t *testing.T) {
	service := &quickPickRouteService{}
	api := &Router{quickPick: service}
	req := httptest.NewRequest(http.MethodPost, "/quick-pick/impressions", strings.NewReader(`{"viewId":"view-1","itemKeys":["track:a","playlist:p"]}`))
	req = req.WithContext(request.WithUser(req.Context(), model.User{ID: "user"}))
	response := httptest.NewRecorder()
	api.recordQuickPickImpressions(response, req)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", response.Code)
	}
	if len(service.impressions) != 1 || service.impressions[0].ViewID != "view-1" || len(service.impressions[0].ItemKeys) != 2 {
		t.Fatalf("captured impressions = %#v", service.impressions)
	}
}
