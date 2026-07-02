package enrich

import "strings"

// Meta is the non-prompt context a classification pass may reason over. It is
// deliberately small: repo (cwd) and tool (source) are what the device knows.
// Team/category are resolved server-side in Atlas, never here.
type Meta struct {
	Repo string
	Tool string
}

// Preamble renders a compact context line prepended to the text handed to
// CLASSIFICATION passes (never to entity/sensitivity passes, which need raw
// offsets). Empty repo renders "none" so the model sees a stable shape.
func (m Meta) Preamble() string {
	parts := []string{"repository: none"}
	if m.Repo != "" {
		parts[0] = "repository: " + m.Repo
	}
	if m.Tool != "" {
		parts = append(parts, "tool: "+m.Tool)
	}
	return "[Context — " + strings.Join(parts, "; ") + "]\nTask: "
}
