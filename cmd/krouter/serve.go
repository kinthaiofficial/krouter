package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kinthaiofficial/krouter/internal/api"
	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/kinthaiofficial/krouter/internal/logging"
	"github.com/kinthaiofficial/krouter/internal/notifications"
	"github.com/kinthaiofficial/krouter/internal/pricing"
	"github.com/kinthaiofficial/krouter/internal/proxy"
	"github.com/kinthaiofficial/krouter/internal/providers"
	anthropicadapter "github.com/kinthaiofficial/krouter/internal/providers/anthropic"
	deepseekadapter "github.com/kinthaiofficial/krouter/internal/providers/deepseek"
	glmadapter "github.com/kinthaiofficial/krouter/internal/providers/glm"
	groqadapter "github.com/kinthaiofficial/krouter/internal/providers/groq"
	minimaxadapter "github.com/kinthaiofficial/krouter/internal/providers/minimax"
	moonshotadapter "github.com/kinthaiofficial/krouter/internal/providers/moonshot"
	qwenadapter "github.com/kinthaiofficial/krouter/internal/providers/qwen"
	"github.com/kinthaiofficial/krouter/internal/remote"
	"github.com/kinthaiofficial/krouter/internal/routing"
	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/kinthaiofficial/krouter/internal/upgrade"
	"github.com/spf13/cobra"
)

// newServeCommand returns the "serve" subcommand.
// Runs the proxy daemon in the foreground; the OS service manager handles
// backgrounding (see DECISIONS.md D-012).
func newServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run daemon (foreground)",
		Long: `Start the krouter daemon. Typically invoked by LaunchAgent (macOS),
systemd --user (Linux), or Task Scheduler (Windows), not directly by users.

The daemon listens on two ports:
  Proxy port      127.0.0.1:8402  Agent-facing (no auth)
  Management port 127.0.0.1:8403  GUI/CLI-facing (Bearer auth)`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logLevel, _ := cmd.Flags().GetString("log-level")
			proxyPort, _ := cmd.Flags().GetInt("proxy-port")
			mgmtPort, _ := cmd.Flags().GetInt("management-port")

			// Exit silently if another instance is already serving on the proxy port.
			// This prevents token-file clobbering when systemd or the installer
			// starts a second copy while the first is still running.
			if conn, err := net.DialTimeout("tcp",
				fmt.Sprintf("127.0.0.1:%d", proxyPort), 200*time.Millisecond); err == nil {
				conn.Close()
				return nil
			}

			logger := logging.New(logLevel)
			logger.Info("starting krouter",
				"version", Version,
				"build_time", BuildTime,
				"proxy_port", proxyPort,
				"mgmt_port", mgmtPort,
			)

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// Open SQLite store.
			dbPath, err := defaultDBPath()
			if err != nil {
				return fmt.Errorf("resolve db path: %w", err)
			}
			store, err := storage.Open(dbPath)
			if err != nil {
				return fmt.Errorf("open storage: %w", err)
			}
			defer func() { _ = store.Close() }()
			if err := store.Migrate(); err != nil {
				return fmt.Errorf("storage migration: %w", err)
			}
			logger.Info("storage ready", "path", dbPath)

			// Shared HTTP client for provider adapters (no timeout — streaming).
			sharedClient := &http.Client{
				Transport: &http.Transport{
					MaxIdleConns:        100,
					MaxIdleConnsPerHost: 10,
					IdleConnTimeout:     90 * time.Second,
				},
			}

			// Provider registry.
			reg := providers.New()
			reg.Register(anthropicadapter.New("https://api.anthropic.com", sharedClient))
			// Register DeepSeek if the API key is available.
			if os.Getenv("DEEPSEEK_API_KEY") != "" {
				reg.Register(deepseekadapter.New(sharedClient))
				logger.Info("deepseek provider registered")
			}
			if os.Getenv("GROQ_API_KEY") != "" {
				reg.Register(groqadapter.New(sharedClient))
				logger.Info("groq provider registered")
			}
			if os.Getenv("MOONSHOT_API_KEY") != "" {
				reg.Register(moonshotadapter.New(sharedClient))
				logger.Info("moonshot-cn provider registered")
			}
			if os.Getenv("ZHIPU_API_KEY") != "" {
				reg.Register(glmadapter.New(sharedClient))
				logger.Info("glm provider registered")
			}
			if os.Getenv("DASHSCOPE_API_KEY") != "" {
				reg.Register(qwenadapter.New(sharedClient))
				logger.Info("qwen provider registered")
			}
			if os.Getenv("MINIMAX_API_KEY") != "" {
				reg.Register(minimaxadapter.New(sharedClient))
				logger.Info("minimax provider registered")
			}

			// Routing engine.
			engine := routing.New(reg)
			engine.WithHealth(store)

			// Pricing service.
			pricingSvc := pricing.New(store)
			pricingSvc.StartSync(ctx, 24*time.Hour)

			// Proxy server.
			proxySrv := proxy.New(
				proxy.WithLogger(logger),
				proxy.WithEngine(engine),
				proxy.WithRegistry(reg),
				proxy.WithStore(store),
				proxy.WithPricing(pricingSvc),
			)

			// Settings manager (for language preference used by notifications).
			configPath, _ := cmd.Flags().GetString("config")
			settings := config.New(configPath)

			// Notifications service — polls CDN feed every 6h.
			notifSvc := notifications.New(store, settings, reg, Version)
			go func() {
				if err := notifSvc.Start(ctx); err != nil {
					logger.Warn("notifications service stopped", "err", err)
				}
			}()

			// Upgrade service — checks for new versions every 24h.
			upgradeSvc, err := upgrade.New(Version)
			if err != nil {
				logger.Warn("upgrade service init failed", "err", err)
			} else {
				go upgradeSvc.Start(ctx, 24*time.Hour)
			}

			// Remote-access service.
			remoteSvc := remote.New(store)

			// Management API server.
			apiSrv := api.New(store, Version, proxyPort, mgmtPort)
			apiSrv.SetPricing(pricingSvc)
			if upgradeSvc != nil {
				apiSrv.SetUpgrade(upgradeSvc)
			}
			apiSrv.SetRemote(remoteSvc)
			apiSrv.SetRegistry(reg)
			apiSrv.SetSettings(settings)

			// Start management API. When remote access is toggled, the API
			// restarts to switch between plain HTTP (127.0.0.1) and TLS (0.0.0.0).
			go func() {
				runManagementAPI(ctx, apiSrv, remoteSvc, mgmtPort, logger)
			}()

			logger.Info("proxy listening", "host", "127.0.0.1", "port", proxyPort)

			// Proxy blocks; returns on ctx cancellation or fatal error.
			_ = proxySrv.Serve(ctx, "127.0.0.1", proxyPort)

			logger.Info("daemon stopped")
			return nil
		},
	}

	cmd.Flags().Int("proxy-port", 8402, "Proxy port (always bound to 127.0.0.1)")
	cmd.Flags().Int("management-port", 8403, "Management API port")
	cmd.Flags().String("log-level", "info", "Log level: debug/info/warn/error")
	cmd.Flags().String("config", "", "Config file path (default: ~/.kinthai/settings.json)")

	return cmd
}

// runManagementAPI starts the management API and hot-swaps between plain HTTP
// and TLS based on the remote-access service state. When remote is enabled the
// API restarts on 0.0.0.0 with self-signed TLS; on disable it restarts on 127.0.0.1.
func runManagementAPI(ctx context.Context, apiSrv *api.Server, remoteSvc *remote.Service, mgmtPort int, logger logging.Logger) {
	for {
		st := remoteSvc.GetStatus()
		enabledCh := remoteSvc.EnabledCh()

		serveCtx, cancel := context.WithCancel(ctx)

		if st.Enabled {
			hostname, _ := os.Hostname()
			lanIPs := localLANIPs()
			certPEM, keyPEM, err := remote.LoadOrGenerateCert(hostname, lanIPs)
			if err != nil {
				logger.Warn("remote: cannot load TLS cert, falling back to plain HTTP", "err", err)
				logger.Info("management API listening (plain)", "host", "127.0.0.1", "port", mgmtPort)
				go func() { _ = apiSrv.Serve(serveCtx, "127.0.0.1", mgmtPort) }()
			} else {
				logger.Info("management API listening (TLS)", "host", "0.0.0.0", "port", mgmtPort)
				go func() { _ = apiSrv.ServeWithTLS(serveCtx, "0.0.0.0", mgmtPort, certPEM, keyPEM) }()
			}
		} else {
			logger.Info("management API listening (plain)", "host", "127.0.0.1", "port", mgmtPort)
			go func() { _ = apiSrv.Serve(serveCtx, "127.0.0.1", mgmtPort) }()
		}

		select {
		case <-ctx.Done():
			cancel()
			return
		case <-enabledCh:
			// Remote enabled state changed — cancel current server and restart.
			cancel()
			// Brief pause to let the port be released.
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// localLANIPs returns non-loopback IPv4 addresses for TLS SANs.
func localLANIPs() []net.IP {
	var ips []net.IP
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip4 := ip.To4(); ip4 != nil {
				ips = append(ips, ip4)
			}
		}
	}
	return ips
}

// defaultDBPath returns the default SQLite database path (~/.kinthai/data.db).
func defaultDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".kinthai")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "data.db"), nil
}
