# Dreaming 增量语义集成测试迁移 实施计划

**状态：** 已完成（CI run 41161797386 四绿：Build / Lint / Unit Test / Integration Test；集成审计该目录跨度 3、Exempt=false）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 Python `tests/system_tests/test_dreaming_e2e.py` 的 7 个用例迁到 Go 集成层，补 5 条增量边界，真跑 code/agent 两种 `CreateInstance` 打通 adapter→sweeper 接线，并撤销该目录的集成审计豁免。

**Architecture:** 用 `testhelpers.NewAdapterTestEnv`（注册 mock provider + `UAPCLAW_DATA_DIR` 沙箱 + `pathutil.ResetCache`）让 `Sweeper.extractViaLLM` 走真实链路（提示词模板 + ```json 围栏 + JSON 解析）而模型响应可控，从而对 `RunSweep` 的增量窗口、去重、checkpoint 推进做精确断言。生产代码零改动；只有测试侧 `mockllm` 增加入参记录。

**Tech Stack:** Go 1.22 / testify suite / `//go:build integration` / `//go:build integration && llm` / `cmd/integration-audit` 双层门禁。

**Spec:** `docs/superpowers/specs/2026-10-31-dreaming-incremental-itest-design.md`

---

## 文件结构

| 文件 | 责任 |
|---|---|
| `tests/integration/mockllm/mock_client.go`（改） | `InvokeRecord` 增 `Messages` 字段，记录每次 Invoke 的入参 |
| `tests/integration/testhelpers/dreaming_env.go`（新） | `DreamingEnv`：会话播种、追加轮次、mtime 控制、响应排队、prompt 提取、checkpoint 读取 |
| `tests/integration/swarm/agents/harness/common/memory/dreaming/sweeper_incremental_test.go`（新） | 12 个管线用例（7 移植 + 5 边界） |
| `tests/integration/swarm/agents/harness/common/memory/dreaming/start_wiring_test.go`（新） | 4 个接线用例（启停 2 + code/agent 真跑 CreateInstance 2）+ 1 可选 |
| `tests/integration/swarm/agents/harness/common/memory/dreaming/sweeper_e2e_test.go`（改） | helper 加 mode 参数、mtime 改 `os.Chtimes`；用例一律不删 |
| `tests/integration/real_llm/dreaming_real_test.go`（新） | 真模型 1 例 |
| `tests/integration/swarm/agents/harness/common/memory/dreaming/doc.go`（改） | 目录清单补两个新文件 |
| `.integration-scope-exempt`（改） | 删除该目录的豁免条目与注释 |
| `IMPLEMENTATION_PLAN.md`（改） | 7.20 备注追加集成用例计数 |

---

## Task 1: `mockllm` 记录 Invoke 入参

**Files:**
- Modify: `tests/integration/mockllm/mock_client.go:28-33`（`InvokeRecord`）、`:145`（`Invoke` 签名）
- Test: `tests/integration/mockllm/mock_client_test.go`

- [x] **Step 1: 写失败测试**

在 `tests/integration/mockllm/mock_client_test.go` 末尾追加（该文件已有 `//go:build integration`，且已导入 `context` / `model_clients` / `llmschema`，无需补导入）：

```go
// TestMockModelClient_记录Invoke入参 测试 InvokeRecord 保存 messages 入参。
func TestMockModelClient_记录Invoke入参(t *testing.T) {
	client := NewMockModelClient(CreateTextResponse("ok"))
	msgs := model_clients.NewMessagesParam(
		llmschema.NewSystemMessage("sys"),
		llmschema.NewUserMessage("用户提问"),
	)
	if _, err := client.Invoke(context.Background(), msgs); err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}

	rec := client.GetInvokeCall(0)
	if rec == nil {
		t.Fatal("应记录第 1 次调用")
	}
	var userTexts []string
	for _, m := range rec.Messages.Messages() {
		if m.GetRole() == llmschema.RoleTypeUser {
			userTexts = append(userTexts, m.GetContent().Text())
		}
	}
	if len(userTexts) != 1 || userTexts[0] != "用户提问" {
		t.Fatalf("入参记录不正确: %+v", userTexts)
	}
}
```

若文件顶部缺少 `context` / `model_clients` / `llmschema` 导入，一并补上（已存在则跳过本句）。

- [x] **Step 2: 运行确认失败**

```bash
GOPROXY=https://goproxy.cn,direct go test -tags=integration ./tests/integration/mockllm/ -run TestMockModelClient_记录Invoke入参 -v
```

Expected: 编译失败，`rec.Messages` 未定义。

- [x] **Step 3: 实现**

`InvokeRecord` 改为：

```go
// InvokeRecord 记录一次 Invoke 调用，用于测试断言。
// 对齐 Python MockLLMModel.call_history 中的条目。
type InvokeRecord struct {
	// InvokeCount 调用序号（从 1 开始）
	InvokeCount int
	// Messages 本次调用传入的消息参数（用于断言 prompt 内容）
	Messages model_clients.MessagesParam
}
```

`Invoke` 签名去掉占位符并记录入参：

```go
func (m *MockModelClient) Invoke(_ context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callCount++
	record := InvokeRecord{InvokeCount: m.callCount, Messages: messages}
	m.invokeCalls = append(m.invokeCalls, record)
```

（`Stream` 的签名保持不变。）

- [x] **Step 4: 运行确认通过**

```bash
GOPROXY=https://goproxy.cn,direct go test -tags=integration ./tests/integration/mockllm/ -v
GOPROXY=https://goproxy.cn,direct go build ./...
```

Expected: PASS，全仓库编译通过。

- [x] **Step 5: 提交**

```bash
git add tests/integration/mockllm/mock_client.go tests/integration/mockllm/mock_client_test.go
git commit -m "test(mockllm): InvokeRecord 记录 messages 入参，供 prompt 级断言使用"
```

---

## Task 2: `DreamingEnv` 测试环境助手

**Files:**
- Create: `tests/integration/testhelpers/dreaming_env.go`

- [x] **Step 1: 写文件**

```go
//go:build integration

package testhelpers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	swarmdreaming "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory/dreaming"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DreamingEnv dreaming 集成测试的隔离环境。
//
// 在 AdapterTestEnv 之上补充 dreaming 专用的会话播种与断言能力：
//   - SessionsDir 指向 <DataDir>/agent/sessions（与 workspace.AgentSessionsDir 一致）
//   - PlantSession / AppendRounds / Touch 提供确定性的 history.json 与 mtime
//   - QueueJSON / QueueError 预置 extractViaLLM 的模型响应
//   - UserPrompts 抽取每次 Invoke 的 user 文本，用于增量窗口断言
type DreamingEnv struct {
	// AdapterTestEnv 隔离环境（数据目录 + mock provider 注册 + 路径缓存重置）
	*AdapterTestEnv
	// SessionsDir 会话根目录
	SessionsDir string
	// Mock 与注册到 ClientRegistry 的 mock 客户端（响应队列读写）
	Mock *mockllm.MockModelClient
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDreamingEnv 构建隔离环境并写入指向 mock provider 的 config.yaml。
//
// 必须先注册 mock provider 再写 config.yaml（YAML 中 client_provider 引用注册名），
// 该顺序由 NewAdapterTestEnv 保证。
func NewDreamingEnv(t *testing.T, mock *mockllm.MockModelClient, prefix string) *DreamingEnv {
	t.Helper()

	base := NewAdapterTestEnv(t, mock, prefix)
	base.WriteConfigYAML(t, "")

	env := &DreamingEnv{
		AdapterTestEnv: base,
		SessionsDir:    filepath.Join(base.DataDir, "agent", "sessions"),
		Mock:           mock,
	}
	if err := os.MkdirAll(env.SessionsDir, 0o755); err != nil {
		t.Fatalf("创建 sessions 目录失败: %v", err)
	}
	return env
}

// OutputDir 返回 dreaming 输出目录（agent 模式为 memory，code 模式为 coding_memory）。
func (e *DreamingEnv) OutputDir(name string) string {
	return filepath.Join(e.DataDir, "agent", "workspace", name)
}

// Reset 清空 mock 的响应队列与调用计数。
//
// BaseIntegrationSuite 在 SetupSuite 创建 MockLLM，同一套件的多个用例共享该实例，
// 因此每个用例开头必须调用本方法，避免响应队列与计数串味。
func (e *DreamingEnv) Reset() {
	e.Mock.SetResponses()
}

// PlantSession 在 SessionsDir 下创建会话目录：history.json + metadata.json。
// rounds 为对话轮次数，第 i 轮的用户消息为「第i个问题」、助手消息为「这是第i个回答」。
func (e *DreamingEnv) PlantSession(t *testing.T, sid, mode string, rounds int) {
	t.Helper()

	dir := filepath.Join(e.SessionsDir, sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建会话目录 %s 失败: %v", sid, err)
	}

	entries := make([]map[string]any, 0, rounds*2)
	for i := 1; i <= rounds; i++ {
		entries = append(entries, map[string]any{"role": "user", "content": fmt.Sprintf("第%d个问题", i)})
		entries = append(entries, map[string]any{
			"role":       "assistant",
			"content":    fmt.Sprintf("这是第%d个回答", i),
			"event_type": "chat.final",
		})
	}
	e.writeJSON(t, filepath.Join(dir, "history.json"), entries)
	e.writeJSON(t, filepath.Join(dir, "metadata.json"), map[string]any{
		"mode":       mode,
		"session_id": sid,
		"created_at": time.Now().Unix(),
	})
}

// AppendRounds 向已有会话追加 [from, to] 闭区间的新轮次并刷新 mtime。
func (e *DreamingEnv) AppendRounds(t *testing.T, sid string, from, to int) {
	t.Helper()

	path := filepath.Join(e.SessionsDir, sid, "history.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", sid, err)
	}
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("解析 %s 失败: %v", sid, err)
	}
	for i := from; i <= to; i++ {
		entries = append(entries, map[string]any{"role": "user", "content": fmt.Sprintf("第%d个问题", i)})
		entries = append(entries, map[string]any{
			"role":       "assistant",
			"content":    fmt.Sprintf("这是第%d个回答", i),
			"event_type": "chat.final",
		})
	}
	e.writeJSON(t, path, entries)
}

// Touch 把会话的 history.json 与目录 mtime 设为 now+delta（delta 可为负）。
//
// 禁用 time.Sleep 造增量：文件系统 mtime 精度不稳定，显式 Chtimes 才能确定性。
func (e *DreamingEnv) Touch(t *testing.T, sid string, delta time.Duration) {
	t.Helper()

	ts := time.Now().Add(delta)
	dir := filepath.Join(e.SessionsDir, sid)
	if err := os.Chtimes(filepath.Join(dir, "history.json"), ts, ts); err != nil {
		t.Fatalf("设置 %s history.json mtime 失败: %v", sid, err)
	}
	if err := os.Chtimes(dir, ts, ts); err != nil {
		t.Fatalf("设置 %s 目录 mtime 失败: %v", sid, err)
	}
}

// QueueJSON 追加一条 ```json 围栏响应，内容为 [{title, content}, ...]。
//
// 用围栏形式命中 extractViaLLM 的 ```json 正则分支，顺带覆盖该分支。
func (e *DreamingEnv) QueueJSON(t *testing.T, items []map[string]string) {
	t.Helper()

	payload := make([]map[string]string, 0, len(items))
	for _, it := range items {
		payload = append(payload, map[string]string{"title": it["title"], "content": it["content"]})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("序列化知识条目失败: %v", err)
	}
	e.Mock.AddTextResponse("```json\n" + string(body) + "\n```")
}

// QueueError 追加一条错误响应，用于验证 LLM 报错时的落盘语义。
func (e *DreamingEnv) QueueError(err error) {
	e.Mock.AddErrorResponse(err)
}

// UserPrompts 返回每次 Invoke 调用中 user 消息的文本（按调用顺序）。
func (e *DreamingEnv) UserPrompts() []string {
	var out []string
	total := e.Mock.InvokeCallCount()
	for i := 0; i < total; i++ {
		rec := e.Mock.GetInvokeCall(i)
		if rec == nil {
			continue
		}
		var parts []string
		for _, m := range rec.Messages.Messages() {
			if m.GetRole() == llmschema.RoleTypeUser {
				parts = append(parts, m.GetContent().Text())
			}
		}
		out = append(out, strings.Join(parts, "\n"))
	}
	return out
}

// ReadCheckpoint 读取 <outputDir>/.dreams/ingestion-checkpoint.json。
func (e *DreamingEnv) ReadCheckpoint(t *testing.T, outputDir string) swarmdreaming.CheckpointData {
	t.Helper()

	path := filepath.Join(outputDir, ".dreams", "ingestion-checkpoint.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 checkpoint 失败: %v", err)
	}
	var cp swarmdreaming.CheckpointData
	if err := json.Unmarshal(raw, &cp); err != nil {
		t.Fatalf("解析 checkpoint 失败: %v", err)
	}
	return cp
}

// ReadDreamingMD 读取 <outputDir>/DREAMING.md，不存在时返回空串。
func (e *DreamingEnv) ReadDreamingMD(outputDir string) string {
	raw, err := os.ReadFile(filepath.Join(outputDir, "DREAMING.md"))
	if err != nil {
		return ""
	}
	return string(raw)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeJSON 以 UTF-8 JSON 写入文件。
func (e *DreamingEnv) writeJSON(t *testing.T, path string, v any) {
	t.Helper()

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化 %s 失败: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("写入 %s 失败: %v", path, err)
	}
}
```

- [x] **Step 2: 编译验证**

```bash
GOPROXY=https://goproxy.cn,direct go vet -tags=integration ./tests/integration/testhelpers/
```

Expected: 无输出（通过）。

- [x] **Step 3: 提交**

```bash
git add tests/integration/testhelpers/dreaming_env.go
git commit -m "test(testhelpers): 新增 DreamingEnv 隔离环境助手（会话播种/mtime/响应排队/prompt 抽取）"
```

---

## Task 3: 管线用例 1~2（首次扫描 + 跨轮去重）

**Files:**
- Create: `tests/integration/swarm/agents/harness/common/memory/dreaming/sweeper_incremental_test.go`

- [x] **Step 1: 写骨架与用例 1、2**

```go
//go:build integration

package dreaming

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	swarmdreaming "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory/dreaming"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SweeperIncrementalSuite 测试 dreaming 增量管线的端到端语义。
//
// 对齐 Python: jiuwenswarm-develop/tests/system_tests/test_dreaming_e2e.py
// 与 Python 的差别：Python 用 mock.patch 替换 _extract_via_llm；Go 通过
// testhelpers.NewDreamingEnv 注册 mock provider，让 extractViaLLM 真实执行
// （提示词模板 + ```json 围栏 + JSON 解析），模型响应由队列预置。
type SweeperIncrementalSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestSweeperIncrementalSuite(t *testing.T) {
	suite.Run(t, new(SweeperIncrementalSuite))
}

// TestSweep_首次扫描写出知识与检查点 测试一次 sweep 写出 DREAMING.md 与 checkpoint。
// 对齐 Python: TestDreamingE2EAgent.test_sweep_writes_dreaming_md
func (s *SweeperIncrementalSuite) TestSweep_首次扫描写出知识与检查点() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	for _, sid := range []string{"sess-001", "sess-002", "sess-003"} {
		env.PlantSession(s.T(), sid, "agent", 5)
	}
	items := []map[string]string{
		{"title": "用户偏好 Python", "content": "用户倾向于使用 Python 开发"},
		{"title": "项目使用 FastAPI", "content": "当前项目框架是 FastAPI"},
	}
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)

	sweeper := s.newSweeper(env, outDir, "agent")
	sweeper.RunSweep(s.Ctx)

	s.Equal(3, s.MockLLM.InvokeCallCount(), "三个会话各应调用一次模型")

	entries := swarmdreaming.ParseDreamingEntries(env.ReadDreamingMD(outDir))
	s.Len(entries, 2, "两条不同标题应各保留一条")
	s.Contains(env.ReadDreamingMD(outDir), "_source: sess-001", "条目应记录来源会话")

	cp := env.ReadCheckpoint(s.T(), outDir)
	s.Len(cp.ScannedSessions, 3, "三个会话都应写入 checkpoint")
	for _, sid := range []string{"sess-001", "sess-002", "sess-003"} {
		snapshot, ok := cp.ScannedSessions[sid]
		s.True(ok, "%s 应在 checkpoint 中", sid)
		s.Equal(5, snapshot.RoundCount, "%s 轮次应记录为 5", sid)
		s.Greater(snapshot.HistoryMtime, float64(0), "%s mtime 应被记录", sid)
	}
	s.NotEmpty(cp.LastScanTs, "last_scan_ts 不应为空")
}

// TestSweep_第二次同标题不重复 测试跨轮 sweep 的标题去重。
//
// 对齐 Python: TestDreamingE2EAgent.test_dedup_skips_existing_title
// 但修正其空转缺陷：Python 第二轮只新增 2 个会话（<MIN_NEW_SESSIONS=3），
// scan_new_sessions 直接返回空列表，第二轮 sweep 从未执行、断言恒真。
// 本用例第二轮补足 3 个新会话，让门槛放行并真正命中去重分支。
func (s *SweeperIncrementalSuite) TestSweep_第二次同标题不重复() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	for _, sid := range []string{"sess-a", "sess-b", "sess-c"} {
		env.PlantSession(s.T(), sid, "agent", 5)
	}
	first := []map[string]string{{"title": "用户偏好 Python", "content": "第一轮提取"}}
	env.QueueJSON(s.T(), first)
	env.QueueJSON(s.T(), first)
	env.QueueJSON(s.T(), first)

	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(3, s.MockLLM.InvokeCallCount())

	for _, sid := range []string{"sess-002", "sess-003", "sess-004"} {
		env.PlantSession(s.T(), sid, "agent", 5)
	}
	second := []map[string]string{{"title": "用户偏好 Python", "content": "第二轮提取（应被去重）"}}
	env.QueueJSON(s.T(), second)
	env.QueueJSON(s.T(), second)
	env.QueueJSON(s.T(), second)

	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(6, s.MockLLM.InvokeCallCount(), "第二轮应真的执行了三次提取")

	content := env.ReadDreamingMD(outDir)
	s.Equal(1, strings.Count(content, "## 用户偏好 Python"), "同标题只应保留一条")
	s.NotContains(content, "第二轮提取", "去重命中后内容应保持第一轮")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newEnv 构建隔离环境并清空 mock 状态（MockLLM 为套件级共享）。
func (s *SweeperIncrementalSuite) newEnv() *testhelpers.DreamingEnv {
	env := testhelpers.NewDreamingEnv(s.T(), s.MockLLM, "dreaming_itest")
	env.Reset()
	return env
}

// newSweeper 在 env 的会话目录上创建 sweeper 并 Init。
func (s *SweeperIncrementalSuite) newSweeper(env *testhelpers.DreamingEnv, outDir, mode string) *swarmdreaming.Sweeper {
	sweeper := swarmdreaming.NewSweeper(env.SessionsDir, outDir, mode, "zh")
	s.Require().NoError(sweeper.Init())
	return sweeper
}
```

本任务的导入块即最终导入块的子集；后续 Task 4 追加 `time`、Task 5 追加 `os` 与 `path/filepath`、Task 6 追加 `errors`。

- [x] **Step 2: 运行确认通过**

```bash
GOPROXY=https://goproxy.cn,direct go test -tags=integration ./tests/integration/swarm/agents/harness/common/memory/dreaming/ -run 'TestSweeperIncrementalSuite/TestSweep_首次扫描写出知识与检查点|TestSweeperIncrementalSuite/TestSweep_第二次同标题不重复' -v
```

Expected: 两个用例 PASS。若 `InvokeCallCount` 为 0，说明 `config.GetDefaultModels(nil)` 没拿到 mock provider，检查 `NewDreamingEnv` 是否先注册后写 yaml。

- [x] **Step 3: 提交**

```bash
git add tests/integration/swarm/agents/harness/common/memory/dreaming/sweeper_incremental_test.go
git commit -m "test(dreaming_itest): 迁移首次扫描与跨轮标题去重用例"
```

---

## Task 4: 管线用例 3~4（增量窗口 + 未变更跳过）

**Files:**
- Modify: `tests/integration/swarm/agents/harness/common/memory/dreaming/sweeper_incremental_test.go`

- [x] **Step 1: 追加两个用例**

```go
// TestSweep_增量只重扫新轮次 测试已扫会话追加轮次后只压缩新增部分。
//
// 对齐 Python: TestDreamingE2EAgent.test_incremental_scan_reprocesses_updated_session
// 并加强：断言第二轮 prompt 含新问题、不含首轮问题（Python 只断言产物共存）。
func (s *SweeperIncrementalSuite) TestSweep_增量只重扫新轮次() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	for _, sid := range []string{"sess-001", "sess-a", "sess-b"} {
		env.PlantSession(s.T(), sid, "agent", 5)
	}
	first := []map[string]string{{"title": "第一轮知识", "content": "第一次扫描提取"}}
	env.QueueJSON(s.T(), first)
	env.QueueJSON(s.T(), first)
	env.QueueJSON(s.T(), first)
	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(3, s.MockLLM.InvokeCallCount())

	for _, sid := range []string{"sess-001", "sess-a", "sess-b"} {
		env.AppendRounds(s.T(), sid, 6, 9)
		env.Touch(s.T(), sid, 2*time.Second)
	}
	second := []map[string]string{{"title": "第二轮新增知识", "content": "增量扫描提取"}}
	env.QueueJSON(s.T(), second)
	env.QueueJSON(s.T(), second)
	env.QueueJSON(s.T(), second)
	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(6, s.MockLLM.InvokeCallCount(), "三个会话各应重扫一次")

	prompts := env.UserPrompts()
	s.Require().Len(prompts, 6)
	incrementalPrompt := prompts[3]
	s.Contains(incrementalPrompt, "第6个问题", "增量 prompt 应含新轮次")
	s.Contains(incrementalPrompt, "第9个问题", "增量 prompt 应含最后一轮新轮次")
	s.NotContains(incrementalPrompt, "第1个问题", "增量 prompt 不应重复首轮内容")

	content := env.ReadDreamingMD(outDir)
	s.Contains(content, "第一轮知识")
	s.Contains(content, "第二轮新增知识")

	cp := env.ReadCheckpoint(s.T(), outDir)
	s.Equal(9, cp.ScannedSessions["sess-001"].RoundCount, "checkpoint 轮次应推进到 9")
}

// TestSweep_未变更会话跳过 测试无变更时第二次 sweep 不调用模型且不改文件。
//
// 对齐 Python: TestDreamingE2EAgent.test_skips_unchanged_session（断言更强）
func (s *SweeperIncrementalSuite) TestSweep_未变更会话跳过() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	for _, sid := range []string{"sess-001", "sess-a", "sess-b"} {
		env.PlantSession(s.T(), sid, "agent", 5)
	}
	items := []map[string]string{{"title": "初次知识", "content": "第一次扫描"}}
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)
	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(3, s.MockLLM.InvokeCallCount())
	before := env.ReadDreamingMD(outDir)

	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(3, s.MockLLM.InvokeCallCount(), "无变更时不应再调用模型")
	s.Equal(before, env.ReadDreamingMD(outDir), "DREAMING.md 应保持字节级不变")
	s.NotContains(env.ReadDreamingMD(outDir), "第二次扫描不应该发生")
}
```

导入需补 `time`。用例 4 末尾的 `NotContains` 对齐 Python 的"不应该出现"断言（该文案不由本轮 mock 产生，属防御性断言）。

- [x] **Step 2: 运行**

```bash
GOPROXY=https://goproxy.cn,direct go test -tags=integration ./tests/integration/swarm/agents/harness/common/memory/dreaming/ -run 'TestSweeperIncrementalSuite/TestSweep_增量只重扫新轮次|TestSweeperIncrementalSuite/TestSweep_未变更会话跳过' -v
```

Expected: PASS。若增量用例未触发（`InvokeCallCount` 仍为 3），检查 `Touch` 是否把 mtime 推到 checkpoint 记录值之后。

- [x] **Step 3: 提交**

```bash
git add tests/integration/swarm/agents/harness/common/memory/dreaming/sweeper_incremental_test.go
git commit -m "test(dreaming_itest): 增量窗口只重扫新轮次 + 未变更会话零调用"
```

---

## Task 5: 管线用例 5~6（code 模式）

**Files:**
- Modify: `tests/integration/swarm/agents/harness/common/memory/dreaming/sweeper_incremental_test.go`

- [x] **Step 1: 追加两个用例**

```go
// TestSweep_Code模式写consolidated 测试 code 模式写出 consolidated_*.md。
// 对齐 Python: TestDreamingE2ECode.test_sweep_writes_consolidated_files
func (s *SweeperIncrementalSuite) TestSweep_Code模式写consolidated() {
	env := s.newEnv()
	outDir := env.OutputDir("coding_memory")
	for _, sid := range []string{"sess-010", "sess-011", "sess-012"} {
		env.PlantSession(s.T(), sid, "code.normal", 5)
	}
	items := []map[string]string{
		{"title": "Python 性能优化", "content": "使用列表推导式替代 for 循环"},
		{"title": "Git 工作流最佳实践", "content": "推荐使用 feature branch + rebase"},
	}
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)

	s.newSweeper(env, outDir, "code").RunSweep(s.Ctx)
	s.Equal(3, s.MockLLM.InvokeCallCount())

	files, err := filepath.Glob(filepath.Join(outDir, "consolidated_*.md"))
	s.Require().NoError(err)
	s.Len(files, 2, "内容相同的条目按 hash 归并，应只有 2 个文件")
	for _, f := range files {
		raw, readErr := os.ReadFile(f)
		s.Require().NoError(readErr)
		text := string(raw)
		s.Contains(text, "---")
		s.Contains(text, "name:")
		s.Contains(text, "source_session:")
		s.Contains(text, "created_at:")
	}
}

// TestSweep_Code内容哈希去重 测试同内容条目只落一个文件。
// 对齐 Python: TestDreamingE2ECode.test_content_hash_dedup
func (s *SweeperIncrementalSuite) TestSweep_Code内容哈希去重() {
	env := s.newEnv()
	outDir := env.OutputDir("coding_memory")
	for _, sid := range []string{"sess-020", "sess-021", "sess-022"} {
		env.PlantSession(s.T(), sid, "code.normal", 5)
	}
	shared := "完全相同的知识内容用于测试去重"
	items := []map[string]string{
		{"title": "知识点A", "content": shared},
		{"title": "知识点B", "content": shared},
	}
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)

	s.newSweeper(env, outDir, "code").RunSweep(s.Ctx)
	s.Equal(3, s.MockLLM.InvokeCallCount())

	files, err := filepath.Glob(filepath.Join(outDir, "consolidated_*.md"))
	s.Require().NoError(err)
	s.Len(files, 1, "同内容只应产生 1 个文件")
}
```

导入需补 `os`。

- [x] **Step 2: 运行**

```bash
GOPROXY=https://goproxy.cn,direct go test -tags=integration ./tests/integration/swarm/agents/harness/common/memory/dreaming/ -run 'TestSweeperIncrementalSuite/TestSweep_Code' -v
```

Expected: PASS。

- [x] **Step 3: 提交**

```bash
git add tests/integration/swarm/agents/harness/common/memory/dreaming/sweeper_incremental_test.go
git commit -m "test(dreaming_itest): code 模式 consolidated 落盘与内容哈希去重"
```

---

## Task 6: 管线用例 7~8（LLM 报错语义 + 门槛不足）

**Files:**
- Modify: `tests/integration/swarm/agents/harness/common/memory/dreaming/sweeper_incremental_test.go`

- [x] **Step 1: 追加两个用例**

```go
// TestSweep_LLM报错无知识且会话仍记为已扫 测试模型报错时的实际落盘语义。
//
// 对齐 Python 的真实行为（非注释意图）：run_sweep 的 except 分支不可达，
// 因为 _extract_via_llm 内部吞掉全部异常返回 []；Go 的 extractViaLLM 同样把
// 错误降级为 (nil, nil)（sweeper.go S-02）。因此会话仍被记为已扫、不会重试。
func (s *SweeperIncrementalSuite) TestSweep_LLM报错无知识且会话仍记为已扫() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	for _, sid := range []string{"sess-001", "sess-a", "sess-b"} {
		env.PlantSession(s.T(), sid, "agent", 5)
	}
	env.QueueError(errors.New("mock llm failure"))
	env.QueueError(errors.New("mock llm failure"))
	env.QueueError(errors.New("mock llm failure"))

	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(3, s.MockLLM.InvokeCallCount(), "三个会话仍应各尝试一次")
	s.Empty(env.ReadDreamingMD(outDir), "报错时不应产出任何知识")

	cp := env.ReadCheckpoint(s.T(), outDir)
	s.Len(cp.ScannedSessions, 3, "报错视为成功，检查点仍应推进（不会重试）")
	s.Equal(5, cp.ScannedSessions["sess-001"].RoundCount)

	env.Reset()
	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(0, s.MockLLM.InvokeCallCount(), "已扫会话不应重试")
}

// TestSweep_新会话不足3且无过期会话则整轮跳过 测试 MIN_NEW_SESSIONS 门槛。
func (s *SweeperIncrementalSuite) TestSweep_新会话不足3且无过期会话则整轮跳过() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	for _, sid := range []string{"sess-001", "sess-002"} {
		env.PlantSession(s.T(), sid, "agent", 5)
	}
	env.QueueJSON(s.T(), []map[string]string{{"title": "不应产生", "content": "x"}})

	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(0, s.MockLLM.InvokeCallCount(), "2 个新会话 < 3，不应调用模型")
	s.Empty(env.ReadDreamingMD(outDir))
}
```

导入需补 `errors`。用例 7 末尾先 `env.Reset()` 再建新 sweeper，是为了排除上一轮队列残留影响计数断言。

- [x] **Step 2: 运行**

```bash
GOPROXY=https://goproxy.cn,direct go test -tags=integration ./tests/integration/swarm/agents/harness/common/memory/dreaming/ -run 'TestSweeperIncrementalSuite/TestSweep_LLM报错|TestSweeperIncrementalSuite/TestSweep_新会话不足3' -v
```

Expected: PASS。

- [x] **Step 3: 提交**

```bash
git add tests/integration/swarm/agents/harness/common/memory/dreaming/sweeper_incremental_test.go
git commit -m "test(dreaming_itest): LLM 报错语义与 min-new-sessions 门槛"
```

---

## Task 7: 管线用例 9~10（7 天绕过 + 30 天丢弃）

**Files:**
- Modify: `tests/integration/swarm/agents/harness/common/memory/dreaming/sweeper_incremental_test.go`

- [x] **Step 1: 追加两个用例**

```go
// TestSweep_存在7天前会话则绕过门槛 测试 _MIN_SESSION_AGE_BYPASS_DAYS 旁路。
func (s *SweeperIncrementalSuite) TestSweep_存在7天前会话则绕过门槛() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	env.PlantSession(s.T(), "sess-old", "agent", 5)
	env.PlantSession(s.T(), "sess-new", "agent", 5)
	env.Touch(s.T(), "sess-old", -8*24*time.Hour)

	items := []map[string]string{{"title": "旁路知识", "content": "门槛被绕过"}}
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)

	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(2, s.MockLLM.InvokeCallCount(), "存在 8 天旧会话时应绕过 3 会话门槛")
	s.Contains(env.ReadDreamingMD(outDir), "旁路知识")
}

// TestSweep_超30天会话标记已扫并丢弃 测试 _MAX_SESSION_AGE_DAYS 丢弃规则。
//
// 注意 saveCheckpoint 只在有成功会话时落盘，因此需另配 2 个正常会话
// 让检查点真正写出，才能观察被丢弃会话的零值快照。
func (s *SweeperIncrementalSuite) TestSweep_超30天会话标记已扫并丢弃() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	env.PlantSession(s.T(), "sess-ancient", "agent", 5)
	env.PlantSession(s.T(), "sess-b", "agent", 5)
	env.PlantSession(s.T(), "sess-c", "agent", 5)
	env.Touch(s.T(), "sess-ancient", -31*24*time.Hour)

	env.QueueJSON(s.T(), []map[string]string{{"title": "正常知识B", "content": "b"}})
	env.QueueJSON(s.T(), []map[string]string{{"title": "正常知识C", "content": "c"}})

	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(2, s.MockLLM.InvokeCallCount(), "超 30 天会话不应被提取")

	content := env.ReadDreamingMD(outDir)
	s.Contains(content, "正常知识B")
	s.Contains(content, "正常知识C")

	cp := env.ReadCheckpoint(s.T(), outDir)
	snapshot, ok := cp.ScannedSessions["sess-ancient"]
	s.True(ok, "被丢弃的会话应被标记为已扫")
	s.Equal(0, snapshot.RoundCount, "标记为空快照")
	s.Equal(float64(0), snapshot.HistoryMtime, "标记为空快照")
}
```

- [x] **Step 2: 运行**

```bash
GOPROXY=https://goproxy.cn,direct go test -tags=integration ./tests/integration/swarm/agents/harness/common/memory/dreaming/ -run 'TestSweeperIncrementalSuite/TestSweep_存在7天前|TestSweeperIncrementalSuite/TestSweep_超30天' -v
```

Expected: PASS。

- [x] **Step 3: 提交**

```bash
git add tests/integration/swarm/agents/harness/common/memory/dreaming/sweeper_incremental_test.go
git commit -m "test(dreaming_itest): 7 天旁路与 30 天丢弃边界"
```

---

## Task 8: 管线用例 11~12（增量轮次不足 + 模式不匹配）

**Files:**
- Modify: `tests/integration/swarm/agents/harness/common/memory/dreaming/sweeper_incremental_test.go`

- [x] **Step 1: 追加两个用例**

```go
// TestSweep_增量新轮次不足4则跳过 测试增量侧 minSessionRounds 门槛。
func (s *SweeperIncrementalSuite) TestSweep_增量新轮次不足4则跳过() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	for _, sid := range []string{"sess-001", "sess-a", "sess-b"} {
		env.PlantSession(s.T(), sid, "agent", 5)
	}
	items := []map[string]string{{"title": "初次知识", "content": "第一次扫描"}}
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)
	env.QueueJSON(s.T(), items)
	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(3, s.MockLLM.InvokeCallCount())

	for _, sid := range []string{"sess-001", "sess-a", "sess-b"} {
		env.AppendRounds(s.T(), sid, 6, 7)
		env.Touch(s.T(), sid, 2*time.Second)
	}

	s.newSweeper(env, outDir, "agent").RunSweep(s.Ctx)
	s.Equal(3, s.MockLLM.InvokeCallCount(), "新增 2 轮 < 4 轮，应全部跳过")
}

// TestScanNewSessions_模式不匹配跳过 测试 metadata.mode 前缀过滤。
func (s *SweeperIncrementalSuite) TestScanNewSessions_模式不匹配跳过() {
	env := s.newEnv()
	outDir := env.OutputDir("memory")
	for _, sid := range []string{"sess-001", "sess-002", "sess-003"} {
		env.PlantSession(s.T(), sid, "agent", 5)
	}
	env.PlantSession(s.T(), "sess-code", "code.normal", 5)

	sweeper := s.newSweeper(env, outDir, "agent")
	sessions := sweeper.ScanNewSessions()
	s.Len(sessions, 3, "code.normal 会话不应进入 agent 模式的扫描结果")
	for _, sess := range sessions {
		s.NotEqual("sess-code", sess.SessionID)
	}
}
```

- [x] **Step 2: 运行整套管线用例**

```bash
GOPROXY=https://goproxy.cn,direct go test -tags=integration ./tests/integration/swarm/agents/harness/common/memory/dreaming/ -run TestSweeperIncrementalSuite -v
```

Expected: 12 个用例全部 PASS。

- [x] **Step 3: 提交**

```bash
git add tests/integration/swarm/agents/harness/common/memory/dreaming/sweeper_incremental_test.go
git commit -m "test(dreaming_itest): 增量轮次不足与模式前缀过滤边界"
```

---

## Task 9: 接线用例（启停 + code 模式真跑 CreateInstance）

**Files:**
- Create: `tests/integration/swarm/agents/harness/common/memory/dreaming/start_wiring_test.go`

- [x] **Step 1: 写文件**

```go
//go:build integration

package dreaming

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	swarmdreaming "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory/dreaming"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StartWiringSuite 测试 dreaming 的启停生命周期与 adapter→sweeper 接线。
//
// 接线断言落在副作用上：StartDreaming 内部会 Sweeper.Init()，
// 而 Init 会创建 outputDir 与 outputDir/.dreams。断言这两个目录存在，
// 即可证明 adapter 算出的 sessions/output 目录正确、sweeper 真被初始化。
// 不能等待 tick：Orchestrator 首轮 sweep 需等一个 interval（下限 60s），
// 且 tick() 未导出、无法手动驱动。
type StartWiringSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestStartWiringSuite(t *testing.T) {
	suite.Run(t, new(StartWiringSuite))
}

// SetupTest 每个用例前重置 mock 与全局 orchestrator 注册表。
func (s *StartWiringSuite) SetupTest() {
	s.MockLLM.SetResponses()
	swarmdreaming.StopDreaming(s.Ctx, "")
}

// TearDownTest 用例结束后清理全局注册表，避免跨用例串味。
func (s *StartWiringSuite) TearDownTest() {
	swarmdreaming.StopDreaming(s.Ctx, "")
}

// TestStartStop_启用配置 测试启用配置下启动与停止的注册表行为。
// 对齐 Python: TestDreamingE2ELifecycle.test_start_stop
func (s *StartWiringSuite) TestStartStop_启用配置() {
	env := s.newEnv()
	env.Setenv(s.T(), "DREAMING_AGENT_ENABLED", "true")
	outDir := env.OutputDir("memory")

	orch, err := swarmdreaming.StartDreaming(s.Ctx, env.SessionsDir, outDir, "agent", "zh", func() bool { return false })
	s.Require().NoError(err)
	s.Require().NotNil(orch)
	s.Same(orch, swarmdreaming.GetDreamingOrchestrator("agent"), "注册表应返回同一实例")
	s.True(orch.IsRunning())
	s.DirExists(outDir, "启动后 sweeper.Init 应创建输出目录")
	s.DirExists(filepath.Join(outDir, ".dreams"), "启动后应创建 .dreams 目录")

	swarmdreaming.StopDreaming(s.Ctx, "agent")
	s.Nil(swarmdreaming.GetDreamingOrchestrator("agent"), "停止后注册表应为空")
}

// TestStart_禁用配置 测试 enabled=false 时不启动。
// 对齐 Python: TestDreamingE2ELifecycle.test_start_disabled_config
func (s *StartWiringSuite) TestStart_禁用配置() {
	env := s.newEnv()
	env.Setenv(s.T(), "DREAMING_AGENT_ENABLED", "false")

	orch, err := swarmdreaming.StartDreaming(s.Ctx, env.SessionsDir, env.OutputDir("memory"), "agent", "zh", nil)
	s.Require().NoError(err)
	s.Nil(orch, "禁用配置应返回 nil")
	s.Nil(swarmdreaming.GetDreamingOrchestrator("agent"))
}

// TestWiring_CodeAdapter真跑后启动Dreaming 测试 code 模式 adapter 全链路接线。
//
// 覆盖：CreateInstance 设置 dreamingMode=code 与 agent/workspace 目录 →
// TryStartDreaming 用 workspace.AgentSessionsDir 与 coding_memory 输出目录
// 调用 StartDreaming → sweeper.Init 落目录 → TryStopDreaming 注销。
func (s *StartWiringSuite) TestWiring_CodeAdapter真跑后启动Dreaming() {
	env := s.newEnv()
	env.Setenv(s.T(), "DREAMING_CODE_ENABLED", "true")
	s.Equal(workspace.AgentSessionsDir(), env.SessionsDir, "沙箱 sessions 目录应与生产路径一致")
	for _, sid := range []string{"sess-001", "sess-002", "sess-003"} {
		env.PlantSession(s.T(), sid, "code.normal", 5)
	}

	codeAdapter := adapter.NewCodeAdapter()
	s.Require().NoError(codeAdapter.CreateInstance(s.Ctx, map[string]any{"project_dir": filepath.Join(env.DataDir, "project")}, "code", ""))
	s.Require().NoError(codeAdapter.TryStartDreaming(s.Ctx, func() bool { return false }))

	orch := swarmdreaming.GetDreamingOrchestrator("code")
	s.Require().NotNil(orch, "code 模式 orchestrator 应已注册")
	s.True(orch.IsRunning())

	codingMemoryDir := filepath.Join(env.DataDir, "agent", "workspace", "coding_memory")
	s.DirExists(codingMemoryDir, "sweeper.Init 应创建 coding_memory 目录")
	s.DirExists(filepath.Join(codingMemoryDir, ".dreams"), "sweeper.Init 应创建 .dreams 目录")

	s.Require().NoError(codeAdapter.TryStopDreaming(s.Ctx))
	s.Nil(swarmdreaming.GetDreamingOrchestrator("code"))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newEnv 构建隔离环境。
func (s *StartWiringSuite) newEnv() *testhelpers.DreamingEnv {
	return testhelpers.NewDreamingEnv(s.T(), s.MockLLM, "dreaming_wiring")
}
```

- [x] **Step 2: 运行**

```bash
GOPROXY=https://goproxy.cn,direct go test -tags=integration ./tests/integration/swarm/agents/harness/common/memory/dreaming/ -run TestStartWiringSuite -v
```

Expected: 3 个用例 PASS。若 code 模式 `CreateInstance` 失败，按 spec §7 视为生产缺陷：记录现象与堆栈、定位 `internal/swarm/server/adapter/code_adapter.go`、修复后重跑，并在 spec 追加"实施期发现的偏差"小节；不得改为跳过。

- [x] **Step 3: 提交**

```bash
git add tests/integration/swarm/agents/harness/common/memory/dreaming/start_wiring_test.go
git commit -m "test(dreaming_itest): 启停生命周期与 CodeAdapter 真跑 CreateInstance 接线"
```

---

## Task 10: 接线用例（agent 模式真跑 CreateInstance）

**Files:**
- Modify: `tests/integration/swarm/agents/harness/common/memory/dreaming/start_wiring_test.go`

- [x] **Step 1: 追加用例**

```go
// TestWiring_DeepAdapter真跑后启动Dreaming 测试 agent 模式 adapter 全链路接线。
//
// 生产链路（Python _adapter_mode_for_request / Go resolveModeFromRequest）把
// "agent.plan" 拆成 mode="agent" + subMode="plan"，因此 CreateInstance 收到的
// mode 恒为 "agent"：dreamingMode="agent"、env DREAMING_AGENT_ENABLED、
// 输出目录 <DataDir>/agent/workspace/memory。
// 若直接把 "agent.plan" 传给 CreateInstance，Python/Go 的 output_name 判定
// （mode == "agent"）都会落到 coding_memory —— 属测试误用。
func (s *StartWiringSuite) TestWiring_DeepAdapter真跑后启动Dreaming() {
	env := s.newEnv()
	env.Setenv(s.T(), "DREAMING_AGENT_ENABLED", "true")
	for _, sid := range []string{"sess-001", "sess-002", "sess-003"} {
		env.PlantSession(s.T(), sid, "agent.plan", 5)
	}

	deepAdapter := adapter.NewDeepAdapter()
	s.Require().NoError(deepAdapter.CreateInstance(s.Ctx, nil, "agent", ""))
	s.Require().NoError(deepAdapter.TryStartDreaming(s.Ctx, func() bool { return false }))

	orch := swarmdreaming.GetDreamingOrchestrator("agent")
	s.Require().NotNil(orch, "agent 模式 orchestrator 应已注册")
	s.True(orch.IsRunning())

	memoryDir := filepath.Join(env.DataDir, "agent", "workspace", "memory")
	s.DirExists(memoryDir, "sweeper.Init 应创建 memory 目录")
	s.DirExists(filepath.Join(memoryDir, ".dreams"), "sweeper.Init 应创建 .dreams 目录")

	s.Require().NoError(deepAdapter.TryStopDreaming(s.Ctx))
	s.Nil(swarmdreaming.GetDreamingOrchestrator("agent"))
}
```

- [x] **Step 2: 运行**

```bash
GOPROXY=https://goproxy.cn,direct go test -tags=integration ./tests/integration/swarm/agents/harness/common/memory/dreaming/ -run 'TestStartWiringSuite/TestWiring_DeepAdapter' -v
```

Expected: PASS。若 agent 模式 `CreateInstance` 失败：先确认 `config.yaml` 是否缺 `modes` / `react` 段，再定位 `deep_adapter.go` 中缺失的初始化；失败原因与修复记入 spec 偏差小节。**注意 mode 必须用生产归一化后的 `"agent"`**（`resolveModeFromRequest` 从 `agent.plan` 拆出 mode/subMode）；传子模式串会因 `output_name` 判定落到 `coding_memory`，那是测试误用而非生产行为。

- [x] **Step 3: 提交**

```bash
git add tests/integration/swarm/agents/harness/common/memory/dreaming/start_wiring_test.go
git commit -m "test(dreaming_itest): DeepAdapter 真跑 CreateInstance 后启动 dreaming 接线"
```

---

## Task 11: 既有 e2e 用例 helper 调整

**Files:**
- Modify: `tests/integration/swarm/agents/harness/common/memory/dreaming/sweeper_e2e_test.go:574-605`

- [x] **Step 1: 改 helper 签名与 mtime 构造**

`createSessionDir` 增加 `mode` 参数（现有 3 处调用传 `"agent"`）：

```go
// createSessionDir 创建测试用 session 目录，含 history.json 和 metadata.json。
// rounds 指定对话轮次数；mode 指定 metadata.mode（code 模式传 "code.normal"）。
func (s *SweeperE2ESuite) createSessionDir(sessionDir string, rounds int, mode string) {
	s.Require().NoError(os.MkdirAll(sessionDir, 0o755))
	s.createHistoryFile(sessionDir, rounds)

	metadata := map[string]any{
		"session_id":  filepath.Base(sessionDir),
		"mode":        mode,
		"created_at":  time.Now().Format(time.RFC3339),
		"round_count": rounds,
	}
	data, _ := json.Marshal(metadata)
	s.Require().NoError(os.WriteFile(filepath.Join(sessionDir, "metadata.json"), data, 0o644))
}
```

- [x] **Step 2: 更新调用点并编译**

把 `s.createSessionDir(filepath.Join(sessionsDir, sid), 5)` 改为 `s.createSessionDir(filepath.Join(sessionsDir, sid), 5, "agent")`，`createSessionDir(..., 2)` 改为 `createSessionDir(..., 2, "agent")`。

```bash
GOPROXY=https://goproxy.cn,direct go vet -tags=integration ./tests/integration/swarm/agents/harness/common/memory/dreaming/
```

Expected: 无输出。

- [x] **Step 3: 跑既有用例确认无回归**

```bash
GOPROXY=https://goproxy.cn,direct go test -tags=integration ./tests/integration/swarm/agents/harness/common/memory/dreaming/ -run 'TestSweeperE2ESuite|TestStartWiringSuite|TestSweeperIncrementalSuite' -v
```

Expected: 全绿（既有 20 例 + 新增 16 例）。

- [x] **Step 4: 提交**

```bash
git add tests/integration/swarm/agents/harness/common/memory/dreaming/sweeper_e2e_test.go
git commit -m "test(dreaming_itest): createSessionDir 支持 mode 参数，消除 code 模式扫描盲区"
```

---

## Task 12: real-llm 用例

**Files:**
- Create: `tests/integration/real_llm/dreaming_real_test.go`

- [x] **Step 1: 写文件**

```go
//go:build integration && llm

package real_llm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	swarmdreaming "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory/dreaming"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DreamingRealSuite 用真实模型跑一次 dreaming sweep。
//
// 目的：补"提示词能被真实模型消费 + ```json 围栏解析在真输出上生效"的空白。
// 增量语义不在此验证：提示词本身写明"绝大多数情况输出 []"，无法对 title /
// 条目数做精确断言，那部分由 sweeper_incremental_test.go 的 mock 用例承担。
type DreamingRealSuite struct {
	isuite.RealLLMSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestDreamingRealSuite(t *testing.T) {
	suite.Run(t, new(DreamingRealSuite))
}

// TestDreamingReal_真实LLM扫一次 测试真实模型下的 sweep 全流程。
func (s *DreamingRealSuite) TestDreamingReal_真实LLM扫一次() {
	if os.Getenv("RUN_REAL_LLM_TESTS") != "1" {
		s.T().Skip("Real LLM 测试未启用。设置 RUN_REAL_LLM_TESTS=1 开启（对齐 Python 守卫）。")
	}
	s.SkipIfNoRealLLM()

	env := testhelpers.NewAdapterTestEnv(s.T(), mockllm.NewMockModelClient(), "dreaming_real")
	s.writeRealConfig(env.DataDir)

	sessionsDir := filepath.Join(env.DataDir, "agent", "sessions")
	for _, sid := range []string{"sess-001", "sess-002", "sess-003"} {
		s.plantSession(sessionsDir, sid, 5)
	}

	outDir := filepath.Join(env.DataDir, "agent", "workspace", "memory")
	sweeper := swarmdreaming.NewSweeper(sessionsDir, outDir, "agent", "zh")
	s.Require().NoError(sweeper.Init())
	sweeper.RunSweep(s.Ctx)

	cpPath := filepath.Join(outDir, ".dreams", "ingestion-checkpoint.json")
	raw, err := os.ReadFile(cpPath)
	s.Require().NoError(err, "真实模型下 checkpoint 也应落盘")

	var cp swarmdreaming.CheckpointData
	s.Require().NoError(json.Unmarshal(raw, &cp))
	for _, sid := range []string{"sess-001", "sess-002", "sess-003"} {
		s.Contains(cp.ScannedSessions, sid, "%s 应写入 checkpoint", sid)
	}

	dreamingPath := filepath.Join(outDir, "DREAMING.md")
	if data, readErr := os.ReadFile(dreamingPath); readErr == nil {
		s.Contains(string(data), "# Dreaming", "有产出时应带文件头")
	}
}
```

- [x] **Step 2: 追加非导出方法**

在同一文件末尾追加两个方法（`s.Seq` 之外不再需要其他脚手架）：

```go
// writeRealConfig 把 RealLLMSuite 的真实模型配置写入 <dataDir>/config/config.yaml。
//
// extractViaLLM 通过 config.GetDefaultModels(nil) 读取该文件，因此必须落盘。
// 写完需重置 path 缓存，否则 ConfigDir 仍指向旧路径。
func (s *DreamingRealSuite) writeRealConfig(dataDir string) {
	s.T().Helper()

	mcc := s.RealClientConfig
	content := fmt.Sprintf(`models:
  defaults:
    - model_client_config:
        model_name: %s
        client_provider: %s
        api_key: %s
        api_base: %s
        client_id: %s
      is_default: true
`, s.RealModelConfig.ModelName, mcc.ClientProvider, mcc.APIKey, mcc.APIBase, mcc.ClientID)
	path := filepath.Join(dataDir, "config", "config.yaml")
	s.Require().NoError(os.MkdirAll(filepath.Dir(path), 0o755))
	s.Require().NoError(os.WriteFile(path, []byte(content), 0o644))
	pathutil.ResetCache()
}

// plantSession 在 sessionsDir 下写 history.json 与 metadata.json。
func (s *DreamingRealSuite) plantSession(sessionsDir, sid string, rounds int) {
	s.T().Helper()

	dir := filepath.Join(sessionsDir, sid)
	s.Require().NoError(os.MkdirAll(dir, 0o755))
	entries := make([]map[string]any, 0, rounds*2)
	for i := 1; i <= rounds; i++ {
		entries = append(entries, map[string]any{"role": "user", "content": fmt.Sprintf("第%d个问题：Go 里如何做 mtime 断言？", i)})
		entries = append(entries, map[string]any{
			"role":       "assistant",
			"content":    fmt.Sprintf("第%d个回答：用 os.Chtimes 显式设置。", i),
			"event_type": "chat.final",
		})
	}
	data, err := json.Marshal(entries)
	s.Require().NoError(err)
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "history.json"), data, 0o644))

	meta, err := json.Marshal(map[string]any{"mode": "agent.plan", "session_id": sid, "created_at": time.Now().Unix()})
	s.Require().NoError(err)
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "metadata.json"), meta, 0o644))
}
```

`mockllm.NewMockModelClient()` 仅用于满足 `NewAdapterTestEnv` 的签名（真实模型不走注入的 mock，provider 由 `writeRealConfig` 指向真实配置）；不要删除该实参，否则 `NewAdapterTestEnv` 无法注册 provider 名称。

- [x] **Step 3: 编译验证（无凭据时 Skip）**

```bash
GOPROXY=https://goproxy.cn,direct go test -tags='integration llm' ./tests/integration/real_llm/ -run TestDreamingRealSuite -v
```

Expected: SKIP（无 `RUN_REAL_LLM_TESTS`）或 PASS（有凭据）。

- [x] **Step 4: 提交**

```bash
git add tests/integration/real_llm/dreaming_real_test.go
git commit -m "test(real_llm): 真实模型跑通 dreaming sweep 链路"
```

---

## Task 13: 文档、豁免与审计收尾

**Files:**
- Modify: `tests/integration/swarm/agents/harness/common/memory/dreaming/doc.go`
- Modify: `.integration-scope-exempt`
- Modify: `IMPLEMENTATION_PLAN.md:466`（7.20 行的备注列）

- [x] **Step 1: 更新 doc.go 目录清单**

把 doc.go 中的文件目录块整体替换为（注意对齐列，gofmt 会保持 `//` 后 tab 缩进）：

```
//	dreaming/
//	├── doc.go                        # 包文档
//	├── start_wiring_test.go          # 启停生命周期 + adapter 真跑 CreateInstance 接线
//	├── sweeper_e2e_test.go           # Sweeper E2E（Init/Scan/Promote/Checkpoint）
//	└── sweeper_incremental_test.go   # 增量管线（RunSweep 两次扫描/去重/checkpoint 推进）
```

同时把文件头注释的"覆盖"段落补一行：`增量语义（两次 RunSweep 的增量窗口、标题与内容哈希去重、checkpoint 推进）、adapter→sweeper 接线`。

- [x] **Step 2: 删除豁免登记**

删除 `.integration-scope-exempt` 中以下两行及其注释：

```
# Swarm Dreaming 扫描器：覆盖 2 子系统（swarm/agents 248 行 + agentcore/memory 47 行）
tests/integration/swarm/agents/harness/common/memory/dreaming
```

- [x] **Step 3: 更新计划表备注**

`IMPLEMENTATION_PLAN.md` 第 466 行（7.20 行）备注列末尾追加：`；✅ 集成用例 37 个（原有 20 + 12 管线增量 + 4 接线 + 1 real-llm），跨 3 子系统，审计豁免已撤销`。

- [x] **Step 4: 跑审计验证豁免撤销成立**

```bash
GOPROXY=https://goproxy.cn,direct go run ./cmd/integration-audit -root tests/integration/swarm/agents/harness/common/memory/dreaming -defer-span
```

Expected: 该目录 `Exempt=false`、`CallSpan` 包含 `swarm/agents` / `agentcore/memory` / `swarm/server` 三个子系统（`internal/common/*` 按审计规则不计入跨度）、`Passed=true`、无违规。

- [x] **Step 5: 提交**

```bash
git add tests/integration/swarm/agents/harness/common/memory/dreaming/doc.go .integration-scope-exempt IMPLEMENTATION_PLAN.md
git commit -m "docs(dreaming_itest): 目录清单/豁免登记/计划表同步，撤销集成审计豁免"
```

---

## Task 14: 全量回归与远端 CI

- [x] **Step 1: 本地全量集成回归**

```bash
GOPROXY=https://goproxy.cn,direct go test -tags=integration -race ./tests/integration/swarm/agents/harness/common/memory/dreaming/ -v
GOPROXY=https://goproxy.cn,direct go test -tags=integration ./tests/integration/... 2>&1 | tail -40
```

Expected: 全绿，无 race 告警。

- [x] **Step 2: 格式与静态检查**

```bash
gofmt -l tests/integration/mockllm tests/integration/testhelpers tests/integration/swarm/agents/harness/common/memory/dreaming tests/integration/real_llm
GOPROXY=https://goproxy.cn,direct go vet -tags=integration ./tests/integration/...
```

Expected: `gofmt -l` 无输出；`go vet` 无输出。

- [x] **Step 3: 推送并盯 CI**

```bash
git push
gh run list --limit 3
gh run watch <run-id>
```

Expected: Build / Lint / Unit Test / Integration Test 四个 job 全 SUCCESS。红灯则按规则 6 修复后重推，直至全绿。

- [x] **Step 4: 记录收尾**

CI 全绿后在本计划文件顶部追加一行 `**状态：** 已完成（CI run <id> 四绿）`，并提交：

```bash
git add docs/superpowers/plans/2026-10-31-dreaming-incremental-itest.md
git commit -m "docs(plans): dreaming 增量集成测试迁移完结（CI 四绿）"
```

---

## 验收清单

- [x] 12 个管线用例 PASS，每个都断言了 `InvokeCallCount`（无空跑）
- [x] 4 个接线用例 PASS（code 与 agent 两种 `CreateInstance` 均真跑通过；失败已定位修复并记入 spec）
- [x] real-llm 用例在无凭据时 Skip、有凭据时 PASS
- [x] 既有 20 个 e2e 用例零回归，`createSessionDir` 支持 mode
- [x] `.integration-scope-exempt` 条目已删，审计 `Exempt=false` 且跨度 ≥3
- [x] `gofmt -l` / `go vet` 干净
- [x] 远端 CI 四绿
