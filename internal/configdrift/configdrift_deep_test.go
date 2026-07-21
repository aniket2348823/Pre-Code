package configdrift

import (
	"sync"
	"testing"
)

func TestCompare_Identical(t *testing.T) {
	d := NewDetector()
	d.SetConfig(map[string]string{"a": "1", "b": "2"})
	id := d.Snapshot("s1", 100)
	report, err := d.Compare(id)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Identical {
		t.Error("identical configs should have no drift")
	}
}

func TestCompare_NilSnapshot(t *testing.T) {
	d := NewDetector()
	_, err := d.Compare("nonexistent")
	if err == nil {
		t.Error("nonexistent snapshot should error")
	}
}

func TestDetect_AllFieldsChanged(t *testing.T) {
	d := NewDetector()
	d.SetConfig(map[string]string{"a": "1", "b": "2"})
	id := d.Snapshot("before", 100)
	d.SetConfig(map[string]string{"a": "x", "b": "y"})
	report, _ := d.Compare(id)
	if len(report.Drifts) != 2 {
		t.Errorf("expected 2 drifts, got %d", len(report.Drifts))
	}
}

func TestDetect_NoFieldsChanged(t *testing.T) {
	d := NewDetector()
	d.SetConfig(map[string]string{"a": "1"})
	id := d.Snapshot("before", 100)
	d.SetConfig(map[string]string{"a": "1"})
	report, _ := d.Compare(id)
	if !report.Identical {
		t.Error("should be identical")
	}
}

func TestDetect_AddedField(t *testing.T) {
	d := NewDetector()
	d.SetConfig(map[string]string{"a": "1"})
	id := d.Snapshot("before", 100)
	d.SetConfig(map[string]string{"a": "1", "b": "2"})
	report, _ := d.Compare(id)
	if len(report.Drifts) != 1 || report.Drifts[0].Type != "added" {
		t.Errorf("expected 1 added drift, got %v", report.Drifts)
	}
}

func TestDetect_RemovedField(t *testing.T) {
	d := NewDetector()
	d.SetConfig(map[string]string{"a": "1", "b": "2"})
	id := d.Snapshot("before", 100)
	d.SetConfig(map[string]string{"a": "1"})
	report, _ := d.Compare(id)
	if len(report.Drifts) != 1 || report.Drifts[0].Type != "removed" {
		t.Errorf("expected 1 removed drift, got %v", report.Drifts)
	}
}

func TestDetect_ChangedField(t *testing.T) {
	d := NewDetector()
	d.SetConfig(map[string]string{"host": "localhost"})
	id := d.Snapshot("before", 100)
	d.SetConfig(map[string]string{"host": "production"})
	report, _ := d.Compare(id)
	if report.Identical {
		t.Error("should detect change")
	}
	if report.Drifts[0].Expected != "localhost" || report.Drifts[0].Actual != "production" {
		t.Errorf("wrong drift values: %+v", report.Drifts[0])
	}
}

func TestConcurrentSnapshotCreation(t *testing.T) {
	d := NewDetector()
	d.SetConfig(map[string]string{"a": "1"})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			d.Snapshot("s"+string(rune('0'+n%10)), int64(n))
		}(i)
	}
	wg.Wait()
	if len(d.Snapshots()) == 0 {
		t.Error("expected at least one snapshot")
	}
}

func TestSnapshot_EmptyConfig(t *testing.T) {
	d := NewDetector()
	id := d.Snapshot("empty", 100)
	if id == "" {
		t.Error("empty config should still produce a snapshot")
	}
}

func TestSnapshot_SpecialCharacters(t *testing.T) {
	d := NewDetector()
	d.SetConfig(map[string]string{"key": "value with spaces & symbols <>&\""})
	id := d.Snapshot("special", 100)
	report, err := d.Compare(id)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Identical {
		t.Error("special characters should be handled")
	}
}

func TestSnapshot_DeeplyNested(t *testing.T) {
	d := NewDetector()
	d.SetConfig(map[string]string{"a.b.c.d.e": "deep"})
	id := d.Snapshot("deep", 100)
	report, err := d.Compare(id)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Identical {
		t.Error("deeply nested should work")
	}
}

func TestMultipleDriftTypes_Deep(t *testing.T) {
	d := NewDetector()
	d.SetConfig(map[string]string{"a": "1", "b": "2", "c": "3"})
	id := d.Snapshot("before", 100)
	d.SetConfig(map[string]string{"a": "1", "b": "changed", "d": "4"})
	report, _ := d.Compare(id)
	types := map[string]bool{}
	for _, drift := range report.Drifts {
		types[drift.Type] = true
	}
	if !types["changed"] || !types["removed"] || !types["added"] {
		t.Errorf("expected all 3 drift types, got %v", types)
	}
}

func TestSnapshots_List(t *testing.T) {
	d := NewDetector()
	d.SetConfig(map[string]string{"a": "1"})
	d.Snapshot("s1", 100)
	d.Snapshot("s2", 200)
	ids := d.Snapshots()
	if len(ids) != 2 {
		t.Errorf("expected 2 snapshots, got %d", len(ids))
	}
}

func TestMarshalSnapshot(t *testing.T) {
	d := NewDetector()
	d.SetConfig(map[string]string{"a": "1"})
	id := d.Snapshot("s1", 100)
	data, err := d.MarshalSnapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty data")
	}
}

func TestMarshalSnapshot_Nonexistent(t *testing.T) {
	d := NewDetector()
	_, err := d.MarshalSnapshot("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent snapshot")
	}
}
