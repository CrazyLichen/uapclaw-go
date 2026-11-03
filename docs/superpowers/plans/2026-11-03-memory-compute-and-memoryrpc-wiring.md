# memory.compute 真值实现 + memoryrpc 接线 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `/web` 的 `memory.compute` 返回真实内存数据，并让 `memory.list/edit/status/toggle/open` 在 `/tui` 路由以本地 handler 形式真正可用。

**Architecture:** 严格按 Python 分层：`memory.compute` 留在 web 通道（`web/memory_compute.go`），5 个 memory.* 作为 `/tui` 本地 handler（`tui/tui_memory_handlers.go`）薄封装已有的 `memoryrpc.HandleMemory*`。采集用 gopsutil v3（psutil 的 Go 等价物）。wrapper 自己发 res 帧并恒返回 nil，避免 gateway 二次包装错误文案。

**Tech Stack:** Go 1.26、gopsutil v3（`process`/`mem`）、gorilla/websocket、testify、项目内 `integration` build tag 测试套件。

**Spec:** `docs/superpowers/specs/2026-11-03-memory-compute-and-memoryrpc-wiring-design.md`

---

## 文件结构

| 文件 | 职责 |
|---|---|
| `internal/swarm/gateway/channel_manager/web/memory_compute.go`（新建） | `handleMemoryCompute()` + `collectMemory` 注入点 + `bytesToMB` |
| `internal/swarm/gateway/channel_manager/web/memory_compute_test.go`（新建） | 换算/真值/错误分支单测 + 真 WS 四字段用例 |
| `internal/swarm/gateway/channel_manager/web/web_handlers.go`（改 1 行） | `memory.compute` 由 stub 换成真 handler |
| `internal/swarm/gateway/channel_manager/tui/tui_memory_handlers.go`（新建） | 5 方法 wrapper + `memoryMode`/`resolveProjectDir`/`withProjectDir` |
| `internal/swarm/gateway/channel_manager/tui/tui_memory_handlers_test.go`（新建） | 参数解析、成功/错误/panic 帧、默认映射覆盖 |
| `internal/swarm/gateway/channel_manager/tui/tui_connect.go`（改 5 行） | `Install` 内注册 5 个本地 handler |
| `internal/swarm/gateway/channel_manager/web/doc.go`、`tui/doc.go` | 文件目录补条目 |
| `tests/integration/swarm/gateway/tui_cli_route_test.go`（追加方法） | `memory.status` / `memory.list` 端到端 |
| `go.mod` | gopsutil v3 提为直接依赖 |
| `IMPLEMENTATION_PLAN.md` | 11.15 / 10.6.13-18 回填标注 |

---

## Task 1: gopsutil v3 提升为直接依赖

**Files:**
- Modify: `go.mod:83-84`（直接依赖块，`sentencizer` 与 `spf13/cobra` 之间插入）
- Modify: `go.mod:181`（删除 indirect 行）

- [ ] **Step 1: 在直接依赖块插入 gopsutil**

`go.mod` 直接依赖块内，`github.com/sentencizer/sentencizer v0.3.0` 之后插入一行：

```
	github.com/shirou/gopsutil/v3 v3.23.12
```

- [ ] **Step 2: 删除间接依赖行**

删除第 181 行：

```
	github.com/shirou/gopsutil/v3 v3.23.12 // indirect
```

- [ ] **Step 3: 校验依赖图未被改动**

```bash
cd /home/opensource/uap-claw-go && go mod verify
```

Expected: `all modules verified`

- [ ] **Step 4: 提交**

```bash
git add go.mod go.sum
git commit -m "chore(deps): gopsutil v3 提升为直接依赖（内存采集）"
```

---

## Task 2: memory.compute 采集与换算

**Files:**
- Create: `internal/swarm/gateway/channel_manager/web/memory_compute.go`
- Test: `internal/swarm/gateway/channel_manager/web/memory_compute_test.go`

- [ ] **Step 1: 写失败测试（换算 + 真值 + 错误分支）**

新建 `internal/swarm/gateway/channel_manager/web/memory_compute_test.go`：

```go
package web

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHandleMemoryCompute_字节换算为MB 锁死 bytes/(1024*1024) 公式。
func TestHandleMemoryCompute_字节换算为MB(t *testing.T) {
	orig := collectMemory
	defer func() { collectMemory = orig }()
	collectMemory = func() (uint64, uint64, uint64, float64, error) {
		return 1048576, 2097152, 524288, 25.5, nil
	}

	payload, err := handleMemoryCompute()(context.Background(), nil, "")
	require.NoError(t, err)
	require.Equal(t, 1.0, payload["rss_mb"])
	require.Equal(t, 2.0, payload["total_mb"])
	require.Equal(t, 0.5, payload["available_mb"])
	require.Equal(t, 25.5, payload["used_percent"])
}

// TestHandleMemoryCompute_真实采集四字段 断言真实 gopsutil 采集的取值范围。
func TestHandleMemoryCompute_真实采集四字段(t *testing.T) {
	payload, err := handleMemoryCompute()(context.Background(), nil, "")
	require.NoError(t, err)

	rssMB, ok := payload["rss_mb"].(float64)
	require.True(t, ok, "rss_mb 应为 float64，实际 %T", payload["rss_mb"])
	require.Greater(t, rssMB, 0.0, "本进程 RSS 应大于 0")

	totalMB, ok := payload["total_mb"].(float64)
	require.True(t, ok, "total_mb 应为 float64，实际 %T", payload["total_mb"])
	require.Greater(t, totalMB, 0.0, "系统总内存应大于 0")

	availMB, ok := payload["available_mb"].(float64)
	require.True(t, ok, "available_mb 应为 float64，实际 %T", payload["available_mb"])
	require.Greater(t, availMB, 0.0, "系统可用内存应大于 0")

	usedPercent, ok := payload["used_percent"].(float64)
	require.True(t, ok, "used_percent 应为 float64，实际 %T", payload["used_percent"])
	require.GreaterOrEqual(t, usedPercent, 0.0)
	require.LessOrEqual(t, usedPercent, 100.0)

	require.LessOrEqual(t, rssMB, totalMB, "进程 RSS 不应超过系统总内存")
}

// TestHandleMemoryCompute_采集失败返回错误 覆盖 gopsutil 错误分支。
func TestHandleMemoryCompute_采集失败返回错误(t *testing.T) {
	orig := collectMemory
	defer func() { collectMemory = orig }()
	collectMemory = func() (uint64, uint64, uint64, float64, error) {
		return 0, 0, 0, 0, errors.New("collect failed")
	}

	payload, err := handleMemoryCompute()(context.Background(), nil, "")
	require.Error(t, err)
	require.Nil(t, payload)
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/ -run 'TestHandleMemoryCompute' -v
```

Expected: 编译失败 `undefined: collectMemory`

- [ ] **Step 3: 写最小实现**

新建 `internal/swarm/gateway/channel_manager/web/memory_compute.go`：

```go
package web

import (
	"context"
	"fmt"
	"os"

	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// collectMemory 采集本进程与系统内存，返回字节数及系统内存使用百分比。
//
// 以包级变量暴露供测试注入：gopsutil 的错误分支无法由真实环境稳定触发。
var collectMemory = defaultCollectMemory

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleMemoryCompute 处理 memory.compute 请求，返回进程与系统内存占用。
//
// Python: _memory_compute (app_web_handlers.py L1414-1427)
func handleMemoryCompute() RPCHandlerFunc {
	return func(_ context.Context, _ map[string]any, _ string) (map[string]any, error) {
		rssBytes, totalBytes, availableBytes, usedPercent, err := collectMemory()
		if err != nil {
			logger.Warn(logComponent).
				Err(err).
				Str("method", "memory.compute").
				Msg("memory.compute collect failed")
			return nil, err
		}
		return map[string]any{
			"rss_mb":       bytesToMB(rssBytes),
			"total_mb":     bytesToMB(totalBytes),
			"available_mb": bytesToMB(availableBytes),
			"used_percent": usedPercent,
		}, nil
	}
}

// defaultCollectMemory 用 gopsutil 采集真实数据（psutil 等价物）。
//
// Python: process = psutil.Process(); process.memory_info().rss; psutil.virtual_memory()
func defaultCollectMemory() (rssBytes, totalBytes, availableBytes uint64, usedPercent float64, err error) {
	proc, err := process.NewProcess(int32(os.Getpid()))
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("获取当前进程信息失败: %w", err)
	}
	procMem, err := proc.MemoryInfo()
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("获取进程内存信息失败: %w", err)
	}
	vm, err := mem.VirtualMemory()
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("获取系统内存信息失败: %w", err)
	}
	return procMem.RSS, vm.Total, vm.Available, vm.UsedPercent, nil
}

// bytesToMB 字节转 MB，对齐 Python bytes / (1024 * 1024)。
func bytesToMB(b uint64) float64 {
	return float64(b) / (1024 * 1024)
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/ -run 'TestHandleMemoryCompute' -v
```

Expected: `--- PASS: TestHandleMemoryCompute_字节换算为MB` / `_真实采集四字段` / `_采集失败返回错误`

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/gateway/channel_manager/web/memory_compute.go internal/swarm/gateway/channel_manager/web/memory_compute_test.go
git commit -m "feat(web): memory.compute 返回真实内存数据，含 used_percent"
```

---

## Task 3: 替换 memory.compute 桩 + 真 WS 四字段用例

**Files:**
- Modify: `internal/swarm/gateway/channel_manager/web/web_handlers.go:316-317`
- Test: `internal/swarm/gateway/channel_manager/web/memory_compute_test.go`（追加）

- [ ] **Step 1: 写失败测试（真 WS 走 dispatcher）**

在 `memory_compute_test.go` 追加（import 块补齐 `encoding/json`、`net/http`、`net/http/httptest`、`strings`、`time`、`github.com/gorilla/websocket`）：

```go
// TestMemoryCompute_真实WS返回四字段 真 WebChannel + 真 dispatcher + 真采集。
func TestMemoryCompute_真实WS返回四字段(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	RegisterWebHandlers(&WebHandlersBindParams{Channel: wc})
	require.NoError(t, wc.Start(context.Background()))
	defer func() { _ = wc.Stop(context.Background()) }()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wc.HandleWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	reqFrame := ReqFrame{Type: "req", ID: "mem-1", Method: "memory.compute"}
	reqData, err := reqFrame.Encode()
	require.NoError(t, err)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, reqData))

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, respData, err := conn.ReadMessage()
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(respData, &resp))
	require.Equal(t, "res", resp["type"])
	require.Equal(t, true, resp["ok"])

	payload, ok := resp["payload"].(map[string]any)
	require.True(t, ok, "payload 应为 map，实际 %T", resp["payload"])

	for _, key := range []string{"rss_mb", "total_mb", "available_mb"} {
		value, isNum := payload[key].(float64)
		require.True(t, isNum, "%s 应为 number，实际 %T", key, payload[key])
		require.Greater(t, value, 0.0, "%s 应大于 0", key)
	}
	usedPercent, isNum := payload["used_percent"].(float64)
	require.True(t, isNum, "used_percent 应为 number，实际 %T", payload["used_percent"])
	require.GreaterOrEqual(t, usedPercent, 0.0)
	require.LessOrEqual(t, usedPercent, 100.0)
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/ -run 'TestMemoryCompute_真实WS' -v
```

Expected: FAIL，payload 里没有 `rss_mb`（桩返回 `rss`/`total`）

- [ ] **Step 3: 替换注册行**

`internal/swarm/gateway/channel_manager/web/web_handlers.go` 第 316-317 行，把：

```go
	// ─── 内存信息 ───
	d.Register("memory.compute", stubHandler("memory.compute", map[string]any{"rss": 0, "total": 0}))
```

改为：

```go
	// ─── 内存信息 ───
	// Python: _memory_compute (app_web_handlers.py L1414-1427)，psutil 真实采集
	d.Register("memory.compute", handleMemoryCompute())
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/ -run 'TestMemoryCompute_真实WS' -v
```

Expected: `--- PASS: TestMemoryCompute_真实WS返回四字段`

- [ ] **Step 5: 跑整包测试（现有用例只断言方法已注册，不改动）**

```bash
cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/web/ -v 2>&1 | tail -20
```

Expected: 全 PASS（`TestRegisterWebHandlers_全量注册` 仅校验 `memory.compute` 键存在，不校验 payload，无需改动）

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/gateway/channel_manager/web/web_handlers.go internal/swarm/gateway/channel_manager/web/memory_compute_test.go
git commit -m "fix(web): memory.compute 由写死桩换成真实采集，修界面内存占用恒为 0"
```

---

## Task 4: tui 参数解析辅助函数

**Files:**
- Create: `internal/swarm/gateway/channel_manager/tui/tui_memory_handlers_test.go`
- Create: `internal/swarm/gateway/channel_manager/tui/tui_memory_handlers.go`（本任务只写辅助函数）

- [ ] **Step 1: 写失败测试（project_dir 三级回退 / mode 缺省 / params 不改）**

新建 `internal/swarm/gateway/channel_manager/tui/tui_memory_handlers_test.go`：

```go
package tui

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveProjectDir_三级回退 对齐 Python _resolve_project_dir。
func TestResolveProjectDir_三级回退(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		want   string
	}{
		{"project_dir 优先", map[string]any{"project_dir": "/a", "trusted_dirs": []any{"/b"}, "cwd": "/c"}, "/a"},
		{"trusted_dirs 次之", map[string]any{"trusted_dirs": []any{"/b"}, "cwd": "/c"}, "/b"},
		{"cwd 兜底", map[string]any{"cwd": "/c"}, "/c"},
		{"全缺省返回空", map[string]any{}, ""},
		{"空字符串视为缺省", map[string]any{"project_dir": "", "cwd": ""}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, resolveProjectDir(tc.params))
		})
	}
}

// TestMemoryMode_缺省为plan 对齐 Python params.get("mode", "plan")。
func TestMemoryMode_缺省为plan(t *testing.T) {
	require.Equal(t, "plan", memoryMode(map[string]any{}))
	require.Equal(t, "plan", memoryMode(map[string]any{"mode": ""}))
	require.Equal(t, "code.normal", memoryMode(map[string]any{"mode": "code.normal"}))
}

// TestWithProjectDir_不修改入参且命中时注入副本 对齐 Python {**params, ...}。
func TestWithProjectDir_不修改入参且命中时注入副本(t *testing.T) {
	params := map[string]any{"cwd": "/c"}

	merged := withProjectDir(params, "/p")
	require.Equal(t, "/p", merged["project_dir"])
	require.Equal(t, "/c", merged["cwd"])
	_, hasKey := params["project_dir"]
	require.False(t, hasKey, "不得就地修改调用方 map")

	unchanged := withProjectDir(params, "")
	require.Equal(t, "/c", unchanged["cwd"])
	_, hasKey = unchanged["project_dir"]
	require.False(t, hasKey)
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/tui/ -run 'TestResolveProjectDir|TestMemoryMode|TestWithProjectDir' -v
```

Expected: 编译失败 `undefined: resolveProjectDir`

- [ ] **Step 3: 写最小实现（仅辅助函数）**

新建 `internal/swarm/gateway/channel_manager/tui/tui_memory_handlers.go`：

```go
package tui

// ──────────────────────────── 常量 ────────────────────────────

const (
	// memoryRPCErrorCode 记忆 RPC 内部错误码，对齐 Python code="INTERNAL_ERROR"
	memoryRPCErrorCode = "INTERNAL_ERROR"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// memoryMode 取请求 mode，缺省 "plan"。
//
// Python: mode = params.get("mode", "plan")
func memoryMode(params map[string]any) string {
	if mode, ok := params["mode"].(string); ok && mode != "" {
		return mode
	}
	return "plan"
}

// resolveProjectDir 解析项目目录。
//
// Python: _resolve_project_dir —— params.project_dir → params.trusted_dirs[0] → params.cwd
func resolveProjectDir(params map[string]any) string {
	if projectDir, ok := params["project_dir"].(string); ok && projectDir != "" {
		return projectDir
	}
	if dirs, ok := params["trusted_dirs"].([]any); ok && len(dirs) > 0 {
		if projectDir, ok := dirs[0].(string); ok && projectDir != "" {
			return projectDir
		}
	}
	if cwd, ok := params["cwd"].(string); ok && cwd != "" {
		return cwd
	}
	return ""
}

// withProjectDir 返回注入 project_dir 后的 params 副本；未命中时原样返回。
//
// Python: if project_dir: params = {**params, "project_dir": project_dir}
func withProjectDir(params map[string]any, projectDir string) map[string]any {
	if projectDir == "" {
		return params
	}
	merged := make(map[string]any, len(params)+1)
	for key, value := range params {
		merged[key] = value
	}
	merged["project_dir"] = projectDir
	return merged
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/tui/ -run 'TestResolveProjectDir|TestMemoryMode|TestWithProjectDir' -v
```

Expected: 3 个顶层用例全 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/gateway/channel_manager/tui/tui_memory_handlers.go internal/swarm/gateway/channel_manager/tui/tui_memory_handlers_test.go
git commit -m "feat(tui): 记忆 RPC 参数解析辅助（workspace/mode/project_dir 三级回退）"
```

---

## Task 5: 五方法 wrapper + 帧行为

**Files:**
- Modify: `internal/swarm/gateway/channel_manager/tui/tui_memory_handlers.go`（追加映射与 wrapper）
- Test: `internal/swarm/gateway/channel_manager/tui/tui_memory_handlers_test.go`（追加）

- [ ] **Step 1: 写失败测试（成功帧 / error 字段 / panic / 默认映射五方法 / edit 真失败）**

在 `tui_memory_handlers_test.go` 追加（import 补齐 `context`、`os`、`path/filepath`、`github.com/gorilla/websocket`、`pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"`）：

```go
// capturedFrame 捕获的 res 帧
type capturedFrame struct {
	reqID   string
	ok      bool
	payload map[string]any
	errMsg  string
	code    string
}

// fakeResSender 捕获 SendResFrame 调用
type fakeResSender struct {
	frames []capturedFrame
}

// SendResFrame 实现 ResponseSender
func (f *fakeResSender) SendResFrame(_ *websocket.Conn, reqID string, ok bool, payload map[string]any, errMsg, code string) {
	f.frames = append(f.frames, capturedFrame{reqID: reqID, ok: ok, payload: payload, errMsg: errMsg, code: code})
}

// lastFrame 取最后一帧
func (f *fakeResSender) lastFrame(t *testing.T) capturedFrame {
	t.Helper()
	require.NotEmpty(t, f.frames, "应有 res 帧")
	return f.frames[len(f.frames)-1]
}

// setupAgentWorkspace 设置隔离的 UAPCLAW_DATA_DIR 并返回 agent 工作区路径
func setupAgentWorkspace(t *testing.T) string {
	t.Helper()
	dataDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "config"), 0o755))
	agentWorkspace := filepath.Join(dataDir, "agent", "workspace")
	require.NoError(t, os.MkdirAll(agentWorkspace, 0o755))

	t.Setenv(pathutil.EnvDataDir, dataDir)
	pathutil.ResetCache()
	t.Cleanup(pathutil.ResetCache)
	return agentWorkspace
}

// TestMemoryLocalHandler_成功帧透传payload
func TestMemoryLocalHandler_成功帧透传payload(t *testing.T) {
	orig := memoryHandlers["memory.list"]
	defer func() { memoryHandlers["memory.list"] = orig }()
	memoryHandlers["memory.list"] = func(_ context.Context, workspace, mode string, params map[string]any) map[string]any {
		return map[string]any{"echo_workspace": workspace, "echo_mode": mode, "echo_project": params["project_dir"]}
	}

	sender := &fakeResSender{}
	handler := memoryLocalHandler(sender, "memory.list")
	require.NoError(t, handler(nil, "req-1", map[string]any{"mode": "code.normal", "cwd": "/proj"}, "sess_1"))

	frame := sender.lastFrame(t)
	require.True(t, frame.ok)
	require.Empty(t, frame.errMsg)
	require.Equal(t, "code.normal", frame.payload["echo_mode"])
	require.Equal(t, "/proj", frame.payload["echo_project"], "list 应注入 project_dir")
}

// TestMemoryLocalHandler_error字段转失败帧 result["error"] → ok=false + INTERNAL_ERROR
func TestMemoryLocalHandler_error字段转失败帧(t *testing.T) {
	orig := memoryHandlers["memory.edit"]
	defer func() { memoryHandlers["memory.edit"] = orig }()
	memoryHandlers["memory.edit"] = func(_ context.Context, _, _ string, _ map[string]any) map[string]any {
		return map[string]any{"error": "mkdir failed"}
	}

	sender := &fakeResSender{}
	handler := memoryLocalHandler(sender, "memory.edit")
	require.NoError(t, handler(nil, "req-2", map[string]any{}, "sess_1"))

	frame := sender.lastFrame(t)
	require.False(t, frame.ok)
	require.Equal(t, "mkdir failed", frame.errMsg)
	require.Equal(t, "INTERNAL_ERROR", frame.code)
	require.Nil(t, frame.payload)
}

// TestMemoryLocalHandler_panic转失败帧 覆盖 defer recover 分支
func TestMemoryLocalHandler_panic转失败帧(t *testing.T) {
	orig := memoryHandlers["memory.status"]
	defer func() { memoryHandlers["memory.status"] = orig }()
	memoryHandlers["memory.status"] = func(_ context.Context, _, _ string, _ map[string]any) map[string]any {
		panic("boom")
	}

	sender := &fakeResSender{}
	handler := memoryLocalHandler(sender, "memory.status")
	require.NoError(t, handler(nil, "req-3", map[string]any{}, "sess_1"))

	frame := sender.lastFrame(t)
	require.False(t, frame.ok)
	require.Equal(t, "boom", frame.errMsg)
	require.Equal(t, "INTERNAL_ERROR", frame.code)
}

// TestMemoryHandlers_默认映射覆盖五个方法 生产映射必须能调通（memory 目录为普通文件，
// 使索引初始化快速失败，仅验证映射与调用链，不做业务断言）。
func TestMemoryHandlers_默认映射覆盖五个方法(t *testing.T) {
	agentWorkspace := setupAgentWorkspace(t)
	require.NoError(t, os.WriteFile(filepath.Join(agentWorkspace, "memory"), []byte("not a dir"), 0o644))

	for _, method := range []string{"memory.list", "memory.edit", "memory.status", "memory.toggle", "memory.open"} {
		handler, ok := memoryHandlers[method]
		require.True(t, ok, "缺少方法 %s", method)
		result := handler(context.Background(), agentWorkspace, "plan", map[string]any{})
		require.NotNil(t, result, "%s 应返回 payload", method)
	}
}

// TestMemoryLocalHandler_edit真失败走失败帧 用生产 handler 触发 mkdir 失败（memory 是普通文件）。
func TestMemoryLocalHandler_edit真失败走失败帧(t *testing.T) {
	agentWorkspace := setupAgentWorkspace(t)
	require.NoError(t, os.WriteFile(filepath.Join(agentWorkspace, "memory"), []byte("not a dir"), 0o644))

	sender := &fakeResSender{}
	handler := memoryLocalHandler(sender, "memory.edit")
	require.NoError(t, handler(nil, "req-4", map[string]any{"path": "memory/new.md"}, "sess_1"))

	frame := sender.lastFrame(t)
	require.False(t, frame.ok, "父目录不可创建时应为失败帧")
	require.Equal(t, "INTERNAL_ERROR", frame.code)
	require.NotEmpty(t, frame.errMsg)
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/tui/ -run 'TestMemoryLocalHandler|TestMemoryHandlers' -v
```

Expected: 编译失败 `undefined: memoryHandlers`

- [ ] **Step 3: 写实现（映射 + wrapper）**

在 `tui_memory_handlers.go` 追加（`import` 块补齐 `context`、`fmt`、`github.com/gorilla/websocket`、`memoryrpc`、`routing`、`workspace`）：

```go
// memoryHandlerFunc 统一记忆 RPC handler 签名（mode 对 edit/open 无意义但保持同形）
type memoryHandlerFunc func(ctx context.Context, workspace, mode string, params map[string]any) map[string]any

// memoryHandlers 方法 → handler 映射。
//
// 生产填充 memoryrpc 实现；测试可替换单项以覆盖 error/panic 分支。
var memoryHandlers = map[string]memoryHandlerFunc{
	"memory.list": func(ctx context.Context, ws, mode string, params map[string]any) map[string]any {
		return memoryrpc.HandleMemoryList(ctx, ws, mode, params)
	},
	"memory.edit": func(ctx context.Context, ws, _ string, params map[string]any) map[string]any {
		return memoryrpc.HandleMemoryEdit(ctx, ws, params)
	},
	"memory.status": func(ctx context.Context, ws, mode string, params map[string]any) map[string]any {
		return memoryrpc.HandleMemoryStatus(ctx, ws, mode, params)
	},
	"memory.toggle": func(ctx context.Context, ws, mode string, params map[string]any) map[string]any {
		return memoryrpc.HandleMemoryToggle(ctx, ws, mode, params)
	},
	"memory.open": func(ctx context.Context, ws, _ string, params map[string]any) map[string]any {
		return memoryrpc.HandleMemoryOpen(ctx, ws, params)
	},
}

// memoryProjectDirMethods 需要解析并注入 project_dir 的方法。
//
// Python: _memory_toggle 不解析 project_dir，其余四个解析。
var memoryProjectDirMethods = map[string]bool{
	"memory.list":   true,
	"memory.edit":   true,
	"memory.status": true,
	"memory.open":   true,
}
```

追加 wrapper：

```go
// memoryLocalHandler 构造记忆 RPC 的本地 handler。
//
// Python: tui_connect.py 的 _memory_list/_memory_edit/_memory_status/_memory_toggle/_memory_open
//
// 与 Python 一致：handler 自己发 res 帧并恒返回 nil——gateway 对返回 error 的本地
// handler 会二次包装成 "handler error: ..."，与 Python 文案不符。
func memoryLocalHandler(send ResponseSender, method string) routing.LocalHandlerFunc {
	return func(ws *websocket.Conn, reqID string, params map[string]any, _ string) error {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error(logComponent).
					Str("method", method).
					Any("recover", recovered).
					Msg("memory rpc panic recovered")
				send.SendResFrame(ws, reqID, false, nil, fmt.Sprintf("%v", recovered), memoryRPCErrorCode)
			}
		}()

		methodParams := params
		if memoryProjectDirMethods[method] {
			methodParams = withProjectDir(params, resolveProjectDir(params))
		}

		result := memoryHandlers[method](context.Background(), workspace.AgentWorkspaceDir(), memoryMode(params), methodParams)
		if errMsg, failed := result["error"].(string); failed && errMsg != "" {
			logger.Warn(logComponent).
				Str("method", method).
				Str("error", errMsg).
				Msg("memory rpc failed")
			send.SendResFrame(ws, reqID, false, nil, errMsg, memoryRPCErrorCode)
			return nil
		}

		send.SendResFrame(ws, reqID, true, result, "", "")
		return nil
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/channel_manager/tui/ -v 2>&1 | tail -25
```

Expected: 全 PASS（含 Task 4 的 3 个用例）

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/gateway/channel_manager/tui/tui_memory_handlers.go internal/swarm/gateway/channel_manager/tui/tui_memory_handlers_test.go
git commit -m "feat(tui): 记忆 RPC 本地 handler（自己发帧 + recover + error 字段转失败帧）"
```

---

## Task 6: 注册 5 个本地 handler + doc.go 同步

**Files:**
- Modify: `internal/swarm/gateway/channel_manager/tui/tui_connect.go:261-265`
- Modify: `internal/swarm/gateway/channel_manager/tui/doc.go`、`internal/swarm/gateway/channel_manager/web/doc.go`

- [ ] **Step 1: Install 内注册**

`tui_connect.go` 的 `Install` 回调中，`rs.RegisterLocalHandler("/tui", "history.get", historyGetHandler(rs))` 之后追加：

```go
			// 记忆 RPC（对齐 Python tui_connect.py L1903-1907）
			for method := range memoryHandlers {
				rs.RegisterLocalHandler("/tui", method, memoryLocalHandler(rs, method))
			}
```

- [ ] **Step 2: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/swarm/... && go vet ./internal/swarm/gateway/channel_manager/tui/
```

Expected: 无输出（成功）

- [ ] **Step 3: 更新两个 doc.go 的文件目录**

`tui/doc.go`：在文件树中 `├── tui_connect.go  # ...` 之后加一行：

```
//	├── tui_memory_handlers.go  # 记忆 RPC 本地 handler（list/edit/status/toggle/open）
```

`web/doc.go`：在文件树中补一行：

```
//	├── memory_compute.go  # memory.compute：进程/系统内存采集
```

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/gateway/channel_manager/tui/tui_connect.go internal/swarm/gateway/channel_manager/tui/doc.go internal/swarm/gateway/channel_manager/web/doc.go
git commit -m "feat(tui): /tui 路由注册 memory.* 五个本地 handler 并同步 doc.go"
```

---

## Task 7: 集成用例（memory.status / memory.list）

**Files:**
- Modify: `tests/integration/swarm/gateway/tui_cli_route_test.go`（追加 2 个方法 + 1 个发送辅助）

- [ ] **Step 1: 追加发送辅助与两个用例**

在 `tui_cli_route_test.go` 末尾追加（该文件只 import `testify/suite`；`require := s.Require()` 是局部变量，**不要**再 import `testify/require`，否则同名冲突）：

```go
// sendReq 发送任意 method 的 req 帧
func (s *CliRouteFullChainSuite) sendReq(conn *websocket.Conn, reqID, method string, params map[string]any) {
	frame := map[string]any{
		"type":   "req",
		"id":     reqID,
		"method": method,
		"params": params,
	}
	data, err := json.Marshal(frame)
	s.Require().NoError(err)
	s.Require().NoError(conn.WriteMessage(websocket.TextMessage, data))
}

// readRes 读指定 reqID 的 res 帧
func (s *CliRouteFullChainSuite) readRes(conn *websocket.Conn, reqID string) map[string]any {
	return s.readFrameUntil(conn, 10*time.Second, func(f map[string]any) bool {
		return f["type"] == "res" && f["id"] == reqID
	})
}

// TestMemoryStatusFullChain /tui 本地 handler 返回真实记忆统计。
func (s *CliRouteFullChainSuite) TestMemoryStatusFullChain() {
	agentWorkspace := filepath.Join(os.Getenv("UAPCLAW_DATA_DIR"), "agent", "workspace")
	require := s.Require()
	require.NoError(os.MkdirAll(filepath.Join(agentWorkspace, "memory"), 0o755))
	require.NoError(os.WriteFile(filepath.Join(agentWorkspace, "memory", "auto_one.md"), []byte("# one\n"), 0o644))
	require.NoError(os.WriteFile(filepath.Join(agentWorkspace, "memory", "auto_two.md"), []byte("# two\n"), 0o644))
	require.NoError(os.WriteFile(filepath.Join(agentWorkspace, "MEMORY.md"), []byte("# project\n"), 0o644))

	conn := s.dialWS()
	defer conn.Close()

	s.sendReq(conn, "mem-status-1", "memory.status", map[string]any{"detailed": true})
	res := s.readRes(conn, "mem-status-1")
	require.Equal(true, res["ok"], "memory.status 应成功，帧: %v", res)

	payload, ok := res["payload"].(map[string]any)
	require.True(ok, "payload 应为 map")
	require.Equal("plan", payload["current_mode"], "未传 mode 时应为 plan（Python 缺省）")
	require.Contains(payload, "storage_mode")
	require.Contains(payload, "enabled")

	autoMemory, ok := payload["auto_memory"].(map[string]any)
	require.True(ok, "auto_memory 应为 map")
	require.Equal(float64(2), autoMemory["files_count"], "workspace/memory 下应有 2 个 md")

	projectMemory, ok := payload["project_memory"].(map[string]any)
	require.True(ok, "project_memory 应为 map")
	require.GreaterOrEqual(t, projectMemory["files_count"].(float64), float64(1), "应发现项目记忆文件")

	_, isArray := payload["large_files"].([]any)
	require.True(t, isArray, "large_files 应为数组")
}

// TestMemoryListFullChain /tui 本地 handler 列出记忆文件。
func (s *CliRouteFullChainSuite) TestMemoryListFullChain() {
	agentWorkspace := filepath.Join(os.Getenv("UAPCLAW_DATA_DIR"), "agent", "workspace")
	require := s.Require()
	require.NoError(os.MkdirAll(filepath.Join(agentWorkspace, "memory"), 0o755))
	target := filepath.Join(agentWorkspace, "memory", "list_target.md")
	require.NoError(os.WriteFile(target, []byte("# target\n"), 0o644))

	conn := s.dialWS()
	defer conn.Close()

	s.sendReq(conn, "mem-list-1", "memory.list", map[string]any{"include_project": false})
	res := s.readRes(conn, "mem-list-1")
	require.Equal(true, res["ok"], "memory.list 应成功，帧: %v", res)

	payload, ok := res["payload"].(map[string]any)
	require.True(ok, "payload 应为 map")
	require.Equal("plan", payload["mode"], "mode 应回显 plan")

	files, ok := payload["files"].([]any)
	require.True(ok, "files 应为数组")
	found := false
	for _, item := range files {
		fileMap, isMap := item.(map[string]any)
		if !isMap {
			continue
		}
		if fileMap["path"] == target {
			found = true
			require.Equal("auto", fileMap["kind"], "workspace/memory 下的文件 kind 应为 auto")
		}
	}
	require.True(found, "files 应包含 %s", target)
}
```

- [ ] **Step 2: 运行两个集成用例**

```bash
cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/gateway/ -run 'TestCliRouteFullChainSuite/(TestMemoryStatusFullChain|TestMemoryListFullChain)' -v
```

Expected: 两个用例 PASS

- [ ] **Step 3: 跑整个 gateway 集成套件防串扰**

```bash
cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/gateway/ -v 2>&1 | tail -20
```

Expected: 全 PASS（若 `TestMemoryStatusFullChain` 因前序用例残留 `MEMORY.md` 导致计数变化，把 `project_memory` 断言改为 `>= 1`——上面已按 `GreaterOrEqual` 写）

- [ ] **Step 4: 审计自检**

```bash
cd /home/opensource/uap-claw-go && go run ./cmd/integration-audit -verify 2>&1 | tail -20
```

Expected: 无 FAIL

- [ ] **Step 5: 提交**

```bash
git add tests/integration/swarm/gateway/tui_cli_route_test.go
git commit -m "test(itest): /tui 记忆 RPC 端到端用例（memory.status/list 真实计数）"
```

---

## Task 8: 覆盖率、计划状态同步、全量验证

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md:725`（10.6.13-18 行）、`IMPLEMENTATION_PLAN.md:764`（11.15 行）

- [ ] **Step 1: 覆盖率检查（改动文件 ≥85%）**

```bash
cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -coverprofile=/tmp/memcov.out ./internal/swarm/gateway/channel_manager/web/ ./internal/swarm/gateway/channel_manager/tui/ && go tool cover -func=/tmp/memcov.out | grep -E "memory_compute|tui_memory_handlers|total:"
```

Expected: `memory_compute.go` 与 `tui_memory_handlers.go` 每函数 100% 或接近，`total:` ≥ 85%。若低于 85%，补齐对应未覆盖分支（多半是 `defaultCollectMemory` 的三条错误分支——用 `collectMemory` 注入覆盖换算/错误即可，真实采集函数本身的行由 Task 2 的真值用例覆盖）。

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN 两处**

- `IMPLEMENTATION_PLAN.md:725`（10.6.13-18）把结尾 `⤵️ TUI 面板注册(11.15)` 改为 `✅ TUI 面板注册(11.15 已回填)`
- `IMPLEMENTATION_PLAN.md:764`（11.15）把「⤴️ 需回填来源：10.6.18 memory_rpc 5 个 local handler 注册进 TUI 记忆管理面板」改为「✅ 10.6.18 memory_rpc 5 个 local handler 已注册进 /tui（memory.list/edit/status/toggle/open）」；11.15 状态保持 `🔄`（27 个纯本地 handler 仍有 22 个未补）

- [ ] **Step 3: 格式与静态检查**

```bash
cd /home/opensource/uap-claw-go && gofmt -l internal/swarm/gateway/channel_manager/web internal/swarm/gateway/channel_manager/tui tests/integration/swarm/gateway
```

Expected: 无输出。若有输出，`gofmt -w <文件>` 后重跑。

```bash
cd /home/opensource/uap-claw-go && go vet ./internal/swarm/gateway/... ./tests/integration/swarm/gateway/
```

Expected: 无输出

- [ ] **Step 4: 全量单测**

```bash
cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/gateway/... 2>&1 | tail -15
```

Expected: 全 `ok`，无 `FAIL`

- [ ] **Step 5: 提交计划状态**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(plan): 10.6.18/11.15 记忆 RPC 接线标注为已回填"
```

- [ ] **Step 6: 推送并盯 CI 四绿**

```bash
cd /home/opensource/uap-claw-go && git push origin HEAD && gh run list --limit 3
```

Expected: Build / Lint / Unit Test / Integration Test 全部 SUCCESS。任一失败按日志修复后重推，直到全绿。

---

## 验收对照（对应 spec）

| spec 要求 | 落地位置 |
|---|---|
| `memory.compute` 四字段真值 | Task 2 + Task 3（含真 WS 断言） |
| `used_percent` 超集 | Task 2 实现 + Task 3 WS 断言 |
| memory.* 只在 `/tui` 注册 | Task 6（web 侧只改 memory.compute） |
| `result["error"]` → `ok=false` + `INTERNAL_ERROR` | Task 5（单测 + edit 真失败用例） |
| panic 转失败帧 | Task 5 |
| project_dir 三级回退 / mode 缺省 plan / params 不改 | Task 4 |
| status/list 真实计数端到端 | Task 7 |
| 覆盖率 ≥85% + CI 四绿 | Task 8 |
