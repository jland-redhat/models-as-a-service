package subscription

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestBuildModelAliasCandidates(t *testing.T) {
	modelIndex := map[string]*unstructured.Unstructured{
		"llm/facebook-opt-125m-simulated": {
			Object: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":      "facebook-opt-125m-simulated",
					"namespace": "llm",
				},
				"status": map[string]interface{}{
					"modelAliases": []interface{}{
						"llm/facebook-opt-125m-simulated",
						"publishers/llm/models/facebook/opt-125m",
						"facebook/opt-125m",
					},
				},
			},
		},
		"llm/premium-simulated-simulated-premium": {
			Object: map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":      "premium-simulated-simulated-premium",
					"namespace": "llm",
				},
				"status": map[string]interface{}{
					"modelAliases": []interface{}{
						"llm/premium-simulated-simulated-premium",
						"publishers/llm/models/facebook/opt-125m",
						"facebook/opt-125m",
					},
				},
			},
		},
	}

	index := buildModelAliasCandidates(modelIndex)
	shared := index["publishers/llm/models/facebook/opt-125m"]
	if len(shared) != 2 {
		t.Fatalf("shared alias candidates = %v, want both models", shared)
	}
}

func TestResolveRequestedModelForSubscription(t *testing.T) {
	index := buildModelAliasCandidates(map[string]*unstructured.Unstructured{
		"llm/facebook-opt-125m-simulated": {
			Object: map[string]interface{}{
				"metadata": map[string]interface{}{"name": "facebook-opt-125m-simulated", "namespace": "llm"},
				"status": map[string]interface{}{
					"modelAliases": []interface{}{
						"llm/facebook-opt-125m-simulated",
						"publishers/llm/models/facebook/opt-125m",
					},
				},
			},
		},
		"llm/premium-simulated-simulated-premium": {
			Object: map[string]interface{}{
				"metadata": map[string]interface{}{"name": "premium-simulated-simulated-premium", "namespace": "llm"},
				"status": map[string]interface{}{
					"modelAliases": []interface{}{
						"llm/premium-simulated-simulated-premium",
						"publishers/llm/models/facebook/opt-125m",
					},
				},
			},
		},
	})

	freeSub := &subscription{
		ModelRefs: []ModelRefInfo{{
			Namespace: "llm",
			Name:      "facebook-opt-125m-simulated",
		}},
	}
	premiumSub := &subscription{
		ModelRefs: []ModelRefInfo{{
			Namespace: "llm",
			Name:      "premium-simulated-simulated-premium",
		}},
	}

	got := resolveRequestedModelForSubscription("publishers/llm/models/facebook/opt-125m", index, freeSub)
	if got != "llm/facebook-opt-125m-simulated" {
		t.Fatalf("free sub resolve = %q, want llm/facebook-opt-125m-simulated", got)
	}

	got = resolveRequestedModelForSubscription("publishers/llm/models/facebook/opt-125m", index, premiumSub)
	if got != "llm/premium-simulated-simulated-premium" {
		t.Fatalf("premium sub resolve = %q, want llm/premium-simulated-simulated-premium", got)
	}
}
