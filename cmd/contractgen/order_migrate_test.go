package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestGeneratedMigrationContractRequiresAdminSessionCSRFAndTargetUser(t *testing.T) {
	t.Chdir(t.TempDir())
	main()
	raw, err := os.ReadFile("api/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths map[string]map[string]struct {
			Description string
			Security    []map[string]any
			Parameters  []struct {
				In, Name string
				Required bool
			}
			RequestBody struct {
				Required bool
				Content  map[string]struct {
					Schema struct {
						Ref string `json:"$ref"`
					}
				}
			}
			Responses map[string]json.RawMessage
		}
		Components struct {
			Schemas map[string]struct {
				Required   []string
				Properties map[string]struct{ Type, Format string }
			}
		}
	}
	if err = json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	op, exists := spec.Paths["/api/v1/admin/order/migrate"]["post"]
	if !exists {
		t.Fatal("admin migration endpoint is missing")
	}
	if len(op.Security) == 0 || len(op.Responses["401"]) == 0 || len(op.Responses["403"]) == 0 {
		t.Fatal("migration must document authenticated authorization failures")
	}
	csrf := false
	for _, p := range op.Parameters {
		csrf = csrf || p.In == "header" && p.Name == "X-CSRF-Token" && p.Required
	}
	if !csrf || !op.RequestBody.Required || op.RequestBody.Content["application/json"].Schema.Ref != "#/components/schemas/MigrateOrderInput" {
		t.Fatal("missing CSRF or required migration input")
	}
	input := spec.Components.Schemas["MigrateOrderInput"]
	for _, field := range []string{"id", "user_id"} {
		if !slices.Contains(input.Required, field) || input.Properties[field].Format != "uuid" {
			t.Errorf("%s must be a required UUID", field)
		}
	}
	if !strings.Contains(op.Description, "ORDER_MIGRATION_NOT_ALLOWED") || !strings.Contains(string(op.Responses["200"]), "#/components/schemas/MigrationResult") {
		t.Fatal("migration outcomes and response schema must be documented")
	}
}
