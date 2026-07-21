package skills

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SkillPackage represents a distributable skill package.
type SkillPackage struct {
	Manifest  *Manifest `json:"manifest"`
	Checksum  string    `json:"checksum"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

// PackageMetadata is the info.json inside a skill package.
type PackageMetadata struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description"`
	Author      string    `json:"author"`
	License     string    `json:"license"`
	Checksum    string    `json:"checksum"`
	CreatedAt   time.Time `json:"created_at"`
}

// --- Package Operations ---

// CreatePackage creates a tar.gz skill package from a directory.
func CreatePackage(skillDir string, manifest *Manifest) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Add manifest first
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal manifest: %w", err)
	}

	// Write manifest
	header := &tar.Header{
		Name:    "manifest.json",
		Mode:    0644,
		Size:    int64(len(manifestData)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return nil, err
	}
	if _, err := tw.Write(manifestData); err != nil {
		return nil, err
	}

	// Walk skill directory and add all files
	err = filepath.Walk(skillDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// Skip hidden files and test files
		name := info.Name()
		if strings.HasPrefix(name, ".") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		relPath, err := filepath.Rel(skillDir, path)
		if err != nil {
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		header := &tar.Header{
			Name:    filepath.ToSlash(relPath),
			Mode:    0644,
			Size:    int64(len(data)),
			ModTime: info.ModTime(),
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
	if err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close tar writer: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("failed to close gzip writer: %w", err)
	}

	return buf.Bytes(), nil
}

// ExtractPackage extracts a tar.gz skill package to a directory.
func ExtractPackage(data []byte, destDir string) (*Manifest, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var manifest *Manifest

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		// Sanitize path to prevent directory traversal
		name := filepath.Clean(header.Name)
		if strings.Contains(name, "..") {
			continue
		}

		target := filepath.Join(destDir, name)

		switch header.Typeflag {
		case tar.TypeReg:
			// Create directory if needed
			dir := filepath.Dir(target)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, err
			}

			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return nil, err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return nil, err
			}
			f.Close()

			// Parse manifest
			if name == "manifest.json" {
				manifestData, err := os.ReadFile(target)
				if err == nil {
					manifest = &Manifest{}
					json.Unmarshal(manifestData, manifest)
				}
			}
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return nil, err
			}
		}
	}

	return manifest, nil
}

// ComputePackageChecksum calculates SHA-256 of package data.
func ComputePackageChecksum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// --- Versioning ---

// Semver represents a semantic version (major.minor.patch).
type Semver struct {
	Major int
	Minor int
	Patch int
}

var semverRegex = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-([a-zA-Z0-9.]+))?(?:\+([a-zA-Z0-9.]+))?$`)

// ParseSemver parses a version string into Semver.
func ParseSemver(version string) (Semver, error) {
	matches := semverRegex.FindStringSubmatch(version)
	if matches == nil {
		return Semver{}, fmt.Errorf("invalid semver: %s", version)
	}

	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])

	return Semver{Major: major, Minor: minor, Patch: patch}, nil
}

// String returns the version as a string.
func (s Semver) String() string {
	return fmt.Sprintf("%d.%d.%d", s.Major, s.Minor, s.Patch)
}

// Compare returns -1, 0, or 1 if s < other, s == other, or s > other.
func (s Semver) Compare(other Semver) int {
	if s.Major != other.Major {
		if s.Major < other.Major {
			return -1
		}
		return 1
	}
	if s.Minor != other.Minor {
		if s.Minor < other.Minor {
			return -1
		}
		return 1
	}
	if s.Patch != other.Patch {
		if s.Patch < other.Patch {
			return -1
		}
		return 1
	}
	return 0
}

// IsCompatible checks if other is compatible (same major version).
func (s Semver) IsCompatible(other Semver) bool {
	return s.Major == other.Major
}

// NextMajor bumps to next major version.
func (s Semver) NextMajor() Semver {
	return Semver{Major: s.Major + 1, Minor: 0, Patch: 0}
}

// NextMinor bumps to next minor version.
func (s Semver) NextMinor() Semver {
	return Semver{Major: s.Major, Minor: s.Minor + 1, Patch: 0}
}

// NextPatch bumps to next patch version.
func (s Semver) NextPatch() Semver {
	return Semver{Major: s.Major, Minor: s.Minor, Patch: s.Patch + 1}
}

// ValidateVersion checks if a version string is valid semver.
func ValidateVersion(version string) error {
	_, err := ParseSemver(version)
	return err
}

// IsNewerVersion returns true if newVersion is newer than currentVersion.
func IsNewerVersion(current, newVersion string) (bool, error) {
	currentSem, err := ParseSemver(current)
	if err != nil {
		return false, err
	}
	newSem, err := ParseSemver(newVersion)
	if err != nil {
		return false, err
	}
	return newSem.Compare(currentSem) > 0, nil
}

// GetLatestVersion finds the latest version from a list.
func GetLatestVersion(versions []string) (string, error) {
	if len(versions) == 0 {
		return "", fmt.Errorf("no versions provided")
	}

	latest, err := ParseSemver(versions[0])
	if err != nil {
		return "", err
	}

	for _, v := range versions[1:] {
		sem, err := ParseSemver(v)
		if err != nil {
			continue
		}
		if sem.Compare(latest) > 0 {
			latest = sem
		}
	}

	return latest.String(), nil
}

// FilterCompatibleVersions returns only versions compatible with the given version.
func FilterCompatibleVersions(versions []string, target string) []string {
	targetSem, err := ParseSemver(target)
	if err != nil {
		return nil
	}

	var compatible []string
	for _, v := range versions {
		sem, err := ParseSemver(v)
		if err != nil {
			continue
		}
		if targetSem.IsCompatible(sem) {
			compatible = append(compatible, v)
		}
	}
	return compatible
}

// SortVersions sorts version strings in descending order.
func SortVersions(versions []string) []string {
	parsed := make([]struct {
		version string
		sem     Semver
	}, 0, len(versions))

	for _, v := range versions {
		sem, err := ParseSemver(v)
		if err != nil {
			continue
		}
		parsed = append(parsed, struct {
			version string
			sem     Semver
		}{version: v, sem: sem})
	}

	// Simple bubble sort for small lists
	for i := 0; i < len(parsed); i++ {
		for j := i + 1; j < len(parsed); j++ {
			if parsed[i].sem.Compare(parsed[j].sem) < 0 {
				parsed[i], parsed[j] = parsed[j], parsed[i]
			}
		}
	}

	result := make([]string, len(parsed))
	for i, p := range parsed {
		result[i] = p.version
	}
	return result
}
