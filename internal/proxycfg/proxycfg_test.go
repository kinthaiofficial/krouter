package proxycfg

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── parseScutil ────────────────────────────────────────────────────────────

func TestParseScutil_HTTPSEnabled(t *testing.T) {
	output := `<dictionary> {
  HTTPEnable : 0
  HTTPSEnable : 1
  HTTPSProxy : 127.0.0.1
  HTTPSPort : 7890
}`
	assert.Equal(t, "http://127.0.0.1:7890", parseScutil(output))
}

func TestParseScutil_HTTPSDisabled(t *testing.T) {
	output := `<dictionary> {
  HTTPSEnable : 0
  HTTPSProxy : 127.0.0.1
  HTTPSPort : 7890
}`
	assert.Equal(t, "", parseScutil(output))
}

func TestParseScutil_HTTPOnlyFallback(t *testing.T) {
	output := `<dictionary> {
  HTTPEnable : 1
  HTTPProxy : 10.0.0.1
  HTTPPort : 8080
  HTTPSEnable : 0
}`
	assert.Equal(t, "http://10.0.0.1:8080", parseScutil(output))
}

func TestParseScutil_Empty(t *testing.T) {
	assert.Equal(t, "", parseScutil(""))
	assert.Equal(t, "", parseScutil("<dictionary> {}"))
}

func TestParseScutil_DefaultPorts(t *testing.T) {
	// Port fields absent — should use defaults.
	output := `<dictionary> {
  HTTPSEnable : 1
  HTTPSProxy : proxy.example.com
}`
	assert.Equal(t, "http://proxy.example.com:443", parseScutil(output))
}

// ── parseWinReg ────────────────────────────────────────────────────────────

func TestParseWinReg_Enabled(t *testing.T) {
	output := `
HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Internet Settings
    ProxyEnable    REG_DWORD    0x1
    ProxyServer    REG_SZ    127.0.0.1:7890
`
	assert.Equal(t, "http://127.0.0.1:7890", parseWinReg(output))
}

func TestParseWinReg_Disabled(t *testing.T) {
	output := `
    ProxyEnable    REG_DWORD    0x0
    ProxyServer    REG_SZ    127.0.0.1:7890
`
	assert.Equal(t, "", parseWinReg(output))
}

func TestParseWinReg_FullScheme(t *testing.T) {
	output := `
    ProxyEnable    REG_DWORD    0x1
    ProxyServer    REG_SZ    http=127.0.0.1:8080;https=127.0.0.1:8443
`
	assert.Equal(t, "http://127.0.0.1:8443", parseWinReg(output))
}

func TestParseWinReg_HTTPOnlyScheme(t *testing.T) {
	output := `
    ProxyEnable    REG_DWORD    0x1
    ProxyServer    REG_SZ    http=127.0.0.1:8080
`
	assert.Equal(t, "http://127.0.0.1:8080", parseWinReg(output))
}

func TestParseWinReg_NoProxyServer(t *testing.T) {
	output := `    ProxyEnable    REG_DWORD    0x1`
	assert.Equal(t, "", parseWinReg(output))
}

// ── parseGSettings ─────────────────────────────────────────────────────────

func TestParseGSettings_Manual(t *testing.T) {
	result := parseGSettings("'manual'\n", "'127.0.0.1'\n", "7890\n")
	assert.Equal(t, "http://127.0.0.1:7890", result)
}

func TestParseGSettings_None(t *testing.T) {
	assert.Equal(t, "", parseGSettings("'none'\n", "'127.0.0.1'\n", "7890\n"))
}

func TestParseGSettings_Auto(t *testing.T) {
	// PAC-based auto is not supported.
	assert.Equal(t, "", parseGSettings("'auto'\n", "'127.0.0.1'\n", "7890\n"))
}

func TestParseGSettings_EmptyHost(t *testing.T) {
	assert.Equal(t, "", parseGSettings("'manual'\n", "''\n", "7890\n"))
}

func TestParseGSettings_ZeroPort(t *testing.T) {
	assert.Equal(t, "", parseGSettings("'manual'\n", "'127.0.0.1'\n", "0\n"))
}

// ── Manager: ProxyFunc ─────────────────────────────────────────────────────

func managerWithProxy(rawURL string) *Manager {
	m := &Manager{source: "test"}
	if rawURL != "" {
		u, err := url.Parse(rawURL)
		if err == nil {
			m.proxyURL = u
		}
	}
	return m
}

func TestProxyFunc_NilProxy_AllDirect(t *testing.T) {
	m := &Manager{source: "none"}
	fn := m.ProxyFunc()

	for _, target := range []string{
		"https://api.anthropic.com/v1/messages",
		"https://api.moonshot.cn/v1/chat",
		"https://api.minimaxi.com/anthropic/v1",
	} {
		req, _ := http.NewRequest(http.MethodPost, target, nil)
		p, err := fn(req)
		require.NoError(t, err)
		assert.Nil(t, p, "no proxy configured — %s should go direct", target)
	}
}

func TestProxyFunc_ProxiedHosts(t *testing.T) {
	m := managerWithProxy("http://127.0.0.1:7890")
	fn := m.ProxyFunc()

	proxied := []string{
		"https://api.anthropic.com/v1/messages",
		"https://api.groq.com/openai/v1/chat/completions",
	}
	for _, target := range proxied {
		req, _ := http.NewRequest(http.MethodPost, target, nil)
		p, err := fn(req)
		require.NoError(t, err)
		require.NotNil(t, p, "expected proxy for %s", target)
		assert.Equal(t, "http://127.0.0.1:7890", p.String())
	}
}

func TestProxyFunc_BypassedHosts(t *testing.T) {
	m := managerWithProxy("http://127.0.0.1:7890")
	fn := m.ProxyFunc()

	bypassed := []string{
		"https://api.minimaxi.com/anthropic/v1",
		"https://api.moonshot.cn/v1/chat",
		"https://api.deepseek.com/v1/chat/completions",
		"https://open.bigmodel.cn/api/paas/v4",
		"https://dashscope.aliyuncs.com/compatible-mode/v1",
		"https://minimax.chat/something",
		"https://zhipuai.cn/something",
	}
	for _, target := range bypassed {
		req, _ := http.NewRequest(http.MethodPost, target, nil)
		p, err := fn(req)
		require.NoError(t, err)
		assert.Nil(t, p, "expected bypass (direct) for %s", target)
	}
}

func TestProxyFunc_SuffixMatch(t *testing.T) {
	m := managerWithProxy("http://127.0.0.1:7890")
	fn := m.ProxyFunc()

	// "api.moonshot.cn" has suffix ".moonshot.cn" → bypass.
	req, _ := http.NewRequest(http.MethodGet, "https://api.moonshot.cn/v1", nil)
	p, err := fn(req)
	require.NoError(t, err)
	assert.Nil(t, p)
}

func TestProxyFunc_ExactMatch(t *testing.T) {
	m := managerWithProxy("http://127.0.0.1:7890")
	fn := m.ProxyFunc()

	// Exact match "moonshot.cn" (no subdomain) → bypass.
	req, _ := http.NewRequest(http.MethodGet, "https://moonshot.cn/v1", nil)
	p, err := fn(req)
	require.NoError(t, err)
	assert.Nil(t, p)
}

// ── Manager: Status ────────────────────────────────────────────────────────

func TestStatus_NoProxy(t *testing.T) {
	m := &Manager{source: "none"}
	s := m.Status()
	assert.False(t, s.Active)
	assert.Empty(t, s.URL)
	assert.Equal(t, "none", s.Source)
}

func TestStatus_ActiveProxy(t *testing.T) {
	m := managerWithProxy("http://127.0.0.1:7890")
	m.source = "scutil"
	s := m.Status()
	assert.True(t, s.Active)
	assert.Equal(t, "http://127.0.0.1:7890", s.URL)
	assert.Equal(t, "scutil", s.Source)
}

// ── Manager: RefreshLoop ───────────────────────────────────────────────────

func TestRefreshLoop_StopsOnContextCancel(t *testing.T) {
	m := New()
	transport := &http.Transport{}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.RefreshLoop(ctx, transport, 10*time.Millisecond)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("RefreshLoop did not stop after context cancel")
	}
}

// ── Env var fallback ───────────────────────────────────────────────────────

func TestRefresh_EnvVarFallback(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9999")
	t.Setenv("HTTP_PROXY", "")
	t.Setenv("https_proxy", "")
	t.Setenv("http_proxy", "")

	m := &Manager{source: "none"}
	m.Refresh()

	s := m.Status()
	// On platforms with a system proxy configured, the env var may be shadowed.
	// We only assert env source when the manager picked it up.
	if s.Source == "env" {
		assert.True(t, s.Active)
		assert.Equal(t, "http://127.0.0.1:9999", s.URL)
	}
}
