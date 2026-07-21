package scanner

// These additional baseline cases cover every builtin rule that was missing
// from the original getBaselineCases() set.
func getExtraBaselineCases() []baselineCase {
	return []baselineCase{
		// ── secrets ──────────────────────────────────────────────
		{
			name:        "aws_secret_key",
			filename:    "aws.go",
			code:        `aws_secret_access_key = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`,
			expectRules: []string{"aws_secret_key"},
			minFindings: 1,
		},
		{
			name:        "github_token",
			filename:    "ci.go",
			code:        `var token = "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef1234"`,
			expectRules: []string{"github_token"},
			minFindings: 1,
		},
		{
			name:        "slack_token",
			filename:    "webhook.go",
			code:        `var token = "xoxb-fake-slack-token-for-testing-purposes"`,
			expectRules: []string{"slack_token"},
			minFindings: 1,
		},
		{
			name:        "private_key_literal",
			filename:    "keys.go",
			code:        `var key = "-----BEGIN RSA PRIVATE KEY-----"`,
			expectRules: []string{"private_key_literal"},
			minFindings: 1,
		},
		{
			name:        "gcp_service_account_key",
			filename:    "gcp.go",
			code:        `"type": "service_account"`,
			expectRules: []string{"gcp_service_account_key"},
			minFindings: 1,
		},
		{
			name:        "world_readable_secret",
			filename:    "deploy.go",
			code:        `os.WriteFile("secret.key", data, 0664)`,
			expectRules: []string{"world_readable_secret"},
			minFindings: 1,
		},

		// ── crypto ───────────────────────────────────────────────
		{
			name:        "weak_cipher_des",
			filename:    "crypto.go",
			code:        `c, _ := des.NewCipher(key)`,
			expectRules: []string{"weak_cipher_des"},
			minFindings: 1,
		},
		{
			name:        "insecure_ecb_mode",
			filename:    "crypto.go",
			code:        `mode := ecb.NewEncrypter(block)`,
			expectRules: []string{"insecure_ecb_mode"},
			minFindings: 1,
		},
		{
			name:        "hardcoded_iv",
			filename:    "crypto.go",
			code:        `iv := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10}`,
			expectRules: []string{"hardcoded_iv"},
			minFindings: 1,
		},

		// ── injection ────────────────────────────────────────────
		{
			name:        "template_injection",
			filename:    "handler.go",
			code:        `t := template.New(req.Input)`,
			expectRules: []string{"template_injection"},
			minFindings: 1,
		},
		{
			name:        "log_injection",
			filename:    "handler.go",
			code:        `log.Printf("user %s logged in", req.Username)`,
			expectRules: []string{"log_injection"},
			minFindings: 1,
		},

		// ── path traversal ──────────────────────────────────────
		{
			name:        "path_traversal_unsanitized",
			filename:    "file.go",
			code:        `f, _ := os.Open("uploads/" + r.FileName)`,
			expectRules: []string{"path_traversal_unsanitized"},
			minFindings: 1,
		},

		// ── SSRF ────────────────────────────────────────────────
		{
			name:        "ssrf_http_client",
			filename:    "client.go",
			code:        `resp, _ := Client.Get(req.URL)`,
			expectRules: []string{"ssrf_http_client"},
			minFindings: 1,
		},
		{
			name:        "ssrf_url_parse",
			filename:    "handler.go",
			code:        `u, _ := url.Parse(req.URL)`,
			expectRules: []string{"ssrf_url_parse"},
			minFindings: 1,
		},

		// ── deserialization ─────────────────────────────────────
		{
			name:        "unsafe_xml_parse",
			filename:    "xml.go",
			code:        `dec := xml.NewDecoder(r.Body)`,
			expectRules: []string{"unsafe_xml_parse"},
			minFindings: 1,
		},
		{
			name:        "unsafe_yaml_decode",
			filename:    "yaml.go",
			code:        `yaml.Unmarshal(data, &cfg)`,
			expectRules: []string{"unsafe_yaml_decode"},
			minFindings: 1,
		},
		{
			name:        "gorilla_unsafe_mux",
			filename:    "mux.go",
			code:        `id := mux.Vars(r)["id"]`,
			expectRules: []string{"gorilla_unsafe_mux"},
			minFindings: 1,
		},

		// ── info disclosure ─────────────────────────────────────
		{
			name:        "stack_trace_exposure",
			filename:    "debug.go",
			code:        `debug.PrintStack()`,
			expectRules: []string{"stack_trace_exposure"},
			minFindings: 1,
		},
		{
			name:        "verbose_error_handler",
			filename:    "handler.go",
			code:        `http.Error(w, err.Error(), 500)`,
			expectRules: []string{"verbose_error_handler"},
			minFindings: 1,
		},
		{
			name:        "debug_endpoint_exposed",
			filename:    "server.go",
			code:        `import _ "net/http/pprof"`,
			expectRules: []string{"debug_endpoint_exposed"},
			minFindings: 1,
		},

		// ── permissions ─────────────────────────────────────────
		{
			name:        "chmod_777",
			filename:    "setup.go",
			code:        `os.Chmod("/tmp/data", 0777)`,
			expectRules: []string{"chmod_777"},
			minFindings: 1,
		},

		// ── go-specific ─────────────────────────────────────────
		{
			name:        "mutex_not_unlocked",
			filename:    "cache.go",
			code:        "mu.Lock()\n",
			expectRules: []string{"mutex_not_unlocked"},
			minFindings: 1,
		},
		{
			name:        "context_leak",
			filename:    "handler.go",
			code:        `ctx := context.Background()`,
			expectRules: []string{"context_leak"},
			minFindings: 1,
		},
		{
			name:        "time_sleep_in_handler",
			filename:    "handler.go",
			code:        `time.Sleep(5 * time.Second)`,
			expectRules: []string{"time_sleep_in_handler"},
			minFindings: 1,
		},
	}
}
