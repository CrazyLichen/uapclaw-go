# ToolResultBudgetProcessor 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 5.29 ToolResultBudgetProcessor，按对话轮次控制工具结果的 Token 预算，超预算时从最大的工具结果开始逐个卸载。

**Architecture:** 与 MessageOffloader / MessageSummaryOffloader 平级实现，嵌入 `*processor.BaseProcessor`。在 processor 包补充 `FindAllDialogueRound` 和 `EstimateMessageTokens` 两个公共函数供复用。

**Tech Stack:** Go 1.22+, 项目已有的 context_engine/processor 体系

---

### Task 1: processor/round.go 补充 FindAllDialogueRound

**Files:**
- Modify: `internal/agentcore/context_engine/processor/round.go`
- Test: `internal/agentcore/context_engine/processor/round_test.go`

- [ ] **Step 1: 编写 FindAllDialogueRound 的失败测试**

在 `round_test.go` 末尾追加测试用例。覆盖场景：空消息、单轮完整对话、多轮对话、不完整轮次（有 user 无 assistant）、连续 user 消息组。

```go
// TestFindAllDialogueRound_空消息 验证空消息列表
func TestFindAllDialogueRound_空消息(t *testing.T) {
	result := FindAllDialogueRound(nil)
	if len(result) != 0 {
		t.Errorf("空消息应返回空切片，实际 %d 项", len(result))
	}
}

// TestFindAllDialogueRound_单轮完整对话 验证 user→assistant(无tool_calls) 的轮次
func TestFindAllDialogueRound_单轮完整对话(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	result := FindAllDialogueRound(messages)
	if len(result) != 1 {
		t.Fatalf("轮次数 = %d, want 1", len(result))
	}
	if result[0][0] == nil || *result[0][0] != 0 {
		t.Errorf("轮次1 userIdx = %v, want 0", result[0][0])
	}
	if result[0][1] == nil || *result[0][1] != 1 {
		t.Errorf("轮次1 assistantIdx = %v, want 1", result[0][1])
	}
}

// TestFindAllDialogueRound_含工具调用 验证 user→assistant(tool_calls)→tool→assistant(无tool_calls) 的轮次
func TestFindAllDialogueRound_含工具调用(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("我来查",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{"city":"北京"}`},
			}),
		),
		llm_schema.NewToolMessage("call_1", "晴天"),
		llm_schema.NewAssistantMessage("北京今天晴天"),
	}
	result := FindAllDialogueRound(messages)
	if len(result) != 1 {
		t.Fatalf("轮次数 = %d, want 1", len(result))
	}
	if result[0][0] == nil || *result[0][0] != 0 {
		t.Errorf("userIdx = %v, want 0", result[0][0])
	}
	if result[0][1] == nil || *result[0][1] != 3 {
		t.Errorf("assistantIdx = %v, want 3", result[0][1])
	}
}

// TestFindAllDialogueRound_多轮对话 验证多轮从新到旧排列
func TestFindAllDialogueRound_多轮对话(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("第一轮"),
		llm_schema.NewAssistantMessage("第一轮回答"),
		llm_schema.NewUserMessage("第二轮"),
		llm_schema.NewAssistantMessage("第二轮回答"),
	}
	result := FindAllDialogueRound(messages)
	if len(result) != 2 {
		t.Fatalf("轮次数 = %d, want 2", len(result))
	}
	// 从新到旧：第二轮在前面
	if result[0][0] == nil || *result[0][0] != 2 {
		t.Errorf("最新轮次 userIdx = %v, want 2", result[0][0])
	}
	if result[1][0] == nil || *result[1][0] != 0 {
		t.Errorf("最老轮次 userIdx = %v, want 0", result[1][0])
	}
}

// TestFindAllDialogueRound_不完整轮次 验证有 user 无 assistant 的轮次
func TestFindAllDialogueRound_不完整轮次(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		// 无 assistant 回复
	}
	result := FindAllDialogueRound(messages)
	if len(result) != 1 {
		t.Fatalf("轮次数 = %d, want 1", len(result))
	}
	if result[0][0] == nil || *result[0][0] != 0 {
		t.Errorf("userIdx = %v, want 0", result[0][0])
	}
	if result[0][1] != nil {
		t.Errorf("不完整轮次 assistantIdx 应为 nil，实际 %v", result[0][1])
	}
}

// TestFindAllDialogueRound_连续user消息 验证连续 user 消息被视为同组起始
func TestFindAllDialogueRound_连续user消息(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewUserMessage("再说一遍"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	result := FindAllDialogueRound(messages)
	if len(result) != 1 {
		t.Fatalf("轮次数 = %d, want 1", len(result))
	}
	// 连续 user 消息的第一条是轮次起始
	if result[0][0] == nil || *result[0][0] != 0 {
		t.Errorf("连续 user 组的起始 userIdx = %v, want 0", result[0][0])
	}
	if result[0][1] == nil || *result[0][1] != 2 {
		t.Errorf("assistantIdx = %v, want 2", result[0][1])
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill; export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/ -run TestFindAllDialogueRound -v -count=1 2>&1 | tail -20
```

Expected: 编译失败（FindAllDialogueRound 未定义）

- [ ] **Step 3: 实现 FindAllDialogueRound**

在 `round.go` 的"结构体"区块添加 `DialogueRound` 类型，在"导出函数"区块添加 `FindAllDialogueRound` 函数。逻辑对齐 Python `ContextUtils.find_all_dialogue_round()`：

```go
// ──────────────────────────── 结构体 ────────────────────────────

// DialogueRound 对话轮次，[0]=userIdx, [1]=assistantIdx（nil 表示不完整轮次）。
//
// 一轮对话定义为：user 消息 → 下一条不含 tool_calls 的 assistant 消息。
// 不完整轮次（有 user 无 assistant）的 assistantIdx 为 nil。
//
// 对应 Python: ContextUtils.find_all_dialogue_round() 返回的单个 [user_idx, assistant_idx]
type DialogueRound [2]*int

// ──────────────────────────── 导出函数 ────────────────────────────

// FindAllDialogueRound 查找所有对话轮次边界。
//
// 从后往前扫描消息列表，识别 user → assistant(无 tool_calls) 的轮次。
// 返回从新到旧排列的轮次列表。连续的 user 消息被视为同组的起始。
//
// 对应 Python: ContextUtils.find_all_dialogue_round()
func FindAllDialogueRound(messages []llm_schema.BaseMessage) []DialogueRound {
	var rounds []DialogueRound
	i := len(messages) - 1

	findContiguousUserGroupStart := func(userIdx int) int {
		for userIdx-1 >= 0 && isUserMessage(messages[userIdx-1]) {
			userIdx--
		}
		return userIdx
	}

	for i >= 0 {
		// 查找该轮的 assistant（可能不存在）
		assistantIdx := (*int)(nil)
		roundEnd := i

		// 跳过非 assistant 消息
		for i >= 0 && !isAssistantMessage(messages[i]) {
			i--
		}

		if i >= 0 {
			msg := messages[i]
			hasToolCalls := len(getToolCalls(msg)) > 0

			if !hasToolCalls {
				idx := i
				assistantIdx = &idx
			}
			i--
		} else {
			// 未找到 assistant，将剩余部分视为不完整轮次
			i = roundEnd
		}

		// 查找该轮的 user 消息
		for i >= 0 && !isUserMessage(messages[i]) {
			i--
		}

		if i < 0 {
			break
		}

		foundUserIdx := i
		userIdx := findContiguousUserGroupStart(foundUserIdx)

		// 首轮：查找尾部不完整轮次（最后一个 user 之后还有 user）
		if len(rounds) == 0 {
			for lastIdx := len(messages) - 1; lastIdx > foundUserIdx; lastIdx-- {
				if isUserMessage(messages[lastIdx]) {
					startIdx := findContiguousUserGroupStart(lastIdx)
					rounds = append(rounds, DialogueRound{&startIdx, nil})
					break
				}
			}
		}

		rounds = append(rounds, DialogueRound{&userIdx, assistantIdx})
		i = userIdx - 1
	}

	return rounds
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill; export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/ -run TestFindAllDialogueRound -v -count=1 2>&1 | tail -20
```

Expected: 所有 FindAllDialogueRound 测试通过

- [ ] **Step 5: 确认既有测试不受影响**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill; export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/ -v -count=1 2>&1 | tail -30
```

Expected: 所有测试通过

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/context_engine/processor/round.go internal/agentcore/context_engine/processor/round_test.go
git commit -m "feat(context_engine): 添加 FindAllDialogueRound 对话轮次识别函数（5.29 前置）"
```

---

### Task 2: processor/util.go 补充 EstimateMessageTokens

**Files:**
- Modify: `internal/agentcore/context_engine/processor/util.go`
- Test: `internal/agentcore/context_engine/processor/util_test.go`

- [ ] **Step 1: 编写 EstimateMessageTokens 的失败测试**

在 `util_test.go` 末尾追加：

```go
func TestEstimateMessageTokens_字符串内容(t *testing.T) {
	msg := llm_schema.NewUserMessage("hello world")
	result := EstimateMessageTokens(msg)
	if result != len("hello world")/3 {
		t.Errorf("期望 %d, 实际 %d", len("hello world")/3, result)
	}
}

func TestEstimateMessageTokens_空内容(t *testing.T) {
	msg := llm_schema.NewUserMessage("")
	result := EstimateMessageTokens(msg)
	if result != 0 {
		t.Errorf("期望 0, 实际 %d", result)
	}
}

func TestEstimateMessageTokens_长内容(t *testing.T) {
	content := strings.Repeat("x", 3000)
	msg := llm_schema.NewToolMessage("tc-1", content)
	result := EstimateMessageTokens(msg)
	if result != len(content)/3 {
		t.Errorf("期望 %d, 实际 %d", len(content)/3, result)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill; export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/ -run TestEstimateMessageTokens -v -count=1 2>&1 | tail -10
```

Expected: 编译失败

- [ ] **Step 3: 实现 EstimateMessageTokens**

在 `util.go` 导出函数区块的 `EstimateContentTokens` 后面追加：

```go
// EstimateMessageTokens 估算单条消息的 Token 数。
//
// 优先使用 content 文本估算，为空时尝试 JSON 序列化后估算。
//
// 对应 Python: ContextUtils.estimate_message_tokens()
func EstimateMessageTokens(msg llm_schema.BaseMessage) int {
	if msg == nil {
		return 0
	}
	content := msg.GetContent()
	if content.IsText() {
		text := content.Text()
		if text == "" {
			return 0
		}
		return len(text) / 3
	}
	return EstimateContentTokens(content)
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill; export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/ -run TestEstimateMessageTokens -v -count=1 2>&1 | tail -10
```

Expected: 通过

- [ ] **Step 5: 确认既有测试不受影响**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill; export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/ -v -count=1 2>&1 | tail -30
```

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/context_engine/processor/util.go internal/agentcore/context_engine/processor/util_test.go
git commit -m "feat(context_engine): 添加 EstimateMessageTokens 单条消息 Token 估算函数（5.29 前置）"
```

---

### Task 3: ToolResultBudgetProcessorConfig 配置与构造

**Files:**
- Create: `internal/agentcore/context_engine/processor/offloader/tool_result_budget_processor.go`
- Test: `internal/agentcore/context_engine/processor/offloader/tool_result_budget_processor_test.go`

- [ ] **Step 1: 编写 Config 的失败测试**

创建测试文件，包含 Config 验证测试：

```go
package offloader

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestToolResultBudgetProcessorConfig_默认值(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{}
	_ = cfg.Validate()
	if cfg.TokensThreshold != 50000 {
		t.Errorf("TokensThreshold 期望 50000, 实际 %d", cfg.TokensThreshold)
	}
	if cfg.LargeMessageThreshold != 10000 {
		t.Errorf("LargeMessageThreshold 期望 10000, 实际 %d", cfg.LargeMessageThreshold)
	}
	if cfg.TrimSize != 3000 {
		t.Errorf("TrimSize 期望 3000, 实际 %d", cfg.TrimSize)
	}
	if cfg.ToolNameAllowlist != nil {
		t.Errorf("ToolNameAllowlist 期望 nil, 实际 %v", cfg.ToolNameAllowlist)
	}
	if cfg.OffloadFilePrefix != "ToolResultBudgetProcessor" {
		t.Errorf("OffloadFilePrefix 期望 ToolResultBudgetProcessor, 实际 %s", cfg.OffloadFilePrefix)
	}
	if len(cfg.OffloadMessageTypes) == 0 || cfg.OffloadMessageTypes[0] != "tool" {
		t.Errorf("OffloadMessageTypes 期望 [tool], 实际 %v", cfg.OffloadMessageTypes)
	}
}

func TestToolResultBudgetProcessorConfig_自定义值(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{
		TokensThreshold:       100000,
		LargeMessageThreshold: 5000,
		TrimSize:              500,
		ToolNameAllowlist:     []string{"grep", "read_file"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("期望验证通过，实际错误: %v", err)
	}
	if cfg.TokensThreshold != 100000 {
		t.Errorf("TokensThreshold 期望 100000, 实际 %d", cfg.TokensThreshold)
	}
}

func TestToolResultBudgetProcessorConfig_Validate_TokensThreshold零(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{
		TokensThreshold:       0,
		LargeMessageThreshold: 100,
		TrimSize:              50,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("TokensThreshold=0 期望报错，实际通过")
	}
}

func TestToolResultBudgetProcessorConfig_Validate_LargeMessageThreshold零(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{
		TokensThreshold:       50000,
		LargeMessageThreshold: 0,
		TrimSize:              50,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("LargeMessageThreshold=0 期望报错，实际通过")
	}
}

func TestToolResultBudgetProcessorConfig_Validate_TrimSize零(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{
		TokensThreshold:       50000,
		LargeMessageThreshold: 100,
		TrimSize:              0,
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("TrimSize=0 期望报错，实际通过")
	}
}

func TestNewToolResultBudgetProcessor_正常创建(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{}
	trbp, err := NewToolResultBudgetProcessor(cfg)
	if err != nil {
		t.Fatalf("期望创建成功，实际错误: %v", err)
	}
	if trbp.ProcessorType() != "ToolResultBudgetProcessor" {
		t.Errorf("ProcessorType 期望 ToolResultBudgetProcessor, 实际 %s", trbp.ProcessorType())
	}
}

func TestToolResultBudgetProcessor_SaveLoadState(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{}
	trbp, _ := NewToolResultBudgetProcessor(cfg)
	state := trbp.SaveState()
	if len(state) != 0 {
		t.Errorf("SaveState 期望空 map, 实际 %v", state)
	}
	trbp.LoadState(map[string]any{"key": "value"}) // 不应 panic
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill; export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/offloader/ -run TestToolResultBudgetProcessor -v -count=1 2>&1 | tail -15
```

Expected: 编译失败

- [ ] **Step 3: 实现 ToolResultBudgetProcessorConfig + 结构体 + 构造函数**

创建 `tool_result_budget_processor.go`，包含：Config 结构体 + applyDefaults + Validate + ToolResultBudgetProcessor 结构体 + 构造函数 + ProcessorType + 钩子默认实现 + SaveState/LoadState + init() 注册 + WithSysOption 选项 + 常量（PersistedOutputTag/PersistedOutputClosingTag）。

文件声明顺序遵循项目规范（结构体 → 枚举 → 常量 → 全局变量 → 导出函数 → 非导出函数）。

关键实现要点：
- Config 兼容字段 `MessagesThreshold`/`MessagesToKeep` 用 `int` 零值表示未设置
- `applyDefaults()` 中零值字段设置默认值
- `Validate()` 检查 TokensThreshold > 0、LargeMessageThreshold > 0、TrimSize > 0
- `WithSysOption(op any)` 注入 sysOperation，标注 `⤵️ 9.32 回填`
- `newOffloadHandleAndPath()` 标注 `⤵️ 5.31 回填`（workspaceDir 为空字符串）
- `init()` 注册工厂 `"ToolResultBudgetProcessor"`

完整代码见设计文档，此处为避免冗余不再全文贴出，实现时参照 Python 源码 `tool_result_budget_processor.py` 和 Go 已有的 `message_summary_offloader.go` 模式编写。

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill; export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/offloader/ -run TestToolResultBudgetProcessor -v -count=1 2>&1 | tail -20
```

Expected: Config 测试全部通过

- [ ] **Step 5: 确认既有测试不受影响**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill; export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/offloader/ -v -count=1 2>&1 | tail -30
```

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/context_engine/processor/offloader/tool_result_budget_processor.go internal/agentcore/context_engine/processor/offloader/tool_result_budget_processor_test.go
git commit -m "feat(context_engine): 添加 ToolResultBudgetProcessor 配置与构造函数（5.29）"
```

---

### Task 4: shouldOffloadMessage 与辅助判断方法

**Files:**
- Modify: `internal/agentcore/context_engine/processor/offloader/tool_result_budget_processor.go`
- Modify: `internal/agentcore/context_engine/processor/offloader/tool_result_budget_processor_test.go`

- [ ] **Step 1: 编写 shouldOffloadMessage / isAllowlistedToolMessage / isAlreadyOffloaded 的测试**

在测试文件追加：

```go
func newTestTRBP() *ToolResultBudgetProcessor {
	cfg := &ToolResultBudgetProcessorConfig{
		TokensThreshold:       50000,
		LargeMessageThreshold: 100,
		TrimSize:              20,
	}
	_ = cfg.Validate()
	bp := processor.NewBaseProcessor(cfg)
	return &ToolResultBudgetProcessor{BaseProcessor: bp, config: cfg}
}

func TestTRBP_shouldOffloadMessage_角色不匹配(t *testing.T) {
	p := newTestTRBP()
	msg := llm_schema.NewUserMessage("hello")
	if p.shouldOffloadMessage(msg, nil, nil) {
		t.Fatal("UserMessage 不在 OffloadMessageTypes 中，期望返回 false")
	}
}

func TestTRBP_shouldOffloadMessage_已卸载消息(t *testing.T) {
	p := newTestTRBP()
	msg := schema.NewOffloadToolMessage("tc-1", strings.Repeat("x", 500), "handle", "filesystem")
	if p.shouldOffloadMessage(msg, nil, nil) {
		t.Fatal("已卸载消息不应重复卸载")
	}
}

func TestTRBP_shouldOffloadMessage_非纯文本内容(t *testing.T) {
	p := newTestTRBP()
	// 多模态内容的 ToolMessage 不支持卸载
	msg := llm_schema.NewToolMessage("tc-1", "") // 空内容 → IsText()=true 但 Text()=""
	if p.shouldOffloadMessage(msg, nil, nil) {
		t.Fatal("空文本内容的 ToolMessage 不应卸载")
	}
}

func TestTRBP_shouldOffloadMessage_白名单工具(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{
		TokensThreshold:       50000,
		LargeMessageThreshold: 100,
		TrimSize:              20,
		ToolNameAllowlist:     []string{"important_tool"},
	}
	_ = cfg.Validate()
	bp := processor.NewBaseProcessor(cfg)
	p := &ToolResultBudgetProcessor{BaseProcessor: bp, config: cfg}

	tc := &llm_schema.ToolCall{ID: "tc-1", Name: "important_tool", Arguments: ""}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{tc}))
	tm := llm_schema.NewToolMessage("tc-1", strings.Repeat("x", 500))
	messages := []llm_schema.BaseMessage{am, tm}

	if p.shouldOffloadMessage(tm, messages, nil) {
		t.Fatal("白名单工具消息不应被卸载")
	}
}

func TestTRBP_shouldOffloadMessage_符合卸载条件(t *testing.T) {
	p := newTestTRBP()
	tc := &llm_schema.ToolCall{ID: "tc-1", Name: "grep", Arguments: ""}
	am := llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{tc}))
	tm := llm_schema.NewToolMessage("tc-1", strings.Repeat("x", 500))
	messages := []llm_schema.BaseMessage{am, tm}

	if !p.shouldOffloadMessage(tm, messages, nil) {
		t.Fatal("普通工具的大消息应该被卸载")
	}
}

func TestTRBP_isAlreadyOffloaded(t *testing.T) {
	p := newTestTRBP()
	offloaded := schema.NewOffloadToolMessage("tc-x", "<persisted-output>...", "fake-handle", "filesystem")
	if !p.isAlreadyOffloaded(offloaded) {
		t.Fatal("OffloadToolMessage 应被识别为已卸载")
	}
	normal := llm_schema.NewToolMessage("tc-y", "normal content")
	if p.isAlreadyOffloaded(normal) {
		t.Fatal("普通 ToolMessage 不应被识别为已卸载")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill; export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/offloader/ -run "TestTRBP_should|TestTRBP_isAlready" -v -count=1 2>&1 | tail -15
```

- [ ] **Step 3: 实现 shouldOffloadMessage / isAllowlistedToolMessage / isAlreadyOffloaded / messageSize**

在 `tool_result_budget_processor.go` 非导出函数区块添加：

```go
// shouldOffloadMessage 判断消息是否符合卸载条件。
//
// 5 条规则（全部通过才卸载）：
//  1. 角色在 OffloadMessageTypes 中
//  2. 不是已卸载消息
//  3. content 必须是纯文本（IsText()）
//  4. 不是白名单工具
//  5. 消息大小 > LargeMessageThreshold
//
// 对应 Python: ToolResultBudgetProcessor._should_offload_message()
func (p *ToolResultBudgetProcessor) shouldOffloadMessage(message llm_schema.BaseMessage, contextMessages []llm_schema.BaseMessage, mc iface.ModelContext) bool {
	cfg := p.config

	// 规则 1：角色检查
	roleMatch := false
	role := message.GetRole().String()
	for _, rt := range cfg.OffloadMessageTypes {
		if rt == role {
			roleMatch = true
			break
		}
	}
	if !roleMatch {
		return false
	}

	// 规则 2：已卸载消息不重复卸载
	if p.isAlreadyOffloaded(message) {
		return false
	}

	// 规则 3：content 必须是纯文本
	if !message.GetContent().IsText() {
		return false
	}

	// 规则 4：白名单工具不卸载
	if p.isAllowlistedToolMessage(message, contextMessages) {
		return false
	}

	// 规则 5：大小检查
	return p.messageSize(message, mc) > cfg.LargeMessageThreshold
}

// isAllowlistedToolMessage 检查消息是否为白名单工具的结果。
//
// 白名单内的工具结果不会被卸载。
//
// 对应 Python: ToolResultBudgetProcessor._is_allowlisted_tool_message()
func (p *ToolResultBudgetProcessor) isAllowlistedToolMessage(message llm_schema.BaseMessage, contextMessages []llm_schema.BaseMessage) bool {
	allowlist := p.config.ToolNameAllowlist
	if len(allowlist) == 0 {
		return false
	}
	toolName := processor.ResolveToolNameFromMessage(message, contextMessages)
	if toolName == "" {
		return false
	}
	for _, name := range allowlist {
		if name == toolName {
			return true
		}
	}
	return false
}

// isAlreadyOffloaded 检查消息是否已被卸载。
//
// 对应 Python: ToolResultBudgetProcessor._is_already_offloaded()
func (p *ToolResultBudgetProcessor) isAlreadyOffloaded(message llm_schema.BaseMessage) bool {
	return schema.IsOffloaded(message)
}

// messageSize 计算消息大小用于阈值比较。
//
// 优先使用 Token 计数，降级使用字符数/3。
//
// 对应 Python: ToolResultBudgetProcessor._message_size()
func (p *ToolResultBudgetProcessor) messageSize(message llm_schema.BaseMessage, mc iface.ModelContext) int {
	if mc == nil {
		return processor.EstimateMessageTokens(message)
	}
	tokenCounter := mc.TokenCounter()
	if tokenCounter != nil {
		count, err := tokenCounter.CountMessages([]llm_schema.BaseMessage{message}, "")
		if err == nil && count > 0 {
			return count
		}
	}
	return processor.EstimateMessageTokens(message)
}
```

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill; export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/offloader/ -run "TestTRBP_should|TestTRBP_isAlready" -v -count=1 2>&1 | tail -15
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/context_engine/processor/offloader/tool_result_budget_processor.go internal/agentcore/context_engine/processor/offloader/tool_result_budget_processor_test.go
git commit -m "feat(context_engine): 实现 TRBP shouldOffloadMessage/allowlist/messageSize（5.29）"
```

---

### Task 5: TriggerAddMessages 与 OnAddMessages 核心流程

**Files:**
- Modify: `internal/agentcore/context_engine/processor/offloader/tool_result_budget_processor.go`
- Modify: `internal/agentcore/context_engine/processor/offloader/tool_result_budget_processor_test.go`

- [ ] **Step 1: 编写 TriggerAddMessages 和 OnAddMessages 测试**

```go
func TestTRBP_TriggerAddMessages_低于阈值不触发(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{
		TokensThreshold:       100000,
		LargeMessageThreshold: 100,
		TrimSize:              20,
	}
	trbp, _ := NewToolResultBudgetProcessor(cfg)
	mc := &fakeModelContext{
		messages: []llm_schema.BaseMessage{
			llm_schema.NewUserMessage("short"),
			llm_schema.NewToolMessage("tc-1", "short"),
		},
	}
	triggered, err := trbp.TriggerAddMessages(context.Background(), mc, nil)
	if err != nil {
		t.Fatalf("TriggerAddMessages 返回错误: %v", err)
	}
	if triggered {
		t.Fatal("低于阈值期望不触发")
	}
}

func TestTRBP_TriggerAddMessages_超过阈值触发(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{
		TokensThreshold:       100,
		LargeMessageThreshold: 50,
		TrimSize:              20,
	}
	trbp, _ := NewToolResultBudgetProcessor(cfg)
	tc := &llm_schema.ToolCall{ID: "tc-1", Name: "grep", Arguments: ""}
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("task"),
		llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{tc})),
		llm_schema.NewToolMessage("tc-1", strings.Repeat("x", 600)), // 200 tokens by estimate
		llm_schema.NewAssistantMessage("done"),
	}
	mc := &fakeModelContext{messages: messages}
	triggered, err := trbp.TriggerAddMessages(context.Background(), mc, []llm_schema.BaseMessage{}, iface.WithSysOperation(nil))
	if err != nil {
		t.Fatalf("TriggerAddMessages 返回错误: %v", err)
	}
	if !triggered {
		t.Fatal("超过阈值期望触发")
	}
}

func TestTRBP_OnAddMessages_单轮超预算卸载(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{
		TokensThreshold:       100,
		LargeMessageThreshold: 50,
		TrimSize:              20,
	}
	trbp, _ := NewToolResultBudgetProcessor(cfg)
	tc := &llm_schema.ToolCall{ID: "tc-1", Name: "grep", Arguments: ""}
	contextMessages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("task"),
	}
	messagesToAdd := []llm_schema.BaseMessage{
		llm_schema.NewAssistantMessage("", llm_schema.WithToolCalls([]*llm_schema.ToolCall{tc})),
		llm_schema.NewToolMessage("tc-1", strings.Repeat("x", 600)), // 200 tokens
		llm_schema.NewAssistantMessage("done"),
	}
	mc := &fakeModelContext{messages: contextMessages}
	event, result, err := trbp.OnAddMessages(context.Background(), mc, messagesToAdd, iface.WithSysOperation(nil))
	if err != nil {
		t.Fatalf("OnAddMessages 返回错误: %v", err)
	}
	if event == nil {
		t.Fatal("期望有 ContextEvent，实际为 nil")
	}
	if len(event.MessagesToModify) == 0 {
		t.Fatal("期望有消息被修改")
	}
	// 验证 ToolMessage 被替换为 OffloadMessage
	for _, msg := range result {
		if msg.GetRole() == llm_schema.RoleTypeTool && schema.IsOffloaded(msg) {
			content := msg.GetContent().Text()
			if !strings.Contains(content, PersistedOutputTag) {
				t.Errorf("卸载后的消息内容应包含 %s, 实际: %s", PersistedOutputTag, content[:min(200, len(content))])
			}
		}
	}
}

func TestTRBP_OnAddMessages_无需卸载(t *testing.T) {
	cfg := &ToolResultBudgetProcessorConfig{
		TokensThreshold:       999999,
		LargeMessageThreshold: 10000,
		TrimSize:              20,
	}
	trbp, _ := NewToolResultBudgetProcessor(cfg)
	mc := &fakeModelContext{
		messages: []llm_schema.BaseMessage{llm_schema.NewUserMessage("q1")},
	}
	messagesToAdd := []llm_schema.BaseMessage{llm_schema.NewAssistantMessage("a1")}
	event, result, err := trbp.OnAddMessages(context.Background(), mc, messagesToAdd)
	if err != nil {
		t.Fatalf("OnAddMessages 返回错误: %v", err)
	}
	if event != nil {
		t.Fatal("无需卸载时，期望 ContextEvent 为 nil")
	}
	if len(result) != 1 {
		t.Fatalf("期望透传 1 条消息，实际: %d", len(result))
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill; export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/offloader/ -run "TestTRBP_Trigger|TestTRBP_OnAdd" -v -count=1 2>&1 | tail -20
```

- [ ] **Step 3: 实现 TriggerAddMessages / OnAddMessages 及所有辅助方法**

在 `tool_result_budget_processor.go` 中实现完整的核心流程方法。关键方法清单：

1. `TriggerAddMessages` — 遍历轮次，检查是否有超预算轮次
2. `OnAddMessages` — 遍历轮次，调用 shrinkRoundToBudget 逐轮卸载
3. `OnGetContextWindow` — 透传
4. `TriggerGetContextWindow` — 不触发
5. `iterRoundRanges` — 调用 FindAllDialogueRound 转换为 [2]int 范围
6. `roundBudgetExceeded` — 返回超预算的轮次列表
7. `roundToolResultSize` — 计算单轮内所有 ToolMessage 的 Token 总和
8. `collectRoundCandidates` — 收集单轮内可卸载的候选消息
9. `shrinkRoundToBudget` — 循环卸载最大候选直到符合预算
10. `offloadToolMessage` — 卸载单条工具消息（两阶段构建 persisted-output）
11. `buildPersistedOutputMessage` — 构建 persisted-output 占位符内容
12. `newOffloadHandleAndPath` — 生成卸载句柄和路径（⤵️ 5.31 回填）

实现要点：
- `offloadToolMessage` 中先构建 handle="pending" 的 persistedContent，调用 `p.OffloadMessages()` 后提取实际 handle/type 重建内容
- `shrinkRoundToBudget` 中 candidates 按大小降序排列，卸载最大的
- `OnAddMessages` 中修改 messages 切片中的元素（原地替换），收集修改索引
- `iterRoundRanges` 将 `DialogueRound`（[2]*int）转为 `[2]int`（assistantIdx 为 nil 时用 len(messages)-1）

- [ ] **Step 4: 运行测试验证通过**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill; export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/offloader/ -run "TestTRBP_Trigger|TestTRBP_OnAdd" -v -count=1 2>&1 | tail -20
```

- [ ] **Step 5: 补充 buildPersistedOutputMessage 独立测试**

```go
func TestBuildPersistedOutputMessage_有更多内容(t *testing.T) {
	result := buildPersistedOutputMessage(50000, "handle123", "preview text", true)
	if !strings.Contains(result, PersistedOutputTag) {
		t.Error("缺少开始标签")
	}
	if !strings.Contains(result, PersistedOutputClosingTag) {
		t.Error("缺少结束标签")
	}
	if !strings.Contains(result, "50000") {
		t.Error("缺少原始大小")
	}
	if !strings.Contains(result, "handle123") {
		t.Error("缺少 offload handle")
	}
	if !strings.Contains(result, "preview text") {
		t.Error("缺少预览内容")
	}
	if !strings.Contains(result, "...") {
		t.Error("has_more=true 时应包含省略号")
	}
}

func TestBuildPersistedOutputMessage_无更多内容(t *testing.T) {
	result := buildPersistedOutputMessage(100, "handle456", "short", false)
	if strings.Contains(result, "...") {
		t.Error("has_more=false 时不应包含省略号")
	}
}
```

- [ ] **Step 6: 运行全部 TRBP 测试**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill; export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/offloader/ -run TestTRBP -v -count=1 2>&1 | tail -30
```

- [ ] **Step 7: 确认既有测试不受影响**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill; export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/processor/offloader/ -v -count=1 2>&1 | tail -30
```

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/context_engine/processor/offloader/tool_result_budget_processor.go internal/agentcore/context_engine/processor/offloader/tool_result_budget_processor_test.go
git commit -m "feat(context_engine): 实现 ToolResultBudgetProcessor 核心流程（5.29）"
```

---

### Task 6: doc.go 更新与覆盖率检查

**Files:**
- Modify: `internal/agentcore/context_engine/processor/offloader/doc.go`

- [ ] **Step 1: 更新 doc.go**

将 "后续实现（5.29）" 段落删除，在 "当前实现" 列表新增 TRBP 条目，文件目录新增两个文件。

```go
// Package offloader 提供上下文引擎的消息卸载处理器实现。
//
// 卸载处理器在对话消息数或 Token 数超过阈值时，将大消息的内容裁剪
// 并卸载到文件系统或内存，生成轻量占位符。原始内容可通过 reloader_tool
// 按 offload_handle 取回。
//
// 当前实现：
//   - MessageOffloader：基础裁剪卸载，将大消息截断为 trim_size + 省略标记
//   - MessageSummaryOffloader：LLM 自适应压缩卸载，用 LLM 生成摘要替代简单裁剪，
//     支持抽取式/生成式两种压缩策略，含三级降级机制
//   - ToolResultBudgetProcessor：按轮次控制工具结果 Token 预算，
//     超预算时从最大的工具结果开始逐个卸载，保留前 N 字符预览
//
// 文件目录：
//
//	offloader/
//	├── doc.go                              # 包文档
//	├── message_offloader.go                # MessageOffloader + Config
//	├── message_offloader_test.go           # MessageOffloader 单元测试
//	├── message_summary_offloader.go        # MessageSummaryOffloader + Config
//	├── message_summary_offloader_test.go   # MessageSummaryOffloader 单元测试
//	├── tool_result_budget_processor.go     # ToolResultBudgetProcessor + Config
//	└── tool_result_budget_processor_test.go # ToolResultBudgetProcessor 单元测试
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/offloader/
package offloader
```

- [ ] **Step 2: 运行覆盖率检查**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill; export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/context_engine/processor/offloader/ -count=1 2>&1 | tail -10
```

如果覆盖率低于 85%，补充测试用例直到达标。

- [ ] **Step 3: 运行 context_engine 全包测试**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill; export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/context_engine/... -count=1 2>&1 | tail -20
```

Expected: 全部通过

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/context_engine/processor/offloader/doc.go
git commit -m "docs(context_engine): 更新 offloader/doc.go 文档，新增 TRBP 条目（5.29）"
```

---

### Task 7: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 将 5.29 行状态从 ☐ 改为 ✅**

找到 5.29 行，将 `☐` 改为 `✅`，补充完成详情（参照 5.28 的格式）：

```
| 5.29 | ✅ | ToolResultBudgetProcessor | ✅ ToolResultBudgetProcessorConfig（兼容字段+实际字段+默认值+Validate）；✅ 嵌入 *BaseProcessor 平级实现；✅ TriggerAddMessages（按轮次检查预算）；✅ OnAddMessages（逐轮卸载最大工具结果）；✅ shouldOffloadMessage（5条规则+IsText判断）；✅ isAllowlistedToolMessage（白名单保护）；✅ messageSize（Token计数+字符/3降级）；✅ offloadToolMessage（两阶段persisted-output构建）；✅ buildPersistedOutputMessage；✅ FindAllDialogueRound（processor/round.go 新增）；✅ EstimateMessageTokens（processor/util.go 新增）；✅ init()自动注册；⤵️ 5.31 回填 newOffloadHandleAndPath WorkspaceDir；⤵️ 5.31 回填 OffloadMessages OffloadMessages；⤵️ 9.32 回填 writeOffloadToFile SysOperation；测试覆盖率 ≥ 85% | `openjiuwen/core/context_engine/processor/offloader/` |
```

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 5.29 状态为已完成"
```
