# 14.2 枚举层（devtools agent_builder utils/enums）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `internal/agentcore/devtools/agentbuilder/utils/` 同包续建 enums.go：4 个 `type X string` 枚举（26 常量）+ Parse×4 + All×4，并用 enums_test.go 收口 12 个 Python unit 用例（1:1 映射）+ 4.5 个 system 回填用例 + 增强用例。

**Architecture:** 方案 A（已批准 spec docs/superpowers/specs/2026-10-26-devtools-enums-14.2-design.md）：开放字符串类型 + 显式 Parse 收口（对应 Python str-Enum 的 from-string 构造 ValueError 语义）；All 函数对应 Python Enum 可迭代/len 语义。AgentTypeLiteral 无 Go 对应物（纯静态类型别名）。

**Tech Stack:** Go 标准库（fmt/errors/slices 无需第三方）；测试用标准 testing 包 + t.Run 子用例（包内既有风格，无 testify）。

**Python 参考（实施前必读）:**
- 源码：`/home/opensource/agent-core/openjiuwen/dev_tools/agent_builder/utils/enums.py`（49 行）
- 单测：`/home/opensource/agent-core/tests/unit_tests/dev_tools/agent_builder/utils/test_enums.py`（5 类 12 方法）
- system：`/home/opensource/agent-core/tests/system_tests/dev_tools/agent_builder/utils/test_utils_integration.py`（TestEnumIntegration 4 方法 + test_constants_enums_consistency 枚举半边）

**包内既有惯例（已核实，直接沿用）:**
- 断言风格：`if got != want { t.Errorf("Xxx = %v, 期望 %v", got, want) }`，无 testify
- system 用例注释格式：`// TestUtils集成_中文场景 对齐 Python system test_xxx`
- unit 用例注释格式：`// TestXxx_中文场景 对齐 Python TestXxx（N 用例）`
- 文件头注释：`// N 个共享常量定义（包文档见 doc.go）。\n// 对齐 Python: openjiuwen/dev_tools/agent_builder/utils/enums.py。`
- doc.go 独立维护（2026-10-24 审查修正先例）

**验收（spec §9）:** 覆盖率 ≥ 85%（包基线 96.7% 不降）；全部用例 PASS；gofmt + 声明顺序合规；注释全中文；完成后回填 3 处（计划行/14.1 spec 勘误/记忆文件）。

---

### Task 1: enums.go——四类型 + 26 常量

**Files:**
- Create: `internal/agentcore/devtools/agentbuilder/utils/enums.go`

- [ ] **Step 1: 写 enums.go 完整声明部分**

```go
// 4 个枚举类型与 26 个常量定义（包文档见 doc.go）。
// 对齐 Python: openjiuwen/dev_tools/agent_builder/utils/enums.py。
package utils

// ──────────────────────────── 枚举 ────────────────────────────

// AgentType 构建的 Agent 类型，对齐 Python str-Enum AgentType。
//
// 消费方：14.5 executor（ParseAgentType 入参校验关口→140032）、
// 14.6 factory（map[AgentType] 注册键）。
type AgentType string

// BuildState 构建器状态机三态，对齐 Python str-Enum BuildState。
//
// 消费方：14.6 BaseAgentBuilder._state（INITIAL→PROCESSING→COMPLETED）。
type BuildState string

// ProgressStage 构建进度阶段（15 值，分 Common/LLM Agent/Workflow Agent 三组），
// 对齐 Python str-Enum ProgressStage。
//
// 消费方：14.3 progress（BuildProgress.current_stage / ProgressStep.stage）。
type ProgressStage string

// ProgressStatus 构建进度状态，对齐 Python str-Enum ProgressStatus。
//
// 消费方：14.3 progress（BuildProgress.current_status / ProgressStep.status）。
type ProgressStatus string

// ──────────────────────────── 常量 ────────────────────────────

const (
	// AgentTypeLLMAgent LLM Agent 类型
	AgentTypeLLMAgent AgentType = "llm_agent"
	// AgentTypeWorkflow Workflow Agent 类型
	AgentTypeWorkflow AgentType = "workflow"
)

const (
	// BuildStateInitial 初始态
	BuildStateInitial BuildState = "initial"
	// BuildStateProcessing 处理中
	BuildStateProcessing BuildState = "processing"
	// BuildStateCompleted 已完成
	BuildStateCompleted BuildState = "completed"
)

const (
	// ── Common 通用阶段 ──

	// ProgressStageInitializing 初始化
	ProgressStageInitializing ProgressStage = "initializing"
	// ProgressStageClarifying 需求澄清
	ProgressStageClarifying ProgressStage = "clarifying"
	// ProgressStageResourceRetrieving 资源检索
	ProgressStageResourceRetrieving ProgressStage = "resource_retrieving"
	// ProgressStageCompleted 已完成
	ProgressStageCompleted ProgressStage = "completed"
	// ProgressStageError 错误
	ProgressStageError ProgressStage = "error"
	// ProgressStageOptimizing 优化中
	ProgressStageOptimizing ProgressStage = "optimizing"

	// ── LLM Agent 专属阶段 ──

	// ProgressStageGeneratingConfig 生成配置
	ProgressStageGeneratingConfig ProgressStage = "generating_config"
	// ProgressStageTransformingDSL 转换 DSL
	ProgressStageTransformingDSL ProgressStage = "transforming_dsl"

	// ── Workflow Agent 专属阶段 ──

	// ProgressStageDetectingIntention 意图检测
	ProgressStageDetectingIntention ProgressStage = "detecting_intention"
	// ProgressStageGeneratingWorkflowDesign 生成工作流设计
	ProgressStageGeneratingWorkflowDesign ProgressStage = "generating_workflow_design"
	// ProgressStageGeneratingDL 生成 DL
	ProgressStageGeneratingDL ProgressStage = "generating_dl"
	// ProgressStageValidatingDL 校验 DL
	ProgressStageValidatingDL ProgressStage = "validating_dl"
	// ProgressStageRefiningDL 修正 DL
	ProgressStageRefiningDL ProgressStage = "refining_dl"
	// ProgressStageTransformingMermaid 转换 Mermaid 图
	ProgressStageTransformingMermaid ProgressStage = "transforming_mermaid"
	// ProgressStageTransformingWorkflowDSL 转换工作流 DSL
	ProgressStageTransformingWorkflowDSL ProgressStage = "transforming_workflow_dsl"
)

const (
	// ProgressStatusPending 待执行
	ProgressStatusPending ProgressStatus = "pending"
	// ProgressStatusRunning 执行中
	ProgressStatusRunning ProgressStatus = "running"
	// ProgressStatusSuccess 已成功
	ProgressStatusSuccess ProgressStatus = "success"
	// ProgressStatusFailed 已失败
	ProgressStatusFailed ProgressStatus = "failed"
	// ProgressStatusWarning 警告
	ProgressStatusWarning ProgressStatus = "warning"
)
```

注意：导出函数区块此时尚空（Task 2 填入），文件暂只含枚举+常量区块与分隔注释。

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/devtools/agentbuilder/utils/`
Expected: 无输出（成功）

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/devtools/agentbuilder/utils/enums.go
git commit -m "feat(devtools): 14.2 枚举层四类型+26常量对齐 Python enums.py"
```

---

### Task 2: enums.go——Parse×4 + All×4 导出函数

**Files:**
- Modify: `internal/agentcore/devtools/agentbuilder/utils/enums.go`（追加导出函数区块）

- [ ] **Step 1: 追加 8 个导出函数**

在常量区块后追加（含分隔注释）：

```go
// ──────────────────────────── 导出函数 ────────────────────────────

// ParseAgentType 解析字符串为 AgentType，非法值返回错误。
// 对应 Python AgentType("x") 的 ValueError 语义；14.5 executor 消费
// （err 链入 BuildError cause 触发 140032 AGENT_BUILDER_AGENT_TYPE_NOT_SUPPORTED）。
func ParseAgentType(s string) (AgentType, error) {
	switch AgentType(s) {
	case AgentTypeLLMAgent, AgentTypeWorkflow:
		return AgentType(s), nil
	default:
		return "", fmt.Errorf("unsupported agent type: %q, valid: [llm_agent workflow]", s)
	}
}

// ParseBuildState 解析字符串为 BuildState，非法值返回错误。
// 对应 Python BuildState("x") 的 ValueError 语义（语言特性级等价物，对称补齐）。
func ParseBuildState(s string) (BuildState, error) {
	switch BuildState(s) {
	case BuildStateInitial, BuildStateProcessing, BuildStateCompleted:
		return BuildState(s), nil
	default:
		return "", fmt.Errorf("unsupported build state: %q, valid: [initial processing completed]", s)
	}
}

// ParseProgressStage 解析字符串为 ProgressStage，非法值返回错误。
// 对应 Python ProgressStage("x") 的 ValueError 语义（语言特性级等价物，对称补齐）。
func ParseProgressStage(s string) (ProgressStage, error) {
	switch ProgressStage(s) {
	case ProgressStageInitializing, ProgressStageClarifying, ProgressStageResourceRetrieving,
		ProgressStageCompleted, ProgressStageError, ProgressStageOptimizing,
		ProgressStageGeneratingConfig, ProgressStageTransformingDSL,
		ProgressStageDetectingIntention, ProgressStageGeneratingWorkflowDesign,
		ProgressStageGeneratingDL, ProgressStageValidatingDL, ProgressStageRefiningDL,
		ProgressStageTransformingMermaid, ProgressStageTransformingWorkflowDSL:
		return ProgressStage(s), nil
	default:
		return "", fmt.Errorf("unsupported progress stage: %q, valid: 15 values", s)
	}
}

// ParseProgressStatus 解析字符串为 ProgressStatus，非法值返回错误。
// 对应 Python ProgressStatus("x") 的 ValueError 语义（语言特性级等价物，对称补齐）。
func ParseProgressStatus(s string) (ProgressStatus, error) {
	switch ProgressStatus(s) {
	case ProgressStatusPending, ProgressStatusRunning, ProgressStatusSuccess,
		ProgressStatusFailed, ProgressStatusWarning:
		return ProgressStatus(s), nil
	default:
		return "", fmt.Errorf("unsupported progress status: %q, valid: [pending running success failed warning]", s)
	}
}

// AllAgentTypes 按声明序返回全部 AgentType 值。
// 对应 Python [t for t in AgentType] 可迭代语义；14.5 executor 消费
// （supported_types 错误详情字段）。
func AllAgentTypes() []AgentType {
	return []AgentType{AgentTypeLLMAgent, AgentTypeWorkflow}
}

// AllBuildStates 按声明序返回全部 BuildState 值。
// 对应 Python len(BuildState) 计数与可迭代语义；混合回填用例消费。
func AllBuildStates() []BuildState {
	return []BuildState{BuildStateInitial, BuildStateProcessing, BuildStateCompleted}
}

// AllProgressStages 按声明序返回全部 ProgressStage 值（15 个）。
// 对应 Python 可迭代语义（对称补齐）。
func AllProgressStages() []ProgressStage {
	return []ProgressStage{
		ProgressStageInitializing, ProgressStageClarifying, ProgressStageResourceRetrieving,
		ProgressStageCompleted, ProgressStageError, ProgressStageOptimizing,
		ProgressStageGeneratingConfig, ProgressStageTransformingDSL,
		ProgressStageDetectingIntention, ProgressStageGeneratingWorkflowDesign,
		ProgressStageGeneratingDL, ProgressStageValidatingDL, ProgressStageRefiningDL,
		ProgressStageTransformingMermaid, ProgressStageTransformingWorkflowDSL,
	}
}

// AllProgressStatuses 按声明序返回全部 ProgressStatus 值。
// 对应 Python 可迭代语义（对称补齐）。
func AllProgressStatuses() []ProgressStatus {
	return []ProgressStatus{ProgressStatusPending, ProgressStatusRunning, ProgressStatusSuccess, ProgressStatusFailed, ProgressStatusWarning}
}
```

同时在文件头 import 区加入 `"fmt"`（本文件唯一 import）。

- [ ] **Step 2: 编译 + gofmt 验证**

Run: `cd /home/opensource/uap-claw-go && gofmt -l internal/agentcore/devtools/agentbuilder/utils/ && go build ./internal/agentcore/devtools/agentbuilder/utils/`
Expected: gofmt 无输出（格式合规）+ 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/devtools/agentbuilder/utils/enums.go
git commit -m "feat(devtools): 14.2 Parse×4+All×4——from-string 收口与可迭代语义等价物"
```

---

### Task 3: enums_test.go——12 个 Python unit 用例 1:1 映射

**Files:**
- Create: `internal/agentcore/devtools/agentbuilder/utils/enums_test.go`

- [ ] **Step 1: 写 5 个测试函数（值断言/is-string/from-string/invalid/Literal）**

对齐 Python test_enums.py 五类 12 方法（is-string 断言转 Go 类型转换比较，spec §4-2；Literal 用例转 AllAgentTypes 集合断言，spec §4-5）：

```go
package utils

import "testing"

// TestAgentType 对齐 Python TestAgentType（4 用例）
func TestAgentType(t *testing.T) {
	t.Run("值断言", func(t *testing.T) {
		if string(AgentTypeLLMAgent) != "llm_agent" {
			t.Errorf("AgentTypeLLMAgent = %q, 期望 llm_agent", AgentTypeLLMAgent)
		}
		if string(AgentTypeWorkflow) != "workflow" {
			t.Errorf("AgentTypeWorkflow = %q, 期望 workflow", AgentTypeWorkflow)
		}
	})
	t.Run("is_string_enum", func(t *testing.T) {
		// isinstance(X, str) → Go 类型转换即等价物
		if AgentTypeLLMAgent != AgentType("llm_agent") {
			t.Errorf("AgentTypeLLMAgent 应可与字符串互转")
		}
		if AgentTypeWorkflow != AgentType("workflow") {
			t.Errorf("AgentTypeWorkflow 应可与字符串互转")
		}
	})
	t.Run("from_string", func(t *testing.T) {
		got, err := ParseAgentType("llm_agent")
		if err != nil || got != AgentTypeLLMAgent {
			t.Errorf("ParseAgentType(llm_agent) = %v, %v", got, err)
		}
		got, err = ParseAgentType("workflow")
		if err != nil || got != AgentTypeWorkflow {
			t.Errorf("ParseAgentType(workflow) = %v, %v", got, err)
		}
	})
	t.Run("invalid_value", func(t *testing.T) {
		// pytest.raises(ValueError) → assert err != nil
		_, err := ParseAgentType("invalid_type")
		if err == nil {
			t.Errorf("ParseAgentType(invalid_type) 应返回错误")
		}
	})
}

// TestBuildState 对齐 Python TestBuildState（3 用例）
func TestBuildState(t *testing.T) {
	t.Run("值断言", func(t *testing.T) {
		cases := []struct {
			got  BuildState
			want string
		}{
			{BuildStateInitial, "initial"},
			{BuildStateProcessing, "processing"},
			{BuildStateCompleted, "completed"},
		}
		for _, c := range cases {
			if string(c.got) != c.want {
				t.Errorf("BuildState = %q, 期望 %q", c.got, c.want)
			}
		}
	})
	t.Run("is_string_enum", func(t *testing.T) {
		if BuildStateInitial != BuildState("initial") || BuildStateProcessing != BuildState("processing") || BuildStateCompleted != BuildState("completed") {
			t.Errorf("BuildState 应可与字符串互转")
		}
	})
	t.Run("from_string", func(t *testing.T) {
		for _, s := range []string{"initial", "processing", "completed"} {
			if _, err := ParseBuildState(s); err != nil {
				t.Errorf("ParseBuildState(%q) 不应报错: %v", s, err)
			}
		}
	})
}

// TestProgressStage 对齐 Python TestProgressStage（2 用例）
func TestProgressStage(t *testing.T) {
	t.Run("值断言", func(t *testing.T) {
		cases := []struct {
			got  ProgressStage
			want string
		}{
			{ProgressStageInitializing, "initializing"},
			{ProgressStageClarifying, "clarifying"},
			{ProgressStageResourceRetrieving, "resource_retrieving"},
			{ProgressStageCompleted, "completed"},
			{ProgressStageError, "error"},
			{ProgressStageOptimizing, "optimizing"},
			{ProgressStageGeneratingConfig, "generating_config"},
			{ProgressStageTransformingDSL, "transforming_dsl"},
			{ProgressStageDetectingIntention, "detecting_intention"},
			{ProgressStageGeneratingWorkflowDesign, "generating_workflow_design"},
			{ProgressStageGeneratingDL, "generating_dl"},
			{ProgressStageValidatingDL, "validating_dl"},
			{ProgressStageRefiningDL, "refining_dl"},
			{ProgressStageTransformingMermaid, "transforming_mermaid"},
			{ProgressStageTransformingWorkflowDSL, "transforming_workflow_dsl"},
		}
		for _, c := range cases {
			if string(c.got) != c.want {
				t.Errorf("ProgressStage = %q, 期望 %q", c.got, c.want)
			}
		}
	})
	t.Run("is_string_enum", func(t *testing.T) {
		if ProgressStageInitializing != ProgressStage("initializing") || ProgressStageCompleted != ProgressStage("completed") {
			t.Errorf("ProgressStage 应可与字符串互转")
		}
	})
}

// TestProgressStatus 对齐 Python TestProgressStatus（2 用例）
func TestProgressStatus(t *testing.T) {
	t.Run("值断言", func(t *testing.T) {
		cases := []struct {
			got  ProgressStatus
			want string
		}{
			{ProgressStatusPending, "pending"},
			{ProgressStatusRunning, "running"},
			{ProgressStatusSuccess, "success"},
			{ProgressStatusFailed, "failed"},
			{ProgressStatusWarning, "warning"},
		}
		for _, c := range cases {
			if string(c.got) != c.want {
				t.Errorf("ProgressStatus = %q, 期望 %q", c.got, c.want)
			}
		}
	})
	t.Run("is_string_enum", func(t *testing.T) {
		if ProgressStatusPending != ProgressStatus("pending") || ProgressStatusSuccess != ProgressStatus("success") {
			t.Errorf("ProgressStatus 应可与字符串互转")
		}
	})
}

// TestAgentTypeLiteral 对齐 Python TestAgentTypeLiteral（1 用例）
// Python AgentTypeLiteral 为纯静态类型别名（零运行时行为，spec §4-1 显式偏差：
// 无 Go 对应物）；本用例转为 AllAgentTypes 集合断言（语义等价，spec §4-5）。
func TestAgentTypeLiteral(t *testing.T) {
	got := AllAgentTypes()
	want := map[AgentType]bool{AgentTypeLLMAgent: true, AgentTypeWorkflow: true}
	if len(got) != 2 {
		t.Errorf("AllAgentTypes 长度 = %d, 期望 2", len(got))
	}
	for _, v := range got {
		if !want[v] {
			t.Errorf("AllAgentTypes 含意外值 %q", v)
		}
	}
}
```

- [ ] **Step 2: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/devtools/agentbuilder/utils/ -run 'TestAgentType|TestBuildState|TestProgressStage|TestProgressStatus' -v 2>&1 | tail -20`
Expected: 全部 PASS（5 个测试函数）

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/devtools/agentbuilder/utils/enums_test.go
git commit -m "test(devtools): 14.2 unit 12 用例 1:1 映射对齐 Python test_enums.py"
```

---

### Task 4: enums_test.go——system 回填 4.5 用例 + 增强用例

**Files:**
- Modify: `internal/agentcore/devtools/agentbuilder/utils/enums_test.go`（追加）

- [ ] **Step 1: 追加 system 回填用例（TestEnumIntegration 4 方法 + 混合半边）**

沿用包内 system 命名先例（`TestUtils集成_* 对齐 Python system test_*`）：

```go
// TestEnumIntegration_AgentType字符串转换 对齐 Python system test_agent_type_string_conversion
func TestEnumIntegration_AgentType字符串转换(t *testing.T) {
	if string(AgentTypeLLMAgent) != "llm_agent" {
		t.Errorf("AgentTypeLLMAgent = %q", AgentTypeLLMAgent)
	}
	if string(AgentTypeWorkflow) != "workflow" {
		t.Errorf("AgentTypeWorkflow = %q", AgentTypeWorkflow)
	}
}

// TestEnumIntegration_BuildState状态流转 对齐 Python system test_build_state_transitions
func TestEnumIntegration_BuildState状态流转(t *testing.T) {
	for _, s := range AllBuildStates() {
		if len(string(s)) == 0 {
			t.Errorf("BuildState %q 值为空", s)
		}
	}
}

// TestEnumIntegration_ProgressStage值 对齐 Python system test_progress_stage_values
func TestEnumIntegration_ProgressStage值(t *testing.T) {
	for _, stage := range []ProgressStage{ProgressStageInitializing, ProgressStageClarifying, ProgressStageCompleted} {
		if len(string(stage)) == 0 {
			t.Errorf("ProgressStage %q 值为空", stage)
		}
	}
}

// TestEnumIntegration_ProgressStatus值 对齐 Python system test_progress_status_values
func TestEnumIntegration_ProgressStatus值(t *testing.T) {
	for _, status := range []ProgressStatus{ProgressStatusPending, ProgressStatusRunning, ProgressStatusSuccess, ProgressStatusFailed} {
		if len(string(status)) == 0 {
			t.Errorf("ProgressStatus %q 值为空", status)
		}
	}
}

// TestConstantsEnumsConsistency_枚举半边 对齐 Python system test_constants_enums_consistency 的枚举半边
// （常量半边已随 14.1 并入 constants_test.go；本用例为 14.2 回填义务，spec §5）
func TestConstantsEnumsConsistency_枚举半边(t *testing.T) {
	if len(AllAgentTypes()) < 2 {
		t.Errorf("AgentType 计数 = %d, 应 >= 2", len(AllAgentTypes()))
	}
	if len(AllBuildStates()) < 3 {
		t.Errorf("BuildState 计数 = %d, 应 >= 3", len(AllBuildStates()))
	}
}
```

- [ ] **Step 2: 追加增强用例（Parse×3 正反例 + All×4 顺序断言）**

沿用 14.1 LoadJSONFile 增强先例（Python 无对应，标注"测试增强"）：

```go
// TestParseBuildState_正反例 测试增强（Python 无对应：语言特性级等价物的补测）
func TestParseBuildState_正反例(t *testing.T) {
	for _, s := range []string{"initial", "processing", "completed"} {
		if _, err := ParseBuildState(s); err != nil {
			t.Errorf("ParseBuildState(%q) 不应报错: %v", s, err)
		}
	}
	if _, err := ParseBuildState("invalid"); err == nil {
		t.Errorf("ParseBuildState(invalid) 应返回错误")
	}
}

// TestParseProgressStage_正反例 测试增强（Python 无对应）
func TestParseProgressStage_正反例(t *testing.T) {
	for _, s := range []string{"initializing", "clarifying", "resource_retrieving", "completed", "error", "optimizing", "generating_config", "transforming_dsl", "detecting_intention", "generating_workflow_design", "generating_dl", "validating_dl", "refining_dl", "transforming_mermaid", "transforming_workflow_dsl"} {
		if _, err := ParseProgressStage(s); err != nil {
			t.Errorf("ParseProgressStage(%q) 不应报错: %v", s, err)
		}
	}
	if _, err := ParseProgressStage("no_such_stage"); err == nil {
		t.Errorf("ParseProgressStage(no_such_stage) 应返回错误")
	}
}

// TestParseProgressStatus_正反例 测试增强（Python 无对应）
func TestParseProgressStatus_正反例(t *testing.T) {
	for _, s := range []string{"pending", "running", "success", "failed", "warning"} {
		if _, err := ParseProgressStatus(s); err != nil {
			t.Errorf("ParseProgressStatus(%q) 不应报错: %v", s, err)
		}
	}
	if _, err := ParseProgressStatus("paused"); err == nil {
		t.Errorf("ParseProgressStatus(paused) 应返回错误")
	}
}

// TestAllEnums_声明序 测试增强（Python 无对应：All 函数顺序契约）
func TestAllEnums_声明序(t *testing.T) {
	if got := AllAgentTypes(); got[0] != AgentTypeLLMAgent || got[1] != AgentTypeWorkflow {
		t.Errorf("AllAgentTypes 顺序不符: %v", got)
	}
	if got := AllBuildStates(); got[0] != BuildStateInitial || got[1] != BuildStateProcessing || got[2] != BuildStateCompleted {
		t.Errorf("AllBuildStates 顺序不符: %v", got)
	}
	if got := AllProgressStages(); len(got) != 15 || got[0] != ProgressStageInitializing || got[14] != ProgressStageTransformingWorkflowDSL {
		t.Errorf("AllProgressStages 长度/首尾不符: len=%d", len(got))
	}
	if got := AllProgressStatuses(); len(got) != 5 || got[0] != ProgressStatusPending || got[4] != ProgressStatusWarning {
		t.Errorf("AllProgressStatuses 长度/首尾不符: len=%d", len(got))
	}
}
```

- [ ] **Step 3: 运行全量包测试 + 覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/devtools/agentbuilder/utils/ -cover -v 2>&1 | tail -30`
Expected: 全部 PASS（既有 19 + 新增 14 测试函数）；覆盖率 ≥ 96%（spec §9-2）

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/devtools/agentbuilder/utils/enums_test.go
git commit -m "test(devtools): 14.2 system 回填 4.5 用例+Parse/All 增强用例"
```

---

### Task 5: doc.go 目录树更新

**Files:**
- Modify: `internal/agentcore/devtools/agentbuilder/utils/doc.go:11-18`

- [ ] **Step 1: 目录树补两条目**

将 doc.go 的文件目录段替换为（按 规则1 排序惯例：doc.go → 核心类型文件 → 辅助功能文件 → 测试文件）：

```go
// 文件目录：
//
//	utils/
//	├── doc.go              # 包文档
//	├── enums.go            # 4 枚举类型+26 常量+Parse×4+All×4（对齐 Python enums.py）
//	├── constants.go        # 20 个共享常量（含 13 个死常量，对齐 Python 全量）
//	├── utils.go            # 7 个工具函数 + 2 个预编译正则
//	├── constants_test.go   # 21.5 用例（对齐 Python test_constants.py）
//	├── enums_test.go       # 12 unit+4.5 system 回填+增强用例（对齐 Python test_enums.py + system）
//	└── utils_test.go       # 39.5+17 用例（对齐 Python test_utils.py + system 3 + 增强）
```

- [ ] **Step 2: gofmt 验证 + 编译**

Run: `cd /home/opensource/uap-claw-go && gofmt -l internal/agentcore/devtools/agentbuilder/utils/ && go build ./internal/agentcore/devtools/agentbuilder/utils/`
Expected: 均无输出

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/devtools/agentbuilder/utils/doc.go
git commit -m "docs(devtools): 14.2 doc.go 目录树补 enums 两文件条目"
```

---

### Task 6: 回填三处（计划行 + 14.1 spec 勘误 + 记忆文件）

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md:825`
- Modify: `docs/superpowers/specs/2026-10-25-devtools-utils-14.1-design.md:94`
- Modify: `/root/.codebuddy/projects/home-opensource-uap-claw-go/memory/domain-14-devtools-progress.md`
- Modify: `/root/.codebuddy/projects/home-opensource-uap-claw-go/memory/MEMORY.md`（如需更新索引行）

- [ ] **Step 1: 更新 IMPLEMENTATION_PLAN.md 14.2 行**

状态 ☐→✅，产出摘要（含勘误后计数）。替换 825 行为：

```markdown
| 14.2 | ✅ | 枚举层 | `internal/agentcore/devtools/agentbuilder/utils/enums.go`：AgentType(2)/BuildState(3)/ProgressStage(15,三注释组)/ProgressStatus(5) 四个 type X string + 26 常量 + ParseAgentType(唯一真实消费者,140032 入口,错误含合法值清单)/ParseBuildState/ParseProgressStage/ParseProgressStatus(语言特性级等价物) + AllAgentTypes(supported_types 消费)/AllBuildStates/AllProgressStages/AllProgressStatuses(可迭代语义)；AgentTypeLiteral 零 Go 对应物(显式偏差)；enums_test.go 统一收口 12 unit 1:1 + system 回填 **4**+混合半边(spec §4-4 勘误:14.1 归属拆分算式少算 1,正确 12=3+1+4+4) + Parse×3 正反例+All×4 声明序增强；包覆盖率 9x%；设计：docs/superpowers/specs/2026-10-26-devtools-enums-14.2-design.md | `openjiuwen/dev_tools/agent_builder/utils/enums.py` |
```

（实际覆盖率数字待 Task 4 Step 3 实测后填入，不留占位符。）

- [ ] **Step 2: 14.1 spec §5 锚点行勘误补丁**

将 2026-10-25-devtools-utils-14.1-design.md 第 94 行的
`3 个枚举用例归 14.2；4 个 progress 用例归 14.3` 中"3 个枚举用例"改为
`4 个枚举用例（勘误 2026-10-26：原少算 1，正确算式 12=3+1+4+4）`。

- [ ] **Step 3: 更新记忆文件 domain-14-devtools-progress.md**

在"后续步骤"段更新：14.2 完成（日期+提交号+覆盖率），下一节 14.3（已裁决维持原序）；勘误记录：system 枚举回填用例 3→4（14.1 spec 算式修正）。

- [ ] **Step 4: Commit（仓库内两文件）**

```bash
git add IMPLEMENTATION_PLAN.md docs/superpowers/specs/2026-10-25-devtools-utils-14.1-design.md
git commit -m "docs(plan): 14.2 状态回填 ✅ + 14.1 spec 枚举用例计数勘误 3→4"
```

---

### Task 7: 全量验证收口

**Files:** 无新文件（验证-only）

- [ ] **Step 1: 包全量测试 + 覆盖率确认**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/devtools/agentbuilder/utils/ -cover`
Expected: ok + coverage ≥ 96%（基线 96.7%，enums.go 近 100% 代码，包级不降）

- [ ] **Step 2: 单进程互斥确认（规则 5）**

Run: `pgrep -f 'go (build|test)'`（如有残留先 kill）

- [ ] **Step 3: 远端 CI 确认（规则 6）**

```bash
git push
gh run list --limit 3
gh run watch <run-id>
```
Expected: Build/Lint/Unit Test/Integration Test 四 job 全绿。

说明：14.1 先例（纯函数包，无 tests/integration 变更）CI 风险极低；失败则按记忆 review-2026-10-25-ci-race-fixes 的 race 复现法排查（count=5 整包）。

---

## 自检记录（writing-plans Self-Review）

1. **Spec 覆盖**：§2 文件结构→Task 1/3/5；§3.1 常量→Task 1；§3.2 Parse→Task 2；§3.3 All→Task 2；§3.4 Literal 偏差→Task 3 Step 1（TestAgentTypeLiteral 注释+断言）；§4 五偏差→Task 3（4-2/4-3/4-5）+Task 4 Step 1 注释（4-4）；§5 测试表 8 行→Task 3/4 逐行落位；§6 零日志→无需任务（已注记）；§8 回填 4 项→Task 6 三处+14.3 锚点不变；§9 验收→Task 7。无缺口。
2. **占位符扫描**：Task 6 Step 1 的"9x%"是唯一待实测数字，已注明"实测后填入不留占位符"——执行者从 Task 4 Step 3 输出取数。其余零 TBD/TODO。
3. **类型一致性**：ParseAgentType/AllAgentTypes 等 8 函数名在 Task 2 定义、Task 3/4 消费，签名一致；26 常量名 Task 1 定义、Task 2 switch 分支与 Task 3/4 断言逐一同名。
