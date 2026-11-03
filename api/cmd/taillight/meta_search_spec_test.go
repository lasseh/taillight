package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/lasseh/taillight/docs"
)

// TestMetaSearchParameterDocumented checks every endpoint the meta search
// golden lists documents its search parameter in openapi.yml, under the
// golden's name and max length, so the spec and the golden cannot drift.
func TestMetaSearchParameterDocumented(t *testing.T) {
	raw, err := os.ReadFile("../../internal/handler/testdata/golden/meta_search_params.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var golden struct {
		Param     string   `json:"param"`
		MaxLength int      `json:"max_length"`
		Endpoints []string `json:"endpoints"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("parse golden: %v", err)
	}

	type param struct {
		Ref    string `yaml:"$ref"`
		Name   string `yaml:"name"`
		In     string `yaml:"in"`
		Schema struct {
			MaxLength int `yaml:"maxLength"`
		} `yaml:"schema"`
	}
	var spec struct {
		Paths      map[string]map[string]yaml.Node `yaml:"paths"`
		Components struct {
			Parameters map[string]param `yaml:"parameters"`
		} `yaml:"components"`
	}
	if err := yaml.Unmarshal(docs.Spec(), &spec); err != nil {
		t.Fatalf("parse embedded openapi.yml: %v", err)
	}

	for _, endpoint := range golden.Endpoints {
		node, ok := spec.Paths[specPathFor(endpoint)]["get"]
		if !ok {
			t.Errorf("GET %s is not in openapi.yml", endpoint)
			continue
		}
		var op struct {
			Parameters []param `yaml:"parameters"`
		}
		if err := node.Decode(&op); err != nil {
			t.Fatalf("decode GET %s: %v", endpoint, err)
		}
		var found *param
		for _, p := range op.Parameters {
			if name, ok := strings.CutPrefix(p.Ref, "#/components/parameters/"); ok {
				p = spec.Components.Parameters[name]
			}
			if p.Name == golden.Param && p.In == "query" {
				found = &p
				break
			}
		}
		switch {
		case found == nil:
			t.Errorf("GET %s documents no %q query parameter", endpoint, golden.Param)
		case found.Schema.MaxLength != golden.MaxLength:
			t.Errorf("GET %s documents %q with maxLength %d, want %d", endpoint, golden.Param, found.Schema.MaxLength, golden.MaxLength)
		}
	}
}
