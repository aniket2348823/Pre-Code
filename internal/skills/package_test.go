package skills

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSemver_PrefixedV(t *testing.T) {
	sv, err := ParseSemver("v2.3.4")
	require.NoError(t, err)
	assert.Equal(t, 2, sv.Major)
	assert.Equal(t, 3, sv.Minor)
	assert.Equal(t, 4, sv.Patch)
}

func TestParseSemver_Prerelease(t *testing.T) {
	sv, err := ParseSemver("1.0.0-rc.1")
	require.NoError(t, err)
	assert.Equal(t, 1, sv.Major)
}

func TestParseSemver_BuildMetadata(t *testing.T) {
	sv, err := ParseSemver("1.0.0+build.123")
	require.NoError(t, err)
	assert.Equal(t, 1, sv.Major)
}

func TestSemverCompare_AllZero(t *testing.T) {
	assert.Equal(t, 0, Semver{0, 0, 0}.Compare(Semver{0, 0, 0}))
}

func TestSemverCompare_MajorDiffers(t *testing.T) {
	assert.Equal(t, -1, Semver{1, 0, 0}.Compare(Semver{2, 0, 0}))
	assert.Equal(t, 1, Semver{2, 0, 0}.Compare(Semver{1, 0, 0}))
}

func TestSemverCompare_MinorDiffers(t *testing.T) {
	assert.Equal(t, -1, Semver{1, 1, 0}.Compare(Semver{1, 2, 0}))
	assert.Equal(t, 1, Semver{1, 2, 0}.Compare(Semver{1, 1, 0}))
}

func TestSemverCompare_PatchDiffers(t *testing.T) {
	assert.Equal(t, -1, Semver{1, 2, 3}.Compare(Semver{1, 2, 4}))
	assert.Equal(t, 1, Semver{1, 2, 4}.Compare(Semver{1, 2, 3}))
}

func TestSemverIsCompatible_SameMajor(t *testing.T) {
	assert.True(t, Semver{1, 0, 0}.IsCompatible(Semver{1, 99, 99}))
}

func TestSemverIsCompatible_DifferentMajor(t *testing.T) {
	assert.False(t, Semver{1, 0, 0}.IsCompatible(Semver{2, 0, 0}))
}

func TestSemverNextMajor_FromZero(t *testing.T) {
	got := Semver{0, 0, 0}.NextMajor()
	assert.Equal(t, Semver{1, 0, 0}, got)
}

func TestSemverNextMinor_FromZero(t *testing.T) {
	got := Semver{0, 0, 0}.NextMinor()
	assert.Equal(t, Semver{0, 1, 0}, got)
}

func TestSemverNextPatch_FromZero(t *testing.T) {
	got := Semver{0, 0, 0}.NextPatch()
	assert.Equal(t, Semver{0, 0, 1}, got)
}

func TestValidateVersion_AllInvalid(t *testing.T) {
	for _, v := range []string{"", "1", "1.2", "a.b.c", "1.2.3.4"} {
		assert.Error(t, ValidateVersion(v), "expected error for %q", v)
	}
}

func TestIsNewerVersion_Same(t *testing.T) {
	got, err := IsNewerVersion("1.0.0", "1.0.0")
	require.NoError(t, err)
	assert.False(t, got)
}

func TestGetLatestVersion_SingleValid(t *testing.T) {
	got, err := GetLatestVersion([]string{"1.0.0"})
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", got)
}

func TestGetLatestVersion_AllInvalid(t *testing.T) {
	_, err := GetLatestVersion([]string{"bad1", "bad2"})
	assert.Error(t, err)
}

func TestFilterCompatibleVersions_SameMajor(t *testing.T) {
	versions := []string{"1.0.0", "1.5.0", "2.0.0"}
	got := FilterCompatibleVersions(versions, "1.0.0")
	assert.Len(t, got, 2)
}

func TestFilterCompatibleVersions_InvalidTarget(t *testing.T) {
	got := FilterCompatibleVersions([]string{"1.0.0"}, "bad")
	assert.Nil(t, got)
}

func TestFilterCompatibleVersions_Empty(t *testing.T) {
	got := FilterCompatibleVersions(nil, "1.0.0")
	assert.Empty(t, got)
}

func TestSortVersions_Descending(t *testing.T) {
	got := SortVersions([]string{"1.0.0", "3.0.0", "2.0.0"})
	assert.Equal(t, []string{"3.0.0", "2.0.0", "1.0.0"}, got)
}

func TestSortVersions_InvalidSkipped(t *testing.T) {
	got := SortVersions([]string{"1.0.0", "bad", "2.0.0"})
	assert.Len(t, got, 2)
}

func TestSortVersions_Empty(t *testing.T) {
	assert.Empty(t, SortVersions(nil))
}

func TestComputePackageChecksum_DifferentData(t *testing.T) {
	c1 := ComputePackageChecksum([]byte("abc"))
	c2 := ComputePackageChecksum([]byte("xyz"))
	assert.NotEqual(t, c1, c2)
}

func TestComputePackageChecksum_Length(t *testing.T) {
	c := ComputePackageChecksum([]byte("test"))
	assert.Len(t, c, 64, "SHA-256 hex should be 64 chars")
}

func TestSkillPackage_NilManifest(t *testing.T) {
	pkg := SkillPackage{Checksum: "abc", Size: 100}
	assert.Nil(t, pkg.Manifest)
}

func TestPackageMetadata_AllFields(t *testing.T) {
	meta := PackageMetadata{
		Name:        "my-skill",
		Version:     "2.0.0",
		Description: "desc",
		Author:      "me",
		License:     "MIT",
		Checksum:    "abc123",
		CreatedAt:   time.Now(),
	}
	assert.Equal(t, "my-skill", meta.Name)
	assert.Equal(t, "2.0.0", meta.Version)
}
