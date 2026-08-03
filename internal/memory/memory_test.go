package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

type testTransport struct {
	serverURL string
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = "http"
	req.URL.Host = t.serverURL
	return http.DefaultTransport.RoundTrip(req)
}

func startTestServer(t *testing.T, code int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code)
		fmt.Fprint(w, body)
	}))
	return srv
}

func TestOpenAIEmbedder_NameAndDimensions(t *testing.T) {
	e := NewOpenAIEmbedder("test-key")
	if e.Name() != "openai" {
		t.Errorf("expected openai, got %s", e.Name())
	}
	if e.Dimensions() != 1536 {
		t.Errorf("expected 1536, got %d", e.Dimensions())
	}
}

func TestOpenAIEmbedder_Embed_NoServer(t *testing.T) {
	e := NewOpenAIEmbedder("test-key")
	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Error("expected error connecting to nonexistent server")
	}
}

func TestOpenAIEmbedder_Embed_ErrorResponse(t *testing.T) {
	srv := startTestServer(t, 500, `{"error":"bad"}`)
	defer srv.Close()

	e := &OpenAIEmbedder{
		apiKey:     "test-key",
		model:      "text-embedding-3-small",
		dimensions: 1536,
		httpClient: &http.Client{},
	}
	e.httpClient.Transport = &testTransport{serverURL: srv.Listener.Addr().String()}
	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestOpenAIEmbedder_Embed_EmptyData(t *testing.T) {
	srv := startTestServer(t, 200, `{"data":[],"usage":{"prompt_tokens":0,"total_tokens":0}}`)
	defer srv.Close()

	e := &OpenAIEmbedder{
		apiKey:     "test-key",
		model:      "text-embedding-3-small",
		dimensions: 1536,
		httpClient: &http.Client{},
	}
	e.httpClient.Transport = &testTransport{serverURL: srv.Listener.Addr().String()}
	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestOpenAIEmbedder_Embed_BadJSON(t *testing.T) {
	srv := startTestServer(t, 200, `not json`)
	defer srv.Close()

	e := &OpenAIEmbedder{
		apiKey:     "test-key",
		model:      "text-embedding-3-small",
		dimensions: 1536,
		httpClient: &http.Client{},
	}
	e.httpClient.Transport = &testTransport{serverURL: srv.Listener.Addr().String()}
	_, err := e.Embed(context.Background(), "test")
	if err == nil {
		t.Error("expected error for bad JSON")
	}
}

func TestOpenAIEmbedder_Embed_Success(t *testing.T) {
	resp := `{"data":[{"embedding":[0.1,0.2,0.3],"index":0}],"usage":{"prompt_tokens":1,"total_tokens":1}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		fmt.Fprint(w, resp)
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	e := &OpenAIEmbedder{
		apiKey:     "test-key",
		model:      "text-embedding-3-small",
		dimensions: 3,
		httpClient: &http.Client{},
	}
	e.httpClient.Transport = &testTransport{serverURL: u.Host}
	vec, err := e.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 3 || vec[0] != 0.1 || vec[1] != 0.2 || vec[2] != 0.3 {
		t.Errorf("unexpected vector: %v", vec)
	}
}

func TestWorkingMemory_EmptySearch(t *testing.T) {
	wm := NewWorkingMemory(0)
	results := wm.Search("anything", 10)
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestWorkingMemory_GetReturnsCopy(t *testing.T) {
	wm := NewWorkingMemory(0)
	wm.Add(Message{Role: "user", Content: "hello"})
	msgs := wm.Get()
	msgs[0].Content = "modified"
	orig := wm.Get()
	if orig[0].Content != "hello" {
		t.Error("Get should return a copy")
	}
}

func TestWorkingMemory_SearchCaseInsensitive(t *testing.T) {
	wm := NewWorkingMemory(0)
	wm.Add(Message{Role: "user", Content: "HELLO WORLD"})
	results := wm.Search("hello", 10)
	if len(results) != 1 {
		t.Errorf("expected 1, got %d", len(results))
	}
}

func TestWorkingMemory_SearchNoMatch(t *testing.T) {
	wm := NewWorkingMemory(0)
	wm.Add(Message{Role: "user", Content: "hello"})
	results := wm.Search("xyz", 10)
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestRedisBackedWorkingMemory_NilRedis(t *testing.T) {
	wm := NewRedisBackedWorkingMemory(nil, "session1", 10*time.Minute)
	wm.Add(Message{Role: "user", Content: "hello"})
	if wm.Count() != 1 {
		t.Errorf("expected 1, got %d", wm.Count())
	}
	msgs := wm.Get()
	if len(msgs) != 1 {
		t.Errorf("expected 1, got %d", len(msgs))
	}
}

func TestRedisBackedWorkingMemory_EmptySessionID(t *testing.T) {
	wm := NewRedisBackedWorkingMemory(nil, "", 10*time.Minute)
	if wm.Count() != 0 {
		t.Error("expected 0 messages")
	}
}

func TestWorkingMemoryKey(t *testing.T) {
	key := workingMemoryKey("sess123")
	if key != "vigilagent:working_memory:sess123" {
		t.Errorf("unexpected key: %s", key)
	}
}

func TestWorkingMemory_TimestampSet(t *testing.T) {
	wm := NewWorkingMemory(0)
	wm.Add(Message{Role: "user", Content: "hello"})
	msgs := wm.Get()
	if msgs[0].Timestamp.IsZero() {
		t.Error("expected timestamp to be set")
	}
}

func TestProceduralStore_SearchLimit(t *testing.T) {
	s := NewProceduralStore()
	for i := 0; i < 5; i++ {
		s.Store(context.Background(), &Workflow{ID: string(rune('a' + i)), Name: "deploy"})
	}
	results, _ := s.Search(context.Background(), "deploy", 2)
	if len(results) != 2 {
		t.Errorf("expected 2, got %d", len(results))
	}
}

func TestProceduralStore_SearchDescription(t *testing.T) {
	s := NewProceduralStore()
	s.Store(context.Background(), &Workflow{ID: "w1", Name: "task", Description: "deploy pipeline"})
	s.Store(context.Background(), &Workflow{ID: "w2", Name: "other", Description: "nothing"})
	results, _ := s.Search(context.Background(), "pipeline", 10)
	if len(results) != 1 {
		t.Errorf("expected 1, got %d", len(results))
	}
}

func TestProceduralStore_ListByUserLimit(t *testing.T) {
	s := NewProceduralStore()
	for i := 0; i < 5; i++ {
		s.Store(context.Background(), &Workflow{ID: string(rune('a' + i)), UserID: "u1", Name: "w"})
	}
	results, _ := s.ListByUser(context.Background(), "u1", 2)
	if len(results) != 2 {
		t.Errorf("expected 2, got %d", len(results))
	}
}

func TestManager_AddWorkingMessage(t *testing.T) {
	m := NewManagerWithEmbedder(nil, NewNoOpEmbedder(4))
	m.initWorkingMemory()
	m.AddWorkingMessage("user", "hello", 5)
	working := m.working.Load().(*WorkingMemory)
	if working.Count() != 1 {
		t.Errorf("expected 1, got %d", working.Count())
	}
}

func TestManager_GetWorkingMessages(t *testing.T) {
	m := NewManagerWithEmbedder(nil, NewNoOpEmbedder(4))
	m.AddWorkingMessage("user", "hello", 5)
	m.AddWorkingMessage("assistant", "hi", 3)
	msgs := m.GetWorkingMessages()
	if len(msgs) != 2 {
		t.Errorf("expected 2, got %d", len(msgs))
	}
}

func TestManager_ClearWorkingMemory(t *testing.T) {
	m := NewManagerWithEmbedder(nil, NewNoOpEmbedder(4))
	m.initWorkingMemory()
	m.AddWorkingMessage("user", "hello", 5)
	m.ClearWorkingMemory()
	working := m.working.Load().(*WorkingMemory)
	if working.Count() != 0 {
		t.Error("expected 0 after clear")
	}
}

func TestManager_Recall_WorkingOnly(t *testing.T) {
	m := NewManagerWithEmbedder(nil, NewNoOpEmbedder(4))
	m.episodic = nil
	m.semantic = nil
	m.AddWorkingMessage("user", "fix auth", 5)
	results, err := m.Recall(context.Background(), "auth", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].Type != "working" {
		t.Errorf("expected working, got %s", results[0].Type)
	}
	if results[0].Score != 0.9 {
		t.Errorf("expected 0.9, got %v", results[0].Score)
	}
}

func TestManager_Recall_Empty(t *testing.T) {
	m := NewManagerWithEmbedder(nil, NewNoOpEmbedder(4))
	m.episodic = nil
	m.semantic = nil
	results, err := m.Recall(context.Background(), "anything", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestManager_SearchMemory_FilterByType(t *testing.T) {
	m := NewManagerWithEmbedder(nil, NewNoOpEmbedder(4))
	m.episodic = nil
	m.semantic = nil
	m.AddWorkingMessage("user", "fix auth", 5)
	results, err := m.SearchMemory(context.Background(), "auth", []string{"episodic"}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 (type filter excludes working), got %d", len(results))
	}
}

func TestManager_SearchMemory_FilterByScore(t *testing.T) {
	m := NewManagerWithEmbedder(nil, NewNoOpEmbedder(4))
	m.episodic = nil
	m.semantic = nil
	m.AddWorkingMessage("user", "fix auth", 5)
	results, err := m.SearchMemory(context.Background(), "auth", nil, 10, 0.95)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 (score below min), got %d", len(results))
	}
}

func TestMemoryResult_JSON(t *testing.T) {
	r := MemoryResult{
		Type:    "working",
		Content: "hello",
		Title:   "t",
		Score:   0.9,
		Metadata: map[string]interface{}{
			"role": "user",
		},
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var r2 MemoryResult
	if err := json.Unmarshal(data, &r2); err != nil {
		t.Fatal(err)
	}
	if r2.Type != "working" || r2.Content != "hello" || r2.Score != 0.9 {
		t.Errorf("roundtrip mismatch: %+v", r2)
	}
}

func TestEpisodicMemory_JSON(t *testing.T) {
	mem := EpisodicMemory{
		ID:          "ep1",
		UserID:      "u1",
		EpisodeType: "decision",
		Title:       "chose stack",
		Content:     "used Go",
		Importance:  0.8,
		Tags:        []string{"go", "stack"},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	data, err := json.Marshal(mem)
	if err != nil {
		t.Fatal(err)
	}
	var mem2 EpisodicMemory
	if err := json.Unmarshal(data, &mem2); err != nil {
		t.Fatal(err)
	}
	if mem2.ID != "ep1" || mem2.Title != "chose stack" {
		t.Errorf("roundtrip mismatch: %+v", mem2)
	}
}

func TestPattern_JSON(t *testing.T) {
	p := Pattern{
		ID:          "p1",
		UserID:      "u1",
		ProjectID:   "proj1",
		PatternType: "arch",
		Name:        "mvc pattern",
		Description: "model-view-controller",
		Confidence:  0.9,
		Examples:    []string{"a", "b"},
		FilePatterns: []string{"*.go"},
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var p2 Pattern
	if err := json.Unmarshal(data, &p2); err != nil {
		t.Fatal(err)
	}
	if p2.ID != "p1" || p2.Name != "mvc pattern" {
		t.Errorf("roundtrip mismatch: %+v", p2)
	}
}

func TestWorkflow_JSON(t *testing.T) {
	wf := Workflow{
		ID:          "w1",
		UserID:      "u1",
		Name:        "deploy",
		Description: "deploy workflow",
		Steps: []WorkflowStep{
			{Action: "build", Description: "build binary", Tool: "go"},
		},
		SuccessRate: 0.95,
		UsageCount:  10,
		CreatedAt:   time.Now(),
	}
	data, err := json.Marshal(wf)
	if err != nil {
		t.Fatal(err)
	}
	var wf2 Workflow
	if err := json.Unmarshal(data, &wf2); err != nil {
		t.Fatal(err)
	}
	if wf2.ID != "w1" || wf2.Name != "deploy" || len(wf2.Steps) != 1 {
		t.Errorf("roundtrip mismatch: %+v", wf2)
	}
}

func TestWorkflowStep_JSON(t *testing.T) {
	step := WorkflowStep{
		Action:      "deploy",
		Description: "deploy to prod",
		Tool:        "kubectl",
		Params:      map[string]interface{}{"namespace": "default"},
	}
	data, err := json.Marshal(step)
	if err != nil {
		t.Fatal(err)
	}
	var step2 WorkflowStep
	if err := json.Unmarshal(data, &step2); err != nil {
		t.Fatal(err)
	}
	if step2.Action != "deploy" || step2.Tool != "kubectl" {
		t.Errorf("roundtrip mismatch: %+v", step2)
	}
}

func TestManager_EnableRedisBacking_NilRedis(t *testing.T) {
	m := NewManagerWithEmbedder(nil, NewNoOpEmbedder(4))
	m.AddWorkingMessage("user", "hello", 5)
	m.EnableRedisBacking(nil, "session1")
	msgs := m.GetWorkingMessages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1, got %d", len(msgs))
	}
}

func TestManager_EnableRedisBacking_EmptySession(t *testing.T) {
	m := NewManagerWithEmbedder(nil, NewNoOpEmbedder(4))
	m.initWorkingMemory()
	m.EnableRedisBacking(nil, "")
	working := m.working.Load().(*WorkingMemory)
	if working.Count() != 0 {
		t.Error("expected 0 messages")
	}
}

func TestManager_Recall_EarlyReturnFromWorking(t *testing.T) {
	m := NewManagerWithEmbedder(nil, NewNoOpEmbedder(4))
	m.episodic = nil
	m.semantic = nil
	m.AddWorkingMessage("user", "fix auth bug", 5)
	m.AddWorkingMessage("assistant", "done", 3)
	results, err := m.Recall(context.Background(), "auth", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

func TestManager_SearchMemory_TypeMatches(t *testing.T) {
	m := NewManagerWithEmbedder(nil, NewNoOpEmbedder(4))
	m.episodic = nil
	m.semantic = nil
	m.AddWorkingMessage("user", "fix auth", 5)
	results, err := m.SearchMemory(context.Background(), "auth", []string{"working"}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].Type != "working" {
		t.Errorf("expected working, got %s", results[0].Type)
	}
}

func TestManager_SearchMemory_TypeMatchLimit(t *testing.T) {
	m := NewManagerWithEmbedder(nil, NewNoOpEmbedder(4))
	m.episodic = nil
	m.semantic = nil
	m.AddWorkingMessage("user", "fix auth", 5)
	m.AddWorkingMessage("assistant", "done", 3)
	results, err := m.SearchMemory(context.Background(), "auth", []string{"working"}, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

func TestManager_SearchMemory_EmptyTypes(t *testing.T) {
	m := NewManagerWithEmbedder(nil, NewNoOpEmbedder(4))
	m.episodic = nil
	m.semantic = nil
	m.AddWorkingMessage("user", "fix auth", 5)
	results, err := m.SearchMemory(context.Background(), "auth", []string{}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
}

func TestManager_SearchMemory_NoResults(t *testing.T) {
	m := NewManagerWithEmbedder(nil, NewNoOpEmbedder(4))
	m.episodic = nil
	m.semantic = nil
	results, err := m.SearchMemory(context.Background(), "anything", nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestManager_SearchMemory_MultipleTypesOneMatch(t *testing.T) {
	m := NewManagerWithEmbedder(nil, NewNoOpEmbedder(4))
	m.episodic = nil
	m.semantic = nil
	m.AddWorkingMessage("user", "fix auth", 5)
	results, err := m.SearchMemory(context.Background(), "auth", []string{"episodic", "working"}, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].Type != "working" {
		t.Errorf("expected working, got %s", results[0].Type)
	}
}

func TestWorkingMemory_LoadFromRedis_NilRedis(t *testing.T) {
	wm := &WorkingMemory{messages: make([]Message, 0)}
	wm.loadFromRedis()
	if wm.Count() != 0 {
		t.Error("expected 0 messages")
	}
}

func TestWorkingMemory_LoadFromRedis_EmptySession(t *testing.T) {
	wm := &WorkingMemory{messages: make([]Message, 0), sessionID: ""}
	wm.loadFromRedis()
	if wm.Count() != 0 {
		t.Error("expected 0 messages")
	}
}

func TestWorkingMemory_PersistToRedis_NilRedis(t *testing.T) {
	wm := &WorkingMemory{messages: make([]Message, 0)}
	wm.persistToRedis()
}

func TestWorkingMemory_PersistToRedis_EmptySession(t *testing.T) {
	wm := &WorkingMemory{messages: make([]Message, 0), sessionID: ""}
	wm.persistToRedis()
}

func TestProceduralStore_StoreSetsCreatedAt(t *testing.T) {
	s := NewProceduralStore()
	wf := &Workflow{ID: "w1", Name: "test"}
	s.Store(context.Background(), wf)
	got, _ := s.Get(context.Background(), "w1")
	if got.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}
}

func TestProceduralStore_SearchNoMatch(t *testing.T) {
	s := NewProceduralStore()
	s.Store(context.Background(), &Workflow{ID: "w1", Name: "deploy"})
	results, _ := s.Search(context.Background(), "xyz", 10)
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestProceduralStore_ListByUserNoMatch(t *testing.T) {
	s := NewProceduralStore()
	s.Store(context.Background(), &Workflow{ID: "w1", UserID: "u1", Name: "w"})
	results, _ := s.ListByUser(context.Background(), "u2", 10)
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestManager_Recall_WorkingMetadata(t *testing.T) {
	m := NewManagerWithEmbedder(nil, NewNoOpEmbedder(4))
	m.episodic = nil
	m.semantic = nil
	m.AddWorkingMessage("user", "fix auth", 5)
	results, _ := m.Recall(context.Background(), "auth", 10)
	if results[0].Metadata["role"] != "user" {
		t.Errorf("expected role=user, got %v", results[0].Metadata["role"])
	}
}

func TestManager_NewManagerWithEmbedder_NilEmbedder(t *testing.T) {
	m := NewManagerWithEmbedder(nil, nil)
	if m.embedder == nil {
		t.Fatal("expected fallback embedder")
	}
	if m.embedder.Name() != "noop" {
		t.Errorf("expected noop, got %s", m.embedder.Name())
	}
}

func TestManager_NewManagerWithEmbedder_NilPool(t *testing.T) {
	m := NewManagerWithEmbedder(nil, NewNoOpEmbedder(4))
	if m.episodic == nil {
		t.Fatal("expected episodic store")
	}
	if m.semantic == nil {
		t.Fatal("expected semantic store")
	}
}

func TestWorkingMemory_SearchLimitZero(t *testing.T) {
	wm := NewWorkingMemory(0)
	wm.Add(Message{Role: "user", Content: "hello"})
	results := wm.Search("hello", 0)
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestWorkingMemory_TokenCountEmpty(t *testing.T) {
	wm := NewWorkingMemory(0)
	if wm.TokenCount() != 0 {
		t.Errorf("expected 0, got %d", wm.TokenCount())
	}
}

func TestManager_Recall_EmbedFallback(t *testing.T) {
	m := NewManagerWithEmbedder(nil, NewNoOpEmbedder(4))
	m.episodic = nil
	m.semantic = nil
	results, err := m.Recall(context.Background(), "query", 10)
	if err != nil {
		t.Fatal(err)
	}
	if results != nil {
		t.Errorf("expected nil, got %v", results)
	}
}
