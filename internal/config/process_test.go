package config_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/stretchr/testify/assert"
)

// WaitForProcessExit is cross-platform (no build tag), so all tests run on every OS.

func TestWaitForProcessExit_ReturnsImmediatelyWhenProcessGone(t *testing.T) {
	start := time.Now()
	config.WaitForProcessExit("noproc", 2*time.Second, 10*time.Millisecond,
		func(string) bool { return false })
	assert.Less(t, time.Since(start), 100*time.Millisecond,
		"should return immediately when process is already gone")
}

func TestWaitForProcessExit_WaitsUntilProcessExits(t *testing.T) {
	var calls atomic.Int32
	config.WaitForProcessExit("krouter", 2*time.Second, 5*time.Millisecond,
		func(string) bool {
			n := calls.Add(1)
			return n < 4 // reports running for first 3 polls, gone on 4th
		})
	assert.GreaterOrEqual(t, calls.Load(), int32(4),
		"should poll until processExistsFn returns false")
}

func TestWaitForProcessExit_TimesOutWhenProcessNeverDies(t *testing.T) {
	timeout := 150 * time.Millisecond
	start := time.Now()
	config.WaitForProcessExit("immortal", timeout, 10*time.Millisecond,
		func(string) bool { return true })
	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, timeout,
		"should wait at least the full timeout")
	assert.Less(t, elapsed, timeout+200*time.Millisecond,
		"should not wait significantly longer than the timeout")
}

func TestWaitForProcessExit_PassesCorrectNameToChecker(t *testing.T) {
	var gotName string
	config.WaitForProcessExit("myprocess", 50*time.Millisecond, 5*time.Millisecond,
		func(name string) bool {
			gotName = name
			return false
		})
	assert.Equal(t, "myprocess", gotName)
}

func TestWaitForProcessExit_ZeroTimeoutReturnsImmediately(t *testing.T) {
	calls := 0
	start := time.Now()
	config.WaitForProcessExit("krouter", 0, 10*time.Millisecond,
		func(string) bool { calls++; return true })
	assert.Less(t, time.Since(start), 50*time.Millisecond)
	// With zero timeout the loop condition is false from the start.
	assert.Equal(t, 0, calls, "should not poll at all with zero timeout")
}

func TestWaitForProcessExit_ExitsAfterFirstFalse(t *testing.T) {
	// Even if timeout is very large, returns as soon as checker says false.
	var calls atomic.Int32
	config.WaitForProcessExit("krouter", 60*time.Second, 1*time.Millisecond,
		func(string) bool {
			return calls.Add(1) < 2 // false on second call
		})
	assert.Equal(t, int32(2), calls.Load(),
		"should stop polling after first false response")
}
