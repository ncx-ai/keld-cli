package settings

import "sync"

// Live holds the effective settings — the local base with the org's remote doc
// overlaid (remote-wins per key present). Safe for concurrent Apply/read.
type Live struct {
	mu   sync.RWMutex
	base Settings // local ~/.keld/agent-config.json, loaded once at startup
	eff  Settings // effective = base + remote overlay
}

func NewLive(base Settings) *Live { return &Live{base: base, eff: base} }

// Apply recomputes the effective settings from the local base with the remote
// keys that are present overlaid. A nil remote (or one omitting a key) leaves
// that key at the local base value.
func (l *Live) Apply(r *Remote) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.base
	if r != nil {
		if r.IncludeEntityText != nil {
			e.IncludeEntityText = *r.IncludeEntityText
		}
	}
	l.eff = e
}

func (l *Live) IncludeEntityText() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.eff.IncludeEntityText
}
