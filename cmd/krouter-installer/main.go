// krouter-installer is a standalone binary that serves the browser-based
// install wizard on :8404 and drives the Orchestrator via its HTTP API.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/kinthaiofficial/krouter/internal/install"
	"github.com/kinthaiofficial/krouter/internal/webui/installer"
)

func main() {
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

	// Block until the process is killed (the browser wizard will redirect away after finalize).
	select {}
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
