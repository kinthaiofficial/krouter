package main

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWaitPortFree_ReturnsTrueImmediatelyWhenFree(t *testing.T) {
	start := time.Now()
	free := waitPortFree("127.0.0.1:19999", 2*time.Second, 10*time.Millisecond)
	assert.True(t, free)
	assert.Less(t, time.Since(start), 500*time.Millisecond)
}

func TestWaitPortFree_WaitsUntilPortReleased(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()

	// Release the port after 200 ms.
	go func() {
		time.Sleep(200 * time.Millisecond)
		_ = ln.Close()
	}()

	start := time.Now()
	free := waitPortFree(addr, 2*time.Second, 20*time.Millisecond)
	elapsed := time.Since(start)

	assert.True(t, free, "should return true once port is released")
	assert.GreaterOrEqual(t, elapsed, 150*time.Millisecond, "should wait until port is free")
	assert.Less(t, elapsed, 1500*time.Millisecond, "should not wait longer than necessary")
}

func TestWaitPortFree_ReturnsFalseOnTimeout(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	addr := ln.Addr().String()

	timeout := 150 * time.Millisecond
	start := time.Now()
	free := waitPortFree(addr, timeout, 20*time.Millisecond)
	elapsed := time.Since(start)

	assert.False(t, free, "should return false when port never freed")
	assert.GreaterOrEqual(t, elapsed, timeout)
	assert.Less(t, elapsed, timeout+200*time.Millisecond)
}

func TestWaitPortFree_CorrectAddr(t *testing.T) {
	// Bind a random port, verify waitPortFree returns true for an unbound port
	// and false for the bound one (within zero timeout).
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	bound := ln.Addr().String()
	unbound := fmt.Sprintf("127.0.0.1:%d", freePort(t))

	assert.True(t, waitPortFree(unbound, 100*time.Millisecond, 10*time.Millisecond))
	assert.False(t, waitPortFree(bound, 50*time.Millisecond, 10*time.Millisecond))
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}
