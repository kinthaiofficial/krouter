// krouter-installer is a standalone binary that serves the browser-based
// install wizard on :8404 and drives the Orchestrator via its HTTP API.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/kinthaiofficial/krouter/internal/install"
	"github.com/kinthaiofficial/krouter/internal/webui/installer"
)

func main() {
	// Stop any running daemon before launching the wizard.
	// This is the upgrade path: the old daemon is registered with KeepAlive=true,
	// so killing it manually just causes launchd to restart it. bootout removes it
	// from launchd supervision entirely, so the new binary can start cleanly.
	stopRunningDaemon()

	// Kill any previous installer process still occupying :8404. Without this,
	// a stale installer causes install.Listen to silently pick :8405, breaking
	// scripts and users that assume a fixed installer port.
	stopRunningInstaller(8404)

	token, err := randomToken()
	if err != nil {
		fmt.Fprintln(os.Stderr, "krouter-installer: generate token:", err)
		os.Exit(1)
	}

	orch := install.New(install.NullUI{}, install.Options{SrcBinary: daemonBinary()})
	srv := install.NewServer(token, orch)

	// Attach the embedded frontend.
	sub, err := fs.Sub(installer.Assets, "dist")
	if err != nil {
		fmt.Fprintln(os.Stderr, "krouter-installer: embed assets:", err)
		os.Exit(1)
	}
	srv.SetUIAssets(sub)

	ln, addr, err := install.Listen(8404, srv.Handler())
	if err != nil {
		fmt.Fprintln(os.Stderr, "krouter-installer:", err)
		os.Exit(1)
	}
	defer func() { _ = ln.Close() }()

	url := fmt.Sprintf("http://%s/?token=%s", addr, token)
	fmt.Println("krouter installer running at", url)

	openBrowser(url)

	// Block until the install wizard completes and the browser has been redirected
	// to the dashboard. ShutdownCh is closed by handleDaemonReady once ready:true
	// has been sent, so we exit cleanly instead of waiting to be killed.
	<-srv.ShutdownCh()
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// daemonBinary returns the path to the krouter daemon binary if it lives in the
// same directory as this installer (e.g. inside a macOS .app bundle where both
// Contents/MacOS/krouter and Contents/MacOS/krouter-installer are present).
// Returns "" if not found; CopyBinary will then fall back to os.Executable().
func daemonBinary() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)
	candidates := []string{"krouter"}
	if runtime.GOOS == "windows" {
		candidates = []string{"krouter.exe", "krouter"}
	}
	for _, name := range candidates {
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}
	return ""
}

// stopRunningInstaller kills any process listening on installerPort (default 8404).
// If nothing is listening the function is a no-op.
// Uses OS-specific commands (lsof/fuser/netstat) because net.Listener doesn't
// expose a cross-platform "who owns this port?" API.
func stopRunningInstaller(installerPort int) {
	addr := fmt.Sprintf("127.0.0.1:%d", installerPort)
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err != nil {
		return // nothing listening — nothing to kill
	}
	_ = conn.Close()

	// Something is on the port. Try to kill it.
	portStr := fmt.Sprintf("%d", installerPort)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// lsof prints the PID of every process bound to the port; xargs kill -9.
		cmd = exec.Command("sh", "-c",
			"lsof -ti tcp:"+portStr+" | xargs kill -9 2>/dev/null")
	case "linux":
		cmd = exec.Command("fuser", "-k", portStr+"/tcp")
	case "windows":
		// Find PID(s) using the port, then kill them.
		cmd = exec.Command("cmd", "/C",
			`for /f "tokens=5" %a in ('netstat -aon ^| findstr :` + portStr + `') do taskkill /PID %a /F 2>nul`)
	default:
		return
	}
	_ = cmd.Run()
	time.Sleep(200 * time.Millisecond) // brief grace period for port release
}

// stopRunningDaemon stops the currently running krouter daemon (if any).
// Best-effort: errors are silently ignored (daemon may not be installed yet).
func stopRunningDaemon() {
	switch runtime.GOOS {
	case "darwin":
		_ = config.StopLaunchAgent()
	case "linux":
		_ = config.StopSystemdService()
	case "windows":
		_ = config.StopTask()
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
