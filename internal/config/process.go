package config

import "time"

// WaitForProcessExit polls until processExistsFn returns false (process gone)
// or timeout elapses. interval controls how long to sleep between polls.
// Exported so tests can exercise the logic without build-tag restrictions.
func WaitForProcessExit(name string, timeout, interval time.Duration, processExistsFn func(string) bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processExistsFn(name) {
			return
		}
		time.Sleep(interval)
	}
}
