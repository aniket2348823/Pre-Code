package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/vigilagent/vigilagent/internal/config"
)

func newDisconnectedNATS() *nats.Conn {
	nc, _ := nats.Connect("nats://127.0.0.1:1",
		nats.RetryOnFailedConnect(true),
		nats.MaxReconnects(0),
		nats.Timeout(1*time.Millisecond),
	)
	return nc
}

// --- Mock implementations ---

type mockMsg struct {
	data    []byte
	headers nats.Header
	ackErr  error
	nakErr  error

	mu       sync.Mutex
	acked    bool
	naked    bool
	nakDelay time.Duration
}

func newMockMsg(payload interface{}) *mockMsg {
	data, _ := json.Marshal(payload)
	return &mockMsg{
		data:    data,
		headers: nats.Header{},
	}
}

func newMockMsgRaw(data []byte) *mockMsg {
	return &mockMsg{
		data:    data,
		headers: nats.Header{},
	}
}

func (m *mockMsg) Metadata() (*jetstream.MsgMetadata, error) { return nil, nil }
func (m *mockMsg) Data() []byte                              { return m.data }
func (m *mockMsg) Headers() nats.Header {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.headers
}
func (m *mockMsg) Subject() string                 { return "test.subject" }
func (m *mockMsg) Reply() string                   { return "test.reply" }
func (m *mockMsg) DoubleAck(context.Context) error { return nil }
func (m *mockMsg) InProgress() error               { return nil }
func (m *mockMsg) Term() error                     { return nil }
func (m *mockMsg) TermWithReason(string) error     { return nil }

func (m *mockMsg) Ack() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.acked = true
	return m.ackErr
}

func (m *mockMsg) Nak() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.naked = true
	return m.nakErr
}

func (m *mockMsg) NakWithDelay(d time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.naked = true
	m.nakDelay = d
	return m.nakErr
}

func (m *mockMsg) isAcked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.acked
}

func (m *mockMsg) isNaked() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.naked
}

func (m *mockMsg) getNakDelay() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nakDelay
}

type mockMessageBatch struct {
	msgs chan jetstream.Msg
	err  error
}

func (b *mockMessageBatch) Messages() <-chan jetstream.Msg { return b.msgs }
func (b *mockMessageBatch) Error() error                   { return b.err }

type mockConsumer struct {
	fetchFunc func(batch int, opts ...jetstream.FetchOpt) (jetstream.MessageBatch, error)
}

func (c *mockConsumer) Fetch(batch int, opts ...jetstream.FetchOpt) (jetstream.MessageBatch, error) {
	return c.fetchFunc(batch, opts...)
}
func (c *mockConsumer) FetchBytes(int, ...jetstream.FetchOpt) (jetstream.MessageBatch, error) {
	return nil, fmt.Errorf("not implemented")
}
func (c *mockConsumer) FetchNoWait(int) (jetstream.MessageBatch, error) {
	return nil, fmt.Errorf("not implemented")
}
func (c *mockConsumer) Consume(jetstream.MessageHandler, ...jetstream.PullConsumeOpt) (jetstream.ConsumeContext, error) {
	return nil, fmt.Errorf("not implemented")
}
func (c *mockConsumer) Messages(...jetstream.PullMessagesOpt) (jetstream.MessagesContext, error) {
	return nil, fmt.Errorf("not implemented")
}
func (c *mockConsumer) Next(...jetstream.FetchOpt) (jetstream.Msg, error) {
	return nil, fmt.Errorf("not implemented")
}
func (c *mockConsumer) Info(context.Context) (*jetstream.ConsumerInfo, error) {
	return nil, fmt.Errorf("not implemented")
}
func (c *mockConsumer) CachedInfo() *jetstream.ConsumerInfo { return nil }

type mockJetStream struct {
	publishFunc                func(ctx context.Context, subject string, payload []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
	createOrUpdateStreamFunc   func(ctx context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error)
	createOrUpdateConsumerFunc func(ctx context.Context, stream string, cfg jetstream.ConsumerConfig) (jetstream.Consumer, error)
}

func (m *mockJetStream) AccountInfo(context.Context) (*jetstream.AccountInfo, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) Conn() *nats.Conn { return nil }
func (m *mockJetStream) Options() jetstream.JetStreamOptions {
	return jetstream.JetStreamOptions{}
}
func (m *mockJetStream) Publish(ctx context.Context, subject string, payload []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	if m.publishFunc != nil {
		return m.publishFunc(ctx, subject, payload, opts...)
	}
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) PublishMsg(context.Context, *nats.Msg, ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) PublishAsync(string, []byte, ...jetstream.PublishOpt) (jetstream.PubAckFuture, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) PublishMsgAsync(*nats.Msg, ...jetstream.PublishOpt) (jetstream.PubAckFuture, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) PublishAsyncPending() int { return 0 }
func (m *mockJetStream) PublishAsyncComplete() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}
func (m *mockJetStream) CleanupPublisher() {}
func (m *mockJetStream) CreateStream(context.Context, jetstream.StreamConfig) (jetstream.Stream, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) UpdateStream(context.Context, jetstream.StreamConfig) (jetstream.Stream, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) CreateOrUpdateStream(ctx context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error) {
	if m.createOrUpdateStreamFunc != nil {
		return m.createOrUpdateStreamFunc(ctx, cfg)
	}
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) Stream(context.Context, string) (jetstream.Stream, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) StreamNameBySubject(context.Context, string) (string, error) {
	return "", fmt.Errorf("not implemented")
}
func (m *mockJetStream) DeleteStream(context.Context, string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockJetStream) ListStreams(context.Context, ...jetstream.StreamListOpt) jetstream.StreamInfoLister {
	return nil
}
func (m *mockJetStream) StreamNames(context.Context, ...jetstream.StreamListOpt) jetstream.StreamNameLister {
	return nil
}
func (m *mockJetStream) CreateOrUpdateConsumer(ctx context.Context, stream string, cfg jetstream.ConsumerConfig) (jetstream.Consumer, error) {
	if m.createOrUpdateConsumerFunc != nil {
		return m.createOrUpdateConsumerFunc(ctx, stream, cfg)
	}
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) CreateConsumer(context.Context, string, jetstream.ConsumerConfig) (jetstream.Consumer, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) UpdateConsumer(context.Context, string, jetstream.ConsumerConfig) (jetstream.Consumer, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) OrderedConsumer(context.Context, string, jetstream.OrderedConsumerConfig) (jetstream.Consumer, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) Consumer(context.Context, string, string) (jetstream.Consumer, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) DeleteConsumer(context.Context, string, string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockJetStream) PauseConsumer(context.Context, string, string, time.Time) (*jetstream.ConsumerPauseResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) ResumeConsumer(context.Context, string, string) (*jetstream.ConsumerPauseResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) ResetConsumer(context.Context, string, string) (*jetstream.ConsumerResetResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) ResetConsumerToSequence(context.Context, string, string, uint64) (*jetstream.ConsumerResetResponse, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) CreateOrUpdatePushConsumer(context.Context, string, jetstream.ConsumerConfig) (jetstream.PushConsumer, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) CreatePushConsumer(context.Context, string, jetstream.ConsumerConfig) (jetstream.PushConsumer, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) UpdatePushConsumer(context.Context, string, jetstream.ConsumerConfig) (jetstream.PushConsumer, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) PushConsumer(context.Context, string, string) (jetstream.PushConsumer, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) KeyValue(context.Context, string) (jetstream.KeyValue, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) CreateKeyValue(context.Context, jetstream.KeyValueConfig) (jetstream.KeyValue, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) UpdateKeyValue(context.Context, jetstream.KeyValueConfig) (jetstream.KeyValue, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) CreateOrUpdateKeyValue(context.Context, jetstream.KeyValueConfig) (jetstream.KeyValue, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) DeleteKeyValue(context.Context, string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockJetStream) KeyValueStoreNames(context.Context) jetstream.KeyValueNamesLister {
	return nil
}
func (m *mockJetStream) KeyValueStores(context.Context) jetstream.KeyValueLister {
	return nil
}
func (m *mockJetStream) ObjectStore(context.Context, string) (jetstream.ObjectStore, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) CreateObjectStore(context.Context, jetstream.ObjectStoreConfig) (jetstream.ObjectStore, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) UpdateObjectStore(context.Context, jetstream.ObjectStoreConfig) (jetstream.ObjectStore, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) CreateOrUpdateObjectStore(context.Context, jetstream.ObjectStoreConfig) (jetstream.ObjectStore, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockJetStream) DeleteObjectStore(context.Context, string) error {
	return fmt.Errorf("not implemented")
}
func (m *mockJetStream) ObjectStoreNames(context.Context) jetstream.ObjectStoreNamesLister {
	return nil
}
func (m *mockJetStream) ObjectStores(context.Context) jetstream.ObjectStoresLister {
	return nil
}

// --- Tests for existing functionality ---

func TestDefaultWorkerConfig(t *testing.T) {
	cfg := DefaultWorkerConfig()
	if cfg.Stream != "vigilagent" {
		t.Fatalf("expected stream 'vigilagent', got %q", cfg.Stream)
	}
	if cfg.Subject != "tasks.execute" {
		t.Fatalf("expected subject 'tasks.execute', got %q", cfg.Subject)
	}
	if cfg.MaxRetries != 3 {
		t.Fatalf("expected max retries 3, got %d", cfg.MaxRetries)
	}
	if cfg.AckWait != 60*time.Second {
		t.Fatalf("expected ack wait 60s, got %v", cfg.AckWait)
	}
	if cfg.MaxDeliver != 4 {
		t.Fatalf("expected max deliver 4, got %d", cfg.MaxDeliver)
	}
}

func TestTaskPayloadSerialization(t *testing.T) {
	payload := TaskPayload{
		TaskID:        "task-123",
		ProjectID:     "proj-456",
		UserID:        "user-789",
		Prompt:        "Fix the auth bug",
		MaxTokens:     4096,
		MaxIterations: 20,
		Tags:          []string{"bugfix", "auth"},
		Priority:      1,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded TaskPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.TaskID != payload.TaskID {
		t.Fatalf("task ID mismatch: %q vs %q", decoded.TaskID, payload.TaskID)
	}
	if decoded.Prompt != payload.Prompt {
		t.Fatalf("prompt mismatch: %q vs %q", decoded.Prompt, payload.Prompt)
	}
	if decoded.MaxTokens != payload.MaxTokens {
		t.Fatalf("max tokens mismatch: %d vs %d", decoded.MaxTokens, payload.MaxTokens)
	}
	if len(decoded.Tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(decoded.Tags))
	}
}

func TestTaskPayloadMinimal(t *testing.T) {
	payload := TaskPayload{
		TaskID:    "task-min",
		ProjectID: "proj-min",
		UserID:    "user-min",
		Prompt:    "hello",
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}
	if _, ok := m["tags"]; ok {
		t.Fatal("tags should be omitted when nil")
	}
}

func TestWorkerConfigCustom(t *testing.T) {
	cfg := WorkerConfig{
		Stream:     "custom-stream",
		Subject:    "custom.subject",
		MaxRetries: 5,
		AckWait:    30 * time.Second,
		MaxDeliver: 10,
	}

	if cfg.Stream != "custom-stream" {
		t.Fatalf("expected custom stream, got %q", cfg.Stream)
	}
	if cfg.MaxRetries != 5 {
		t.Fatalf("expected 5 retries, got %d", cfg.MaxRetries)
	}
}

func TestTaskPayload_NilPayload(t *testing.T) {
	payload := TaskPayload{TaskID: "t1", ProjectID: "p1", UserID: "u1", Prompt: "test"}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded TaskPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.TaskID != "t1" {
		t.Errorf("expected t1, got %s", decoded.TaskID)
	}
}

func TestTaskPayload_EmptyPayload(t *testing.T) {
	payload := TaskPayload{TaskID: "t1", Prompt: ""}
	data, _ := json.Marshal(payload)
	var decoded TaskPayload
	json.Unmarshal(data, &decoded)
	if decoded.TaskID != "t1" {
		t.Error("task ID mismatch")
	}
}

func TestTaskPayload_NestedMap(t *testing.T) {
	payload := TaskPayload{
		TaskID: "t1",
		Prompt: "test prompt",
		Tags:   []string{"scan", "security"},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded TaskPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
}

func TestTaskPayload_EmptyTaskID(t *testing.T) {
	payload := TaskPayload{TaskID: "", Prompt: "test"}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded TaskPayload
	json.Unmarshal(data, &decoded)
	if decoded.Prompt != "test" {
		t.Error("prompt mismatch")
	}
}

func TestTaskPayload_EmptyTaskType(t *testing.T) {
	payload := TaskPayload{TaskID: "t1", Prompt: "test"}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded TaskPayload
	json.Unmarshal(data, &decoded)
	if decoded.TaskID != "t1" {
		t.Error("task ID mismatch")
	}
}

func TestWorkerConfig_ZeroConcurrency(t *testing.T) {
	cfg := WorkerConfig{MaxRetries: 3, MaxDeliver: 4}
	if cfg.MaxRetries != 3 {
		t.Errorf("expected 3, got %d", cfg.MaxRetries)
	}
}

func TestWorkerConfig_NegativeConcurrency(t *testing.T) {
	cfg := WorkerConfig{MaxRetries: -1}
	if cfg.MaxRetries != -1 {
		t.Error("negative retries should be stored")
	}
}

func TestWorkerConfig_ZeroMaxRetries(t *testing.T) {
	cfg := WorkerConfig{MaxRetries: 0}
	if cfg.MaxRetries != 0 {
		t.Error("zero retries should be stored")
	}
}

func TestDefaultWorkerConfigDeep(t *testing.T) {
	cfg := DefaultWorkerConfig()
	if cfg.Stream != "vigilagent" {
		t.Errorf("expected vigilagent, got %s", cfg.Stream)
	}
	if cfg.Subject != "tasks.execute" {
		t.Errorf("expected tasks.execute, got %s", cfg.Subject)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("expected 3 retries, got %d", cfg.MaxRetries)
	}
}

// --- Tests for processMessage ---

func TestProcessMessage_Success(t *testing.T) {
	payload := TaskPayload{TaskID: "t1", ProjectID: "p1", UserID: "u1", Prompt: "do stuff"}
	msg := newMockMsg(payload)

	var called int
	w := &TaskWorker{
		handler: func(ctx context.Context, p TaskPayload) error {
			called++
			if p.TaskID != "t1" {
				t.Errorf("expected task ID t1, got %s", p.TaskID)
			}
			return nil
		},
		maxRetries: 3,
	}

	w.processMessage(context.Background(), msg)

	if !msg.isAcked() {
		t.Error("expected message to be acked")
	}
	if called != 1 {
		t.Errorf("expected handler called once, got %d", called)
	}
}

func TestProcessMessage_HandlerError(t *testing.T) {
	payload := TaskPayload{TaskID: "t2", ProjectID: "p1", UserID: "u1", Prompt: "fail"}
	msg := newMockMsg(payload)

	w := &TaskWorker{
		handler: func(ctx context.Context, p TaskPayload) error {
			return errors.New("handler failed")
		},
		maxRetries: 3,
	}

	w.processMessage(context.Background(), msg)

	if msg.isAcked() {
		t.Error("should not ack on handler error")
	}
	if !msg.isNaked() {
		t.Error("expected nak on handler error")
	}
	if msg.getNakDelay() != 5*time.Second {
		t.Errorf("expected nak delay 5s, got %v", msg.getNakDelay())
	}
}

func TestProcessMessage_PanicRecovery(t *testing.T) {
	payload := TaskPayload{TaskID: "t3", ProjectID: "p1", UserID: "u1", Prompt: "panic"}
	msg := newMockMsg(payload)

	w := &TaskWorker{
		handler: func(ctx context.Context, p TaskPayload) error {
			panic("kaboom")
		},
		maxRetries: 3,
	}

	w.processMessage(context.Background(), msg)

	if !msg.isNaked() {
		t.Error("expected nak after panic to trigger retry with backoff")
	}
}

func TestProcessMessage_BadJSON(t *testing.T) {
	msg := newMockMsgRaw([]byte("{{{{invalid json"))

	w := &TaskWorker{
		handler:    func(ctx context.Context, p TaskPayload) error { return nil },
		maxRetries: 3,
	}

	w.processMessage(context.Background(), msg)

	if msg.isAcked() {
		t.Error("should not ack on bad JSON")
	}
	if !msg.isNaked() {
		t.Error("expected nak on bad JSON")
	}
}

func TestProcessMessage_HandlerReturnsContextError(t *testing.T) {
	payload := TaskPayload{TaskID: "t4", ProjectID: "p1", UserID: "u1", Prompt: "ctx"}
	msg := newMockMsg(payload)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	w := &TaskWorker{
		handler: func(ctx context.Context, p TaskPayload) error {
			return ctx.Err()
		},
		maxRetries: 3,
	}

	w.processMessage(ctx, msg)

	if msg.isAcked() {
		t.Error("should not ack when handler returns error")
	}
	if !msg.isNaked() {
		t.Error("expected nak when handler returns error")
	}
}

// --- Tests for Stop ---

func TestStop(t *testing.T) {
	w := &TaskWorker{
		stream:  "test",
		subject: "test.sub",
	}
	w.Stop()
}

// --- Tests for Start ---

func TestStart_ProcessesMessages(t *testing.T) {
	payload := TaskPayload{TaskID: "t5", ProjectID: "p1", UserID: "u1", Prompt: "start test"}

	msgCh := make(chan jetstream.Msg, 1)
	msgCh <- newMockMsg(payload)
	close(msgCh)

	consumer := &mockConsumer{
		fetchFunc: func(batch int, opts ...jetstream.FetchOpt) (jetstream.MessageBatch, error) {
			return &mockMessageBatch{msgs: msgCh}, nil
		},
	}

	var handlerCalled int
	w := &TaskWorker{
		consumer: consumer,
		stream:   "test",
		subject:  "test.sub",
		handler: func(ctx context.Context, p TaskPayload) error {
			handlerCalled++
			return nil
		},
		maxRetries: 3,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- w.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	err := <-done
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handlerCalled != 1 {
		t.Errorf("expected handler called once, got %d", handlerCalled)
	}
}

func TestStart_ContextCancelledImmediately(t *testing.T) {
	consumer := &mockConsumer{
		fetchFunc: func(batch int, opts ...jetstream.FetchOpt) (jetstream.MessageBatch, error) {
			t.Fatal("should not fetch after context cancel")
			return nil, nil
		},
	}

	w := &TaskWorker{
		consumer:   consumer,
		stream:     "test",
		subject:    "test.sub",
		handler:    func(ctx context.Context, p TaskPayload) error { return nil },
		maxRetries: 3,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := w.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStart_FetchErrorThenContextCancel(t *testing.T) {
	callCount := 0
	consumer := &mockConsumer{
		fetchFunc: func(batch int, opts ...jetstream.FetchOpt) (jetstream.MessageBatch, error) {
			callCount++
			if callCount == 1 {
				return nil, errors.New("transient error")
			}
			<-make(chan struct{})
			return nil, nil
		},
	}

	w := &TaskWorker{
		consumer:   consumer,
		stream:     "test",
		subject:    "test.sub",
		handler:    func(ctx context.Context, p TaskPayload) error { return nil },
		maxRetries: 3,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- w.Start(ctx)
	}()

	time.Sleep(150 * time.Millisecond)
	cancel()

	err := <-done
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStart_FetchErrorThenImmediateCancel(t *testing.T) {
	callCount := 0
	consumer := &mockConsumer{
		fetchFunc: func(batch int, opts ...jetstream.FetchOpt) (jetstream.MessageBatch, error) {
			callCount++
			if callCount == 1 {
				return nil, errors.New("transient error")
			}
			return nil, context.Canceled
		},
	}

	w := &TaskWorker{
		consumer:   consumer,
		stream:     "test",
		subject:    "test.sub",
		handler:    func(ctx context.Context, p TaskPayload) error { return nil },
		maxRetries: 3,
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	err := w.Start(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStart_ProcessesMultipleMessages(t *testing.T) {
	payload1 := TaskPayload{TaskID: "t6", ProjectID: "p1", UserID: "u1", Prompt: "first"}
	payload2 := TaskPayload{TaskID: "t7", ProjectID: "p1", UserID: "u1", Prompt: "second"}

	msgCh := make(chan jetstream.Msg, 2)
	msgCh <- newMockMsg(payload1)
	msgCh <- newMockMsg(payload2)
	close(msgCh)

	consumer := &mockConsumer{
		fetchFunc: func(batch int, opts ...jetstream.FetchOpt) (jetstream.MessageBatch, error) {
			return &mockMessageBatch{msgs: msgCh}, nil
		},
	}

	var mu sync.Mutex
	var taskIDs []string
	w := &TaskWorker{
		consumer: consumer,
		stream:   "test",
		subject:  "test.sub",
		handler: func(ctx context.Context, p TaskPayload) error {
			mu.Lock()
			defer mu.Unlock()
			taskIDs = append(taskIDs, p.TaskID)
			return nil
		},
		maxRetries: 3,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- w.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	<-done
	mu.Lock()
	defer mu.Unlock()
	if len(taskIDs) != 2 {
		t.Errorf("expected 2 tasks processed, got %d", len(taskIDs))
	}
}

func TestStart_BatchWithNoMessages(t *testing.T) {
	msgCh := make(chan jetstream.Msg)
	close(msgCh)

	consumer := &mockConsumer{
		fetchFunc: func(batch int, opts ...jetstream.FetchOpt) (jetstream.MessageBatch, error) {
			return &mockMessageBatch{msgs: msgCh}, nil
		},
	}

	w := &TaskWorker{
		consumer:   consumer,
		stream:     "test",
		subject:    "test.sub",
		handler:    func(ctx context.Context, p TaskPayload) error { return nil },
		maxRetries: 3,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- w.Start(ctx)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	err := <-done
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Tests for PublishTask ---

func TestPublishTask_Success(t *testing.T) {
	var publishedSubject string
	var publishedData []byte

	js := &mockJetStream{
		publishFunc: func(ctx context.Context, subject string, payload []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
			publishedSubject = subject
			publishedData = payload
			return &jetstream.PubAck{}, nil
		},
	}

	w := &TaskWorker{
		nats:    &NATS{JS: js},
		stream:  "vigilagent",
		subject: "tasks.execute",
	}

	payload := TaskPayload{TaskID: "t8", ProjectID: "p1", UserID: "u1", Prompt: "publish test"}
	err := w.PublishTask(context.Background(), payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if publishedSubject != "vigilagent.tasks.execute" {
		t.Errorf("expected subject vigilagent.tasks.execute, got %s", publishedSubject)
	}

	var decoded TaskPayload
	if err := json.Unmarshal(publishedData, &decoded); err != nil {
		t.Fatalf("failed to unmarshal published data: %v", err)
	}
	if decoded.TaskID != "t8" {
		t.Errorf("expected task ID t8, got %s", decoded.TaskID)
	}
}

func TestPublishTask_PublishError(t *testing.T) {
	js := &mockJetStream{
		publishFunc: func(ctx context.Context, subject string, payload []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
			return nil, errors.New("nats: connection closed")
		},
	}

	w := &TaskWorker{
		nats:    &NATS{JS: js},
		stream:  "test",
		subject: "test.sub",
	}

	payload := TaskPayload{TaskID: "t9", ProjectID: "p1", UserID: "u1", Prompt: "fail"}
	err := w.PublishTask(context.Background(), payload)
	if err == nil {
		t.Fatal("expected error from publish")
	}
}

func TestPublishTask_ContextCancellation(t *testing.T) {
	js := &mockJetStream{
		publishFunc: func(ctx context.Context, subject string, payload []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
			return nil, ctx.Err()
		},
	}

	w := &TaskWorker{
		nats:    &NATS{JS: js},
		stream:  "test",
		subject: "test.sub",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	payload := TaskPayload{TaskID: "t15", ProjectID: "p1", UserID: "u1", Prompt: "cancelled"}
	err := w.PublishTask(ctx, payload)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// --- Tests for NewTaskWorker ---

func TestNewTaskWorker_Success(t *testing.T) {
	js := &mockJetStream{
		createOrUpdateStreamFunc: func(ctx context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error) {
			return nil, nil
		},
		createOrUpdateConsumerFunc: func(ctx context.Context, stream string, cfg jetstream.ConsumerConfig) (jetstream.Consumer, error) {
			return &mockConsumer{
				fetchFunc: func(batch int, opts ...jetstream.FetchOpt) (jetstream.MessageBatch, error) {
					return &mockMessageBatch{msgs: make(chan jetstream.Msg)}, nil
				},
			}, nil
		},
	}

	natsConn := &NATS{JS: js}
	cfg := DefaultWorkerConfig()
	handler := func(ctx context.Context, p TaskPayload) error { return nil }

	w, err := NewTaskWorker(natsConn, cfg, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w == nil {
		t.Fatal("expected non-nil worker")
	}
	if w.stream != cfg.Stream {
		t.Errorf("expected stream %s, got %s", cfg.Stream, w.stream)
	}
	if w.subject != cfg.Subject {
		t.Errorf("expected subject %s, got %s", cfg.Subject, w.subject)
	}
	if w.maxRetries != cfg.MaxRetries {
		t.Errorf("expected maxRetries %d, got %d", cfg.MaxRetries, w.maxRetries)
	}
}

func TestNewTaskWorker_StreamError(t *testing.T) {
	js := &mockJetStream{
		createOrUpdateStreamFunc: func(ctx context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error) {
			return nil, errors.New("stream creation failed")
		},
	}

	natsConn := &NATS{JS: js}
	cfg := DefaultWorkerConfig()
	handler := func(ctx context.Context, p TaskPayload) error { return nil }

	_, err := NewTaskWorker(natsConn, cfg, handler)
	if err == nil {
		t.Fatal("expected error from stream creation")
	}
}

func TestNewTaskWorker_ConsumerError(t *testing.T) {
	js := &mockJetStream{
		createOrUpdateStreamFunc: func(ctx context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error) {
			return nil, nil
		},
		createOrUpdateConsumerFunc: func(ctx context.Context, stream string, cfg jetstream.ConsumerConfig) (jetstream.Consumer, error) {
			return nil, errors.New("consumer creation failed")
		},
	}

	natsConn := &NATS{JS: js}
	cfg := DefaultWorkerConfig()
	handler := func(ctx context.Context, p TaskPayload) error { return nil }

	_, err := NewTaskWorker(natsConn, cfg, handler)
	if err == nil {
		t.Fatal("expected error from consumer creation")
	}
}

// --- Tests for NATS methods ---

func TestNATS_DrainNilConn(t *testing.T) {
	n := &NATS{Conn: nil}
	err := n.Drain(context.Background())
	if err != nil {
		t.Fatalf("expected nil error for nil conn, got %v", err)
	}
}

func TestNATS_CloseNilConn(t *testing.T) {
	n := &NATS{Conn: nil}
	n.Close()
}

func TestNATS_HealthCheckDisconnected(t *testing.T) {
	nc := newDisconnectedNATS()
	if nc == nil {
		t.Skip("could not create disconnected connection")
	}
	defer nc.Close()

	n := &NATS{Conn: nc}
	err := n.HealthCheck()
	if err == nil {
		t.Error("expected error for disconnected NATS")
	}
}

func TestNATS_HealthCheckConnected(t *testing.T) {
	t.Skip("requires running NATS server")
}

func TestNATS_CloseDisconnected(t *testing.T) {
	nc := newDisconnectedNATS()
	if nc == nil {
		t.Skip("could not create disconnected connection")
	}

	n := &NATS{Conn: nc}
	n.Close() // should not panic
}

func TestNATS_DrainDisconnectedTimeout(t *testing.T) {
	nc := newDisconnectedNATS()
	if nc == nil {
		t.Skip("could not create disconnected connection")
	}
	defer nc.Close()

	n := &NATS{Conn: nc}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := n.Drain(ctx)
	if err == nil {
		t.Error("expected timeout error from drain on disconnected conn")
	}
}

func TestNewNATS_StreamCreationError(t *testing.T) {
	cfg := &config.NATSConfig{
		URL:    "nats://127.0.0.1:1",
		Stream: "test-stream",
	}

	n, err := NewNATS(cfg)
	if err != nil {
		// nats.Connect with RetryOnFailedConnect(true) returns conn even if unreachable
		// but CreateOrUpdateStream will fail → error returned
		t.Logf("NewNATS failed as expected: %v", err)
		return
	}
	// If we got a conn, it's because connect succeeded but stream creation failed
	if n != nil {
		n.Close()
		t.Error("expected error from NewNATS with unreachable server")
	}
}

func TestNATS_HealthCheckAfterClose(t *testing.T) {
	nc := newDisconnectedNATS()
	if nc == nil {
		t.Skip("could not create disconnected connection")
	}

	n := &NATS{Conn: nc}
	n.Close()

	err := n.HealthCheck()
	if err == nil {
		t.Error("expected error for closed NATS")
	}
}

func TestNATS_DrainSuccess(t *testing.T) {
	nc := newDisconnectedNATS()
	if nc == nil {
		t.Skip("could not create disconnected connection")
	}

	n := &NATS{Conn: nc}
	err := n.Drain(context.Background())
	// drain on disconnected conn should either error or succeed quickly
	if err != nil {
		t.Logf("drain returned error (expected): %v", err)
	}
}

// waitForNak polls until the mock message has been nacked, failing after a
// timeout. Used by Start tests whose nak happens in a background goroutine.
func waitForNak(t *testing.T, msg *mockMsg) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if msg.isNaked() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("message was not nacked within timeout")
}

func TestTaskHandlerType(t *testing.T) {
	var h TaskHandler = func(ctx context.Context, p TaskPayload) error {
		return nil
	}
	if h == nil {
		t.Fatal("handler should not be nil")
	}
}

// --- Edge cases ---

func TestProcessMessage_EmptyHandlerReturn(t *testing.T) {
	payload := TaskPayload{TaskID: "t10", ProjectID: "p1", UserID: "u1", Prompt: "edge"}
	msg := newMockMsg(payload)

	w := &TaskWorker{
		handler:    func(ctx context.Context, p TaskPayload) error { return nil },
		maxRetries: 0,
	}

	w.processMessage(context.Background(), msg)

	if !msg.isAcked() {
		t.Error("expected ack")
	}
}

func TestProcessMessage_HandlerReturnsSpecificError(t *testing.T) {
	payload := TaskPayload{TaskID: "t11", ProjectID: "p1", UserID: "u1", Prompt: "specific"}
	msg := newMockMsg(payload)

	w := &TaskWorker{
		handler: func(ctx context.Context, p TaskPayload) error {
			return errors.New("rate limited")
		},
		maxRetries: 5,
	}

	w.processMessage(context.Background(), msg)

	if msg.isAcked() {
		t.Error("should not ack on error")
	}
	if !msg.isNaked() {
		t.Error("expected nak")
	}
}

func TestProcessMessage_PanicWithString(t *testing.T) {
	payload := TaskPayload{TaskID: "t12", ProjectID: "p1", UserID: "u1", Prompt: "panic str"}
	msg := newMockMsg(payload)

	w := &TaskWorker{
		handler: func(ctx context.Context, p TaskPayload) error {
			panic("string panic value")
		},
		maxRetries: 3,
	}

	w.processMessage(context.Background(), msg)

	if !msg.isNaked() {
		t.Error("expected nak after panic")
	}
}

func TestProcessMessage_PanicWithNil(t *testing.T) {
	payload := TaskPayload{TaskID: "t13", ProjectID: "p1", UserID: "u1", Prompt: "panic nil"}
	msg := newMockMsg(payload)

	w := &TaskWorker{
		handler: func(ctx context.Context, p TaskPayload) error {
			panic(nil)
		},
		maxRetries: 3,
	}

	w.processMessage(context.Background(), msg)

	if !msg.isNaked() {
		t.Error("expected nak after nil panic")
	}
}

func TestProcessMessage_PanicWithError(t *testing.T) {
	payload := TaskPayload{TaskID: "t14", ProjectID: "p1", UserID: "u1", Prompt: "panic err"}
	msg := newMockMsg(payload)

	w := &TaskWorker{
		handler: func(ctx context.Context, p TaskPayload) error {
			panic(errors.New("panic error"))
		},
		maxRetries: 3,
	}

	w.processMessage(context.Background(), msg)

	if !msg.isNaked() {
		t.Error("expected nak after error panic")
	}
}

func TestStartHandlerContextCancelled(t *testing.T) {
	payload := TaskPayload{TaskID: "ctx-cancel", ProjectID: "p1", UserID: "u1", Prompt: "ctx cancel"}

	msgCh := make(chan jetstream.Msg, 1)
	msgCh <- newMockMsg(payload)
	close(msgCh)

	consumer := &mockConsumer{
		fetchFunc: func(batch int, opts ...jetstream.FetchOpt) (jetstream.MessageBatch, error) {
			return &mockMessageBatch{msgs: msgCh}, nil
		},
	}

	handlerCalled := make(chan struct{})
	w := &TaskWorker{
		consumer: consumer,
		stream:   "test",
		subject:  "test.sub",
		handler: func(ctx context.Context, p TaskPayload) error {
			close(handlerCalled)
			return nil
		},
		maxRetries: 3,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- w.Start(ctx)
	}()

	// Wait deterministically for the handler to run, then cancel and assert
	// the worker exits cleanly. No sleeps — immune to scheduler load.
	select {
	case <-handlerCalled:
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("handler was not called within timeout")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWorkerConfig_AllFields(t *testing.T) {
	cfg := WorkerConfig{
		Stream:     "my-stream",
		Subject:    "my.subject",
		MaxRetries: 10,
		AckWait:    120 * time.Second,
		MaxDeliver: 20,
	}
	if cfg.Stream != "my-stream" {
		t.Errorf("stream mismatch")
	}
	if cfg.Subject != "my.subject" {
		t.Errorf("subject mismatch")
	}
	if cfg.MaxRetries != 10 {
		t.Errorf("max retries mismatch")
	}
	if cfg.AckWait != 120*time.Second {
		t.Errorf("ack wait mismatch")
	}
	if cfg.MaxDeliver != 20 {
		t.Errorf("max deliver mismatch")
	}
}

func TestTaskPayload_AllFields(t *testing.T) {
	p := TaskPayload{
		TaskID:        "id",
		ProjectID:     "pid",
		UserID:        "uid",
		Prompt:        "prompt",
		MaxTokens:     100,
		MaxIterations: 5,
		Tags:          []string{"a"},
		Priority:      2,
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var decoded TaskPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.TaskID != "id" || decoded.ProjectID != "pid" || decoded.UserID != "uid" || decoded.Prompt != "prompt" || decoded.MaxTokens != 100 || decoded.MaxIterations != 5 || decoded.Priority != 2 || len(decoded.Tags) != 1 {
		t.Errorf("field mismatch after round-trip")
	}
}

func TestStart_HandlerErrorNaksMessage(t *testing.T) {
	payload := TaskPayload{TaskID: "err-msg", ProjectID: "p1", UserID: "u1", Prompt: "err"}

	msg := newMockMsg(payload)
	msgCh := make(chan jetstream.Msg, 1)
	msgCh <- msg
	close(msgCh)

	consumer := &mockConsumer{
		fetchFunc: func(batch int, opts ...jetstream.FetchOpt) (jetstream.MessageBatch, error) {
			return &mockMessageBatch{msgs: msgCh}, nil
		},
	}

	w := &TaskWorker{
		consumer: consumer,
		stream:   "test",
		subject:  "test.sub",
		handler: func(ctx context.Context, p TaskPayload) error {
			return errors.New("fail")
		},
		maxRetries: 3,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- w.Start(ctx)
	}()

	// Wait for the background processMessage goroutine to nak the message
	// before cancelling, so the worker is guaranteed to have fetched it.
	waitForNak(t, msg)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStart_PanicMessageAcked(t *testing.T) {
	payload := TaskPayload{TaskID: "panic-msg", ProjectID: "p1", UserID: "u1", Prompt: "panic"}

	msg := newMockMsg(payload)
	msgCh := make(chan jetstream.Msg, 1)
	msgCh <- msg
	close(msgCh)

	consumer := &mockConsumer{
		fetchFunc: func(batch int, opts ...jetstream.FetchOpt) (jetstream.MessageBatch, error) {
			return &mockMessageBatch{msgs: msgCh}, nil
		},
	}

	w := &TaskWorker{
		consumer: consumer,
		stream:   "test",
		subject:  "test.sub",
		handler: func(ctx context.Context, p TaskPayload) error {
			panic("boom")
		},
		maxRetries: 3,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- w.Start(ctx)
	}()

	// Wait for the background processMessage goroutine to nak the message
	// before cancelling, so the worker is guaranteed to have fetched it.
	waitForNak(t, msg)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewTaskWorker_CustomConfig(t *testing.T) {
	js := &mockJetStream{
		createOrUpdateStreamFunc: func(ctx context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error) {
			return nil, nil
		},
		createOrUpdateConsumerFunc: func(ctx context.Context, stream string, cfg jetstream.ConsumerConfig) (jetstream.Consumer, error) {
			return &mockConsumer{
				fetchFunc: func(batch int, opts ...jetstream.FetchOpt) (jetstream.MessageBatch, error) {
					return &mockMessageBatch{msgs: make(chan jetstream.Msg)}, nil
				},
			}, nil
		},
	}

	natsConn := &NATS{JS: js}
	cfg := WorkerConfig{
		Stream:     "custom",
		Subject:    "custom.task",
		MaxRetries: 7,
		AckWait:    90 * time.Second,
		MaxDeliver: 15,
	}
	handler := func(ctx context.Context, p TaskPayload) error { return nil }

	w, err := NewTaskWorker(natsConn, cfg, handler)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.stream != "custom" {
		t.Errorf("expected stream custom, got %s", w.stream)
	}
	if w.subject != "custom.task" {
		t.Errorf("expected subject custom.task, got %s", w.subject)
	}
	if w.maxRetries != 7 {
		t.Errorf("expected maxRetries 7, got %d", w.maxRetries)
	}
}

func TestPublishTask_FullRoundTrip(t *testing.T) {
	var captured []byte
	js := &mockJetStream{
		publishFunc: func(ctx context.Context, subject string, payload []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
			captured = make([]byte, len(payload))
			copy(captured, payload)
			return &jetstream.PubAck{}, nil
		},
	}

	w := &TaskWorker{
		nats:    &NATS{JS: js},
		stream:  "s",
		subject: "sub",
	}

	p := TaskPayload{TaskID: "rt", ProjectID: "p", UserID: "u", Prompt: "round trip", Priority: 9}
	if err := w.PublishTask(context.Background(), p); err != nil {
		t.Fatal(err)
	}

	var out TaskPayload
	if err := json.Unmarshal(captured, &out); err != nil {
		t.Fatal(err)
	}
	if out.Priority != 9 || out.Prompt != "round trip" {
		t.Errorf("payload mismatch: %+v", out)
	}
}
