package compliance

import (
	"strings"
	"testing"
)

func TestCheck_EmptyDescription(t *testing.T) {
	c := NewChecker()
	rep := c.Check("", nil)
	if len(rep.Frameworks) != 0 {
		t.Errorf("empty description should trigger 0 frameworks, got %d", len(rep.Frameworks))
	}
}

func TestCheck_WhitespaceOnly(t *testing.T) {
	c := NewChecker()
	rep := c.Check("   \n\t  \n  ", nil)
	if len(rep.Required) != 0 {
		t.Errorf("whitespace-only should trigger 0 controls, got %d", len(rep.Required))
	}
}

func TestCheck_NonEnglish(t *testing.T) {
	c := NewChecker()
	rep := c.Check("构建一个支付处理系统", nil)
	if len(rep.Required) != 0 {
		t.Errorf("non-English text should not trigger controls, got %d", len(rep.Required))
	}
}

func TestCheck_UppercaseKeywords(t *testing.T) {
	c := NewChecker()
	rep := c.Check("PAYMENT processing system", nil)
	if len(rep.Frameworks) == 0 {
		t.Error("uppercase PAYMENT should still trigger PCI-DSS")
	}
}

func TestCheck_KeywordWithPunctuation(t *testing.T) {
	c := NewChecker()
	rep := c.Check("Build a payment.", nil)
	if len(rep.Frameworks) == 0 {
		t.Error("'payment.' with punctuation should still trigger PCI-DSS")
	}
}

func TestCheck_AuthAsPrefix(t *testing.T) {
	c := NewChecker()
	rep := c.Check("authenticate the user with password", nil)
	if len(rep.Required) == 0 {
		t.Error("'authenticate' should trigger auth controls")
	}
}

func TestCheck_AuthAsSuffix(t *testing.T) {
	c := NewChecker()
	rep := c.Check("superauth system for access control", nil)
	// "superauth" should NOT match "auth" keyword (word boundary)
	for _, m := range rep.Required {
		if m.Entity == "auth" {
			t.Error("'superauth' should NOT match auth rule via word boundary")
		}
	}
}

func TestCheck_NonexistentControls(t *testing.T) {
	c := NewChecker()
	rep := c.Check("payment processing", []string{"nonexistent_control_1", "nonexistent_control_2"})
	// All required controls should be missing
	if len(rep.Missing) != len(rep.Required) {
		t.Errorf("all controls should be missing, got %d missing of %d required", len(rep.Missing), len(rep.Required))
	}
}

func TestCheck_DuplicateDeclaredControls(t *testing.T) {
	c := NewChecker()
	rep := c.Check("payment processing with credit cards", []string{"pci_encrypt", "pci_encrypt", "pci_access"})
	// Duplicate declarations should still work — at least one declared control should be satisfied
	found := false
	for _, m := range rep.Satisfied {
		for _, ctrl := range m.Controls {
			if ctrl.ID == "pci_encrypt" || ctrl.ID == "pci_access" {
				found = true
			}
		}
	}
	if !found {
		t.Error("declared controls should be satisfied")
	}
}

func TestCheck_AllFrameworksTriggered(t *testing.T) {
	c := NewChecker()
	rep := c.Check("payment authentication personal data health records", nil)
	frameworkSet := map[Framework]bool{}
	for _, fw := range rep.Frameworks {
		frameworkSet[fw] = true
	}
	if !frameworkSet[FrameworkPCIDSS] {
		t.Error("should trigger PCI-DSS")
	}
	if !frameworkSet[FrameworkSOC2] {
		t.Error("should trigger SOC2")
	}
	if !frameworkSet[FrameworkGDPR] {
		t.Error("should trigger GDPR")
	}
	if !frameworkSet[FrameworkHIPAA] {
		t.Error("should trigger HIPAA")
	}
}

func TestCheck_MultipleFrameworksSimultaneously(t *testing.T) {
	c := NewChecker()
	rep := c.Check("PCI-DSS payment processing with GDPR personal data and HIPAA patient records and SOC2 authentication login", nil)
	frameworkSet := map[Framework]bool{}
	for _, fw := range rep.Frameworks {
		frameworkSet[fw] = true
	}
	if len(frameworkSet) < 3 {
		t.Errorf("expected at least 3 frameworks, got %d: %v", len(frameworkSet), rep.Frameworks)
	}
}

func TestCheck_LongDescription(t *testing.T) {
	c := NewChecker()
	longDesc := strings.Repeat("Build a secure payment processing system that handles credit card data. ", 50)
	rep := c.Check(longDesc, nil)
	if len(rep.Required) == 0 {
		t.Error("long description with payment keywords should trigger controls")
	}
}

func TestCheck_RegexSpecialChars(t *testing.T) {
	c := NewChecker()
	rep := c.Check("payment (test) [data] {system} $value #hash", nil)
	if len(rep.Frameworks) == 0 {
		t.Error("description with regex special chars should still trigger PCI-DSS")
	}
}

func TestCheck_SQLInDescription(t *testing.T) {
	c := NewChecker()
	rep := c.Check("payment system; DROP TABLE users; --", nil)
	if len(rep.Frameworks) == 0 {
		t.Error("SQL injection in description should still trigger PCI-DSS")
	}
}

func TestCheck_DeduplicationManyIdentical(t *testing.T) {
	c := NewChecker()
	desc := strings.Repeat("payment ", 100) + "credit card processing"
	rep := c.Check(desc, nil)
	seen := map[string]int{}
	for _, m := range rep.Required {
		for _, ctrl := range m.Controls {
			seen[ctrl.ID]++
		}
	}
	for id, count := range seen {
		if count > 1 {
			t.Errorf("control %s appeared %d times (should be deduplicated to 1)", id, count)
		}
	}
}

func TestReport_FrameworksSorted(t *testing.T) {
	c := NewChecker()
	rep := c.Check("payment authentication personal data", nil)
	for i := 1; i < len(rep.Frameworks); i++ {
		if rep.Frameworks[i] < rep.Frameworks[i-1] {
			t.Errorf("frameworks not sorted: %v", rep.Frameworks)
			break
		}
	}
}
