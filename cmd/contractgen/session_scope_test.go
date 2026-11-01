package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestGeneratedSessionScopeHeadersMatchReadRoutes(t *testing.T) {
	t.Chdir(t.TempDir())
	main()
	data, err := os.ReadFile("api/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths map[string]map[string]struct {
			Parameters []struct {
				Name, In string
				Required bool
			}
			Responses map[string]struct{ Headers map[string]json.RawMessage }
		}
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"order/list", "order/detail", "admin/order/list", "admin/user/detail", "admin/overview", "admin/task/list"} {
		operation := spec.Paths["/api/v1/"+path]["get"]
		found := false
		for _, p := range operation.Parameters {
			if p.Name == "X-Session-Scope" && p.In == "header" && !p.Required {
				found = true
			}
		}
		if !found || len(operation.Responses["403"].Headers["X-Session-Mismatch"]) == 0 {
			t.Errorf("missing optional read scope contract for %s", path)
		}
	}
	for _, path := range []string{"site/info", "user/profile", "user/login/status"} {
		for _, p := range spec.Paths["/api/v1/"+path]["get"].Parameters {
			if p.Name == "X-Session-Scope" {
				t.Errorf("discovery %s must remain independent of the cached scope", path)
			}
		}
	}
}
