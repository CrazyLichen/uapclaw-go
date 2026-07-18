# 9.77 基础 Trajectory 类型 + 9.72e BaseOptimizer & LLMResilience 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现自演化系统的 Trajectory 基础类型和 BaseOptimizer 优化器基类 + LLMResilience 重试策略，并回填已有代码中的 `any` 占位。

**Architecture:** 先实现 9.77 Trajectory 基础类型（纯数据结构 + JSONSafe/MessageToDict 工具函数），再实现 9.72e BaseOptimizer（接口 + Mixin 辅助结构体 + TextualParameter）和 LLMResilience（LLMInvokePolicy + 带重试的文本调用），最后回填 SingleDimUpdater/MultiDimUpdater/Updater protocol 中的 `any` 占位。

**Tech Stack:** Go 1.22+、context、encoding/json、math、time、项目内 exception/logger/operator/signal/schema 包

---

## 文件结构

### 新建文件

| 文件 | 职责 |
|---|---|
| `internal/evolving/trajectory/doc.go` | 包文档 |
| `internal/evolving/trajectory/types.go` | StepKind/CostInfo/StepDetail/LLMCallDetail/ToolCallDetail/TrajectoryStep/Trajectory + ToMessages |
| `internal/evolving/trajectory/json_safe.go` | JSONSafe + MessageToDict + responseToText |
| `internal/evolving/trajectory/types_test.go` | 核心类型测试 |
| `internal/evolving/trajectory/json_safe_test.go` | JSONSafe/MessageToDict/responseToText 测试 |
| `internal/evolving/optimizer/doc.go` | 包文档 |
| `internal/evolving/optimizer/base.go` | BaseOptimizer 接口 + BaseOptimizerMixin + TextualParameter |
| `internal/evolving/optimizer/base_test.go` | Mixin + TextualParameter 测试 |
| `internal/evolving/optimizer/llm_resilience/doc.go` | 包文档 |
| `internal/evolving/optimizer/llm_resilience/llm_resilience.go` | LLMInvokePolicy + InvokeTextWithRetry + InvokeTextWithRetryAndPrompt + 辅助函数 |
| `internal/evolving/optimizer/llm_resilience/llm_resilience_test.go` | LLMResilience 测试 |

### 修改文件

| 文件 | 修改内容 |
|---|---|
| `internal/evolving/doc.go` | 添加 trajectory/optimizer 子包到子包列表 |
| `internal/evolving/updater/protocol.go` | `trajectories []any` → `[]*trajectory.Trajectory`（2处） |
| `internal/evolving/updater/protocol_test.go` | mockUpdater 签名同步修改 |
| `internal/evolving/updater/single_dim/single_dim.go` | `opt any` → `opt optimizer.BaseOptimizer`，删除类型断言 |
| `internal/evolving/updater/single_dim/single_dim_test.go` | 重写 mockOptimizer 实现 BaseOptimizer |
| `internal/evolving/updater/multi_dim/multi_dim.go` | `domainOptimizers map[string]any` → `map[string]optimizer.BaseOptimizer` |
| `internal/evolving/updater/multi_dim/multi_dim_test.go` | 重写 mockDomainOptimizer |
| `IMPLEMENTATION_PLAN.md` | 9.77 ☐→✅，9.72e ☐→✅ |

---

### Task 1: Trajectory 包 doc.go + 核心类型 types.go

**Files:**
- Create: `internal/evolving/trajectory/doc.go`
- Create: `internal/evolving/trajectory/types.go`
- Test: `internal/evolving/trajectory/types_test.go`

- [ ] **Step 1: 创建 trajectory 目录**

```bash
mkdir -p internal/evolving/trajectory
```

- [ ] **Step 2: 创建 doc.go**

```go
// Package trajectory 提供自演化系统的执行轨迹数据类型。
//
// Trajectory 是 Agent 执行过程的完整记录，包含有序的 TrajectoryStep 列表。
// 每个 Step 通过 StepDetail 接口区分 LLM 调用（LLMCallDetail）和工具调用（ToolCallDetail）。
// Trajectory 是优化器 backward 阶段的单源真相（single source of truth）。
//
// 本包仅提供基础类型定义和序列化工具函数，Builder/Extractor/Aggregator/Registry/Store
// 等高级功能在后续章节实现。
//
// 文件目录：
//
//	trajectory/
//	├── doc.go           # 包文档
//	├── types.go         # 核心类型定义 + ToMessages
//	└── json_safe.go     # JSONSafe + MessageToDict + responseToText
//
// 对应 Python 代码：openjiuwen/agent_evolving/trajectory/types.py
package trajectory
```

- [ ] **Step 3: 创建 types.go**

```go
package trajectory

// ──────────────────────────── 结构体 ────────────────────────────

// StepDetail 执行步骤的详细数据接口。
//
// LLM 步骤由 LLMCallDetail 实现，工具步骤由 ToolCallDetail 实现。
// StepKind() 方法提供类型判别，也可通过 Go 类型断言 switch d.(type) 判别。
//
// 对应 Python: StepDetail = Union[LLMCallDetail, ToolCallDetail]
type StepDetail interface {
	// StepKind 返回步骤类型（llm 或 tool）。
	StepKind() StepKind
}

// LLMCallDetail LLM 调用完整执行数据。
//
// 对应 Python: LLMCallDetail dataclass
type LLMCallDetail struct {
	// Model 模型名称
	Model string
	// Messages 消息列表（原始消息对象，经 JSONSafe 处理后为可序列化形式）
	Messages []any
	// Response 模型响应（可选）
	Response any
	// Tools 工具定义列表（可选）
	Tools []map[string]any
	// Usage 使用量信息（可选）
	Usage map[string]any
	// Meta 扩展元数据
	Meta map[string]any
}

// ToolCallDetail 工具调用完整执行数据。
//
// 对应 Python: ToolCallDetail dataclass
type ToolCallDetail struct {
	// ToolName 工具名称
	ToolName string
	// CallArgs 调用参数（可选）
	CallArgs any
	// CallResult 调用结果（可选）
	CallResult any
	// ToolDescription 工具描述（可选）
	ToolDescription string
	// ToolSchema 工具 JSON Schema（可选）
	ToolSchema map[string]any
	// ToolCallID 工具调用 ID，用于脚本产物跟踪（可选）
	ToolCallID string
}

// TrajectoryStep 执行轨迹中的单个步骤。
//
// 字段分类：
//   - 核心执行事实：Kind, Error, StartTimeMs, EndTimeMs
//   - 结构化详情：Detail (LLMCallDetail | ToolCallDetail | nil)
//   - 后注入字段：Reward, PromptTokenIDs, CompletionTokenIDs, Logprobs
//   - 扩展元数据：Meta
//
// 对应 Python: TrajectoryStep dataclass
type TrajectoryStep struct {
	// Kind 步骤类型（llm/tool）
	Kind StepKind
	// Error 错误信息（可选）
	Error map[string]any
	// StartTimeMs 步骤开始时间（毫秒时间戳，可选）
	StartTimeMs int
	// EndTimeMs 步骤结束时间（毫秒时间戳，可选）
	EndTimeMs int
	// Detail 结构化步骤数据（LLMCallDetail 或 ToolCallDetail，可选）
	Detail StepDetail
	// Reward 标量奖励，来自 PRM 或 SignalDetector（可选）
	Reward float64
	// PromptTokenIDs 提示词 token ID 列表，仅 kind=llm（可选）
	PromptTokenIDs []int
	// CompletionTokenIDs 补全 token ID 列表，仅 kind=llm（可选）
	CompletionTokenIDs []int
	// Logprobs token 对数概率，仅 kind=llm（可选）
	Logprobs any
	// Meta 扩展元数据，包含 operator_id、agent_id、invoke 关系等
	Meta map[string]any
}

// Trajectory 完整执行轨迹。
//
// 对应 Python: Trajectory dataclass
type Trajectory struct {
	// ExecutionID 唯一执行标识符
	ExecutionID string
	// Steps 有序执行步骤列表
	Steps []*TrajectoryStep
	// Source 执行来源："online"（deepagents）或 "offline"（trainer）
	Source string
	// CaseID 离线模式下的数据集用例标识（可选）
	CaseID string
	// SessionID 在线模式下的会话 ID（可选）
	SessionID string
	// Cost 聚合成本指标（可选）
	Cost CostInfo
	// Meta 扩展元数据，包含 member_id、member_count 等
	Meta map[string]any
}

// CostInfo 聚合成本指标。
//
// 对应 Python: CostInfo = Dict[str, int]  # {"input_tokens": N, "output_tokens": M}
type CostInfo map[string]int

// ──────────────────────────── 枚举 ────────────────────────────

// StepKind 执行步骤类型。
//
// 对应 Python: StepKind = Literal["llm", "tool"]
type StepKind string

const (
	// StepKindLLM LLM 调用步骤
	StepKindLLM StepKind = "llm"
	// StepKindTool 工具调用步骤
	StepKindTool StepKind = "tool"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// StepKind 返回 StepKindLLM，实现 StepDetail 接口。
func (d *LLMCallDetail) StepKind() StepKind { return StepKindLLM }

// StepKind 返回 StepKindTool，实现 StepDetail 接口。
func (d *ToolCallDetail) StepKind() StepKind { return StepKindTool }

// ToMessages 返回 LLM 步骤中记录的消息类字典列表。
//
// 遍历所有 kind=llm 且 detail 为 LLMCallDetail 的步骤，
// 提取 messages 和 response，通过 MessageToDict 标准化为字典。
//
// 对应 Python: Trajectory.to_messages()
func (t *Trajectory) ToMessages() []map[string]any {
	messages := make([]map[string]any, 0)
	for _, step := range t.Steps {
		if step.Kind != StepKindLLM {
			continue
		}
		llmDetail, ok := step.Detail.(*LLMCallDetail)
		if !ok {
			continue
		}
		for _, msg := range llmDetail.Messages {
			messages = append(messages, MessageToDict(msg))
		}
		if llmDetail.Response != nil {
			responseMsg := MessageToDict(llmDetail.Response)
			if _, hasRole := responseMsg["role"]; hasRole {
				messages = append(messages, responseMsg)
			} else if _, hasContent := responseMsg["content"]; hasContent {
				messages = append(messages, responseMsg)
			}
		}
	}
	return messages
}
```

- [ ] **Step 4: 创建 types_test.go**

```go
package trajectory

import "testing"

// TestStepKind_常量值 验证 StepKind 常量对齐 Python Literal
func TestStepKind_常量值(t *testing.T) {
	if StepKindLLM != "llm" {
		t.Errorf("StepKindLLM = %q, want %q", StepKindLLM, "llm")
	}
	if StepKindTool != "tool" {
		t.Errorf("StepKindTool = %q, want %q", StepKindTool, "tool")
	}
}

// TestLLMCallDetail_StepKind 验证 LLMCallDetail 实现 StepDetail 接口
func TestLLMCallDetail_StepKind(t *testing.T) {
	var d StepDetail = &LLMCallDetail{}
	if d.StepKind() != StepKindLLM {
		t.Errorf("LLMCallDetail.StepKind() = %v, want %v", d.StepKind(), StepKindLLM)
	}
}

// TestToolCallDetail_StepKind 验证 ToolCallDetail 实现 StepDetail 接口
func TestToolCallDetail_StepKind(t *testing.T) {
	var d StepDetail = &ToolCallDetail{}
	if d.StepKind() != StepKindTool {
		t.Errorf("ToolCallDetail.StepKind() = %v, want %v", d.StepKind(), StepKindTool)
	}
}

// TestTrajectoryStep_字段 验证 TrajectoryStep 字段赋值
func TestTrajectoryStep_字段(t *testing.T) {
	step := &TrajectoryStep{
		Kind:           StepKindLLM,
		StartTimeMs:    1000,
		EndTimeMs:      2000,
		Detail:         &LLMCallDetail{Model: "qwen-max"},
		Reward:         0.8,
		PromptTokenIDs: []int{1, 2, 3},
		Meta:           map[string]any{"operator_id": "agent1/llm_main"},
	}
	if step.Kind != StepKindLLM {
		t.Errorf("Kind = %v, want %v", step.Kind, StepKindLLM)
	}
	if step.StartTimeMs != 1000 {
		t.Errorf("StartTimeMs = %d, want 1000", step.StartTimeMs)
	}
	if step.Reward != 0.8 {
		t.Errorf("Reward = %f, want 0.8", step.Reward)
	}
}

// TestTrajectory_默认Source 验证 Trajectory 默认 Source 为空（需调用方设置 "offline"）
func TestTrajectory_默认Source(t *testing.T) {
	traj := &Trajectory{ExecutionID: "test-001", Steps: []*TrajectoryStep{}}
	// Go 中 struct 无默认值机制，Source 默认为空字符串
	// 对齐 Python: source: str = "offline" 需在构造时显式设置
	if traj.Source != "" {
		t.Errorf("default Source = %q, want empty string", traj.Source)
	}
}

// TestTrajectory_ToMessages_空轨迹 验证空轨迹返回空列表
func TestTrajectory_ToMessages_空轨迹(t *testing.T) {
	traj := &Trajectory{ExecutionID: "test", Steps: []*TrajectoryStep{}}
	msgs := traj.ToMessages()
	if len(msgs) != 0 {
		t.Errorf("ToMessages on empty trajectory returned %d messages, want 0", len(msgs))
	}
}

// TestTrajectory_ToMessages_只有LLM步骤 验证提取 LLM 步骤消息
func TestTrajectory_ToMessages_只有LLM步骤(t *testing.T) {
	traj := &Trajectory{
		ExecutionID: "test",
		Steps: []*TrajectoryStep{
			{
				Kind: StepKindLLM,
				Detail: &LLMCallDetail{
					Messages: []any{
						map[string]any{"role": "user", "content": "hello"},
					},
					Response: map[string]any{"role": "assistant", "content": "hi"},
				},
			},
		},
	}
	msgs := traj.ToMessages()
	if len(msgs) != 2 {
		t.Fatalf("ToMessages returned %d messages, want 2", len(msgs))
	}
	if msgs[0]["role"] != "user" {
		t.Errorf("msgs[0][role] = %v, want user", msgs[0]["role"])
	}
	if msgs[1]["role"] != "assistant" {
		t.Errorf("msgs[1][role] = %v, want assistant", msgs[1]["role"])
	}
}

// TestTrajectory_ToMessages_跳过工具步骤 验证工具步骤被跳过
func TestTrajectory_ToMessages_跳过工具步骤(t *testing.T) {
	traj := &Trajectory{
		ExecutionID: "test",
		Steps: []*TrajectoryStep{
			{Kind: StepKindTool, Detail: &ToolCallDetail{ToolName: "search"}},
			{Kind: StepKindLLM, Detail: &LLMCallDetail{
				Messages: []any{map[string]any{"role": "user", "content": "hi"}},
			}},
		},
	}
	msgs := traj.ToMessages()
	if len(msgs) != 1 {
		t.Errorf("ToMessages returned %d messages, want 1 (tool step skipped)", len(msgs))
	}
}

// TestTrajectory_ToMessages_nilResponse 验证 nil response 不追加消息
func TestTrajectory_ToMessages_nilResponse(t *testing.T) {
	traj := &Trajectory{
		ExecutionID: "test",
		Steps: []*TrajectoryStep{
			{
				Kind:   StepKindLLM,
				Detail: &LLMCallDetail{Messages: []any{map[string]any{"role": "user", "content": "hi"}}, Response: nil},
			},
		},
	}
	msgs := traj.ToMessages()
	if len(msgs) != 1 {
		t.Errorf("ToMessages returned %d messages, want 1 (nil response not appended)", len(msgs))
	}
}

// TestTrajectory_ToMessages_nilDetail 验证 nil detail 步骤被跳过
func TestTrajectory_ToMessages_nilDetail(t *testing.T) {
	traj := &Trajectory{
		ExecutionID: "test",
		Steps: []*TrajectoryStep{
			{Kind: StepKindLLM, Detail: nil},
		},
	}
	msgs := traj.ToMessages()
	if len(msgs) != 0 {
		t.Errorf("ToMessages returned %d messages, want 0 (nil detail skipped)", len(msgs))
	}
}

// TestTrajectory_ToMessages_detail类型断言失败 验证 Detail 非 LLMCallDetail 时跳过
func TestTrajectory_ToMessages_detail类型断言失败(t *testing.T) {
	traj := &Trajectory{
		ExecutionID: "test",
		Steps: []*TrajectoryStep{
			{Kind: StepKindLLM, Detail: &ToolCallDetail{ToolName: "search"}},
		},
	}
	msgs := traj.ToMessages()
	if len(msgs) != 0 {
		t.Errorf("ToMessages returned %d messages, want 0 (wrong detail type skipped)", len(msgs))
	}
}

// TestCostInfo_用法 验证 CostInfo map 用法
func TestCostInfo_用法(t *testing.T) {
	cost := CostInfo{"input_tokens": 100, "output_tokens": 50}
	if cost["input_tokens"] != 100 {
		t.Errorf("input_tokens = %d, want 100", cost["input_tokens"])
	}
	if cost["output_tokens"] != 50 {
		t.Errorf("output_tokens = %d, want 50", cost["output_tokens"])
	}
}
```

- [ ] **Step 5: 运行测试确认编译通过**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill 2>/dev/null; export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/trajectory/... -v -count=1
```

Expected: 编译通过，但 ToMessages 引用了 MessageToDict（尚未创建），会编译失败 — 这是预期的，将在 Task 2 中修复。

- [ ] **Step 6: Commit**

```bash
git add internal/evolving/trajectory/doc.go internal/evolving/trajectory/types.go internal/evolving/trajectory/types_test.go
git commit -m "feat(evolving): 添加 trajectory 包核心类型定义 (9.77 Step 1/3)"
```

---

### Task 2: Trajectory json_safe.go 工具函数

**Files:**
- Create: `internal/evolving/trajectory/json_safe.go`
- Create: `internal/evolving/trajectory/json_safe_test.go`

- [ ] **Step 1: 创建 json_safe.go**

```go
package trajectory

import (
	"encoding/json"
	"fmt"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// JSONSafe 递归转换常见消息/工具调用对象为可 JSON 序列化的值。
//
// 处理规则：
//   - nil → nil
//   - string/int/float64/bool → 原值
//   - []any → 递归每个元素
//   - map[string]any → 递归 value
//   - 其他类型 → json.Marshal→json.Unmarshal 到 any（兜底，利用 Go JSON 序列化链）
//   - Marshal 失败 → fmt.Sprint(value) 转字符串
//
// 对应 Python: _json_safe(value)
func JSONSafe(value any) any {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case string, int, float64, bool:
		return v
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = JSONSafe(item)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			result[key] = JSONSafe(item)
		}
		return result
	default:
		// 兜底：json.Marshal→json.Unmarshal，利用 Go 的 json.Marshaler 接口
		// 等价于 Python 的 getattr(value, "model_dump", None)
		b, err := json.Marshal(value)
		if err != nil {
			return fmt.Sprint(value)
		}
		var unmarshalled any
		if err := json.Unmarshal(b, &unmarshalled); err != nil {
			return fmt.Sprint(value)
		}
		return JSONSafe(unmarshalled)
	}
}

// MessageToDict 将运行时消息对象标准化为消息类字典。
//
// 处理规则：
//  1. 已经是 map[string]any → 直接 JSONSafe
//  2. 尝试 json.Marshal→json.Unmarshal 到 map[string]any → JSONSafe
//  3. 兜底 → {"role": "unknown", "content": fmt.Sprint(msg)}
//
// 对应 Python: Trajectory._message_to_dict(message)
func MessageToDict(msg any) map[string]any {
	if msg == nil {
		return map[string]any{"role": "unknown", "content": ""}
	}
	// 分支1: 已经是 map
	if m, ok := msg.(map[string]any); ok {
		return JSONSafe(m).(map[string]any)
	}
	// 分支2+3: 尝试 JSON 序列化（等价于 Python getattr + model_dump）
	b, err := json.Marshal(msg)
	if err == nil {
		var m map[string]any
		if json.Unmarshal(b, &m) == nil {
			return JSONSafe(m).(map[string]any)
		}
	}
	// 兜底
	return map[string]any{"role": "unknown", "content": fmt.Sprint(msg)}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// responseToText 从 LLM 响应中提取文本内容。
//
// 处理规则：
//  1. 有 Content 字段（断言为有 Content 方法的接口）→ 返回 Content
//  2. map[string]any → 取 "content" 或 "text" 键
//  3. 兜底 → fmt.Sprint(response)
//
// 对应 Python: _response_to_text(response)
func responseToText(response any) string {
	if response == nil {
		return ""
	}
	// 分支1: 有 Content 方法
	type contenter interface {
		Content() string
	}
	if c, ok := response.(contenter); ok {
		return c.Content()
	}
	// 分支2: map[string]any
	if m, ok := response.(map[string]any); ok {
		if content, _ := m["content"].(string); content != "" {
			return content
		}
		if text, _ := m["text"].(string); text != "" {
			return text
		}
	}
	// 分支3: 兜底
	return fmt.Sprint(response)
}
```

- [ ] **Step 2: 创建 json_safe_test.go**

```go
package trajectory

import "testing"

// TestJSONSafe_nil 验证 nil 返回 nil
func TestJSONSafe_nil(t *testing.T) {
	if JSONSafe(nil) != nil {
		t.Error("JSONSafe(nil) should return nil")
	}
}

// TestJSONSafe_基础类型 验证基础类型原值返回
func TestJSONSafe_基础类型(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  any
	}{
		{"string", "hello", "hello"},
		{"int", 42, 42},
		{"float64", 3.14, 3.14},
		{"bool", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if JSONSafe(tt.input) != tt.want {
				t.Errorf("JSONSafe(%v) = %v, want %v", tt.input, JSONSafe(tt.input), tt.want)
			}
		})
	}
}

// TestJSONSafe_切片 验证切片递归
func TestJSONSafe_切片(t *testing.T) {
	input := []any{"a", 1, true}
	result := JSONSafe(input).([]any)
	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}
	if result[0] != "a" || result[1] != 1 || result[2] != true {
		t.Errorf("JSONSafe slice result = %v, want [a 1 true]", result)
	}
}

// TestJSONSafe_映射 验证映射递归
func TestJSONSafe_映射(t *testing.T) {
	input := map[string]any{"key": "value", "num": 42}
	result := JSONSafe(input).(map[string]any)
	if result["key"] != "value" || result["num"] != 42 {
		t.Errorf("JSONSafe map result = %v", result)
	}
}

// TestJSONSafe_嵌套 验证嵌套结构递归
func TestJSONSafe_嵌套(t *testing.T) {
	input := map[string]any{
		"list": []any{1, "two"},
		"nested": map[string]any{
			"deep": true,
		},
	}
	result := JSONSafe(input).(map[string]any)
	list := result["list"].([]any)
	if list[0] != 1 || list[1] != "two" {
		t.Errorf("nested list = %v", list)
	}
	nested := result["nested"].(map[string]any)
	if nested["deep"] != true {
		t.Errorf("nested.deep = %v, want true", nested["deep"])
	}
}

// TestJSONSafe_自定义对象兜底 验证自定义对象走 json.Marshal 路径
func TestJSONSafe_自定义对象兜底(t *testing.T) {
	type testObj struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	obj := testObj{Name: "test", Age: 25}
	result := JSONSafe(obj)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("JSONSafe custom object returned %T, want map[string]any", result)
	}
	if m["name"] != "test" {
		t.Errorf("name = %v, want test", m["name"])
	}
	// JSON number 解析为 float64
	if age, _ := m["age"].(float64); age != 25 {
		t.Errorf("age = %v, want 25", m["age"])
	}
}

// TestJSONSafe_不可序列化对象 验证 Marshal 失败时走 fmt.Sprint 兜底
func TestJSONSafe_不可序列化对象(t *testing.T) {
	// channel 不可 json.Marshal
	ch := make(chan int)
	result := JSONSafe(ch)
	str, ok := result.(string)
	if !ok {
		t.Fatalf("JSONSafe unserializable returned %T, want string", result)
	}
	if str == "" {
		t.Error("fallback string should not be empty")
	}
}

// TestMessageToDict_map输入 验证已经是 map 的情况
func TestMessageToDict_map输入(t *testing.T) {
	input := map[string]any{"role": "user", "content": "hello"}
	result := MessageToDict(input)
	if result["role"] != "user" {
		t.Errorf("role = %v, want user", result["role"])
	}
	if result["content"] != "hello" {
		t.Errorf("content = %v, want hello", result["content"])
	}
}

// TestMessageToDict_结构体输入 验证结构体走 JSON 序列化路径
func TestMessageToDict_结构体输入(t *testing.T) {
	type testMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	msg := testMsg{Role: "assistant", Content: "response"}
	result := MessageToDict(msg)
	if result["role"] != "assistant" {
		t.Errorf("role = %v, want assistant", result["role"])
	}
}

// TestMessageToDict_nil 验证 nil 输入
func TestMessageToDict_nil(t *testing.T) {
	result := MessageToDict(nil)
	if result["role"] != "unknown" {
		t.Errorf("role = %v, want unknown", result["role"])
	}
}

// TestMessageToDict_不可序列化 验证兜底
func TestMessageToDict_不可序列化(t *testing.T) {
	ch := make(chan int)
	result := MessageToDict(ch)
	if result["role"] != "unknown" {
		t.Errorf("role = %v, want unknown", result["role"])
	}
	if result["content"] == "" {
		t.Error("content should not be empty in fallback")
	}
}

// TestResponseToText_map_content 验证 map 取 content 键
func TestResponseToText_map_content(t *testing.T) {
	result := responseToText(map[string]any{"content": "hello world"})
	if result != "hello world" {
		t.Errorf("responseToText = %q, want %q", result, "hello world")
	}
}

// TestResponseToText_map_text 验证 map 取 text 键（无 content 时）
func TestResponseToText_map_text(t *testing.T) {
	result := responseToText(map[string]any{"text": "hello"})
	if result != "hello" {
		t.Errorf("responseToText = %q, want %q", result, "hello")
	}
}

// TestResponseToText_nil 验证 nil 返回空字符串
func TestResponseToText_nil(t *testing.T) {
	result := responseToText(nil)
	if result != "" {
		t.Errorf("responseToText(nil) = %q, want empty", result)
	}
}

// TestResponseToText_兜底 验证其他类型走 fmt.Sprint
func TestResponseToText_兜底(t *testing.T) {
	result := responseToText(42)
	if result != "42" {
		t.Errorf("responseToText(42) = %q, want %q", result, "42")
	}
}
```

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill 2>/dev/null; export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/trajectory/... -v -count=1
```

Expected: 全部 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/evolving/trajectory/json_safe.go internal/evolving/trajectory/json_safe_test.go
git commit -m "feat(evolving): 添加 trajectory JSONSafe/MessageToDict 工具函数 (9.77 Step 2/3)"
```

---

### Task 3: 更新 evolving/doc.go 添加 trajectory 子包

**Files:**
- Modify: `internal/evolving/doc.go`

- [ ] **Step 1: 更新 doc.go 子包列表**

在 `evolving/doc.go` 的子包列表中添加 `trajectory/`：

将：
```
// 子包：
//
//	dataset/    # 数据集加载与用例管理
//	evaluator/  # 评估器与评估指标
//	schema/     # 进化协议与数据结构
//	trainer/    # 训练执行器与进度管理
```

替换为：
```
// 子包：
//
//	dataset/     # 数据集加载与用例管理
//	evaluator/   # 评估器与评估指标
//	schema/      # 进化协议与数据结构
//	trainer/     # 训练执行器与进度管理
//	trajectory/  # 执行轨迹数据类型
```

- [ ] **Step 2: Commit**

```bash
git add internal/evolving/doc.go
git commit -m "docs(evolving): 添加 trajectory 子包到 doc.go (9.77 Step 3/3)"
```

---

### Task 4: Optimizer 包 doc.go + base.go

**Files:**
- Create: `internal/evolving/optimizer/doc.go`
- Create: `internal/evolving/optimizer/base.go`
- Test: `internal/evolving/optimizer/base_test.go`

- [ ] **Step 1: 创建 optimizer 目录**

```bash
mkdir -p internal/evolving/optimizer/llm_resilience
```

- [ ] **Step 2: 创建 optimizer/doc.go**

```go
// Package optimizer 提供自演化系统的维度优化器。
//
// BaseOptimizer 定义优化器的公共接口和 Mixin 辅助结构体，
// 子优化器嵌入 Mixin 获得公共字段和方法，自己实现 Backward/Step 等核心方法。
// TextualParameter 是梯度容器，存储 target→梯度值和可选描述。
//
// 文件目录：
//
//	optimizer/
//	├── doc.go                # 包文档
//	├── base.go               # BaseOptimizer 接口 + BaseOptimizerMixin + TextualParameter
//	└── llm_resilience/       # LLM 弹性重试策略
//	    ├── doc.go            # 包文档
//	    └── llm_resilience.go # LLMInvokePolicy + InvokeTextWithRetry
//
// 对应 Python 代码：openjiuwen/agent_evolving/optimizer/base.py
package optimizer
```

- [ ] **Step 3: 创建 optimizer/base.go**

```go
package optimizer

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TextualParameter operator_id 的梯度容器，存储 target→梯度值和可选描述。
// 不再持有 Operator 引用。
//
// 对应 Python: TextualParameter
type TextualParameter struct {
	// OperatorID 所属 Operator 标识
	OperatorID string
	// Gradients 梯度映射 target → gradient value (string 或 []string)
	Gradients map[string]any
	// Description 可选描述
	Description string
}

// BaseOptimizerMixin 优化器公共逻辑嵌入结构体。
//
// 子优化器嵌入此结构体，获得公共字段和辅助方法（Bind/AddTrajectory/ValidateParameters 等），
// 然后自己实现 BaseOptimizer 接口的全部方法。
//
// 典型子优化器实现模式：
//   - Domain()/RequiresForwardData()/DefaultTargets() — 返回维度常量
//   - Bind() — 委托 o.BaseOptimizerMixin.Bind()
//   - AddTrajectory()/GetTrajectories()/ClearTrajectories() — 委托 Mixin
//   - Backward() — 调用 Mixin.ValidateParameters() + SelectSignals() + 子类逻辑 + 错误包装
//   - Step() — 调用 Mixin.ValidateParameters() + 子类逻辑 + ClearTrajectories()
//   - Parameters()/SelectSignals() — 委托 Mixin
type BaseOptimizerMixin struct {
	// operators 绑定的 Operator 映射
	operators map[string]operator.Operator
	// parameters 梯度容器映射
	parameters map[string]*TextualParameter
	// targets 优化目标列表
	targets []string
	// trajectories 缓存的执行轨迹列表
	trajectories []*trajectory.Trajectory
	// selectedSignals 选中的演化信号列表
	selectedSignals []*signal.EvolutionSignal
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent optimizer 包日志组件常量
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTextualParameter 创建 TextualParameter 实例。
func NewTextualParameter(operatorID string) *TextualParameter {
	return &TextualParameter{
		OperatorID: operatorID,
		Gradients:  map[string]any{},
	}
}

// SetGradient 设置目标梯度值。
func (p *TextualParameter) SetGradient(name string, gradient any) {
	p.Gradients[name] = gradient
}

// GetGradient 获取目标梯度值。
func (p *TextualParameter) GetGradient(name string) any {
	return p.Gradients[name]
}

// SetDescription 设置描述。
func (p *TextualParameter) SetDescription(description string) {
	p.Description = description
}

// GetDescription 获取描述。
func (p *TextualParameter) GetDescription() string {
	return p.Description
}

// Bind 过滤并绑定可优化的 Operator，返回匹配数量；0 触发上层软退出。
//
// 对齐 Python:
//
//	self._targets = list(targets or self.default_targets())
//	self._operators = self.filter_operators(operators, self._targets)
//	self._parameters = {op_id: TextualParameter(operator_id=op_id) for op_id in self._operators}
//	self._trajectories = []
//	self._selected_signals = []
//
// 对应 Python: BaseOptimizer.bind()
func (m *BaseOptimizerMixin) Bind(operators map[string]operator.Operator, targets []string, config map[string]any) int {
	m.targets = targets
	m.operators = FilterOperators(operators, m.targets)
	m.parameters = make(map[string]*TextualParameter, len(m.operators))
	for opID := range m.operators {
		m.parameters[opID] = NewTextualParameter(opID)
	}
	m.trajectories = nil
	m.selectedSignals = nil
	if len(m.operators) == 0 {
		logger.Error(logComponent).
			Str("method", "Bind").
			Strs("targets", m.targets).
			Msg("[optimizer] no operator matches targets; will soft-exit")
	}
	return len(m.operators)
}

// AddTrajectory 缓存 Trajectory 供 backward 阶段查询。
//
// 对应 Python: BaseOptimizer.add_trajectory()
func (m *BaseOptimizerMixin) AddTrajectory(traj *trajectory.Trajectory) {
	m.trajectories = append(m.trajectories, traj)
}

// GetTrajectories 返回当前缓存的轨迹列表（副本）。
//
// 对应 Python: BaseOptimizer.get_trajectories()
func (m *BaseOptimizerMixin) GetTrajectories() []*trajectory.Trajectory {
	result := make([]*trajectory.Trajectory, len(m.trajectories))
	copy(result, m.trajectories)
	return result
}

// ClearTrajectories 清空轨迹缓存。
//
// 对应 Python: BaseOptimizer.clear_trajectories()
func (m *BaseOptimizerMixin) ClearTrajectories() {
	m.trajectories = nil
}

// Parameters 返回梯度容器的副本。
//
// 对应 Python: BaseOptimizer.parameters()
func (m *BaseOptimizerMixin) Parameters() map[string]*TextualParameter {
	result := make(map[string]*TextualParameter, len(m.parameters))
	for k, v := range m.parameters {
		result[k] = v
	}
	return result
}

// SelectSignals 选择此优化器可消费的信号。默认保留全部信号。
//
// 对应 Python: BaseOptimizer._select_signals()
func (m *BaseOptimizerMixin) SelectSignals(signals []*signal.EvolutionSignal) []*signal.EvolutionSignal {
	result := make([]*signal.EvolutionSignal, len(signals))
	copy(result, signals)
	return result
}

// ValidateParameters 空参数校验，参数为空时抛异常。
//
// 对应 Python: BaseOptimizer._validate_parameters()
func (m *BaseOptimizerMixin) ValidateParameters() {
	if len(m.parameters) == 0 {
		panic(exception.NewBaseError(
			exception.NewStatusCode("TOOLCHAIN_AGENT_PARAM_ERROR", 170000, ""),
			exception.WithMsg("cannot optimize empty parameters"),
		))
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// FilterOperators 过滤暴露任何 target 的 Operator。对不匹配的记录警告，不中断。
//
// 对应 Python: BaseOptimizer.filter_operators()
func FilterOperators(operators map[string]operator.Operator, targets []string) map[string]operator.Operator {
	out := make(map[string]operator.Operator)
	for opID, op := range operators {
		tunables := op.GetTunables()
		matched := false
		for _, t := range targets {
			if _, exists := tunables[t]; exists {
				matched = true
				break
			}
		}
		if !matched {
			logger.Warn(logComponent).
				Str("method", "FilterOperators").
				Str("operator_id", opID).
				Strs("targets", targets).
				Msg("[optimizer] operator has no tunables in targets")
			continue
		}
		out[opID] = op
	}
	return out
}
```

- [ ] **Step 4: 创建 optimizer/base_test.go**

测试文件需包含 TextualParameter 全部方法、BaseOptimizerMixin 的 Bind/AddTrajectory/GetTrajectories/ClearTrajectories/Parameters/FilterOperators/ValidateParameters/SelectSignals 测试。由于篇幅限制，此处列出核心测试框架，实现时应逐方法覆盖。

```go
package optimizer

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockOperator 用于测试的模拟 Operator
type mockOperator struct {
	operatorID string
	tunables   map[string]operator.TunableSpec
	state      map[string]any
}

func (m *mockOperator) OperatorID() string                          { return m.operatorID }
func (m *mockOperator) GetTunables() map[string]operator.TunableSpec { return m.tunables }
func (m *mockOperator) GetState() map[string]any                    { return m.state }
func (m *mockOperator) SetParameter(target string, value any)       {}
func (m *mockOperator) ApplyUpdate(target string, update any) any   { return nil }
func (m *mockOperator) LoadState(state map[string]any)              {}

// ──────────────────────────── 导出函数 ────────────────────────────

// TextualParameter 测试
func TestTextualParameter_梯度操作(t *testing.T) {
	p := NewTextualParameter("op1")
	p.SetGradient("system_prompt", "improved prompt")
	if p.GetGradient("system_prompt") != "improved prompt" {
		t.Error("gradient mismatch")
	}
	if p.GetGradient("nonexistent") != nil {
		t.Error("nonexistent gradient should be nil")
	}
}

func TestTextualParameter_描述操作(t *testing.T) {
	p := NewTextualParameter("op1")
	p.SetDescription("test description")
	if p.GetDescription() != "test description" {
		t.Error("description mismatch")
	}
}

func TestTextualParameter_OperatorID(t *testing.T) {
	p := NewTextualParameter("agent1/llm_main")
	if p.OperatorID != "agent1/llm_main" {
		t.Errorf("OperatorID = %q, want %q", p.OperatorID, "agent1/llm_main")
	}
}

// BaseOptimizerMixin.Bind 测试
func TestBaseOptimizerMixin_Bind匹配(t *testing.T) {
	m := &BaseOptimizerMixin{}
	ops := map[string]operator.Operator{
		"op1": &mockOperator{
			operatorID: "op1",
			tunables:   map[string]operator.TunableSpec{"system_prompt": {Name: "system_prompt"}},
		},
	}
	n := m.Bind(ops, []string{"system_prompt"}, nil)
	if n != 1 {
		t.Errorf("Bind returned %d, want 1", n)
	}
	if len(m.parameters) != 1 {
		t.Errorf("parameters count = %d, want 1", len(m.parameters))
	}
	if len(m.trajectories) != 0 {
		t.Errorf("trajectories should be empty after bind")
	}
}

func TestBaseOptimizerMixin_Bind无匹配(t *testing.T) {
	m := &BaseOptimizerMixin{}
	ops := map[string]operator.Operator{
		"op1": &mockOperator{
			operatorID: "op1",
			tunables:   map[string]operator.TunableSpec{"tool_description": {Name: "tool_description"}},
		},
	}
	n := m.Bind(ops, []string{"system_prompt"}, nil)
	if n != 0 {
		t.Errorf("Bind returned %d, want 0", n)
	}
}

// BaseOptimizerMixin.AddTrajectory/GetTrajectories/ClearTrajectories 测试
func TestBaseOptimizerMixin_轨迹缓存(t *testing.T) {
	m := &BaseOptimizerMixin{}
	traj1 := &trajectory.Trajectory{ExecutionID: "exec1", Source: "offline"}
	traj2 := &trajectory.Trajectory{ExecutionID: "exec2", Source: "offline"}

	m.AddTrajectory(traj1)
	m.AddTrajectory(traj2)

	trajs := m.GetTrajectories()
	if len(trajs) != 2 {
		t.Fatalf("GetTrajectories returned %d, want 2", len(trajs))
	}
	if trajs[0].ExecutionID != "exec1" {
		t.Errorf("trajs[0].ExecutionID = %q, want exec1", trajs[0].ExecutionID)
	}

	m.ClearTrajectories()
	if len(m.GetTrajectories()) != 0 {
		t.Error("ClearTrajectories should empty the list")
	}
}

// BaseOptimizerMixin.Parameters 测试
func TestBaseOptimizerMixin_Parameters副本(t *testing.T) {
	m := &BaseOptimizerMixin{}
	ops := map[string]operator.Operator{
		"op1": &mockOperator{
			operatorID: "op1",
			tunables:   map[string]operator.TunableSpec{"system_prompt": {Name: "system_prompt"}},
		},
	}
	m.Bind(ops, []string{"system_prompt"}, nil)

	params := m.Parameters()
	// 修改副本不应影响原始
	delete(params, "op1")
	if _, ok := m.parameters["op1"]; !ok {
		t.Error("modifying Parameters() copy should not affect original")
	}
}

// BaseOptimizerMixin.SelectSignals 测试
func TestBaseOptimizerMixin_SelectSignals默认全选(t *testing.T) {
	m := &BaseOptimizerMixin{}
	signals := []*signal.EvolutionSignal{
		{SignalType: "low_score"},
		{SignalType: "error"},
	}
	selected := m.SelectSignals(signals)
	if len(selected) != 2 {
		t.Errorf("SelectSignals returned %d, want 2", len(selected))
	}
}

// BaseOptimizerMixin.ValidateParameters 测试
func TestBaseOptimizerMixin_ValidateParameters空时panic(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("ValidateParameters should panic on empty parameters")
		}
	}()
	m := &BaseOptimizerMixin{}
	m.ValidateParameters()
}

func TestBaseOptimizerMixin_ValidateParameters有参数时不panic(t *testing.T) {
	m := &BaseOptimizerMixin{}
	ops := map[string]operator.Operator{
		"op1": &mockOperator{
			operatorID: "op1",
			tunables:   map[string]operator.TunableSpec{"system_prompt": {Name: "system_prompt"}},
		},
	}
	m.Bind(ops, []string{"system_prompt"}, nil)
	// 不应 panic
	m.ValidateParameters()
}

// FilterOperators 测试
func TestFilterOperators_匹配(t *testing.T) {
	ops := map[string]operator.Operator{
		"op1": &mockOperator{
			operatorID: "op1",
			tunables:   map[string]operator.TunableSpec{"system_prompt": {Name: "system_prompt"}},
		},
	}
	filtered := FilterOperators(ops, []string{"system_prompt"})
	if len(filtered) != 1 {
		t.Errorf("FilterOperators returned %d, want 1", len(filtered))
	}
}

func TestFilterOperators_不匹配(t *testing.T) {
	ops := map[string]operator.Operator{
		"op1": &mockOperator{
			operatorID: "op1",
			tunables:   map[string]operator.TunableSpec{"tool_description": {Name: "tool_description"}},
		},
	}
	filtered := FilterOperators(ops, []string{"system_prompt"})
	if len(filtered) != 0 {
		t.Errorf("FilterOperators returned %d, want 0", len(filtered))
	}
}

func TestFilterOperators_空操作符(t *testing.T) {
	filtered := FilterOperators(nil, []string{"system_prompt"})
	if len(filtered) != 0 {
		t.Errorf("FilterOperators returned %d, want 0", len(filtered))
	}
}
```

- [ ] **Step 5: 运行测试**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill 2>/dev/null; export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/optimizer/... -v -count=1
```

Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/evolving/optimizer/doc.go internal/evolving/optimizer/base.go internal/evolving/optimizer/base_test.go
git commit -m "feat(evolving): 添加 optimizer 包 BaseOptimizer 接口+Mixin+TextualParameter (9.72e Step 1/3)"
```

---

### Task 5: LLMResilience 子包

**Files:**
- Create: `internal/evolving/optimizer/llm_resilience/doc.go`
- Create: `internal/evolving/optimizer/llm_resilience/llm_resilience.go`
- Create: `internal/evolving/optimizer/llm_resilience/llm_resilience_test.go`

- [ ] **Step 1: 创建 llm_resilience/doc.go**

```go
// Package llm_resilience 提供演化层的 LLM 弹性重试策略。
//
// LLMInvokePolicy 控制单次尝试超时、总预算、最大尝试次数和指数退避。
// InvokeTextWithRetry 和 InvokeTextWithRetryAndPrompt 提供带重试的 LLM 文本调用，
// 处理三种失败模式：调用异常、空响应、不可用响应。
//
// 总预算控制采用双重方案：外层 context.WithTimeout + 每次 attempt 前手动检查剩余预算。
//
// 文件目录：
//
//	llm_resilience/
//	├── doc.go                # 包文档
//	├── llm_resilience.go     # LLMInvokePolicy + InvokeTextWithRetry
//	└── llm_resilience_test.go # 测试
//
// 对应 Python 代码：openjiuwen/agent_evolving/optimizer/llm_resilience.py
package llm_resilience
```

- [ ] **Step 2: 创建 llm_resilience.go**

完整实现 `LLMInvokePolicy`、`InvokeTextWithRetry`、`InvokeTextWithRetryAndPrompt`、`isTimeoutLike`、`sleepBeforeRetry`、`raiseLLMResilienceError`、`InvokeRetryOption` 及其选项函数。对齐设计文档中描述的所有分支逻辑（三种失败模式、超时检测+retry_prompt回退、双重预算控制、指数退避）。具体代码参照设计文档第二步中 LLMResilience 部分。

关键实现要点：
- `InvokeTextWithRetryAndPrompt` 函数签名为 `func InvokeTextWithRetryAndPrompt(ctx context.Context, model *llm.Model, modelName string, prompt string, policy LLMInvokePolicy, opts ...InvokeRetryOption) (text string, promptUsed string, err error)`
- 使用 `context.WithTimeout(ctx, ...)` 创建 budgetCtx
- 每次 attempt 前手动检查 `time.Since(startedAt).Seconds() > policy.TotalBudgetSecs`
- 单次 timeout 取 `math.Min(policy.AttemptTimeoutSecs, remainingBudget)`
- LLM 调用使用 `model.Invoke(budgetCtx, messages, model_clients.WithInvokeModel(modelName), model_clients.WithInvokeTimeout(timeoutSecs), ...)`
- `isTimeoutLike` 检测 `context.DeadlineExceeded` + 类型名包含 "timeout" + 消息包含 "timeout"/"timed out"
- `sleepBeforeRetry` 使用 `select { case <-ctx.Done(): return ctx.Err(); case <-time.After(backoff): return nil }`
- `raiseLLMResilienceError` 使用 `exception.NewBaseError` + `exception.WithMsg` + `exception.WithCause`
- 错误原因常量：`reasonTotalBudgetExceeded`/`reasonInvokeFailed`/`reasonEmptyResponse`/`reasonUnusableResponse`
- StatusCode 使用已有的 `exception.NewStatusCode("TOOLCHAIN_EVOLVING_TOOL_CALL_LLM_CALL_EXECUTION_ERROR", 174031, "")` 和 `exception.NewStatusCode("TOOLCHAIN_EVOLVING_TOOL_CALL_OUTPUT_PARSE_ERROR", 174034, "")`

- [ ] **Step 3: 创建 llm_resilience_test.go**

使用 `httptest.NewServer` 创建模拟 LLM 服务器进行测试。覆盖以下场景：

1. `TestLLMInvokePolicy_默认值` — 验证结构体字段默认值
2. `TestInvokeTextWithRetry_成功` — 首次调用成功返回文本
3. `TestInvokeTextWithRetry_空响应重试` — 首次返回空、第二次返回有效文本
4. `TestInvokeTextWithRetry_调用失败` — 返回错误
5. `TestInvokeTextWithRetry_超时回退到RetryPrompt` — 首次超时后使用短 prompt 重试
6. `TestInvokeTextWithRetry_不可用响应` — isResultUsable 返回 false 时重试
7. `TestInvokeTextWithRetry_总预算超限` — policy.TotalBudgetSecs <= 0 直接报错
8. `TestIsTimeoutLike_各分支` — DeadlineExceeded/类型名/消息匹配
9. `TestSleepBeforeRetry_退避计算` — 验证指数退避和预算尊重
10. `TestRaiseLLMResilienceError_错误构建` — 验证错误包含 reason/attempts/last_response

- [ ] **Step 4: 运行测试**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill 2>/dev/null; export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/optimizer/llm_resilience/... -v -count=1
```

Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/evolving/optimizer/llm_resilience/
git commit -m "feat(evolving): 添加 LLMResilience 弹性重试策略 (9.72e Step 2/3)"
```

---

### Task 6: 更新 evolving/doc.go 添加 optimizer 子包 + 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/evolving/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 doc.go 添加 optimizer 子包**

在 `evolving/doc.go` 的子包列表中添加 `optimizer/`：

将：
```
//	trajectory/  # 执行轨迹数据类型
```

替换为：
```
//	trajectory/  # 执行轨迹数据类型
//	optimizer/   # 维度优化器基类 + LLM 弹性重试
```

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

将 9.77 行的 `☐` 改为 `✅`，9.72e 行的 `☐` 改为 `✅`。

- [ ] **Step 3: Commit**

```bash
git add internal/evolving/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(evolving): 更新 doc.go 和 IMPLEMENTATION_PLAN.md 标记 9.77+9.72e 完成 (9.72e Step 3/3)"
```

---

### Task 7: 回填 Updater protocol.go — trajectories []any → []*trajectory.Trajectory

**Files:**
- Modify: `internal/evolving/updater/protocol.go`
- Modify: `internal/evolving/updater/protocol_test.go`

- [ ] **Step 1: 修改 protocol.go**

将 `protocol.go` 中 `Updater` 接口的 `Update` 和 `Process` 方法的 `trajectories []any` 参数类型替换为 `[]*trajectory.Trajectory`。

添加 import: `"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"`

删除两处 `⤵️ 9.77` 注释。

- [ ] **Step 2: 修改 protocol_test.go**

将 `mockUpdater` 的 `Update` 和 `Process` 方法签名中 `trajectories []any` 替换为 `trajectories []*trajectory.Trajectory`。

添加 import: `"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"`

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill 2>/dev/null; export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/updater/... -v -count=1
```

Expected: 编译失败（single_dim/multi_dim 还未更新签名），这是预期的

- [ ] **Step 4: Commit**

```bash
git add internal/evolving/updater/protocol.go internal/evolving/updater/protocol_test.go
git commit -m "refactor(evolving): 回填 Updater protocol trajectories 类型 (9.77 ⤴️)"
```

---

### Task 8: 回填 SingleDimUpdater — opt any → optimizer.BaseOptimizer

**Files:**
- Modify: `internal/evolving/updater/single_dim/single_dim.go`
- Modify: `internal/evolving/updater/single_dim/single_dim_test.go`

- [ ] **Step 1: 重写 single_dim.go**

核心修改：
1. `opt any` → `opt optimizer.BaseOptimizer`
2. `NewSingleDimUpdater(optimizer any)` → `NewSingleDimUpdater(opt optimizer.BaseOptimizer)`
3. 删除所有 `type xxx interface{}` 类型断言（binder/requirer/trajectoryAdder/backwarder/stepper）
4. 删除所有 `⤵️ 9.72e 回填后消除` 的警告日志
5. 直接调用 `u.opt.Bind(...)`、`u.opt.RequiresForwardData()`、`u.opt.AddTrajectory(...)`、`u.opt.Backward(...)`、`u.opt.Step()`
6. `trajectories []any` → `[]*trajectory.Trajectory`（对齐 protocol 变化）
7. 添加 import `"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer"` 和 `"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"`

- [ ] **Step 2: 重写 single_dim_test.go**

核心修改：
1. `mockOptimizer` 实现 `optimizer.BaseOptimizer` 接口（全部 11 个方法）
2. `AddTrajectory(traj any)` → `AddTrajectory(traj *trajectory.Trajectory)`
3. 测试中的 `[]any{traj1, traj2}` → `[]*trajectory.Trajectory{traj1, traj2}`
4. 删除 `TestSingleDimUpdater_NilOptimizer`（BaseOptimizer 接口不支持 nil）

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill 2>/dev/null; export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/updater/single_dim/... -v -count=1
```

Expected: 全部 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/evolving/updater/single_dim/
git commit -m "refactor(evolving): 回填 SingleDimUpdater opt any→BaseOptimizer (9.72e ⤴️)"
```

---

### Task 9: 回填 MultiDimUpdater — map[string]any → map[string]optimizer.BaseOptimizer

**Files:**
- Modify: `internal/evolving/updater/multi_dim/multi_dim.go`
- Modify: `internal/evolving/updater/multi_dim/multi_dim_test.go`

- [ ] **Step 1: 重写 multi_dim.go**

核心修改：
1. `domainOptimizers map[string]any` → `map[string]optimizer.BaseOptimizer`
2. `WithDomainOptimizers(optimizers map[string]any)` → `map[string]optimizer.BaseOptimizer`
3. `DomainOptimizers() map[string]any` → `map[string]optimizer.BaseOptimizer`
4. 删除 `RequiresForwardData()` 中的类型断言，直接调用 `opt.RequiresForwardData()`
5. `trajectories []any` → `[]*trajectory.Trajectory`（对齐 protocol 变化）
6. 删除 `⤵️ 9.72e` 注释和警告日志
7. 添加 import

- [ ] **Step 2: 重写 multi_dim_test.go**

核心修改：
1. `mockDomainOptimizer` 实现 `optimizer.BaseOptimizer` 接口（全部 11 个方法）
2. `WithDomainOptimizers(map[string]any{...})` → `map[string]optimizer.BaseOptimizer{...}`
3. `DomainOptimizers()` 返回值类型同步修改

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill 2>/dev/null; export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/updater/multi_dim/... -v -count=1
```

Expected: 全部 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/evolving/updater/multi_dim/
git commit -m "refactor(evolving): 回填 MultiDimOptimizer map[string]any→BaseOptimizer (9.72e ⤴️)"
```

---

### Task 10: 全量测试 + 最终验证

**Files:** 无新增/修改

- [ ] **Step 1: 运行 evolving 全量测试**

```bash
cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' | xargs -r kill 2>/dev/null; export GOPROXY=https://goproxy.cn,direct && go test ./internal/evolving/... -v -count=1
```

Expected: 全部 PASS

- [ ] **Step 2: 检查覆盖率**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/evolving/trajectory/... ./internal/evolving/optimizer/...
```

Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 验证无残留 ⤵️ 标记**

```bash
grep -rn '⤵️ 9\.72e\|⤵️ 9\.77' internal/evolving/updater/
```

Expected: 无输出（所有 ⤵️ 标记已回填消除）

- [ ] **Step 4: Final commit if any remaining changes**

```bash
git add -A && git commit -m "chore(evolving): 9.77+9.72e 实现完成，全量测试通过"
```
