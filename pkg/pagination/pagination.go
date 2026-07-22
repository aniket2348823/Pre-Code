package pagination

import (
	"encoding/base64"
	"net/http"
	"strconv"
)

// DefaultLimit is the default page size.
const DefaultLimit = 20

// MaxLimit is the maximum page size.
const MaxLimit = 100

// Params holds cursor pagination parameters.
type Params struct {
	Limit  int
	Cursor string
}

// ParseRequest extracts pagination parameters from the URL query.
func ParseRequest(r *http.Request) Params {
	limitStr := r.URL.Query().Get("limit")
	limit := DefaultLimit
	if limitStr != "" {
		if val, err := strconv.Atoi(limitStr); err == nil {
			limit = val
		}
	}

	if limit <= 0 {
		limit = DefaultLimit
	} else if limit > MaxLimit {
		limit = MaxLimit
	}

	cursor := r.URL.Query().Get("cursor")

	return Params{
		Limit:  limit,
		Cursor: cursor,
	}
}

// DecodeCursor decodes the base64 cursor string into a plain string (usually UUID or timestamp).
func DecodeCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	bytes, err := base64.StdEncoding.DecodeString(cursor)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

// EncodeCursor encodes a plain string (usually UUID or timestamp) into a base64 cursor.
func EncodeCursor(val string) string {
	if val == "" {
		return ""
	}
	return base64.StdEncoding.EncodeToString([]byte(val))
}
