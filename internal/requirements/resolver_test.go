package requirements

import "testing"

func TestResolveDetectsPaymentControls(t *testing.T) {
	r := NewResolver()
	rep := r.Resolve("Build a payment processing service that stores card data", nil)

	if !contains(rep.Entities, "payment") {
		t.Fatalf("expected payment entity, got %v", rep.Entities)
	}
	if len(rep.Missing) != len(rep.Required) {
		t.Fatalf("with no declared controls, all required must be missing: %d vs %d", len(rep.Missing), len(rep.Required))
	}
	if len(rep.Satisfied) != 0 {
		t.Fatalf("expected 0 satisfied, got %d", len(rep.Satisfied))
	}
	if !hasControl(rep.Missing, "encryption") || !hasControl(rep.Missing, "audit_log") {
		t.Fatalf("payment must require encryption + audit_log, missing=%v", ids(rep.Missing))
	}
}

func TestResolveSubtractsDeclared(t *testing.T) {
	r := NewResolver()
	rep := r.Resolve("payment processing system with credit card handling", []string{"encryption", "AUDIT_LOG"})

	if hasControl(rep.Missing, "encryption") {
		t.Fatal("declared encryption must not be missing")
	}
	if hasControl(rep.Missing, "audit_log") {
		t.Fatal("declared audit_log (case-insensitive) must not be missing")
	}
	if !hasControl(rep.Satisfied, "encryption") {
		t.Fatal("declared+required encryption must be satisfied")
	}
}

func TestResolveNoEntitiesNoRequirements(t *testing.T) {
	r := NewResolver()
	rep := r.Resolve("a static marketing homepage", nil)
	if len(rep.Entities) != 0 || len(rep.Required) != 0 || len(rep.Missing) != 0 {
		t.Fatalf("expected empty result, got entities=%v required=%d", rep.Entities, len(rep.Required))
	}
}

func TestResolveDedupesControlsAcrossEntities(t *testing.T) {
	r := NewResolver()
	rep := r.Resolve("payment api with user authentication", nil)
	seen := 0
	for _, req := range rep.Required {
		if req.Control.ID == "rate_limit" {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("rate_limit must be de-duplicated across entities, appeared %d times", seen)
	}
}

// === FALSE POSITIVE TESTS ===

func TestFalsePositive_UnpaidInvoice(t *testing.T) {
	r := NewResolver()
	// "unpaid" contains "payment" but is not payment processing
	rep := r.Resolve("mark unpaid invoices as overdue", nil)

	if hasEntity(rep.Entities, "payment") {
		t.Error("'unpaid invoices' should NOT detect payment entity")
	}
	for _, req := range rep.Required {
		if req.Control.ID == "fraud_monitoring" || req.Control.ID == "encryption" {
			t.Errorf("unpaid invoices should NOT require %s", req.Control.ID)
		}
	}
}

func TestFalsePositive_AuthorizationOnly(t *testing.T) {
	r := NewResolver()
	// "authorization" contains "auth" but is not authentication
	rep := r.Resolve("authorization header validation middleware", nil)

	if hasEntity(rep.Entities, "auth") {
		t.Error("'authorization' should NOT detect auth entity (word boundary)")
	}
}

func TestFalsePositive_Captcha(t *testing.T) {
	r := NewResolver()
	// "captcha" contains "api" substring
	rep := r.Resolve("implement captcha verification on signup form", nil)

	if hasEntity(rep.Entities, "api") {
		t.Error("'captcha' should NOT detect api entity")
	}
}

func TestFalsePositive_StaticContent(t *testing.T) {
	r := NewResolver()
	descriptions := []string{
		"static marketing landing page",
		"blog post about cloud computing",
		"open source library for image processing",
		"readme documentation",
	}
	for _, desc := range descriptions {
		rep := r.Resolve(desc, nil)
		if len(rep.Required) != 0 {
			t.Errorf("description %q should NOT trigger any requirements, got %d", desc, len(rep.Required))
		}
	}
}

func TestFalsePositive_ByteBuffer(t *testing.T) {
	r := NewResolver()
	// "ByteBuffer" contains "auth" substring — must not match
	rep := r.Resolve("create a ByteBuffer for network I/O", nil)
	if hasEntity(rep.Entities, "auth") {
		t.Error("'ByteBuffer' should NOT detect auth entity")
	}
}

// === TRUE POSITIVE TESTS ===

func TestTruePositive_PaymentFull(t *testing.T) {
	r := NewResolver()
	rep := r.Resolve("payment processing service with checkout and billing system", nil)

	if !hasEntity(rep.Entities, "payment") {
		t.Error("payment+checkout+billing should detect payment entity")
	}
	if !hasControl(rep.Required, "encryption") {
		t.Error("payment should require encryption")
	}
	if !hasControl(rep.Required, "fraud_monitoring") {
		t.Error("payment should require fraud monitoring")
	}
}

func TestTruePositive_AuthWithLogin(t *testing.T) {
	r := NewResolver()
	rep := r.Resolve("authentication service with login and sign-in flows", nil)

	if !hasEntity(rep.Entities, "auth") {
		t.Error("authentication+login should detect auth entity")
	}
	if !hasControl(rep.Required, "mfa") {
		t.Error("auth should require MFA")
	}
}

func TestTruePositive_PersonalData(t *testing.T) {
	r := NewResolver()
	rep := r.Resolve("user profile system that stores personal data and email addresses", nil)

	if !hasEntity(rep.Entities, "pii") {
		t.Error("personal data should detect pii entity")
	}
	if !hasControl(rep.Required, "consent") {
		t.Error("pii should require consent management")
	}
}

// === WORD BOUNDARY TESTS ===

func TestWordBoundary_API(t *testing.T) {
	r := NewResolver()

	// " api" with leading space should match
	rep := r.Resolve("build a REST api for users", nil)
	if !hasEntity(rep.Entities, "api") {
		t.Error("'api' should match api rule")
	}

	// "endpoint" should match
	rep2 := r.Resolve("user endpoint for data retrieval", nil)
	if !hasEntity(rep2.Entities, "api") {
		t.Error("'endpoint' should match api rule")
	}
}

func TestWordBoundary_Auth(t *testing.T) {
	r := NewResolver()

	// "authentication" should match
	rep := r.Resolve("user authentication system", nil)
	if !hasEntity(rep.Entities, "auth") {
		t.Error("'authentication' should match auth entity")
	}

	// "authorization" should NOT match
	rep2 := r.Resolve("authorization token parsing middleware", nil)
	if hasEntity(rep2.Entities, "auth") {
		t.Error("'authorization' should NOT match auth entity (word boundary)")
	}
}

func TestWordBoundary_Payment(t *testing.T) {
	r := NewResolver()

	// "payment" should match
	rep := r.Resolve("payment processing system", nil)
	if !hasEntity(rep.Entities, "payment") {
		t.Error("'payment' should match payment entity")
	}

	// "unpaid" should NOT match
	rep2 := r.Resolve("unpaid invoice tracking", nil)
	if hasEntity(rep2.Entities, "payment") {
		t.Error("'unpaid' should NOT match payment entity")
	}
}

// === HELPERS ===

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func hasEntity(entities []string, want string) bool {
	return contains(entities, want)
}

func hasControl(reqs []Requirement, id string) bool {
	for _, r := range reqs {
		if r.Control.ID == id {
			return true
		}
	}
	return false
}

func ids(reqs []Requirement) []string {
	out := make([]string, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, r.Control.ID)
	}
	return out
}
