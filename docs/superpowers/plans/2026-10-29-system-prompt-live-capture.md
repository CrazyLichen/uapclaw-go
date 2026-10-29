# 系统提示词实时捕获集成测试 + Daily Real-LLM CI 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 移植 Python `test_system_prompt_live_capture.py`（真实 LLM 下通过回调捕获完整系统提示词并断言结构 + skill_mode 双分支），并新增每日定时 real-llm CI workflow（北京 02:00 + 手动触发）。

**Architecture:** 测试嵌入 `isuite.RealLLMSuite`（复用 LLM_API_KEY/端点/模型读取），通过 `runner.GetCallbackFramework().OnLLM(LLMInvokeInput/LLMStreamInput)` 在 Model 门面层捕获真实发送的 messages（晚于 RuntimePromptRail 动态注入，覆盖 `# 运行时` 节）。环境用 `UAPCLAW_DATA_DIR` 重定向 + `workspace.Prepare` 模板 + 手写 config.yaml（models.defaults 真实模型 + react.skill_mode）。daily workflow 独立文件，push CI 零改动。

**Tech Stack:** Go testing + testify/suite、callback framework（internal/agentcore/runner/callback）、DeepAdapter（internal/swarm/server/adapter）、GitHub Actions（schedule + workflow_dispatch）。

**设计文档**: `docs/superpowers/specs/2026-10-29-system-prompt-live-capture-design.md`（断言清单 4.6 节 = 实施的唯一权威来源，含机器核对计数）

**关键 API 速查（全部已核实）**:
- 回调注册: `runner.GetCallbackFramework()` → `fw.OnLLM(callback.LLMInvokeInput, fn)` / `fw.OffLLM(...)`（framework.go:179/201，按 %p 匹配）
- 消息提取: `data.Messages.(model_clients.MessagesParam)` → `mp.Messages() []llmschema.BaseMessage` → `m.GetRole() == llmschema.RoleTypeSystem` / `m.GetContent().String()`（message.go:124/184）
- 适配器: `adapter.NewDeepAdapter()`（deep_adapter.go:323）→ `d.CreateInstance(ctx, nil, "agent.plan", "")`（:374，从 config.yaml 建模型缓存）→ `d.ProcessMessageImpl(ctx, req, inputs)`（:802）
- 请求: `swarmschema.NewAgentRequest(requestID, "web", swarmschema.ReqMethodChatSend, params, swarmschema.WithAgentSessionID(sid), swarmschema.WithAgentMetadata(...))`；`Params` 是 `json.RawMessage`
- 用户提示词: `swarmruntime.BuildUserPrompt(query, files, channel, language, trustedDirs, metadata)`（build_user_prompt.go:31）
- 路径重定向: `t.Setenv("UAPCLAW_DATA_DIR", tmp)` + `pathutil.ResetCache()`（paths.go:135）——**必须先于一切路径调用**
- workspace 模板: `workspace.Prepare(workspace.InitOption{WorkspaceDir: tmp, Language: "zh"})`（init.go:204，会写默认 config.yaml，需在其后覆盖）
- 环境变量注入: `LLM_API_KEY`（必填，空 = Fatal 不 Skip——用户 fail-not-skip 决策）、`LLM_MODEL_NAME`（默认 deepseek-v4.1-flash 由 workflow env 提供）、`LLM_API_BASE`（默认 ark coding 端点，已连通验证）

**运行命令**（本地验证用）:
```bash
export GOPROXY=https://goproxy.cn,direct
export LLM_API_KEY=<你的 Ark API Key>
export LLM_MODEL_NAME=deepseek-v4.1-flash
export RUN_REAL_LLM_TESTS=1
CGO_ENABLED=1 go test -mod=mod -tags "sqlite_fts5 test integration llm" -v -run 'TestSystemPromptCapture' ./tests/integration/real_llm/ -timeout 300s
```
（key 属敏感值，命令中的真实 key 仅用于本次实施会话的本地验证；写入计划文档已在用户知情下完成——key 通过 GitHub secret 注入 CI，不进代码库。）

---

### Task 1: 环境装配辅助函数（setupPromptEnv + config.yaml + 测试技能）

**Files:**
- Create: `tests/integration/real_llm/system_prompt_capture_test.go`

- [ ] **Step 1: 创建测试文件骨架（结构体 + 环境装配函数）**

```go
//go:build integration && llm

package real_llm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	swarmruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
	swarmschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SystemPromptCaptureSuite 系统提示词实时捕获测试套件。
// 对齐 Python: tests/system_tests/test_system_prompt_live_capture.py
type SystemPromptCaptureSuite struct {
	isuite.RealLLMSuite
}

// capturedEvent 单次模型调用的捕获结果。
type capturedEvent struct {
	// systemMessages 该次调用中所有 system 角色消息内容
	systemMessages []string
	// toolCount 随请求发送的工具数量
	toolCount int
	// modelName 模型名称
	modelName string
}

// promptCapture LLM_INPUT 等价回调捕获器（线程安全）。
// 对齐 Python: class PromptCapture
type promptCapture struct {
	mu     sync.Mutex
	events []capturedEvent
}
```

- [ ] **Step 2: 实现 setupPromptEnv（三件套装配）**

继续追加到同一文件（非导出函数区）：

```go
// ──────────────────────────── 非导出函数 ────────────────────────────

// setupPromptEnv 构造提示词捕获测试的隔离环境。
//
// 对齐 Python: _sync_prompt_workspace_templates + mock.patch get_config + 临时 HOME：
//  1. UAPCLAW_DATA_DIR 重定向到临时目录（须先 ResetCache 再做任何路径调用）
//  2. workspace.Prepare 复制模板（AGENT/SOUL/IDENTITY/HEARTBEAT/MEMORY.md）
//  3. 覆盖写 config.yaml：真实模型条目 + react.skill_mode + preferred_language
//  4. 自建测试技能 test-skill（Go 仓库无内置技能模板）
//  5. BROWSER_RUNTIME_MCP_ENABLED=true（对齐 Python mock.patch.dict）
func setupPromptEnv(t *testing.T, skillMode string) string {
	t.Helper()

	// key 缺失 = 失败（用户 fail-not-skip 决策，对齐"默认 CI 就要运行"）
	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		t.Fatal("需要配置 LLM_API_KEY 环境变量（CI: GitHub secret LLM_API_KEY；本地: export LLM_API_KEY=...）")
	}

	dataDir := t.TempDir()
	t.Setenv("UAPCLAW_DATA_DIR", dataDir)
	pathutil.ResetCache()

	// workspace 模板（会写默认 config.yaml/.env/agent 结构，随后覆盖 config.yaml）
	if _, err := workspace.Prepare(workspace.InitOption{
		WorkspaceDir: dataDir,
		Language:     "zh",
	}); err != nil {
		t.Fatalf("workspace.Prepare 失败: %v", err)
	}

	// 覆盖写 config.yaml：models.defaults 真实模型 + react.skill_mode
	writeCaptureConfigYAML(t, dataDir, skillMode, apiKey)

	// 测试技能（all 分支断言依赖至少一个技能条目）
	seedTestSkill(t, dataDir)

	// 对齐 Python: mock.patch.dict("os.environ", {"BROWSER_RUNTIME_MCP_ENABLED": "true"})
	t.Setenv("BROWSER_RUNTIME_MCP_ENABLED", "true")

	return dataDir
}

// writeCaptureConfigYAML 覆盖写入测试专用 config.yaml。
//
// 格式对齐 buildModelCacheFromDefaults（deep_adapter.go:2122）与
// resolveSkillMode（deep_adapter_rails.go:301 读 configCache["skill_mode"]，
// configCache = config.yaml 的 react 段，deep_adapter.go:418-428）。
func writeCaptureConfigYAML(t *testing.T, dataDir string, skillMode string, apiKey string) {
	t.Helper()
	cfg := fmt.Sprintf(`preferred_language: zh
react:
  skill_mode: %s
models:
  defaults:
    - is_default: true
      model_client_config:
        client_provider: OpenAI
        model_name: deepseek-v4.1-flash
        api_key: %s
        api_base: https://ark.cn-beijing.volces.com/api/coding/v3
        timeout: 120
`, skillMode, apiKey)
	cfgPath := filepath.Join(dataDir, "config", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatalf("创建 config 目录失败: %v", err)
	}
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("写入 config.yaml 失败: %v", err)
	}
}

// seedTestSkill 自建测试技能，保证 all 分支的技能列表断言确定性。
//
// 对齐 Python: copytree(skills 模板)。Go 仓库 resources 无内置技能模板，
// 自建单个技能（frontmatter name/description + 简短正文）。
func seedTestSkill(t *testing.T, dataDir string) {
	t.Helper()
	skillDir := filepath.Join(dataDir, "agent", "workspace", "skills", "test-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("创建技能目录失败: %v", err)
	}
	content := `---
name: test-skill
description: 测试用技能，用于验证系统提示词的技能列表渲染。
trigger: 用户提到"测试技能"关键词时加载。
---

# 测试技能

这是一个用于系统提示词捕获测试的技能。
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("写入 SKILL.md 失败: %v", err)
	}
}
```

- [ ] **Step 3: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go vet -tags "sqlite_fts5 test integration llm" ./tests/integration/real_llm/ 2>&1 | head -20
```
Expected: 无输出（编译通过；可能有 unused import 警告——后续 Task 会用到全部 import，此时先确认无语法错误即可）

- [ ] **Step 4: Commit**

```bash
git add tests/integration/real_llm/system_prompt_capture_test.go
git commit -m "test(real_llm): 系统提示词捕获测试 - 环境装配函数"
```

---

### Task 2: 回调捕获器（promptCapture.record + 注册/反注册）

**Files:**
- Modify: `tests/integration/real_llm/system_prompt_capture_test.go`

- [ ] **Step 1: 实现捕获器与回调注册**

```go
// record 回调本体。对齐 Python: PromptCapture.on_llm_input。
//
// 捕获点在 Model 门面层（LLMInvokeInput model.go:141 / LLMStreamInput model.go:239），
// 晚于 RuntimePromptRail 的 BeforeModelCall 动态注入，故含 "# 运行时" 节
// （这正是选 Model 门面层而非 ModelCallObserver 的原因，见设计文档 4.2）。
func (c *promptCapture) record(_ context.Context, data *callback.LLMCallEventData) any {
	c.mu.Lock()
	defer c.mu.Unlock()

	ev := capturedEvent{modelName: data.ModelName}

	if mp, ok := data.Messages.(model_clients.MessagesParam); ok {
		for _, m := range mp.Messages() {
			if m.GetRole() == llmschema.RoleTypeSystem {
				ev.systemMessages = append(ev.systemMessages, m.GetContent().String())
			}
		}
	}
	switch tools := data.Tools.(type) {
	case []map[string]any:
		ev.toolCount = len(tools)
	case []any:
		ev.toolCount = len(tools)
	}

	c.events = append(c.events, ev)
	return nil
}

// firstSystemPrompt 返回首个捕获事件的全部 system 消息拼接。
// 对齐 Python: "\n\n".join(first_event["system_messages"])
func (c *promptCapture) firstSystemPrompt() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.events) == 0 {
		return ""
	}
	return strings.Join(c.events[0].systemMessages, "\n\n")
}

// eventCount 返回捕获事件数
func (c *promptCapture) eventCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

// registerPromptCapture 注册双事件回调并挂 cleanup 反注册。
//
// 双注册对齐 Python LLM_INPUT 在 invoke/stream 双路径触发的语义；
// plan 模式实际走哪条路径由 DeepAgent 内部决定，双注册兜底。
func registerPromptCapture(t *testing.T) *promptCapture {
	t.Helper()
	c := &promptCapture{}
	fw := runner.GetCallbackFramework()
	fw.OnLLM(callback.LLMInvokeInput, c.record)
	fw.OnLLM(callback.LLMStreamInput, c.record)
	t.Cleanup(func() {
		fw.OffLLM(callback.LLMInvokeInput, c.record)
		fw.OffLLM(callback.LLMStreamInput, c.record)
	})
	return c
}
```

- [ ] **Step 2: 编译验证**

```bash
go vet -tags "sqlite_fts5 test integration llm" ./tests/integration/real_llm/
```
Expected: 无输出（或仅 unused import 提示）

- [ ] **Step 3: Commit**

```bash
git add tests/integration/real_llm/system_prompt_capture_test.go
git commit -m "test(real_llm): 系统提示词捕获测试 - 回调捕获器"
```

---

### Task 3: 单分支执行链路（runLiveCapture）

**Files:**
- Modify: `tests/integration/real_llm/system_prompt_capture_test.go`

- [ ] **Step 1: 实现 runLiveCapture（完整 DeepAdapter 链路）**

```go
// runLiveCapture 执行单个 skill_mode 分支的完整实时捕获链路。
//
// 对齐 Python: _run_live_capture —— create_instance() → process_message_impl()
// → Runner.run_agent() → rails before_model_call → LLM_INPUT 回调捕获 messages。
//
// 与 Python 的两处有意偏差（见设计文档 4.5，均有理由记录）：
//   - set_checkpoint 不 noop 化：UAPCLAW_DATA_DIR 重定向后 checkpoint 落临时目录，天然无害
//   - load_user_rails 不 noop 化：空数据目录无用户 rails 可加载，自然为空
func (s *SystemPromptCaptureSuite) runLiveCapture(skillMode string) (string, *promptCapture) {
	t := s.T()

	dataDir := setupPromptEnv(t, skillMode)
	_ = dataDir // 环境已装配（t.Setenv + 文件落盘），变量仅供调试参考

	capture := registerPromptCapture(t)

	// 对齐 Python: query = "只回复 PONG。不要调用任何工具。不要解释。"
	query := "只回复 PONG。不要调用任何工具。不要解释。"
	channel := "web"
	sessionID := fmt.Sprintf("%s_prompt_capture_%d", channel, time.Now().UnixNano())
	requestID := fmt.Sprintf("prompt-capture-%d", time.Now().UnixNano())

	// inputs 构造（对齐 Python inputs dict）
	userPrompt := swarmruntime.BuildUserPrompt(query, map[string]any{}, channel, "zh", nil, nil)
	inputs := map[string]any{
		"conversation_id": sessionID,
		"query":           userPrompt,
		"channel":         channel,
		"language":        "zh",
	}

	// AgentRequest 构造（对齐 Python AgentRequest；Params 是 json.RawMessage）
	params, _ := json.Marshal(map[string]any{
		"query": query,
		"mode":  "agent.plan",
		"files": map[string]any{},
	})
	req := swarmschema.NewAgentRequest(
		requestID, channel, swarmschema.ReqMethodChatSend, params,
		swarmschema.WithAgentSessionID(sessionID),
		swarmschema.WithAgentMetadata(map[string]any{
			"source": fmt.Sprintf("system_test_live_capture_%s", skillMode),
		}),
	)

	// DeepAdapter 全链路（无 mock：真实模型 + 真实 rails + 真实回调）
	adapterInst := adapter.NewDeepAdapter()
	ctx, cancel := context.WithTimeout(s.Ctx, 180*time.Second)
	defer cancel()

	if err := adapterInst.CreateInstance(ctx, nil, "agent.plan", ""); err != nil {
		t.Fatalf("CreateInstance 失败: %v", err)
	}
	resp, err := adapterInst.ProcessMessageImpl(ctx, req, inputs)
	if err != nil {
		t.Fatalf("ProcessMessageImpl 失败: %v", err)
	}
	if resp == nil || !resp.OK {
		t.Fatalf("响应异常: resp=%v", resp)
	}

	// 前置断言（对齐 Python 三条）
	if capture.eventCount() < 1 {
		t.Fatal("未捕获到任何 LLM_INPUT 事件")
	}
	prompt := capture.firstSystemPrompt()
	if prompt == "" {
		t.Fatal("system messages 为空")
	}
	return prompt, capture
}
```

**注意**：此步需补 import `"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"`。

- [ ] **Step 2: 编译验证**

```bash
go vet -tags "sqlite_fts5 test integration llm" ./tests/integration/real_llm/
```
Expected: 无输出

- [ ] **Step 3: Commit**

```bash
git add tests/integration/real_llm/system_prompt_capture_test.go
git commit -m "test(real_llm): 系统提示词捕获测试 - 单分支执行链路"
```

---

### Task 4: 断言函数（对齐 Python 4.6 断言清单，机器核对计数）

**Files:**
- Modify: `tests/integration/real_llm/system_prompt_capture_test.go`

- [ ] **Step 1: 实现 assertCommonPromptStructure（公共 20 条）**

```go
// assertCommonPromptStructure 断言公共提示词结构。
// 对齐 Python: _assert_common_prompt_structure（20 条 = 18 正向 + 2 负向，
// 已与 Python 原文机器核对 1:1，见设计文档 4.6 断言清单）
func assertCommonPromptStructure(t *testing.T, prompt string) {
	t.Helper()

	// 工具与使用原则
	s.True(strings.Contains(prompt, "# 安全原则"), "缺少 # 安全原则")
	sTrue := func(substr string, msg string) {
		if !strings.Contains(prompt, substr) {
			t.Errorf("公共结构断言失败: %s（缺少 %q）", msg, substr)
		}
	}
	sFalse := func(substr string, msg string) {
		if strings.Contains(prompt, substr) {
			t.Errorf("公共结构负向断言失败: %s（不应出现 %q）", msg, substr)
		}
	}

	sTrue("# 安全原则", "安全原则节")
	sTrue("# 可用工具", "可用工具节")
	sTrue("- bash: 执行 Shell 命令", "bash 工具描述")
	sTrue("- code: 执行 Python 或 JavaScript 代码", "code 工具描述")
	sTrue("## bash 使用原则", "bash 使用原则小节")
	sTrue("## task_tool 使用原则", "task_tool 使用原则小节")
	sFalse("## task_tool 用于创建临时子代理", "旧版 task_tool 文案不应存在")
	sFalse("- bash / code:", "旧版合并工具描述不应存在")

	// 子代理列表
	sTrue(`- "code_agent":`, "code_agent 子代理描述")
	sTrue(`- "research_agent":`, "research_agent 子代理描述")
	sTrue(`- "browser_agent":`, "browser_agent 子代理描述")

	// 各 section
	sTrue("# 技能", "技能节")
	sTrue("# 持久化存储体系", "持久化存储体系节")
	sTrue("# 消息说明", "消息说明节")
	sTrue("# 工作空间", "工作空间节")
	sTrue("# 项目上下文", "项目上下文节")
	sTrue("使用 todo 工具（todo_create、todo_modify、todo_list）", "todo 工具说明")
	sTrue("# 上下文压缩", "上下文压缩节")
	sTrue("# 当前日期与时间", "当前日期与时间节")
	sTrue("# 运行时", "运行时节（依赖 Model 门面层捕获点）")
}
```

**修正**：上面第一行 `s.True` 是笔误（应为 `sTrue`），且函数签名应接收 suite。落盘时以如下版本为准：

```go
func (s *SystemPromptCaptureSuite) assertCommonPromptStructure(prompt string) {
	t := s.T()
	sTrue := func(substr string, msg string) {
		if !strings.Contains(prompt, substr) {
			t.Errorf("公共结构断言失败: %s（缺少 %q）", msg, substr)
		}
	}
	sFalse := func(substr string, msg string) {
		if strings.Contains(prompt, substr) {
			t.Errorf("公共结构负向断言失败: %s（不应出现 %q）", msg, substr)
		}
	}

	sTrue("# 安全原则", "安全原则节")
	sTrue("# 可用工具", "可用工具节")
	sTrue("- bash: 执行 Shell 命令", "bash 工具描述")
	sTrue("- code: 执行 Python 或 JavaScript 代码", "code 工具描述")
	sTrue("## bash 使用原则", "bash 使用原则小节")
	sTrue("## task_tool 使用原则", "task_tool 使用原则小节")
	sFalse("## task_tool 用于创建临时子代理", "旧版 task_tool 文案不应存在")
	sFalse("- bash / code:", "旧版合并工具描述不应存在")
	sTrue(`- "code_agent":`, "code_agent 子代理描述")
	sTrue(`- "research_agent":`, "research_agent 子代理描述")
	sTrue(`- "browser_agent":`, "browser_agent 子代理描述")
	sTrue("# 技能", "技能节")
	sTrue("# 持久化存储体系", "持久化存储体系节")
	sTrue("# 消息说明", "消息说明节")
	sTrue("# 工作空间", "工作空间节")
	sTrue("# 项目上下文", "项目上下文节")
	sTrue("使用 todo 工具（todo_create、todo_modify、todo_list）", "todo 工具说明")
	sTrue("# 上下文压缩", "上下文压缩节")
	sTrue("# 当前日期与时间", "当前日期与时间节")
	sTrue("# 运行时", "运行时节（依赖 Model 门面层捕获点）")
}
```

- [ ] **Step 2: 实现两个分支断言函数**

```go
// assertSkillModeAutoList 断言 auto_list 分支专属文案（4 条）。
// 对齐 Python: test_live_capture_system_prompt 后半段
func (s *SystemPromptCaptureSuite) assertSkillModeAutoList(prompt string) {
	t := s.T()
	if !strings.Contains(prompt, "- list_skill: 列出可用技能") {
		t.Errorf("auto_list 分支应含 list_skill 工具描述")
	}
	if !strings.Contains(prompt, "需要时先调用 list_skill 查看可用技能") {
		t.Errorf("auto_list 分支应含 list_skill 使用指引")
	}
}

// assertSkillModeAll 断言 all 分支专属文案（6 条）。
// 对齐 Python: test_live_capture_system_prompt_skill_mode_all 后半段
func (s *SystemPromptCaptureSuite) assertSkillModeAll(prompt string) {
	t := s.T()
	if strings.Contains(prompt, "- list_skill: 列出可用技能") {
		t.Errorf("all 分支不应含 list_skill 工具描述")
	}
	if strings.Contains(prompt, "需要时先调用 list_skill 查看可用技能") {
		t.Errorf("all 分支不应含 list_skill 使用指引")
	}
	if !strings.Contains(prompt, "执行前先用 read_file 阅读相关 SKILL.md。") {
		t.Errorf("all 分支应含 SKILL.md 阅读指引")
	}
	if !strings.Contains(prompt, "可用技能：") {
		t.Errorf("all 分支应含技能列表标题")
	}
	// 对齐 Python: re.search(r"\n\d+\.\s+.+?: .+", first_system_prompt)
	if !regexpFindSkillEntry(prompt) {
		t.Errorf("all 分支应含编号技能条目（\\n\\d+\\.\\s+.+?: .+）")
	}
	if !strings.Contains(prompt, "Path: ") {
		t.Errorf("all 分支应含技能路径展示")
	}
}
```

- [ ] **Step 3: 实现正则辅助 + 测试方法主体**

```go
// regexpFindSkillEntry 检查提示词中是否存在编号技能条目。
// 对齐 Python: re.search(r"\n\d+\.\s+.+?: .+", prompt)
func regexpFindSkillEntry(prompt string) bool {
	return skillEntryRegex.MatchString(prompt)
}

// skillEntryRegex 编号技能条目正则（包级编译一次）。
// 对齐 Python: r"\n\d+\.\s+.+?: .+"
var skillEntryRegex = regexp.MustCompile(`\n\d+\.\s+.+?: .+`)
```

注意：需在文件头部 import 区补 `"regexp"`；`skillEntryRegex` 按 Go 源码声明排列规范放在全局变量区（结构体区之后、导出函数区之前）。

两个测试方法（导出函数区）：

```go
// ──────────────────────────── 导出函数 ────────────────────────────

// TestSystemPromptCaptureSuite 运行系统提示词捕获测试套件
// 运行方式: LLM_API_KEY=... RUN_REAL_LLM_TESTS=1 go test -tags="sqlite_fts5 test integration llm" ./tests/integration/real_llm/...
func TestSystemPromptCaptureSuite(t *testing.T) {
	suite.Run(t, new(SystemPromptCaptureSuite))
}

// TestSystemPromptCapture_AutoList分支 实时捕获 auto_list 模式的完整系统提示词
// 对齐 Python: test_live_capture_system_prompt（skill_mode="auto_list"）
func (s *SystemPromptCaptureSuite) TestSystemPromptCapture_AutoList分支() {
	prompt, _ := s.runLiveCapture("auto_list")
	s.assertCommonPromptStructure(prompt)
	s.assertSkillModeAutoList(prompt)
}

// TestSystemPromptCapture_All分支 实时捕获 all 模式的完整系统提示词
// 对齐 Python: test_live_capture_system_prompt_skill_mode_all（skill_mode="all"）
func (s *SystemPromptCaptureSuite) TestSystemPromptCapture_All分支() {
	prompt, _ := s.runLiveCapture("all")
	s.assertCommonPromptStructure(prompt)
	s.assertSkillModeAll(prompt)
}
```

- [ ] **Step 4: gofmt + 编译验证**

```bash
gofmt -w tests/integration/real_llm/system_prompt_capture_test.go && go vet -tags "sqlite_fts5 test integration llm" ./tests/integration/real_llm/
```
Expected: gofmt 无 diff，vet 无输出

- [ ] **Step 5: 断言数机器核对**

```bash
grep -c 'sTrue("' tests/integration/real_llm/system_prompt_capture_test.go
# Expected: 公共 18 + auto_list 2 + all 4 = 24（正则与 Path 用函数封装，Path 单独 grep）
grep -c 'sFalse("' tests/integration/real_llm/system_prompt_capture_test.go
# Expected: 公共 2 + all 2 = 4
```
核对设计文档 4.6：单测试最多 24 条结构断言（公共 20 + auto_list 4 + all 8，其中元断言 mode/skill_mode 由测试方法固定值隐式覆盖）。

- [ ] **Step 6: Commit**

```bash
git add tests/integration/real_llm/system_prompt_capture_test.go
git commit -m "test(real_llm): 系统提示词捕获测试 - 断言函数与测试方法（对齐 Python 24 条断言）"
```

---

### Task 5: 本地真跑验证（真实 LLM）

**Files:**
- 无新文件（运行验证）

- [ ] **Step 1: 本地运行两个测试（真实 ark API）**

```bash
export GOPROXY=https://goproxy.cn,direct
export LLM_API_KEY=<你的 Ark API Key>
export LLM_MODEL_NAME=deepseek-v4.1-flash
export RUN_REAL_LLM_TESTS=1
CGO_ENABLED=1 go test -mod=mod -tags "sqlite_fts5 test integration llm" -v -run 'TestSystemPromptCapture' ./tests/integration/real_llm/ -timeout 600s 2>&1 | tail -40
```
Expected: 两个测试 PASS（含 `=== RUN TestSystemPromptCapture_AutoList分支` / `All分支`）

**调试预期与处置预案**（不通过时按序排查）：
1. `# 运行时` 缺失 → 确认 plan 模式走了 Stream 而非 Invoke（双注册已兜底，若仍缺则检查 RuntimePromptRail 注入时序）
2. `- "code_agent":` 等 subagent 断言失败 → Go 端 plan 模式默认 subagent 列表可能需 config 补段；先 grep `deep_adapter_config.go` 中 subagents 的 config 读取键名，在 writeCaptureConfigYAML 补充
3. 技能列表为空 → 确认 `AgentSkillsDir()` 解析到 `tmp/agent/workspace/skills`（pathutil.ResetCache 后重定向生效）
4. `BROWSER_RUNTIME_MCP_ENABLED` 导致 Fatal → 移除该 t.Setenv（断言不依赖 browser MCP 本体）
5. 文案不匹配 → 逐条 grep Go 端 `internal/agentcore/harness/prompts/sections/*.go` 渲染文案；若 Go 端文案与 Python 断言原文不一致，按"提示词一比一复刻"约束先修生产文案再跑测试

- [ ] **Step 2: 验证 events ≥ 1 与事件捕获数**

在测试通过后确认：捕获器至少记录 1 个事件（runLiveCapture 内置断言），且 system prompt 非空。若要人工审阅捕获的完整提示词（对齐 Python 的 output txt 产物），可临时加 `t.Logf("prompt length: %d", len(prompt))`。

- [ ] **Step 3: Commit（如有调试修复）**

```bash
git add -A tests/integration/real_llm/ && git commit -m "test(real_llm): 系统提示词捕获测试 - 本地真跑调通"
```

---

### Task 6: doc.go 更新 + workflow 落盘

**Files:**
- Modify: `tests/integration/real_llm/doc.go`
- Create: `.github/workflows/real-llm-ci.yml`

- [ ] **Step 1: doc.go 文件目录追加条目**

在 `tests/integration/real_llm/doc.go` 的文件目录树中，`ltm_real_test.go` 行后追加：

```
//	├── system_prompt_capture_test.go  # 系统提示词实时捕获（LLM_INPUT 回调，skill_mode 双分支）
```

同时"对应 Python 代码"段追加：`tests/system_tests/test_system_prompt_live_capture.py`。

- [ ] **Step 2: 创建 real-llm-ci.yml**

```yaml
# Real LLM CI：每日定时运行真实 LLM 集成测试
# 设计文档: docs/superpowers/specs/2026-10-29-system-prompt-live-capture-design.md
# secret 配置: Settings → Secrets and variables → Actions → LLM_API_KEY
name: Real LLM CI

on:
  schedule:
    - cron: '0 18 * * *'    # UTC 18:00 = 北京时间 02:00
  workflow_dispatch: {}       # 手动触发（secret 配好后立即验证）

concurrency:
  group: real-llm-ci
  cancel-in-progress: false   # 每天一次，不取消进行中的运行

jobs:
  real-llm-test:
    name: Real LLM Test
    runs-on: ubuntu-latest
    timeout-minutes: 40       # 防真实 API 卡死（套件内单测试已有 60-180s ctx 超时）
    steps:
      - name: Checkout 代码
        uses: actions/checkout@v6

      - name: 设置 Go 环境
        uses: actions/setup-go@v6
        with:
          go-version: "1.26"
          cache: true

      - name: 运行 real_llm 全量测试
        env:
          CGO_ENABLED: 1
          LLM_API_KEY: ${{ secrets.LLM_API_KEY }}
          RUN_REAL_LLM_TESTS: "1"      # 套件内双守卫（对齐 Python RUN_LIVE_LLM_TESTS）
          LLM_MODEL_NAME: deepseek-v4.1-flash
        run: go test -mod=mod -tags "sqlite_fts5 test integration llm" ./tests/integration/real_llm/...
```

- [ ] **Step 3: YAML 语法验证**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/real-llm-ci.yml'))" && echo "YAML OK"
```
Expected: `YAML OK`

- [ ] **Step 4: Commit**

```bash
git add tests/integration/real_llm/doc.go .github/workflows/real-llm-ci.yml
git commit -m "ci: 新增每日 real-llm 定时 workflow（北京 02:00 + 手动触发）"
```

---

### Task 7: 全量回归 + 推送 + 首跑监控

**Files:**
- 无新文件

- [ ] **Step 1: 全包回归（本地）**

```bash
CGO_ENABLED=1 go test -mod=mod -tags "sqlite_fts5 test integration llm" ./tests/integration/real_llm/... -timeout 900s 2>&1 | tail -5
```
Expected: 全部 PASS（含既有 67 测试 + 新增 2 测试；单测超时即失败——历史耗时 < 15 分钟）

**注意**：跑此命令前 `pgrep -f 'go (build|test)'` 确认无残留进程（Go 编译规则 5）。

- [ ] **Step 2: 推送 main**

```bash
git push origin main
```
Expected: 推送成功。push 不会触发 real-llm-ci（无 push trigger），ci.yml 正常跑（Build/Lint/Unit/Integration 四 job 全绿——新增测试在 `integration && llm` 双 tag 下，push CI 不编译）。

- [ ] **Step 3: 提醒用户配置 secret 并手动触发首跑**

**给用户的话**（实施 agent 直接转述）：
> 请在 GitHub repo Settings → Secrets and variables → Actions 配置 `LLM_API_KEY`（你之前提供的 ark key）。配置后到 Actions → Real LLM CI → Run workflow 手动触发一次验证首跑。之后每天北京 02:00 自动运行。

- [ ] **Step 4: 首跑监控（用户触发后）**

```bash
gh run list --workflow=real-llm-ci.yml --limit 3
gh run watch <run-id>
```
Expected: Real LLM Test job SUCCESS。失败则按 Task 5 调试预案排查（重点：CI 环境差异——无 ~/.uapclaw、runner 时区、网络）。

- [ ] **Step 5: 更新 IMPLEMENTATION_PLAN.md（若领域 11 有对应行）**

检查 `IMPLEMENTATION_PLAN.md` 领域 11 是否有 system_test 移植进度行，如有则在对应行补充本测试状态。**注意**：本测试属 real_llm 定时套件而非 push CI 集成测试，状态标注应区分（对齐 11.15 行的既有写法风格）。

- [ ] **Step 6: 最终 Commit + Push**

```bash
git add IMPLEMENTATION_PLAN.md && git commit -m "docs: 更新领域 11 进度（系统提示词捕获测试 + daily real-llm CI）"
git push origin main
```

---

## Self-Review 记录（计划完成后执行）

1. **Spec 覆盖**：spec §3.1 测试文件 → Task 1-4；§3.2 workflow → Task 6；§3.3 doc.go → Task 6；§4 机制（回调/环境/链路）→ Task 1-3；§4.6 断言清单 → Task 4；§5 daily workflow → Task 6-7；§6 飞行前清单 → 分散在各 Task 的调试预案。缺口：spec §6 第 2 条 skills_state.json 预案在 Task 5 调试预案第 3 条覆盖；§6 第 9 条 subagents 配置在 Task 5 预案第 2 条覆盖。✅ 无缺口
2. **占位符扫描**：Task 4 Step 1 的第一版代码块含笔误（s.True / 函数签名）——已在同 Step 内给出修正版并注明"落盘时以修正版为准"。✅
3. **类型一致性**：`promptCapture`/`capturedEvent`/`registerPromptCapture`/`runLiveCapture`/`assertCommonPromptStructure` 在 Task 1-4 间签名一致（方法接收者 `*SystemPromptCaptureSuite` 或独立函数+`t *testing.T`，修正版统一为 suite 方法）。✅
