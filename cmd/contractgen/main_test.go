package main

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func TestGeneratedOAuthCallbackAcceptsConfiguredProviderIDs(t *testing.T) {
	t.Chdir(t.TempDir())
	main()
	data, err := os.ReadFile("api/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths map[string]struct {
			Get struct {
				Parameters []struct {
					In, Name string
					Schema   struct {
						Type string
						Enum []string
					}
				}
			}
		}
	}
	if err = json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	for _, p := range spec.Paths["/api/v1/user/oauth/callback/{provider}"].Get.Parameters {
		if p.In != "path" || p.Name != "provider" {
			continue
		}
		if p.Schema.Type != "string" {
			t.Fatal("callback provider must be a string")
		}
		for _, id := range []string{"qq", "oidc", "campus", "sso_2026", "a1234567890123456789012345678901"} {
			if len(p.Schema.Enum) > 0 && !slices.Contains(p.Schema.Enum, id) {
				t.Errorf("generated callback contract rejects valid provider ID %q", id)
			}
		}
		return
	}
	t.Fatal("callback provider path parameter is missing")
}
