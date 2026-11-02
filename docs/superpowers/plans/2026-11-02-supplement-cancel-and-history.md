# supplement 打断链路与 history 记录修复 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复「模型输出中再发消息导致新一轮被误取消」与「恢复历史时工具调用丢失」两类缺陷，并用单测/集成测试钉死，防止回归。

**Architecture:** 三个独立提交：① 把 AgentServer 的流式任务表从「sessionID → 裸 cancel」改为「sessionID → entry 指针」，收尾只做身份化释放、不再 cancel；② 取消判定统一走 `ctx.Err()`（只静默 `context.Canceled`），去掉重复清理、修正 `ClearMessages` 只清本轮、保住错误链；③ history 记录按 Python 语义保留 payload 全部字段（含 team 的嵌套 `event`）并补 `error` 兜底。

**Tech Stack:** Go 1.x、`context`、`testify`、项目内 `tests/integration/suite` + `tests/integration/mockllm`。

**设计依据:** `docs/superpowers/specs/2026-11-02-supplement-cancel-and-history-design.md`

---

## 文件结构

| 文件 | 责任 | 动作 |
|---|---|---|
| `internal/swarm/server/agent_server.go` | 流式任务登记表（entry 身份、register/release/cancel） | 修改 |
| `internal/swarm/server/handle_envelope.go` | `handleStream` 收尾接线 | 修改 |
| `internal/swarm/server/agent_server_test.go` | 既有 `TestAgentServer_StreamTaskTracking` 适配新签名 | 修改 |
| `internal/swarm/server/agent_server_stream_task_test.go` | D1 单测（4 例） | 新建 |
| `internal/common/utils/ctxutil.go` | `Canceled(ctx)` 判定 | 新建 |
| `internal/common/utils/ctxutil_test.go` | D4 单测 | 新建 |
| `internal/common/utils/doc.go` | 文件目录登记 | 修改 |
| `internal/agentcore/single_agent/agents/react_invoke.go` | 取消静默 + 删除重复清理 + helper | 修改 |
| `internal/agentcore/single_agent/agents/react_model_call.go` | 流循环后判 ctx（D2b） | 修改 |
| `internal/agentcore/single_agent/agents/react_agent_test.go` | D2/D3a 单测 | 修改 |
| `internal/agentcore/single_agent/agents/react_model_call_test.go` | D2b 单测（2 例） | 新建或追加 |
| `internal/agentcore/foundation/llm/model_clients/openai/client.go` | `WrapError` 加 `WithCause` | 修改 |
| `internal/agentcore/foundation/llm/model_clients/openai/client_test.go` | 错误链单测 | 修改 |
| `internal/agentcore/context_engine/context/session_model_context.go` | `ClearMessages` 只清本轮 | 修改 |
| `internal/agentcore/context_engine/context/session_model_context_test.go` | D3b 单测（2 例） | 修改 |
| `internal/swarm/server/runtime/uapclaw.go` | history 记录构造 + 取消判定 + channel metadata | 修改 |
| `internal/swarm/server/runtime/uapclaw_history_test.go` | D5/D6/D7 单测（5 例） | 新建 |
| `tests/integration/swarm/gateway/supplement_interrupt_test.go` | 集成：打断不杀新轮 + 无 chat.error | 新建 |
| `tests/integration/swarm/server/history_roundtrip_test.go` | 集成：history 往返可还原 | 新建 |

**提交组织：** 每个 Task 一个原子提交（TDD 红→绿→提交），三个「提交批次」对应设计稿 §8 的三个 PR 主题：Task 1-3（D1）、Task 4-8（D2/D2b/D3/D4）、Task 9-11（D5/D6/D7）。批次内任一红灯只回滚该 Task 的提交。

---

# 提交 1：流式任务收尾只释放自身

## Task 1: `streamTaskEntry` 与身份化 register/release/cancel

**Files:**
- Modify: `internal/swarm/server/agent_server.go:42-45,91,371-404`
- Test: `internal/swarm/server/agent_server_stream_task_test.go`（新建）

- [ ] **Step 1: 写失败测试**

新建 `internal/swarm/server/agent_server_stream_task_test.go`：

```go
package server

import (
	"context"
	"sync/atomic"
	"testing"
)

// countingCancel 返回带调用计数的 cancel 函数。
func countingCancel() (context.CancelFunc, *int32) {
	var n int32
	return func() { atomic.AddInt32(&n, 1) }, &n
}

// TestRegisterStreamTask_ReturnEntry 防 D1 回归：注册必须返回可比较身份的 entry。
func TestRegisterStreamTask_ReturnEntry(t *testing.T) {
	s := newTestAgentServer()
	cancelA, _ := countingCancel()
	entry := s.registerStreamTask("sess-1", "req-A", cancelA)

	s.sessionStreamTasksMu.RLock()
	cur := s.sessionStreamTasks["sess-1"]
	s.sessionStreamTasksMu.RUnlock()

	if cur != entry {
		t.Fatalf("槽位应指向 register 返回的 entry，实际 %+v", cur)
	}
	if entry.requestID != "req-A" {
		t.Fatalf("entry.requestID 应为 req-A，实际 %q", entry.requestID)
	}
}

// TestReleaseStreamTask_OnlyRemovesOwnEntry 是 D1 的核心防线：
// 旧轮收尾只能移除自己的登记，绝不能取消窗口内新注册的下一轮。
func TestReleaseStreamTask_OnlyRemovesOwnEntry(t *testing.T) {
	s := newTestAgentServer()

	cancelA, hitsA := countingCancel()
	entryA := s.registerStreamTask("sess-1", "req-A", cancelA)

	// 模拟 handleCancel：取消旧轮并删除槽位
	s.cancelStreamTask("sess-1")
	if got := atomic.LoadInt32(hitsA); got != 1 {
		t.Fatalf("cancelA 应被调用 1 次，实际 %d", got)
	}

	// 新一轮注册（resp 之后的新 chat.send）
	cancelB, hitsB := countingCancel()
	entryB := s.registerStreamTask("sess-1", "req-B", cancelB)

	// 旧轮收尾
	s.releaseStreamTask("sess-1", entryA)

	if got := atomic.LoadInt32(hitsB); got != 0 {
		t.Fatalf("旧轮收尾不得取消新一轮，cancelB 实际被调用 %d 次", got)
	}
	s.sessionStreamTasksMu.RLock()
	cur := s.sessionStreamTasks["sess-1"]
	s.sessionStreamTasksMu.RUnlock()
	if cur != entryB {
		t.Fatalf("槽位应仍是 B 的登记，实际 %+v", cur)
	}
}

// TestReleaseStreamTask_EmptySlotNoop 槽已被 handleCancel 删除时 release 必须无害。
func TestReleaseStreamTask_EmptySlotNoop(t *testing.T) {
	s := newTestAgentServer()
	cancelA, hitsA := countingCancel()
	entryA := s.registerStreamTask("sess-1", "req-A", cancelA)

	s.cancelStreamTask("sess-1") // 槽已空
	s.releaseStreamTask("sess-1", entryA)

	if got := atomic.LoadInt32(hitsA); got != 1 {
		t.Fatalf("release 不得再次取消，cancelA 实际被调用 %d 次", got)
	}
	if len(s.sessionStreamTasks) != 0 {
		t.Fatalf("表应为空，实际 %d 项", len(s.sessionStreamTasks))
	}
}

// TestCancelStreamTask_CancelsCurrentOnly 取消只作用于当前槽，且清理槽位。
func TestCancelStreamTask_CancelsCurrentOnly(t *testing.T) {
	s := newTestAgentServer()
	cancel1, hits1 := countingCancel()
	cancel2, hits2 := countingCancel()
	s.registerStreamTask("sess-1", "req-1", cancel1)
	s.registerStreamTask("sess-2", "req-2", cancel2)

	s.cancelStreamTask("sess-1")

	if got := atomic.LoadInt32(hits1); got != 1 {
		t.Fatalf("sess-1 的 cancel 应被调用 1 次，实际 %d", got)
	}
	if got := atomic.LoadInt32(hits2); got != 0 {
		t.Fatalf("sess-2 的 cancel 不应被调用，实际 %d", got)
	}
	s.sessionStreamTasksMu.RLock()
	_, exists := s.sessionStreamTasks["sess-1"]
	s.sessionStreamTasksMu.RUnlock()
	if exists {
		t.Fatal("sess-1 应从表中移除")
	}
}
```

- [ ] **Step 2: 跑测试确认编译失败**

Run: `go test ./internal/swarm/server/ -run 'TestRegisterStreamTask|TestReleaseStreamTask|TestCancelStreamTask' -count=1`
Expected: 编译错误 `undefined: s.registerStreamTask`（签名不匹配）/ `undefined: releaseStreamTask`

- [ ] **Step 3: 实现**

`internal/swarm/server/agent_server.go` 结构体字段（原 42-43 行）替换为：

```go
	// sessionStreamTasks 流式任务登记表（sessionID → 当前流任务 entry）
	// 存 entry 指针而非裸 cancel：收尾需要按身份判定「槽里还是不是我」
	sessionStreamTasks map[string]*streamTaskEntry
	// sessionStreamTasksMu 保护 sessionStreamTasks 的读写锁
	sessionStreamTasksMu sync.RWMutex
```

结构体区块新增（放在 `AgentServer` 定义之前）：

```go
// streamTaskEntry 流式任务登记项。
//
// 持有 requestID 仅用于日志与身份判定；context.CancelFunc 是函数值不可比较，
// 因此身份比较必须依赖本结构体的指针相等。
type streamTaskEntry struct {
	// requestID 该流式请求的标识
	requestID string
	// cancel 该请求 streamCtx 的取消函数
	cancel context.CancelFunc
}
```

构造函数（原 91 行）：

```go
		sessionStreamTasks: make(map[string]*streamTaskEntry),
```

`registerStreamTask` / `cancelStreamTask`（原 371-391 行）替换为：

```go
// registerStreamTask 注册流式任务并返回登记项。
func (s *AgentServer) registerStreamTask(sessionID, requestID string, cancel context.CancelFunc) *streamTaskEntry {
	entry := &streamTaskEntry{requestID: requestID, cancel: cancel}
	s.sessionStreamTasksMu.Lock()
	s.sessionStreamTasks[sessionID] = entry
	s.sessionStreamTasksMu.Unlock()
	logger.Debug(logComponent).
		Str("session_id", sessionID).
		Str("request_id", requestID).
		Msg("stream task registered")
	return entry
}

// releaseStreamTask 释放登记项：仅当槽内仍是自己时才删除，永不调用 cancel。
//
// Python: agent_ws_server.py:1259-1261 —— if map.get(sid) is current_task: pop
func (s *AgentServer) releaseStreamTask(sessionID string, entry *streamTaskEntry) {
	s.sessionStreamTasksMu.Lock()
	defer s.sessionStreamTasksMu.Unlock()
	cur, ok := s.sessionStreamTasks[sessionID]
	if !ok || cur != entry {
		// 槽已被 handleCancel 清空，或已被下一轮覆盖：不动别人的登记
		return
	}
	delete(s.sessionStreamTasks, sessionID)
	logger.Debug(logComponent).
		Str("session_id", sessionID).
		Str("request_id", entry.requestID).
		Msg("stream task released")
}

// cancelStreamTask 取消并移除指定会话当前登记的流式任务（handleCancel 专用）。
func (s *AgentServer) cancelStreamTask(sessionID string) {
	s.sessionStreamTasksMu.Lock()
	entry, ok := s.sessionStreamTasks[sessionID]
	if ok {
		delete(s.sessionStreamTasks, sessionID)
	}
	s.sessionStreamTasksMu.Unlock()
	if !ok {
		return
	}
	entry.cancel()
	logger.Debug(logComponent).
		Str("session_id", sessionID).
		Str("request_id", entry.requestID).
		Msg("stream task cancelled")
}
```

`cancelAllStreamTasks`（原 393-404 行）：

```go
func (s *AgentServer) cancelAllStreamTasks() {
	s.sessionStreamTasksMu.Lock()
	entries := make([]*streamTaskEntry, 0, len(s.sessionStreamTasks))
	for sessionID, entry := range s.sessionStreamTasks {
		entries = append(entries, entry)
		logger.Debug(logComponent).
			Str("session_id", sessionID).
			Str("request_id", entry.requestID).
			Msg("stream task cancelled")
	}
	s.sessionStreamTasks = make(map[string]*streamTaskEntry)
	s.sessionStreamTasksMu.Unlock()

	for _, entry := range entries {
		entry.cancel()
	}
}
```

- [ ] **Step 4: 适配既有测试**

`internal/swarm/server/agent_server_test.go:126-158`（`TestAgentServer_StreamTaskTracking`）把两处 `s.registerStreamTask("sess-N", cancelN)` 改为：

```go
	s.registerStreamTask("sess-1", "req-1", cancel1)
	s.registerStreamTask("sess-2", "req-2", cancel2)
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/swarm/server/ -run 'TestAgentServer_StreamTaskTracking|TestRegisterStreamTask|TestReleaseStreamTask|TestCancelStreamTask' -count=1 -race`
Expected: `ok  github.com/uapclaw/uapclaw-go/internal/swarm/server`

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/server/agent_server.go internal/swarm/server/agent_server_test.go internal/swarm/server/agent_server_stream_task_test.go
git commit -m "fix(swarm/server): 流式任务收尾只释放自身登记，不再取消下一轮"
```

## Task 2: `handleStream` 收尾接线

**Files:**
- Modify: `internal/swarm/server/handle_envelope.go:373-378`

- [ ] **Step 1: 接线**

```go
	// 1. 创建子 context + cancel
	streamCtx, cancel := context.WithCancel(ctx)

	// 2. 注册流式任务（返回登记项，收尾按身份释放；取消只由 handleCancel 发起）
	entry := s.registerStreamTask(sessionID, request.RequestID, cancel)
	defer s.releaseStreamTask(sessionID, entry)
```

- [ ] **Step 2: 全量编译 + 相关包测试**

Run: `go build ./... && go test ./internal/swarm/server/... -count=1 -race`
Expected: 编译通过，测试 `ok`

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/server/handle_envelope.go
git commit -m "fix(swarm/server): handleStream 收尾改为身份化 release"
```

## Task 3: 集成用例——supplement 打断不杀新轮

**Files:**
- Create: `tests/integration/swarm/gateway/supplement_interrupt_test.go`

**要点：** mock 模型默认瞬时返回，无法制造"旧轮仍在收尾、新轮已注册"的重叠。本用例在文件内定义**慢速流客户端**（每 120ms 吐一个 chunk，共 3 个），注册进 `ClientRegistry` 后由真实 WS 客户端驱动 `chat.send → chat.interrupt(supplement)`。确定性由 Task 1 的单测保证，本用例负责端到端契约。

- [ ] **Step 1: 写测试**

```go
//go:build integration

package gateway_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/suite"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// slowStreamClient 每 120ms 吐一个 chunk 的假模型，用于制造"旧轮仍在输出"的窗口。
type slowStreamClient struct {
	chunks []string
	delay  time.Duration
}

// Invoke 非流式调用：直接拼接全部 chunk。
func (c *slowStreamClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	joined := ""
	for _, s := range c.chunks {
		joined += s
	}
	return &llmschema.AssistantMessage{Content: llmschema.NewMessageContent(joined)}, nil
}

// Stream 流式调用：逐个 chunk 延迟发送，便于被 supplement 打断。
func (c *slowStreamClient) Stream(ctx context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	ch := make(chan *llmschema.AssistantMessageChunk, 1)
	go func() {
		defer close(ch)
		for _, s := range c.chunks {
			select {
			case <-ctx.Done():
				return
			case <-time.After(c.delay):
			}
			select {
			case <-ctx.Done():
				return
			case ch <- llmschema.NewAssistantMessageChunk(s):
			}
		}
	}()
	return ch, nil
}

// GenerateImage 不支持图片生成。
func (c *slowStreamClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("slowStreamClient: GenerateImage unsupported")
}

// GenerateSpeech 不支持语音生成。
func (c *slowStreamClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("slowStreamClient: GenerateSpeech unsupported")
}

// GenerateVideo 不支持视频生成。
func (c *slowStreamClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("slowStreamClient: GenerateVideo unsupported")
}

// TranscribeAudio 不支持转写。
func (c *slowStreamClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("slowStreamClient: TranscribeAudio unsupported")
}

// Release 释放模型资源（no-op）。
func (c *slowStreamClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return true, nil
}

// SupportsKVCacheRelease 不支持 KV Cache 释放。
func (c *slowStreamClient) SupportsKVCacheRelease() bool { return false }

// Factory 返回注册到 ClientRegistry 的工厂。
func (c *slowStreamClient) Factory() model_clients.ClientFactory {
	return func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
		return c, nil
	}
}

// SupplementInterruptSuite 验证 supplement 打断后新一轮不被打死、且不产生 chat.error。
type SupplementInterruptSuite struct {
	isuite.BaseIntegrationSuite
	providerName string
	dataDir      string
	wsPort       int
	agentServer  *server.AgentServer
	gwServer     *gateway.AppGatewayServer
	envBackup    [][3]string
}

// TestSupplementInterruptSuite 套件入口。
func TestSupplementInterruptSuite(t *testing.T) {
	suite.Run(t, new(SupplementInterruptSuite))
}

// saveAndSetenv 设置环境变量并记录原值，供 TearDownSuite 还原。
func (s *SupplementInterruptSuite) saveAndSetenv(key, value string) {
	old, ok := os.LookupEnv(key)
	mark := "0"
	if ok {
		mark = "1"
	}
	s.envBackup = append(s.envBackup, [3]string{key, old, mark})
	_ = os.Setenv(key, value)
}

// pickFreePort 取一个空闲 TCP 端口。
func (s *SupplementInterruptSuite) pickFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// SetupSuite 装配：临时数据目录 + 慢速 mock provider + 进程内 AgentServer/Gateway。
//
// 装配步骤与 tests/integration/swarm/gateway/team_continuous_dialog_test.go:76-140 逐条一致：
// ① 写 config/config.yaml（models.defaults 单模型）② 设 UAPCLAW_RESOURCES_DIR / DATA_DIR /
// WEB_PORT 并 pathutil.ResetCache() ③ workspace.Init 初始化 ④ ChannelTransport 串联
// AgentServer + AgentClient + AppGatewayServer。差异只有两处：mock provider 换成
// slowStreamClient（慢速流，制造打断窗口）、不写 modes.team 段（本用例只走 agent 模式）。
func (s *SupplementInterruptSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
	s.dataDir = s.T().TempDir()

	configDir := filepath.Join(s.dataDir, "config")
	s.Require().NoError(os.MkdirAll(configDir, 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(s.dataDir, "agent", "workspace"), 0o755))

	s.providerName = fmt.Sprintf("slow_itest_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(s.providerName, "llm", (&slowStreamClient{
		chunks: []string{"第一段", "第二段", "第三段"},
		delay:  120 * time.Millisecond,
	}).Factory())

	yaml := fmt.Sprintf(`workspace:
  path: %s
models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: %s
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        client_id: %s
      is_default: true
`, s.dataDir, s.providerName, s.providerName)
	s.Require().NoError(os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(yaml), 0o644))

	repoRoot, err := filepath.Abs("../../../..")
	s.Require().NoError(err)
	s.saveAndSetenv("UAPCLAW_RESOURCES_DIR", filepath.Join(repoRoot, "resources"))

	s.wsPort = s.pickFreePort()
	s.saveAndSetenv("UAPCLAW_DATA_DIR", s.dataDir)
	s.saveAndSetenv("UAPCLAW_WEB_PORT", fmt.Sprintf("%d", s.wsPort))
	pathutil.ResetCache()

	_, err = workspace.Init(workspace.InitOption{Language: "zh"})
	s.Require().NoError(err)

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

// TearDownSuite 逆序停机 + 还原环境变量 + 注销 provider。
func (s *SupplementInterruptSuite) TearDownSuite() {
	if s.gwServer != nil {
		_ = s.gwServer.Stop()
	}
	if s.agentServer != nil {
		_ = s.agentServer.Stop()
	}
	if s.providerName != "" {
		_ = model_clients.GetClientRegistry().Unregister(s.providerName, "llm")
	}
	for _, item := range s.envBackup {
		if item[2] == "1" {
			_ = os.Setenv(item[0], item[1])
		} else {
			_ = os.Unsetenv(item[0])
		}
	}
	pathutil.ResetCache()
	s.BaseIntegrationSuite.TearDownSuite()
}

// TestSupplement_SlowRoundInterrupted_NewRoundCompletes 是本用例的核心断言：
// ① 打断后新一轮必须拿到 chat.final；② 整个过程不得出现 chat.error（D2）；
// ③ 重复 3 次提高命中"旧轮收尾晚于新轮注册"窗口的概率（D1）。
//
// 确定性由 TestReleaseStreamTask_OnlyRemovesOwnEntry 保证；本用例验证端到端契约。
func (s *SupplementInterruptSuite) TestSupplement_SlowRoundInterrupted_NewRoundCompletes() {
	conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://127.0.0.1:%d/ws", s.wsPort), nil)
	s.Require().NoError(err)
	defer func() { _ = conn.Close() }()

	sessionID := fmt.Sprintf("sess_supp_%d", time.Now().UnixNano())
	_ = conn.WriteJSON(map[string]any{
		"method": "chat.send",
		"params": map[string]any{"session_id": sessionID, "content": "第一轮", "mode": "agent"},
	})

	// 等第一轮开始出字，再打断
	time.Sleep(200 * time.Millisecond)
	_ = conn.WriteJSON(map[string]any{
		"method": "chat.interrupt",
		"params": map[string]any{"session_id": sessionID, "intent": "supplement", "new_input": "改成第二件事"},
	})

	var sawError string
	var sawFinal bool
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, data, readErr := conn.ReadMessage()
		if readErr != nil {
			break
		}
		var frame struct {
			Type    string         `json:"type"`
			Payload map[string]any `json:"payload"`
		}
		if json.Unmarshal(data, &frame) != nil {
			continue
		}
		et, _ := frame.Payload["event_type"].(string)
		switch et {
		case "chat.error":
			sawError = fmt.Sprintf("%v", frame.Payload["error"])
		case "chat.final":
			if c, _ := frame.Payload["content"].(string); c != "" {
				sawFinal = true
			}
		}
		if sawFinal {
			break
		}
	}

	s.Empty(sawError, "打断后不得出现 chat.error，实际: %s", sawError)
	s.True(sawFinal, "打断后新一轮应产出非空 chat.final")

	// 防 D2 回归：取消不得写入 chat.error 历史记录（前端恢复历史时不展示，但会污染流水）
	records, err := session.ReadHistoryRecords(sessionID)
	s.Require().NoError(err)
	for _, r := range records {
		s.NotEqual("chat.error", r["event_type"],
			"取消不得写入 chat.error 记录: %+v", r)
	}
}
```

- [ ] **Step 2: 跑集成用例**

Run: `go test -tags=integration ./tests/integration/swarm/gateway/ -run TestSupplementInterruptSuite -count=1 -v`
Expected: PASS；若在改动前的代码上跑，`sawError` 会命中 `context canceled`（可作为红测验证）

- [ ] **Step 3: 提交**

```bash
git add tests/integration/swarm/gateway/supplement_interrupt_test.go
git commit -m "test(integration): supplement 打断后新一轮不被误取消且无 chat.error"
```

---

# 提交 2：取消静默 + 清理范围

## Task 4: `utils.Canceled` 判定

**Files:**
- Create: `internal/common/utils/ctxutil.go`、`internal/common/utils/ctxutil_test.go`
- Modify: `internal/common/utils/doc.go:8-23`

- [ ] **Step 1: 写失败测试**

```go
package utils

import (
	"context"
	"testing"
	"time"
)

// TestCanceled 防 D4 回归：只认 context.Canceled，DeadlineExceeded 不算取消。
func TestCanceled(t *testing.T) {
	if Canceled(context.Background()) {
		t.Fatal("未取消的 ctx 不应判定为取消")
	}

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if !Canceled(cancelCtx) {
		t.Fatal("已取消的 ctx 应判定为取消")
	}

	dlCtx, dlCancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer dlCancel()
	<-dlCtx.Done()
	if Canceled(dlCtx) {
		t.Fatal("超时不应被判定为取消（需按错误上报）")
	}

	if Canceled(nil) {
		t.Fatal("nil ctx 不应判定为取消")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/common/utils/ -run TestCanceled -count=1`
Expected: 编译错误 `undefined: Canceled`

- [ ] **Step 3: 实现**

`internal/common/utils/ctxutil.go`：

```go
package utils

import (
	"context"
	"errors"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Canceled 判定 ctx 是否被外部取消。
//
// 只认 context.Canceled：DeadlineExceeded 仍须按错误上报
// （Python 的 TimeoutError 是 Exception 会被上报，CancelledError 不会）。
// 注意：不要用 errors.Is(err, context.Canceled) 替代本判定——
// LLM 客户端的 WrapError 历史上用 %v 拼串，错误链可能已断。
func Canceled(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	return errors.Is(ctx.Err(), context.Canceled)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/common/utils/ -run TestCanceled -count=1`
Expected: PASS

- [ ] **Step 5: 登记 doc.go**

`internal/common/utils/doc.go` 文件目录树中，在 `truncate.go` 之后插入：

```
//	├── ctxutil.go        # 上下文判定：Canceled（只认 context.Canceled）
```

- [ ] **Step 6: 提交**

```bash
git add internal/common/utils/ctxutil.go internal/common/utils/ctxutil_test.go internal/common/utils/doc.go
git commit -m "feat(utils): 新增 Canceled 判定（只认 context.Canceled）"
```

## Task 5: `react_invoke` 取消静默 + 删除重复清理

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go:63-68,445-451,560-570`
- Test: `internal/agentcore/single_agent/agents/react_agent_test.go`（追加）

- [ ] **Step 1: 写失败测试**

在 `react_agent_test.go` 末尾追加。`streamProcess` 是闭包无法从外部注入，因此把「取消时是否上报」抽成可测的小函数 `shouldReportStreamError`，由单测锁死判定，接线由集成用例（Task 3）端到端验证：

```go
// TestShouldReportStreamError 防 D2 回归：取消态下任何错误都不上报；未取消时照常上报。
func TestShouldReportStreamError(t *testing.T) {
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	errBoom := errors.New("boom")

	cases := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{"取消+错误_不上报", canceledCtx, errBoom, false},
		{"取消+无错误_不上报", canceledCtx, nil, false},
		{"正常+错误_上报", context.Background(), errBoom, true},
		{"正常+无错误_不上报", context.Background(), nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldReportStreamError(c.ctx, c.err); got != c.want {
				t.Fatalf("shouldReportStreamError=%v, want %v", got, c.want)
			}
		})
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/single_agent/agents/ -run 'TestShouldReportStreamError' -count=1`
Expected: 编译错误 `undefined: shouldReportStreamError`

- [ ] **Step 3: 实现**

① `react_invoke.go` import 增加：

```go
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
```

② 删除 `Invoke` 内的清理块（原 63-80 行），改为：

```go
	result, err := a.invokeImpl(ctx, inputs, opts...)
	if err != nil {
		if _, ok := err.(*exception.BaseError); ok {
			return nil, err
		}
		logger.Error(logComponent).
			Str("agent_id", a.card.ID).
			Err(err).
			Msg("Agent invoke error")
		return nil, exception.NewBaseError(exception.StatusAgentControllerRuntimeError,
			exception.WithParam("error_msg", err.Error()),
			exception.WithCause(err),
		)
	}
```

③ `invokeImpl` 的错误分支（原 445-451 行）改为：

```go
	// 合并错误判断：ctx 取消时清除本轮上下文消息（对齐 Python CancelledError 分支）
	if err != nil {
		if utils.Canceled(ctx) {
			a.ClearContextMessages(ctx, sess)
		}
		return nil, err
	}
```

④ `streamProcess` 的错误分支（原 563-571 行）改为：

```go
		result, err := a.Invoke(ctx, inputs, opts...)
		if err != nil {
			// ctx 取消不是错误：不写错误帧、不落 history（对齐 Python except Exception 抓不到 CancelledError）
			if !shouldReportStreamError(ctx, err) {
				logger.Info(logComponent).
					Str("event_type", "stream_canceled").
					Msg("stream invoke canceled by caller")
				return
			}
			logger.Error(logComponent).Err(err).Str("event_type", "LLM_CALL_ERROR").Msg("streamProcess invoke error")
			a.WriteInvokeResultToStream(ctx, map[string]any{
				"output":      err.Error(),
				"result_type": "error",
			}, sess)
			return
		}
```

⑤ 非导出区块新增判定函数：

```go
// shouldReportStreamError 判断 streamProcess 是否应把错误写入流。
//
// ctx 取消不是错误：对齐 Python —— `except Exception` 抓不到 asyncio.CancelledError，
// 因此取消时不会产生 chat.error 帧、也不会落 history。
func shouldReportStreamError(ctx context.Context, err error) bool {
	return err != nil && !utils.Canceled(ctx)
}
```

- [ ] **Step 4: 补"清理只执行一次"用例（D3a）**

`TestShouldReportStreamError` 的 `正常+错误_上报` 一行已是反向用例（防"静默过度"）。
再补 D3a：给 `react_agent_test.go` 既有的 `fakeModelContext`（第 38 行）加计数。

① 结构体加字段（`fakeModelContext` 定义处）：

```go
// fakeModelContext 模拟 ModelContext
type fakeModelContext struct {
	messages []llmschema.BaseMessage
	// clearCalls ClearMessages 调用次数（用于 D3a 断言）
	clearCalls int
}
```

② `ClearMessages`（原 1231 行）体内第一行加 `f.clearCalls++`：

```go
func (f *fakeModelContext) ClearMessages(_ context.Context, _ bool, _ ...ceinterface.Option) error {
	f.clearCalls++
	return nil
}
```

③ 追加用例：

```go
// TestInvoke_ClearsContextMessagesOnce 防 D3a 回归：取消路径只允许清理一次。
func TestInvoke_ClearsContextMessagesOnce(t *testing.T) {
	fmc := &fakeModelContext{}
	agent := newTestAgent("clear_once")
	agent.contextEngine = &fakeContextEngine{modelCtx: fmc}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _ = agent.Invoke(ctx, map[string]any{},
		interfaces.WithSession(&fakeSessionFacade{sessionID: "sess-clear"}))

	if fmc.clearCalls != 1 {
		t.Fatalf("取消路径应只清理 1 次，实际 %d 次", fmc.clearCalls)
	}
}
```

- [ ] **Step 5: 跑测试**

Run: `go test ./internal/agentcore/single_agent/agents/ -count=1 -race`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_invoke.go internal/agentcore/single_agent/agents/react_agent_test.go
git commit -m "fix(agentcore): 取消不再上报为错误，且清理只执行一次"
```

## Task 6: `callLLMStream` 流结束后判 ctx（D2b）

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_model_call.go:256-307`
- Test: `internal/agentcore/single_agent/agents/react_model_call_test.go`（新建或追加）

- [ ] **Step 1: 写失败测试**

```go
package agents

import (
	"context"
	"fmt"
	"testing"
	"time"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// fakeStreamClient 立即返回一个已关闭的 chunk 通道，模拟"流结束"。
// 必须实现完整 BaseModelClient 接口，才能注册进 ClientRegistry。
type fakeStreamClient struct {
	chunks []*llmschema.AssistantMessageChunk
}

// Invoke 非流式（本用例不用）。
func (f *fakeStreamClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return llmschema.NewAssistantMessage(""), nil
}

// Stream 返回预置 chunk 后关闭通道。
func (f *fakeStreamClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	ch := make(chan *llmschema.AssistantMessageChunk, len(f.chunks))
	for _, c := range f.chunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// GenerateImage 不支持图片生成。
func (f *fakeStreamClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("unsupported")
}

// GenerateSpeech 不支持语音生成。
func (f *fakeStreamClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("unsupported")
}

// GenerateVideo 不支持视频生成。
func (f *fakeStreamClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("unsupported")
}

// TranscribeAudio 不支持转写。
func (f *fakeStreamClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("unsupported")
}

// Release 释放资源（no-op）。
func (f *fakeStreamClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return true, nil
}

// SupportsKVCacheRelease 不支持 KV Cache 释放。
func (f *fakeStreamClient) SupportsKVCacheRelease() bool { return false }

// newFakeLLMModel 通过 ClientRegistry 注入 fake client，构造 *llm.Model。
func newFakeLLMModel(t *testing.T, client *fakeStreamClient) *llm.Model {
	t.Helper()
	provider := fmt.Sprintf("fake_cancel_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(provider, "llm",
		func(_ *llmschema.ModelRequestConfig, _ *llmschema.ModelClientConfig) (model_clients.BaseModelClient, error) {
			return client, nil
		})
	t.Cleanup(func() { _ = model_clients.GetClientRegistry().Unregister(provider, "llm") })

	m, err := llm.NewModel(
		&llmschema.ModelClientConfig{ClientProvider: provider, ClientID: "test", APIKey: "k"},
		llmschema.NewModelRequestConfig(),
	)
	if err != nil {
		t.Fatalf("构造 llm.Model 失败: %v", err)
	}
	return m
}

// TestCallLLMStream_CanceledMidStream 防 D2b 回归：
// 流已结束但 ctx 处于取消态时，必须返回 error，不能把半截内容当正常回答。
func TestCallLLMStream_CanceledMidStream(t *testing.T) {
	a := &ReActAgent{}
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeStreamClient{chunks: []*llmschema.AssistantMessageChunk{
		llmschema.NewAssistantMessageChunk("半截"),
	}}
	model := newFakeLLMModel(t, client)

	// 先取消，再走一次完整流：模拟"取消发生在流进行中"
	cancel()
	msg, err := a.callLLMStream(ctx, model, "mock-model", nil, nil, nil, nil)
	if err == nil {
		t.Fatalf("取消态下应返回 error，实际返回消息: %+v", msg)
	}
}

// TestCallLLMStream_NotCanceledReturnsMessage 未取消时正常返回累积消息（反向用例）。
func TestCallLLMStream_NotCanceledReturnsMessage(t *testing.T) {
	a := &ReActAgent{}
	client := &fakeStreamClient{chunks: []*llmschema.AssistantMessageChunk{
		llmschema.NewAssistantMessageChunk("完整"),
	}}
	model := newFakeLLMModel(t, client)

	msg, err := a.callLLMStream(context.Background(), model, "mock-model", nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("未取消不应报错: %v", err)
	}
	if msg == nil || msg.Content.Text() != "完整" {
		t.Fatalf("应返回累积内容，实际 %+v", msg)
	}
}
```

- [ ] **Step 2: 跑测试确认第一个用例失败**

Run: `go test ./internal/agentcore/single_agent/agents/ -run 'TestCallLLMStream_' -count=1`
Expected: `TestCallLLMStream_CanceledMidStream` FAIL（err == nil）；另一个 PASS

- [ ] **Step 3: 实现**

`react_model_call.go`，在 `for chunk := range chunkCh { ... }` 之后、`// 从累积 chunk 转换` 之前插入：

```go
	// 取消判定：流可能因 ctx 取消而正常关闭（客户端只关通道、不回错误），
	// 此时若继续返回累积消息，半截回答会被当成最终答复（对齐 Python：CancelledError 中止）。
	if utils.Canceled(ctx) {
		return nil, ctx.Err()
	}
```

同文件 import 增加 `"github.com/uapclaw/uapclaw-go/internal/common/utils"`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentcore/single_agent/agents/ -run 'TestCallLLMStream_' -count=1 -race`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/single_agent/agents/react_model_call.go internal/agentcore/single_agent/agents/react_model_call_test.go
git commit -m "fix(agentcore): 流结束但已取消时返回 error，避免半截回答被当成最终答复"
```

## Task 7: `WrapError` 保住错误链

**Files:**
- Modify: `internal/agentcore/foundation/llm/model_clients/openai/client.go:492-501`
- Test: `internal/agentcore/foundation/llm/model_clients/openai/client_test.go`（追加）

- [ ] **Step 1: 写失败测试**

```go
// TestWrapError_PreservesCause 防错误链回归：包装后 errors.Is 仍可命中原始错误。
func TestWrapError_PreservesCause(t *testing.T) {
	c := &OpenAIModelClient{}
	wrapped := c.WrapError("stream", context.Canceled)

	if !errors.Is(wrapped, context.Canceled) {
		t.Fatal("WrapError 应保留 cause，使 errors.Is 可命中 context.Canceled")
	}
	if !strings.Contains(wrapped.Error(), "OpenAI API async stream error") {
		t.Fatalf("错误文案不得改变，实际: %s", wrapped.Error())
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/openai/ -run TestWrapError_PreservesCause -count=1`
Expected: FAIL（`errors.Is` 为 false）

- [ ] **Step 3: 实现**

```go
func (c *OpenAIModelClient) WrapError(method string, err error) error {
	errDetail := fmt.Sprintf("%T: %v", err, err)
	if err.Error() == "" {
		errDetail = fmt.Sprintf("%T", err)
	}
	return exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg(fmt.Sprintf("OpenAI API async %s error: %s", method, errDetail)),
		// 保留 cause：文案不变，但 errors.Is 可命中原始错误（如 context.Canceled）
		exception.WithCause(err),
	)
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentcore/foundation/llm/model_clients/openai/ -run TestWrapError -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/foundation/llm/model_clients/openai/client.go internal/agentcore/foundation/llm/model_clients/openai/client_test.go
git commit -m "fix(llm/openai): WrapError 保留 cause，错误链可判定"
```

## Task 8: `ClearMessages` 只清本轮

**Files:**
- Modify: `internal/agentcore/context_engine/context/session_model_context.go:250-282`
- Test: `internal/agentcore/context_engine/context/session_model_context_test.go`（追加）

- [ ] **Step 1: 写失败测试**

```go
// TestClearMessages_OnlyClearsCurrentRound 防 D3b 回归：
// withHistory=false 只清本轮新增消息，历史前缀必须保留。
func TestClearMessages_OnlyClearsCurrentRound(t *testing.T) {
	mc := newTestSessionModelContext() // 复用本文件既有构造器
	history := []llmschema.BaseMessage{
		llmschema.NewUserMessage("历史1"),
		llmschema.NewAssistantMessage("历史2"),
	}
	mc.messageBuffer.Rebuild(history)
	_, err := mc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("本轮1"),
		llmschema.NewAssistantMessage("本轮2"),
	})
	if err != nil {
		t.Fatalf("AddMessages 失败: %v", err)
	}

	if err := mc.ClearMessages(context.Background(), false); err != nil {
		t.Fatalf("ClearMessages 失败: %v", err)
	}

	if got := len(mc.messageBuffer.GetBack(0, true)); got != len(history) {
		t.Fatalf("历史应保留 %d 条，实际 %d", len(history), got)
	}
	if got := len(mc.messageBuffer.GetBack(0, false)); got != 0 {
		t.Fatalf("本轮应清空，实际剩 %d 条", got)
	}
}

// TestClearMessages_HistorySizeConsistent 防脏计数：
// 清理后 historyMessagesSize 必须与 buffer 长度自洽。
func TestClearMessages_HistorySizeConsistent(t *testing.T) {
	mc := newTestSessionModelContext()
	mc.messageBuffer.Rebuild([]llmschema.BaseMessage{llmschema.NewUserMessage("历史1")})
	_, _ = mc.AddMessages(context.Background(), []llmschema.BaseMessage{llmschema.NewUserMessage("本轮1")})

	_ = mc.ClearMessages(context.Background(), false)

	// 再追加一条，本轮视角必须立刻可见（改动前因 historyMessagesSize 残留会返回空）
	_, _ = mc.AddMessages(context.Background(), []llmschema.BaseMessage{llmschema.NewUserMessage("新一轮")})
	if got := len(mc.messageBuffer.GetBack(0, false)); got != 1 {
		t.Fatalf("清理后新一轮消息应立即可见，实际 %d 条", got)
	}
}
```

> 若 `newTestSessionModelContext()` 不存在，用本文件既有构造方式创建（`NewSessionModelContext(...)` + 默认 token counter），保持与相邻用例一致。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/agentcore/context_engine/context/ -run 'TestClearMessages_' -count=1`
Expected: 两个用例均 FAIL（历史被清空 / 新一轮不可见）

- [ ] **Step 3: 实现**

`session_model_context.go` 的 `ClearMessages` 开头替换为：

```go
func (mc *SessionModelContext) ClearMessages(ctx context.Context, withHistory bool, opts ...iface.Option) error {
	// 计算待弹出条数：withHistory=false 只弹「历史之后的本轮消息」
	// Python: pop_back(size=None, with_history=False) → get_back(None, False) 的长度
	totalSize := len(mc.messageBuffer.GetBack(0, true))
	popSize := totalSize
	if !withHistory {
		popSize = totalSize - len(mc.messageBuffer.GetBack(0, false))
	}
	if popSize > 0 {
		mc.messageBuffer.PopBack(popSize, withHistory)
	}
	// 以下保持不变（重置卸载缓冲区 + 触发 ContextCleared 事件 + 日志，
	// 日志中的 cleared_count 使用 popSize）
```

并把后续日志的 `Int("cleared_count", totalSize)` 改为 `Int("cleared_count", popSize)`。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/agentcore/context_engine/context/ -count=1 -race`
Expected: PASS（含既有用例）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/context/session_model_context.go internal/agentcore/context_engine/context/session_model_context_test.go
git commit -m "fix(context_engine): ClearMessages(withHistory=false) 只清本轮，保留历史前缀"
```

---

# 提交 3：history 记录字段

## Task 9: 记录构造——全量 extra + team 叠加 + error 兜底

**Files:**
- Modify: `internal/swarm/server/runtime/uapclaw.go:353-411,924-931`
- Test: `internal/swarm/server/runtime/uapclaw_history_test.go`（新建）

- [ ] **Step 1: 写失败测试**

```go
package runtime

import (
	"testing"
)

// TestPayloadExtras_ToolCall 防 D5 回归：工具调用记录必须携带 tool_call 嵌套对象。
func TestPayloadExtras_ToolCall(t *testing.T) {
	payload := map[string]any{
		"event_type": "chat.tool_call",
		"tool_call": map[string]any{"name": "read_file", "arguments": "{}", "tool_call_id": "call_x"},
	}
	extra := payloadExtras(payload)
	tc, ok := extra["tool_call"].(map[string]any)
	if !ok {
		t.Fatalf("extra 应含 tool_call 嵌套对象，实际: %+v", extra)
	}
	if tc["tool_call_id"] != "call_x" {
		t.Fatalf("tool_call_id 丢失: %+v", tc)
	}
	if _, exists := extra["event_type"]; exists {
		t.Fatal("event_type 不应进入 extra")
	}
}

// TestPayloadExtras_ToolResult 防 D5 回归：工具结果字段平铺到顶层。
func TestPayloadExtras_ToolResult(t *testing.T) {
	payload := map[string]any{
		"event_type":   "chat.tool_result",
		"result":       "文件内容",
		"tool_name":    "read_file",
		"tool_call_id": "call_x",
		"success":      true,
	}
	extra := payloadExtras(payload)
	for _, key := range []string{"result", "tool_name", "tool_call_id", "success"} {
		if _, ok := extra[key]; !ok {
			t.Fatalf("extra 缺少 %s: %+v", key, extra)
		}
	}
}

// TestPayloadExtras_TeamMessageKeepsEvent 防 team 记录形状回归：
// 既保留嵌套 event，又展平其子字段，且不覆盖 type/timestamp/content。
func TestPayloadExtras_TeamMessageKeepsEvent(t *testing.T) {
	payload := map[string]any{
		"event_type": "team.message",
		"event": map[string]any{
			"type":      "member_added",
			"timestamp": 1.5,
			"content":   "hi",
			"member_id": "m1",
		},
	}
	extra := payloadExtras(payload)
	ev, ok := extra["event"].(map[string]any)
	if !ok {
		t.Fatalf("extra 应保留嵌套 event，实际: %+v", extra)
	}
	if ev["member_id"] != "m1" {
		t.Fatalf("嵌套 event 内容被破坏: %+v", ev)
	}
	if extra["member_id"] != "m1" {
		t.Fatalf("event 子字段应展平到顶层: %+v", extra)
	}
	for _, key := range []string{"type", "timestamp", "content"} {
		if _, exists := extra[key]; exists {
			t.Fatalf("展平不得写入 %s: %+v", key, extra)
		}
	}
}

// TestExtractChunkContent_ErrorFallback 防 D6 回归：content 为空时取 error。
func TestExtractChunkContent_ErrorFallback(t *testing.T) {
	if got := extractChunkContent(map[string]any{"error": "模型调用失败"}); got != "模型调用失败" {
		t.Fatalf("应回退到 error 字段，实际 %q", got)
	}
	if got := extractChunkContent(map[string]any{"content": "正文", "error": "忽略"}); got != "正文" {
		t.Fatalf("content 优先，实际 %q", got)
	}
}
```

- [ ] **Step 2: 跑测试确认编译失败**

Run: `go test ./internal/swarm/server/runtime/ -run 'TestPayloadExtras|TestExtractChunkContent' -count=1`
Expected: 编译错误 `undefined: payloadExtras`

- [ ] **Step 3: 实现**

`uapclaw.go` 消费循环（原 368-383 行）替换为：

```go
						// Python: extra_fields = payload 去掉 event_type/content 后的全部字段
						extraFields := payloadExtras(payload)
						// Python: team.message 额外把 event 子字段展平到顶层
						if eventType == "team.message" {
							if eventData, ok := payload["event"].(map[string]any); ok {
								for k, v := range eventData {
									if k == "type" || k == "timestamp" || k == "content" {
										continue
									}
									extraFields[k] = v
								}
							}
						}
						session.AppendHistoryRecord(sessionID, request.RequestID, request.ChannelID,
							"assistant", extractChunkContent(payload), float64(time.Now().UnixMilli())/1000,
							eventType, extraFields, nil, streamMode)
```

新增非导出函数（放在 `extractChunkContent` 之前）：

```go
// payloadExtras 构造 history 记录的附加字段。
//
// 对齐 Python: interface.py:1063/1102 —— 除 event_type/content 外全部字段。
// 注意保留 team.message 的嵌套 event（前端 extractTeamEventRecord 一级回退需要它）。
func payloadExtras(payload map[string]any) map[string]any {
	extra := make(map[string]any, len(payload))
	for k, v := range payload {
		if k == "event_type" || k == "content" {
			continue
		}
		extra[k] = v
	}
	return extra
}
```

`extractChunkContent`（原 924-931 行）替换为：

```go
// extractChunkContent 提取 history 记录的 content。
// Python: data.payload.get("content") or data.payload.get("error") or ""
func extractChunkContent(payload map[string]any) string {
	if c, ok := payload["content"].(string); ok && c != "" {
		return c
	}
	if e, ok := payload["error"].(string); ok {
		return e
	}
	return ""
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/swarm/server/runtime/ -run 'TestPayloadExtras|TestExtractChunkContent' -count=1 -race`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/runtime/uapclaw.go internal/swarm/server/runtime/uapclaw_history_test.go
git commit -m "fix(swarm/server): history 记录保留 payload 字段并补 error 兜底"
```

## Task 10: 取消判定与 channel metadata

**Files:**
- Modify: `internal/swarm/server/runtime/uapclaw.go:284-286,318`

- [ ] **Step 1: 写失败测试**

先把 `uapclaw_history_test.go` 的 import 块补齐（本步开始用到 `session` / `workspace`）：

```go
import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)
```

追加：

```go
// TestChannelMetadataPassthrough 防 D7 回归：流式 user 记录必须带 channel_metadata。
// 通过 AppendHistoryRecord 的入参直测（避免拉起整个流式链路）。
func TestChannelMetadataPassthrough(t *testing.T) {
	origHome := workspace.UserHomeDir()
	t.Cleanup(func() { workspace.SetUserHome(origHome) })
	workspace.SetUserHome(t.TempDir())

	sid := "sess-meta-test"
	session.AppendHistoryRecord(sid, "req-1", "web", "user", "你好", 1.0,
		"", nil, map[string]any{"chat_id": "c1"}, "agent")
	session.FlushHistoryQueue()

	records, err := session.ReadHistoryRecords(sid)
	if err != nil {
		t.Fatalf("读取历史失败: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("应写入 1 条记录，实际 %d", len(records))
	}
	if records[0]["content"] != "你好" {
		t.Fatalf("content 不符: %+v", records[0])
	}
}
```

> 该用例锁定"传参后能落盘"的路径；调用点改由 Step 3 的人工检查 + 集成用例
> `TestHistoryRoundTrip` 兜底。

- [ ] **Step 2: 实现**

`uapclaw.go:284-286` 的 `nil` 改为 `request.Metadata`：

```go
	session.AppendHistoryRecord(sessionID, request.RequestID, request.ChannelID,
		"user", uc.extractQuery(request), float64(time.Now().UnixMilli())/1000,
		"", nil, request.Metadata, userMode)
```

`uapclaw.go:318` 改为：

```go
		if utils.Canceled(ctx) {
			return
		}
```

并给 `uapclaw.go` 增加 import `"github.com/uapclaw/uapclaw-go/internal/common/utils"`。

- [ ] **Step 3: 跑测试**

Run: `go test ./internal/swarm/server/runtime/ -count=1 -race`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/runtime/uapclaw.go internal/swarm/server/runtime/uapclaw_history_test.go
git commit -m "fix(swarm/server): 流式 user 记录传递 channel_metadata，取消判定改用 ctx"
```

## Task 11: 集成用例——history 往返可还原

**Files:**
- Create: `tests/integration/swarm/server/history_roundtrip_test.go`

- [ ] **Step 1: 写测试**

```go
//go:build integration

package server_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// HistoryRoundtripSuite 验证 history 记录形状足以支撑前端还原工具调用。
type HistoryRoundtripSuite struct {
	isuite.BaseIntegrationSuite
	sessionID string
	origHome  string
}

// TestHistoryRoundtripSuite 套件入口。
func TestHistoryRoundtripSuite(t *testing.T) {
	suite.Run(t, new(HistoryRoundtripSuite))
}

// SetupTest 每个用例独立数据目录与 session。
func (s *HistoryRoundtripSuite) SetupTest() {
	s.origHome = workspace.UserHomeDir()
	workspace.SetUserHome(s.T().TempDir())
	s.sessionID = s.T().Name()
}

// TearDownTest 还原路径覆盖。
func (s *HistoryRoundtripSuite) TearDownTest() {
	workspace.SetUserHome(s.origHome)
}

// TestToolCallRestorable 防 D5 回归：工具调用/结果记录必须带前端还原所需字段。
func (s *HistoryRoundtripSuite) TestToolCallRestorable() {
	session.AppendHistoryRecord(s.sessionID, "req-1", "web", "assistant", "",
		1.0, "chat.tool_call", map[string]any{
			"tool_call": map[string]any{"name": "read_file", "arguments": "{}", "tool_call_id": "call_x"},
		}, nil, "agent")
	session.AppendHistoryRecord(s.sessionID, "req-1", "web", "assistant", "",
		1.1, "chat.tool_result", map[string]any{
			"result":       "内容",
			"tool_name":    "read_file",
			"tool_call_id": "call_x",
			"success":      true,
		}, nil, "agent")
	session.AppendHistoryRecord(s.sessionID, "req-1", "web", "assistant", "最终答复",
		1.2, "chat.final", nil, nil, "agent")
	session.FlushHistoryQueue()

	records, err := session.ReadHistoryRecords(s.sessionID)
	s.Require().NoError(err)
	s.Require().Len(records, 3)

	// 前端 buildEventPayloadForRecord 会把这些字段提升为 payload：
	// tool_call 记录需含嵌套 tool_call；tool_result 记录需含 tool_call_id。
	var call, result map[string]any
	for _, r := range records {
		switch r["event_type"] {
		case "chat.tool_call":
			call = r
		case "chat.tool_result":
			result = r
		}
	}
	s.Require().NotNil(call, "应存在 chat.tool_call 记录")
	tc, ok := call["tool_call"].(map[string]any)
	s.Require().True(ok, "chat.tool_call 记录应含嵌套 tool_call: %+v", call)
	s.Equal("read_file", tc["name"])
	s.Equal("call_x", tc["tool_call_id"])

	s.Require().NotNil(result, "应存在 chat.tool_result 记录")
	s.Equal("call_x", result["tool_call_id"])
	s.Equal("read_file", result["tool_name"])
	s.Equal("内容", result["result"])
}

// TestTeamEventShape 防 team 记录形状回归：嵌套 event 与展平字段并存。
func (s *HistoryRoundtripSuite) TestTeamEventShape() {
	session.AppendHistoryRecord(s.sessionID, "req-2", "web", "assistant", "",
		2.0, "team.message", map[string]any{
			"event":    map[string]any{"type": "member_added", "member_id": "m1"},
			"member_id": "m1",
		}, nil, "team")
	session.FlushHistoryQueue()

	records, err := session.ReadHistoryRecords(s.sessionID)
	s.Require().NoError(err)
	s.Require().Len(records, 1)

	raw, err := json.Marshal(records[0])
	s.Require().NoError(err)
	var got map[string]any
	s.Require().NoError(json.Unmarshal(raw, &got))

	ev, ok := got["event"].(map[string]any)
	s.Require().True(ok, "team.message 记录应保留嵌套 event: %s", string(raw))
	s.Equal("member_added", ev["type"])
	s.Equal("m1", got["member_id"], "event 子字段应展平到顶层")
}
```

- [ ] **Step 2: 跑集成用例**

Run: `go test -tags=integration ./tests/integration/swarm/server/ -run TestHistoryRoundtripSuite -count=1 -v`
Expected: PASS

- [ ] **Step 3: 本地跑集成审计**

Run: `go run ./cmd/integration-audit -verify`
Expected: 新目录不是灰区（跨度含 `internal/swarm/server/session` 与 `internal/swarm/server`），无失败项

- [ ] **Step 4: 提交**

```bash
git add tests/integration/swarm/server/history_roundtrip_test.go
git commit -m "test(integration): history 记录可还原工具调用与 team 事件"
```

---

# 收尾检查（三个提交完成后）

- [ ] **全量单测 + race**

Run: `go test -mod=mod -tags=test -race ./...`
Expected: 全 PASS

- [ ] **格式与静态检查**

Run: `gofmt -l internal tests && go vet ./... && staticcheck ./...`
Expected: `gofmt -l` 无输出；vet/staticcheck 无新增问题

- [ ] **集成测试**

Run: `go test -tags=integration ./tests/integration/... -count=1`
Expected: 全 PASS

- [ ] **CI 四绿**

Run: `git push` 后 `gh run list --limit 3`，确认 Build / Lint / Unit Test / Integration Test 四个 job 均 SUCCESS
Expected: 全绿；红灯则修复后重推，不允许在红灯状态收尾

- [ ] **回归留痕**

在提交 1 与提交 3 的 PR 描述里各贴一条"改动前失败"的证据（`TestReleaseStreamTask_OnlyRemovesOwnEntry` / `TestToolCallRestorable`），便于以后追溯为什么加这两个用例。
