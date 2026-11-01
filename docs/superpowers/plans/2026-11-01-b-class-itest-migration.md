# B 类集成测试迁移 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 jiuwenswarm 侧 `test_init_workspace.py`（15 条有效用例）与 `test_team_continuous_dialog.py`（1 条）迁移为 Go 集成测试，并补一条真 LLM 变体，共 17 个 Go 测试方法。

**Architecture:** 新增 Linux PTY 测试助手（`testhelpers.OpenPTY`）让 Go 能进交互分支；init 测试做链路级（`workspace.Init` → `config.New` → 进程内 `transport`/`server`/`gateway` 装配 → WS 往返），由测试文件自身调用三个子系统构造器满足 `cmd/integration-audit` 跨度门禁；team 用例走进程内 `/ws`，先探针确定 MockLLM 能否驱动 team 事件，再定断言档位，强语义由 `real_llm` 真 LLM 版承担。

**Tech Stack:** Go 1.26、`stretchr/testify/suite`、`gorilla/websocket`、`golang.org/x/sys/unix`（PTY ioctl）、`tests/integration/mockllm`

**规格文档：** `docs/superpowers/specs/2026-11-01-b-class-itest-migration-design.md`

---

## 文件结构

| 文件 | 责任 |
|---|---|
| `tests/integration/testhelpers/pty_linux.go` | Linux PTY 打开/装配/写输入三个函数（`//go:build linux`） |
| `tests/integration/testhelpers/pty_stub.go` | 非 Linux 同签名占位，`OpenPTY` 返回 `ErrPTYUnsupported` |
| `tests/integration/testhelpers/pty_linux_test.go` | PTY helper 自测：slave 是 char device、写读闭环 |
| `tests/integration/swarm/workspace/doc.go` | 包文档 + 文件目录 |
| `tests/integration/swarm/workspace/init_flow_test.go` | 全部 init 用例（语言解析 / 结构 / PTY 交互 / force / 链路） |
| `tests/integration/swarm/gateway/team_continuous_dialog_test.go` | team 模式 MockLLM 用例 |
| `tests/integration/real_llm/team_continuous_dialog_real_test.go` | team 模式真 LLM 用例 |

---

## Task 1: T0 探针 —— 观测 MockLLM 能否驱动 team 事件

**目的**：在写正式断言前先确定断言档位。探针只 `t.Log` 事件序列、不做断言，跑完按结论决定 Task 7 的形态。

**Files:**
- Create: `tests/integration/swarm/gateway/team_continuous_dialog_test.go`

- [ ] **Step 1: 写探针测试文件**

```go
//go:build integration

package gateway_test

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/suite"

	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamDialogSuite team 模式连续对话集成测试套件。
type TeamDialogSuite struct {
	isuite.BaseIntegrationSuite
	// providerName 注册到 ClientRegistry 的 mock provider 名
	providerName string
	// wsPort Web 通道监听端口
	wsPort int
	// dataDir 隔离的数据目录
	dataDir string
	// agentServer 进程内 AgentServer
	agentServer *server.AgentServer
	// gwServer 进程内 AppGatewayServer
	gwServer *gateway.AppGatewayServer
	// envBackup 环境变量备份
	envBackup [][3]string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTeamDialogSuite 套件入口。
func TestTeamDialogSuite(t *testing.T) {
	suite.Run(t, new(TeamDialogSuite))
}

// SetupSuite 装配进程内 serve（gateway + server + transport）。
func (s *TeamDialogSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()

	s.providerName = fmt.Sprintf("mock_team_itest_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(s.providerName, "llm", s.MockLLM.Factory())

	s.dataDir = s.T().TempDir()
	configDir := filepath.Join(s.dataDir, "config")
	s.Require().NoError(os.MkdirAll(configDir, 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(s.dataDir, "agent", "workspace"), 0o755))
	yaml := fmt.Sprintf(`models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: %s
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        client_id: %s
      is_default: true
`, s.providerName, s.providerName)
	s.Require().NoError(os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(yaml), 0o644))

	s.wsPort = s.pickFreePort()
	s.saveAndSetenv("UAPCLAW_DATA_DIR", s.dataDir)
	s.saveAndSetenv("UAPCLAW_GATEWAY_PORT", fmt.Sprintf("%d", s.wsPort))
	pathutil.ResetCache()

	cfg, err := config.New("")
	s.Require().NoError(err)
	s.Require().NoError(cfg.Load())

	// ── 三子系统构造（审计跨度来源，勿移到 helper 包）──
	tr := transport.NewChannelTransport()
	s.agentServer = server.NewAgentServer(cfg, tr)
	agentClient := routing.NewAgentClient(tr)
	s.gwServer, err = gateway.NewAppGatewayServer(cfg, agentClient)
	s.Require().NoError(err)

	s.Require().NoError(s.agentServer.Start(s.Ctx))
	s.Require().NoError(s.gwServer.Start(s.Ctx))
}

// TearDownSuite 逆序停机 + 清理注册。
func (s *TeamDialogSuite) TearDownSuite() {
	if s.gwServer != nil {
		_ = s.gwServer.Stop()
	}
	if s.agentServer != nil {
		_ = s.agentServer.Stop()
	}
	if s.providerName != "" {
		_ = model_clients.GetClientRegistry().Unregister(s.providerName, "llm")
	}
	s.restoreEnv()
	pathutil.ResetCache()
	s.BaseIntegrationSuite.TearDownSuite()
}

// TestProbe_Team事件序列 探针：跑一次 mode=team 的 chat.send，记录收到的事件类型。
// 探针只观测不断言，结论决定 TestTeamContinuousDialog 的断言档位。
func (s *TeamDialogSuite) TestProbe_Team事件序列() {
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("team.spawn_member", `{"member_name":"teammate-probe"}`),
		mockllm.CreateTextResponse("这是探针响应"),
		mockllm.CreateTextResponse("探针收尾"),
	)

	conn, err := websocket.DefaultDialer.Dial(
		fmt.Sprintf("ws://127.0.0.1:%d/ws", s.wsPort), nil)
	s.Require().NoError(err, "WS 拨号失败")
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))

	req := map[string]any{
		"type":   "req",
		"id":     "probe-team-1",
		"method": "chat.send",
		"params": map[string]any{
			"session_id": "sess_probe_team",
			"mode":       "team",
			"content":    "告诉我你有哪些工具",
		},
	}
	payload, err := json.Marshal(req)
	s.Require().NoError(err)
	s.Require().NoError(conn.WriteMessage(websocket.TextMessage, payload))

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var frame map[string]any
		if json.Unmarshal(raw, &frame) != nil {
			continue
		}
		event, _ := frame["event"].(string)
		s.T().Logf("事件: event=%q payload_keys=%v", event, keysOf(frame["payload"]))
		if event == "stream.end" {
			break
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pickFreePort 选一个空闲端口。
func (s *TeamDialogSuite) pickFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// saveAndSetenv 记录并设置环境变量。
func (s *TeamDialogSuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	flag := "0"
	if had {
		flag = "1"
	}
	s.envBackup = append(s.envBackup, [3]string{key, orig, flag})
	_ = os.Setenv(key, val)
}

// restoreEnv 恢复环境变量。
func (s *TeamDialogSuite) restoreEnv() {
	for _, e := range s.envBackup {
		if e[2] == "1" {
			_ = os.Setenv(e[0], e[1])
		} else {
			_ = os.Unsetenv(e[0])
		}
	}
	s.envBackup = nil
}

// keysOf 提取 map 的键列表，便于日志观测。
func keysOf(v any) []string {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
```

- [ ] **Step 2: 编译并运行探针**

```bash
cd /home/opensource/uap-claw-go
pgrep -f 'go (build|test)' || true
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" -run 'TestTeamDialogSuite/TestProbe' -v \
  ./tests/integration/swarm/gateway/ 2>&1 | tail -60
```

预期：编译通过；日志里出现一串 `事件: event=...` 行。**若编译报错**（例如 `config.Config` 无 `Load`、`routing.NewAgentClient` 签名不符），按报错修正 import 与调用后再跑。

- [ ] **Step 3: 判定探针结论并记录**

从输出里找是否出现 `event="team.member"` 或 `event="team.message"`：

- **出现** → 记 `探针结论：强断言可用`，Task 7 走强档
- **未出现，但收到其它事件**（`chat.delta` / `chat.final` / `res`） → 记 `探针结论：弱断言`，Task 7 走弱档
- **一个事件都没收到** → 先把 `conn.ReadMessage` 的错误 `t.Logf` 出来（下一步再做一次），排查是装配问题还是 team 路由问题；仍无事件则记 `探针结论：弱断言 + 事件缺失`，Task 7 只断言协议契约并在注释写明偏差

把结论写进 `docs/superpowers/specs/2026-11-01-b-class-itest-migration-design.md` 第 7 节末尾（一行即可）。

- [ ] **Step 4: 提交探针**

```bash
git add tests/integration/swarm/gateway/team_continuous_dialog_test.go docs/superpowers/specs/2026-11-01-b-class-itest-migration-design.md
git commit -m "test(itest): team 模式事件探针（T0）"
```

---

## Task 2: PTY 测试助手

**Files:**
- Create: `tests/integration/testhelpers/pty_linux.go`
- Create: `tests/integration/testhelpers/pty_stub.go`
- Create: `tests/integration/testhelpers/pty_linux_test.go`
- Modify: `tests/integration/testhelpers/doc.go`（文件目录表）
- Modify: `go.mod`（`golang.org/x/sys` 由 indirect 提升为 direct）

- [ ] **Step 1: 写 PTY 自测（先失败）**

```go
//go:build linux

package testhelpers

import (
	"os"
	"testing"
)

// TestOpenPTY_从端是字符设备 验证 slave 的 Stat 报 ModeCharDevice。
// 这正是 workspace.isInteractive() 的判据：管道/普通文件都不满足。
func TestOpenPTY_从端是字符设备(t *testing.T) {
	master, slave, err := OpenPTY()
	if err != nil {
		t.Fatalf("OpenPTY 失败: %v", err)
	}
	defer master.Close()
	defer slave.Close()

	fi, err := slave.Stat()
	if err != nil {
		t.Fatalf("slave.Stat 失败: %v", err)
	}
	if fi.Mode()&os.ModeCharDevice == 0 {
		t.Fatalf("slave 应为字符设备，实际 mode=%v", fi.Mode())
	}
}

// TestOpenPTY_写读闭环 验证 master 写、slave 读的闭环。
func TestOpenPTY_写读闭环(t *testing.T) {
	master, slave, err := OpenPTY()
	if err != nil {
		t.Fatalf("OpenPTY 失败: %v", err)
	}
	defer master.Close()
	defer slave.Close()

	if _, err := master.WriteString("zh\n"); err != nil {
		t.Fatalf("写 master 失败: %v", err)
	}

	buf := make([]byte, 16)
	n, err := slave.Read(buf)
	if err != nil {
		t.Fatalf("读 slave 失败: %v", err)
	}
	if n == 0 {
		t.Fatal("未读到数据")
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
cd /home/opensource/uap-claw-go
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" -run 'TestOpenPTY' -v \
  ./tests/integration/testhelpers/ 2>&1 | tail -20
```

预期：`undefined: OpenPTY`（编译失败）。

- [ ] **Step 3: 实现 Linux PTY**

`tests/integration/testhelpers/pty_linux.go`：

```go
//go:build linux

package testhelpers

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"golang.org/x/sys/unix"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrPTYUnsupported 当前环境无法提供 PTY（/dev/ptmx 缺失等）。
var ErrPTYUnsupported = errors.New("PTY 终端模拟不可用")

// ──────────────────────────── 导出函数 ────────────────────────────

// OpenPTY 打开一对伪终端。
//
// slave 的 Stat().Mode() 含 ModeCharDevice，等价于真实终端——用于覆盖
// 依赖 isatty 判定的交互分支（Go 无 monkeypatch，只能真造终端）。
// 调用方遇错应 t.Skip，不要当作测试失败。
func OpenPTY() (master *os.File, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: 打开 /dev/ptmx 失败: %v", ErrPTYUnsupported, err)
	}

	// TIOCGPTN 取从端编号
	n, err := unix.IoctlGetInt(int(master.Fd()), unix.TIOCGPTN)
	if err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("TIOCGPTN 失败: %w", err)
	}

	// TIOCSPTLCK 写 0 解锁从端
	if err := unix.IoctlSetPointerInt(int(master.Fd()), unix.TIOCSPTLCK, 0); err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("TIOCSPTLCK 失败: %w", err)
	}

	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR, 0)
	if err != nil {
		_ = master.Close()
		return nil, nil, fmt.Errorf("打开 /dev/pts/%d 失败: %w", n, err)
	}
	return master, slave, nil
}

// WithPTYStdin 把 slave 装为 os.Stdin，测试结束自动复原。
//
// 必须在读取 stdin 的代码执行前调用；slave 由调用方负责关闭。
func WithPTYStdin(t *testing.T, slave *os.File) {
	t.Helper()
	old := os.Stdin
	os.Stdin = slave
	t.Cleanup(func() { os.Stdin = old })
}

// WritePTYInput 向 master 写入内容，模拟用户在终端敲键。
//
// 内容需自带换行符：终端行规程（canonical mode）只在收到 \n 时才把
// 输入交给读端。
func WritePTYInput(t *testing.T, master *os.File, input string) {
	t.Helper()
	if _, err := master.WriteString(input); err != nil {
		t.Fatalf("写 PTY 输入失败: %v", err)
	}
}
```

- [ ] **Step 4: 实现非 Linux 占位**

`tests/integration/testhelpers/pty_stub.go`：

```go
//go:build !linux

package testhelpers

import (
	"errors"
	"os"
	"testing"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrPTYUnsupported 当前平台不支持 PTY 终端模拟。
var ErrPTYUnsupported = errors.New("PTY 终端模拟仅在 Linux 支持")

// ──────────────────────────── 导出函数 ────────────────────────────

// OpenPTY 非 Linux 平台恒返回 ErrPTYUnsupported，调用方应 t.Skip。
func OpenPTY() (*os.File, *os.File, error) {
	return nil, nil, ErrPTYUnsupported
}

// WithPTYStdin 非 Linux 平台无实现（OpenPTY 已返回错误，正常不会走到这里）。
func WithPTYStdin(_ *testing.T, _ *os.File) {}

// WritePTYInput 非 Linux 平台无实现（OpenPTY 已返回错误，正常不会走到这里）。
func WritePTYInput(_ *testing.T, _ *os.File, _ string) {}
```

- [ ] **Step 5: 提升 x/sys 为直接依赖**

```bash
cd /home/opensource/uap-claw-go
export GOPROXY=https://goproxy.cn,direct
go get golang.org/x/sys@v0.49.0
go mod tidy
grep -n "golang.org/x/sys" go.mod
```

预期：`golang.org/x/sys v0.49.0` 出现在 `require` 第一块（无 `// indirect`）。

- [ ] **Step 6: 运行 PTY 自测**

```bash
cd /home/opensource/uap-claw-go
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" -run 'TestOpenPTY' -v \
  ./tests/integration/testhelpers/ 2>&1 | tail -20
```

预期：`--- PASS: TestOpenPTY_从端是字符设备`、`--- PASS: TestOpenPTY_写读闭环`。
若报 `PTY 终端模拟不可用` → 该环境无 `/dev/ptmx`，先记为环境限制，继续下一步（init 的 7 条交互用例会自动 Skip）。

- [ ] **Step 7: 更新 doc.go 文件目录**

在 `tests/integration/testhelpers/doc.go` 的文件目录树中按字母序补两行（并补 pty_linux_test.go 说明放在注释末尾即可，`_test.go` 不入目录表）：

```
//	├── pty_linux.go      # Linux PTY 终端模拟（覆盖 isatty 交互分支）
//	└── pty_stub.go       # 非 Linux 同签名占位
```

- [ ] **Step 8: 提交**

```bash
git add tests/integration/testhelpers/pty_linux.go tests/integration/testhelpers/pty_stub.go \
        tests/integration/testhelpers/pty_linux_test.go tests/integration/testhelpers/doc.go go.mod go.sum
git commit -m "test(itest): 新增 Linux PTY 测试助手（免改生产代码覆盖交互分支）"
```

---

## Task 3: init 测试骨架 + 语言解析用例（#1 #2）

**Files:**
- Create: `tests/integration/swarm/workspace/doc.go`
- Create: `tests/integration/swarm/workspace/init_flow_test.go`

- [ ] **Step 1: 写包文档**

`tests/integration/swarm/workspace/doc.go`：

```go
// Package workspace_test 覆盖工作区初始化链路（jiuwenswarm-init）的集成测试。
//
// 对齐 Python jiuwenswarm/tests/system_tests/test_init_workspace.py：
// 语言解析、交互式语言选择、首次初始化目录结构、force 重建、取消语义，
// 以及「初始化产物能让服务真正跑起来」的端到端链路。
//
// 交互分支用 testhelpers.OpenPTY 造真终端——Go 无 monkeypatch，
// os.Pipe 造出的 stdin 被判为 ModeNamedPipe，进不到交互分支。
//
// 文件目录：
//
//	workspace/
//	└── init_flow_test.go   # InitFlowSuite：语言解析 / 结构 / PTY 交互 / force / 链路
//
// 对应 Python 代码：jiuwenswarm/tests/system_tests/test_init_workspace.py
package workspace_test
```

- [ ] **Step 2: 写骨架 + 前两条用例（先失败）**

`tests/integration/swarm/workspace/init_flow_test.go`：

```go
//go:build integration

package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InitFlowSuite 工作区初始化链路集成测试套件。
type InitFlowSuite struct {
	isuite.BaseIntegrationSuite
	// dataRoot 本次测试的隔离根目录
	dataRoot string
	// resDir 模板资源目录
	resDir string
	// wsDir 初始化产生的工作区目录
	wsDir string
	// envBackup 环境变量备份
	envBackup [][3]string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestInitFlowSuite 套件入口。
func TestInitFlowSuite(t *testing.T) {
	suite.Run(t, new(InitFlowSuite))
}

// SetupTest 每条用例重建隔离环境。
func (s *InitFlowSuite) SetupTest() {
	s.dataRoot = s.T().TempDir()
	s.resDir = filepath.Join(s.dataRoot, "resources")
	s.wsDir = filepath.Join(s.dataRoot, "data")
	s.buildTestResources()

	s.saveAndSetenv(workspace.EnvDataDir, s.wsDir)
	s.saveAndSetenv(workspace.EnvResourcesDir, s.resDir)
	workspace.SetUserHome("")
	pathutil.ResetCache()
}

// TearDownTest 恢复环境变量与缓存。
func (s *InitFlowSuite) TearDownTest() {
	s.restoreEnv()
	workspace.SetUserHome("")
	pathutil.ResetCache()
}

// TestInit_显式语言 对齐 Python test_resolve_explicit_language。
// 显式 Language 参数优先，写回 config.yaml 且 config.New 可读回。
func (s *InitFlowSuite) TestInit_显式语言() {
	result, err := workspace.Init(workspace.InitOption{Language: "en"})
	s.Require().NoError(err)
	s.Require().False(result.Cancelled)
	s.Require().Equal(s.wsDir, result.WorkspaceDir)

	cfg, err := config.New("")
	s.Require().NoError(err)
	_, err = cfg.Load()
	s.Require().NoError(err)
	s.Require().Equal("en", cfg.Get("preferred_language"))
}

// TestInit_缺省回落zh 对齐 Python test_resolve_default_to_zh。
// 非交互（管道 stdin）且不传语言时回落 zh。
func (s *InitFlowSuite) TestInit_缺省回落zh() {
	old := os.Stdin
	r, w, err := os.Pipe()
	s.Require().NoError(err)
	os.Stdin = r
	_ = w.Close()
	s.T().Cleanup(func() { os.Stdin = old })

	result, err := workspace.Init(workspace.InitOption{})
	s.Require().NoError(err)
	s.Require().False(result.Cancelled)

	cfg, err := config.New("")
	s.Require().NoError(err)
	_, err = cfg.Load()
	s.Require().NoError(err)
	s.Require().Equal("zh", cfg.Get("preferred_language"))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildTestResources 构造模板资源目录。
//
// config.yaml 必须包含 models 段——链路用例要据此装配 mock provider。
func (s *InitFlowSuite) buildTestResources() {
	ws := filepath.Join(s.resDir, "agent", "workspace")
	s.Require().NoError(os.MkdirAll(filepath.Join(ws, "memory"), 0o755))

	configYAML := `preferred_language: zh
models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: mock
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        client_id: mock
      is_default: true
`
	s.Require().NoError(os.WriteFile(filepath.Join(s.resDir, "config.yaml"), []byte(configYAML), 0o644))
	s.Require().NoError(os.WriteFile(filepath.Join(s.resDir, ".env.template"), []byte("API_KEY=sk-test\n"), 0o644))

	for _, name := range []string{"AGENT", "HEARTBEAT", "IDENTITY", "SOUL"} {
		s.Require().NoError(os.WriteFile(filepath.Join(ws, name+"_ZH.md"), []byte("# "+name+" 中文"), 0o644))
		s.Require().NoError(os.WriteFile(filepath.Join(ws, name+"_EN.md"), []byte("# "+name+" English"), 0o644))
	}
	s.Require().NoError(os.WriteFile(filepath.Join(ws, "USER.md"), []byte("# User"), 0o644))
	s.Require().NoError(os.WriteFile(filepath.Join(ws, "memory", "MEMORY_ZH.md"), []byte("记忆中文"), 0o644))
	s.Require().NoError(os.WriteFile(filepath.Join(ws, "memory", "MEMORY_EN.md"), []byte("Memory English"), 0o644))
}

// saveAndSetenv 记录并设置环境变量。
func (s *InitFlowSuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	flag := "0"
	if had {
		flag = "1"
	}
	s.envBackup = append(s.envBackup, [3]string{key, orig, flag})
	_ = os.Setenv(key, val)
}

// restoreEnv 恢复环境变量并清空备份。
func (s *InitFlowSuite) restoreEnv() {
	for _, e := range s.envBackup {
		if e[2] == "1" {
			_ = os.Setenv(e[0], e[1])
		} else {
			_ = os.Unsetenv(e[0])
		}
	}
	s.envBackup = nil
}
```

- [ ] **Step 3: 编译并运行**

```bash
cd /home/opensource/uap-claw-go
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" -run 'TestInitFlowSuite' -v \
  ./tests/integration/swarm/workspace/ 2>&1 | tail -40
```

预期：两条 PASS。若 `cfg.Get("preferred_language")` 返回 `nil`，先确认 `Init` 确实把 config.yaml 复制到 `s.wsDir/config/config.yaml`（`ls` 一下临时目录或加 `s.T().Log`），再检查 `UAPCLAW_DATA_DIR` 是否在 `config.New` 之前生效。

- [ ] **Step 4: 提交**

```bash
git add tests/integration/swarm/workspace/
git commit -m "test(itest): 工作区初始化骨架 + 语言解析用例"
```

---

## Task 4: init 结构 / force / config 内容用例（#11 #15 + Python skip 项）

**Files:**
- Modify: `tests/integration/swarm/workspace/init_flow_test.go`

> **必须先读**：Go 的 `workspace.Prepare` 只创建 `config/`、`agent/.checkpoint`、`agent/.logs`、`agent/sessions`、`agent/workspace`（含 `memory/`）。Python `conftest.py:165-174` 额外期望 `agent/home`、`agent/skills`、`agent/memory`（模板也放在 `agent/home/PRINCIPLE.md` 而非 `agent/workspace/AGENT.md`）。**这是既有布局差异，本批不改生产代码**，用例按 Go 实际布局断言，并在注释中标注与 Python 的差异。

- [ ] **Step 1: 追加三条用例**

```go
// TestInit_首次初始化_结构 对齐 Python test_init_user_workspace_first_time。
//
// 断言按 Go 实际布局（workspace.Prepare）：config/ + agent/{.checkpoint,.logs,
// sessions,workspace}。与 Python conftest.py:165-174 的 agent/home、
// agent/skills、agent/memory 存在布局差异，本批不修改生产代码，仅记录。
func (s *InitFlowSuite) TestInit_首次初始化_结构() {
	result, err := workspace.Init(workspace.InitOption{Language: "zh"})
	s.Require().NoError(err)
	s.Require().False(result.Cancelled)

	dirs := []string{
		filepath.Join(s.wsDir, "config"),
		filepath.Join(s.wsDir, "agent", "workspace"),
		filepath.Join(s.wsDir, "agent", "workspace", "memory"),
		filepath.Join(s.wsDir, "agent", "sessions"),
		filepath.Join(s.wsDir, "agent", ".checkpoint"),
		filepath.Join(s.wsDir, "agent", ".logs"),
	}
	for _, d := range dirs {
		s.Require().DirExists(d)
	}

	files := []string{
		filepath.Join(s.wsDir, "config", "config.yaml"),
		filepath.Join(s.wsDir, "config", ".env"),
		filepath.Join(s.wsDir, "agent", "workspace", "AGENT.md"),
		filepath.Join(s.wsDir, "agent", "workspace", "HEARTBEAT.md"),
		filepath.Join(s.wsDir, "agent", "workspace", "IDENTITY.md"),
		filepath.Join(s.wsDir, "agent", "workspace", "SOUL.md"),
		filepath.Join(s.wsDir, "agent", "workspace", "memory", "MEMORY.md"),
	}
	for _, f := range files {
		s.Require().FileExists(f)
	}
}

// TestInit_force重建 对齐 Python test_main_force_init。
// force 模式清空工作区并重建，脏文件被清除。
func (s *InitFlowSuite) TestInit_force重建() {
	s.Require().NoError(os.MkdirAll(s.wsDir, 0o755))
	dirty := filepath.Join(s.wsDir, "dirty.txt")
	s.Require().NoError(os.WriteFile(dirty, []byte("stale"), 0o644))

	result, err := workspace.Init(workspace.InitOption{Language: "en", Overwrite: true})
	s.Require().NoError(err)
	s.Require().False(result.Cancelled)
	s.Require().NoFileExists(dirty)
	s.Require().FileExists(filepath.Join(s.wsDir, "config", "config.yaml"))
}

// TestInit_config内容 覆盖 Python 中被 skip 的 test_config_file_content。
// config.yaml 的语言与 models 段能被 config.New 正确解析。
func (s *InitFlowSuite) TestInit_config内容() {
	_, err := workspace.Init(workspace.InitOption{Language: "en"})
	s.Require().NoError(err)

	cfg, err := config.New("")
	s.Require().NoError(err)
	_, err = cfg.Load()
	s.Require().NoError(err)

	s.Require().Equal("en", cfg.Get("preferred_language"))
	s.Require().NotNil(cfg.Get("models.defaults"), "models.defaults 应可解析")
}
```

- [ ] **Step 2: 运行**

```bash
cd /home/opensource/uap-claw-go
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" -run 'TestInitFlowSuite' -v \
  ./tests/integration/swarm/workspace/ 2>&1 | tail -40
```

预期：5 条 PASS。

- [ ] **Step 3: 提交**

```bash
git add tests/integration/swarm/workspace/init_flow_test.go
git commit -m "test(itest): 初始化结构 / force 重建 / config 内容断言"
```

---

## Task 5: PTY 交互用例（#3-#10 #12 #14）

**Files:**
- Modify: `tests/integration/swarm/workspace/init_flow_test.go`

- [ ] **Step 1: 追加 PTY helper 与 10 条用例**

在 `init_flow_test.go` 的非导出函数区追加：

```go
// ──────────────────────────── 非导出函数（PTY） ────────────────────────────

// withPTY 打开 PTY、装上 stdin、写入 input，返回清理函数。
// 环境不支持 PTY 时 Skip（不判失败）。
func (s *InitFlowSuite) withPTY(input string) {
	s.T().Helper()
	master, slave, err := testhelpers.OpenPTY()
	if err != nil {
		s.T().Skipf("当前环境不支持 PTY：%v", err)
	}
	s.T().Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	testhelpers.WithPTYStdin(s.T(), slave)
	testhelpers.WritePTYInput(s.T(), master, input)
}

// TestPrompt_选择编号1 对齐 Python test_prompt_select_chinese。
func (s *InitFlowSuite) TestPrompt_选择编号1() {
	s.withPTY("1\n")
	result, err := workspace.Init(workspace.InitOption{})
	s.Require().NoError(err)
	s.Require().False(result.Cancelled)
	s.Require().Equal("zh", s.readPreferredLanguage())
}

// TestPrompt_选择编号2 对齐 Python test_prompt_select_english。
func (s *InitFlowSuite) TestPrompt_选择编号2() {
	s.withPTY("2\n")
	result, err := workspace.Init(workspace.InitOption{})
	s.Require().NoError(err)
	s.Require().False(result.Cancelled)
	s.Require().Equal("en", s.readPreferredLanguage())
}

// TestPrompt_别名zh 对齐 Python test_prompt_select_zh_alias。
func (s *InitFlowSuite) TestPrompt_别名zh() {
	s.withPTY("zh\n")
	result, err := workspace.Init(workspace.InitOption{})
	s.Require().NoError(err)
	s.Require().False(result.Cancelled)
	s.Require().Equal("zh", s.readPreferredLanguage())
}

// TestPrompt_别名en 对齐 Python test_prompt_select_en_alias。
func (s *InitFlowSuite) TestPrompt_别名en() {
	s.withPTY("en\n")
	result, err := workspace.Init(workspace.InitOption{})
	s.Require().NoError(err)
	s.Require().False(result.Cancelled)
	s.Require().Equal("en", s.readPreferredLanguage())
}

// TestPrompt_取消no 对齐 Python test_prompt_cancel_with_no。
func (s *InitFlowSuite) TestPrompt_取消no() {
	s.withPTY("no\n")
	s.requireCancelledWithoutWorkspace()
}

// TestPrompt_取消n 对齐 Python test_prompt_cancel_with_n。
func (s *InitFlowSuite) TestPrompt_取消n() {
	s.withPTY("n\n")
	s.requireCancelledWithoutWorkspace()
}

// TestPrompt_取消q 对齐 Python test_prompt_cancel_with_q。
func (s *InitFlowSuite) TestPrompt_取消q() {
	s.withPTY("q\n")
	s.requireCancelledWithoutWorkspace()
}

// TestPrompt_非法输入 对齐 Python test_prompt_invalid_input_returns_none。
func (s *InitFlowSuite) TestPrompt_非法输入() {
	s.withPTY("xyz\n")
	s.requireCancelledWithoutWorkspace()
}

// TestInit_取消不落盘 对齐 Python test_init_user_workspace_cancel_language_selection。
func (s *InitFlowSuite) TestInit_取消不落盘() {
	s.withPTY("q\n")
	s.requireCancelledWithoutWorkspace()
}

// TestInit_覆盖前拒绝 对齐 Python test_main_cancelled_init。
// force 模式下对 overwrite 确认答 no，原工作区保持不动。
func (s *InitFlowSuite) TestInit_覆盖前拒绝() {
	_, err := workspace.Init(workspace.InitOption{Language: "zh"})
	s.Require().NoError(err)
	sentinel := filepath.Join(s.wsDir, "sentinel.txt")
	s.Require().NoError(os.WriteFile(sentinel, []byte("keep"), 0o644))

	s.withPTY("no\n")
	result, err := workspace.Init(workspace.InitOption{Language: "zh", Overwrite: true})
	s.Require().NoError(err)
	s.Require().True(result.Cancelled)
	s.Require().FileExists(sentinel)
}

// readPreferredLanguage 读回工作区 config.yaml 中的 preferred_language。
func (s *InitFlowSuite) readPreferredLanguage() any {
	cfg, err := config.New("")
	s.Require().NoError(err)
	_, err = cfg.Load()
	s.Require().NoError(err)
	return cfg.Get("preferred_language")
}

// requireCancelledWithoutWorkspace 断言取消且未创建工作区。
func (s *InitFlowSuite) requireCancelledWithoutWorkspace() {
	result, err := workspace.Init(workspace.InitOption{})
	s.Require().NoError(err)
	s.Require().True(result.Cancelled, "应取消初始化")
	s.Require().NoDirExists(filepath.Join(s.wsDir, "config"))
}
```

并在 import 块补：

```go
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
```

- [ ] **Step 2: 运行**

```bash
cd /home/opensource/uap-claw-go
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" -run 'TestInitFlowSuite' -v \
  ./tests/integration/swarm/workspace/ 2>&1 | tail -60
```

预期：15 条 PASS（PTY 不可用时其中 10 条 SKIP，日志显示 `当前环境不支持 PTY`）。

**若卡住不返回**：说明 `promptPreferredLanguage` 在读 stdin 阻塞——检查 `withPTY` 是否在 `Init` 之前调用、`WritePTYInput` 是否带了 `\n`。用 `-timeout 120s` 兜底定位。

- [ ] **Step 3: 提交**

```bash
git add tests/integration/swarm/workspace/init_flow_test.go
git commit -m "test(itest): PTY 覆盖交互式语言选择与取消语义（10 条）"
```

---

## Task 6: init 链路用例（#13 服务可启动）

**Files:**
- Modify: `tests/integration/swarm/workspace/init_flow_test.go`

- [ ] **Step 1: 追加链路用例**

```go
// TestInit_链路_服务可启动 覆盖 Python 中被 skip 的 test_full_initialization_flow。
//
// 初始化产物 → 配置加载 → 进程内 transport/server/gateway 装配 → WS 握手
// → initialize 往返。这条用例同时是本目录通过 integration-audit 调用跨度
// 门禁的依据：文件内真实调用 swarm/transport、swarm/server、swarm/gateway
// 三个子系统，跨度 = 3。
func (s *InitFlowSuite) TestInit_链路_服务可启动() {
	_, err := workspace.Init(workspace.InitOption{Language: "en"})
	s.Require().NoError(err)

	wsPort := s.pickFreePort()
	s.saveAndSetenv("UAPCLAW_GATEWAY_PORT", fmt.Sprintf("%d", wsPort))
	pathutil.ResetCache()

	cfg, err := config.New("")
	s.Require().NoError(err)
	_, err = cfg.Load()
	s.Require().NoError(err)
	s.Require().Equal("en", cfg.Get("preferred_language"))

	ctx := s.Ctx
	tr := transport.NewChannelTransport()
	agentServer := server.NewAgentServer(cfg, tr)
	agentClient := routing.NewAgentClient(tr)
	gwServer, err := gateway.NewAppGatewayServer(cfg, agentClient)
	s.Require().NoError(err)

	s.Require().NoError(agentServer.Start(ctx))
	s.Require().NoError(gwServer.Start(ctx))
	s.T().Cleanup(func() {
		_ = gwServer.Stop()
		_ = agentServer.Stop()
	})

	url := fmt.Sprintf("ws://127.0.0.1:%d/ws", wsPort)
	var conn *websocket.Conn
	s.Require().Eventually(func() bool {
		c, dialErr := websocket.DefaultDialer.Dial(url, nil)
		if dialErr != nil {
			return false
		}
		conn = c
		return true
	}, 10*time.Second, 200*time.Millisecond, "WS 拨号超时：%s", url)
	defer conn.Close()

	// initialize 是轻量握手方法，不触发 LLM 调用
	req := map[string]any{
		"type":   "req",
		"id":     "init-probe-1",
		"method": "initialize",
		"params": map[string]any{},
	}
	payload, err := json.Marshal(req)
	s.Require().NoError(err)
	s.Require().NoError(conn.WriteMessage(websocket.TextMessage, payload))

	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	for {
		_, raw, err := conn.ReadMessage()
		s.Require().NoError(err, "等待 initialize 响应超时")
		var frame map[string]any
		if json.Unmarshal(raw, &frame) != nil {
			continue
		}
		if id, _ := frame["id"].(string); id == "init-probe-1" {
			s.T().Logf("initialize 响应: %s", string(raw))
			return
		}
	}
}
```

在 import 块补：

```go
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/gorilla/websocket"

	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
```

非导出函数区补：

```go
// pickFreePort 选一个空闲端口。
func (s *InitFlowSuite) pickFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
```

- [ ] **Step 2: 运行**

```bash
cd /home/opensource/uap-claw-go
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" -run 'TestInitFlowSuite/TestInit_链路' -v \
  ./tests/integration/swarm/workspace/ 2>&1 | tail -40
```

预期：PASS，日志打印 `initialize 响应: {...}`。

**若 10 秒内拨号失败**：检查 `UAPCLAW_GATEWAY_PORT` 是否在 `gateway.NewAppGatewayServer` 之前设置（该值在构造期被读入 `webCfg.Port`），以及端口是否被占用。

- [ ] **Step 3: 提交**

```bash
git add tests/integration/swarm/workspace/init_flow_test.go
git commit -m "test(itest): 初始化产物驱动进程内 serve 全链路（对齐 Python skip 的 full_initialization_flow）"
```

---

## Task 7: team 连续对话用例（MockLLM 版）

**Files:**
- Modify: `tests/integration/swarm/gateway/team_continuous_dialog_test.go`

- [ ] **Step 1: 用正式用例替换探针**

删除 `TestProbe_Team事件序列`，改写成（**强档版本**；若 Task 1 结论为弱档，把断言 ② 换成注释里给的弱档三断言）：

```go
// TestTeamContinuousDialog 对齐 Python test_team_continuous_dialog（Python 侧 skip）。
//
// 场景：同一 session 首条 chat.send（mode=team）尚未结束时发出第二条，
// 验证服务端不互斥、两条请求都被受理。
//
// 断言档位：强档（T0 探针证明 MockLLM 可驱动 team 事件）。
// 若探针结论为弱档，替换为：
//   ① 两条 request id 均有响应 ② 无互斥错误帧 ③ 首条流未在第二条到达前终止。
func (s *TeamDialogSuite) TestTeamContinuousDialog() {
	s.ResetCalls()
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("team.spawn_member", `{"member_name":"teammate-itest"}`),
		mockllm.CreateTextResponse("第一条消息的响应"),
		mockllm.CreateTextResponse("第二条消息的响应"),
	)

	conn, err := websocket.DefaultDialer.Dial(
		fmt.Sprintf("ws://127.0.0.1:%d/ws", s.wsPort), nil)
	s.Require().NoError(err, "WS 拨号失败")
	defer conn.Close()

	sessionID := fmt.Sprintf("sess_team_%d", time.Now().UnixNano())
	send := func(id, content string) {
		req := map[string]any{
			"type":   "req",
			"id":     id,
			"method": "chat.send",
			"params": map[string]any{
				"session_id": sessionID,
				"mode":       "team",
				"content":    content,
			},
		}
		payload, err := json.Marshal(req)
		s.Require().NoError(err)
		s.Require().NoError(conn.WriteMessage(websocket.TextMessage, payload))
	}

	send("req-team-1", "告诉我你有哪些工具，不要立刻开始任务")

	var (
		events           []map[string]any
		sawTeamEvent     bool
		sentSecond       bool
		secondResponded  bool
		firstStreamEnded bool
	)

	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var frame map[string]any
		if json.Unmarshal(raw, &frame) != nil {
			continue
		}
		events = append(events, frame)

		switch eventOf(frame) {
		case "team.member", "team.message":
			sawTeamEvent = true
		case "stream.end":
			if !sentSecond {
				firstStreamEnded = true
			}
		}
		if idOf(frame) == "req-team-2" {
			secondResponded = true
		}

		if !sentSecond && sawTeamEvent {
			send("req-team-2", "现在创建一个成员叫安娜，让安娜告诉我他有哪些工具")
			sentSecond = true
		}
		if secondResponded && sawTeamEvent {
			break
		}
	}

	s.Require().NotEmpty(events, "应收到至少一个事件")
	s.True(sentSecond, "第二条消息应在首条流未结束时发出")
	s.False(firstStreamEnded, "首条流不应在第二条到达前终止")
	s.True(sawTeamEvent, "应收到 team.member 或 team.message 事件")
	s.True(secondResponded, "第二条请求应收到响应")
}
```

非导出函数区补：

```go
// eventOf 取帧的 event 名（顶层或 payload 内层）。
func eventOf(frame map[string]any) string {
	if e, ok := frame["event"].(string); ok && e != "" {
		return e
	}
	if inner, ok := frame["payload"].(map[string]any); ok {
		if e, ok := inner["event_type"].(string); ok {
			return e
		}
	}
	return ""
}

// idOf 取帧的 id。
func idOf(frame map[string]any) string {
	id, _ := frame["id"].(string)
	return id
}
```

- [ ] **Step 2: 运行**

```bash
cd /home/opensource/uap-claw-go
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" -run 'TestTeamDialogSuite' -v \
  -timeout 300s ./tests/integration/swarm/gateway/ 2>&1 | tail -60
```

预期：PASS。

**若 `sawTeamEvent` 为 false**：说明其实处在弱档——按文件注释换成弱档断言，并在同目录 `doc.go` 记录降级原因。

- [ ] **Step 3: 更新 `swarm/gateway/doc.go` 文件目录**

在目录树中补：

```
//	├── team_continuous_dialog_test.go  # team 模式连续对话（MockLLM 版）
```

- [ ] **Step 4: 提交**

```bash
git add tests/integration/swarm/gateway/
git commit -m "test(itest): team 模式连续对话（同 session 并发第二条 chat.send）"
```

---

## Task 8: team 真 LLM 用例

**Files:**
- Create: `tests/integration/real_llm/team_continuous_dialog_real_test.go`
- Modify: `tests/integration/real_llm/doc.go`（文件目录）
- Modify: `.github/workflows/real-llm-ci.yml`（注释「12 套件」→「13 套件」）

- [ ] **Step 1: 写用例**

```go
//go:build integration && llm

package real_llm

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamContinuousRealSuite team 连续对话真 LLM 套件。
type TeamContinuousRealSuite struct {
	isuite.RealLLMSuite
	// wsPort Web 通道端口
	wsPort int
	// dataDir 隔离数据目录
	dataDir string
	// agentServer 进程内 AgentServer
	agentServer *server.AgentServer
	// gwServer 进程内 AppGatewayServer
	gwServer *gateway.AppGatewayServer
	// envBackup 环境变量备份
	envBackup [][3]string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTeamContinuousRealSuite 套件入口。
func TestTeamContinuousRealSuite(t *testing.T) {
	suite.Run(t, new(TeamContinuousRealSuite))
}

// SetupSuite 装真实 LLM 配置 + 进程内 serve。
func (s *TeamContinuousRealSuite) SetupSuite() {
	s.RealLLMSuite.SetupSuite()
	if s.RealClientConfig == nil {
		s.T().Skip("LLM_API_KEY 未设置，跳过真 LLM 套件")
	}

	s.dataDir = s.T().TempDir()
	configDir := filepath.Join(s.dataDir, "config")
	s.Require().NoError(os.MkdirAll(configDir, 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(s.dataDir, "agent", "workspace"), 0o755))

	yaml := fmt.Sprintf(`models:
  defaults:
    - model_client_config:
        model_name: %s
        client_provider: %s
        api_key: %s
        api_base: %s
        client_id: real_llm_team
      is_default: true
`, s.RealModelConfig.ModelName, s.RealClientConfig.ClientProvider,
		s.RealClientConfig.APIKey, s.RealClientConfig.APIBase)
	s.Require().NoError(os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(yaml), 0o644))

	s.wsPort = s.pickFreePort()
	s.saveAndSetenv("UAPCLAW_DATA_DIR", s.dataDir)
	s.saveAndSetenv("UAPCLAW_GATEWAY_PORT", fmt.Sprintf("%d", s.wsPort))
	pathutil.ResetCache()

	cfg, err := config.New("")
	s.Require().NoError(err)
	_, err = cfg.Load()
	s.Require().NoError(err)

	tr := transport.NewChannelTransport()
	s.agentServer = server.NewAgentServer(cfg, tr)
	agentClient := routing.NewAgentClient(tr)
	s.gwServer, err = gateway.NewAppGatewayServer(cfg, agentClient)
	s.Require().NoError(err)

	s.Require().NoError(s.agentServer.Start(s.Ctx))
	s.Require().NoError(s.gwServer.Start(s.Ctx))
}

// TearDownSuite 逆序停机。
func (s *TeamContinuousRealSuite) TearDownSuite() {
	if s.gwServer != nil {
		_ = s.gwServer.Stop()
	}
	if s.agentServer != nil {
		_ = s.agentServer.Stop()
	}
	s.restoreEnv()
	pathutil.ResetCache()
	s.RealLLMSuite.TearDownSuite()
}

// TestTeamContinuousDialog_真实调用 完全对齐 Python test_team_continuous_dialog 场景。
func (s *TeamContinuousRealSuite) TestTeamContinuousDialog_真实调用() {
	conn, err := websocket.DefaultDialer.Dial(
		fmt.Sprintf("ws://127.0.0.1:%d/ws", s.wsPort), nil)
	s.Require().NoError(err)
	defer conn.Close()

	sessionID := fmt.Sprintf("sess_team_real_%d", time.Now().UnixNano())
	send := func(id, content string) {
		req := map[string]any{
			"type":   "req",
			"id":     id,
			"method": "chat.send",
			"params": map[string]any{
				"session_id": sessionID,
				"mode":       "team",
				"content":    content,
			},
		}
		payload, err := json.Marshal(req)
		s.Require().NoError(err)
		s.Require().NoError(conn.WriteMessage(websocket.TextMessage, payload))
	}

	send("req-real-1", "告诉我你有哪些工具，不要立刻开始任务")

	var (
		sawTeamEvent bool
		sentSecond   bool
		secondOK     bool
	)
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			break
		}
		var frame map[string]any
		if json.Unmarshal(raw, &frame) != nil {
			continue
		}
		if e := realEventOf(frame); e == "team.member" || e == "team.message" {
			sawTeamEvent = true
		}
		if realIDOf(frame) == "req-real-2" {
			secondOK = true
		}
		if !sentSecond && sawTeamEvent {
			send("req-real-2", "现在创建一个成员叫安娜，让安娜告诉我他有哪些工具")
			sentSecond = true
		}
		if secondOK {
			break
		}
	}

	s.True(sentSecond, "应在首条流未结束时发出第二条消息")
	s.True(sawTeamEvent, "应收到 team.member 或 team.message 事件（真 LLM）")
	s.True(secondOK, "第二条消息应被受理并返回响应")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pickFreePort 选一个空闲端口。
func (s *TeamContinuousRealSuite) pickFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// saveAndSetenv 记录并设置环境变量。
func (s *TeamContinuousRealSuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	flag := "0"
	if had {
		flag = "1"
	}
	s.envBackup = append(s.envBackup, [3]string{key, orig, flag})
	_ = os.Setenv(key, val)
}

// restoreEnv 恢复环境变量。
func (s *TeamContinuousRealSuite) restoreEnv() {
	for _, e := range s.envBackup {
		if e[2] == "1" {
			_ = os.Setenv(e[0], e[1])
		} else {
			_ = os.Unsetenv(e[0])
		}
	}
	s.envBackup = nil
}

// realEventOf 取帧的 event 名。
func realEventOf(frame map[string]any) string {
	if e, ok := frame["event"].(string); ok && e != "" {
		return e
	}
	if inner, ok := frame["payload"].(map[string]any); ok {
		if e, ok := inner["event_type"].(string); ok {
			return e
		}
	}
	return ""
}

// realIDOf 取帧的 id。
func realIDOf(frame map[string]any) string {
	id, _ := frame["id"].(string)
	return id
}
```

- [ ] **Step 2: 编译（不真实调用）**

```bash
cd /home/opensource/uap-claw-go
CGO_ENABLED=1 go vet -tags "sqlite_fts5 test integration llm" ./tests/integration/real_llm/ 2>&1 | tail -20
```

预期：无输出（编译通过）。

- [ ] **Step 3: 有 key 时本地真跑一次**

```bash
cd /home/opensource/uap-claw-go
LLM_API_KEY="$LLM_API_KEY" RUN_REAL_LLM_TESTS=1 CGO_ENABLED=1 \
  go test -tags "sqlite_fts5 test integration llm" -run 'TestTeamContinuousRealSuite' -v \
  -timeout 20m ./tests/integration/real_llm/ 2>&1 | tail -40
```

预期：有 key 时 PASS；无 key 时 `--- SKIP`。无 key 也可提交，夜间 job 会跑。

- [ ] **Step 4: 更新文档与 workflow 注释**

`tests/integration/real_llm/doc.go` 文件目录补一行：

```
//	├── team_continuous_dialog_real_test.go  # team 连续对话（真 LLM）
```

`.github/workflows/real-llm-ci.yml` 中两处「12 套件」改为「13 套件」，并在运行范围清单末尾补 `team_continuous`。

- [ ] **Step 5: 提交**

```bash
git add tests/integration/real_llm/ .github/workflows/real-llm-ci.yml
git commit -m "test(itest): team 连续对话真 LLM 变体（对齐 Python skip 用例语义）"
```

---

## Task 9: 门禁自检 + 文档同步 + CI 全绿

**Files:**
- Modify: `docs/superpowers/specs/2026-11-01-b-class-itest-migration-design.md`（补「实施结论」小节）
- Possibly Modify: `.integration-scope-exempt`（仅当 `swarm/workspace` 被判灰区）

- [ ] **Step 1: 全目录跑一遍**

```bash
cd /home/opensource/uap-claw-go
pgrep -f 'go (build|test)' || true
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" -v -count=1 \
  ./tests/integration/swarm/workspace/... ./tests/integration/swarm/gateway/... 2>&1 | tail -60
```

预期：全部 PASS（PTY 用例在无 `/dev/ptmx` 环境 SKIP）。

- [ ] **Step 2: 覆盖率不倒退（改了测试文件会拉低目录覆盖时需确认）**

```bash
cd /home/opensource/uap-claw-go
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" \
  -coverprofile=/tmp/itest_b_class.out ./tests/integration/swarm/workspace/... 2>&1 | tail -5
```

预期：输出 `coverage: xx.x%`。仅记录数值，不设阈值（该目录为集成测试，覆盖率由审计第二层判定）。

- [ ] **Step 3: 审计自检**

```bash
cd /home/opensource/uap-claw-go
go run ./cmd/integration-audit -verify 2>&1 | tail -40
```

预期：`swarm/workspace` 与 `swarm/gateway` 均不出现「跨度不足」/「覆盖不足」告警。

**若 `swarm/workspace` 被判灰区**：先在链路用例里补一条 `agentcore/session` 会话落盘断言（调用 `session` 包构造会话目录），再次 `-verify`；仍不通过才在 `.integration-scope-exempt` 的 C 类追加：

```
# 工作区初始化链路：初始化产物驱动进程内 serve，覆盖 common/workspace（不计跨度）
# + swarm/transport + swarm/server + swarm/gateway；跨度 3 但覆盖集中于 common
tests/integration/swarm/workspace
```

- [ ] **Step 4: 提交并推送，观察 CI**

```bash
cd /home/opensource/uap-claw-go
git add -A
git commit -m "chore(itest): B 类集成测试门禁自检与文档同步"
git push
gh run list --limit 3
```

- [ ] **Step 5: 等四绿**

```bash
gh run watch <run-id> --exit-status
```

预期：Build / Lint / Unit Test / Integration Test 四个 job 全 SUCCESS。红灯则按日志修复后再次推送，直到全绿——**本地通过不算完成**。

- [ ] **Step 6: 在规格文档补实施结论**

在 `docs/superpowers/specs/2026-11-01-b-class-itest-migration-design.md` 末尾追加：

```markdown
## 11. 实施结论

- T0 探针结论：<强断言可用 / 弱断言 / 弱断言+事件缺失>
- init 用例：<N> 条落地，PTY 覆盖 <M> 条（环境无 /dev/ptmx 时 Skip <K> 条）
- team 用例：MockLLM 版 <强/弱> 档 + 真 LLM 版
- 审计：`integration-audit -verify` 结果
- 已知布局差异：Go 未创建 agent/home、agent/skills、agent/memory（Python conftest.py:165-174 期望），本批未修改生产代码
- CI：<run-id> 四绿
```

提交：

```bash
git add docs/superpowers/specs/2026-11-01-b-class-itest-migration-design.md
git commit -m "docs(itest): B 类迁移实施结论"
git push
```

---

## 自检记录

**规格覆盖**：spec §4.1 PTY → Task 2；§4.2 进程内装配 → Task 6/7/8；§5 init 16 行映射 → Task 3（#1#2）、Task 4（#11#15+config）、Task 5（#3-#10 #12 #14）、Task 6（#13）；§6.1 MockLLM team → Task 1（探针）+ Task 7；§6.2 真 LLM → Task 8；§8 验收 → Task 9。

**类型一致性**：`OpenPTY() (*os.File, *os.File, error)`、`WithPTYStdin(t, *os.File)`、`WritePTYInput(t, *os.File, string)` 三个签名在 Task 2 定义、Task 5 使用，一致；`InitFlowSuite.pickFreePort/saveAndSetenv/restoreEnv` 在 Task 3 定义、Task 4-6 使用，一致；`TeamDialogSuite` 的 `pickFreePort/saveAndSetenv/restoreEnv` 在 Task 1 定义、Task 7 使用，一致。

**已知风险**：探针结论可能落在弱档；`swarm/workspace` 可能被判灰区；PTY 可能在 CI 不可用。三者均有明确降级路径（Task 1 Step 3、Task 9 Step 3、Task 5 Step 1）。
