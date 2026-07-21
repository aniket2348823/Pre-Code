package util

import "testing"

func TestItoa(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{100, "100"},
		{999, "999"},
		{12345, "12345"},
	}
	for _, tt := range tests {
		if got := Itoa(tt.input); got != tt.want {
			t.Errorf("Itoa(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestJoin(t *testing.T) {
	tests := []struct {
		parts []string
		sep   string
		want  string
	}{
		{nil, ",", ""},
		{[]string{}, ",", ""},
		{[]string{"a"}, ",", "a"},
		{[]string{"a", "b"}, ",", "a,b"},
		{[]string{"a", "b", "c"}, " | ", "a | b | c"},
	}
	for _, tt := range tests {
		if got := Join(tt.parts, tt.sep); got != tt.want {
			t.Errorf("Join(%v, %q) = %q, want %q", tt.parts, tt.sep, got, tt.want)
		}
	}
}

// === ContainsWord tests ===

func TestContainsWord_LongKeyword(t *testing.T) {
	// Long keywords use simple substring match
	if !ContainsWord("build a payment system", "payment") {
		t.Error("should find 'payment' in text")
	}
	if ContainsWord("build a payment system", "invoice") {
		t.Error("should not find 'invoice' in text")
	}
}

func TestContainsWord_ShortKeyword(t *testing.T) {
	// Short keywords require word boundaries
	if !ContainsWord("build a REST api", "api") {
		t.Error("should find 'api' at word boundary")
	}
	if ContainsWord("solve the captcha puzzle", "api") {
		t.Error("'api' should NOT match inside 'captcha'")
	}
}

func TestContainsWord_ShortKeywordAtStart(t *testing.T) {
	if !ContainsWord("api server for users", "api") {
		t.Error("should find 'api' at start of text")
	}
}

func TestContainsWord_ShortKeywordAtEnd(t *testing.T) {
	if !ContainsWord("build a rest api", "api") {
		t.Error("should find 'api' at end of text")
	}
}

func TestContainsWord_NotFound(t *testing.T) {
	if ContainsWord("hello world", "xyz") {
		t.Error("should not find 'xyz'")
	}
}

// === HasWordBoundary tests ===

func TestHasWordBoundary_FindsValidMatch(t *testing.T) {
	// "auth" in "authentication system" — should match
	if !HasWordBoundary("authentication system", "auth") {
		t.Error("should find 'auth' as prefix of 'authentication'")
	}
}

func TestHasWordBoundary_RejectsSubstring(t *testing.T) {
	// "api" inside "captcha" — should NOT match
	if HasWordBoundary("solve the captcha puzzle", "api") {
		t.Error("'api' should NOT match inside 'captcha'")
	}
}

func TestHasWordBoundary_MultipleOccurrences(t *testing.T) {
	// First occurrence is bad (inside "captcha"), second is good (standalone)
	if !HasWordBoundary("solve captcha, then call api endpoint", "api") {
		t.Error("should find 'api' at word boundary even when first occurrence is invalid")
	}
}

func TestHasWordBoundary_ExactMatch(t *testing.T) {
	if !HasWordBoundary("api", "api") {
		t.Error("exact match should work")
	}
}

func TestHasWordBoundary_UnderscoreBoundary(t *testing.T) {
	// underscore is a word character
	if HasWordBoundary("my_api_server", "api") {
		t.Error("'api' inside underscores should NOT match")
	}
}

// === IsWordChar tests ===

func TestIsWordChar(t *testing.T) {
	tests := []struct {
		c    byte
		want bool
	}{
		{'a', true},
		{'Z', true},
		{'0', true},
		{'_', true},
		{' ', false},
		{'-', false},
		{'.', false},
	}
	for _, tt := range tests {
		if got := IsWordChar(tt.c); got != tt.want {
			t.Errorf("IsWordChar(%q) = %v, want %v", tt.c, got, tt.want)
		}
	}
}

// === ComputeMatchScore tests ===

func TestComputeMatchScore_AllMatch(t *testing.T) {
	score := ComputeMatchScore("payment processing with credit card checkout", []string{"payment", "credit card", "checkout"}, nil)
	if score < 0.9 {
		t.Errorf("expected high score for all-match, got %f", score)
	}
}

func TestComputeMatchScore_NoMatch(t *testing.T) {
	score := ComputeMatchScore("static homepage about cats", []string{"payment", "billing", "checkout"}, nil)
	if score != 0.0 {
		t.Errorf("expected 0 for no match, got %f", score)
	}
}

func TestComputeMatchScore_ExclusionKills(t *testing.T) {
	score := ComputeMatchScore("unpaid invoice tracking", []string{"payment", "invoice"}, []string{"unpaid"})
	if score != 0.0 {
		t.Errorf("exclusion 'unpaid' should zero out score, got %f", score)
	}
}

func TestComputeMatchScore_NoKeywords(t *testing.T) {
	score := ComputeMatchScore("anything", nil, nil)
	if score != 0.0 {
		t.Errorf("empty keywords should return 0, got %f", score)
	}
}

func TestComputeMatchScore_ShortKeywordWordBoundary(t *testing.T) {
	// "api" in "captcha" should NOT match
	score := ComputeMatchScore("solve the captcha puzzle", []string{"api"}, nil)
	if score != 0.0 {
		t.Errorf("'api' in 'captcha' should score 0, got %f", score)
	}
}
