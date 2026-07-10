package maas

import "strings"

func modelRefKey(namespace, name string) string {
	return namespace + "/" + name
}

// orderedUniqueStrings returns primary first, followed by deduplicated extras in stable order.
func orderedUniqueStrings(primary string, extras []string) []string {
	if primary == "" {
		return nil
	}
	seen := map[string]struct{}{primary: {}}
	out := []string{primary}
	for _, extra := range extras {
		if extra == "" {
			continue
		}
		if _, ok := seen[extra]; ok {
			continue
		}
		seen[extra] = struct{}{}
		out = append(out, extra)
	}
	return out
}

// expandAllowlistAliases registers the same allowlist entry under each model alias key.
// When multiple models share an alias, allowlists are merged so auth succeeds if any model grants access.
func expandAllowlistAliases(aggregate map[string]modelSubjectAllowlist, aliasesByModelKey map[string][]string) {
	for modelKey, entry := range aggregate {
		for _, alias := range aliasesByModelKey[modelKey] {
			if alias == "" || alias == modelKey {
				continue
			}
			if existing, ok := aggregate[alias]; ok {
				aggregate[alias] = mergeModelSubjectAllowlists(existing, entry)
				continue
			}
			aggregate[alias] = entry
		}
	}
}

func mergeModelSubjectAllowlists(a, b modelSubjectAllowlist) modelSubjectAllowlist {
	return modelSubjectAllowlist{
		Groups: deduplicateAndSort(append(append([]string{}, a.Groups...), b.Groups...)),
		Users:  deduplicateAndSort(append(append([]string{}, a.Users...), b.Users...)),
	}
}

// parseModelRefKey splits a namespace/name model reference key.
func parseModelRefKey(key string) (namespace, name string, ok bool) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
