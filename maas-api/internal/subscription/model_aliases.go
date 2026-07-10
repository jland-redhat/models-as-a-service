package subscription

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// buildModelAliasCandidates maps alias strings to all canonical namespace/name model refs
// that advertise that alias. Multiple models may share the same served model ID.
func buildModelAliasCandidates(modelIndex map[string]*unstructured.Unstructured) map[string][]string {
	if len(modelIndex) == 0 {
		return nil
	}
	index := make(map[string][]string)
	for _, u := range modelIndex {
		if u == nil {
			continue
		}
		canonical := u.GetNamespace() + "/" + u.GetName()
		index[canonical] = appendUniqueAliasCandidate(index[canonical], canonical)

		aliases, found, _ := unstructured.NestedStringSlice(u.Object, "status", "modelAliases")
		if !found || len(aliases) == 0 {
			continue
		}
		for _, alias := range aliases {
			if alias == "" {
				continue
			}
			index[alias] = appendUniqueAliasCandidate(index[alias], canonical)
		}
	}
	return index
}

func appendUniqueAliasCandidate(existing []string, candidate string) []string {
	for _, item := range existing {
		if item == candidate {
			return existing
		}
	}
	return append(existing, candidate)
}

// resolveRequestedModelForSubscription maps a requested model identity to the canonical
// namespace/name used in subscription modelRefs. When multiple MaaSModelRefs share an
// alias, prefer the model included in the subscription.
func resolveRequestedModelForSubscription(requestedModel string, aliasCandidates map[string][]string, sub *subscription) string {
	if requestedModel == "" || sub == nil {
		return requestedModel
	}
	if subscriptionIncludesModel(sub, requestedModel) {
		return requestedModel
	}

	candidates := aliasCandidates[requestedModel]
	for _, ref := range sub.ModelRefs {
		canonical := ref.Namespace + "/" + ref.Name
		for _, candidate := range candidates {
			if candidate == canonical {
				return canonical
			}
		}
	}

	if len(candidates) == 1 {
		return candidates[0]
	}
	return requestedModel
}
