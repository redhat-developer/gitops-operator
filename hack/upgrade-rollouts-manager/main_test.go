package main

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestStripDescriptionFields(t *testing.T) {

	input := `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: rollouts.argoproj.io
spec:
  description: top level description
  versions:
  - name: v1alpha1
    schema:
      openAPIV3Schema:
        description: Rollout is a specification for a Rollout resource
        properties:
          spec:
            description: RolloutSpec is the spec for a Rollout resource
            properties:
              paused:
                description: Paused pauses the rollout at its current step.
                type: boolean
            type: object
        type: object
`

	out, err := stripDescriptionFields([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(string(out), "description") {
		t.Fatalf("expected all 'description' fields to be removed, got:\n%s", out)
	}

	// Verify the rest of the structure survived the round trip
	var result map[string]interface{}
	if err := yaml.Unmarshal(out, &result); err != nil {
		t.Fatalf("output is not valid yaml: %v", err)
	}

	metadata, ok := result["metadata"].(map[string]interface{})
	if !ok || metadata["name"] != "rollouts.argoproj.io" {
		t.Fatalf("expected metadata.name to be preserved, got: %v", result["metadata"])
	}

	spec, ok := result["spec"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected spec to be preserved, got: %v", result["spec"])
	}
	if _, exists := spec["description"]; exists {
		t.Fatalf("expected spec.description to be removed")
	}

	versions, ok := spec["versions"].([]interface{})
	if !ok || len(versions) != 1 {
		t.Fatalf("expected spec.versions to be preserved, got: %v", spec["versions"])
	}
}
