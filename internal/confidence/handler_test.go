package confidence

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandler_Score(t *testing.T) {
	handler := NewHTTPHandler(NewEngine())

	body, _ := json.Marshal(ScoreRequest{
		Evidence: []Evidence{
			{Source: "schema", Verdict: "pass", Weight: 1.0},
			{Source: "scan", Verdict: "fail", Severity: "critical", Weight: 0.8},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var score Score
	json.Unmarshal(w.Body.Bytes(), &score)
	if score.Confidence < 0 || score.Confidence > 1 {
		t.Errorf("confidence out of range: %f", score.Confidence)
	}
	if score.Grade == "" {
		t.Error("grade should not be empty")
	}
	if score.Reason == "" {
		t.Error("reason should not be empty")
	}
	if score.Passed != 1 {
		t.Errorf("expected 1 passed, got %d", score.Passed)
	}
	if score.Failed != 1 {
		t.Errorf("expected 1 failed, got %d", score.Failed)
	}
}

func TestHandler_Score_Empty(t *testing.T) {
	handler := NewHTTPHandler(NewEngine())

	body, _ := json.Marshal(ScoreRequest{Evidence: []Evidence{}})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var score Score
	json.Unmarshal(w.Body.Bytes(), &score)
	if score.Confidence != 1.0 {
		t.Errorf("empty evidence should yield confidence=1.0, got %f", score.Confidence)
	}
}

func TestHandler_Score_AllPass(t *testing.T) {
	handler := NewHTTPHandler(NewEngine())

	body, _ := json.Marshal(ScoreRequest{
		Evidence: []Evidence{
			{Source: "schema", Verdict: "pass", Weight: 1.0},
			{Source: "requirements", Verdict: "pass", Weight: 1.0},
			{Source: "compliance", Verdict: "pass", Weight: 1.0},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var score Score
	json.Unmarshal(w.Body.Bytes(), &score)
	if score.Grade != "A+" {
		t.Errorf("expected grade A+ for all-pass, got %q", score.Grade)
	}
}

func TestHandler_InvalidJSON(t *testing.T) {
	handler := NewHTTPHandler(NewEngine())

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("bad json")))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
