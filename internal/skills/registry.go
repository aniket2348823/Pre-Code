package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Manifest represents a skill's YAML manifest.
type Manifest struct {
	Name         string                 `json:"name"`
	DisplayName  string                 `json:"display_name"`
	Version      string                 `json:"version"`
	Description  string                 `json:"description"`
	Author       AuthorInfo             `json:"author"`
	License      string                 `json:"license"`
	Category     string                 `json:"category"`
	Tags         []string               `json:"tags"`
	Requires     Requirements           `json:"requires"`
	ConfigSchema map[string]interface{} `json:"config_schema,omitempty"`
	EntryPoint   string                 `json:"entry_point"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// AuthorInfo contains author details.
type AuthorInfo struct {
	Name  string `json:"name"`
	URL   string `json:"url,omitempty"`
	Email string `json:"email,omitempty"`
}

// Requirements specifies skill requirements.
type Requirements struct {
	PlatformVersion string   `json:"platform_version,omitempty"`
	Permissions     []string `json:"permissions,omitempty"`
	Tools           []string `json:"tools,omitempty"`
	Runtime         string   `json:"runtime,omitempty"`
}

// Skill represents a registered skill.
type Skill struct {
	ID           string    `json:"id"`
	AuthorID     string    `json:"author_id"`
	Manifest     Manifest  `json:"manifest"`
	PackageURL   string    `json:"package_url"`
	PackageSize  int       `json:"package_size"`
	Checksum     string    `json:"checksum"`
	IsVerified   bool      `json:"is_verified"`
	IsFeatured   bool      `json:"is_featured"`
	IsPublished  bool      `json:"is_published"`
	InstallCount int       `json:"install_count"`
	RatingAvg    float64   `json:"rating_avg"`
	RatingCount  int       `json:"rating_count"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	PublishedAt  *time.Time `json:"published_at,omitempty"`
}

// UserSkill represents an installed skill for a user.
type UserSkill struct {
	ID          string                 `json:"id"`
	UserID      string                 `json:"user_id"`
	SkillID     string                 `json:"skill_id"`
	Version     string                 `json:"version"`
	Config      map[string]interface{} `json:"config,omitempty"`
	Enabled     bool                   `json:"enabled"`
	UsageCount  int                    `json:"usage_count"`
	InstalledAt time.Time              `json:"installed_at"`
}

// Registry manages skills in the marketplace.
type Registry struct {
	skills map[string]*Skill
}

// NewRegistry creates a new skill registry.
func NewRegistry() *Registry {
	return &Registry{
		skills: make(map[string]*Skill),
	}
}

// Register adds a skill to the registry.
func (r *Registry) Register(skill *Skill) {
	r.skills[skill.ID] = skill
}

// Get retrieves a skill by ID.
func (r *Registry) Get(id string) (*Skill, bool) {
	skill, ok := r.skills[id]
	return skill, ok
}

// List returns all published skills sorted by install count descending.
func (r *Registry) List(category string, limit int) []*Skill {
	var result []*Skill
	for _, skill := range r.skills {
		if !skill.IsPublished {
			continue
		}
		if category != "" && skill.Manifest.Category != category {
			continue
		}
		result = append(result, skill)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].InstallCount > result[j].InstallCount
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result
}

// Search finds skills by text query.
func (r *Registry) Search(query string, limit int) []*Skill {
	var result []*Skill
	queryLower := strings.ToLower(query)
	for _, skill := range r.skills {
		if len(result) >= limit {
			break
		}
		if !skill.IsPublished {
			continue
		}
		if strings.Contains(strings.ToLower(skill.Manifest.Name), queryLower) ||
			strings.Contains(strings.ToLower(skill.Manifest.DisplayName), queryLower) ||
			strings.Contains(strings.ToLower(skill.Manifest.Description), queryLower) {
			result = append(result, skill)
		}
	}
	return result
}

// LoadManifest loads a skill manifest from a file.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	return &manifest, nil
}

// ValidateManifest checks if a manifest is valid.
func ValidateManifest(m *Manifest) error {
	if m.Name == "" {
		return fmt.Errorf("skill name is required")
	}
	if m.Version == "" {
		return fmt.Errorf("skill version is required")
	}
	if m.Description == "" {
		return fmt.Errorf("skill description is required")
	}
	if m.Category == "" {
		return fmt.Errorf("skill category is required")
	}
	return nil
}

// GetSkillDir returns the skill directory for a given skill name.
func GetSkillDir(skillsDir, skillName string) string {
	return filepath.Join(skillsDir, skillName)
}
