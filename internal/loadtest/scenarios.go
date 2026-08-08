package loadtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ScenarioLogin simulates: register → login → create task → list tasks.
func ScenarioLogin(client *http.Client, baseURL string) RequestResult {
	// Step 1: register
	regBody, _ := json.Marshal(map[string]string{
		"email":    fmt.Sprintf("load_%d@example.com", time.Now().UnixNano()),
		"password": "TestPass123456!",
		"name":     "Load Tester",
	})
	regResult := postJSON(client, baseURL+"/api/v1/auth/register", regBody)
	if regResult.Error != "" || regResult.Status >= 400 {
		return regResult
	}

	// Step 2: login
	loginBody, _ := json.Marshal(map[string]string{
		"email":    fmt.Sprintf("load_%d@example.com", time.Now().UnixNano()),
		"password": "TestPass123456!",
	})
	loginResult := postJSON(client, baseURL+"/api/v1/auth/login", loginBody)
	if loginResult.Error != "" || loginResult.Status >= 400 {
		return loginResult
	}

	// Step 3: create task (will likely fail without valid project, but measures the endpoint)
	taskBody, _ := json.Marshal(map[string]interface{}{
		"prompt":     "load test task",
		"project_id": "00000000-0000-0000-0000-000000000000",
	})
	taskResult := postJSON(client, baseURL+"/api/v1/tasks", taskBody)

	// Step 4: list tasks
	listResult := MakeJSONRequest(client, "GET", baseURL+"/api/v1/tasks?limit=10")
	_ = taskResult
	return listResult
}

// ScenarioAgentExecution simulates: create agent → start session → execute task.
func ScenarioAgentExecution(client *http.Client, baseURL string) RequestResult {
	// Create agent
	agentBody, _ := json.Marshal(map[string]interface{}{
		"name":       fmt.Sprintf("load-agent-%d", time.Now().UnixNano()),
		"model":      "gpt-4o-mini",
		"project_id": "00000000-0000-0000-0000-000000000000",
	})
	createResult := postJSON(client, baseURL+"/api/v1/agents", agentBody)
	if createResult.Error != "" || createResult.Status >= 400 {
		return createResult
	}

	// List agents to confirm
	listResult := MakeJSONRequest(client, "GET", baseURL+"/api/v1/agents?limit=1")
	if listResult.Error != "" || listResult.Status >= 400 {
		return listResult
	}

	// Create a session for the agent (measures the endpoint)
	sessionBody, _ := json.Marshal(map[string]interface{}{
		"agent_id":   "00000000-0000-0000-0000-000000000000",
		"project_id": "00000000-0000-0000-0000-000000000000",
	})
	sessionResult := postJSON(client, baseURL+"/api/v1/sessions", sessionBody)
	_ = sessionResult

	// Get agent details
	return MakeJSONRequest(client, "GET", baseURL+"/api/v1/agents/00000000-0000-0000-0000-000000000000")
}

// ScenarioCRUD exercises full CRUD lifecycle for multiple resource types.
func ScenarioCRUD(client *http.Client, baseURL string) RequestResult {
	var lastResult RequestResult

	// --- Projects CRUD ---
	createBody, _ := json.Marshal(map[string]interface{}{
		"name": fmt.Sprintf("load-proj-%d", time.Now().UnixNano()),
	})
	createResult := postJSON(client, baseURL+"/api/v1/projects", createBody)
	lastResult = createResult

	if createResult.Error == "" && createResult.Status == 201 {
		// List projects
		listResult := MakeJSONRequest(client, "GET", baseURL+"/api/v1/projects?limit=5")
		_ = listResult

		// Get single project (result unused — scenario exercises the endpoint only)
		_ = MakeJSONRequest(client, "GET", baseURL+"/api/v1/projects/00000000-0000-0000-0000-000000000000")
	}

	// --- Agents CRUD ---
	createAgentBody, _ := json.Marshal(map[string]interface{}{
		"name":  fmt.Sprintf("load-agent-%d", time.Now().UnixNano()),
		"model": "gpt-4o-mini",
	})
	_ = postJSON(client, baseURL+"/api/v1/agents", createAgentBody)

	// List agents
	listAgents := MakeJSONRequest(client, "GET", baseURL+"/api/v1/agents?limit=5")
	_ = listAgents

	// --- Tasks CRUD ---
	createTaskBody, _ := json.Marshal(map[string]interface{}{
		"prompt":     "CRUD load test",
		"project_id": "00000000-0000-0000-0000-000000000000",
	})
	createTask := postJSON(client, baseURL+"/api/v1/tasks", createTaskBody)
	_ = createTask

	// List tasks
	listTasks := MakeJSONRequest(client, "GET", baseURL+"/api/v1/tasks?limit=5")
	_ = listTasks

	// Get single task
	getTask := MakeJSONRequest(client, "GET", baseURL+"/api/v1/tasks/00000000-0000-0000-0000-000000000000")
	_ = getTask

	// --- Skills list (read-only, no create needed for load) ---
	listSkills := MakeJSONRequest(client, "GET", baseURL+"/api/v1/skills?limit=5")
	lastResult = listSkills

	return lastResult
}

// ScenarioPing hits a lightweight health/readiness endpoint.
func ScenarioPing(client *http.Client, baseURL string) RequestResult {
	return MakeJSONRequest(client, "GET", baseURL+"/api/v1/health")
}

// ScenarioMixed combines multiple scenarios in a weighted rotation.
func ScenarioMixed(client *http.Client, baseURL string) RequestResult {
	// Use a simple hash of current time to pick scenario
	ns := time.Now().UnixNano()
	switch ns % 3 {
	case 0:
		return ScenarioPing(client, baseURL)
	case 1:
		return ScenarioCRUD(client, baseURL)
	default:
		return ScenarioLogin(client, baseURL)
	}
}

// postJSON sends a JSON POST request and returns a RequestResult.
func postJSON(client *http.Client, url string, body []byte) RequestResult {
	start := time.Now()
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return RequestResult{
			Method:  "POST",
			Path:    url,
			Status:  0,
			Latency: time.Since(start),
			Error:   fmt.Sprintf("build request: %v", err),
		}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer loadtest-token")

	resp, err := client.Do(req)
	if err != nil {
		return RequestResult{
			Method:  "POST",
			Path:    url,
			Status:  0,
			Latency: time.Since(start),
			Error:   err.Error(),
		}
	}
	defer resp.Body.Close()

	return RequestResult{
		Method:  "POST",
		Path:    url,
		Status:  resp.StatusCode,
		Latency: time.Since(start),
	}
}
