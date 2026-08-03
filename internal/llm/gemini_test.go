package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalculateGeminiCost_Gemini25Pro(t *testing.T) {
	cost := calculateGeminiCost("gemini-2.5-pro", 1000, 500)
	assert.Greater(t, cost, 0.0)
	// gemini-2.5-pro: input=0.00125/1K, output=0.01/1K
	expected := 1.0*0.00125 + 0.5*0.01
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateGeminiCost_Gemini20Flash(t *testing.T) {
	cost := calculateGeminiCost("gemini-2.0-flash", 1000, 500)
	assert.Greater(t, cost, 0.0)
	expected := 1.0*0.000075 + 0.5*0.0003
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateGeminiCost_Gemini15Pro(t *testing.T) {
	cost := calculateGeminiCost("gemini-1.5-pro", 1000, 500)
	assert.Greater(t, cost, 0.0)
}

func TestCalculateGeminiCost_Gemini15Flash(t *testing.T) {
	cost := calculateGeminiCost("gemini-1.5-flash", 1000, 500)
	assert.Greater(t, cost, 0.0)
}

func TestCalculateGeminiCost_UnknownModel(t *testing.T) {
	cost := calculateGeminiCost("unknown-model", 1000, 500)
	// Falls back to gemini-2.0-flash pricing
	assert.Greater(t, cost, 0.0)
	expected := 1.0*0.000075 + 0.5*0.0003
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateGeminiCost_ZeroTokens(t *testing.T) {
	cost := calculateGeminiCost("gemini-2.5-pro", 0, 0)
	assert.Equal(t, 0.0, cost)
}

func TestBuildGeminiContents_EmptyMessages(t *testing.T) {
	contents := buildGeminiContents(nil)
	assert.Empty(t, contents)
}

func TestBuildGeminiContents_UserAndAssistant(t *testing.T) {
	contents := buildGeminiContents([]Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	})
	assert.Len(t, contents, 2)
	assert.Equal(t, "user", contents[0].Role)
	assert.Equal(t, "model", contents[1].Role)
}

func TestBuildGeminiContents_NonAssistantMapsToUser(t *testing.T) {
	contents := buildGeminiContents([]Message{
		{Role: "system", Content: "be helpful"},
		{Role: "tool", Content: "result"},
		{Role: "function", Content: "output"},
	})
	assert.Len(t, contents, 3)
	assert.Equal(t, "user", contents[0].Role)
	assert.Equal(t, "user", contents[1].Role)
	assert.Equal(t, "user", contents[2].Role)
}

func TestBuildGeminiContents_PreservesContent(t *testing.T) {
	contents := buildGeminiContents([]Message{
		{Role: "user", Content: "Hello World"},
	})
	require.Len(t, contents, 1)
	assert.Equal(t, "Hello World", contents[0].Parts[0].Text)
}

func TestPtrFloat32Values(t *testing.T) {
	tests := []float32{0, 0.5, 1.0, 3.14, -1.0}
	for _, v := range tests {
		p := ptrFloat32(v)
		assert.Equal(t, v, *p)
	}
}

func TestPtrFloat32_PointerDistinct(t *testing.T) {
	p1 := ptrFloat32(1.0)
	p2 := ptrFloat32(1.0)
	*p1 = 2.0
	assert.Equal(t, float32(2.0), *p1)
	assert.Equal(t, float32(1.0), *p2)
}
