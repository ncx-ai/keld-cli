package enrich

import "testing"

func TestRunProducesEnrichedProfile(t *testing.T) {
	p := Run("write a go function; email jane@acme.com", "claude_code", NewDeterministic())
	if p.PipelineStatus != "enriched" {
		t.Fatalf("status = %q, want enriched", p.PipelineStatus)
	}
	if p.TaskType.Value != "codegen" {
		t.Fatalf("task_type = %+v", p.TaskType)
	}
	if p.Sensitivity.Value != "pii" {
		t.Fatalf("sensitivity = %+v, want pii (email)", p.Sensitivity)
	}
	if p.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version not set")
	}
	if len(p.ExtractorVersions) != 3 {
		t.Fatalf("want 3 extractor versions, got %d", len(p.ExtractorVersions))
	}
	if p.EnrichedAt.IsZero() {
		t.Fatal("EnrichedAt must be set")
	}
}

type panicModel struct{ Model }

func (panicModel) Extract(string, map[string]string, map[string][]string) ExtractResult {
	panic("boom")
}

func TestRunIsolatesPanicAsPartial(t *testing.T) {
	// task_type uses Classify (works via embedded Model); sensitivity+domain use
	// Extract (panics). Pipeline must survive and mark partial.
	m := panicModel{Model: NewDeterministic()}
	p := Run("write a function", "claude_code", m)
	if p.PipelineStatus != "partial" {
		t.Fatalf("status = %q, want partial", p.PipelineStatus)
	}
	if p.TaskType.Value != "codegen" {
		t.Fatalf("surviving stage should still populate: %+v", p.TaskType)
	}
}
