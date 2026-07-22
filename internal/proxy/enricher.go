package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

type CodeBlock struct {
	Language   string
	Code       string
	StartIndex int
	EndIndex   int
}

type AnalysisResult struct {
	Grade          string `json:"grade"`
	Score          int    `json:"score"`
	CriticalIssues int    `json:"critical_issues"`
	Suggestions    int    `json:"suggestions"`
	Reviewers      map[string]string `json:"reviewers"`
}

func ExtractCodeBlocks(text string) []CodeBlock {
	var blocks []CodeBlock
	re := regexp.MustCompile("(?s)```([a-zA-Z0-9+-]*)\n(.*?)\n```")
	matches := re.FindAllStringSubmatchIndex(text, -1)

	for _, match := range matches {
		if len(match) >= 6 {
			lang := text[match[2]:match[3]]
			code := text[match[4]:match[5]]
			blocks = append(blocks, CodeBlock{
				Language:   lang,
				Code:       code,
				StartIndex: match[0],
				EndIndex:   match[1],
			})
		}
	}
	return blocks
}

func AnalyzeCode(ctx context.Context, backendURL, apiKey, code, language string) (*AnalysisResult, error) {
	payload := map[string]string{
		"code":     code,
		"language": language,
	}
	bodyBytes, _ := json.Marshal(payload)
	
	req, err := http.NewRequestWithContext(ctx, "POST", backendURL+"/api/v1/review", bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("backend returned status: %d", resp.StatusCode)
	}

	var result AnalysisResult
	respBytes, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func FormatAnalysisSummary(results []*AnalysisResult) string {
	if len(results) == 0 {
		return ""
	}

	res := results[0]
	
	summary := "---\n"
	summary += fmt.Sprintf("🛡️ VigilAgent Analysis: %s (%d%%)\n", res.Grade, res.Score)
	summary += fmt.Sprintf("• %d critical issues\n", res.CriticalIssues)
	summary += fmt.Sprintf("• %d suggestions\n", res.Suggestions)
	
	reviewers := "• Reviewers:"
	for k, v := range res.Reviewers {
		reviewers += fmt.Sprintf(" %s %s |", k, v)
	}
	if len(res.Reviewers) > 0 {
		reviewers = reviewers[:len(reviewers)-2] // remove last " |"
	} else {
		reviewers += " None"
	}
	summary += reviewers + "\n"
	summary += "---"

	return summary
}
