// Package tools provides tests for the GeminiAdapter.
package tools

import (
	"strings"
	"testing"
)

func TestGeminiApplySetsTelemetry(t *testing.T) {
	a := &GeminiAdapter{}
	p := SetupParams{Endpoint: "https://e", IngestToken: "tok", Actor: "me"}
	cur := "{\n  \"theme\": \"dark\"\n}\n"
	plan := a.Apply(&cur, p, false)
	if !plan.Changed || !strings.Contains(plan.AfterText, "otlpEndpoint") || !strings.Contains(plan.AfterText, "\"theme\"") {
		t.Fatalf("telemetry not merged into existing config:\n%s", plan.AfterText)
	}
	st := a.Status(&plan.AfterText, nil)
	if !st.Configured {
		t.Fatal("expected configured")
	}
}

func TestGeminiApplyRemoveRoundTrip(t *testing.T) {
	a := &GeminiAdapter{}
	p := SetupParams{Endpoint: "https://otel.example.com", IngestToken: "secret", Actor: "alice"}

	// Start with a config that has an extra key.
	original := "{\n  \"theme\": \"dark\"\n}\n"

	// Apply: should set telemetry.
	applyPlan := a.Apply(&original, p, false)
	if !applyPlan.Changed {
		t.Fatal("Apply: expected Changed=true")
	}
	if !strings.Contains(applyPlan.AfterText, "otlpEndpoint") {
		t.Fatalf("Apply: AfterText missing otlpEndpoint:\n%s", applyPlan.AfterText)
	}
	if !strings.Contains(applyPlan.AfterText, "\"theme\"") {
		t.Fatalf("Apply: AfterText missing original 'theme' key:\n%s", applyPlan.AfterText)
	}

	// Status after apply: Configured must be true.
	stAfterApply := a.Status(&applyPlan.AfterText, applyPlan.Managed)
	if !stAfterApply.Configured {
		t.Fatalf("Status after Apply: expected Configured=true, detail=%s", stAfterApply.Detail)
	}

	// Remove: strip telemetry back out.
	removePlan := a.Remove(&applyPlan.AfterText, applyPlan.Managed)
	if !removePlan.Changed {
		t.Fatal("Remove: expected Changed=true")
	}

	// Status after remove: Configured must be false.
	stAfterRemove := a.Status(&removePlan.AfterText, nil)
	if stAfterRemove.Configured {
		t.Fatalf("Status after Remove: expected Configured=false, detail=%s", stAfterRemove.Detail)
	}

	// The surviving config should still contain the original "theme" key.
	if !strings.Contains(removePlan.AfterText, "\"theme\"") {
		t.Fatalf("Remove: AfterText lost original 'theme' key:\n%s", removePlan.AfterText)
	}
}

func TestGeminiApplyNilCurrentText(t *testing.T) {
	a := &GeminiAdapter{}
	p := SetupParams{Endpoint: "https://otel.example.com", IngestToken: "tok2", Actor: "bob"}

	plan := a.Apply(nil, p, false)
	if !plan.Changed {
		t.Fatal("Apply on nil currentText: expected Changed=true")
	}
	if !strings.Contains(plan.AfterText, "otlpEndpoint") {
		t.Fatalf("Apply on nil currentText: AfterText missing otlpEndpoint:\n%s", plan.AfterText)
	}

	// managed["created"] should be true when currentText was nil.
	created, ok := plan.Managed["created"]
	if !ok || created != true {
		t.Fatalf("Apply on nil currentText: expected managed[\"created\"]=true, got %v", plan.Managed)
	}
}

func TestGeminiMeta(t *testing.T) {
	a := &GeminiAdapter{}
	if a.Name() != "gemini" {
		t.Fatalf("Name()=%q, want %q", a.Name(), "gemini")
	}
	if a.DisplayName() != "Gemini CLI" {
		t.Fatalf("DisplayName()=%q, want %q", a.DisplayName(), "Gemini CLI")
	}
	cp := a.ConfigPath()
	if !strings.HasSuffix(cp, "/.gemini/settings.json") {
		t.Fatalf("ConfigPath()=%q should end with /.gemini/settings.json", cp)
	}
}
