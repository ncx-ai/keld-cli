package eval

import (
	"testing"

	"github.com/ncx-ai/keld-cli/internal/agent/enrich"
)

func TestRunModelOnDeterministicBaseline(t *testing.T) {
	gold, err := LoadGold()
	if err != nil {
		t.Fatal(err)
	}
	pred := RunModel(enrich.NewDeterministic(), gold)
	if len(pred) != len(gold) {
		t.Fatalf("pred len = %d, want %d", len(pred), len(gold))
	}
	m := Score(gold, pred, []string{"task_type", "domain", "sensitivity"})

	// The deterministic backend has strong regex priors for SSN + API keys, so
	// it must catch BOTH sensitive gold rows (missing a secret is the costly error).
	if got := m["sensitivity"]["sensitive_recall"]; got < 1.0 {
		t.Fatalf("deterministic sensitive_recall = %v, want 1.0 (SSN->phi, api key->secrets)", got)
	}
	// task_type accuracy is a baseline signal, not a hard gate here; just require
	// the runner actually produced predictions the scorer can read.
	if _, ok := m["task_type"]["accuracy"]; !ok {
		t.Fatal("task_type accuracy missing")
	}
	t.Logf("deterministic baseline: %+v", m)
}
