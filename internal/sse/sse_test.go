package sse

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNewStreamer(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	if s == nil {
		t.Fatal("NewStreamer should not return nil for ResponseRecorder")
	}
}

func TestSendToken(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	err := s.SendToken("hello")
	if err != nil {
		t.Fatalf("SendToken failed: %v", err)
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: token") {
		t.Error("expected 'event: token' in response")
	}
	if !strings.Contains(body, "hello") {
		t.Error("expected token content in response")
	}
}

func TestSendDone(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	err := s.SendDone(map[string]string{"status": "complete"})
	if err != nil {
		t.Fatalf("SendDone failed: %v", err)
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: done") {
		t.Error("expected 'event: done' in response")
	}
}

func TestSendError(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	err := s.SendError("something went wrong")
	if err != nil {
		t.Fatalf("SendError failed: %v", err)
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: error") {
		t.Error("expected 'event: error' in response")
	}
	if !strings.Contains(body, "something went wrong") {
		t.Error("expected error message in response")
	}
}

func TestSendCritique(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	err := s.SendCritique(map[string]interface{}{"score": 0.85, "grade": "B+"})
	if err != nil {
		t.Fatalf("SendCritique failed: %v", err)
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: critique") {
		t.Error("expected 'event: critique' in response")
	}
}

func TestSendStatus(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	err := s.SendStatus("routing", "selecting model")
	if err != nil {
		t.Fatalf("SendStatus failed: %v", err)
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: status") {
		t.Error("expected 'event: status' in response")
	}
}

func TestClosePreventsSend(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	s.Close()
	err := s.SendToken("test")
	if err == nil {
		t.Error("expected error when sending to closed stream")
	}
}

func TestSendAutoAssignsID(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	s.SendToken("first")
	s.SendToken("second")
	body := w.Body.String()
	if !strings.Contains(body, "id: 1") {
		t.Error("expected id: 1")
	}
	if !strings.Contains(body, "id: 2") {
		t.Error("expected id: 2")
	}
}

func TestSSEHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	s.SendToken("test")
	header := w.Header()
	if header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("expected text/event-stream, got %s", header.Get("Content-Type"))
	}
	if header.Get("Cache-Control") != "no-cache" {
		t.Error("expected no-cache")
	}
}

func TestSendLargePayload(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	bigData := strings.Repeat("x", 10000)
	err := s.SendToken(bigData)
	if err != nil {
		t.Fatalf("SendToken with large payload failed: %v", err)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(bigData)) {
		t.Error("large payload not found in response")
	}
}

func TestConcurrentSend(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			s.SendToken("x")
			done <- true
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestSend_AfterClose(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	s.Close()
	err := s.SendToken("test")
	if err == nil {
		t.Error("send after close should return error")
	}
}

func TestSend_NilData(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	err := s.Send(Event{Data: nil})
	if err != nil {
		t.Errorf("nil data should not error: %v", err)
	}
}

func TestSend_SpecialCharacters(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	err := s.SendToken("line\nwith\nnewlines\x00null")
	if err != nil {
		t.Errorf("special chars should not error: %v", err)
	}
}

func TestSendToken_EmptyString(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	err := s.SendToken("")
	if err != nil {
		t.Errorf("empty token should not error: %v", err)
	}
	if !strings.Contains(w.Body.String(), "event: token") {
		t.Error("should still have event type")
	}
}

func TestSendError_LongMessage(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	longMsg := strings.Repeat("x", 10000)
	err := s.SendError(longMsg)
	if err != nil {
		t.Errorf("long error message should not error: %v", err)
	}
}

func TestSendDone_NilResult(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	err := s.SendDone(nil)
	if err != nil {
		t.Errorf("nil result should not error: %v", err)
	}
}

func TestSendStatus_NilDetail(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	err := s.SendStatus("routing", nil)
	if err != nil {
		t.Errorf("nil detail should not error: %v", err)
	}
}

func TestConcurrentSend_Deep(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.SendToken("x")
		}()
	}
	wg.Wait()
}

func TestNewStreamer_NonFlushing(t *testing.T) {
	w := &nonFlushWriter{httptest.NewRecorder()}
	s := NewStreamer(w)
	if s != nil {
		t.Error("non-flushing writer should return nil streamer")
	}
}

func TestEventID_Increment(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	for i := 0; i < 1000; i++ {
		s.SendToken("t")
	}
	body := w.Body.String()
	if !strings.Contains(body, "id: 1") {
		t.Error("expected id: 1")
	}
	if !strings.Contains(body, "id: 1000") {
		t.Error("expected id: 1000")
	}
}

func TestSend_PreservesID(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	err := s.Send(Event{ID: "custom-42", Data: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.Body.String(), "id: custom-42") {
		t.Error("custom ID should be preserved")
	}
}

func TestSend_EmptyEventType(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	err := s.Send(Event{Data: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(w.Body.String(), "id: 1") {
		t.Error("should have event ID")
	}
}

func TestDoubleClose(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	s.Close()
	s.Close() // should not panic
}

func TestSend_CircularReference(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	m := make(map[string]interface{})
	m["self"] = m
	err := s.Send(Event{Data: m})
	if err != nil {
		t.Errorf("circular ref should be handled: %v", err)
	}
}

func TestSend_StreamLifetimeExceeded(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	s.mu.Lock()
	s.createdAt = time.Now().Add(-10 * time.Minute)
	s.mu.Unlock()
	err := s.SendToken("test")
	if err == nil {
		t.Error("expected error for stream lifetime exceeded")
	}
}

func TestSend_MapInterfaceKey(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	m := map[interface{}]interface{}{1: "one", "two": 2}
	err := s.Send(Event{Data: m})
	if err != nil {
		t.Errorf("should handle map[interface{}]: %v", err)
	}
}

func TestSend_SliceData(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	err := s.Send(Event{Data: []interface{}{"a", "b", "c"}})
	if err != nil {
		t.Errorf("slice data should not error: %v", err)
	}
}

func TestSend_StringData(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	err := s.Send(Event{Data: "plain string"})
	if err != nil {
		t.Errorf("string data should not error: %v", err)
	}
	if !strings.Contains(w.Body.String(), "plain string") {
		t.Error("string data should appear in output")
	}
}

func TestSend_NilEvent(t *testing.T) {
	w := httptest.NewRecorder()
	s := NewStreamer(w)
	err := s.Send(Event{})
	if err != nil {
		t.Errorf("empty event should not error: %v", err)
	}
}

type nonFlushWriter struct {
	http.ResponseWriter
}
