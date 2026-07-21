package query

import (
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/vigilagent/vigilagent/pkg/pagination"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// Filter holds the parsed query filters.
type Filter struct {
	Status         string
	Type           string
	CreatedAfter   *time.Time
	CreatedBefore  *time.Time
	ProjectID      string
	OrganizationID string
	Search         string
}

// Sort holds sorting parameters.
type Sort struct {
	Field string
	Order string
}

// Parse extracts filtering and sorting parameters from the URL query.
func Parse(r *http.Request) (Filter, Sort) {
	q := r.URL.Query()

	var createdAfter *time.Time
	if ca := q.Get("created_after"); ca != "" {
		if t, err := time.Parse(time.RFC3339, ca); err == nil {
			createdAfter = &t
		} else if t, err := time.Parse("2006-01-02", ca); err == nil {
			createdAfter = &t
		}
	}

	var createdBefore *time.Time
	if cb := q.Get("created_before"); cb != "" {
		if t, err := time.Parse(time.RFC3339, cb); err == nil {
			createdBefore = &t
		} else if t, err := time.Parse("2006-01-02", cb); err == nil {
			createdBefore = &t
		}
	}

	filter := Filter{
		Status:         q.Get("status"),
		Type:           q.Get("type"),
		CreatedAfter:   createdAfter,
		CreatedBefore:  createdBefore,
		ProjectID:      q.Get("project_id"),
		OrganizationID: q.Get("organization_id"),
		Search:         q.Get("search"),
	}

	sortField := q.Get("sort")
	if sortField == "" {
		sortField = "created_at"
	}
	sortOrder := strings.ToLower(q.Get("order"))
	if sortOrder != "asc" && sortOrder != "desc" {
		sortOrder = "desc"
	}

	sort := Sort{
		Field: sortField,
		Order: sortOrder,
	}

	return filter, sort
}

// ProcessList filters, sorts, and paginates any slice of structs.
func ProcessList[T any](items []T, filter Filter, sorting Sort, pag pagination.Params) ([]T, *response.Meta) {
	if len(items) == 0 {
		return []T{}, &response.Meta{
			Limit:   pag.Limit,
			HasMore: false,
		}
	}

	// 1. Filter items
	filtered := make([]T, 0)
	for _, item := range items {
		val := reflect.ValueOf(item)
		if val.Kind() == reflect.Ptr {
			val = val.Elem()
		}

		if val.Kind() != reflect.Struct {
			filtered = append(filtered, item)
			continue
		}

		match := true

		// Filter by Status
		if filter.Status != "" {
			statusField := getFieldValue(val, "Status", "status")
			if statusField != "" && !strings.EqualFold(statusField, filter.Status) {
				match = false
			}
		}

		// Filter by Type
		if filter.Type != "" {
			typeField := getFieldValue(val, "Type", "type", "EventType", "event_type")
			if typeField != "" && !strings.EqualFold(typeField, filter.Type) {
				match = false
			}
		}

		// Filter by ProjectID
		if filter.ProjectID != "" {
			projectField := getFieldValue(val, "ProjectID", "project_id")
			if projectField != "" && projectField != filter.ProjectID {
				match = false
			}
		}

		// Filter by OrganizationID
		if filter.OrganizationID != "" {
			orgField := getFieldValue(val, "OrgID", "org_id", "OrganizationID", "organization_id")
			if orgField != "" && orgField != filter.OrganizationID {
				match = false
			}
		}

		// Filter by Search (checks Name, Description, Prompt, etc.)
		if filter.Search != "" {
			nameField := getFieldValue(val, "Name", "name")
			descField := getFieldValue(val, "Description", "description")
			promptField := getFieldValue(val, "Prompt", "prompt")
			searchLower := strings.ToLower(filter.Search)

			found := strings.Contains(strings.ToLower(nameField), searchLower) ||
				strings.Contains(strings.ToLower(descField), searchLower) ||
				strings.Contains(strings.ToLower(promptField), searchLower)

			if !found {
				match = false
			}
		}

		// Filter by CreatedAfter / CreatedBefore
		createdAtField := val.FieldByName("CreatedAt")
		if createdAtField.IsValid() && !createdAtField.IsZero() {
			if t, ok := createdAtField.Interface().(time.Time); ok {
				if filter.CreatedAfter != nil && t.Before(*filter.CreatedAfter) {
					match = false
				}
				if filter.CreatedBefore != nil && t.After(*filter.CreatedBefore) {
					match = false
				}
			}
		}

		if match {
			filtered = append(filtered, item)
		}
	}

	// 2. Sort items
	sort.SliceStable(filtered, func(i, j int) bool {
		valI := reflect.ValueOf(filtered[i])
		valJ := reflect.ValueOf(filtered[j])
		if valI.Kind() == reflect.Ptr {
			valI = valI.Elem()
			valJ = valJ.Elem()
		}

		fieldI := getFieldAny(valI, sorting.Field)
		fieldJ := getFieldAny(valJ, sorting.Field)

		less := compareValues(fieldI, fieldJ)
		if sorting.Order == "desc" {
			return !less && fieldI != fieldJ
		}
		return less
	})

	// 3. Paginate (cursor-based)
	total := len(filtered)
	startIndex := 0

	// If cursor is provided, decode and find start element
	if pag.Cursor != "" {
		decodedCursor, err := pagination.DecodeCursor(pag.Cursor)
		if err == nil {
			for idx, item := range filtered {
				val := reflect.ValueOf(item)
				if val.Kind() == reflect.Ptr {
					val = val.Elem()
				}
				id := getFieldValue(val, "ID", "id")
				if id == decodedCursor {
					// Cursor points to the last element of previous page. Start from next element.
					startIndex = idx + 1
					break
				}
			}
		}
	}

	endIndex := startIndex + pag.Limit
	hasMore := false
	if endIndex < len(filtered) {
		hasMore = true
	} else {
		endIndex = len(filtered)
	}

	paginated := make([]T, 0)
	if startIndex < len(filtered) {
		paginated = filtered[startIndex:endIndex]
	}

	nextCursor := ""
	if hasMore && len(paginated) > 0 {
		lastItem := paginated[len(paginated)-1]
		val := reflect.ValueOf(lastItem)
		if val.Kind() == reflect.Ptr {
			val = val.Elem()
		}
		lastID := getFieldValue(val, "ID", "id")
		nextCursor = pagination.EncodeCursor(lastID)
	}

	return paginated, &response.Meta{
		Total:      total,
		Limit:      pag.Limit,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}
}

// Helpers
func getFieldValue(val reflect.Value, names ...string) string {
	for _, name := range names {
		f := val.FieldByName(name)
		if f.IsValid() && f.Kind() == reflect.String {
			return f.String()
		}
	}
	return ""
}

func getFieldAny(val reflect.Value, name string) interface{} {
	// Try PascalCase
	pascal := strings.ToUpper(name[:1]) + name[1:]
	if strings.Contains(pascal, "_") {
		parts := strings.Split(pascal, "_")
		for i, p := range parts {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
		pascal = strings.Join(parts, "")
	}

	f := val.FieldByName(pascal)
	if f.IsValid() {
		return f.Interface()
	}

	// Try exactly as passed
	f = val.FieldByName(name)
	if f.IsValid() {
		return f.Interface()
	}

	return nil
}

func compareValues(i, j interface{}) bool {
	if i == nil || j == nil {
		return i == nil && j != nil
	}

	switch valI := i.(type) {
	case string:
		return valI < j.(string)
	case int:
		return valI < j.(int)
	case int64:
		return valI < j.(int64)
	case float64:
		return valI < j.(float64)
	case time.Time:
		return valI.Before(j.(time.Time))
	default:
		return fmt.Sprintf("%v", i) < fmt.Sprintf("%v", j)
	}
}
