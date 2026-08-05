package websocket

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewHub(t *testing.T) {
	hub := NewHub(50)
	if hub == nil {
		t.Fatal("NewHub returned nil")
	}
	if hub.ConnectionCount() != 0 {
		t.Errorf("expected 0 connections, got %d", hub.ConnectionCount())
	}
}

func TestNewHub_DefaultMaxQueue(t *testing.T) {
	hub := NewHub(0)
	if hub == nil {
		t.Fatal("NewHub returned nil")
	}
	if hub.maxQueue != 100 {
		t.Errorf("expected default maxQueue 100, got %d", hub.maxQueue)
	}
}

func TestHub_RunStop(t *testing.T) {
	hub := NewHub(10)
	go hub.Run()
	time.Sleep(10 * time.Millisecond)
	hub.Stop()
}

func TestHub_BroadcastEmpty(t *testing.T) {
	hub := NewHub(10)
	go hub.Run()
	defer hub.Stop()

	hub.Broadcast(Event{
		Type:      EventTaskUpdated,
		Payload:   map[string]string{"task_id": "t1"},
		Timestamp: time.Now(),
	})
	// No connections — should not block or panic
	time.Sleep(10 * time.Millisecond)
}

func TestHub_ConnectionCount(t *testing.T) {
	hub := NewHub(10)
	go hub.Run()
	defer hub.Stop()

	if hub.ConnectionCount() != 0 {
		t.Errorf("expected 0, got %d", hub.ConnectionCount())
	}
}

func TestMarshalEventJSON(t *testing.T) {
	event := Event{
		Type:      EventTaskCompleted,
		Payload:   map[string]string{"task_id": "t1"},
		Timestamp: time.Now().Truncate(time.Millisecond),
	}
	data, err := MarshalEventJSON(event)
	if err != nil {
		t.Fatalf("MarshalEventJSON failed: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if decoded.Type != EventTaskCompleted {
		t.Errorf("expected type %s, got %s", EventTaskCompleted, decoded.Type)
	}
}

func TestEventTypes(t *testing.T) {
	types := []EventType{
		EventTaskUpdated,
		EventTaskCompleted,
		EventAgentStatus,
		EventAlertTriggered,
	}
	seen := make(map[EventType]bool)
	for _, et := range types {
		if seen[et] {
			t.Errorf("duplicate event type: %s", et)
		}
		seen[et] = true
		if et == "" {
			t.Error("empty event type")
		}
	}
}

func TestConnectionMeta(t *testing.T) {
	meta := ConnectionMeta{
		UserID:    "user-1",
		Channels:  map[string]bool{string(EventTaskUpdated): true},
		CreatedAt: time.Now(),
	}
	if meta.UserID != "user-1" {
		t.Errorf("expected user-1, got %s", meta.UserID)
	}
	if !meta.Channels[string(EventTaskUpdated)] {
		t.Error("expected task.updated channel to be true")
	}
}

func TestClientMessage(t *testing.T) {
	msg := ClientMessage{
		Type:     "subscribe",
		Channels: []string{string(EventTaskUpdated), string(EventAgentStatus)},
		Token:    "test-token",
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var decoded ClientMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if len(decoded.Channels) != 2 {
		t.Errorf("expected 2 channels, got %d", len(decoded.Channels))
	}
}
