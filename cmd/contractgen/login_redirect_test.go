package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestGeneratedLoginGETDocumentsBrowserRedirectAndQuery(t *testing.T) {
	t.Chdir(t.TempDir())
	main()
	data, err := os.ReadFile("api/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Paths map[string]map[string]struct {
			RequestBody json.RawMessage
			Parameters  []struct {
				In, Name string
				Required bool
			}
			Responses map[string]json.RawMessage
		}
	}
	if err = json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	login := spec.Paths["/api/v1/user/login"]["get"]
	if len(login.Responses["302"]) == 0 || len(login.RequestBody) != 0 {
		t.Fatal("GET must document browser redirect without JSON body")
	}
	if len(login.Responses["303"]) == 0 || len(login.Responses["200"]) != 0 {
		t.Fatal("GET begin failures must document the safe browser error-page redirect, not a JSON business response")
	}
	found := map[string]bool{}
	for _, p := range login.Parameters {
		if p.In != "query" {
			t.Errorf("unexpected navigation header: %s", p.Name)
		}
		found[p.Name] = true
		if p.Name == "provider" && !p.Required {
			t.Error("provider must be required")
		}
	}
	if !found["provider"] || !found["return_to"] {
		t.Fatal("missing login query parameters")
	}
	if len(spec.Paths["/api/v1/user/login"]["post"].RequestBody) == 0 {
		t.Fatal("legacy POST contract lost")
	}
}
