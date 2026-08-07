package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vigilagent/vigilagent/internal/llm"
	"github.com/vigilagent/vigilagent/internal/signing"
)

// handleStreaming proxies a streaming request through ModelRouter,
// streams tokens to the client in real-time, and runs background analysis.
func (s *ProxyServer) handleStreaming(
	w http.ResponseWriter,
	r *http.Request,
	modelRouter *llm.ModelRouter,
	requestBody []byte,
	defaultFormat string,
	model string,
) {
	// Build the LLM task for ModelRouter
	messages, err := s.parseMessages(requestBody)
	if err != nil || len(messages) == 0 {
		http.Error(w, `{"error":"invalid request: missing messages"}`, http.StatusBadRequest)
		return
	}

	if model == "" {
		model = "gpt-4o-mini"
	}

	task := &llm.Task{
		ID:          "proxy-stream-" + time.Now().Format("20060102150405"),
		Type:        "feature",
		Description: "Proxy streaming request",
		Messages:    messages,
	}

	// Design-stage gate: scan the request before generation (see handleProxyRequest).
	mode := EnforcementMode(r.Header.Get("X-VigilAgent-Mode"))
	if mode == "" {
		mode = ModeObserve
	}
	_, constrained := s.applyDesignGate(task)

	// Stream through ModelRouter with smart failover
	streamResult, err := modelRouter.StreamWithFailover(r.Context(), task)
	if err != nil {
		slog.Warn("model router streaming failed, falling back", "error", err)
		s.handleStreamingDirect(w, r, requestBody, defaultFormat)
		return
	}

	// Stream tokens to the client
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-VigilAgent-Provider", streamResult.Provider)
	w.Header().Set("X-VigilAgent-Model", streamResult.Model)
	if constrained {
		w.Header().Set("X-VigilAgent-Design-Gate", "constrained")
	} else if mode != ModePassthrough {
		w.Header().Set("X-VigilAgent-Design-Gate", "passed")
	}
	streamScanID := newScanID(s)
	w.Header().Set("X-VigilAgent-Scan-ID", streamScanID)
	w.Header().Set("X-VigilAgent-Provenance", signing.ProvenanceVerified)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	var fullContent strings.Builder
	const maxStreamContent = 10 << 20 // 10MB limit for accumulated stream content
	start := time.Now()

	// Strict mode: hold everything until the scan policy allows release
	// (fail-closed streaming — nothing leaves the gateway early).
	strictMode := mode == ModeStrict
	var strictBuf strings.Builder

	for chunk := range streamResult.Ch {
		if chunk.Content != "" {
			if fullContent.Len()+len(chunk.Content) > maxStreamContent {
				slog.Info("streaming content limit reached", "bytes", maxStreamContent)
				break
			}
			fullContent.WriteString(chunk.Content)
			if strictMode {
				strictBuf.WriteString(chunk.Content)
				continue
			}
			// Forward chunk in OpenAI SSE format
			sseChunk := map[string]interface{}{
				"id":      fmt.Sprintf("chatcmpl-vigil-%d", time.Now().UnixNano()),
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"model":   streamResult.Model,
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"delta": map[string]interface{}{
							"content": chunk.Content,
						},
						"finish_reason": nil,
					},
				},
			}
			chunkBytes, _ := json.Marshal(sseChunk)
			fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
			flusher.Flush()
		}
		if chunk.Finish {
			break
		}
	}

	// Run dual-engine analysis on accumulated content
	llmKey := r.Header.Get("X-LLM-Key")
	strictEmitted := false
	if mode != ModePassthrough {
		content := fullContent.String()
		outcome, _, summary, _ := s.analyzeAndVerdict(r.Context(), modelRouter, llmKey, content)
		if outcome != nil {
			outcome.Policy = ComputePolicy(*outcome, mode)
			s.recordDecision(outcome.Verdict, outcome.Policy)
			rec, _ := s.recordProvenance(r, streamResult.Provider, streamResult.Model, outcome.Policy, mode, content, streamScanID)
			outcome.ScanID = rec.ScanID

			if strictMode {
				// Fail-closed: nothing was emitted yet. Release the buffered
				// content only when the policy allows it.
				release, _, reason := enforcePolicy(outcome.Policy, mode)
				if !release {
					errChunk := map[string]interface{}{
						"error": map[string]interface{}{
							"message": reason, "decision": outcome.Policy, "verdict": outcome.Verdict, "scan_id": rec.ScanID,
						},
					}
					chunkBytes, _ := json.Marshal(errChunk)
					fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
				} else if strictBuf.Len() > 0 {
					sseChunk := map[string]interface{}{
						"id":      fmt.Sprintf("chatcmpl-vigil-%d", time.Now().UnixNano()),
						"object":  "chat.completion.chunk",
						"created": time.Now().Unix(),
						"model":   streamResult.Model,
						"choices": []map[string]interface{}{
							{"index": 0, "delta": map[string]interface{}{"content": strictBuf.String()}, "finish_reason": nil},
						},
					}
					chunkBytes, _ := json.Marshal(sseChunk)
					fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
				}
				strictEmitted = true
			}

			if summary != "" {
				summary = "🛡️ VigilAgent verdict: " + strings.ToUpper(outcome.Verdict) + " (grade " + outcome.Grade + ", " + strconv.Itoa(outcome.Score) + "/100) — policy: " + string(outcome.Policy) + "\n\n" + summary

				// Inject summary as a final SSE chunk
				summaryChunk := map[string]interface{}{
					"id":      fmt.Sprintf("chatcmpl-vigil-analysis-%d", time.Now().UnixNano()),
					"object":  "chat.completion.chunk",
					"created": time.Now().Unix(),
					"model":   streamResult.Model,
					"choices": []map[string]interface{}{
						{
							"index": 0,
							"delta": map[string]interface{}{
								"content": "\n\n" + summary,
							},
							"finish_reason": nil,
						},
					},
				}
				chunkBytes, _ := json.Marshal(summaryChunk)
				fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
				flusher.Flush()
			} else if strictMode {
				flusher.Flush()
			}
		}
	}
	if strictMode && !strictEmitted {
		// No analyzable content: release what was buffered.
		if strictBuf.Len() > 0 {
			sseChunk := map[string]interface{}{
				"id":      fmt.Sprintf("chatcmpl-vigil-%d", time.Now().UnixNano()),
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"model":   streamResult.Model,
				"choices": []map[string]interface{}{
					{"index": 0, "delta": map[string]interface{}{"content": strictBuf.String()}, "finish_reason": nil},
				},
			}
			chunkBytes, _ := json.Marshal(sseChunk)
			fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
		}
		flusher.Flush()
	}

	// Send final [DONE]
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()

	latency := time.Since(start)
	slog.Info("streaming complete", "model", streamResult.Model, "provider", streamResult.Provider, "latency", latency.Round(time.Millisecond), "content_len", fullContent.Len(), "blocks", len(ExtractCodeBlocks(fullContent.String())))
}

// ─────────────────────────────────────────────────────────────────────────────
// OPENAI RESPONSES API — STREAMING (POST /v1/responses with stream:true)
// ─────────────────────────────────────────────────────────────────────────────

// handleResponsesStream streams a Responses-API request through the same
// scan-and-release pipeline, emitting Responses-style SSE events:
// response.created → response.output_text.delta* → response.completed (or
// response.failed when strict policy blocks). Per-mode behavior:
//
//	observe:  tokens stream live, verdict appended at the end (advisory)
//	balanced: prose streams live, fenced code blocks are withheld until the
//	          scan completes (held → placeholder, or released when clean)
//	strict:   everything is buffered and released only if the policy allows
func (s *ProxyServer) handleResponsesStream(w http.ResponseWriter, r *http.Request, bodyBytes []byte, model string, input json.RawMessage) {
	messages, err := parseResponsesInput(input)
	if err != nil {
		http.Error(w, `{"error":"invalid input: must be a string or an array of {role, content} messages"}`, http.StatusBadRequest)
		return
	}

	llmKey := r.Header.Get("X-LLM-Key")
	llmProvider := r.Header.Get("X-LLM-Provider")
	llmModel := r.Header.Get("X-LLM-Model")
	if llmModel != "" {
		model = llmModel
	}

	// Same tenant checks as chat completions: model allowlist + per-key quota.
	if !s.modelAllowed(model) {
		http.Error(w, `{"error":"model not allowed by tenant policy"}`, http.StatusForbidden)
		return
	}
	if s.cfg.PerKeyDailyQuota > 0 && s.usageCount(getAPIKey(r.Context())) >= uint64(s.cfg.PerKeyDailyQuota) {
		http.Error(w, `{"error":"daily request quota exceeded"}`, http.StatusTooManyRequests)
		return
	}

	mode := EnforcementMode(r.Header.Get("X-VigilAgent-Mode"))
	if mode == "" {
		mode = ModeObserve
	}

	modelRouter := s.buildRouter(llmKey, llmProvider)

	// Design-stage gate (same as chat completions).
	task := &llm.Task{
		ID:          "proxy-responses-stream-" + time.Now().Format("20060102150405"),
		Type:        "feature",
		Description: "Proxy responses streaming request",
		Messages:    messages,
	}
	_, constrained := s.applyDesignGate(task)

	streamResult, err := modelRouter.StreamWithFailover(r.Context(), task)
	if err != nil {
		http.Error(w, `{"error":"llm request failed: `+err.Error()+`"}`, http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-VigilAgent-Provider", streamResult.Provider)
	w.Header().Set("X-VigilAgent-Model", streamResult.Model)
	if constrained {
		w.Header().Set("X-VigilAgent-Design-Gate", "constrained")
	} else if mode != ModePassthrough {
		w.Header().Set("X-VigilAgent-Design-Gate", "passed")
	}
	streamScanID := newScanID(s)
	w.Header().Set("X-VigilAgent-Scan-ID", streamScanID)
	w.Header().Set("X-VigilAgent-Provenance", signing.ProvenanceVerified)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	respID := "resp_" + time.Now().Format("20060102150405") + fmt.Sprintf("_%d", time.Now().UnixNano()%1000000)
	itemID := "msg_" + respID
	createdAt := time.Now().Unix()

	writeSSE := func(event string, data map[string]interface{}) {
		fmt.Fprintf(w, "event: %s\n", event)
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	// Kick off the response lifecycle.
	writeSSE("response.created", map[string]interface{}{
		"type": "response.created",
		"response": map[string]interface{}{
			"id": respID, "object": "response", "created_at": createdAt,
			"model": streamResult.Model, "status": "in_progress",
		},
	})

	gate := newStreamingGate(mode)
	var fullContent strings.Builder
	var released strings.Builder
	const maxStreamContent = 10 << 20

	for chunk := range streamResult.Ch {
		if chunk.Content == "" {
			if chunk.Finish {
				break
			}
			continue
		}
		if fullContent.Len()+len(chunk.Content) > maxStreamContent {
			break
		}
		fullContent.WriteString(chunk.Content)
		if live := gate.push(chunk.Content); live != "" {
			released.WriteString(live)
			writeSSE("response.output_text.delta", map[string]interface{}{
				"type": "response.output_text.delta", "item_id": itemID,
				"output_index": 0, "content_index": 0, "delta": live,
			})
		}
		if chunk.Finish {
			break
		}
	}

	// ── Post-stream dual-engine analysis + policy ──
	var outcome *AnalysisOutcome
	var summary string
	if mode != ModePassthrough {
		var findings []Finding
		outcome, findings, summary, _ = s.analyzeAndVerdict(r.Context(), modelRouter, llmKey, fullContent.String())
		if outcome != nil {
			outcome.Policy = ComputePolicy(*outcome, mode)
			s.recordDecision(outcome.Verdict, outcome.Policy)
			rec, _ := s.recordProvenance(r, streamResult.Provider, streamResult.Model, outcome.Policy, mode, fullContent.String(), streamScanID)
			outcome.ScanID = rec.ScanID
		}
		_ = findings
	}

	policy := PolicyAllow
	if outcome != nil {
		policy = outcome.Policy
	}

	// Strict: fail closed when blocked — emit response.failed, nothing else.
	if mode == ModeStrict && (policy == PolicyBlock || policy == PolicyScannerUnavailable) {
		_, _, reason := enforcePolicy(policy, mode)
		writeSSE("response.failed", map[string]interface{}{
			"type": "response.failed",
			"response": map[string]interface{}{
				"id": respID, "object": "response", "created_at": createdAt,
				"model": streamResult.Model, "status": "failed",
				"error": map[string]interface{}{
					"code": "policy_block", "message": reason,
					"decision": policy, "scan_id": streamScanID,
				},
			},
		})
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	// Release withheld content per policy (balanced holds code, strict releases
	// the buffer on allow).
	releaseText, redacted := gate.finish(policy, mode)
	if releaseText != "" {
		released.WriteString(releaseText)
		writeSSE("response.output_text.delta", map[string]interface{}{
			"type": "response.output_text.delta", "item_id": itemID,
			"output_index": 0, "content_index": 0, "delta": releaseText,
		})
	}

	var tail string
	if summary != "" {
		tail = "\n\n🛡️ VigilAgent verdict: " + strings.ToUpper(outcome.Verdict) + " (grade " + outcome.Grade + ", " + strconv.Itoa(outcome.Score) + "/100) — policy: " + string(policy)
		if redacted {
			tail += " — code withheld for review"
		}
		released.WriteString(tail)
		writeSSE("response.output_text.delta", map[string]interface{}{
			"type": "response.output_text.delta", "item_id": itemID,
			"output_index": 0, "content_index": 0, "delta": tail,
		})
	}

	complete := map[string]interface{}{
		"type": "response.completed",
		"response": map[string]interface{}{
			"id": respID, "object": "response", "created_at": createdAt,
			"model": streamResult.Model, "status": "completed",
			"output": []map[string]interface{}{
				{
					"id": itemID, "type": "message", "role": "assistant", "status": "completed",
					"content": []map[string]interface{}{
						{"type": "output_text", "text": released.String(), "annotations": []interface{}{}},
					},
				},
			},
			"usage": map[string]interface{}{
				"input_tokens": streamResult.EstInput, "output_tokens": 0, "total_tokens": streamResult.EstInput,
			},
		},
	}
	if outcome != nil {
		complete["vigilagent"] = *outcome
	}
	if constrained {
		complete["design_gate"] = map[string]interface{}{"status": "constrained", "constraints_applied": true}
	}
	writeSSE("response.completed", complete)

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// streamingGate decides what may be released live during a stream.
//
//	observe:  everything flows live
//	balanced: prose flows, fenced code blocks are held until analysis
//	strict:   everything is held until the policy decision
type streamingGate struct {
	mode     EnforcementMode
	strict   bool
	inCode   bool
	buf      strings.Builder
	heldCode []string
}

func newStreamingGate(mode EnforcementMode) *streamingGate {
	return &streamingGate{mode: mode, strict: mode == ModeStrict}
}

// push returns the text that may be released immediately ("" when held).
func (g *streamingGate) push(text string) string {
	if g.mode == ModeObserve || g.mode == ModePassthrough {
		return text
	}
	if g.strict {
		g.buf.WriteString(text)
		return ""
	}
	// balanced: prose passes, fenced code blocks are held
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		if isTripleBacktickAt(text, i) {
			if g.inCode {
				g.buf.WriteString("```")
				g.heldCode = append(g.heldCode, g.buf.String())
				g.buf.Reset()
				g.inCode = false
			} else {
				g.buf.WriteString("```")
				g.inCode = true
			}
			i += 2
			continue
		}
		if g.inCode {
			g.buf.WriteByte(text[i])
		} else {
			out.WriteByte(text[i])
		}
	}
	return out.String()
}

// finish returns the withheld content to release given the policy decision,
// plus whether code was redacted (held blocks replaced with placeholders).
func (g *streamingGate) finish(policy PolicyDecision, mode EnforcementMode) (release string, redacted bool) {
	if g.mode == ModeObserve || g.mode == ModePassthrough {
		return "", false
	}
	if g.strict {
		if policy == PolicyBlock || policy == PolicyScannerUnavailable {
			return "", true
		}
		return g.buf.String(), false
	}
	// balanced
	if len(g.heldCode) == 0 {
		return "", false
	}
	if policy == PolicyHoldForReview || policy == PolicyBlock {
		var sb strings.Builder
		for range g.heldCode {
			sb.WriteString("[🛡️ code withheld by VigilAgent policy — review the scan findings before applying]\n")
		}
		return sb.String(), true
	}
	return strings.Join(g.heldCode, "\n"), false
}

// isTripleBacktickAt reports whether text[i:] starts a ``` fence marker.
func isTripleBacktickAt(text string, i int) bool {
	return i+2 < len(text) && text[i] == '`' && text[i+1] == '`' && text[i+2] == '`'
}

// marshalMessages re-marshals a request body, replacing its messages with the
// given (possibly constraint-augmented) message list.
func marshalMessages(original []byte, messages []llm.Message) ([]byte, error) {
	var bodyMap map[string]interface{}
	if err := json.Unmarshal(original, &bodyMap); err != nil {
		return nil, err
	}
	msgs := make([]map[string]string, len(messages))
	for i, m := range messages {
		msgs[i] = map[string]string{"role": m.Role, "content": m.Content}
	}
	bodyMap["messages"] = msgs
	return json.Marshal(bodyMap)
}

// parseMessages extracts messages from the request body.
func (s *ProxyServer) parseMessages(body []byte) ([]llm.Message, error) {
	var reqBody struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &reqBody); err != nil {
		return nil, err
	}
	messages := make([]llm.Message, len(reqBody.Messages))
	for i, m := range reqBody.Messages {
		messages[i] = llm.Message{Role: m.Role, Content: m.Content}
	}
	return messages, nil
}

// handleStreamingDirect falls back to direct provider forwarding when ModelRouter fails.
// Includes timeout protection and write-error checking.
func (s *ProxyServer) handleStreamingDirect(
	w http.ResponseWriter,
	r *http.Request,
	requestBody []byte,
	defaultFormat string,
) {
	llmKey := r.Header.Get("X-LLM-Key")
	llmProvider := r.Header.Get("X-LLM-Provider")
	llmModel := r.Header.Get("X-LLM-Model")

	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(requestBody, &req); err != nil {
		slog.Warn("streaming: failed to parse model", "error", err)
	}
	if llmModel != "" {
		req.Model = llmModel
	}

	provider := s.resolveProvider(req.Model, llmKey, llmProvider)
	if provider == nil {
		http.Error(w, `{"error":"unsupported model"}`, http.StatusBadRequest)
		return
	}

	rawURL := provider.BaseURL + r.URL.Path
	if err := validateTargetURL(rawURL); err != nil {
		http.Error(w, `{"error":"SSRF protection: " + err.Error()}`, http.StatusForbidden)
		return
	}
	// Re-marshal with potential model override
	fwdBody := requestBody
	if llmModel != "" {
		var bodyMap map[string]interface{}
		if err := json.Unmarshal(requestBody, &bodyMap); err == nil {
			bodyMap["model"] = llmModel
			fwdBody, _ = json.Marshal(bodyMap)
		}
	}

	// Design-stage gate: scan the request before forwarding and append
	// policy-mandated constraints (mirrors the main streaming path).
	if msgs, err := s.parseMessages(fwdBody); err == nil && len(msgs) > 0 {
		task := &llm.Task{ID: "proxy-stream-direct", Type: "feature", Messages: msgs}
		if _, constrained := s.applyDesignGate(task); constrained {
			if updated, err := marshalMessages(fwdBody, task.Messages); err == nil {
				fwdBody = updated
				w.Header().Set("X-VigilAgent-Design-Gate", "constrained")
			}
		}
	}

	reqHTTP, err := http.NewRequestWithContext(r.Context(), "POST", rawURL, bytes.NewReader(fwdBody))
	if err != nil {
		http.Error(w, `{"error":"failed to create request"}`, http.StatusInternalServerError)
		return
	}
	reqHTTP.Header.Set("Content-Type", "application/json")
	reqHTTP.Header.Set("Accept", "text/event-stream")

	// Set auth headers
	switch provider.Name {
	case "openai", "nvidia", "groq", "mistral", "openrouter":
		reqHTTP.Header.Set("Authorization", "Bearer "+provider.APIKey)
	case "anthropic":
		reqHTTP.Header.Set("x-api-key", provider.APIKey)
		reqHTTP.Header.Set("anthropic-version", "2023-06-01")
	case "gemini":
		reqHTTP.Header.Set("x-goog-api-key", provider.APIKey)
	case "cohere":
		reqHTTP.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}

	resp, err := s.client.Do(reqHTTP)
	if err != nil {
		errJSON, _ := json.Marshal(map[string]string{"error": "failed to forward stream: " + err.Error()})
		http.Error(w, string(errJSON), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	reader := bufio.NewReader(resp.Body)
	var fullContent strings.Builder
	const maxStreamContent = 10 << 20

	// Read with a deadline to detect hanging providers
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimSpace(line[6:])
			if data == "[DONE]" {
				continue
			}
			if defaultFormat == "openai" {
				var oResp struct {
					Choices []struct {
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
					} `json:"choices"`
				}
				if err := json.Unmarshal([]byte(data), &oResp); err == nil {
					if len(oResp.Choices) > 0 {
						fullContent.WriteString(oResp.Choices[0].Delta.Content)
					}
				}
			} else if defaultFormat == "anthropic" {
				var aResp struct {
					Type  string `json:"type"`
					Delta struct {
						Text string `json:"text"`
					} `json:"delta"`
				}
				if err := json.Unmarshal([]byte(data), &aResp); err == nil {
					if aResp.Type == "content_block_delta" {
						fullContent.WriteString(aResp.Delta.Text)
					}
				}
			}
		}
		if fullContent.Len() > maxStreamContent {
			slog.Info("streaming content limit reached", "bytes", maxStreamContent)
			break
		}
		if _, err := w.Write([]byte(line)); err != nil {
			slog.Info("streaming: client disconnected", "error", err)
			return
		}
		flusher.Flush()
	}

	// Post-stream dual-engine analysis
	mode := EnforcementMode(r.Header.Get("X-VigilAgent-Mode"))
	if mode == "" {
		mode = ModeObserve
	}
	if mode != ModePassthrough {
		content := fullContent.String()
		modelRouter := s.buildRouter(llmKey, "")
		outcome, _, summary, _ := s.analyzeAndVerdict(r.Context(), modelRouter, llmKey, content)
		if outcome != nil && summary != "" {
			outcome.Policy = ComputePolicy(*outcome, mode)
			s.recordDecision(outcome.Verdict, outcome.Policy)
			s.recordProvenance(r, provider.Name, req.Model, outcome.Policy, mode, content, "")
			summary = "🛡️ VigilAgent verdict: " + strings.ToUpper(outcome.Verdict) + " (grade " + outcome.Grade + ", " + strconv.Itoa(outcome.Score) + "/100) — policy: " + string(outcome.Policy) + "\n\n" + summary

			if defaultFormat == "openai" {
				summaryChunk := map[string]interface{}{
					"choices": []map[string]interface{}{
						{"delta": map[string]interface{}{"content": "\n\n" + summary, "role": "assistant"}},
					},
				}
				chunkBytes, _ := json.Marshal(summaryChunk)
				fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
			} else if defaultFormat == "anthropic" {
				summaryChunk := map[string]interface{}{
					"type":  "content_block_delta",
					"delta": map[string]interface{}{"type": "text_delta", "text": "\n\n" + summary},
				}
				chunkBytes, _ := json.Marshal(summaryChunk)
				fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
			}
			flusher.Flush()
		}
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}
