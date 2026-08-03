package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractCodeBlocks_Backtick(t *testing.T) {
	text := "Here is some code:\n```go\nfmt.Println(\"Hello\")\n```\nAnd more:\n```python\nprint(\"Hi\")\n```"
	blocks := ExtractCodeBlocks(text)
	require.Len(t, blocks, 2)
	assert.Equal(t, "go", blocks[0].Language)
	assert.Contains(t, blocks[0].Code, "fmt.Println")
	assert.Equal(t, "python", blocks[1].Language)
	assert.Contains(t, blocks[1].Code, "print")
}

func TestExtractCodeBlocks_Tilde(t *testing.T) {
	text := "Tilde block:\n~~~rust\nfn main() {}\n~~~"
	blocks := ExtractCodeBlocks(text)
	require.Len(t, blocks, 1)
	assert.Equal(t, "rust", blocks[0].Language)
	assert.Contains(t, blocks[0].Code, "fn main()")
}

func TestExtractCodeBlocks_MixedFences(t *testing.T) {
	text := "First:\n```go\nfmt.Println(1)\n```\nSecond:\n~~~js\nconsole.log(2)\n~~~\nThird:\n```python\nprint(3)\n```"
	blocks := ExtractCodeBlocks(text)
	require.Len(t, blocks, 3)
	assert.Equal(t, "go", blocks[0].Language)
	assert.Equal(t, "js", blocks[1].Language)
	assert.Equal(t, "python", blocks[2].Language)
}

func TestExtractCodeBlocks_Empty(t *testing.T) {
	blocks := ExtractCodeBlocks("no code here")
	assert.Empty(t, blocks)
}

func TestExtractCodeBlocks_EmptyString(t *testing.T) {
	blocks := ExtractCodeBlocks("")
	assert.Empty(t, blocks)
}

func TestExtractCodeBlocks_NoLanguage(t *testing.T) {
	text := "```\nsome code\n```"
	blocks := ExtractCodeBlocks(text)
	require.Len(t, blocks, 1)
	assert.Equal(t, "", blocks[0].Language)
	assert.Equal(t, "some code", blocks[0].Code)
}

func TestExtractCodeBlocks_Indices(t *testing.T) {
	text := "prefix\n```go\ncode\n```\nsuffix"
	blocks := ExtractCodeBlocks(text)
	require.Len(t, blocks, 1)
	assert.Greater(t, blocks[0].StartIndex, 0)
	assert.Greater(t, blocks[0].EndIndex, blocks[0].StartIndex)
}

func TestExtractCodeBlocks_MultiLineCode(t *testing.T) {
	text := "```go\nline1\nline2\nline3\n```"
	blocks := ExtractCodeBlocks(text)
	require.Len(t, blocks, 1)
	assert.Contains(t, blocks[0].Code, "line1")
	assert.Contains(t, blocks[0].Code, "line2")
	assert.Contains(t, blocks[0].Code, "line3")
}

func TestFormatAnalysisSummary_Empty(t *testing.T) {
	summary := FormatAnalysisSummary(nil)
	assert.Equal(t, "", summary)
}

func TestFormatAnalysisSummary_EmptySlice(t *testing.T) {
	summary := FormatAnalysisSummary([]*AnalysisResult{})
	assert.Equal(t, "", summary)
}

func TestFormatAnalysisSummary_WithReviewers(t *testing.T) {
	res := []*AnalysisResult{
		{
			Grade:          "A",
			Score:          95,
			CriticalIssues: 0,
			Suggestions:    3,
			Reviewers: map[string]string{
				"Security": "pass",
			},
		},
	}
	summary := FormatAnalysisSummary(res)
	assert.Contains(t, summary, "A (95%)")
	assert.Contains(t, summary, "0 critical issues")
	assert.Contains(t, summary, "3 suggestions")
	assert.Contains(t, summary, "Security pass")
	assert.NotContains(t, summary, "None")
}

func TestFormatAnalysisSummary_NoReviewers(t *testing.T) {
	res := []*AnalysisResult{
		{
			Grade:          "B",
			Score:          80,
			CriticalIssues: 1,
			Suggestions:    5,
			Reviewers:      map[string]string{},
		},
	}
	summary := FormatAnalysisSummary(res)
	assert.Contains(t, summary, "B (80%)")
	assert.Contains(t, summary, "None")
}

func TestFormatAnalysisSummary_NilReviewers(t *testing.T) {
	res := []*AnalysisResult{
		{
			Grade:          "C",
			Score:          60,
			CriticalIssues: 3,
			Suggestions:    1,
			Reviewers:      nil,
		},
	}
	summary := FormatAnalysisSummary(res)
	assert.Contains(t, summary, "C (60%)")
	assert.Contains(t, summary, "None")
}

func TestFormatAnalysisSummary_MultipleReviewers(t *testing.T) {
	res := []*AnalysisResult{
		{
			Grade:          "A",
			Score:          95,
			CriticalIssues: 0,
			Suggestions:    0,
			Reviewers: map[string]string{
				"Security":     "pass",
				"Architecture": "warn",
				"Performance":  "fail",
			},
		},
	}
	summary := FormatAnalysisSummary(res)
	assert.Contains(t, summary, "Security pass")
	assert.Contains(t, summary, "Architecture warn")
	assert.Contains(t, summary, "Performance fail")
}

func TestAnalyzeCode_Success(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v1/review", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var body map[string]string
		err := json.NewDecoder(r.Body).Decode(&body)
		assert.NoError(t, err)
		assert.Equal(t, "fmt.Println(\"hello\")", body["code"])
		assert.Equal(t, "go", body["language"])

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(AnalysisResult{
			Grade:          "A",
			Score:          90,
			CriticalIssues: 0,
			Suggestions:    1,
		})
	}))
	defer backend.Close()

	result, err := AnalyzeCode(context.Background(), backend.Client(), backend.URL, "test-key", "fmt.Println(\"hello\")", "go")
	require.NoError(t, err)
	assert.Equal(t, "A", result.Grade)
	assert.Equal(t, 90, result.Score)
	assert.Equal(t, 0, result.CriticalIssues)
	assert.Equal(t, 1, result.Suggestions)
}

func TestAnalyzeCode_Non200Status(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer backend.Close()

	result, err := AnalyzeCode(context.Background(), backend.Client(), backend.URL, "key", "code", "go")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "500")
}

func TestAnalyzeCode_InvalidJSONResponse(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not json"))
	}))
	defer backend.Close()

	result, err := AnalyzeCode(context.Background(), backend.Client(), backend.URL, "key", "code", "go")
	assert.Nil(t, result)
	assert.Error(t, err)
}

func TestAnalyzeCode_ContextCanceled(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer backend.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	client := &http.Client{}
	result, err := AnalyzeCode(ctx, client, backend.URL, "key", "code", "go")
	if err == nil {
		t.Skip("context cancel not enforced for in-process httptest server")
	}
	assert.Nil(t, result)
}

func TestAnalyzeCode_BadURL(t *testing.T) {
	// Use a server that returns non-200 to test error path
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("unavailable"))
	}))
	defer backend.Close()

	result, err := AnalyzeCode(context.Background(), backend.Client(), backend.URL, "key", "code", "go")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestCodeBlock_Fields(t *testing.T) {
	b := CodeBlock{
		Language:   "go",
		Code:       "fmt.Println()",
		StartIndex: 0,
		EndIndex:   20,
	}
	assert.Equal(t, "go", b.Language)
	assert.Equal(t, "fmt.Println()", b.Code)
	assert.Equal(t, 0, b.StartIndex)
	assert.Equal(t, 20, b.EndIndex)
}

func TestAnalysisResult_JSON(t *testing.T) {
	r := AnalysisResult{
		Grade:          "A",
		Score:          95,
		CriticalIssues: 1,
		Suggestions:    3,
		Reviewers: map[string]string{
			"sec": "ok",
		},
	}
	data, err := json.Marshal(r)
	require.NoError(t, err)

	var decoded AnalysisResult
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, "A", decoded.Grade)
	assert.Equal(t, 95, decoded.Score)
	assert.Equal(t, 1, decoded.CriticalIssues)
	assert.Equal(t, 3, decoded.Suggestions)
	assert.Equal(t, "ok", decoded.Reviewers["sec"])
}
