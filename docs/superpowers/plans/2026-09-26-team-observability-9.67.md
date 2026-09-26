# 9.67 Team Observability 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 Python `openjiuwen/agent_teams/observability/` 的 9 个文件移植为 Go 包，实现三层可观测性（Callback/Monitor/Rail）。

**Architecture:** OtelSpanState 通过 ctx 注入（对齐 SessionState 模式），OtelCallbackHandler 为全局单例注册到 CallbackFramework，创建子 span 时从 OtelSpanState 栈顶取 CurrentLLMSpanContext 显式注入 parent。Monitor 层暂用桩。

**Tech Stack:** go.opentelemetry.io/otel v1.40.0, go.opentelemetry.io/otel/sdk v1.40.0, go.opentelemetry.io/otel/trace v1.40.0, go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.20.0, crypto/sha256

**Design Spec:** `docs/superpowers/specs/2026-09-26-team-observability-9.67-design.md`

---

## 文件结构

### 新建文件

| 文件 | 职责 |
|------|------|
| `internal/agent_teams/observability/doc.go` | 包文档 |
| `internal/agent_teams/observability/config.go` | ObservabilityConfig 配置结构体 |
| `internal/agent_teams/observability/semconv.go` | 语义约定常量（gen_ai.* / agentteam.* / deepagent.*） |
| `internal/agent_teams/observability/redaction.go` | Prompt/Completion 脱敏 |
| `internal/agent_teams/observability/span_state.go` | OtelSpanState + ctx 注入 |
| `internal/agent_teams/observability/callback_handler.go` | OtelCallbackHandler 10 个回调 |
| `internal/agent_teams/observability/monitor_handler.go` | OtelTeamMonitorHandler（桩） |
| `internal/agent_teams/observability/rail.go` | ObservabilityRail |
| `internal/agent_teams/observability/setup.go` | init/shutdown/get_tracer/attach |
| `internal/agent_teams/observability/observability.go` | 公共 API 导出 |

### 测试文件

| 文件 | 职责 |
|------|------|
| `internal/agent_teams/observability/config_test.go` | 配置测试 |
| `internal/agent_teams/observability/semconv_test.go` | 常量值测试 |
| `internal/agent_teams/observability/redaction_test.go` | 脱敏测试 |
| `internal/agent_teams/observability/span_state_test.go` | 栈操作测试 |
| `internal/agent_teams/observability/callback_handler_test.go` | 回调 span 生成测试 |
| `internal/agent_teams/observability/monitor_handler_test.go` | Monitor 事件分发测试 |
| `internal/agent_teams/observability/rail_test.go` | Rail span 开闭测试 |
| `internal/agent_teams/observability/setup_test.go` | 生命周期测试 |

### 修改文件

| 文件 | 修改内容 |
|------|---------|
| `internal/agentcore/runner/callback/framework.go` | UnregisterNamespace 扩展支持 llmCallbacks/toolCallbacks/globalAgentCallbacks |
| `internal/agentcore/harness/deep_agent.go` | ensureInitialized 中注入 SpanState，createSubagent 中创建独立 SpanState |
| `IMPLEMENTATION_PLAN.md` | 9.67 状态更新 |

---

### Task 1: config.go + semconv.go + redaction.go — 无依赖的基础层

**Files:**
- Create: `internal/agent_teams/observability/doc.go`
- Create: `internal/agent_teams/observability/config.go`
- Create: `internal/agent_teams/observability/semconv.go`
- Create: `internal/agent_teams/observability/redaction.go`
- Test: `internal/agent_teams/observability/config_test.go`
- Test: `internal/agent_teams/observability/semconv_test.go`
- Test: `internal/agent_teams/observability/redaction_test.go`

- [ ] **Step 1: 创建包目录和 doc.go**

```go
// Package observability 提供团队可观测性基础设施，集成 OpenTelemetry。
//
// 三层架构：
//   - Callback 层：OtelCallbackHandler 注册到 CallbackFramework，拦截 LLM/Tool/Agent 事件
//   - Monitor 层：OtelTeamMonitorHandler 消费 TeamAgent EventMessage 事件流
//   - Rail 层：ObservabilityRail 覆盖 DeepAgent task_iteration 边界
//
// SpanState 通过 context.Value 传播（同 SessionState 模式），
// 每个 DeepAgent 有独立的 OtelSpanState 实例，天然并发隔离。
//
// 文件目录：
//
//	observability/
//	├── doc.go               # 包文档
//	├── config.go            # ObservabilityConfig 配置结构体
//	├── semconv.go           # 语义约定常量
//	├── redaction.go         # Prompt/Completion 脱敏
//	├── span_state.go        # OtelSpanState + ctx 注入
//	├── callback_handler.go  # OtelCallbackHandler
//	├── monitor_handler.go   # OtelTeamMonitorHandler
//	├── rail.go              # ObservabilityRail
//	├── setup.go             # 生命周期管理
//	└── observability.go     # 公共 API 导出
//
// 对应 Python 代码：openjiuwen/agent_teams/observability/
package observability
```

- [ ] **Step 2: 实现 config.go**

对齐 `config.py`。所有注释中文，声明顺序按项目规范。

```go
package observability

// ──────────────────────────── 结构体 ────────────────────────────

// ObservabilityConfig 可观测性运行时配置。
// Python: ObservabilityConfig (pydantic BaseModel)
type ObservabilityConfig struct {
	// Enabled 总开关，为 false 时 init_observability 为 no-op
	Enabled bool
	// ServiceName OTel resource 属性 service.name
	ServiceName string
	// Exporter 导出器类型："otlp_grpc" | "otlp_http" | "console"
	Exporter string
	// Endpoint OTLP 端点 URL（gRPC 默认 localhost:4317；HTTP 4318）
	Endpoint string
	// SampleRate 采样率（0.0 - 1.0）
	SampleRate float64
	// RedactPrompts 为 true 时哈希/截断 prompt 内容
	RedactPrompts bool
	// RedactCompletions 为 true 时哈希/截断 completion 内容
	RedactCompletions bool
	// AttributeValueMaxLength 字符串属性值硬上限
	AttributeValueMaxLength int
	// ExportTimeoutMs Span 导出器关闭超时
	ExportTimeoutMs int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultObservabilityConfig 创建默认配置。
// Python: ObservabilityConfig() 无参默认值
func DefaultObservabilityConfig() *ObservabilityConfig {
	return &ObservabilityConfig{
		Enabled:                 true,
		ServiceName:             "openjiuwen-agent-teams",
		Exporter:                "otlp_grpc",
		Endpoint:                "http://localhost:4317",
		SampleRate:              1.0,
		RedactPrompts:           false,
		RedactCompletions:       false,
		AttributeValueMaxLength: 8192,
		ExportTimeoutMs:         5000,
	}
}
```

- [ ] **Step 3: 实现 semconv.go**

对齐 `semconv.py`，三组常量。

```go
package observability

// ──────────────────────────── 常量 ────────────────────────────

// OpenLLMetry / GenAI 标准属性
// Python: GEN_AI_* 常量
const (
	GenAISystem               = "gen_ai.system"
	GenAIRequestModel         = "gen_ai.request.model"
	GenAIRequestTemperature   = "gen_ai.request.temperature"
	GenAIRequestTopP          = "gen_ai.request.top_p"
	GenAIRequestMaxTokens     = "gen_ai.request.max_tokens"
	GenAIPrompt               = "gen_ai.prompt"
	GenAICompletion           = "gen_ai.completion"
	GenAIUsagePromptTokens    = "gen_ai.usage.prompt_tokens"
	GenAIUsageCompletionTokens = "gen_ai.usage.completion_tokens"
	GenAIUsageTotalTokens     = "gen_ai.usage.total_tokens"
	GenAIResponseFinishReason = "gen_ai.response.finish_reason"
	GenAIResponseModel        = "gen_ai.response.model"
	GenAIResponseTTFTMs       = "gen_ai.response.time_to_first_token_ms"
	GenAIToolName             = "gen_ai.tool.name"
	GenAIToolInput            = "gen_ai.tool.input"
	GenAIToolOutput           = "gen_ai.tool.output"
	GenAIToolID               = "gen_ai.tool.id"
)

// agentteam.* — Team 协作属性（Monitor handler）
// Python: AT_* 常量
const (
	ATTeamName            = "agentteam.team.name"
	ATTeamDisplayName     = "agentteam.team.display_name"
	ATEventType           = "agentteam.event_type"
	ATAgentID             = "agentteam.agent.id"
	ATAgentRole           = "agentteam.agent.role"
	ATAgentInput          = "agentteam.agent.input"
	ATAgentOutput         = "agentteam.agent.output"
	ATMemberName          = "agentteam.member.name"
	ATMemberStatusOld     = "agentteam.member.status.old"
	ATMemberStatusNew     = "agentteam.member.status.new"
	ATMemberRestartReason = "agentteam.member.restart_reason"
	ATMemberRestartCount  = "agentteam.member.restart_count"
	ATMemberShutdownForce = "agentteam.member.shutdown_force"
	ATMessageID           = "agentteam.message.id"
	ATMessageFrom         = "agentteam.message.from"
	ATMessageTo           = "agentteam.message.to"
	ATMessageBroadcast    = "agentteam.message.broadcast"
	ATTaskID              = "agentteam.task.id"
	ATTaskStatus          = "agentteam.task.status"
	ATTaskAssignee        = "agentteam.task.assignee"
	ATTeamLeader          = "agentteam.team.leader"
)

// deepagent.* — DeepAgent task-loop 属性（Rail）
// Python: DA_* 常量
const (
	DATaskIteration  = "deepagent.task.iteration"
	DATaskIsFollowUp = "deepagent.task.is_follow_up"
)
```

- [ ] **Step 4: 实现 redaction.go**

对齐 `redaction.py`。

```go
package observability

import (
	"crypto/sha256"
	"fmt"
)

// ──────────────────────────── 常量 ────────────────────────────

const redactedPrefix = "sha256:"

// ──────────────────────────── 导出函数 ────────────────────────────

// RedactPrompt 对 prompt 片段应用脱敏策略。
// Python: redact_prompt(value, config)
func RedactPrompt(value any, config *ObservabilityConfig) string {
	text := ""
	if value != nil {
		text = fmt.Sprintf("%v", value)
	}
	if config.RedactPrompts {
		return hashValue(text)
	}
	return truncate(text, config.AttributeValueMaxLength)
}

// RedactCompletion 对 completion 片段应用脱敏策略。
// Python: redact_completion(value, config)
func RedactCompletion(value any, config *ObservabilityConfig) string {
	text := ""
	if value != nil {
		text = fmt.Sprintf("%v", value)
	}
	if config.RedactCompletions {
		return hashValue(text)
	}
	return truncate(text, config.AttributeValueMaxLength)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// truncate 硬截断字符串并标注截断长度。
// Python: _truncate(value, max_length)
func truncate(value string, maxLength int) string {
	if maxLength <= 0 || len(value) <= maxLength {
		return value
	}
	return value[:maxLength] + fmt.Sprintf("...<truncated %d chars>", len(value)-maxLength)
}

// hashValue 用 SHA-256 前缀哈希替换原值，保留关联性但不暴露内容。
// Python: _hash(value)
func hashValue(value string) string {
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%s%x", redactedPrefix, digest[:8])
}
```

- [ ] **Step 5: 实现 config_test.go**

```go
package observability

import (
	"testing"
)

func TestDefaultObservabilityConfig(t *testing.T) {
	cfg := DefaultObservabilityConfig()
	if !cfg.Enabled {
		t.Error("期望 Enabled=true")
	}
	if cfg.ServiceName != "openjiuwen-agent-teams" {
		t.Errorf("期望 ServiceName=openjiuwen-agent-teams，实际 %s", cfg.ServiceName)
	}
	if cfg.Exporter != "otlp_grpc" {
		t.Errorf("期望 Exporter=otlp_grpc，实际 %s", cfg.Exporter)
	}
	if cfg.Endpoint != "http://localhost:4317" {
		t.Errorf("期望 Endpoint=http://localhost:4317，实际 %s", cfg.Endpoint)
	}
	if cfg.SampleRate != 1.0 {
		t.Errorf("期望 SampleRate=1.0，实际 %f", cfg.SampleRate)
	}
	if cfg.AttributeValueMaxLength != 8192 {
		t.Errorf("期望 AttributeValueMaxLength=8192，实际 %d", cfg.AttributeValueMaxLength)
	}
	if cfg.ExportTimeoutMs != 5000 {
		t.Errorf("期望 ExportTimeoutMs=5000，实际 %d", cfg.ExportTimeoutMs)
	}
}

func TestObservabilityConfig_自定义值(t *testing.T) {
	cfg := &ObservabilityConfig{
		Enabled:       false,
		ServiceName:   "test-service",
		Exporter:      "console",
		Endpoint:      "http://localhost:4318",
		SampleRate:    0.5,
		RedactPrompts: true,
	}
	if cfg.Enabled {
		t.Error("期望 Enabled=false")
	}
	if cfg.ServiceName != "test-service" {
		t.Errorf("期望 ServiceName=test-service，实际 %s", cfg.ServiceName)
	}
	if !cfg.RedactPrompts {
		t.Error("期望 RedactPrompts=true")
	}
}
```

- [ ] **Step 6: 实现 semconv_test.go**

```go
package observability

import (
	"testing"
)

func TestGenAI常量值与Python一致(t *testing.T) {
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"GenAISystem", GenAISystem, "gen_ai.system"},
		{"GenAIRequestModel", GenAIRequestModel, "gen_ai.request.model"},
		{"GenAIRequestTemperature", GenAIRequestTemperature, "gen_ai.request.temperature"},
		{"GenAIRequestTopP", GenAIRequestTopP, "gen_ai.request.top_p"},
		{"GenAIRequestMaxTokens", GenAIRequestMaxTokens, "gen_ai.request.max_tokens"},
		{"GenAIPrompt", GenAIPrompt, "gen_ai.prompt"},
		{"GenAICompletion", GenAICompletion, "gen_ai.completion"},
		{"GenAIUsagePromptTokens", GenAIUsagePromptTokens, "gen_ai.usage.prompt_tokens"},
		{"GenAIUsageCompletionTokens", GenAIUsageCompletionTokens, "gen_ai.usage.completion_tokens"},
		{"GenAIUsageTotalTokens", GenAIUsageTotalTokens, "gen_ai.usage.total_tokens"},
		{"GenAIResponseFinishReason", GenAIResponseFinishReason, "gen_ai.response.finish_reason"},
		{"GenAIResponseModel", GenAIResponseModel, "gen_ai.response.model"},
		{"GenAIResponseTTFTMs", GenAIResponseTTFTMs, "gen_ai.response.time_to_first_token_ms"},
		{"GenAIToolName", GenAIToolName, "gen_ai.tool.name"},
		{"GenAIToolInput", GenAIToolInput, "gen_ai.tool.input"},
		{"GenAIToolOutput", GenAIToolOutput, "gen_ai.tool.output"},
		{"GenAIToolID", GenAIToolID, "gen_ai.tool.id"},
	}
	for _, tt := range tests {
		if tt.got != tt.expected {
			t.Errorf("%s: 期望 %q，实际 %q", tt.name, tt.expected, tt.got)
		}
	}
}

func TestAgentTeam常量值与Python一致(t *testing.T) {
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"ATTeamName", ATTeamName, "agentteam.team.name"},
		{"ATTeamDisplayName", ATTeamDisplayName, "agentteam.team.display_name"},
		{"ATEventType", ATEventType, "agentteam.event_type"},
		{"ATAgentID", ATAgentID, "agentteam.agent.id"},
		{"ATMemberName", ATMemberName, "agentteam.member.name"},
		{"ATTaskID", ATTaskID, "agentteam.task.id"},
		{"ATMessageID", ATMessageID, "agentteam.message.id"},
	}
	for _, tt := range tests {
		if tt.got != tt.expected {
			t.Errorf("%s: 期望 %q，实际 %q", tt.name, tt.expected, tt.got)
		}
	}
}

func TestDeepAgent常量值与Python一致(t *testing.T) {
	if DATaskIteration != "deepagent.task.iteration" {
		t.Errorf("期望 deepagent.task.iteration，实际 %s", DATaskIteration)
	}
	if DATaskIsFollowUp != "deepagent.task.is_follow_up" {
		t.Errorf("期望 deepagent.task.is_follow_up，实际 %s", DATaskIsFollowUp)
	}
}
```

- [ ] **Step 7: 实现 redaction_test.go**

```go
package observability

import (
	"strings"
	"testing"
)

func TestRedactPrompt_不脱敏时截断(t *testing.T) {
	cfg := &ObservabilityConfig{AttributeValueMaxLength: 10}
	result := RedactPrompt("hello world this is long", cfg)
	if result != "hello worl...<truncated 14 chars>" {
		t.Errorf("期望截断结果，实际 %q", result)
	}
}

func TestRedactPrompt_不脱敏时不截断(t *testing.T) {
	cfg := &ObservabilityConfig{AttributeValueMaxLength: 100}
	result := RedactPrompt("short", cfg)
	if result != "short" {
		t.Errorf("期望 short，实际 %q", result)
	}
}

func TestRedactPrompt_脱敏时返回哈希(t *testing.T) {
	cfg := &ObservabilityConfig{RedactPrompts: true, AttributeValueMaxLength: 100}
	result := RedactPrompt("secret prompt", cfg)
	if !strings.HasPrefix(result, "sha256:") {
		t.Errorf("期望 sha256: 前缀，实际 %q", result)
	}
	// 相同输入应产生相同哈希
	result2 := RedactPrompt("secret prompt", cfg)
	if result != result2 {
		t.Errorf("期望相同输入产生相同哈希，%q != %q", result, result2)
	}
}

func TestRedactPrompt_nil输入(t *testing.T) {
	cfg := &ObservabilityConfig{AttributeValueMaxLength: 100}
	result := RedactPrompt(nil, cfg)
	if result != "" {
		t.Errorf("期望空字符串，实际 %q", result)
	}
}

func TestRedactCompletion_不脱敏时截断(t *testing.T) {
	cfg := &ObservabilityConfig{AttributeValueMaxLength: 5}
	result := RedactCompletion("abcdefgh", cfg)
	if result != "abcde...<truncated 3 chars>" {
		t.Errorf("期望截断结果，实际 %q", result)
	}
}

func TestRedactCompletion_脱敏时返回哈希(t *testing.T) {
	cfg := &ObservabilityConfig{RedactCompletions: true, AttributeValueMaxLength: 100}
	result := RedactCompletion("secret completion", cfg)
	if !strings.HasPrefix(result, "sha256:") {
		t.Errorf("期望 sha256: 前缀，实际 %q", result)
	}
}

func TestTruncate_零maxLength不截断(t *testing.T) {
	result := truncate("any", 0)
	if result != "any" {
		t.Errorf("期望不截断，实际 %q", result)
	}
}

func TestHashValue_空字符串(t *testing.T) {
	result := hashValue("")
	if !strings.HasPrefix(result, "sha256:") {
		t.Errorf("期望 sha256: 前缀，实际 %q", result)
	}
}
```

- [ ] **Step 8: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/observability/... -v -count=1`
Expected: PASS

- [ ] **Step 9: 提交**

```bash
git add internal/agent_teams/observability/
git commit -m "feat(observability): 添加 config/semconv/redaction 基础层 (9.67 Task 1)"
```

---

### Task 2: span_state.go — OtelSpanState + ctx 注入

**Files:**
- Create: `internal/agent_teams/observability/span_state.go`
- Test: `internal/agent_teams/observability/span_state_test.go`

- [ ] **Step 1: 实现 span_state.go**

```go
package observability

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LlmSpanState 单次 LLM 调用的 span 追踪状态。
// Python: LlmSpanState dataclass (span_context.py)
//
// Python 有 context_token 字段用于 otel_context.detach，
// Go 不需要：Go 的 ctx 不可变，无全局状态需恢复。
type LlmSpanState struct {
	// Span 开放的 OTel span
	Span trace.Span
	// StartNs span 开启时的单调时间（纳秒）
	StartNs int64
	// FirstChunkNs 首个 stream chunk 的单调时间（0 表示未到达）
	FirstChunkNs int64
	// ChunkCount stream chunk 计数
	ChunkCount int
}

// NextChunkSeq 递增并返回下一个 chunk 序号。
// Python: LlmSpanState.next_chunk_seq()
func (s *LlmSpanState) NextChunkSeq() int {
	s.ChunkCount++
	return s.ChunkCount
}

// OtelSpanState 每-DeepAgent 的可变 span 追踪状态容器。
// Python: _llm_span_stack + _tool_span_map + _agent_span_map (3 个 ContextVar)
//
// 通过 context.Value 传播 *OtelSpanState 指针（同 SessionState 模式）：
//   - 同一 DeepAgent 内的 goroutine 共享同一 OtelSpanState 引用
//   - 子 DeepAgent 创建新实例 + WithSpanState 派生新 ctx，父不受影响
//
// 并发安全：所有字段读写通过 sync.Mutex 保护。
type OtelSpanState struct {
	mu           sync.Mutex
	llmStack     []*LlmSpanState
	toolSpanMap  map[string][]trace.Span // key=toolName
	agentSpanMap map[string][]trace.Span // key=agentID
}

// spanStateKeyType OtelSpanState 的 context key 类型。
type spanStateKeyType struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// InitSpanState 创建新的 OtelSpanState 实例。
// Python: _llm_span_stack = ContextVar(default=[]) 等 3 个
func InitSpanState() *OtelSpanState {
	return &OtelSpanState{
		toolSpanMap:  make(map[string][]trace.Span),
		agentSpanMap: make(map[string][]trace.Span),
	}
}

// WithSpanState 将 OtelSpanState 注入 context。
// Python: contextvars 自动传播；Go 通过 context.Value 传播指针
func WithSpanState(ctx context.Context, state *OtelSpanState) context.Context {
	return context.WithValue(ctx, spanStateKeyType{}, state)
}

// SpanStateFromCtx 从 context 中获取 OtelSpanState。
// 返回 nil 表示当前 context 未绑定 SpanState（可观测性未启用）。
func SpanStateFromCtx(ctx context.Context) *OtelSpanState {
	if s, ok := ctx.Value(spanStateKeyType{}).(*OtelSpanState); ok {
		return s
	}
	return nil
}

// PushLlmSpanState 将 LLM span 状态压入栈。
// Python: push_llm_span_state(state)
func (s *OtelSpanState) PushLlmSpanState(state *LlmSpanState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.llmStack = append(s.llmStack, state)
}

// PopLlmSpanState 弹出（或窥视）栈顶 LLM span 状态。
// Python: pop_llm_span_state(peek=False)
//
// peek=true 时返回栈顶但不移除，用于 stream chunk 回调。
func (s *OtelSpanState) PopLlmSpanState(peek bool) *LlmSpanState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.llmStack) == 0 {
		return nil
	}
	if peek {
		return s.llmStack[len(s.llmStack)-1]
	}
	top := s.llmStack[len(s.llmStack)-1]
	s.llmStack = s.llmStack[:len(s.llmStack)-1]
	return top
}

// CurrentLLMSpanContext 返回当前栈顶 LLM span 的 SpanContext。
// 用于创建子 span 时显式注入 parent（对齐 Python 的 otel_context.attach 效果）。
// 返回空 SpanContext 表示无当前 LLM span（子 span 将成为根 span）。
func (s *OtelSpanState) CurrentLLMSpanContext() trace.SpanContext {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.llmStack) == 0 {
		return trace.SpanContext{}
	}
	return s.llmStack[len(s.llmStack)-1].Span.SpanContext()
}

// PushToolSpan 将 tool span 压入 map。
// Python: push_tool_span(tool_name, span)
func (s *OtelSpanState) PushToolSpan(toolName string, span trace.Span) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.toolSpanMap[toolName] = append(s.toolSpanMap[toolName], span)
}

// PopToolSpan 弹出指定 toolName 最近的一个 tool span。
// Python: pop_tool_span(tool_name)
func (s *OtelSpanState) PopToolSpan(toolName string) trace.Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.toolSpanMap[toolName]
	if len(bucket) == 0 {
		return nil
	}
	span := bucket[len(bucket)-1]
	if len(bucket) == 1 {
		delete(s.toolSpanMap, toolName)
	} else {
		s.toolSpanMap[toolName] = bucket[:len(bucket)-1]
	}
	return span
}

// PushAgentSpan 将 agent span 压入 map。
// Python: push_agent_span(agent_id, span)
func (s *OtelSpanState) PushAgentSpan(agentID string, span trace.Span) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentSpanMap[agentID] = append(s.agentSpanMap[agentID], span)
}

// PopAgentSpan 弹出指定 agentID 最近的一个 agent span。
// Python: pop_agent_span(agent_id)
func (s *OtelSpanState) PopAgentSpan(agentID string) trace.Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket := s.agentSpanMap[agentID]
	if len(bucket) == 0 {
		return nil
	}
	span := bucket[len(bucket)-1]
	if len(bucket) == 1 {
		delete(s.agentSpanMap, agentID)
	} else {
		s.agentSpanMap[agentID] = bucket[:len(bucket)-1]
	}
	return span
}

// ResetAll 重置所有 span 追踪器。测试用。
// Python: reset_all()
func (s *OtelSpanState) ResetAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.llmStack = nil
	s.toolSpanMap = make(map[string][]trace.Span)
	s.agentSpanMap = make(map[string][]trace.Span)
}

// WithSpanStateAndTimeout 创建带 SpanState 和超时的 context（辅助方法）。
func WithSpanStateAndTimeout(ctx context.Context, state *OtelSpanState, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx = WithSpanState(ctx, state)
	return context.WithTimeout(ctx, timeout)
}
```

- [ ] **Step 2: 实现 span_state_test.go**

测试 LIFO 栈语义、map 操作、并发安全、ctx 注入。

```go
package observability

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// newTestSpanState 创建带 mock tracer 的测试 OtelSpanState
func newTestSpanState() (*OtelSpanState, tracetest.SpanRecorder) {
	recorder := tracetest.NewSpanRecorder()
	return InitSpanState(), recorder
}

func TestInitSpanState(t *testing.T) {
	state := InitSpanState()
	require.NotNil(t, state)
	assert.Empty(t, state.llmStack)
}

func TestWithSpanState_SpanStateFromCtx(t *testing.T) {
	state := InitSpanState()
	ctx := context.Background()

	// 未注入时返回 nil
	assert.Nil(t, SpanStateFromCtx(ctx))

	// 注入后可取出
	ctx = WithSpanState(ctx, state)
	got := SpanStateFromCtx(ctx)
	require.NotNil(t, got)
	assert.Equal(t, state, got)
}

func TestLlmSpanStack_PushPop(t *testing.T) {
	state := InitSpanState()
	span1 := &LlmSpanState{StartNs: 1}
	span2 := &LlmSpanState{StartNs: 2}

	state.PushLlmSpanState(span1)
	state.PushLlmSpanState(span2)

	// Pop LIFO
	popped := state.PopLlmSpanState(false)
	require.NotNil(t, popped)
	assert.Equal(t, int64(2), popped.StartNs)

	popped = state.PopLlmSpanState(false)
	require.NotNil(t, popped)
	assert.Equal(t, int64(1), popped.StartNs)

	// 空栈 pop 返回 nil
	assert.Nil(t, state.PopLlmSpanState(false))
}

func TestLlmSpanStack_Peek(t *testing.T) {
	state := InitSpanState()
	state.PushLlmSpanState(&LlmSpanState{StartNs: 1})

	// Peek 不移除
	peeked := state.PopLlmSpanState(true)
	require.NotNil(t, peeked)
	assert.Equal(t, int64(1), peeked.StartNs)

	// 栈未变
	peeked2 := state.PopLlmSpanState(true)
	require.NotNil(t, peeked2)
	assert.Equal(t, int64(1), peeked2.StartNs)
}

func TestLlmSpanState_NextChunkSeq(t *testing.T) {
	s := &LlmSpanState{}
	assert.Equal(t, 1, s.NextChunkSeq())
	assert.Equal(t, 2, s.NextChunkSeq())
	assert.Equal(t, 3, s.NextChunkSeq())
	assert.Equal(t, 3, s.ChunkCount)
}

func TestToolSpanMap_PushPop(t *testing.T) {
	state := InitSpanState()

	// 使用 noop span 测试 map 操作（不需要真实 tracer）
	// PushToolSpan/PopToolSpan 的 span 参数在测试中可以是 nil
	// 但为验证 map 逻辑正确，我们直接操作底层
	state.PushToolSpan("bash", nil)
	state.PushToolSpan("bash", nil)

	popped := state.PopToolSpan("bash")
	assert.Nil(t, popped) // nil span 但不报错

	popped = state.PopToolSpan("bash")
	assert.Nil(t, popped)

	// 空 map pop 返回 nil
	assert.Nil(t, state.PopToolSpan("bash"))
}

func TestAgentSpanMap_PushPop(t *testing.T) {
	state := InitSpanState()
	state.PushAgentSpan("agent1", nil)
	state.PushAgentSpan("agent1", nil)

	popped := state.PopAgentSpan("agent1")
	assert.Nil(t, popped)

	popped = state.PopAgentSpan("agent1")
	assert.Nil(t, popped)

	assert.Nil(t, state.PopAgentSpan("agent1"))
}

func TestResetAll(t *testing.T) {
	state := InitSpanState()
	state.PushLlmSpanState(&LlmSpanState{StartNs: 1})
	state.PushToolSpan("bash", nil)
	state.PushAgentSpan("agent1", nil)

	state.ResetAll()

	assert.Nil(t, state.PopLlmSpanState(false))
	assert.Nil(t, state.PopToolSpan("bash"))
	assert.Nil(t, state.PopAgentSpan("agent1"))
}

func TestOtelSpanState_并发安全(t *testing.T) {
	state := InitSpanState()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			state.PushLlmSpanState(&LlmSpanState{StartNs: int64(n)})
		}(i)
	}
	wg.Wait()
	// 栈中应有 100 个元素
	count := 0
	for state.PopLlmSpanState(false) != nil {
		count++
	}
	assert.Equal(t, 100, count)
}
```

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/observability/... -v -run 'TestInitSpanState|TestWithSpanState|TestLlmSpanStack|TestLlmSpanState|TestToolSpanMap|TestAgentSpanMap|TestResetAll|TestOtelSpanState_并发' -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/observability/
git commit -m "feat(observability): 添加 OtelSpanState ctx 注入层 (9.67 Task 2)"
```

---

### Task 3: callback_handler.go — OtelCallbackHandler 10 个回调

**Files:**
- Create: `internal/agent_teams/observability/callback_handler.go`
- Test: `internal/agent_teams/observability/callback_handler_test.go`

- [ ] **Step 1: 实现 callback_handler.go**

完整实现 OtelCallbackHandler 的 10 个回调方法，对齐 `callback_handler.py`。关键点：
- 每个回调从 `SpanStateFromCtx(ctx)` 读取 span 状态
- 创建子 span 时用 `trace.ContextWithSpanContext(ctx, spanState.CurrentLLMSpanContext())` 注入 parent
- 所有异常用 `defer recover` 兜底
- 日志用 `logger.Warn(ComponentAgentCore)`
- 不需要 `LlmSpanState.context_token` 字段

代码较长，按 Python 源码逐方法翻译。需引入：
- `go.opentelemetry.io/otel/attribute`
- `go.opentelemetry.io/otel/codes`
- `go.opentelemetry.io/otel/trace`
- `github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback`
- `github.com/uapclaw/uapclaw-go/internal/common/logger`

辅助方法：
- `openLlmSpan(ctx, data, spanState)` — 对齐 `_open_llm_span`
- `closeLlmSpan(state, response)` — 对齐 `_close_llm_span`
- `maybeRecordResponseAttrs(state, response)` — 对齐 `_maybe_record_response_attrs`
- `coerceMessageContent(content)` — 对齐 `_coerce_message_content`
- `messageRole(msg)` / `messageContent(msg)` — 对齐 `_message_role` / `_message_content`
- `serializeToolInputs(inputs)` — 对齐 `_serialize_tool_inputs`

- [ ] **Step 2: 实现 callback_handler_test.go**

使用 `sdktrace.NewTracerProvider` + `tracetest.SpanRecorder` 创建 mock tracer：
- 测试 `onLLMInvokeInput` 生成 span 且属性正确（model, temperature, prompt）
- 测试 `onLLMInvokeOutput` 关闭 span 且属性正确（completion, usage）
- 测试 `onLLMCallError` span 状态为 ERROR
- 测试 `onToolCallStarted/Finished` span 开闭
- 测试 `onAgentInvokeInput/Output` span 开闭
- 测试 span 父子链：Tool span 的 parent 为 LLM span
- 测试 SpanStateFromCtx 为 nil 时回调直接返回 nil

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/observability/... -v -run 'TestOtelCallback' -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/observability/
git commit -m "feat(observability): 添加 OtelCallbackHandler 10 个回调 (9.67 Task 3)"
```

---

### Task 4: monitor_handler.go — OtelTeamMonitorHandler（桩实现）

**Files:**
- Create: `internal/agent_teams/observability/monitor_handler.go`
- Test: `internal/agent_teams/observability/monitor_handler_test.go`

- [ ] **Step 1: 实现 monitor_handler.go**

对齐 `monitor_handler.py` 的结构体和所有方法。`HandleEvent` 方法照常分发事件（用于单元测试），但 `attach_to_team_agent` 暂为桩。

关键结构：
- `OtelTeamMonitorHandler` 持有 `teamSpans map[string]trace.Span` 和 `taskSpans map[string]trace.Span`
- 事件类型分类常量：`taskOpenTypes`, `taskCloseTypes`, `memberTypes`, `messageTypes`
- 使用 `events` 包的 TeamEvent 常量

- [ ] **Step 2: 实现 monitor_handler_test.go**

- 测试 `HandleEvent` 对 team_created / team_cleaned 的分发
- 测试 `HandleEvent` 对 task_created / task_completed 的分发
- 测试 `HandleEvent` 对 member/message 事件的分发
- 测试未知事件类型静默忽略

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/observability/... -v -run 'TestOtelTeamMonitor' -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/observability/
git commit -m "feat(observability): 添加 OtelTeamMonitorHandler 桩实现 (9.67 Task 4)"
```

---

### Task 5: rail.go — ObservabilityRail

**Files:**
- Create: `internal/agent_teams/observability/rail.go`
- Test: `internal/agent_teams/observability/rail_test.go`

- [ ] **Step 1: 实现 rail.go**

对齐 `rail.py`：
- 嵌入 `rails.DeepAgentRail`
- `BeforeTaskIteration`：创建 `deepagent.task_iteration.N` span，存入 `railCtx.Extra[spanKey]`
- `AfterTaskIteration`：从 `railCtx.Extra` 取 span，设状态后关闭
- `GetCallbacks()` 继承自 `DeepAgentRail`，自动注册 BeforeTaskIteration/AfterTaskIteration

- [ ] **Step 2: 实现 rail_test.go**

- 测试 `BeforeTaskIteration` 在 Extra 中存储 span
- 测试 `AfterTaskIteration` 关闭 span 并设 OK 状态
- 测试异常时 span 设 ERROR 状态

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/observability/... -v -run 'TestObservabilityRail' -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/observability/
git commit -m "feat(observability): 添加 ObservabilityRail (9.67 Task 5)"
```

---

### Task 6: setup.go + observability.go — 生命周期管理 + 公共 API

**Files:**
- Create: `internal/agent_teams/observability/setup.go`
- Create: `internal/agent_teams/observability/observability.go`
- Test: `internal/agent_teams/observability/setup_test.go`

- [ ] **Step 1: 实现 setup.go**

对齐 `setup.py`：
- 全局变量 `provider`, `callbackHandler`, `monitorHandler`
- `InitObservability(config, ...Option)` — 创建 TracerProvider + 注册回调
- `ShutdownObservability()` — 注销回调 + flush span
- `GetTracer(name)` — 获取 tracer
- `AttachToTeamAgent(teamAgent)` — 桩实现（no-op）
- `DetachFromTeamAgent(teamAgent)` — 桩实现（no-op）
- `wireCallbackHandlers(handler, fw, namespace)` — 注册 10 个回调
- `buildExporter(config)` — 构建导出器

使用 `ObservabilityOption` 函数选项模式支持 `WithSpanExporterOverride`。

- [ ] **Step 2: 实现 observability.go**

对齐 `__init__.py`，重导出公共类型和函数。

- [ ] **Step 3: 实现 setup_test.go**

- 测试 `InitObservability` 创建 provider 和 handler
- 测试重复 `InitObservability` 返回 warn 不崩溃
- 测试 `ShutdownObservability` 清理全局变量
- 测试 `InitObservability` enabled=false 时为 no-op
- 测试 `wireCallbackHandlers` 注册 10 个回调
- 测试 `ShutdownObservability` 后回调被注销

- [ ] **Step 4: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/observability/... -v -run 'TestInitObservability|TestShutdownObservability|TestWireCallback' -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/observability/
git commit -m "feat(observability): 添加 setup 生命周期 + 公共 API (9.67 Task 6)"
```

---

### Task 7: 扩展 CallbackFramework.UnregisterNamespace

**Files:**
- Modify: `internal/agentcore/runner/callback/framework.go:1281-1319`
- Modify: `internal/agentcore/runner/callback/framework_test.go`（追加测试）

- [ ] **Step 1: 扩展 UnregisterNamespace 遍历 llmCallbacks/toolCallbacks/globalAgentCallbacks**

在现有 `UnregisterNamespace` 方法的 `fw.circuitBreakers` 清理之后，追加三段清理逻辑：

```go
// 清理 LLM 回调
for k, v := range fw.llmCallbacks {
	filtered := make([]*CallbackInfo[LLMCallbackFunc], 0, len(v))
	for _, info := range v {
		if info.Namespace != namespace {
			filtered = append(filtered, info)
		}
	}
	fw.llmCallbacks[k] = filtered
}
// 清理 Tool 回调
for k, v := range fw.toolCallbacks {
	filtered := make([]*CallbackInfo[ToolCallbackFunc], 0, len(v))
	for _, info := range v {
		if info.Namespace != namespace {
			filtered = append(filtered, info)
		}
	}
	fw.toolCallbacks[k] = filtered
}
// 清理 GlobalAgent 回调
for k, v := range fw.globalAgentCallbacks {
	filtered := make([]*CallbackInfo[GlobalAgentCallbackFunc], 0, len(v))
	for _, info := range v {
		if info.Namespace != namespace {
			filtered = append(filtered, info)
		}
	}
	fw.globalAgentCallbacks[k] = filtered
}
```

- [ ] **Step 2: 追加 UnregisterNamespace 测试**

在 `framework_test.go` 中追加：
- 测试 `OnLLM` + namespace 注册后 `UnregisterNamespace` 能正确移除
- 测试不同 namespace 的回调不受影响

- [ ] **Step 3: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/runner/callback/... -v -run 'TestUnregisterNamespace' -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/runner/callback/
git commit -m "feat(callback): 扩展 UnregisterNamespace 支持 LLM/Tool/Agent 回调 (9.67 Task 7)"
```

---

### Task 8: DeepAgent 注入 SpanState

**Files:**
- Modify: `internal/agentcore/harness/deep_agent.go:1668`（ensureInitialized 中 CWD 初始化之后）
- Modify: `internal/agentcore/harness/deep_agent.go:655`（createSubagent 中 CWD 初始化之后）

- [ ] **Step 1: 在 ensureInitialized 中注入 SpanState**

在 CWD 初始化之后（约 line 1668），追加：

```go
// 初始化可观测性 SpanState（对齐 SessionState 注入模式）
// Python: contextvars 自动传播 span_context
// Go: 通过 context.Value 传播 *OtelSpanState 指针
if SpanStateFromCtx(ctx) == nil {
	spanState := InitSpanState()
	ctx = WithSpanState(ctx, spanState)
}
```

需要 import `github.com/uapclaw/uapclaw-go/internal/agent_teams/observability`。

- [ ] **Step 2: 在 createSubagent 中创建独立 SpanState**

在 subCwdState 初始化之后（约 line 656），追加：

```go
// 子 Agent 创建独立 SpanState，实现 inter-Agent 隔离
subSpanState := observability.InitSpanState()
subCtx = observability.WithSpanState(subCtx, subSpanState)
```

- [ ] **Step 3: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/harness/...`
Expected: 编译成功

- [ ] **Step 4: 运行已有测试确认无回归**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/... -count=1`
Expected: PASS（无回归）

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/deep_agent.go
git commit -m "feat(deep_agent): 注入 OtelSpanState 到 ctx (9.67 Task 8)"
```

---

### Task 9: 全量测试 + IMPLEMENTATION_PLAN.md 更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（9.67 状态 ☐ → ✅）
- Update: `internal/agent_teams/observability/doc.go`（确认文件目录完整）

- [ ] **Step 1: 运行全包测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/observability/... -v -cover -count=1`
Expected: PASS，覆盖率 ≥ 85%

- [ ] **Step 2: 运行全项目编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: 编译成功

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将 9.67 行的 `☐` 改为 `✅`。

- [ ] **Step 4: 更新 doc.go 确认文件目录完整**

确认 doc.go 中文件目录与实际文件一致。

- [ ] **Step 5: 提交**

```bash
git add IMPLEMENTATION_PLAN.md internal/agent_teams/observability/doc.go
git commit -m "feat(observability): 完成 9.67 Team Observability 全部实现"
```
