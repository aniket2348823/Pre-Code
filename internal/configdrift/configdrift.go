// Package configdrift detects configuration drift between environments.
package configdrift

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Snapshot captures the configuration state at a point in time.
type Snapshot struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Config    map[string]string `json:"config"`
	Checksum  string            `json:"checksum"`
	CreatedAt int64             `json:"created_at"`
}

// Drift represents a detected difference between two snapshots.
type Drift struct {
	Key      string `json:"key"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Type     string `json:"type"` // "added", "removed", "changed"
}

// DriftReport is the full comparison report.
type DriftReport struct {
	SnapshotID string `json:"snapshot_id"`
	Drifts     []Drift `json:"drifts"`
	Identical  bool    `json:"identical"`
}

// Detector tracks configuration snapshots and detects drift.
type Detector struct {
	mu        sync.RWMutex
	snapshots map[string]*Snapshot
	current   map[string]string
}

// NewDetector creates a new configuration drift detector.
func NewDetector() *Detector {
	return &Detector{
		snapshots: make(map[string]*Snapshot),
		current:   make(map[string]string),
	}
}

// SetConfig updates the current configuration.
func (d *Detector) SetConfig(config map[string]string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.current = make(map[string]string)
	for k, v := range config {
		d.current[k] = v
	}
}

// Snapshot saves the current configuration as a named snapshot.
func (d *Detector) Snapshot(name string, ts int64) string {
	d.mu.Lock()
	defer d.mu.Unlock()

	snap := &Snapshot{
		Name:      name,
		Config:    make(map[string]string),
		CreatedAt: ts,
	}
	for k, v := range d.current {
		snap.Config[k] = v
	}
	snap.Checksum = computeChecksum(snap.Config)
	snap.ID = computeChecksum(map[string]string{"name": name, "checksum": snap.Checksum})[:8]

	d.snapshots[snap.ID] = snap
	return snap.ID
}

// Compare compares the current config against a saved snapshot.
func (d *Detector) Compare(snapshotID string) (*DriftReport, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	snap, ok := d.snapshots[snapshotID]
	if !ok {
		return nil, fmt.Errorf("snapshot %s not found", snapshotID)
	}

	report := &DriftReport{SnapshotID: snapshotID}

	// Check for removed or changed keys
	for k, expected := range snap.Config {
		actual, exists := d.current[k]
		if !exists {
			report.Drifts = append(report.Drifts, Drift{
				Key: k, Expected: expected, Actual: "", Type: "removed",
			})
		} else if actual != expected {
			report.Drifts = append(report.Drifts, Drift{
				Key: k, Expected: expected, Actual: actual, Type: "changed",
			})
		}
	}

	// Check for added keys
	for k, actual := range d.current {
		if _, exists := snap.Config[k]; !exists {
			report.Drifts = append(report.Drifts, Drift{
				Key: k, Expected: "", Actual: actual, Type: "added",
			})
		}
	}

	report.Identical = len(report.Drifts) == 0
	return report, nil
}

// Snapshots returns all saved snapshot IDs.
func (d *Detector) Snapshots() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	ids := make([]string, 0, len(d.snapshots))
	for id := range d.snapshots {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func computeChecksum(config map[string]string) string {
	keys := make([]string, 0, len(config))
	for k := range config {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(config[k])
		sb.WriteString("\n")
	}

	h := sha256.Sum256([]byte(sb.String()))
	return hex.EncodeToString(h[:])
}

// MarshalSnapshot serializes a snapshot to JSON.
func (d *Detector) MarshalSnapshot(id string) ([]byte, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	snap, ok := d.snapshots[id]
	if !ok {
		return nil, fmt.Errorf("snapshot %s not found", id)
	}
	return json.Marshal(snap)
}
