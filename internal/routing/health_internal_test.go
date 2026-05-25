package routing

import (
	"testing"
	"time"
)

// fakeHealth is a controllable HealthChecker for exercising isHealthy directly.
type fakeHealth struct {
	failures map[string]int
	lastFail map[string]time.Time
}

func (f fakeHealth) ConsecutiveFailures(p string) int { return f.failures[p] }
func (f fakeHealth) LastFailureAt(p string) time.Time { return f.lastFail[p] }

// Issue #48: a provider that tripped the failure threshold must recover after
// healthRecoveryTTL instead of being excluded forever.
func TestIsHealthy_RecoveryTTL(t *testing.T) {
	e := &Engine{}
	if !e.isHealthy("anything") {
		t.Fatal("nil health checker should treat every provider as healthy")
	}

	now := time.Now()
	e.health = fakeHealth{
		failures: map[string]int{"ok": 0, "down": 3, "recovered": 3},
		lastFail: map[string]time.Time{
			"down":      now,                             // just failed
			"recovered": now.Add(-2 * healthRecoveryTTL), // failed long ago
		},
	}

	if !e.isHealthy("ok") {
		t.Error("0 consecutive failures → healthy")
	}
	if e.isHealthy("down") {
		t.Error("3 failures with a recent last-failure → unhealthy (excluded)")
	}
	if !e.isHealthy("recovered") {
		t.Error("3 failures but last failure older than the TTL → half-open probe (healthy)")
	}
}
