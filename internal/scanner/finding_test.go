package scanner

import "testing"

func TestComputeFingerprint(t *testing.T) {
	// Same file/line/snippet (differing only in whitespace) → same fingerprint.
	a := ComputeFingerprint("x.go", 3, "query = a + b")
	b := ComputeFingerprint("x.go", 3, "query =   a  +  b")
	if a != b {
		t.Fatalf("whitespace should not change fingerprint: %s vs %s", a, b)
	}
	if len(a) != 16 {
		t.Fatalf("fingerprint length = %d want 16", len(a))
	}
	// Different line → different fingerprint.
	if ComputeFingerprint("x.go", 4, "query = a + b") == a {
		t.Fatal("different line should change fingerprint")
	}
}

func TestSeverityRank(t *testing.T) {
	if SeverityRank(SeverityCritical) <= SeverityRank(SeverityHigh) {
		t.Fatal("critical must outrank high")
	}
	if SeverityRank(SeverityInfo) <= SeverityRank("") {
		t.Fatal("info must outrank unknown")
	}
}
