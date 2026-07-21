package pagination

import (
	"net/http/httptest"
	"testing"
)

func TestParseRequest(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		expectedLimit  int
		expectedCursor string
	}{
		{
			name:           "default values",
			url:            "/items",
			expectedLimit:  20,
			expectedCursor: "",
		},
		{
			name:           "custom limit and cursor",
			url:            "/items?limit=10&cursor=abc",
			expectedLimit:  10,
			expectedCursor: "abc",
		},
		{
			name:           "limit too high clamped",
			url:            "/items?limit=200",
			expectedLimit:  100,
			expectedCursor: "",
		},
		{
			name:           "negative limit clamped",
			url:            "/items?limit=-5",
			expectedLimit:  20,
			expectedCursor: "",
		},
		{
			name:           "invalid limit uses default",
			url:            "/items?limit=invalid",
			expectedLimit:  20,
			expectedCursor: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.url, nil)
			params := ParseRequest(req)
			if params.Limit != tt.expectedLimit {
				t.Errorf("expected limit %d, got %d", tt.expectedLimit, params.Limit)
			}
			if params.Cursor != tt.expectedCursor {
				t.Errorf("expected cursor %q, got %q", tt.expectedCursor, params.Cursor)
			}
		})
	}
}

func TestCursorRoundtrip(t *testing.T) {
	original := "some-uuid-or-timestamp"
	encoded := EncodeCursor(original)
	decoded, err := DecodeCursor(encoded)
	if err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if decoded != original {
		t.Errorf("expected decoded %q to equal original %q", decoded, original)
	}
}
