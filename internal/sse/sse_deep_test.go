package sse

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

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
	// Empty event type should still work
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
	// Should handle gracefully (fallback to fmt.Sprintf)
	if err != nil {
		t.Errorf("circular ref should be handled: %v", err)
	}
}

type nonFlushWriter struct {
	http.ResponseWriter
}


