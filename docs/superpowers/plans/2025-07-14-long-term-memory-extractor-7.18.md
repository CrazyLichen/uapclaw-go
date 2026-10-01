# 7.18 LongTermMemoryExtractor 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 LongTermMemoryExtractor——通过 LLM 从对话中提取长期记忆碎片（用户画像、语义记忆、情景记忆），严格对齐 Python `openjiuwen/core/memory/process/extract/`。

**Architecture:** 新建 `memory/process/extract/` 包，包含参数结构体（common.go）和无状态提取函数（extractor.go）；在 `memory/config/` 包新增 `MemoryScopeConfig`。所有 LLM 调用和 JSON 解析模式对齐已有的 `update_checker.go` 先例。

**Tech Stack:** Go 1.22+, 项目内 llm/prompt/output_parsers/prompts 包

---

## 文件清单

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 创建 | `internal/agentcore/memory/config/scope_config.go` | MemoryScopeConfig + DefaultMemoryScopeConfig |
| 创建 | `internal/agentcore/memory/config/scope_config_test.go` | 默认值测试 |
| 创建 | `internal/agentcore/memory/process/extract/doc.go` | 包文档 |
| 创建 | `internal/agentcore/memory/process/extract/common.go` | ExtractMemoryParams + MemoryOperationParams |
| 创建 | `internal/agentcore/memory/process/extract/common_test.go` | 参数结构体基本测试 |
| 创建 | `internal/agentcore/memory/process/extract/extractor.go` | ExtractLongTermMemory + buildTimeContext |
| 创建 | `internal/agentcore/memory/process/extract/extractor_test.go` | 提取器测试（mock LLM + 时间上下文） |
| 修改 | `internal/agentcore/foundation/llm/model.go` | 添加 WithClient ModelOption（测试注入 mock client） |
| 修改 | `internal/agentcore/memory/config/doc.go` | 添加 scope_config.go 到文件目录 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 7.18 状态 ☐ → ✅ |

---

### Task 1: MemoryScopeConfig + 测试

**Files:**
- Create: `internal/agentcore/memory/config/scope_config.go`
- Create: `internal/agentcore/memory/config/scope_config_test.go`
- Modify: `internal/agentcore/memory/config/doc.go`

- [ ] **Step 1: 编写 scope_config.go 失败测试**

```go
package config

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDefaultMemoryScopeConfig 测试默认记忆作用域配置
func TestDefaultMemoryScopeConfig(t *testing.T) {
	cfg := DefaultMemoryScopeConfig()
	if cfg.UserProfileDefinition == "" {
		t.Error("UserProfileDefinition 不应为空")
	}
	if cfg.SemanticMemoryDefinition == "" {
		t.Error("SemanticMemoryDefinition 不应为空")
	}
	if cfg.EpisodicMemoryDefinition == "" {
		t.Error("EpisodicMemoryDefinition 不应为空")
	}
}

// TestDefaultMemoryScopeConfig_对齐Python默认值 测试默认值与 Python 一致
func TestDefaultMemoryScopeConfig_对齐Python默认值(t *testing.T) {
	cfg := DefaultMemoryScopeConfig()
	// Python: user_profile_definition: str = "用户本人的肯定或否定表述（包含不限于基本身份、兴趣偏好、人际关系、资产状况）"
	expectedUserProfile := "用户本人的肯定或否定表述（包含不限于基本身份、兴趣偏好、人际关系、资产状况）"
	if cfg.UserProfileDefinition != expectedUserProfile {
		t.Errorf("UserProfileDefinition = %q, want %q", cfg.UserProfileDefinition, expectedUserProfile)
	}
	// Python: semantic_memory_definition: str = "用户对话中涉及的和时间无明确关系的事实性内容或概念"
	expectedSemantic := "用户对话中涉及的和时间无明确关系的事实性内容或概念"
	if cfg.SemanticMemoryDefinition != expectedSemantic {
		t.Errorf("SemanticMemoryDefinition = %q, want %q", cfg.SemanticMemoryDefinition, expectedSemantic)
	}
	// Python: episodic_memory_definition: str = "用户对话中涉及的和时间有明确关系的事实性内容或概念"
	expectedEpisodic := "用户对话中涉及的和时间有明确关系的事实性内容或概念"
	if cfg.EpisodicMemoryDefinition != expectedEpisodic {
		t.Errorf("EpisodicMemoryDefinition = %q, want %q", cfg.EpisodicMemoryDefinition, expectedEpisodic)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/config/... -run TestDefaultMemoryScopeConfig -v 2>&1 | head -20`
Expected: 编译失败，`DefaultMemoryScopeConfig` 未定义

- [ ] **Step 3: 实现 scope_config.go**

```go
package config

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryScopeConfig 记忆作用域配置，定义各类型记忆的提取规则。
//
// 暂不包含 model_cfg / model_client_cfg / embedding_cfg，这些字段在 7.18 中未使用，
// 后续 7.27 LongTermMemory 回填时补充。
//
// Python: openjiuwen/core/memory/config/config.py (MemoryScopeConfig)
type MemoryScopeConfig struct {
	// UserProfileDefinition 用户画像提取规则定义
	UserProfileDefinition string
	// SemanticMemoryDefinition 语义记忆提取规则定义
	SemanticMemoryDefinition string
	// EpisodicMemoryDefinition 情景记忆提取规则定义
	EpisodicMemoryDefinition string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultMemoryScopeConfig 返回默认记忆作用域配置。
//
// 默认值对齐 Python MemoryScopeConfig 的字段默认值。
// Python: MemoryScopeConfig(user_profile_definition="...", semantic_memory_definition="...", episodic_memory_definition="...")
func DefaultMemoryScopeConfig() *MemoryScopeConfig {
	return &MemoryScopeConfig{
		UserProfileDefinition:   "用户本人的肯定或否定表述（包含不限于基本身份、兴趣偏好、人际关系、资产状况）",
		SemanticMemoryDefinition: "用户对话中涉及的和时间无明确关系的事实性内容或概念",
		EpisodicMemoryDefinition: "用户对话中涉及的和时间有明确关系的事实性内容或概念",
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/config/... -run TestDefaultMemoryScopeConfig -v`
Expected: PASS

- [ ] **Step 5: 更新 config/doc.go 文件目录**

在 `internal/agentcore/memory/config/doc.go` 中，将文件目录更新为：

```
//	config/
//	├── doc.go              # 包文档
//	├── graph_config.go     # 图记忆配置类型（EpisodeType/BaseStrategy/AddMemStrategy/SearchConfig 等）
//	└── scope_config.go     # 记忆作用域配置（MemoryScopeConfig + DefaultMemoryScopeConfig）
```

同时更新包功能概述，追加"以及记忆作用域配置（MemoryScopeConfig）"。

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/memory/config/scope_config.go internal/agentcore/memory/config/scope_config_test.go internal/agentcore/memory/config/doc.go
git commit -m "feat(memory): add MemoryScopeConfig with default values (7.18 prep)"
```

---

### Task 2: process/extract 包骨架（doc.go + common.go + 测试）

**Files:**
- Create: `internal/agentcore/memory/process/extract/doc.go`
- Create: `internal/agentcore/memory/process/extract/common.go`
- Create: `internal/agentcore/memory/process/extract/common_test.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package extract 提供长期记忆提取功能，通过 LLM 从对话中提取用户画像、语义记忆和情景记忆碎片。
//
// 本包对齐 Python openjiuwen/core/memory/process/extract/，包含参数定义和核心提取器。
// 后续 7.19 MemoryAnalyzer 和 Generator 编排器将在此包中补充。
//
// 文件目录：
//
//	extract/
//	├── doc.go           # 包文档
//	├── common.go        # 提取参数结构体（ExtractMemoryParams / MemoryOperationParams）
//	└── extractor.go     # LongTermMemoryExtractor 长期记忆提取器
//
// 对应 Python 代码：openjiuwen/core/memory/process/extract/
package extract
```

- [ ] **Step 2: 编写 common_test.go 失败测试**

```go
package extract

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestExtractMemoryParams_字段赋值 测试 ExtractMemoryParams 字段赋值
func TestExtractMemoryParams_字段赋值(t *testing.T) {
	params := &ExtractMemoryParams{
		UserID:         "user1",
		ScopeID:        "scope1",
		Messages:       nil,
		HistoryMessages: nil,
		BaseModel:      nil,
	}
	if params.UserID != "user1" {
		t.Errorf("UserID = %q, want %q", params.UserID, "user1")
	}
	if params.ScopeID != "scope1" {
		t.Errorf("ScopeID = %q, want %q", params.ScopeID, "scope1")
	}
}

// TestMemoryOperationParams_字段赋值 测试 MemoryOperationParams 字段赋值
func TestMemoryOperationParams_字段赋值(t *testing.T) {
	params := &MemoryOperationParams{
		UserID:        "user1",
		ScopeID:       "scope1",
		MessageMemID:  "msg1",
		Timestamp:     "2026-01-05 10:00:00",
		BaseModel:     nil,
		SemanticStore: nil,
	}
	if params.UserID != "user1" {
		t.Errorf("UserID = %q, want %q", params.UserID, "user1")
	}
	if params.MessageMemID != "msg1" {
		t.Errorf("MessageMemID = %q, want %q", params.MessageMemID, "msg1")
	}
	if params.Timestamp != "2026-01-05 10:00:00" {
		t.Errorf("Timestamp = %q, want %q", params.Timestamp, "2026-01-05 10:00:00")
	}
}

// TestExtractMemoryParams_类型检查 测试消息字段和模型字段的类型
func TestExtractMemoryParams_类型检查(t *testing.T) {
	// 验证 Messages 字段可赋值 []schema.BaseMessage
	var _ []schema.BaseMessage = ExtractMemoryParams{}.Messages
	// 验证 HistoryMessages 字段可赋值 []schema.BaseMessage
	var _ []schema.BaseMessage = ExtractMemoryParams{}.HistoryMessages
	// 验证 BaseModel 字段可赋值 *llm.Model
	var _ *llm.Model = ExtractMemoryParams{}.BaseModel
}

// TestMemoryOperationParams_类型检查 测试模型字段和语义存储字段的类型
func TestMemoryOperationParams_类型检查(t *testing.T) {
	// 验证 BaseModel 字段可赋值 *llm.Model
	var _ *llm.Model = MemoryOperationParams{}.BaseModel
	// 验证 SemanticStore 字段可赋值 any
	var _ any = MemoryOperationParams{}.SemanticStore
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/process/extract/... -run TestExtractMemoryParams -v 2>&1 | head -20`
Expected: 编译失败，`ExtractMemoryParams` 未定义

- [ ] **Step 4: 实现 common.go**

```go
package extract

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExtractMemoryParams 长期记忆提取参数。
//
// Python: openjiuwen/core/memory/process/extract/common.py (ExtractMemoryParams)
type ExtractMemoryParams struct {
	// UserID 用户标识
	UserID string
	// ScopeID 作用域标识
	ScopeID string
	// Messages 当前轮次消息列表
	Messages []schema.BaseMessage
	// HistoryMessages 历史消息列表
	HistoryMessages []schema.BaseMessage
	// BaseModel 基础聊天模型
	BaseModel *llm.Model
}

// MemoryOperationParams 记忆操作参数（UPDATE/DELETE 语义验证时使用）。
//
// 本参数在 7.18 中不被直接使用，但属于 Python common.py 的一部分，
// 后续 7.19+ Generator 的 _handle_memory_with_instruct 需要此参数。
//
// Python: openjiuwen/core/memory/process/extract/common.py (MemoryOperationParams)
type MemoryOperationParams struct {
	// UserID 用户标识
	UserID string
	// ScopeID 作用域标识
	ScopeID string
	// MessageMemID 关联消息 ID
	MessageMemID string
	// Timestamp 时间戳
	Timestamp string
	// BaseModel 基础聊天模型
	BaseModel *llm.Model
	// SemanticStore 语义存储（用于搜索旧记忆做语义验证）
	SemanticStore any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/process/extract/... -v`
Expected: PASS（3 个测试全部通过）

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/memory/process/extract/
git commit -m "feat(memory): add process/extract package with doc.go and common params (7.18)"
```

---

### Task 3: buildTimeContext + 测试

**Files:**
- Create: `internal/agentcore/memory/process/extract/extractor.go`
- Create: `internal/agentcore/memory/process/extract/extractor_test.go`

- [ ] **Step 1: 编写 buildTimeContext 测试**

```go
package extract

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildTimeContext_合法ISO时间 测试合法 ISO 时间戳转换为中文周范围
func TestBuildTimeContext_合法ISO时间(t *testing.T) {
	// 2026-01-06 是周一，周日应为 2026-01-11
	result := buildTimeContext("2026-01-06T10:00:00")
	expected := "2026年1月6日(周一)～2026年1月11日(周日)（即01.06～01.11）"
	if result != expected {
		t.Errorf("buildTimeContext = %q, want %q", result, expected)
	}
}

// TestBuildTimeContext_周中日期 测试周中的日期也能正确计算周范围
func TestBuildTimeContext_周中日期(t *testing.T) {
	// 2026-03-18 是周三，周一应为 2026-03-16，周日应为 2026-03-22
	result := buildTimeContext("2026-03-18T15:30:00")
	expected := "2026年3月16日(周一)～2026年3月22日(周日)（即03.16～03.22）"
	if result != expected {
		t.Errorf("buildTimeContext = %q, want %q", result, expected)
	}
}

// TestBuildTimeContext_周日日期 测试周日日期
func TestBuildTimeContext_周日日期(t *testing.T) {
	// 2026-01-11 是周日，周一应为 2026-01-05（Python: dt.weekday() 对周日返回 6）
	// 注意：Python datetime.weekday() 中周一=0，周日=6
	result := buildTimeContext("2026-01-11T10:00:00")
	expected := "2026年1月5日(周一)～2026年1月11日(周日)（即01.05～01.11）"
	if result != expected {
		t.Errorf("buildTimeContext = %q, want %q", result, expected)
	}
}

// TestBuildTimeContext_跨月周 测试跨月的周范围
func TestBuildTimeContext_跨月周(t *testing.T) {
	// 2026-03-31 是周二，周一应为 2026-03-30，周日应为 2026-04-05
	result := buildTimeContext("2026-03-31T10:00:00")
	expected := "2026年3月30日(周一)～2026年4月5日(周日)（即03.30～04.05）"
	if result != expected {
		t.Errorf("buildTimeContext = %q, want %q", result, expected)
	}
}

// TestBuildTimeContext_空串 测试空串原样返回
func TestBuildTimeContext_空串(t *testing.T) {
	result := buildTimeContext("")
	if result != "" {
		t.Errorf("buildTimeContext('') = %q, want empty string", result)
	}
}

// TestBuildTimeContext_非法格式 测试非法格式原样返回
func TestBuildTimeContext_非法格式(t *testing.T) {
	result := buildTimeContext("not-a-date")
	if result != "not-a-date" {
		t.Errorf("buildTimeContext('not-a-date') = %q, want %q", result, "not-a-date")
	}
}

// TestBuildTimeContext_带时区信息 测试带时区信息的 ISO 时间戳
func TestBuildTimeContext_带时区信息(t *testing.T) {
	// 2026-01-06 是周一
	result := buildTimeContext("2026-01-06T10:00:00+08:00")
	expected := "2026年1月6日(周一)～2026年1月11日(周日)（即01.06～01.11）"
	if result != expected {
		t.Errorf("buildTimeContext = %q, want %q", result, expected)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/process/extract/... -run TestBuildTimeContext -v 2>&1 | head -20`
Expected: 编译失败，`buildTimeContext` 未定义

- [ ] **Step 3: 实现 buildTimeContext（先创建 extractor.go 骨架）**

```go
package extract

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/output_parsers"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractLongTermMemory 从对话消息中提取长期记忆碎片。
//
// 流程：
//  1. 拼接 history_messages + messages 为 reference_str
//  2. 拼接 user 角色消息为 input_msg_str
//  3. scopeConfig 为 nil 时使用默认值
//  4. 调用 buildTimeContext 构建中文周范围
//  5. 通过 PromptApplier 加载 fragment_memory_prompt 模板
//  6. 调用 LLM + JsonOutputParser 解析，最多重试 retries 次
//  7. 返回 map[string]any，预期键：
//     has_explict_instruct / instruct_memories / user_profile / semantic_memory / episodic_memory
//
// Python: LongTermMemoryExtractor.extract_long_term_memory
func ExtractLongTermMemory(
	ctx context.Context,
	params *ExtractMemoryParams,
	timestamp string,
	scopeConfig *config.MemoryScopeConfig,
	retries int,
) (map[string]any, error) {
	// 步骤 1-2：拼接对话上下文（对齐 Python: reference_str / input_msg_str）
	referenceStr := ""
	inputMsgStr := ""
	for _, msg := range params.HistoryMessages {
		name := msg.GetName()
		if name == "" {
			name = string(msg.GetRole())
		}
		referenceStr += fmt.Sprintf("%s: %s\n", name, msg.GetContent().Text())
	}
	for _, msg := range params.Messages {
		name := msg.GetName()
		if name == "" {
			name = string(msg.GetRole())
		}
		referenceStr += fmt.Sprintf("%s: %s\n", name, msg.GetContent().Text())
		if msg.GetRole() == "user" {
			inputMsgStr += fmt.Sprintf("%s: %s\n", name, msg.GetContent().Text())
		}
	}

	// 步骤 3：scopeConfig nil 时使用默认值（对齐 Python: if not scope_config: scope_config = MemoryScopeConfig()）
	if scopeConfig == nil {
		scopeConfig = config.DefaultMemoryScopeConfig()
	}

	// 步骤 4：构建时间上下文（对齐 Python: current_week = LongTermMemoryExtractor._build_time_context(timestamp)）
	currentWeek := buildTimeContext(timestamp)

	// 步骤 5：加载提示词模板（对齐 Python: PromptApplier().apply("fragment_memory_prompt", {...})）
	userPrompt, err := prompts.DefaultApplier().Apply("fragment_memory_prompt", map[string]any{
		"conversation_time":         timestamp,
		"input_messages":            inputMsgStr,
		"reference_messages":        referenceStr,
		"user_profile_definition":   scopeConfig.UserProfileDefinition,
		"semantic_memory_definition": scopeConfig.SemanticMemoryDefinition,
		"episodic_memory_definition": scopeConfig.EpisodicMemoryDefinition,
		"current_week":              currentWeek,
	})
	if err != nil {
		return nil, fmt.Errorf("加载长期记忆提取提示词模板失败: %w", err)
	}

	// 步骤 6：构造消息（对齐 Python: model_input = [{"role": "user", "content": prompt_content}]）
	formatted := prompt.NewPromptTemplate("fragment_memory_prompt_user", userPrompt)
	messages, err := formatted.ToMessages()
	if err != nil {
		return nil, fmt.Errorf("构造长期记忆提取消息失败: %w", err)
	}
	msgsParam := model_clients.NewMessagesParam(messages...)

	// 步骤 7：LLM 调用 + JSON 解析（对齐 Python: for attempt in range(retries)）
	parser := output_parsers.NewJsonOutputParser()

	for attempt := 0; attempt < retries; attempt++ {
		response, invokeErr := params.BaseModel.Invoke(ctx, msgsParam,
			model_clients.WithInvokeOutputParser(parser))
		if invokeErr != nil {
			// 对齐 Python: invoke 异常向上传播（不在 except JSONDecodeError 内）
			return nil, fmt.Errorf("长期记忆提取 LLM 调用失败: %w", invokeErr)
		}

		parsedResult := response.ParserContent
		if parsedResult == nil {
			if attempt < retries-1 {
				continue
			}
			// 对齐 Python: 全部重试失败返回空 dict
			logger.Error(logComponent).
				Str("event_type", "MEMORY_PROCESS").
				Msg("长期记忆提取模型输出格式错误")
			return map[string]any{}, nil
		}

		result, ok := parsedResult.(map[string]any)
		if !ok {
			if attempt < retries-1 {
				continue
			}
			logger.Error(logComponent).
				Str("event_type", "MEMORY_PROCESS").
				Msg("长期记忆提取模型输出格式错误")
			return map[string]any{}, nil
		}

		return result, nil
	}

	return map[string]any{}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildTimeContext 将 ISO 时间戳转换为中文周范围字符串。
//
// 输出格式: "{年}年{月}月{日}日(周一)～{年}年{月}月{日}日(周日)（即{MM.DD}～{MM.DD}）"
// 解析失败时原样返回 timestamp（对齐 Python: except (ValueError, TypeError): return timestamp）。
//
// Python: LongTermMemoryExtractor._build_time_context
func buildTimeContext(timestamp string) string {
	// 尝试多种格式解析（对齐 Python: datetime.fromisoformat）
	formats := []string{
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	var dt time.Time
	var parseErr error
	for _, layout := range formats {
		dt, parseErr = time.Parse(layout, timestamp)
		if parseErr == nil {
			break
		}
	}
	if parseErr != nil {
		// 对齐 Python: except (ValueError, TypeError): return timestamp
		return timestamp
	}

	// 对齐 Python: monday = dt - timedelta(days=dt.weekday())
	// Go 中 time.Weekday() 周日=0，Python 中 weekday() 周一=0
	// 需要转换：Python weekday() = (Go Weekday() + 6) % 7
	weekday := int(dt.Weekday())
	pyWeekday := (weekday + 6) % 7 // 0=周一, 6=周日，对齐 Python
	monday := dt.AddDate(0, 0, -pyWeekday)
	sunday := monday.AddDate(0, 0, 6)

	// 对齐 Python 输出格式：
	// f"{monday.year}年{monday.month}月{monday.day}日(周一)～"
	// f"{sunday.year}年{sunday.month}月{sunday.day}日(周日)"
	// f"（即{monday.strftime('%m.%d')}～{sunday.strftime('%m.%d')}）"
	return fmt.Sprintf("%d年%d月%d日(周一)～%d年%d月%d日(周日)（即%s～%s）",
		monday.Year(), monday.Month(), monday.Day(),
		sunday.Year(), sunday.Month(), sunday.Day(),
		monday.Format("01.02"), sunday.Format("01.02"))
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/process/extract/... -run TestBuildTimeContext -v`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/process/extract/extractor.go internal/agentcore/memory/process/extract/extractor_test.go
git commit -m "feat(memory): add buildTimeContext with tests (7.18)"
```

---

### Task 4: ExtractLongTermMemory + mock LLM 测试

**Files:**
- Modify: `internal/agentcore/memory/process/extract/extractor_test.go`

- [ ] **Step 1: 在 llm/model.go 中添加 WithClient ModelOption（测试所需）**

`Model.client` 是未导出字段，测试需要注入 fake client。添加一个 `WithClient` 选项函数：

在 `internal/agentcore/foundation/llm/model.go` 的导出函数区域，`WithCallbackFramework` 下方追加：

```go
// WithClient 设置自定义底层客户端（主要用于测试注入 mock）。
func WithClient(client model_clients.BaseModelClient) ModelOption {
	return func(m *Model) { m.client = client }
}
```

- [ ] **Step 2: 在 extractor_test.go 中添加 mock LLM 和 ExtractLongTermMemory 测试**

```go
package extract

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockLLMClient 模拟 BaseModelClient，记录 Invoke 调用参数。
// 对齐项目已有模式：internal/agentcore/context_evolver/service/llm_wrapper_test.go (mockLLMClient)
type mockLLMClient struct {
	// invokeFn 自定义 Invoke 行为，nil 时返回默认成功响应
	invokeFn func(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestExtractLongTermMemory_合法JSON返回 测试 LLM 返回合法 JSON
func TestExtractLongTermMemory_合法JSON返回(t *testing.T) {
	// 构造 mock 模型返回的 JSON（对齐 Python: _fragment_response 输出格式）
	jsonResponse := `{
		"has_explict_instruct": false,
		"instruct_memories": [],
		"user_profile": ["用户喜欢编程"],
		"semantic_memory": ["Go 语言支持泛型"],
		"episodic_memory": ["用户今天学习了 Go 泛型"]
	}`

	model := newFakeModelWithResponse(t, jsonResponse)

	messages := []llmschema.BaseMessage{
		llmschema.NewUserMessage("我今天学习了 Go 泛型"),
	}
	historyMessages := []llmschema.BaseMessage{
		llmschema.NewUserMessage("你好"),
		llmschema.NewAssistantMessage("你好！有什么可以帮你的？"),
	}

	params := &ExtractMemoryParams{
		UserID:          "user1",
		ScopeID:         "scope1",
		Messages:        messages,
		HistoryMessages: historyMessages,
		BaseModel:       model,
	}

	result, err := ExtractLongTermMemory(context.Background(), params, "2026-01-06 10:00:00", nil, 3)
	if err != nil {
		t.Fatalf("ExtractLongTermMemory 返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("result 不应为 nil")
	}

	// 验证返回的 map 包含预期键
	if _, ok := result["user_profile"]; !ok {
		t.Error("结果缺少 user_profile 键")
	}
	if _, ok := result["semantic_memory"]; !ok {
		t.Error("结果缺少 semantic_memory 键")
	}
	if _, ok := result["episodic_memory"]; !ok {
		t.Error("结果缺少 episodic_memory 键")
	}
}

// TestExtractLongTermMemory_解析失败返回空map 测试 LLM 返回非法 JSON 时重试后返回空 map
func TestExtractLongTermMemory_解析失败返回空map(t *testing.T) {
	// 构造一个始终返回非法 JSON 的模型
	model := newFakeModelWithResponse(t, "not valid json {{{")

	params := &ExtractMemoryParams{
		UserID:          "user1",
		ScopeID:         "scope1",
		Messages:        []llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		HistoryMessages: nil,
		BaseModel:       model,
	}

	result, err := ExtractLongTermMemory(context.Background(), params, "2026-01-06 10:00:00", nil, 2)
	if err != nil {
		t.Fatalf("ExtractLongTermMemory 不应返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("result 不应为 nil")
	}
	if len(result) != 0 {
		t.Errorf("解析失败时应返回空 map，实际 %d 个键", len(result))
	}
}

// TestExtractLongTermMemory_Invoke错误向上传播 测试 LLM Invoke 错误直接返回
func TestExtractLongTermMemory_Invoke错误向上传播(t *testing.T) {
	// 构造一个 Invoke 直接返回错误的模型
	model := newFakeModelWithInvokeErr(fmt.Errorf("API 调用失败"))

	params := &ExtractMemoryParams{
		UserID:          "user1",
		ScopeID:         "scope1",
		Messages:        []llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		HistoryMessages: nil,
		BaseModel:       model,
	}

	result, err := ExtractLongTermMemory(context.Background(), params, "2026-01-06 10:00:00", nil, 3)
	if err == nil {
		t.Fatal("期望返回错误，实际返回 nil")
	}
	if result != nil {
		t.Errorf("Invoke 错误时 result 应为 nil，实际 %v", result)
	}
	if !strings.Contains(err.Error(), "长期记忆提取 LLM 调用失败") {
		t.Errorf("错误信息应包含'长期记忆提取 LLM 调用失败'，实际: %v", err)
	}
}

// TestExtractLongTermMemory_scopeConfig为nil使用默认值 测试 scopeConfig 为 nil 时使用默认值
func TestExtractLongTermMemory_scopeConfig为nil使用默认值(t *testing.T) {
	jsonResponse := `{
		"has_explict_instruct": false,
		"instruct_memories": [],
		"user_profile": [],
		"semantic_memory": [],
		"episodic_memory": []
	}`

	model := newFakeModelWithResponse(t, jsonResponse)

	params := &ExtractMemoryParams{
		UserID:          "user1",
		ScopeID:         "scope1",
		Messages:        []llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		HistoryMessages: nil,
		BaseModel:       model,
	}

	// scopeConfig 传 nil，应使用默认值而不 panic
	result, err := ExtractLongTermMemory(context.Background(), params, "2026-01-06 10:00:00", nil, 3)
	if err != nil {
		t.Fatalf("ExtractLongTermMemory 返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("result 不应为 nil")
	}
}

// TestExtractLongTermMemory_自定义scopeConfig 测试传入自定义 scopeConfig
func TestExtractLongTermMemory_自定义scopeConfig(t *testing.T) {
	jsonResponse := `{
		"has_explict_instruct": false,
		"instruct_memories": [],
		"user_profile": ["自定义画像"],
		"semantic_memory": [],
		"episodic_memory": []
	}`

	model := newFakeModelWithResponse(t, jsonResponse)

	params := &ExtractMemoryParams{
		UserID:          "user1",
		ScopeID:         "scope1",
		Messages:        []llmschema.BaseMessage{llmschema.NewUserMessage("测试")},
		HistoryMessages: nil,
		BaseModel:       model,
	}

	scopeConfig := &config.MemoryScopeConfig{
		UserProfileDefinition:    "自定义用户画像定义",
		SemanticMemoryDefinition: "自定义语义记忆定义",
		EpisodicMemoryDefinition: "自定义情景记忆定义",
	}

	result, err := ExtractLongTermMemory(context.Background(), params, "2026-01-06 10:00:00", scopeConfig, 3)
	if err != nil {
		t.Fatalf("ExtractLongTermMemory 返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("result 不应为 nil")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Invoke 实现 BaseModelClient.Invoke，执行 invokeFn。
func (m *mockLLMClient) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	if m.invokeFn != nil {
		return m.invokeFn(ctx, messages, opts...)
	}
	return llmschema.NewAssistantMessage("mock response"), nil
}

// Stream 实现 BaseModelClient.Stream，返回不支持。
func (m *mockLLMClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("not supported in mock")
}

// GenerateImage 实现 BaseModelClient.GenerateImage，返回不支持。
func (m *mockLLMClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, fmt.Errorf("not supported in mock")
}

// GenerateSpeech 实现 BaseModelClient.GenerateSpeech，返回不支持。
func (m *mockLLMClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, fmt.Errorf("not supported in mock")
}

// GenerateVideo 实现 BaseModelClient.GenerateVideo，返回不支持。
func (m *mockLLMClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, fmt.Errorf("not supported in mock")
}

// TranscribeAudio 实现 BaseModelClient.TranscribeAudio，返回不支持。
func (m *mockLLMClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, fmt.Errorf("not supported in mock")
}

// Release 实现 BaseModelClient.Release，返回不支持。
func (m *mockLLMClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, fmt.Errorf("not supported in mock")
}

// SupportsKVCacheRelease 实现 BaseModelClient.SupportsKVCacheRelease。
func (m *mockLLMClient) SupportsKVCacheRelease() bool {
	return false
}

// newFakeModelWithResponse 构造返回预设 JSON 字符串的 fake *llm.Model。
//
// 通过 mockLLMClient 实现 BaseModelClient 接口，让 Invoke 返回包含
// 预设文本的 AssistantMessage。WithInvokeOutputParser 会自动解析 JSON
// 并填入 ParserContent。
func newFakeModelWithResponse(t *testing.T, jsonStr string) *llm.Model {
	t.Helper()
	client := &mockLLMClient{
		invokeFn: func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
			return llmschema.NewAssistantMessage(jsonStr), nil
		},
	}
	clientCfg, err := llmschema.NewModelClientConfig("mock", "test-key", "https://mock.test")
	if err != nil {
		t.Fatalf("构造 ModelClientConfig 失败: %v", err)
	}
	model, err := llm.NewModel(clientCfg, nil, llm.WithClient(client))
	if err != nil {
		t.Fatalf("构造 Model 失败: %v", err)
	}
	return model
}

// newFakeModelWithInvokeErr 构造 Invoke 返回错误的 fake *llm.Model。
func newFakeModelWithInvokeErr(invokeErr error) *llm.Model {
	client := &mockLLMClient{
		invokeFn: func(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
			return nil, invokeErr
		},
	}
	clientCfg, _ := llmschema.NewModelClientConfig("mock", "test-key", "https://mock.test")
	model, _ := llm.NewModel(clientCfg, nil, llm.WithClient(client))
	return model
}
```

- [ ] **Step 3: 运行测试确认编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/process/extract/...`
Expected: 编译成功

- [ ] **Step 4: 运行测试确认行为正确**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/process/extract/... -run TestExtractLongTermMemory -v`
Expected: 全部 PASS

- [ ] **Step 5: 确认 MessageContent.Text() 可用**

确认 `llmschema.BaseMessage` 的 `GetContent()` 返回的 `MessageContent` 是否有 `Text()` 方法。如果没有，需调整 `extractor.go` 中 `msg.GetContent().Text()` 为 `msg.GetContent().String()` 或其他方式。

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/memory/process/extract/extractor_test.go internal/agentcore/foundation/llm/model.go
git commit -m "feat(memory): add ExtractLongTermMemory with mock LLM tests (7.18)"
```

---

### Task 5: 全量编译 + 覆盖率验证

**Files:**
- 无新增

- [ ] **Step 1: 全量编译检查**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: 编译成功，无错误

- [ ] **Step 2: 运行新包测试并检查覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/agentcore/memory/config/... ./internal/agentcore/memory/process/extract/...`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 运行全量测试确认无回归**

Run: `cd /home/opensource/uapclaw-gateway && go test ./... 2>&1 | tail -20`
Expected: 全部 PASS

---

### Task 6: 更新 IMPLEMENTATION_PLAN.md + doc.go 同步

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`
- Verify: `internal/agentcore/memory/config/doc.go` 已在 Task 1 更新

- [ ] **Step 1: 更新 IMPLEMENTATION_PLAN.md**

将 7.18 行从：
```
| 7.18 | ☐ | LongTermMemoryExtractor | 长期记忆提取 | `openjiuwen/core/memory/process/extract/` |
```
改为：
```
| 7.18 | ✅ | LongTermMemoryExtractor | ✅ LongTermMemoryExtractor（ExtractLongTermMemory + buildTimeContext）+ ✅ ExtractMemoryParams + ✅ MemoryOperationParams + ✅ MemoryScopeConfig + DefaultMemoryScopeConfig | `openjiuwen/core/memory/process/extract/` |
```

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: update IMPLEMENTATION_PLAN.md 7.18 status to completed"
```
