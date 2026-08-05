//go:build go1.18

package router

import (
	"encoding/json"
	"testing"

	"gopkg.in/yaml.v3"
)

func FuzzGenerateOpenAPI(f *testing.F) {
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, _ []byte) {
		spec := GenerateOpenAPI()
		if spec == nil {
			t.Fatal("expected non-nil spec")
		}
		if spec.OpenAPI != "3.0.3" {
			t.Errorf("expected openapi 3.0.3, got %q", spec.OpenAPI)
		}
		if spec.Info.Title == "" {
			t.Error("expected non-empty title")
		}
		if spec.Info.Version == "" {
			t.Error("expected non-empty version")
		}
		if len(spec.Paths) == 0 {
			t.Error("expected at least one path")
		}
	})
}

func FuzzGenerateOpenAPI_YAMLMarshal(f *testing.F) {
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, _ []byte) {
		spec := GenerateOpenAPI()
		data, err := spec.MarshalYAML()
		if err != nil {
			t.Fatalf("failed to marshal YAML: %v", err)
		}
		if len(data) == 0 {
			t.Fatal("expected non-empty YAML")
		}

		var parsed map[string]interface{}
		if err := yaml.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("generated YAML is invalid: %v", err)
		}
		if parsed["openapi"] != "3.0.3" {
			t.Errorf("expected openapi 3.0.3 in YAML, got %v", parsed["openapi"])
		}
	})
}

func FuzzGenerateOpenAPI_JSONMarshal(f *testing.F) {
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, _ []byte) {
		spec := GenerateOpenAPI()
		data, err := spec.MarshalJSONBytes()
		if err != nil {
			t.Fatalf("failed to marshal JSON: %v", err)
		}
		if len(data) == 0 {
			t.Fatal("expected non-empty JSON")
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("generated JSON is invalid: %v", err)
		}
		if parsed["openapi"] != "3.0.3" {
			t.Errorf("expected openapi 3.0.3 in JSON, got %v", parsed["openapi"])
		}
	})
}

func FuzzGenerateOpenAPI_AllPathsHaveResponses(f *testing.F) {
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, _ []byte) {
		spec := GenerateOpenAPI()
		for path, item := range spec.Paths {
			ops := []*Operation{
				item.Get, item.Post, item.Put, item.Patch, item.Delete,
			}
			for _, op := range ops {
				if op == nil {
					continue
				}
				if len(op.Responses) == 0 {
					t.Errorf("path %q operation %q has no responses", path, op.OperationID)
				}
			}
		}
	})
}

func FuzzGenerateOpenAPI_ProtectedRoutesHaveSecurity(f *testing.F) {
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, _ []byte) {
		spec := GenerateOpenAPI()
		publicPaths := map[string]bool{
			"/health":                        true,
			"/ready":                         true,
			"/docs":                          true,
			"/docs/openapi.yaml":             true,
			"/auth/register":                 true,
			"/auth/login":                    true,
			"/auth/forgot-password":          true,
			"/auth/reset-password":           true,
			"/auth/verify-email":             true,
			"/providers":                     true,
			"/providers/{providerID}/models": true,
			"/models/{modelID}":              true,
		}
		for path, item := range spec.Paths {
			if publicPaths[path] {
				continue
			}
			ops := []*Operation{
				item.Get, item.Post, item.Put, item.Patch, item.Delete,
			}
			for _, op := range ops {
				if op == nil {
					continue
				}
				if len(op.Security) == 0 {
					t.Errorf("protected path %q operation %q missing security", path, op.OperationID)
				}
			}
		}
	})
}

func FuzzGenerateOpenAPI_SchemasPresent(f *testing.F) {
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, _ []byte) {
		spec := GenerateOpenAPI()
		if len(spec.Components.Schemas) == 0 {
			t.Error("expected at least one schema")
		}
		if len(spec.Components.SecuritySchemes) == 0 {
			t.Error("expected at least one security scheme")
		}
	})
}

func FuzzOpenAPISpec_addPath(f *testing.F) {
	f.Add("/test/path", "GET", "Test operation")
	f.Add("/", "POST", "Root operation")
	f.Add("/deep/nested/path", "DELETE", "Deep path")

	f.Fuzz(func(t *testing.T, path, method, summary string) {
		spec := &OpenAPISpec{
			OpenAPI: "3.0.3",
			Info:    Info{Title: "Test", Version: "1.0.0"},
			Paths:   make(map[string]*PathItem),
			Components: Components{
				SecuritySchemes: map[string]*SecurityScheme{},
				Schemas:         map[string]Schema{},
			},
		}

		op := &Operation{
			Summary: summary,
			Responses: map[string]*Response{
				"200": {Description: "OK"},
			},
		}

		item := &PathItem{}
		switch method {
		case "GET":
			item.Get = op
		case "POST":
			item.Post = op
		case "PUT":
			item.Put = op
		case "PATCH":
			item.Patch = op
		case "DELETE":
			item.Delete = op
		default:
			return
		}

		spec.addPath(path, item)

		if _, ok := spec.Paths[path]; !ok {
			t.Errorf("path %q not added", path)
		}
	})
}
