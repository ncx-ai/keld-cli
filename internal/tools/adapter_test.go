// Package tools contains compile-time interface assertions to guarantee that
// all adapter types satisfy the Adapter interface.
package tools

// _assertImplementations is never called; it exists only so the compiler
// verifies that all three adapter types implement Adapter.
func _assertImplementations() {
	var _ Adapter = (*ClaudeAdapter)(nil)
	var _ Adapter = (*CodexAdapter)(nil)
	var _ Adapter = (*GeminiAdapter)(nil)
}
