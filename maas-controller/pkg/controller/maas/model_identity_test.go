package maas

import "testing"

func TestOrderedUniqueStrings(t *testing.T) {
	got := orderedUniqueStrings("llm/model-a", []string{
		"publishers/llm/models/foo/bar",
		"foo/bar",
		"llm/model-a",
		"publishers/llm/models/foo/bar",
	})
	want := []string{
		"llm/model-a",
		"publishers/llm/models/foo/bar",
		"foo/bar",
	}
	if len(got) != len(want) {
		t.Fatalf("orderedUniqueStrings() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("orderedUniqueStrings()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestExpandAllowlistAliases(t *testing.T) {
	entry := modelSubjectAllowlist{Groups: []string{"team-a"}}
	premium := modelSubjectAllowlist{Groups: []string{"premium-user"}}
	aggregate := map[string]modelSubjectAllowlist{
		"llm/model-a": entry,
		"llm/model-b": premium,
	}
	expandAllowlistAliases(aggregate, map[string][]string{
		"llm/model-a": {
			"llm/model-a",
			"publishers/llm/models/foo/bar",
			"foo/bar",
		},
		"llm/model-b": {
			"llm/model-b",
			"publishers/llm/models/foo/bar",
		},
	})

	shared := aggregate["publishers/llm/models/foo/bar"]
	if len(shared.Groups) != 2 {
		t.Fatalf("shared alias allowlist groups = %v, want team-a and premium-user", shared.Groups)
	}
}

func TestParseModelRefKey(t *testing.T) {
	ns, name, ok := parseModelRefKey("llm/facebook-opt-125m-simulated")
	if !ok || ns != "llm" || name != "facebook-opt-125m-simulated" {
		t.Fatalf("parseModelRefKey() = (%q, %q, %v)", ns, name, ok)
	}
	if _, _, ok := parseModelRefKey("invalid"); ok {
		t.Fatal("expected invalid model ref key")
	}
}
