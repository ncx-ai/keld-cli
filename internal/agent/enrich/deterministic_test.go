package enrich

import "testing"

func findEntity(es []Entity, label string) (Entity, bool) {
	for _, e := range es {
		if e.Label == label {
			return e, true
		}
	}
	return Entity{}, false
}

func TestDeterministicDetectsEmailAndKey(t *testing.T) {
	m := NewDeterministic()
	text := "email me at jane@acme.com with key sk-live-ABCDEF0123456789"
	es := m.Entities(text, SensitiveEntityLabels)
	em, ok := findEntity(es, "email")
	if !ok || text[em.Start:em.End] != "jane@acme.com" {
		t.Fatalf("email span wrong: %+v", em)
	}
	if _, ok := findEntity(es, "api_key"); !ok {
		t.Fatalf("expected api_key entity in %+v", es)
	}
}

func TestDeterministicClassifyCodegen(t *testing.T) {
	m := NewDeterministic()
	res := m.Classify("Write a Go function to parse JSON", map[string][]string{"task_type": TaskTypes})
	ranked := res["task_type"]
	if len(ranked) == 0 || ranked[0].Label != "codegen" {
		t.Fatalf("top task_type = %+v, want codegen", ranked)
	}
}

func TestDeterministicClassifyFallsBackToOther(t *testing.T) {
	m := NewDeterministic()
	res := m.Classify("zzzzz", map[string][]string{"task_type": TaskTypes})
	if res["task_type"][0].Label != "other" {
		t.Fatalf("unmatched should be 'other', got %+v", res["task_type"])
	}
}
