package compliance

import "testing"

func TestCheckPaymentTriggersPCIDSS(t *testing.T) {
	c := NewChecker()
	rep := c.Check("Build a payment processing service that stores card data", nil)

	if len(rep.Frameworks) == 0 {
		t.Fatal("expected at least one framework")
	}
	has := false
	for _, fw := range rep.Frameworks {
		if fw == FrameworkPCIDSS {
			has = true
		}
	}
	if !has {
		t.Fatalf("payment should trigger PCI-DSS, got frameworks %v", rep.Frameworks)
	}
}

func TestCheckSubtractsDeclared(t *testing.T) {
	c := NewChecker()
	rep := c.Check("payment processing system with credit card handling", []string{"pci_encrypt", "pci_access"})

	for _, m := range rep.Missing {
		for _, ctrl := range m.Controls {
			if ctrl.ID == "pci_encrypt" || ctrl.ID == "pci_access" {
				t.Fatalf("declared control %s should not be missing", ctrl.ID)
			}
		}
	}
}

func TestCheckNoEntitiesEmptyResult(t *testing.T) {
	c := NewChecker()
	rep := c.Check("a static marketing homepage", nil)

	if len(rep.Frameworks) != 0 {
		t.Fatalf("expected 0 frameworks, got %v", rep.Frameworks)
	}
	if len(rep.Required) != 0 {
		t.Fatalf("expected 0 required mappings, got %d", len(rep.Required))
	}
}

func TestCheckDeduplicatesControls(t *testing.T) {
	c := NewChecker()
	rep := c.Check("payment authentication personal data system", nil)

	seen := map[string]int{}
	for _, m := range rep.Required {
		for _, ctrl := range m.Controls {
			seen[ctrl.ID]++
		}
	}
	for id, count := range seen {
		if count > 1 {
			t.Fatalf("control %s appeared %d times (should be 1)", id, count)
		}
	}
}

func TestCheckAuthTriggersSOC2(t *testing.T) {
	c := NewChecker()
	rep := c.Check("Build a user authentication service with login system", nil)

	has := false
	for _, fw := range rep.Frameworks {
		if fw == FrameworkSOC2 {
			has = true
		}
	}
	if !has {
		t.Fatalf("auth should trigger SOC2, got frameworks %v", rep.Frameworks)
	}
}

func TestCheckPIITriggersHIPAA(t *testing.T) {
	c := NewChecker()
	rep := c.Check("system that stores patient health data and personal information", nil)

	has := false
	for _, fw := range rep.Frameworks {
		if fw == FrameworkHIPAA {
			has = true
		}
	}
	if !has {
		t.Fatalf("health/PII data should trigger HIPAA, got frameworks %v", rep.Frameworks)
	}
}

// === FALSE POSITIVE TESTS ===

func TestFalsePositive_SubstringAuth(t *testing.T) {
	c := NewChecker()
	// "authorization" contains "auth" but is not authentication
	rep := c.Check("authorization header validation middleware", nil)

	for _, fw := range rep.Frameworks {
		if fw == FrameworkSOC2 {
			t.Error("authorization-only text should NOT trigger SOC2 auth controls")
		}
	}
}

func TestFalsePositive_SubstringPayment(t *testing.T) {
	c := NewChecker()
	// "unpaid" contains "payment" substring but is not payment processing
	rep := c.Check("mark unpaid invoices as overdue", nil)

	for _, m := range rep.Required {
		for _, ctrl := range m.Controls {
			if ctrl.ID == "pci_encrypt" || ctrl.ID == "pci_access" {
				t.Errorf("unpaid invoice text should NOT trigger PCI-DSS controls, got %s", ctrl.ID)
			}
		}
	}
}

func TestFalsePositive_SubstringAPI(t *testing.T) {
	c := NewChecker()
	// "captcha" contains "api" — should not trigger anything
	rep := c.Check("implement captcha verification on signup form", nil)

	if len(rep.Required) != 0 {
		t.Errorf("captcha text should NOT trigger any controls, got %d", len(rep.Required))
	}
}

func TestFalsePositive_StaticPage(t *testing.T) {
	c := NewChecker()
	// Pure static content should trigger nothing
	descriptions := []string{
		"static marketing landing page",
		"blog post about cloud computing",
		"README documentation",
		"open source library for image processing",
	}

	for _, desc := range descriptions {
		rep := c.Check(desc, nil)
		if len(rep.Required) != 0 {
			t.Errorf("description %q should NOT trigger any controls, got %d", desc, len(rep.Required))
		}
	}
}

// === TRUE POSITIVE TESTS ===

func TestTruePositive_PaymentWithCardData(t *testing.T) {
	c := NewChecker()
	rep := c.Check("payment processing service that handles credit card data and checkout", nil)

	if len(rep.Required) == 0 {
		t.Error("payment+card data should trigger controls")
	}
	// Should trigger PCI-DSS
	hasPCI := false
	for _, fw := range rep.Frameworks {
		if fw == FrameworkPCIDSS {
			hasPCI = true
		}
	}
	if !hasPCI {
		t.Error("payment+card data should trigger PCI-DSS framework")
	}
}

func TestTruePositive_AuthWithLogin(t *testing.T) {
	c := NewChecker()
	rep := c.Check("authentication service with login and sign-in flows", nil)

	hasSOC2 := false
	for _, fw := range rep.Frameworks {
		if fw == FrameworkSOC2 {
			hasSOC2 = true
		}
	}
	if !hasSOC2 {
		t.Error("authentication+login should trigger SOC2")
	}
}

func TestTruePositive_HealthcarePII(t *testing.T) {
	c := NewChecker()
	rep := c.Check("medical record management system with patient health data", nil)

	hasHIPAA := false
	for _, fw := range rep.Frameworks {
		if fw == FrameworkHIPAA {
			hasHIPAA = true
		}
	}
	if !hasHIPAA {
		t.Error("patient health data should trigger HIPAA")
	}
}

func TestTruePositive_GDPRPersonalData(t *testing.T) {
	c := NewChecker()
	rep := c.Check("user profile system that stores personal data and email addresses", nil)

	hasGDPR := false
	for _, fw := range rep.Frameworks {
		if fw == FrameworkGDPR {
			hasGDPR = true
		}
	}
	if !hasGDPR {
		t.Error("personal data should trigger GDPR")
	}
}

// === WORD BOUNDARY TESTS ===

func TestWordBoundary_Auth(t *testing.T) {
	c := NewChecker()

	// Should match
	rep := c.Check("authentication system", nil)
	if len(rep.Required) == 0 {
		t.Error("'authentication' should match auth rule")
	}

	// Should NOT match bare "auth" as substring of unrelated words
	rep2 := c.Check("authorization token parsing", nil)
	hasAuth := false
	for _, m := range rep2.Required {
		if m.Entity == "auth" {
			hasAuth = true
		}
	}
	if hasAuth {
		t.Error("'authorization' should NOT match auth rule (word boundary)")
	}
}
