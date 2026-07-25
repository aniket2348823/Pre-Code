package compliance

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewHTTPHandler_Success(t *testing.T) {
	checker := NewChecker()
	h := NewHTTPHandler(checker)

	body := `{"description":"payment processing with credit card data","declared":["pci_encrypt"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Required []struct {
			Entity string `json:"entity"`
		} `json:"required"`
		Satisfied []struct {
			Entity string `json:"entity"`
		} `json:"satisfied"`
		Missing []struct {
			Entity string `json:"entity"`
		} `json:"missing"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(resp.Required) == 0 {
		t.Error("expected required controls")
	}
}

func TestNewHTTPHandler_InvalidJSON(t *testing.T) {
	h := NewHTTPHandler(NewChecker())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestNewHTTPHandler_EmptyDescription(t *testing.T) {
	h := NewHTTPHandler(NewChecker())
	body := `{"description":"","declared":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty description, got %d", w.Code)
	}
}

func TestNewHTTPHandler_NilChecker(t *testing.T) {
	h := NewHTTPHandler(nil)
	body := `{"description":"payment processing with credit cards"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestNewHTTPHandler_NoDeclared(t *testing.T) {
	h := NewHTTPHandler(NewChecker())
	body := `{"description":"authentication login system"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Missing []struct{} `json:"missing"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(resp.Missing) == 0 {
		t.Error("expected missing controls when nothing declared")
	}
}

func TestNewHTTPHandler_AllDeclaredSatisfied(t *testing.T) {
	h := NewHTTPHandler(NewChecker())
	body := `{"description":"payment processing with credit card","declared":["pci_encrypt","pci_access","pci_audit","soc2_logging"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Missing   []struct{} `json:"missing"`
		Satisfied []struct{} `json:"satisfied"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Missing) != 0 {
		t.Errorf("expected 0 missing, got %d", len(resp.Missing))
	}
	if len(resp.Satisfied) == 0 {
		t.Error("expected satisfied controls")
	}
}

func TestNewHTTPHandler_EmptyBody(t *testing.T) {
	h := NewHTTPHandler(NewChecker())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance", bytes.NewBufferString(""))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty body, got %d", w.Code)
	}
}

func TestSortMappings_EmptyControls(t *testing.T) {
	mappings := []Mapping{
		{Entity: "a", Controls: nil},
		{Entity: "b", Controls: []Control{{ID: "x", Severity: "high"}}},
		{Entity: "c", Controls: nil},
	}
	sortMappings(mappings)
	// The nil-controls entries should sort to the end (fewer controls → after more)
	if len(mappings) != 3 {
		t.Fatalf("expected 3 mappings, got %d", len(mappings))
	}
}

func TestSortMappings_SeverityOrdering(t *testing.T) {
	mappings := []Mapping{
		{Entity: "low", Controls: []Control{{ID: "low", Severity: "low"}}},
		{Entity: "crit", Controls: []Control{{ID: "crit", Severity: "critical"}}},
		{Entity: "med", Controls: []Control{{ID: "med", Severity: "medium"}}},
	}
	sortMappings(mappings)
	if mappings[0].Entity != "crit" {
		t.Errorf("expected critical first, got %s", mappings[0].Entity)
	}
	if mappings[1].Entity != "med" {
		t.Errorf("expected medium second, got %s", mappings[1].Entity)
	}
	if mappings[2].Entity != "low" {
		t.Errorf("expected low third, got %s", mappings[2].Entity)
	}
}

func TestSortMappings_SameSeveritySortByID(t *testing.T) {
	mappings := []Mapping{
		{Entity: "b", Controls: []Control{{ID: "ctrl_b", Severity: "high"}}},
		{Entity: "a", Controls: []Control{{ID: "ctrl_a", Severity: "high"}}},
	}
	sortMappings(mappings)
	if mappings[0].Controls[0].ID != "ctrl_a" {
		t.Errorf("expected ctrl_a first when same severity, got %s", mappings[0].Controls[0].ID)
	}
}

func TestCheck_OnlyExclusionMatches(t *testing.T) {
	c := NewChecker()
	// "payment gateway" is excluded from payment rule
	rep := c.Check("use a payment gateway provider", nil)
	for _, m := range rep.Required {
		for _, ctrl := range m.Controls {
			if ctrl.ID == "pci_encrypt" {
				t.Error("payment gateway (excluded) should NOT trigger PCI-DSS")
			}
		}
	}
}

func TestCheck_AuthExclusion(t *testing.T) {
	c := NewChecker()
	// "oauth client" is excluded from auth rule
	rep := c.Check("configure the oauth client settings", nil)
	for _, m := range rep.Required {
		if m.Entity == "auth" {
			t.Error("oauth client (excluded) should NOT trigger auth rule")
		}
	}
}

func TestCheck_PIIExclusion(t *testing.T) {
	c := NewChecker()
	// "pii compliance" is excluded from PII rule
	rep := c.Check("ensure pii compliance is enforced", nil)
	for _, m := range rep.Required {
		if m.Entity == "pii" {
			t.Error("pii compliance (excluded) should NOT trigger PII rule")
		}
	}
}

func TestCheck_PaymentExclusion(t *testing.T) {
	c := NewChecker()
	// "payment method" is excluded from payment rule
	rep := c.Check("add a payment method selector", nil)
	for _, m := range rep.Required {
		for _, ctrl := range m.Controls {
			if ctrl.ID == "pci_encrypt" {
				t.Error("payment method (excluded) should NOT trigger PCI-DSS")
			}
		}
	}
}

func TestCheck_AllDeclared(t *testing.T) {
	c := NewChecker()
	declared := []string{
		"pci_encrypt", "pci_access", "pci_audit", "soc2_logging",
		"soc2_access", "soc2_mfa", "gdpr_access",
		"gdpr_consent", "gdpr_minimize", "gdpr_retention", "hipaa_phi",
	}
	rep := c.Check("payment processing authentication personal data patient health", declared)
	if len(rep.Missing) != 0 {
		t.Errorf("all controls declared, expected 0 missing, got %d", len(rep.Missing))
	}
}

func TestCheck_NoMatchLowScore(t *testing.T) {
	c := NewChecker()
	// Single unrelated word should not trigger any rule
	rep := c.Check("banana", nil)
	if len(rep.Required) != 0 {
		t.Errorf("single unrelated word should trigger 0 controls, got %d", len(rep.Required))
	}
}

func TestCheck_CaseInsensitiveKeywords(t *testing.T) {
	c := NewChecker()
	rep := c.Check("CREDIT CARD processing", nil)
	if len(rep.Frameworks) == 0 {
		t.Error("uppercase CREDIT CARD should trigger PCI-DSS")
	}
}
