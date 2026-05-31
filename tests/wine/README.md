# krouter Wine Integration Tests

Tests krouter Windows binary running under Wine 9.0 on Linux.

## Prerequisites

```bash
# Wine 9.0+
sudo apt install wine wine64   # or winehq-stable

# Tools
sudo apt install curl jq
```

## Files

| 脚本 | 用途 |
|------|------|
| `run_tests.sh` | 主测试套件（17 个测试）— 健康检查、认证、Web UI、SSE、代理端口 |
| `test_proxy_routing.sh` | 路由测试（需要真实 API key）— Balanced/Saver/Quality preset 验证 |
| `setup_openclaw.sh` | 在 Wine 里配置 openclaw 指向 krouter |

## 快速开始

### Option A: krouter-setup.exe 已安装（Windows 安装包）

```bash
# 安装后直接跑测试（daemon 由 Task Scheduler 启动）
cd tests/wine
chmod +x run_tests.sh
./run_tests.sh
```

### Option B: 手动指定二进制（跳过 NSIS）

```bash
# 从 GitHub release 下载 krouter-windows.exe
KROUTER_BIN=/path/to/krouter-windows.exe \
WINEPREFIX=~/.wine-krouter-test \
    ./run_tests.sh
```

## 路由测试（需要 API key）

```bash
ANTHROPIC_API_KEY=sk-ant-... ./test_proxy_routing.sh
```

## openclaw 集成测试

```bash
# 1. 下载 openclaw Windows 二进制
# 2. 配置 openclaw → krouter
OPENCLAW_BIN=/path/to/openclaw-windows.exe ./setup_openclaw.sh

# 3. 发请求验证
ANTHROPIC_API_KEY=sk-ant-... \
    wine /path/to/openclaw-windows.exe run krouter-test -- "say hi"
```

## 测试案例清单

| ID | 测试 | 覆盖的 Bug |
|----|------|-----------|
| T01 | `/health` 返回 200 | 基础 |
| T02 | internal-token 文件存在 | 基础 |
| T03 | 外部 Origin → 403 | CSRF 防护（Origin-based auth） |
| T04 | `/internal/status` 有 token → 200 | 认证 |
| T05 | `/krouter/` 返回 HTML | Web UI |
| T06 | `/krouter/logs` SPA fallback → index.html | SPA fallback |
| T07 | dashboard Origin 无 token → 200 | Origin-based auth |
| T08 | 无 Origin（CLI）无 token → 200 | Origin-based auth |
| T09 | 有效 token + 外部 Origin → 200 | token 覆盖 CSRF |
| T10 | GET `/internal/settings` | settings |
| T11 | POST `/internal/preset` 持久化 | 全局 preset |
| T12 | GET `/internal/budget` | 基础 |
| T13 | GET `/internal/usage` | 基础 |
| T14 | 代理端口 `:8402` 接受连接 | 基础 |
| T15 | SSE heartbeat | 基础 |
| T16 | Task Scheduler 任务已注册 | v2.0.10 Windows fix |
| T17 | ANTHROPIC_BASE_URL 写入注册表 | Windows shell integration |
| R01-R04 | 路由逻辑（需 API key） | v2.0.8 routing fix |

## 已知 Wine 限制

- **T16（Task Scheduler）**：Wine 9.0 对 `schtasks` 支持不完整，可能 SKIP
- **T17（注册表 setx）**：`setx` 在 Wine 下通常可用
- **NSIS 安装包**：不在脚本里测试，建议在真实 Windows 上验证
