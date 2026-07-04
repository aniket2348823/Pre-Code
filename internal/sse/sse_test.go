package sse

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"
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
