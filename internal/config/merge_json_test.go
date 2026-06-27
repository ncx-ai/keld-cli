// internal/config/merge_json_test.go
package config

import (
	"testing"

	"github.com/iancoleman/orderedmap"
)

func TestDumpJSONFormatAndOrder(t *testing.T) {
	o := orderedmap.New()
	o.Set("b", "1")
	o.Set("a", "2")
	got := DumpJSON(o)
	want := "{\n  \"b\": \"1\",\n  \"a\": \"2\"\n}\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLoadJSONInvalid(t *testing.T) {
	if _, err := LoadJSON("{not json"); err == nil {
		t.Fatal("expected error")
	}
	o, err := LoadJSON("   ")
	if err != nil || len(o.Keys()) != 0 {
		t.Fatalf("blank should be empty map, got %v %v", o, err)
	}
}

func TestMergeEnvPreservesExistingOrder(t *testing.T) {
	o, _ := LoadJSON(`{"env":{"EXISTING":"x"}}`)
	env := orderedmap.New()
	env.Set("NEW", "y")
	keys := MergeEnv(o, env)
	if len(keys) != 1 || keys[0] != "NEW" {
		t.Fatalf("keys %v", keys)
	}
	if DumpJSON(o) != "{\n  \"env\": {\n    \"EXISTING\": \"x\",\n    \"NEW\": \"y\"\n  }\n}\n" {
		t.Fatalf("merge output:\n%s", DumpJSON(o))
	}
}

func TestRemoveSectionKeysDeletesEmptySection(t *testing.T) {
	o, _ := LoadJSON(`{"env":{"A":"1"}}`)
	RemoveSectionKeys(o, "env", []string{"A"})
	if DumpJSON(o) != "{}\n" {
		t.Fatalf("expected empty obj, got %s", DumpJSON(o))
	}
}

func TestClaudeHookAddIdempotentAndRemove(t *testing.T) {
	o := orderedmap.New()
	m := "startup"
	AddClaudeHook(o, "SessionStart", &m, "keld __hook --source claude_code")
	AddClaudeHook(o, "SessionStart", &m, "keld __hook --source claude_code") // dup → no-op
	AddClaudeHook(o, "CwdChanged", nil, "keld __hook --source claude_code")
	if !HasHookWithCommand(o, "keld __hook") {
		t.Fatal("expected hook present")
	}
	RemoveHooksByCommand(o, "keld __hook")
	if HasHookWithCommand(o, "keld __hook") {
		t.Fatal("expected hooks removed")
	}
	if len(o.Keys()) != 0 {
		t.Fatalf("hooks key should be pruned, keys=%v", o.Keys())
	}
}
