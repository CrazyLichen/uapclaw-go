# ACP stdio 集成测试迁移 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 Python `jiuwenswarm-develop` 的 ACP stdio 测试迁移为 Go 的 `tests/integration/` 真子进程用例：出站经 `acp_chat` 工具链驱动假 ACP 对端，入站黑盒驱动真 `uapclaw acp` 二进制。

**Architecture:** 出站新增 `tests/integration/swarm/acp/`，用 `TestMain` 预编译 `testdata/fakeacp`（Go 小程序，四 mode：echo/error/permission/fsread），经 `acpchat.NewAcpChatTool(cfg).GetTools()[0].Invoke` 走真实工具链路，真实调用横跨 `swarm/acp`、`swarm/agents`、`agentcore/foundation` 三个二级子系统。入站追加 `tests/integration/cmd/acp_stdio_test.go`，父进程装配 7 件套（GatewayServer + AgentServer + MockLLM），子进程跑真 `uapclaw acp --gateway-url` 只做 AcpChannel，测试通过子进程 stdin/stdout 收发 ACP JSON-RPC。

**Tech Stack:** Go 1.22+、testify suite、`os/exec` 子进程、`encoding/json`、`cmd/integration-audit` 双层门禁。

**Spec:** `docs/superpowers/specs/2026-10-31-acp-stdio-itest-design.md`

---

## 前置约定（每个 Task 都适用）

- 编译前先 `pgrep -f 'go (build|test)'` 检查残留进程，有则 kill（项目规则 5）
- 所有注释用中文；文件内声明顺序：接口/结构体 → 枚举 → 常量 → 全局变量 → 导出函数 → 非导出函数（规则 2）
- 集成测试文件首行必须是 `//go:build integration`，且必须调用 `suite.Run(`（规则 7 第一层）
- 只提交本任务涉及的文件，工作区可能有其他会话的未跟踪文件，禁止 `git add -A`

## 关键事实（实施时不要重新推导）

| 事实 | 位置 | 含义 |
|---|---|---|
| `BuildClientSpec` 的 `args` 断言是 `[]any` | `internal/swarm/agents/harness/common/tools/acp_chat/client_cache.go:84-95` | 测试里 cfg 必须写 `[]any{mode}`，写 `[]string` 会报 `acp_agents.args must be a list of strings` |
| `AutoApprovePermissions` 调用时读 env | `internal/swarm/acp/stdio_client.go:893-897` | `t.Setenv("ACP_AUTO_APPROVE_PERMISSIONS", ...)` 可按用例切换 |
| acpchat 客户端缓存是进程级全局 map | `client_cache.go:50` 附近 `var` 块 | 每个用例用唯一 profile 名；`resetClientsForTest` 非导出，跨进程包不可调用 |
| `acpStdout` 是包 init 早捕获 | `internal/swarm/gateway/channel_manager/protocol/acp/acp_channel.go:164-166` | 子进程协议帧走真实 fd1，测试读子进程 stdout 即可 |
| `uapclaw acp` 前置 `os.Stdout = os.Stderr` | `cmd/uapclaw/acp_cmd.go:76-78` | 日志不污染协议帧 |
| `TestMain` 预编译先例 | `internal/swarm/extensions/plugin/e2e_test.go:15-38` | `exec.Command("go","build","-o",bin,"./testdata/xxx/")`，失败置空 + Skip |
| 门禁子系统口径 | `cmd/integration-audit/audit.go:537-540` | `internal/<一级>/<二级>`，`internal/common/*` 返回空串不计 |
| `tests/integration/cmd` 已在豁免名单 | `.integration-scope-exempt` | 入站黑盒复用该豁免，无需新增条目 |

---

### Task 1: 假 ACP 对端程序 v1（echo + error 两 mode）

**Files:**
- Create: `tests/integration/swarm/acp/testdata/fakeacp/main.go`

- [ ] **Step 1: 写对端程序骨架（echo/error）**

```go
// fakeacp 假 ACP 对端（集成测试用）：逐行读 JSON-RPC，按 method 回响应。
// os.Args[1] 选行为模式：echo | error | permission | fsread。
// 对应 Python tests/system_tests/test_acp_stdio_client_st.py L22-159 的 inline fake agent。
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// main 逐行处理 stdin 上的 JSON-RPC 请求。
func main() {
	mode := "echo"
	if len(os.Args) > 1 {
		mode = strings.ToLower(strings.TrimSpace(os.Args[1]))
	}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	out := bufio.NewWriter(os.Stdout)

	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		method, _ := msg["method"].(string)
		id := msg["id"]

		switch method {
		case "initialize":
			writeMsg(out, map[string]any{
				"jsonrpc": "2.0", "id": id,
				"result": map[string]any{"protocolVersion": 1},
			})
		case "session/new":
			// sessionId 带 mode，便于断言 profile→进程→会话不串台
			writeMsg(out, map[string]any{
				"jsonrpc": "2.0", "id": id,
				"result": map[string]any{"sessionId": "fake-" + mode + "-sess"},
			})
		case "session/prompt":
			handlePrompt(out, in, mode, id, promptText(msg))
		default:
			if id != nil {
				writeMsg(out, map[string]any{
					"jsonrpc": "2.0", "id": id,
					"error": map[string]any{"code": -32601, "message": "method not found: " + method},
				})
			}
		}
	}
}

// writeMsg 写一条单行 compact JSON 并 flush。
func writeMsg(w *bufio.Writer, obj map[string]any) {
	b, err := json.Marshal(obj)
	if err != nil {
		return
	}
	_, _ = w.Write(b)
	_ = w.WriteByte('\n')
	_ = w.Flush()
}

// writeUpdate 发一条 session/update（agent_message_chunk + text）。
func writeUpdate(w *bufio.Writer, mode, text string) {
	writeMsg(w, map[string]any{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params": map[string]any{
			"sessionId": "fake-" + mode + "-sess",
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"content":       map[string]any{"type": "text", "text": text},
			},
		},
	})
}

// promptText 取 params.prompt[0].text。
func promptText(msg map[string]any) string {
	params, _ := msg["params"].(map[string]any)
	prompt, _ := params["prompt"].([]any)
	if len(prompt) == 0 {
		return ""
	}
	first, _ := prompt[0].(map[string]any)
	text, _ := first["text"].(string)
	return text
}

// handlePrompt 按 mode 处理 session/prompt。
func handlePrompt(out *bufio.Writer, in *bufio.Scanner, mode string, id any, text string) {
	switch mode {
	case "error":
		writeMsg(out, map[string]any{
			"jsonrpc": "2.0", "id": id,
			"error": map[string]any{"code": -32603, "message": "agent failed"},
		})
		return
	case "echo":
		writeUpdate(out, mode, "echo:"+text)
	default:
		writeUpdate(out, mode, "mode:"+text)
	}
	writeMsg(out, map[string]any{
		"jsonrpc": "2.0", "id": id,
		"result": map[string]any{"stopReason": "end_turn"},
	})
}

// 保证 in 参数在 v1 未被使用时也参与编译签名（后续 mode 使用它读对端请求响应）。
var _ = fmt.Sprintf
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build -o /tmp/fakeacp ./tests/integration/swarm/acp/testdata/fakeacp/`
Expected: 无输出，退出码 0

- [ ] **Step 3: 手工冒烟（initialize → session/prompt）**

Run:
```bash
printf '%s\n' \
 '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
 '{"jsonrpc":"2.0","id":2,"method":"session/new","params":{}}' \
 '{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"prompt":[{"type":"text","text":"hi"}]}}' \
 | /tmp/fakeacp echo
```
Expected: 三行输出，第三行前另有 `session/update` 帧文本 `echo:hi`，末行 `{"id":3,...,"result":{"stopReason":"end_turn"}}`

- [ ] **Step 4: 清理临时产物 + 提交**

```bash
rm -f /tmp/fakeacp
git add tests/integration/swarm/acp/testdata/fakeacp/main.go
git commit -m "test(acp): 假 ACP 对端小程序（echo/error 两 mode）"
```

---

### Task 2: 出站目录骨架 + TestMain 预编译 + echo 用例

**Files:**
- Create: `tests/integration/swarm/acp/doc.go`
- Create: `tests/integration/swarm/acp/acp_chat_e2e_test.go`

- [ ] **Step 1: 写 doc.go**

```go
//go:build integration

// Package acp_test 提供 ACP stdio 出站客户端（AcpStdioClient）经 acp_chat 工具链的集成测试。
//
// 链路：config acp_agents profile → acpchat.NewAcpChatTool → tool.Invoke →
// acp.AcpStdioClient（spawn 子进程）→ testdata/fakeacp 假对端 → 聚合文本回落。
// 真实调用横跨 internal/swarm/acp、internal/swarm/agents、internal/agentcore/foundation
// 三个二级子系统（integration-audit 第一层门禁要求 ≥3）。
//
// 文件目录：
//
//	acp/
//	├── doc.go               # 包文档
//	├── acp_chat_e2e_test.go # 出站 suite（TestMain 预编译假对端 + 11 例）
//	└── testdata/fakeacp/    # 假 ACP 对端小程序（四 mode：echo/error/permission/fsread）
//
// 已知限制：acp_chat 的客户端缓存是进程级全局 map，且无导出清理 API；
// 各用例使用唯一 profile 名避免串台，残留 peer 子进程活到测试二进制退出，
// 父进程退出时 stdin 关闭 → peer EOF 自然退出（与 Python 进程级 _clients 缓存同语义）。
//
// 对应 Python 代码：tests/system_tests/test_acp_stdio_client_st.py
package acp
```

> **实施修正（2026-10-31 已完成）**：doc.go 必须是 `package acp`，不能是 `package acp_test`。
> cmd/go 对「目录内非测试文件使用 `<名字>_test` 包名」的判定会与同名外部测试包冲突，
> 报 `found packages acp (xxx_test.go) and acp_test (doc.go)`。
> 测试文件仍为 `package acp_test`（标准外部测试包）。

- [ ] **Step 2: 写 suite 骨架 + TestMain + echo 用例（先让它失败）**

```go
//go:build integration

package acp_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	acpchat "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools/acp_chat"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// fakeAcpBin TestMain 预编译的假对端二进制路径（空 = 无 go 工具链，用例 Skip）
var fakeAcpBin string

// ──────────────────────────── 导出函数 ────────────────────────────

// TestMain 预编译 testdata/fakeacp（对齐 internal/swarm/extensions/plugin/e2e_test.go 先例）
func TestMain(m *testing.M) {
	tmp, err := os.MkdirTemp("", "uapclaw-acp-itest-*")
	if err != nil {
		os.Exit(1)
	}
	bin := filepath.Join(tmp, "fakeacp")
	build := exec.Command("go", "build", "-o", bin, "./testdata/fakeacp/")
	build.Env = append(os.Environ(), "GO111MODULE=on")
	if _, err := build.CombinedOutput(); err == nil {
		fakeAcpBin = bin
	}
	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}

// TestAcpChatToolSuite 运行出站 suite
func TestAcpChatToolSuite(t *testing.T) {
	suite.Run(t, new(AcpChatToolSuite))
}

// ──────────────────────────── 结构体 ────────────────────────────

// AcpChatToolSuite 出站 acp_chat 工具链套件
type AcpChatToolSuite struct {
	isuite.BaseIntegrationSuite
	// root 对端子进程工作目录（fs 读写用例的 cwd 边界）
	root string
}

// SetupSuite 预检假对端二进制 + 准备 cwd 根
func (s *AcpChatToolSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
	if fakeAcpBin == "" {
		s.T().Skip("go 工具链不可用，无法编译 testdata/fakeacp")
	}
	s.root = s.T().TempDir()
}

// TestAcpChatTool_Echo文本聚合 session/update 文本聚合（对应 Python echo 模式）
func (s *AcpChatToolSuite) TestAcpChatTool_Echo文本聚合() {
	cfg, profile := s.newConfig("echo", nil)
	s.Equal("echo:hello", s.invoke(cfg, profile, "hello", false))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newConfig 构造唯一 profile 的 cfg（profile 名含纳秒时间戳，规避全局缓存串台）
func (s *AcpChatToolSuite) newConfig(mode string, extra map[string]any) (map[string]any, string) {
	profile := fmt.Sprintf("it_%s_%d", mode, time.Now().UnixNano())
	spec := map[string]any{
		"command": fakeAcpBin,
		// 注意：BuildClientSpec 要求 []any（写 []string 会被判为非法 args）
		"args": []any{mode},
		"cwd":  s.root,
	}
	for k, v := range extra {
		spec[k] = v
	}
	cfg := map[string]any{"acp_agents": map[string]any{profile: spec}}
	return cfg, profile
}

// invoke 经工具卡片执行一次（真实调用 tool.ValidateToolCard / tool.WithTimeout）
func (s *AcpChatToolSuite) invoke(cfg map[string]any, profile, message string, newSession bool) string {
	tools := acpchat.NewAcpChatTool(cfg).GetTools()
	s.Require().Len(tools, 1, "acp_chat 应只注册一个工具")
	s.Require().NoError(tool.ValidateToolCard(tools[0].Card()))

	inputs := map[string]any{"agent": profile, "message": message}
	if newSession {
		inputs["new_session"] = true
	}
	ctx, cancel := context.WithTimeout(s.Ctx, 30*time.Second)
	defer cancel()
	res, err := tools[0].Invoke(ctx, inputs, tool.WithTimeout(30))
	s.Require().NoError(err, "工具不应向调用方抛错")
	out, ok := res["result"].(string)
	s.Require().True(ok, "result 应为字符串，实际 %T", res["result"])
	return out
}
```

- [ ] **Step 3: 运行，确认先失败再通过**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -run TestAcpChatToolSuite ./tests/integration/swarm/acp/ -v`
Expected: 第一次若 `fakeacp` 尚未有 echo 分支会 FAIL；本任务是全新建目录，此处应直接 PASS（`TestAcpChatTool_Echo文本聚合` 输出 `echo:hello` 校验通过）

- [ ] **Step 4: 提交**

```bash
git add tests/integration/swarm/acp/doc.go tests/integration/swarm/acp/acp_chat_e2e_test.go
git commit -m "test(acp): 出站 suite 骨架 + TestMain 预编译假对端 + echo 用例"
```

---

### Task 3: error 模式 —— JSON-RPC 错误转文本

**Files:**
- Modify: `tests/integration/swarm/acp/acp_chat_e2e_test.go`

- [ ] **Step 1: 加用例（先失败）**

```go
// TestAcpChatTool_JSONRPC错误转文本 JSON-RPC error 转 [ERROR] 文本且不抛错（Python error 模式）
func (s *AcpChatToolSuite) TestAcpChatTool_JSONRPC错误转文本() {
	cfg, profile := s.newConfig("error", nil)
	out := s.invoke(cfg, profile, "trigger", false)
	s.Contains(out, "[ERROR] acp_chat failed")
	s.Contains(out, "-32603")
	s.Contains(out, "agent failed")
}
```

- [ ] **Step 2: 运行验证通过**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -run 'TestAcpChatToolSuite/TestAcpChatTool_JSONRPC错误转文本' ./tests/integration/swarm/acp/ -v`
Expected: PASS（对端回 error 帧 → 客户端转 error → 工具兜底成 `[ERROR] acp_chat failed: ACP JSON-RPC error: -32603 agent failed`）

- [ ] **Step 3: 提交**

```bash
git add tests/integration/swarm/acp/acp_chat_e2e_test.go
git commit -m "test(acp): JSON-RPC 错误转 [ERROR] 文本用例"
```

---

### Task 4: permission 模式 —— 自动批准与取消两分支

**Files:**
- Modify: `tests/integration/swarm/acp/testdata/fakeacp/main.go`
- Modify: `tests/integration/swarm/acp/acp_chat_e2e_test.go`

- [ ] **Step 1: 假对端加 permission 分支**

在 `handlePrompt` 的 `switch mode` 前插入 permission 处理，并新增两个辅助函数：

```go
// handlePrompt 按 mode 处理 session/prompt。
func handlePrompt(out *bufio.Writer, in *bufio.Scanner, mode string, id any, text string) {
	if mode == "permission" {
		// Go 增强：把收到的 outcome/optionId 回显进文本，才能区分批准/取消
		outcome, optionID := requestPermission(out, in, mode)
		writeUpdate(out, mode, "after-perm:"+text+":"+outcome+":"+optionID)
		writeMsg(out, map[string]any{
			"jsonrpc": "2.0", "id": id,
			"result": map[string]any{"stopReason": "end_turn"},
		})
		return
	}

	switch mode {
	case "error":
		writeMsg(out, map[string]any{
			"jsonrpc": "2.0", "id": id,
			"error": map[string]any{"code": -32603, "message": "agent failed"},
		})
		return
	case "echo":
		writeUpdate(out, mode, "echo:"+text)
	default:
		writeUpdate(out, mode, "mode:"+text)
	}
	writeMsg(out, map[string]any{
		"jsonrpc": "2.0", "id": id,
		"result": map[string]any{"stopReason": "end_turn"},
	})
}

// requestPermission 发 session/request_permission 并等客户端响应，返回 outcome 与 optionId。
func requestPermission(out *bufio.Writer, in *bufio.Scanner, mode string) (string, string) {
	const permID = 900
	writeMsg(out, map[string]any{
		"jsonrpc": "2.0", "id": permID, "method": "session/request_permission",
		"params": map[string]any{
			"sessionId": "fake-" + mode + "-sess",
			"toolCall":  map[string]any{"toolCallId": "tc-1", "title": "run test"},
			"options": []any{
				map[string]any{"optionId": "allow-once", "kind": "allow"},
				map[string]any{"optionId": "reject-once", "kind": "reject"},
			},
		},
	})
	reply := waitResponse(in, permID)
	if reply == nil {
		return "no-reply", ""
	}
	if errObj, ok := reply["error"]; ok {
		return "error-reply", fmt.Sprintf("%v", errObj)
	}
	result, _ := reply["result"].(map[string]any)
	outcome, _ := result["outcome"].(map[string]any)
	kind, _ := outcome["outcome"].(string)
	optionID, _ := outcome["optionId"].(string)
	return kind, optionID
}

// waitResponse 读 stdin 直到拿到指定 id 的响应（跳过通知/其他请求）。
func waitResponse(in *bufio.Scanner, wantID int) map[string]any {
	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if fmt.Sprintf("%v", msg["id"]) == fmt.Sprintf("%d", wantID) {
			return msg
		}
	}
	return nil
}
```

同时删除 Task 1 结尾的 `var _ = fmt.Sprintf` 占位（`fmt` 已被真实使用）。

- [ ] **Step 2: 加两个用例**

```go
// TestAcpChatTool_权限自动批准 自动应答 allow-once（Python permission 模式 + env 开关）
func (s *AcpChatToolSuite) TestAcpChatTool_权限自动批准() {
	s.T().Setenv("ACP_AUTO_APPROVE_PERMISSIONS", "true")
	cfg, profile := s.newConfig("permission", nil)
	s.Equal("after-perm:need perm:selected:allow-once", s.invoke(cfg, profile, "need perm", false))
}

// TestAcpChatTool_权限取消 自动应答 cancelled（Go 增强：Python 只测了批准分支）
func (s *AcpChatToolSuite) TestAcpChatTool_权限取消() {
	s.T().Setenv("ACP_AUTO_APPROVE_PERMISSIONS", "false")
	cfg, profile := s.newConfig("permission", nil)
	s.Contains(s.invoke(cfg, profile, "need perm", false), "after-perm:need perm:cancelled:")
}
```

- [ ] **Step 3: 运行两个用例**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -run 'TestAcpChatToolSuite/TestAcpChatTool_权限' ./tests/integration/swarm/acp/ -v`
Expected: 两例 PASS

- [ ] **Step 4: 提交**

```bash
git add tests/integration/swarm/acp/testdata/fakeacp/main.go tests/integration/swarm/acp/acp_chat_e2e_test.go
git commit -m "test(acp): permission 模式对端 + 批准/取消两分支用例"
```

---

### Task 5: fsread 模式 —— 对端 fs 读写穿透

**Files:**
- Modify: `tests/integration/swarm/acp/testdata/fakeacp/main.go`
- Modify: `tests/integration/swarm/acp/acp_chat_e2e_test.go`

- [ ] **Step 1: 假对端加 fsread 分支**

`handlePrompt` 顶部（permission 分支之前）加：

```go
	if mode == "fsread" {
		content, errText := fsReadThenWrite(out, in, mode)
		writeUpdate(out, mode, content+errText)
		writeMsg(out, map[string]any{
			"jsonrpc": "2.0", "id": id,
			"result": map[string]any{"stopReason": "end_turn"},
		})
		return
	}
```

新增函数：

```go
// fsReadThenWrite 发 fs/read_text_file（line/limit 切片）+ fs/write_text_file，返回读到的内容。
func fsReadThenWrite(out *bufio.Writer, in *bufio.Scanner, mode string) (string, string) {
	writeMsg(out, map[string]any{
		"jsonrpc": "2.0", "id": 901, "method": "fs/read_text_file",
		"params": map[string]any{"path": "in_root.txt", "line": 1, "limit": 2},
	})
	reply := waitResponse(in, 901)
	if reply == nil || reply["error"] != nil {
		return "", "|read-failed"
	}
	result, _ := reply["result"].(map[string]any)
	content, _ := result["content"].(string)

	writeMsg(out, map[string]any{
		"jsonrpc": "2.0", "id": 902, "method": "fs/write_text_file",
		"params": map[string]any{"path": "written.txt", "content": "hello-acp"},
	})
	_ = waitResponse(in, 902)
	return content, ""
}
```

- [ ] **Step 2: 加用例**

```go
// TestAcpChatTool_fs读写穿透 对端 fs/read（line/limit 切片）与 fs/write 均生效（Python fs_read 模式）
func (s *AcpChatToolSuite) TestAcpChatTool_fs读写穿透() {
	root := s.T().TempDir()
	s.Require().NoError(os.WriteFile(filepath.Join(root, "in_root.txt"),
		[]byte("line1\nline2\nline3\n"), 0o644))

	profile := fmt.Sprintf("it_fsread_%d", time.Now().UnixNano())
	cfg := map[string]any{"acp_agents": map[string]any{
		profile: map[string]any{
			"command": fakeAcpBin,
			"args":    []any{"fsread"},
			"cwd":     root,
		},
	}}

	out := s.invoke(cfg, profile, "read file", false)
	// line=1 limit=2 → 只返回前两行
	s.Equal("line1\nline2\n", out)

	written, err := os.ReadFile(filepath.Join(root, "written.txt"))
	s.Require().NoError(err, "对端 fs/write_text_file 应落盘")
	s.Equal("hello-acp", string(written))
}
```

- [ ] **Step 3: 运行**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -run 'TestAcpChatToolSuite/TestAcpChatTool_fs读写穿透' ./tests/integration/swarm/acp/ -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add tests/integration/swarm/acp/testdata/fakeacp/main.go tests/integration/swarm/acp/acp_chat_e2e_test.go
git commit -m "test(acp): fsread 模式对端 + fs 读写穿透用例"
```

---

### Task 6: profile 校验三例（缺失 / 无 command / args 非列表）

**Files:**
- Modify: `tests/integration/swarm/acp/acp_chat_e2e_test.go`

- [ ] **Step 1: 加三个用例**

```go
// TestAcpChatTool_Profile缺失 未知 profile 转 [ERROR] 文本（Python _get_spec 返回 None）
func (s *AcpChatToolSuite) TestAcpChatTool_Profile缺失() {
	cfg := map[string]any{"acp_agents": map[string]any{}}
	out := s.invoke(cfg, "no_such_profile", "hi", false)
	s.Contains(out, "[ERROR] acp_chat failed")
	s.Contains(out, "unknown acp_agents profile")
}

// TestAcpChatTool_Profile无command command 为空转 [ERROR] 文本（Python _run_once needs command）
func (s *AcpChatToolSuite) TestAcpChatTool_Profile无command() {
	profile := fmt.Sprintf("it_nocmd_%d", time.Now().UnixNano())
	cfg := map[string]any{"acp_agents": map[string]any{
		profile: map[string]any{"command": "   ", "args": []any{"echo"}},
	}}
	out := s.invoke(cfg, profile, "hi", false)
	s.Contains(out, "[ERROR] acp_chat failed")
	s.Contains(out, "needs non-empty command")
}

// TestAcpChatTool_args非列表 args 非列表转 [ERROR] 文本（Python args 类型校验，Go 走真链路）
func (s *AcpChatToolSuite) TestAcpChatTool_args非列表() {
	profile := fmt.Sprintf("it_badargs_%d", time.Now().UnixNano())
	cfg := map[string]any{"acp_agents": map[string]any{
		profile: map[string]any{"command": fakeAcpBin, "args": "echo"},
	}}
	out := s.invoke(cfg, profile, "hi", false)
	s.Contains(out, "[ERROR] acp_chat failed")
	s.Contains(out, "args must be a list of strings")
}
```

- [ ] **Step 2: 运行**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -run 'TestAcpChatToolSuite/TestAcpChatTool_Profile|TestAcpChatToolSuite/TestAcpChatTool_args非列表' ./tests/integration/swarm/acp/ -v`
Expected: 三例 PASS

- [ ] **Step 3: 提交**

```bash
git add tests/integration/swarm/acp/acp_chat_e2e_test.go
git commit -m "test(acp): profile 缺失/无 command/args 非列表三例"
```

---

### Task 7: 缓存复用与 new_session 重启（pid 回显）

**Files:**
- Modify: `tests/integration/swarm/acp/testdata/fakeacp/main.go`
- Modify: `tests/integration/swarm/acp/acp_chat_e2e_test.go`

- [ ] **Step 1: 假对端在文本里回显 pid**

`handlePrompt` 的 `echo` 分支改为：

```go
	case "echo":
		// 回显 pid：供“缓存复用 / new_session 重启”用例区分是否同一子进程
		writeUpdate(out, mode, fmt.Sprintf("echo:%s:pid=%d", text, os.Getpid()))
```

- [ ] **Step 2: 同步修正 Task 2 的 echo 断言**

```go
// TestAcpChatTool_Echo文本聚合 session/update 文本聚合（对应 Python echo 模式）
func (s *AcpChatToolSuite) TestAcpChatTool_Echo文本聚合() {
	cfg, profile := s.newConfig("echo", nil)
	out := s.invoke(cfg, profile, "hello", false)
	s.Regexp(`^echo:hello:pid=\d+$`, out)
}
```

- [ ] **Step 3: 加两个新用例**

```go
// TestAcpChatTool_会话缓存复用 同 profile 连续两次调用复用同一子进程（Go 增强）
func (s *AcpChatToolSuite) TestAcpChatTool_会话缓存复用() {
	cfg, profile := s.newConfig("echo", nil)
	first := pidOf(s.T(), s.invoke(cfg, profile, "one", false))
	second := pidOf(s.T(), s.invoke(cfg, profile, "two", false))
	s.Equal(first, second, "同 profile 第二次应复用缓存子进程")
}

// TestAcpChatTool_newSession重启 new_session=true 重启子进程（Go 增强）
func (s *AcpChatToolSuite) TestAcpChatTool_newSession重启() {
	cfg, profile := s.newConfig("echo", nil)
	first := pidOf(s.T(), s.invoke(cfg, profile, "one", false))
	second := pidOf(s.T(), s.invoke(cfg, profile, "one", true))
	s.NotEqual(first, second, "new_session=true 应拉起新子进程")
}

// pidOf 从 "echo:<text>:pid=<n>" 中取出 pid
func pidOf(t *testing.T, out string) string {
	t.Helper()
	idx := strings.LastIndex(out, "pid=")
	if idx < 0 {
		t.Fatalf("输出缺少 pid 标记: %q", out)
	}
	return out[idx+len("pid="):]
}
```

同步在执行文件的 import 追加 `"strings"`。

- [ ] **Step 4: 运行本任务与 echo 用例**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -run 'TestAcpChatToolSuite/TestAcpChatTool_(Echo|会话缓存复用|newSession重启)' ./tests/integration/swarm/acp/ -v`
Expected: 三例 PASS

- [ ] **Step 5: 提交**

```bash
git add tests/integration/swarm/acp/testdata/fakeacp/main.go tests/integration/swarm/acp/acp_chat_e2e_test.go
git commit -m "test(acp): 缓存复用与 new_session 重启用例（pid 回显）"
```

---

### Task 8: 直连基线用例（补齐门禁第三子系统）

**Files:**
- Modify: `tests/integration/swarm/acp/acp_chat_e2e_test.go`

- [ ] **Step 1: 加直连用例**

```go
// TestAcpStdioClient_直连基线 不用工具层，直接用客户端走通 connect→chat→close
// 对应 Python test_stdio_client_connect_and_chat_with_fake_agent；
// 同时为 integration-audit 提供 internal/swarm/acp 的真实 pkg.Fn 调用。
func (s *AcpChatToolSuite) TestAcpStdioClient_直连基线() {
	c := acp.NewAcpStdioClient(fakeAcpBin, []string{"echo"}, s.root, nil)
	s.Require().NoError(c.Connect(s.Ctx))
	defer func() { s.Require().NoError(c.Close()) }()

	s.Equal("fake-echo-sess", c.SessionID())
	out, err := c.Chat(s.Ctx, "hello", acp.WithChatTimeout(30*time.Second))
	s.Require().NoError(err)
	s.Regexp(`^echo:hello:pid=\d+$`, out)
}
```

import 追加 `"github.com/uapclaw/uapclaw-go/internal/swarm/acp"`。

- [ ] **Step 2: 运行**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -run 'TestAcpChatToolSuite/TestAcpStdioClient_直连基线' ./tests/integration/swarm/acp/ -v`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add tests/integration/swarm/acp/acp_chat_e2e_test.go
git commit -m "test(acp): 直连客户端基线用例（补齐门禁第三子系统真实调用）"
```

---

### Task 9: 出站目录门禁自检

**Files:** 无变更

- [ ] **Step 1: 跑审计**

Run: `cd /home/opensource/uap-claw-go && go run ./cmd/integration-audit -verify 2>&1 | tail -40`
Expected: 输出中 `tests/integration/swarm/acp` 出现在通过列表，不出现 PendingCoverage / 不通过

- [ ] **Step 2: 若被列为灰区**

Run 单目录覆盖：
```bash
go test -tags=integration -coverprofile=/tmp/acp_acp.out ./tests/integration/swarm/acp/
go tool cover -func=/tmp/acp_acp.out | tail -3
```
Expected: 覆盖能产生且审计第二层要求“≥3 子系统各 ≥30 行”时该目录由真实调用跨度满足，不应进入第二层

- [ ] **Step 3: 提交（仅当审计输出需要登记豁免时）**

若审计仍判不通过，在 `.integration-scope-exempt` 末尾追加并注明理由：

```
# ACP 出站工具链：真实调用横跨 swarm/acp + swarm/agents + agentcore/foundation，
# 但子进程 peer 行为断言占主导，按“真跨边界（进程）”归类
tests/integration/swarm/acp
```

```bash
git add .integration-scope-exempt
git commit -m "chore(audit): 登记 ACP 出站集成目录（真跨进程边界）"
```

---

### Task 10: 入站骨架 —— 父进程 7 件套 + 二进制构建 + 子进程 spawn

**Files:**
- Create: `tests/integration/cmd/acp_stdio_test.go`

- [ ] **Step 1: 写装配与 helper（含一个探测用例打印首帧）**

```go
//go:build integration

package cmd_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/suite"

	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager"
	mh "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/message_handler"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AcpStdioBinarySuite 入站黑盒套件：父进程托管网关，子进程跑真 uapclaw acp
type AcpStdioBinarySuite struct {
	isuite.BaseIntegrationSuite
	inboundServer *gateway.InboundGatewayServer
	gwServer      *gateway.GatewayServer
	channelMgr    *channel_manager.ChannelManager
	msgHandler    *mh.MessageHandler
	agentClient   *routing.AgentClient
	chTransport   *transport.ChannelTransport
	agentServer   *server.AgentServer
	gwPort        int
	providerName  string
	uapclawBin    string
	envBackup     [][3]string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAcpStdioBinarySuite 运行入站黑盒套件
func TestAcpStdioBinarySuite(t *testing.T) {
	suite.Run(t, new(AcpStdioBinarySuite))
}

// SetupSuite 装配 7 件套 + 构建 uapclaw 二进制
func (s *AcpStdioBinarySuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()

	s.providerName = fmt.Sprintf("mock_acp_stdio_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(s.providerName, "llm", s.MockLLM.Factory())
	s.saveAndSetenv("UAPCLAW_DATA_DIR", s.setupDataDir())
	pathutil.ResetCache()

	s.chTransport = transport.NewChannelTransport()
	cfg, err := config.New("")
	s.Require().NoError(err)
	s.agentServer = server.NewAgentServer(cfg, s.chTransport)
	s.agentClient = routing.NewAgentClient(s.chTransport)
	s.msgHandler = mh.NewMessageHandler(s.agentClient)
	s.channelMgr = channel_manager.NewChannelManager(s.msgHandler, nil, nil)
	s.inboundServer = gateway.NewInboundGatewayServer(func(ctx context.Context, msg *schema.Message) {
		gateway.NormalizeAndForwardMessage(msg, s.channelMgr)
	})

	s.gwPort = pickFreePort(s.T())
	gwCfg := gateway.NewGatewayServerConfig()
	gwCfg.Host = "127.0.0.1"
	gwCfg.Port = s.gwPort
	gwCfg.Routes = gateway.BuildRouteConfigMap([]*routing.RouteBinding{
		gateway.BuildACPRouteBinding("/acp", "acp"),
	})
	s.gwServer = gateway.NewGatewayServer(gwCfg)

	acpBridge := gateway.NewAcpGatewayBridge(
		func(ctx context.Context, msg *schema.Message) error {
			if s.gwServer == nil {
				return nil
			}
			return s.inboundServer.HandleMessage(ctx, msg)
		},
		gateway.WithBindSessionClient(func(sessionID string, ws *websocket.Conn) {
			s.gwServer.BindAcpSessionClientForTest(sessionID, ws)
		}))
	s.gwServer.SetAcpBridge(acpBridge)
	s.gwServer.InstallDefaultAcpHooks()
	s.channelMgr.RegisterExternalChannel("acp", s.gwServer)
	s.gwServer.OnMessage(s.inboundServer.HandleMessage)

	s.Require().NoError(s.agentServer.Start(s.Ctx))
	s.Require().NoError(s.agentClient.Connect(s.Ctx))
	s.Require().NoError(s.msgHandler.StartForwarding(s.Ctx))
	s.Require().NoError(s.channelMgr.StartDispatch(s.Ctx))
	s.Require().NoError(s.inboundServer.Start(s.Ctx))
	s.Require().NoError(s.gwServer.Start(s.Ctx))

	// 构建 uapclaw 二进制
	bin := filepath.Join(s.T().TempDir(), "uapclaw")
	build := exec.Command("go", "build", "-o", bin, "./cmd/uapclaw/")
	if out, err := build.CombinedOutput(); err != nil {
		s.T().Skipf("构建 uapclaw 失败，跳过: %v\n%s", err, out)
	}
	s.uapclawBin = bin
}

// TearDownSuite 逆序停止 + 清理
func (s *AcpStdioBinarySuite) TearDownSuite() {
	if s.gwServer != nil {
		_ = s.gwServer.Stop()
	}
	if s.inboundServer != nil {
		s.inboundServer.Stop()
	}
	if s.channelMgr != nil {
		_ = s.channelMgr.StopDispatch()
	}
	if s.msgHandler != nil {
		_ = s.msgHandler.StopForwarding()
	}
	if s.agentClient != nil {
		s.agentClient.Disconnect()
	}
	if s.agentServer != nil {
		_ = s.agentServer.Stop()
	}
	if s.providerName != "" {
		_ = model_clients.GetClientRegistry().Unregister(s.providerName, "llm")
	}
	pathutil.ResetCache()
	s.restoreEnv()
	s.BaseIntegrationSuite.TearDownSuite()
}

// TestAcpStdioBinary_探测首帧 打印子进程首帧，确认协议通道与桩日志分离
func (s *AcpStdioBinarySuite) TestAcpStdioBinary_探测首帧() {
	child := s.startChild("")
	defer child.kill()

	child.writeLine(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	frame := child.waitFrame(10*time.Second, func(f map[string]any) bool { return f["id"] == float64(1) })
	s.T().Logf("首帧: %v", frame)
	s.NotNil(frame["result"])
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// childProc 子进程句柄与帧读取器
type childProc struct {
	cmd    *exec.Cmd
	stdin  *bufio.Writer
	stdinW io.WriteCloser
	frames chan map[string]any
	stderr *strings.Builder
}

// startChild 启动 uapclaw acp 子进程（gatewayURL 为空表示用父进程端口）
func (s *AcpStdioBinarySuite) startChild(gatewayURL string) *childProc {
	if gatewayURL == "" {
		gatewayURL = fmt.Sprintf("ws://127.0.0.1:%d/acp", s.gwPort)
	}
	child := &childProc{stderr: &strings.Builder{}}
	child.cmd = exec.Command(s.uapclawBin, "acp", "--gateway-url", gatewayURL)
	// 子进程用独立 data dir（不与父进程共享 config，避免竞争）
	child.cmd.Env = append(os.Environ(), "UAPCLAW_DATA_DIR="+s.setupDataDir())
	child.cmd.Stderr = child.stderr

	stdin, err := child.cmd.StdinPipe()
	s.Require().NoError(err)
	stdout, err := child.cmd.StdoutPipe()
	s.Require().NoError(err)
	s.Require().NoError(child.cmd.Start())
	child.stdinW = stdin
	child.stdin = bufio.NewWriter(stdin)
	child.frames = make(chan map[string]any, 64)

	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var frame map[string]any
			if err := json.Unmarshal([]byte(line), &frame); err != nil {
				child.stderr.WriteString("[non-json stdout] " + line + "\n")
				continue
			}
			child.frames <- frame
		}
		close(child.frames)
	}()
	return child
}

// writeLine 写一行 JSON-RPC
func (c *childProc) writeLine(line string) {
	_, _ = c.stdin.WriteString(line + "\n")
	_ = c.stdin.Flush()
}

// waitFrame 等一帧满足条件的输出（超时返回 nil 并带上 stderr 便于诊断）
func (c *childProc) waitFrame(timeout time.Duration, pred func(map[string]any) bool) map[string]any {
	deadline := time.After(timeout)
	for {
		select {
		case f, ok := <-c.frames:
			if !ok {
				return nil
			}
			if pred(f) {
				return f
			}
		case <-deadline:
			return nil
		}
	}
}

// kill 关 stdin 并确保子进程回收
func (c *childProc) kill() {
	if c.stdin != nil {
		_ = c.stdin.Flush()
	}
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = c.cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = c.cmd.Process.Kill()
			<-done
		}
	}
}

// setupDataDir 构造 UAPCLAW_DATA_DIR 三件套
func (s *AcpStdioBinarySuite) setupDataDir() string {
	dataDir := s.T().TempDir()
	configDir := filepath.Join(dataDir, "config")
	s.Require().NoError(os.MkdirAll(configDir, 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(dataDir, "agent", "workspace"), 0o755))
	yamlContent := fmt.Sprintf(`server:
  agentserver:
    host: "127.0.0.1"
    port: 0
  gateway:
    host: "127.0.0.1"
    port: %d
models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: %s
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        client_id: %s
      is_default: true
`, s.gwPort, s.providerName, s.providerName)
	s.Require().NoError(os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(yamlContent), 0o644))
	return dataDir
}

// pickFreePort 取随机可用端口
func pickFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听随机端口失败: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// saveAndSetenv 记录原值并设置
func (s *AcpStdioBinarySuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	s.envBackup = append(s.envBackup, [3]string{key, orig, fmt.Sprintf("%t", had)})
	_ = os.Setenv(key, val)
}

// restoreEnv 恢复环境变量
func (s *AcpStdioBinarySuite) restoreEnv() {
	for _, e := range s.envBackup {
		if e[2] == "false" {
			_ = os.Unsetenv(e[0])
		} else {
			_ = os.Setenv(e[0], e[1])
		}
	}
}
```

- [ ] **Step 2: 运行探测用例**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -run 'TestAcpStdioBinarySuite/TestAcpStdioBinary_探测首帧' ./tests/integration/cmd/ -v`
Expected: PASS，日志打印首帧 `map[id:1 result:map[protocolVersion:1]]`

若 FAIL：先看 `stderr`（在 `waitFrame` 返回 nil 时把 `child.stderr.String()` 打到 `s.T().Logf`），按提示补 config 段。常见原因：`bootstrapGatewayEnv` 要求 workspace 目录或 config 必需字段缺失。

- [ ] **Step 3: 提交**

```bash
git add tests/integration/cmd/acp_stdio_test.go
git commit -m "test(acp): 入站黑盒套件骨架（7 件套 + uapclaw 二进制子进程）"
```

---

### Task 11: 入站 PromptRoundTrip 用例

**Files:**
- Modify: `tests/integration/cmd/acp_stdio_test.go`

- [ ] **Step 1: 加用例**

```go
// TestAcpStdioBinary_PromptRoundTrip initialize→session/new→session/prompt 全链路往返
// 对应 Python test_acp_cli_returns_final_result（忽略 chunk、取 final result）
func (s *AcpStdioBinarySuite) TestAcpStdioBinary_PromptRoundTrip() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("acp stdio binary reply"))

	child := s.startChild("")
	defer child.kill()

	child.writeLine(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	init := child.waitFrame(10*time.Second, func(f map[string]any) bool { return f["id"] == float64(1) })
	s.Require().NotNil(init, "initialize 应有响应；stderr=%s", child.stderr.String())

	child.writeLine(`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"sessionId":"ide-stdio-1"}}`)
	newFrame := child.waitFrame(10*time.Second, func(f map[string]any) bool { return f["id"] == float64(2) })
	s.Require().NotNil(newFrame, "session/new 应有响应；stderr=%s", child.stderr.String())

	child.writeLine(`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"ide-stdio-1","prompt":[{"type":"text","text":"hello binary"}]}}`)

	// 会话帧：session/update 且文本含 MockLLM 固定回复
	update := child.waitFrame(30*time.Second, func(f map[string]any) bool {
		if f["method"] != "session/update" {
			return false
		}
		return strings.Contains(fmt.Sprintf("%v", f), "acp stdio binary reply")
	})
	s.Require().NotNil(update, "应收到 agent_message_chunk；stderr=%s", child.stderr.String())

	// 终帧：result 且 stopReason=end_turn
	final := child.waitFrame(30*time.Second, func(f map[string]any) bool {
		if f["id"] != float64(3) {
			return false
		}
		result, _ := f["result"].(map[string]any)
		return result != nil && result["stopReason"] == "end_turn"
	})
	s.Require().NotNil(final, "应收到 end_turn 终帧；stderr=%s", child.stderr.String())
}
```

- [ ] **Step 2: 运行**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -run 'TestAcpStdioBinarySuite/TestAcpStdioBinary_PromptRoundTrip' ./tests/integration/cmd/ -v`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add tests/integration/cmd/acp_stdio_test.go
git commit -m "test(acp): 入站二进制 session/prompt 全链路往返用例"
```

---

### Task 12: 入站错误终帧用例（先探测再断言）

**Files:**
- Modify: `tests/integration/cmd/acp_stdio_test.go`

- [ ] **Step 1: 先写探测用例，打印错误路径的真实帧**

```go
// TestAcpStdioBinary_错误终帧 上游失败时子进程输出 error 帧（先探测真实形态）
// 对应 Python test_acp_cli_returns_final_jsonrpc_error
func (s *AcpStdioBinarySuite) TestAcpStdioBinary_错误终帧() {
	s.MockLLM.SetResponses(mockllm.CreateErrorResponse(fmt.Errorf("upstream boom")))

	child := s.startChild("")
	defer child.kill()

	child.writeLine(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	s.Require().NotNil(child.waitFrame(10*time.Second, func(f map[string]any) bool { return f["id"] == float64(1) }))

	child.writeLine(`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"sessionId":"ide-err-1"}}`)
	s.Require().NotNil(child.waitFrame(10*time.Second, func(f map[string]any) bool { return f["id"] == float64(2) }))

	child.writeLine(`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"ide-err-1","prompt":[{"type":"text","text":"boom"}]}}`)

	// 收集 id=3 的终帧（result 或 error 都收），先打印真实形态
	terminal := child.waitFrame(30*time.Second, func(f map[string]any) bool {
		if f["id"] != float64(3) {
			return false
		}
		return f["result"] != nil || f["error"] != nil
	})
	s.Require().NotNil(terminal, "应有终帧；stderr=%s", child.stderr.String())
	s.T().Logf("错误路径终帧: %v", terminal)
}
```

- [ ] **Step 2: 运行并读取真实形态**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -run 'TestAcpStdioBinarySuite/TestAcpStdioBinary_错误终帧' ./tests/integration/cmd/ -v`
Expected: PASS，日志打印终帧内容

- [ ] **Step 3: 按探测结果收紧断言**

若终帧含 `error` 字段（与 Python 一致）：

```go
	errObj, ok := terminal["error"].(map[string]any)
	s.Require().True(ok, "终帧应为 error，实际 %v", terminal)
	s.NotNil(errObj["code"], "error 应带 code")
	s.NotEmpty(errObj["message"], "error 应带 message")
```

若终帧是 `result`（Go 现实现不产生 error 帧）：**先停下来向用户报告差异**，由用户决定是补实现（对齐 Python）还是接受 Go 语义并改用例断言。禁止把断言写成“两种都行”的宽松形式。

- [ ] **Step 4: 提交**

```bash
git add tests/integration/cmd/acp_stdio_test.go
git commit -m "test(acp): 入站错误终帧用例（真实形态断言）"
```

---

### Task 13: 入站 stdin EOF 优雅退出 + 远端不可达

**Files:**
- Modify: `tests/integration/cmd/acp_stdio_test.go`

- [ ] **Step 1: 加两个用例**

```go
// TestAcpStdioBinary_stdinEOF优雅退出 关 stdin 后子进程在宽限内退出码 0（Go 增强）
func (s *AcpStdioBinarySuite) TestAcpStdioBinary_stdinEOF优雅退出() {
	child := s.startChild("")
	child.writeLine(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	s.Require().NotNil(child.waitFrame(10*time.Second, func(f map[string]any) bool { return f["id"] == float64(1) }))

	// 关 stdin 写端（等价 IDE 断开）→ 子进程应在 5s 宽限内自然退出
	s.Require().NoError(child.stdin.Flush())
	s.Require().NoError(child.stdinW.Close())

	done := make(chan error, 1)
	go func() { done <- child.cmd.Wait() }()
	select {
	case err := <-done:
		s.NoError(err, "stdin EOF 后应正常退出；stderr=%s", child.stderr.String())
	case <-time.After(15 * time.Second):
		_ = child.cmd.Process.Kill()
		s.FailNow("标准输入 EOF 后 15s 未退出")
	}
}

// TestAcpStdioBinary_远端不可达 网关不可达时子进程非零退出（Go 增强）
func (s *AcpStdioBinarySuite) TestAcpStdioBinary_远端不可达() {
	child := s.startChild("ws://127.0.0.1:1/acp")
	defer child.kill()

	child.writeLine(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	// 读帧直到通道关闭（子进程退出）
	for range child.frames {
	}

	done := make(chan error, 1)
	go func() { done <- child.cmd.Wait() }()
	select {
	case err := <-done:
		s.Error(err, "网关不可达应非零退出")
		s.NotEmpty(child.stderr.String(), "stderr 应有可诊断信息")
	case <-time.After(20 * time.Second):
		_ = child.cmd.Process.Kill()
		s.FailNow("网关不可达时子进程 20s 未退出")
	}
}
```

`childProc.stdinW` 已在 Task 10 的 `startChild` 中保存，本任务直接使用。

- [ ] **Step 2: 运行**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -run 'TestAcpStdioBinarySuite/TestAcpStdioBinary_(stdinEOF|远端不可达)' ./tests/integration/cmd/ -v`
Expected: 两例 PASS

- [ ] **Step 3: 提交**

```bash
git add tests/integration/cmd/acp_stdio_test.go
git commit -m "test(acp): 入站 stdin EOF 优雅退出与网关不可达两例"
```

---

### Task 14: 入站 stdout 纯协议帧用例

**Files:**
- Modify: `tests/integration/cmd/acp_stdio_test.go`

- [ ] **Step 1: 在 childProc 记录非 JSON 行**

`startChild` 的读 goroutine 已在非 JSON 行时写入 `child.stderr`，在此基础上新增计数：

```go
// dirtyStdout 非 JSON 的 stdout 行（协议帧污染检测）
dirty int32
```

读 goroutine 的 `continue` 前改为：

```go
			child.stderr.WriteString("[non-json stdout] " + line + "\n")
			atomic.AddInt32(&child.dirty, 1)
			continue
```

- [ ] **Step 2: 加用例**

```go
// TestAcpStdioBinary_stdout纯协议帧 子进程日志全部走 stderr，stdout 每行都是合法 JSON
// 验证 os.Stdout=os.Stderr 重定向 + acpStdout 早捕获（Go 增强）
func (s *AcpStdioBinarySuite) TestAcpStdioBinary_stdout纯协议帧() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("protocol only"))

	child := s.startChild("")
	defer child.kill()

	child.writeLine(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	s.Require().NotNil(child.waitFrame(10*time.Second, func(f map[string]any) bool { return f["id"] == float64(1) }))
	child.writeLine(`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"sessionId":"ide-dirty-1"}}`)
	s.Require().NotNil(child.waitFrame(10*time.Second, func(f map[string]any) bool { return f["id"] == float64(2) }))
	child.writeLine(`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"ide-dirty-1","prompt":[{"type":"text","text":"hi"}]}}`)
	s.Require().NotNil(child.waitFrame(30*time.Second, func(f map[string]any) bool {
		if f["id"] != float64(3) {
			return false
		}
		result, _ := f["result"].(map[string]any)
		return result != nil && result["stopReason"] == "end_turn"
	}))

	s.Zero(atomic.LoadInt32(&child.dirty), "stdout 不应出现非 JSON 行；stderr=%s", child.stderr.String())
}
```

import 追加 `"sync/atomic"`。

- [ ] **Step 3: 运行**

Run: `cd /home/opensource/uap-claw-go && go test -tags=integration -run 'TestAcpStdioBinarySuite/TestAcpStdioBinary_stdout纯协议帧' ./tests/integration/cmd/ -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add tests/integration/cmd/acp_stdio_test.go
git commit -m "test(acp): 入站 stdout 纯协议帧用例（日志不污染协议流）"
```

---

### Task 15: 全量验收与文档回填

**Files:**
- Modify: `docs/superpowers/specs/2026-10-31-acp-stdio-itest-design.md`（仅在 Task 9/12 触发变更时回填）

- [ ] **Step 1: 格式与静态检查**

```bash
cd /home/opensource/uap-claw-go
gofmt -l tests/integration/swarm/acp tests/integration/cmd
go vet -tags=integration ./tests/integration/swarm/acp/... ./tests/integration/cmd/...
```
Expected: `gofmt -l` 无输出；`go vet` 无告警

- [ ] **Step 2: 跑两个目录全量用例**

Run: `go test -tags=integration ./tests/integration/swarm/acp/... ./tests/integration/cmd/... -v 2>&1 | tail -40`
Expected: 出站 11 例 + 入站 5 例（含探测用例，可保留为冒烟）全 PASS

- [ ] **Step 3: 跑门禁**

Run: `go run ./cmd/integration-audit -verify 2>&1 | tail -40`
Expected: 两个目录均通过（或 `tests/integration/swarm/acp` 按 Task 9 已登记豁免，且审计对 `tests/integration/cmd` 沿用既有豁免）

- [ ] **Step 4: 回填设计文档**

若 Task 9 登记了豁免，或 Task 12 的探测结果与 Python 不一致，在设计文档相应小节追加一段“实施回填”，写明真实结论；否则跳过本步。

- [ ] **Step 5: 提交**

```bash
git add docs/superpowers/specs/2026-10-31-acp-stdio-itest-design.md
git commit -m "docs(specs): ACP stdio 集成测试实施回填"
```

- [ ] **Step 6: 推远端并盯 CI（项目规则 6）**

```bash
git push origin HEAD
gh run list --limit 3
```
Expected: Build / Lint / Unit Test / Integration Test 四 job 全 SUCCESS；任一红灯回到对应 Task 修复后重推

---

## 自查记录（写完计划后核对）

| 检查项 | 结果 |
|---|---|
| spec §5.1 文件清单 | Task 1/2/10 全覆盖 ✓ |
| spec §5.2 假对端四 mode | Task 1（echo/error）、Task 4（permission）、Task 5（fsread）、Task 7（pid 回显）✓ |
| spec §5.3 出站装配（TestMain + 唯一 profile + 工具 Invoke） | Task 2 ✓ |
| spec §5.4 入站装配（7 件套 + 二进制 + mode-2） | Task 10 ✓ |
| spec §5.5 门禁策略 | Task 9（出站自检 + 兜底登记）、Task 10 目录选 cmd 豁免 ✓ |
| spec §6.1 出站 11 例 | Task 2/3/4/5/6/7/8 = 1+1+2+1+3+2+1 = 11 ✓ |
| spec §6.2 入站 5 例 | Task 10（探测）+ 11/12/13(2 例)/14 = 5 正式例 + 1 探测 ✓ |
| spec §7 稳定性（回收/超时/无并行/跨平台） | Task 10 `kill()` 三段回收、`waitFrame` deadline、无 `t.Parallel` ✓ |
| spec §8 验收 4 项 | Task 15 ✓ |
| spec R1（入站错误帧未知） | Task 12 先探测再收紧，禁止宽松断言 ✓ |
| spec R3（父子 data dir 隔离） | Task 10 `startChild` 用独立 `setupDataDir()` ✓ |
| 类型一致性 | `childProc.frames chan map[string]any`、`waitFrame(timeout, pred)`、`newConfig(mode, extra) (cfg, profile)`、`invoke(cfg, profile, message, newSession) string` 各 Task 一致 ✓ |
| 占位符扫描 | 无 TBD/TODO；Task 12 Step 3 给出两条明确分支而非“待补” ✓ |
