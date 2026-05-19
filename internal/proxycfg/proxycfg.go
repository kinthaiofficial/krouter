// Package proxycfg detects the OS system proxy and provides a per-request
// proxy routing function for http.Transport.
//
// Detection priority (first match wins):
//  1. macOS: /usr/sbin/scutil --proxy
//  2. Windows: HKCU registry (Internet Settings)
//  3. Linux/GNOME: gsettings org.gnome.system.proxy
//  4. Fallback: HTTPS_PROXY / HTTP_PROXY environment variable
//
// Domestic Chinese AI provider hosts are always bypassed (no-proxy list)
// so that only international providers (Anthropic, Groq, …) go through the proxy.
package proxycfg

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProxyStatus describes the currently active proxy configuration.
type ProxyStatus struct {
	URL    string `json:"url,omitempty"` // empty when no proxy
	Source string `json:"source"`        // "scutil"|"registry"|"gsettings"|"env"|"none"
	Active bool   `json:"active"`
}

// defaultNoProxySuffixes lists host suffixes that bypass the proxy.
// These are domestic Chinese AI provider API hosts accessible without a proxy.
var defaultNoProxySuffixes = []string{
	"minimaxi.com",           // MiniMax (api.minimaxi.com)
	"minimax.chat",           // MiniMax alternative
	"zhipuai.cn",             // Zhipu AI legacy
	"bigmodel.cn",            // GLM / Zhipu AI (open.bigmodel.cn)
	"dashscope.aliyuncs.com", // Qwen / Alibaba Cloud
	"moonshot.cn",            // Moonshot AI (api.moonshot.cn)
	"deepseek.com",           // DeepSeek (domestically accessible)
}

// Manager detects the OS system proxy and provides a proxy function for
// http.Transport. Call RefreshLoop in a goroutine to keep it current.
type Manager struct {
	mu       sync.RWMutex
	proxyURL *url.URL
	source   string
}

// New creates a Manager and performs the initial proxy detection.
func New() *Manager {
	m := &Manager{source: "none"}
	m.Refresh()
	return m
}

// Refresh re-detects the OS system proxy. Safe to call concurrently.
func (m *Manager) Refresh() {
	rawURL, source := detectProxy()
	var u *url.URL
	if rawURL != "" {
		parsed, err := url.Parse(rawURL)
		if err == nil && parsed.Host != "" {
			u = parsed
		}
	}
	if u == nil {
		source = "none"
	}
	m.mu.Lock()
	m.proxyURL = u
	m.source = source
	m.mu.Unlock()
}

// ProxyFunc returns a function suitable for http.Transport.Proxy.
// Requests to hosts matching defaultNoProxySuffixes bypass the proxy.
func (m *Manager) ProxyFunc() func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		m.mu.RLock()
		p := m.proxyURL
		m.mu.RUnlock()
		if p == nil {
			return nil, nil
		}
		host := req.URL.Hostname()
		for _, suffix := range defaultNoProxySuffixes {
			if host == suffix || strings.HasSuffix(host, "."+suffix) {
				return nil, nil
			}
		}
		return p, nil
	}
}

// Status returns a snapshot of the current proxy configuration.
func (m *Manager) Status() ProxyStatus {
	m.mu.RLock()
	p, src := m.proxyURL, m.source
	m.mu.RUnlock()
	if p == nil {
		return ProxyStatus{Source: src, Active: false}
	}
	return ProxyStatus{URL: p.String(), Source: src, Active: true}
}

// RefreshLoop periodically re-detects the OS proxy. If the proxy URL changes,
// it calls transport.CloseIdleConnections() so new connections use the new proxy.
func (m *Manager) RefreshLoop(ctx context.Context, transport *http.Transport, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.mu.RLock()
			oldURL := m.proxyURL
			m.mu.RUnlock()

			m.Refresh()

			m.mu.RLock()
			newURL := m.proxyURL
			m.mu.RUnlock()

			if !urlsEqual(oldURL, newURL) {
				transport.CloseIdleConnections()
			}
		case <-ctx.Done():
			return
		}
	}
}

func urlsEqual(a, b *url.URL) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.String() == b.String()
}

// detectProxy returns the system proxy URL string and its source label.
func detectProxy() (proxyURL, source string) {
	switch runtime.GOOS {
	case "darwin":
		if u := runScutil(); u != "" {
			return u, "scutil"
		}
	case "windows":
		if u := runWinReg(); u != "" {
			return u, "registry"
		}
	case "linux":
		if u := runGSettings(); u != "" {
			return u, "gsettings"
		}
	}
	for _, env := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if v := os.Getenv(env); v != "" {
			return v, "env"
		}
	}
	return "", "none"
}

// ── macOS ─────────────────────────────────────────────────────────────────

func runScutil() string {
	out, err := exec.Command("/usr/sbin/scutil", "--proxy").Output()
	if err != nil {
		return ""
	}
	return parseScutil(string(out))
}

// parseScutil parses the output of `scutil --proxy` and returns an HTTP proxy URL.
func parseScutil(output string) string {
	vals := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), " : ", 2)
		if len(parts) == 2 {
			vals[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	if vals["HTTPSEnable"] == "1" && vals["HTTPSProxy"] != "" {
		port := "443"
		if p := vals["HTTPSPort"]; p != "" {
			port = p
		}
		return fmt.Sprintf("http://%s:%s", vals["HTTPSProxy"], port)
	}
	if vals["HTTPEnable"] == "1" && vals["HTTPProxy"] != "" {
		port := "80"
		if p := vals["HTTPPort"]; p != "" {
			port = p
		}
		return fmt.Sprintf("http://%s:%s", vals["HTTPProxy"], port)
	}
	return ""
}

// ── Windows ───────────────────────────────────────────────────────────────

func runWinReg() string {
	const key = `HKCU\Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	out, err := exec.Command("reg", "query", key).Output()
	if err != nil {
		return ""
	}
	return parseWinReg(string(out))
}

// parseWinReg parses `reg query` output for proxy settings.
func parseWinReg(output string) string {
	var enable, server string
	for _, line := range strings.Split(output, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		name, value := f[0], f[len(f)-1]
		switch name {
		case "ProxyEnable":
			enable = value
		case "ProxyServer":
			server = value
		}
	}
	if enable != "0x1" || server == "" {
		return ""
	}
	return winProxyServerToURL(server)
}

// winProxyServerToURL normalises a Windows ProxyServer registry value to an http:// URL.
// Input may be "host:port" or "http=host:port;https=host:port".
func winProxyServerToURL(s string) string {
	for _, part := range strings.Split(s, ";") {
		if after, ok := strings.CutPrefix(strings.TrimSpace(part), "https="); ok {
			return ensureScheme(after)
		}
	}
	for _, part := range strings.Split(s, ";") {
		if after, ok := strings.CutPrefix(strings.TrimSpace(part), "http="); ok {
			return ensureScheme(after)
		}
	}
	return ensureScheme(s)
}

func ensureScheme(s string) string {
	if strings.Contains(s, "://") {
		return s
	}
	return "http://" + s
}

// ── Linux (GNOME gsettings) ───────────────────────────────────────────────

func runGSettings() string {
	modeOut, err := exec.Command("gsettings", "get", "org.gnome.system.proxy", "mode").Output()
	if err != nil {
		return ""
	}
	if strings.Trim(strings.TrimSpace(string(modeOut)), "'\"") != "manual" {
		return ""
	}
	hostOut, err := exec.Command("gsettings", "get", "org.gnome.system.proxy.https", "host").Output()
	if err != nil {
		return ""
	}
	portOut, err := exec.Command("gsettings", "get", "org.gnome.system.proxy.https", "port").Output()
	if err != nil {
		return ""
	}
	return parseGSettings(string(modeOut), string(hostOut), string(portOut))
}

// parseGSettings parses gsettings output for the GNOME HTTPS proxy.
func parseGSettings(modeOut, hostOut, portOut string) string {
	mode := strings.Trim(strings.TrimSpace(modeOut), "'\"")
	if mode != "manual" {
		return ""
	}
	host := strings.Trim(strings.TrimSpace(hostOut), "'\"")
	if host == "" {
		return ""
	}
	port, err := strconv.Atoi(strings.TrimSpace(portOut))
	if err != nil || port <= 0 {
		return ""
	}
	return fmt.Sprintf("http://%s:%d", host, port)
}
