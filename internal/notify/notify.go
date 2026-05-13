// Package notify sends desktop notifications for daemon events.
// Uses gen2brain/beeep (CGO-free: osascript on macOS, notify-send on Linux, PowerShell on Windows).
package notify

import (
	"sync"
	"time"

	"github.com/gen2brain/beeep"
)

const dedupeWindow = 5 * time.Minute

// Notifier sends desktop notifications and deduplicates within a configurable window.
type Notifier struct {
	window time.Duration
	mu     sync.Mutex
	last   map[string]time.Time // event type → last sent time
}

// New creates a Notifier with the default 5-minute dedupe window.
func New() *Notifier {
	return NewWithWindow(dedupeWindow)
}

// NewWithWindow creates a Notifier with a custom dedupe window (for testing).
func NewWithWindow(window time.Duration) *Notifier {
	return &Notifier{window: window, last: make(map[string]time.Time)}
}

// Notify sends a desktop notification for the given event type.
// Calls within dedupeWindow for the same type are silently dropped.
func (n *Notifier) Notify(eventType, title, body string) {
	n.mu.Lock()
	if t, ok := n.last[eventType]; ok && time.Since(t) < n.window {
		n.mu.Unlock()
		return
	}
	n.last[eventType] = time.Now()
	n.mu.Unlock()

	// beeep.Notify is fire-and-forget; errors are informational only.
	_ = beeep.Notify(title, body, "")
}

// HandleEvent maps SSE event types to desktop notification calls.
// Intended to be called from the API server's broadcast path.
func (n *Notifier) HandleEvent(eventType string, data any) {
	switch eventType {
	case "quota_warning":
		n.Notify(eventType, "krouter — Quota Warning", "You are approaching your usage quota.")
	case "announcement_new":
		n.Notify(eventType, "krouter — New Announcement", "There is a new announcement from Kinthai.")
	case "upgrade_available":
		n.Notify(eventType, "krouter — Update Available", "A new version of krouter is available.")
	}
}
