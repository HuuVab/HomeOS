package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStatusReturnsRealScopedSamples(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	response := httptest.NewRecorder()
	NewHandler("").ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	var payload statusResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Status != "healthy" || len(payload.Metrics) < 1 {
		t.Fatalf("unexpected status response: %+v", payload)
	}
	for _, sample := range payload.Metrics {
		if sample.Scope != "application" || sample.State != "available" || sample.SampledAt.IsZero() {
			t.Errorf("invalid metric: %+v", sample)
		}
	}
}

func TestHealthEndpoints(t *testing.T) {
	for _, path := range []string{"/api/v1/health/live", "/api/v1/health/ready"} {
		response := httptest.NewRecorder()
		NewHandler("").ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Errorf("%s returned %d", path, response.Code)
		}
	}
}
