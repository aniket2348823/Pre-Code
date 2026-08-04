package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vigilagent/vigilagent/internal/auth"
)

func reqWithClaims(req *http.Request, userID string) *http.Request {
	claims := &auth.Claims{UserID: userID, Role: "user"}
	return req.WithContext(auth.ContextWithClaims(req.Context(), claims))
}

func TestNewHITLQueue(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()
	assert.NotNil(t, q)
	assert.NotNil(t, q.pending)
}

func TestHITLQueue_SubmitAndDecide(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()

	entry := &HITLCheckpointEntry{
		ID:          "cp-1",
		TaskID:      "task-1",
		UserID:      "user-1",
		OrgID:       "org-1",
		Description: "Delete production database?",
		Tool:        "terminal",
	}

	done := make(chan error, 1)
	go func() {
		result, err := q.Submit(context.Background(), entry)
		if err != nil {
			done <- err
			return
		}
		assert.Equal(t, HITLApprove, result.Decision)
		done <- nil
	}()

	time.Sleep(10 * time.Millisecond)

	err := q.Decide(context.Background(), "cp-1", HITLApprove, "")
	require.NoError(t, err)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for Submit to return")
	}
}

func TestHITLQueue_SubmitReject(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()

	entry := &HITLCheckpointEntry{
		ID:     "cp-2",
		TaskID: "task-2",
		UserID: "user-1",
	}

	done := make(chan error, 1)
	go func() {
		result, err := q.Submit(context.Background(), entry)
		if err != nil {
			done <- err
			return
		}
		assert.Equal(t, HITLReject, result.Decision)
		done <- nil
	}()

	time.Sleep(10 * time.Millisecond)
	err := q.Decide(context.Background(), "cp-2", HITLReject, "")
	require.NoError(t, err)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestHITLQueue_SubmitModify(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()

	entry := &HITLCheckpointEntry{
		ID:     "cp-3",
		TaskID: "task-3",
		UserID: "user-1",
	}

	done := make(chan *HITLCheckpointEntry, 1)
	go func() {
		result, err := q.Submit(context.Background(), entry)
		if err == nil {
			done <- result
		} else {
			done <- nil
		}
	}()

	time.Sleep(10 * time.Millisecond)
	err := q.Decide(context.Background(), "cp-3", HITLModify, `{"command":"ls"}`)
	require.NoError(t, err)

	select {
	case result := <-done:
		require.NotNil(t, result)
		assert.Equal(t, HITLDecision("modify"), result.Decision)
		assert.Equal(t, `{"command":"ls"}`, result.ModifiedData)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestHITLQueue_Timeout(t *testing.T) {
	q := NewHITLQueue(nil, 50*time.Millisecond)
	defer q.Close()

	entry := &HITLCheckpointEntry{
		ID:     "cp-timeout",
		TaskID: "task-timeout",
		UserID: "user-1",
	}

	result, err := q.Submit(context.Background(), entry)
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestHITLQueue_ContextCancel(t *testing.T) {
	q := NewHITLQueue(nil, 10*time.Second)
	defer q.Close()

	entry := &HITLCheckpointEntry{
		ID:     "cp-ctx",
		TaskID: "task-ctx",
		UserID: "user-1",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result, err := q.Submit(ctx, entry)
	assert.Nil(t, result)
	assert.Error(t, err)
}

func TestHITLQueue_DecideNotFound(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()

	err := q.Decide(context.Background(), "nonexistent", HITLApprove, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestHITLQueue_DoubleDecide(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()

	entry := &HITLCheckpointEntry{
		ID:     "cp-double",
		TaskID: "task-double",
		UserID: "user-1",
	}

	go func() {
		q.Submit(context.Background(), entry)
	}()
	time.Sleep(10 * time.Millisecond)

	err := q.Decide(context.Background(), "cp-double", HITLApprove, "")
	assert.NoError(t, err)

	err = q.Decide(context.Background(), "cp-double", HITLReject, "")
	assert.Error(t, err, "second decide should fail")
}

func TestHITLQueue_GetPending(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()

	entry1 := &HITLCheckpointEntry{ID: "cp-a", TaskID: "task-1", UserID: "user-1"}
	entry2 := &HITLCheckpointEntry{ID: "cp-b", TaskID: "task-2", UserID: "user-1"}
	entry3 := &HITLCheckpointEntry{ID: "cp-c", TaskID: "task-3", UserID: "user-2"}

	go q.Submit(context.Background(), entry1)
	go q.Submit(context.Background(), entry2)
	go q.Submit(context.Background(), entry3)
	time.Sleep(10 * time.Millisecond)

	pending := q.GetPending("user-1")
	assert.Len(t, pending, 2)

	pending = q.GetPending("user-2")
	assert.Len(t, pending, 1)

	pending = q.GetPending("user-nonexistent")
	assert.Empty(t, pending)
}

func TestHITLQueue_GetPendingByTask(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()

	entry1 := &HITLCheckpointEntry{ID: "cp-x", TaskID: "task-shared", UserID: "user-1"}
	entry2 := &HITLCheckpointEntry{ID: "cp-y", TaskID: "task-shared", UserID: "user-2"}
	entry3 := &HITLCheckpointEntry{ID: "cp-z", TaskID: "task-other", UserID: "user-1"}

	go q.Submit(context.Background(), entry1)
	go q.Submit(context.Background(), entry2)
	go q.Submit(context.Background(), entry3)
	time.Sleep(10 * time.Millisecond)

	pending := q.GetPendingByTask("task-shared")
	assert.Len(t, pending, 2)

	pending = q.GetPendingByTask("task-other")
	assert.Len(t, pending, 1)

	pending = q.GetPendingByTask("task-nonexistent")
	assert.Empty(t, pending)
}

func TestHITLQueue_SetCallback(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()

	var called atomic.Bool
	q.SetCallback(func(entry *HITLCheckpointEntry) {
		called.Store(true)
	})

	entry := &HITLCheckpointEntry{ID: "cp-cb", TaskID: "task-cb", UserID: "user-1"}
	go q.Submit(context.Background(), entry)
	time.Sleep(10 * time.Millisecond)
	q.Decide(context.Background(), "cp-cb", HITLApprove, "")
	time.Sleep(50 * time.Millisecond)

	assert.True(t, called.Load(), "callback should have been invoked")
}

func TestHITLQueue_GetPendingEmpty(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()

	pending := q.GetPending("any-user")
	assert.Empty(t, pending)
}

// --- HITLHandler Tests ---

func TestHITLHandler_ListPending(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()
	h := NewHITLHandler(q)

	entry := &HITLCheckpointEntry{
		ID:          "cp-list",
		TaskID:      "task-list",
		UserID:      "user-list",
		Description: "Test?",
	}
	go q.Submit(context.Background(), entry)
	time.Sleep(10 * time.Millisecond)

	req := reqWithClaims(httptest.NewRequest("GET", "/hitl/pending", nil), "user-list")
	w := httptest.NewRecorder()
	h.ListPendingHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	assert.Equal(t, float64(1), body["count"])
}

func TestHITLHandler_ListPending_MissingUserID(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()
	h := NewHITLHandler(q)

	req := httptest.NewRequest("GET", "/hitl/pending", nil)
	w := httptest.NewRecorder()
	h.ListPendingHandler(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHITLHandler_ListPending_Empty(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()
	h := NewHITLHandler(q)

	req := reqWithClaims(httptest.NewRequest("GET", "/hitl/pending", nil), "nobody")
	w := httptest.NewRecorder()
	h.ListPendingHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	assert.Equal(t, float64(0), body["count"])
}

func TestHITLHandler_DecideHandler_Success(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()
	h := NewHITLHandler(q)

	entry := &HITLCheckpointEntry{
		ID:     "cp-decide",
		TaskID: "task-decide",
		UserID: "user-decide",
	}
	go q.Submit(context.Background(), entry)
	time.Sleep(10 * time.Millisecond)

	body := `{"checkpoint_id":"cp-decide","decision":"approve"}`
	req := reqWithClaims(httptest.NewRequest("POST", "/hitl/decide", bytes.NewBufferString(body)), "user-decide")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.DecideHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHITLHandler_DecideHandler_MissingUserID(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()
	h := NewHITLHandler(q)

	body := `{"checkpoint_id":"cp-1","decision":"approve"}`
	req := httptest.NewRequest("POST", "/hitl/decide", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.DecideHandler(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHITLHandler_DecideHandler_InvalidBody(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()
	h := NewHITLHandler(q)

	req := reqWithClaims(httptest.NewRequest("POST", "/hitl/decide", bytes.NewBufferString("not json")), "user1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.DecideHandler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHITLHandler_DecideHandler_MissingCheckpointID(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()
	h := NewHITLHandler(q)

	body := `{"decision":"approve"}`
	req := reqWithClaims(httptest.NewRequest("POST", "/hitl/decide", bytes.NewBufferString(body)), "user1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.DecideHandler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHITLHandler_DecideHandler_InvalidDecision(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()
	h := NewHITLHandler(q)

	body := `{"checkpoint_id":"cp-1","decision":"invalid"}`
	req := reqWithClaims(httptest.NewRequest("POST", "/hitl/decide", bytes.NewBufferString(body)), "user1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.DecideHandler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHITLHandler_DecideHandler_NotFound(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()
	h := NewHITLHandler(q)

	body := `{"checkpoint_id":"nonexistent","decision":"approve"}`
	req := reqWithClaims(httptest.NewRequest("POST", "/hitl/decide", bytes.NewBufferString(body)), "user1")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.DecideHandler(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHITLHandler_StatusHandler(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()
	h := NewHITLHandler(q)

	entry := &HITLCheckpointEntry{
		ID:     "cp-status",
		TaskID: "task-status",
		UserID: "user-status",
	}
	go q.Submit(context.Background(), entry)
	time.Sleep(10 * time.Millisecond)

	req := httptest.NewRequest("GET", "/hitl/status?id=cp-status", nil)
	w := httptest.NewRecorder()
	h.StatusHandler(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHITLHandler_StatusHandler_MissingID(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()
	h := NewHITLHandler(q)

	req := httptest.NewRequest("GET", "/hitl/status", nil)
	w := httptest.NewRecorder()
	h.StatusHandler(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHITLHandler_StatusHandler_NotFound(t *testing.T) {
	q := NewHITLQueue(nil, 5*time.Second)
	defer q.Close()
	h := NewHITLHandler(q)

	req := httptest.NewRequest("GET", "/hitl/status?id=nonexistent", nil)
	w := httptest.NewRecorder()
	h.StatusHandler(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHITLDecision_Constants(t *testing.T) {
	assert.Equal(t, HITLDecision("approve"), HITLApprove)
	assert.Equal(t, HITLDecision("reject"), HITLReject)
	assert.Equal(t, HITLDecision("modify"), HITLModify)
}
