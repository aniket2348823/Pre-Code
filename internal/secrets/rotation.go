package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

// SecretMetadata tracks version and rotation info for a secret.
type SecretMetadata struct {
	Name           string    `json:"name"`
	Version        int       `json:"version"`
	LastRotatedAt  time.Time `json:"last_rotated_at"`
	RotationDays   int       `json:"rotation_days"`
	ExpiresAt      time.Time `json:"expires_at,omitempty"`
}

// NeedsRotation returns true if the secret has exceeded its rotation window.
func (m *SecretMetadata) NeedsRotation() bool {
	if m.RotationDays <= 0 {
		return false
	}
	return time.Since(m.LastRotatedAt) >= time.Duration(m.RotationDays)*24*time.Hour
}

// SecretsManager defines the interface for secret storage backends.
type SecretsManager interface {
	Get(name string) (string, error)
	Rotate(name string) (string, error)
	List() ([]SecretMetadata, error)
}

// ---------- Vault implementation ----------

// VaultConfig holds HashiCorp Vault connection settings.
type VaultConfig struct {
	Address    string // e.g. "https://vault.example.com:8200"
	Token      string
	MountPath  string // e.g. "secret"
	HTTPClient *http.Client
}

// VaultSecretsManager stores secrets in HashiCorp Vault via its HTTP API.
type VaultSecretsManager struct {
	addr      string
	token     string
	mount     string
	client    *http.Client
	mu        sync.RWMutex
	versions  map[string]int
	rotations map[string]time.Time
}

// NewVaultSecretsManager creates a Vault-backed secrets manager.
func NewVaultSecretsManager(cfg VaultConfig) *VaultSecretsManager {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	mount := cfg.MountPath
	if mount == "" {
		mount = "secret"
	}
	return &VaultSecretsManager{
		addr:      cfg.Address,
		token:     cfg.Token,
		mount:     mount,
		client:    client,
		versions:  make(map[string]int),
		rotations: make(map[string]time.Time),
	}
}

type vaultSecretData struct {
	Data map[string]string `json:"data"`
}

type vaultReadResponse struct {
	Data vaultSecretData `json:"data"`
}

type vaultWriteRequest struct {
	Data map[string]string `json:"data"`
}

func (v *VaultSecretsManager) url(path string) string {
	return fmt.Sprintf("%s/v1/%s/data/%s", v.addr, v.mount, path)
}

func (v *VaultSecretsManager) doRequest(method, url string, body interface{}) (*http.Response, error) {
	var bodyReader interface{ Read([]byte) (int, error) }
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = &byteReader{data: b}
	}
	req, err := http.NewRequest(method, url, bodyReader.(interface{ Read([]byte) (int, error) }))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-Vault-Token", v.token)
	req.Header.Set("Content-Type", "application/json")
	return v.client.Do(req)
}

// byteReader is a minimal io.Reader for request bodies.
type byteReader struct {
	data []byte
	off  int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, fmt.Errorf("EOF")
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}

// Get retrieves a secret by name from Vault.
func (v *VaultSecretsManager) Get(name string) (string, error) {
	resp, err := v.doRequest("GET", v.url(name), nil)
	if err != nil {
		return "", fmt.Errorf("vault get %q: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("secret %q not found", name)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vault get %q: HTTP %d", name, resp.StatusCode)
	}
	var result vaultReadResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode vault response: %w", err)
	}
	val, ok := result.Data.Data["value"]
	if !ok {
		return "", fmt.Errorf("secret %q has no 'value' field", name)
	}
	return val, nil
}

// Rotate generates a new random 32-byte hex value and stores it in Vault.
func (v *VaultSecretsManager) Rotate(name string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random secret: %w", err)
	}
	newVal := hex.EncodeToString(b)

	payload := vaultWriteRequest{
		Data: map[string]string{"value": newVal},
	}
	resp, err := v.doRequest("POST", v.url(name), payload)
	if err != nil {
		return "", fmt.Errorf("vault rotate %q: %w", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return "", fmt.Errorf("vault rotate %q: HTTP %d", name, resp.StatusCode)
	}

	v.mu.Lock()
	v.versions[name]++
	v.rotations[name] = time.Now()
	v.mu.Unlock()

	slog.Info("secret rotated via vault", "name", name, "version", v.versions[name])
	return newVal, nil
}

// List returns metadata for all tracked secrets.
func (v *VaultSecretsManager) List() ([]SecretMetadata, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	metas := make([]SecretMetadata, 0, len(v.versions))
	for name, ver := range v.versions {
		metas = append(metas, SecretMetadata{
			Name:          name,
			Version:       ver,
			LastRotatedAt: v.rotations[name],
			RotationDays:  90,
		})
	}
	return metas, nil
}

// ---------- Environment variable implementation ----------

// EnvSecretsManager reads secrets from environment variables.
// Secret names are mapped to env var names by uppercasing and replacing "/" with "_".
type EnvSecretsManager struct {
	prefix    string
	mu        sync.RWMutex
	versions  map[string]int
	rotations map[string]time.Time
}

// NewEnvSecretsManager creates an environment variable backed secrets manager.
func NewEnvSecretsManager(prefix string) *EnvSecretsManager {
	return &EnvSecretsManager{
		prefix:    prefix,
		versions:  make(map[string]int),
		rotations: make(map[string]time.Time),
	}
}

func (e *EnvSecretsManager) envName(name string) string {
	if e.prefix != "" {
		return e.prefix + "_" + name
	}
	return name
}

// Get reads a secret from the environment.
func (e *EnvSecretsManager) Get(name string) (string, error) {
	val := os.Getenv(e.envName(name))
	if val == "" {
		return "", fmt.Errorf("env secret %q not found", name)
	}
	return val, nil
}

// Rotate generates a new random value, sets it in the environment, and tracks rotation.
func (e *EnvSecretsManager) Rotate(name string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random secret: %w", err)
	}
	newVal := hex.EncodeToString(b)

	if err := os.Setenv(e.envName(name), newVal); err != nil {
		return "", fmt.Errorf("set env secret %q: %w", name, err)
	}

	e.mu.Lock()
	e.versions[name]++
	e.rotations[name] = time.Now()
	e.mu.Unlock()

	slog.Info("secret rotated via env", "name", name, "version", e.versions[name])
	return newVal, nil
}

// List returns metadata for all tracked secrets.
func (e *EnvSecretsManager) List() ([]SecretMetadata, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	metas := make([]SecretMetadata, 0, len(e.versions))
	for name, ver := range e.versions {
		metas = append(metas, SecretMetadata{
			Name:          name,
			Version:       ver,
			LastRotatedAt: e.rotations[name],
			RotationDays:  90,
		})
	}
	return metas, nil
}

// ---------- Rotation scheduler ----------

// RotationScheduler periodically checks for secrets needing rotation.
type RotationScheduler struct {
	sm          SecretsManager
	interval    time.Duration
	rotationDays int
	stopCh      chan struct{}
	wg          sync.WaitGroup
}

// NewRotationScheduler creates a scheduler that checks rotation needs.
func NewRotationScheduler(sm SecretsManager, checkInterval time.Duration, rotationDays int) *RotationScheduler {
	if rotationDays <= 0 {
		rotationDays = 90
	}
	if checkInterval <= 0 {
		checkInterval = 1 * time.Hour
	}
	return &RotationScheduler{
		sm:           sm,
		interval:     checkInterval,
		rotationDays: rotationDays,
		stopCh:       make(chan struct{}),
	}
}

// Start begins the background rotation check loop.
func (s *RotationScheduler) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.check()
			case <-s.stopCh:
				return
			}
		}
	}()
}

// Stop halts the rotation scheduler.
func (s *RotationScheduler) Stop() {
	close(s.stopCh)
	s.wg.Wait()
}

func (s *RotationScheduler) check() {
	metas, err := s.sm.List()
	if err != nil {
		slog.Error("rotation check: list secrets failed", "error", err)
		return
	}
	for _, m := range metas {
		needDays := m.RotationDays
		if needDays <= 0 {
			needDays = s.rotationDays
		}
		due := time.Duration(needDays) * 24 * time.Hour
		if time.Since(m.LastRotatedAt) >= due {
			slog.Warn("secret rotation overdue",
				"name", m.Name,
				"last_rotated", m.LastRotatedAt,
				"overdue_by", time.Since(m.LastRotatedAt)-due,
			)
		}
	}
}

// ---------- JWT key rotation helper ----------

// JWTKeyRotator handles automatic rotation of JWT signing keys.
type JWTKeyRotator struct {
	sm          SecretsManager
	keyName     string
	rotationDays int
	mu          sync.RWMutex
	currentKey  []byte
}

// NewJWTKeyRotator creates a rotator for JWT signing keys.
func NewJWTKeyRotator(sm SecretsManager, keyName string, rotationDays int) *JWTKeyRotator {
	if rotationDays <= 0 {
		rotationDays = 90
	}
	return &JWTKeyRotator{
		sm:           sm,
		keyName:      keyName,
		rotationDays: rotationDays,
	}
}

// CurrentKey returns the current signing key, rotating if necessary.
func (r *JWTKeyRotator) CurrentKey() ([]byte, error) {
	r.mu.RLock()
	if r.currentKey != nil {
		key := r.currentKey
		r.mu.RUnlock()
		return key, nil
	}
	r.mu.RUnlock()

	return r.rotate()
}

// rotate generates a new key and stores it.
func (r *JWTKeyRotator) rotate() ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock.
	if r.currentKey != nil {
		return r.currentKey, nil
	}

	val, err := r.sm.Rotate(r.keyName)
	if err != nil {
		return nil, fmt.Errorf("jwt key rotation: %w", err)
	}
	r.currentKey = []byte(val)
	return r.currentKey, nil
}

// NeedsRotation checks if the current key has exceeded the rotation window.
func (r *JWTKeyRotator) NeedsRotation() bool {
	metas, err := r.sm.List()
	if err != nil {
		return false
	}
	for _, m := range metas {
		if m.Name == r.keyName {
			return m.NeedsRotation()
		}
	}
	return false
}
