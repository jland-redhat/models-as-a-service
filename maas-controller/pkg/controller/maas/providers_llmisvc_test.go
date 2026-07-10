package maas

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	maasv1alpha1 "github.com/opendatahub-io/models-as-a-service/maas-controller/api/maas/v1alpha1"
)

func TestCollectModelAliasesFromLLMInferenceService(t *testing.T) {
	const (
		ns           = "llm"
		modelRefName = "facebook-opt-125m-simulated"
		llmisvcName  = "facebook-opt-125m-simulated"
	)

	model := &maasv1alpha1.MaaSModelRef{
		ObjectMeta: metav1.ObjectMeta{Name: modelRefName, Namespace: ns},
		Spec: maasv1alpha1.MaaSModelSpec{
			ModelRef: maasv1alpha1.ModelReference{
				Kind: "LLMInferenceService",
				Name: llmisvcName,
			},
		},
	}

	llmisvc := &unstructured.Unstructured{}
	llmisvc.SetGroupVersionKind(llmInferenceServiceGVK)
	llmisvc.SetName(llmisvcName)
	llmisvc.SetNamespace(ns)
	_ = unstructured.SetNestedField(llmisvc.Object, "facebook/opt-125m", "spec", "model", "name")
	_ = unstructured.SetNestedSlice(llmisvc.Object, []interface{}{
		map[string]interface{}{
			"name": "gateway-internal-model-routing",
			"models": []interface{}{
				map[string]interface{}{"name": "publishers/llm/models/facebook/opt-125m"},
			},
		},
		map[string]interface{}{
			"name": "gateway-internal",
			"models": []interface{}{
				map[string]interface{}{"name": "publishers/llm/models/facebook/opt-125m"},
				map[string]interface{}{"name": "facebook/opt-125m"},
			},
		},
	}, "status", "addresses")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(model, llmisvc).Build()
	r := &MaaSModelRefReconciler{Client: c, Scheme: scheme}
	h := &llmisvcHandler{r: r}

	got := h.collectModelAliases(context.Background(), model)
	want := []string{
		"llm/facebook-opt-125m-simulated",
		"publishers/llm/models/facebook/opt-125m",
		"facebook/opt-125m",
	}
	if len(got) != len(want) {
		t.Fatalf("collectModelAliases() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("collectModelAliases()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
