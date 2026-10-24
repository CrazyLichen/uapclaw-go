# devtools agentbuilder utils（14.1）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 `internal/agentcore/devtools/agentbuilder/utils/` 包（7 函数 + 20 常量 + 61 子用例），一比一对齐 Python `openjiuwen/dev_tools/agent_builder/utils/`，并把领域十四章节写入 IMPLEMENTATION_PLAN.md。

**Architecture:** 纯函数工具包（无 agentcore 内部依赖），是 devtools 工具面（agent_builder NL→DSL 生成器）的最底层地基。包路径按 Python 目录树一比一映射（结构一）。错误码 188002/153003 已预埋于 internal/common/exception 直接引用。

**Tech Stack:** Go 标准库（encoding/json、regexp、os、maps、errors）+ internal/common/{exception,logger}。

**Spec:** `docs/superpowers/specs/2026-10-25-devtools-utils-14.1-design.md`

**Python 参考（对照源，实施时逐函数比对）:**
- `/home/opensource/agent-core/openjiuwen/dev_tools/agent_builder/utils/utils.py`（253 行，7 函数）
- `/home/opensource/agent-core/openjiuwen/dev_tools/agent_builder/utils/constants.py`（62 行，20 常量）
- Python 测试：`tests/unit_tests/dev_tools/agent_builder/utils/test_utils.py`（36 用例）+ `test_constants.py`（21 用例）+ `tests/system_tests/dev_tools/agent_builder/utils/test_utils_integration.py`（12 用例，3+0.5 个归本节）

---

## Task 1: 领域十四章节写入 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `/home/opensource/uap-claw-go/IMPLEMENTATION_PLAN.md`（文末领域十三之后、12.13 行之前插入）

- [ ] **Step 1: 在领域十三验证点行（L813）之后插入领域十四整章**

```markdown
---

## 领域十四：devtools 开发者工具面

> 与运行时会话链路（Gateway/E2A/AgentServer/DeepAgent）平行的开发者工具面，进程内直接调用，不经过会话链路。agent_builder 为核心子模块（NL→Agent DSL 生成器）。

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| **14.x 基础层** | — | | | |
| 14.1 | 🔄 | devtools 包骨架 + 共享 utils | `internal/agentcore/devtools/agentbuilder/utils/`：7 函数（ExtractJSONFromText/FormatDialogHistory/SafeJSONLoads/MergeDictLists/LoadJSONFile/DeepMergeDict/ValidateSessionID，含 2 死函数）+ 20 常量（含 13 死常量）+ 61 子用例；错误码 188002/153003 消费 | `openjiuwen/dev_tools/agent_builder/utils/` |
| 14.2 | ☐ | 枚举层 | AgentType/BuildState/ProgressStage/ProgressStatus；⤴️ system 用例 3 个归本节（test_utils_integration.py 枚举部分+混合用例枚举半边） | `openjiuwen/dev_tools/agent_builder/utils/enums.py` |
| 14.3 | ☐ | 进度上报 | ProgressStep/BuildProgress/ProgressReporter/ProgressManager 观察者模式（512 行，Go 需加锁）；⤴️ system 用例 4 个归本节 | `openjiuwen/dev_tools/agent_builder/utils/progress.py` |
| **14.x agent_builder** | — | | | |
| 14.4 | ☐ | resource 层 | PluginProcessor + plugins.json 内嵌 + ResourceRetriever LLM 检索 | `openjiuwen/dev_tools/agent_builder/resource/` |
| 14.5 | ☐ | executor 层 | HistoryManager + createCoreModel + AgentBuilderExecutor | `openjiuwen/dev_tools/agent_builder/executor/` |
| 14.6 | ☐ | builders 基座 | BaseAgentBuilder 三态模板方法 + Factory 注册机制 | `openjiuwen/dev_tools/agent_builder/builders/base.py + factory.py` |
| 14.7 | ☐ | llm_agent 链 | prompts 中英提示词 + Clarifier + Generator + Transformer + IntentionDetector + template | `openjiuwen/dev_tools/agent_builder/builders/llm_agent/` |
| 14.8 | ☐ | workflow 链 | builder + dl_generator + dl_reflector + dl_assets + cycle_checker + dl_transformer + workflow_designer（~4000 行最大块） | `openjiuwen/dev_tools/agent_builder/builders/workflow/` |
| 14.9 | ☐ | AgentBuilder 门面 | build_agent/get_progress/map_state_to_status | `openjiuwen/dev_tools/agent_builder/main.py` |
| 14.10 | ☐ | 测试对齐 | 36 system + 54 unit Python 测试转 Go，闭环 A-4 缺口 | `tests/*_tests/dev_tools/` |
| **14.x 其余模块（处置待裁）** | — | | | |
| 14.11 | ⤵️ | prompt_builder | 处置待裁（依赖就绪可正常实现） | `openjiuwen/dev_tools/prompt_builder/` |
| 14.12 | ⤵️ | skill_creator | 处置待裁（依赖 9.34 sys_operation LOCAL + 技能包 Python 脚本承载方案待设计） | `openjiuwen/dev_tools/skill_creator/` |
| 14.13 | ⤵️ | skill_evaluator | 处置待裁（同 14.12 依赖） | `openjiuwen/dev_tools/skill_evaluator/` |
| 14.14 | ⤵️ | tune 旧版调参 | 处置待裁（与 agent_evolving 9.70-9.72 功能重叠，倾向跳过） | `openjiuwen/dev_tools/tune/` |

**验证点**：devtools agent_builder 工具面可用：NL 多轮对话 → 澄清 → DSL 产出
```

注意：插入位置在 L813（`**验证点**：✅ 外部文档可被解析...`）与 L814（`| 12.13 | ☐ | 自动更新器...`）之间。

- [ ] **Step 2: 验证插入结果**

Run: `grep -n "领域十四" /home/opensource/uap-claw-go/IMPLEMENTATION_PLAN.md`
Expected: 1 行命中（章节标题）

- [ ] **Step 3: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(plan): 领域十四 devtools 章节骨架写入，14.1 状态 🔄"
```

---

## Task 2: constants.go + constants_test.go

**Files:**
- Create: `/home/opensource/uap-claw-go/internal/agentcore/devtools/agentbuilder/utils/constants.go`
- Create: `/home/opensource/uap-claw-go/internal/agentcore/devtools/agentbuilder/utils/constants_test.go`

- [ ] **Step 1: 写 constants.go（20 常量全量）**

```go
// Package utils 提供 devtools agent_builder 共享工具函数与常量。
//
// 本包是 devtools 工具面（agent_builder NL→DSL 生成器）的最底层地基：
// 所有 LLM 调用（Clarifier/Generator/DLGenerator/ResourceRetriever）的输出
// 都经过本包的"剥围栏→安全解析→格式化上下文"管线。
//
// 文件目录：
//
//	utils/
//	├── doc.go              # 包文档
//	├── constants.go        # 20 个共享常量（含 13 个死常量，对齐 Python 全量）
//	├── utils.go            # 7 个工具函数 + 2 个预编译正则
//	├── constants_test.go   # 21.5 用例（对齐 Python test_constants.py）
//	└── utils_test.go       # 39.5 用例（对齐 Python test_utils.py + system 3 + 增强 4）
//
// 对应 Python 代码：openjiuwen/dev_tools/agent_builder/utils/
package utils

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ── Workflow 相关常量（14.8 workflow 链消费）──

	// WorkflowRequestContent 工作流请求话术
	WorkflowRequestContent = "Please provide your desired workflow description so I can generate " +
		"the corresponding flowchart for you. If unclear, you can reply 'unclear' " +
		"and I will plan the process for you."

	// WorkflowDesignResponseContent 工作流设计响应前缀
	WorkflowDesignResponseContent = "Workflow design content:\n"

	// GenerateDLFromDesignContent 从设计生成 DL 话术
	GenerateDLFromDesignContent = "Please generate the corresponding Process Definition Language (DL) " +
		"description based on the following workflow design content:\n"

	// ModifyDLContent 修正 DL 话术
	ModifyDLContent = "Please correct the Process Definition Language (DL) based on the following error message:\n"

	// ── 默认配置（14.5/14.8 消费）──

	// DefaultMaxHistorySize 历史消息默认上限
	DefaultMaxHistorySize = 50

	// DefaultMaxRetries 默认重试次数
	DefaultMaxRetries = 3

	// DefaultTimeout 默认超时秒数（Python 侧死常量，全量对齐保留）
	DefaultTimeout = 30

	// ── 资源类型（死常量，全量对齐保留）──

	// ResourceTypePlugin 插件资源类型
	ResourceTypePlugin = "plugin"

	// ResourceTypeKnowledge 知识库资源类型
	ResourceTypeKnowledge = "knowledge"

	// ResourceTypeWorkflow 工作流资源类型
	ResourceTypeWorkflow = "workflow"

	// ── 正则模式（本包 utils.go 消费）──

	// JSONExtractPattern JSON 围栏提取正则（对齐 Python JSON_EXTRACT_PATTERN）
	JSONExtractPattern = "```(?:json)?\\s*(.*?)\\s*```"

	// ── API 相关（死常量，全量对齐保留）──

	// APIVersion API 版本号
	APIVersion = "v1"

	// APIBasePath API 基础路径（编译期拼接，等价 Python f-string）
	APIBasePath = "/api/" + APIVersion

	// ── 进度相关（死常量，全量对齐保留；14.3 消费与否届时确认）──

	// ProgressUpdateInterval 进度更新间隔秒
	ProgressUpdateInterval = 0.1

	// ProgressHeartbeatInterval 进度心跳间隔秒
	ProgressHeartbeatInterval = 30.0

	// ── 长度限制（死常量，全量对齐保留）──

	// MaxQueryLength 查询最大长度
	MaxQueryLength = 5000

	// MinQueryLength 查询最小长度
	MinQueryLength = 1

	// MaxSessionIDLength 会话 ID 最大长度
	MaxSessionIDLength = 255

	// MaxHistorySize 历史最大条数
	MaxHistorySize = 1000

	// MinHistorySize 历史最小条数
	MinHistorySize = 1
)
```

注意：doc.go 的包注释合并写入 constants.go 头部（Go 惯例：包注释可在任一文件，选 constants.go 承载；后续不再单独建 doc.go，spec §2 的 5 文件修正为 4 文件）。

- [ ] **Step 2: 写 constants_test.go（21.5 用例）**

```go
package utils

import (
	"regexp"
	"strings"
	"testing"
)

// TestConstants_Workflow话术 对齐 Python TestWorkflowConstants（4 用例）
func TestConstants_Workflow话术(t *testing.T) {
	t.Run("workflow_request_content", func(t *testing.T) {
		if !strings.Contains(strings.ToLower(WorkflowRequestContent), "workflow") {
			t.Errorf("WorkflowRequestContent 应包含 workflow: %q", WorkflowRequestContent)
		}
	})
	t.Run("workflow_design_response_content", func(t *testing.T) {
		if !strings.Contains(WorkflowDesignResponseContent, "Workflow design content") {
			t.Errorf("WorkflowDesignResponseContent 应包含前缀")
		}
	})
	t.Run("generate_dl_from_design_content", func(t *testing.T) {
		if !strings.Contains(GenerateDLFromDesignContent, "Process Definition Language") {
			t.Errorf("GenerateDLFromDesignContent 应包含 Process Definition Language")
		}
	})
	t.Run("modify_dl_content", func(t *testing.T) {
		if !strings.Contains(strings.ToLower(ModifyDLContent), "correct") {
			t.Errorf("ModifyDLContent 应包含 correct")
		}
	})
}

// TestConstants_默认配置 对齐 Python TestDefaultConfiguration（3 用例）
func TestConstants_默认配置(t *testing.T) {
	if DefaultMaxHistorySize != 50 {
		t.Errorf("DefaultMaxHistorySize = %d, 期望 50", DefaultMaxHistorySize)
	}
	if DefaultMaxRetries != 3 {
		t.Errorf("DefaultMaxRetries = %d, 期望 3", DefaultMaxRetries)
	}
	if DefaultTimeout != 30 {
		t.Errorf("DefaultTimeout = %d, 期望 30", DefaultTimeout)
	}
}

// TestConstants_资源类型 对齐 Python TestResourceTypes（3 用例）
func TestConstants_资源类型(t *testing.T) {
	if ResourceTypePlugin != "plugin" {
		t.Errorf("ResourceTypePlugin = %q, 期望 plugin", ResourceTypePlugin)
	}
	if ResourceTypeKnowledge != "knowledge" {
		t.Errorf("ResourceTypeKnowledge = %q, 期望 knowledge", ResourceTypeKnowledge)
	}
	if ResourceTypeWorkflow != "workflow" {
		t.Errorf("ResourceTypeWorkflow = %q, 期望 workflow", ResourceTypeWorkflow)
	}
}

// TestConstants_正则模式 对齐 Python TestRegexPatterns（1 用例）
func TestConstants_正则模式(t *testing.T) {
	re := regexp.MustCompile("(?s)" + JSONExtractPattern)
	cases := []struct {
		text     string
		expected string
	}{
		{"```json\n{\"key\": \"value\"}\n```", "{\"key\": \"value\"}"},
		{"```\n{\"key\": \"value\"}\n```", "{\"key\": \"value\"}"},
		{"```json\n[1, 2, 3]\n```", "[1, 2, 3]"},
	}
	for _, c := range cases {
		m := re.FindStringSubmatch(c.text)
		if len(m) < 2 || strings.TrimSpace(m[1]) != strings.TrimSpace(c.expected) {
			t.Errorf("正则提取 %q 失败, got %v", c.text, m)
		}
	}
}

// TestConstants_API 对齐 Python TestApiConstants（2 用例）
func TestConstants_API(t *testing.T) {
	if APIVersion != "v1" {
		t.Errorf("APIVersion = %q, 期望 v1", APIVersion)
	}
	if APIBasePath != "/api/v1" {
		t.Errorf("APIBasePath = %q, 期望 /api/v1", APIBasePath)
	}
}

// TestConstants_进度 对齐 Python TestProgressConstants（2 用例，untyped const 直接值比较）
func TestConstants_进度(t *testing.T) {
	if ProgressUpdateInterval != 0.1 {
		t.Errorf("ProgressUpdateInterval = %v, 期望 0.1", ProgressUpdateInterval)
	}
	if ProgressHeartbeatInterval != 30.0 {
		t.Errorf("ProgressHeartbeatInterval = %v, 期望 30.0", ProgressHeartbeatInterval)
	}
}

// TestConstants_长度限制 对齐 Python TestLimitConstants（6 用例：5 值 + 1 关系）
func TestConstants_长度限制(t *testing.T) {
	if MaxQueryLength != 5000 {
		t.Errorf("MaxQueryLength = %d, 期望 5000", MaxQueryLength)
	}
	if MinQueryLength != 1 {
		t.Errorf("MinQueryLength = %d, 期望 1", MinQueryLength)
	}
	if MaxSessionIDLength != 255 {
		t.Errorf("MaxSessionIDLength = %d, 期望 255", MaxSessionIDLength)
	}
	if MaxHistorySize != 1000 {
		t.Errorf("MaxHistorySize = %d, 期望 1000", MaxHistorySize)
	}
	if MinHistorySize != 1 {
		t.Errorf("MinHistorySize = %d, 期望 1", MinHistorySize)
	}
	if MinQueryLength > MaxQueryLength || MinHistorySize > MaxHistorySize {
		t.Errorf("长度约束关系非法: MinQuery=%d MaxQuery=%d MinHistory=%d MaxHistory=%d",
			MinQueryLength, MaxQueryLength, MinHistorySize, MaxHistorySize)
	}
}

// TestConstants_常量枚举一致性 对齐 Python system test_constants_enums_consistency 的常量半边
// （枚举半边归 14.2 enums_test.go，届时回填标注）
func TestConstants_常量枚举一致性(t *testing.T) {
	if DefaultMaxHistorySize <= 0 {
		t.Errorf("DefaultMaxHistorySize 应 > 0")
	}
	if MaxQueryLength <= MinQueryLength {
		t.Errorf("MaxQueryLength 应 > MinQueryLength")
	}
}
```

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/devtools/agentbuilder/utils/ -run "TestConstants" -v 2>&1 | tail -25`
Expected: 8 个 Test 函数全 PASS（约 21.5 断言点）

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/devtools/agentbuilder/utils/
git commit -m "feat(devtools): 14.1 constants.go 20 常量全量对齐 + 21.5 用例"
```

---

## Task 3: utils.go — 4 个无依赖函数（Extract/Format/Validate/Merge）

**Files:**
- Create: `/home/opensource/uap-claw-go/internal/agentcore/devtools/agentbuilder/utils/utils.go`
- Create: `/home/opensource/uap-claw-go/internal/agentcore/devtools/agentbuilder/utils/utils_test.go`

- [ ] **Step 1: 写 utils_test.go 第一批失败测试（4 函数 26 用例）**

```go
package utils

import (
	"testing"
)

// TestExtractJSONFromText 对齐 Python TestExtractJsonFromText（7 用例）
func TestExtractJSONFromText(t *testing.T) {
	t.Run("json代码块提取", func(t *testing.T) {
		if got := ExtractJSONFromText("```json\n{\"key\": \"value\"}\n```"); got != "{\"key\": \"value\"}" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("裸代码块提取", func(t *testing.T) {
		if got := ExtractJSONFromText("```\n{\"key\": \"value\"}\n```"); got != "{\"key\": \"value\"}" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("无代码块返回原文", func(t *testing.T) {
		if got := ExtractJSONFromText("{\"key\": \"value\"}"); got != "{\"key\": \"value\"}" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("空文本", func(t *testing.T) {
		if got := ExtractJSONFromText(""); got != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("None转空串等价", func(t *testing.T) {
		// Python: extract_json_from_text(None) 返回 None；Go 字符串零值即 ""
		if got := ExtractJSONFromText(""); got != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("json数组", func(t *testing.T) {
		if got := ExtractJSONFromText("```json\n[1, 2, 3]\n```"); got != "[1, 2, 3]" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("多行json", func(t *testing.T) {
		got := ExtractJSONFromText("```json\n{\"key1\": \"value1\",\n\"key2\": \"value2\"}\n```")
		if !contains(got, "key1") || !contains(got, "key2") {
			t.Errorf("got %q", got)
		}
	})
}

// TestFormatDialogHistory 对齐 Python TestFormatDialogHistory（5 用例）
func TestFormatDialogHistory(t *testing.T) {
	t.Run("单条消息", func(t *testing.T) {
		got := FormatDialogHistory([]map[string]string{{"role": "user", "content": "Hello"}})
		if got != "user: Hello" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("多条消息", func(t *testing.T) {
		history := []map[string]string{
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi! How can I help you?"},
		}
		got := FormatDialogHistory(history)
		expected := "user: Hello\nassistant: Hi! How can I help you?"
		if got != expected {
			t.Errorf("got %q, expected %q", got, expected)
		}
	})
	t.Run("空历史", func(t *testing.T) {
		if got := FormatDialogHistory(nil); got != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("自定义分隔符", func(t *testing.T) {
		history := []map[string]string{
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi"},
		}
		got := FormatDialogHistory(history, " | ")
		if got != "user: Hello | assistant: Hi" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("缺失键", func(t *testing.T) {
		history := []map[string]string{
			{"role": "user"},               // 无 content
			{"content": "Missing role"},    // 无 role
		}
		got := FormatDialogHistory(history)
		if !contains(got, "user: ") || !contains(got, "unknown: Missing role") {
			t.Errorf("got %q", got)
		}
	})
}

// TestValidateSessionID 对齐 Python TestValidateSessionId（8 用例）
func TestValidateSessionID(t *testing.T) {
	t.Run("字母数字有效", func(t *testing.T) {
		if !ValidateSessionID("abc123XYZ") {
			t.Error("应为有效")
		}
	})
	t.Run("下划线有效", func(t *testing.T) {
		if !ValidateSessionID("user_001_session") {
			t.Error("应为有效")
		}
	})
	t.Run("连字符有效", func(t *testing.T) {
		if !ValidateSessionID("session-12345") {
			t.Error("应为有效")
		}
	})
	t.Run("组合有效", func(t *testing.T) {
		if !ValidateSessionID("test-session-2024") {
			t.Error("应为有效")
		}
	})
	t.Run("特殊字符无效", func(t *testing.T) {
		if ValidateSessionID("session@123") {
			t.Error("应为无效")
		}
	})
	t.Run("空格无效", func(t *testing.T) {
		if ValidateSessionID("session 123") {
			t.Error("应为无效")
		}
	})
	t.Run("空串无效", func(t *testing.T) {
		if ValidateSessionID("") {
			t.Error("应为无效")
		}
	})
	t.Run("None转空串等价", func(t *testing.T) {
		// Python: validate_session_id(None) is False；Go 字符串零值即 ""
		if ValidateSessionID("") {
			t.Error("应为无效")
		}
	})
}

// TestMergeDictLists 对齐 Python TestMergeDictLists（6 用例）
func TestMergeDictLists(t *testing.T) {
	t.Run("唯一键合并", func(t *testing.T) {
		existing := []map[string]any{{"id": "1", "name": "A"}}
		newItems := []map[string]any{{"id": "2", "name": "B"}}
		result := MergeDictLists(existing, newItems, "id")
		if len(result) != 2 || result[0]["id"] != "1" || result[1]["id"] != "2" {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("重复键保留首个", func(t *testing.T) {
		existing := []map[string]any{{"id": "1", "name": "A"}}
		newItems := []map[string]any{{"id": "1", "name": "B"}}
		result := MergeDictLists(existing, newItems, "id")
		if len(result) != 1 || result[0]["name"] != "A" {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("空新增", func(t *testing.T) {
		existing := []map[string]any{{"id": "1", "name": "A"}}
		result := MergeDictLists(existing, nil, "id")
		if len(result) != 1 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("空存量", func(t *testing.T) {
		newItems := []map[string]any{{"id": "1", "name": "A"}}
		result := MergeDictLists([]map[string]any{}, newItems, "id")
		if len(result) != 1 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("双空", func(t *testing.T) {
		result := MergeDictLists(nil, nil, "id")
		if len(result) != 0 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("缺失唯一键跳过", func(t *testing.T) {
		existing := []map[string]any{{"id": "1", "name": "A"}}
		newItems := []map[string]any{{"name": "B"}} // 无 id 键
		result := MergeDictLists(existing, newItems, "id")
		if len(result) != 1 {
			t.Errorf("result=%v", result)
		}
	})
}

// contains 子串包含断言辅助（仅测试文件使用）
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

注意：contains/containsStr 是为避免引入 strings 包到测试而写的辅助——**实际实施时直接用 `strings.Contains` 替代**（标准库可用，此处展示仅为自包含；最终代码用 strings.Contains，删除这两个辅助函数）。

- [ ] **Step 2: 运行验证编译失败**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/devtools/agentbuilder/utils/ 2>&1 | head -5`
Expected: FAIL（undefined: ExtractJSONFromText 等未定义符号）

- [ ] **Step 3: 写 utils.go 第一批（4 函数 + 2 正则 var）**

```go
package utils

import (
	"regexp"
	"strings"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// jsonExtractRegexp JSON 围栏提取预编译正则（对齐 Python JSON_EXTRACT_PATTERN + re.findall 首个匹配）
	jsonExtractRegexp = regexp.MustCompile("(?s)" + JSONExtractPattern)

	// sessionIDRegexp 会话 ID 校验预编译正则（对齐 Python ^[a-zA-Z0-9_-]+$）
	sessionIDRegexp = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractJSONFromText 从文本中提取 JSON 内容。
// 对齐 Python: extract_json_from_text。
// 支持从 Markdown 代码块（```json ... ```）中提取；无围栏时返回原文；空文本原样返回。
func ExtractJSONFromText(text string) string {
	if text == "" {
		return text
	}
	if m := jsonExtractRegexp.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return text
}

// FormatDialogHistory 将对话历史格式化为字符串。
// 对齐 Python: format_dialog_history。
// 每条消息格式化为 "role: content"，缺 role 键记 "unknown"，缺 content 键记空串。
// separator 变长参数默认 "\n"。
func FormatDialogHistory(history []map[string]string, separator ...string) string {
	if len(history) == 0 {
		return ""
	}
	sep := "\n"
	if len(separator) > 0 {
		sep = separator[0]
	}
	parts := make([]string, 0, len(history))
	for _, msg := range history {
		role := "unknown"
		if v, ok := msg["role"]; ok {
			role = v
		}
		content := ""
		if v, ok := msg["content"]; ok {
			content = v
		}
		parts = append(parts, role+": "+content)
	}
	return strings.Join(parts, sep)
}

// ValidateSessionID 校验会话 ID 格式。
// 对齐 Python: validate_session_id。
// 仅允许字母、数字、下划线和连字符；空串返回 false。
func ValidateSessionID(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	return sessionIDRegexp.MatchString(sessionID)
}

// MergeDictLists 合并字典列表并按唯一键去重。
// 对齐 Python: merge_dict_lists。
// 就地追加语义（Python 原地 append 并返回同一 list，调用方 base.py 依赖此行为）；
// key 值缺失或为 falsy（空串/0/false）的条目跳过；uniqueKey 变长参数默认 "resource_id"。
func MergeDictLists(existing, newItems []map[string]any, uniqueKey ...string) []map[string]any {
	key := "resource_id"
	if len(uniqueKey) > 0 {
		key = uniqueKey[0]
	}
	if len(newItems) == 0 {
		return existing
	}

	existingKeys := make(map[any]bool)
	for _, item := range existing {
		if v, ok := item[key]; ok && isTruthy(v) {
			existingKeys[v] = true
		}
	}
	for _, item := range newItems {
		keyValue, ok := item[key]
		if !ok || !isTruthy(keyValue) {
			continue
		}
		if !existingKeys[keyValue] {
			existing = append(existing, item)
			existingKeys[keyValue] = true
		}
	}
	return existing
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isTruthy 判断值是否为 Python truthy 语义（空串/0/false 视为 falsy）。
// 对齐 Python: item[unique_key] 的隐式布尔转换。
func isTruthy(v any) bool {
	switch val := v.(type) {
	case string:
		return val != ""
	case bool:
		return val
	case int:
		return val != 0
	case int64:
		return val != 0
	case float64:
		return val != 0
	case nil:
		return false
	default:
		return true
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/devtools/agentbuilder/utils/ -run "TestExtract|TestFormat|TestValidate|TestMerge" -v 2>&1 | tail -10`
Expected: 4 个 Test 函数全 PASS（26 子用例）

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/devtools/agentbuilder/utils/
git commit -m "feat(devtools): 14.1 utils.go 第一批 4 函数（Extract/Format/Validate/Merge）+ 26 用例"
```

---

## Task 4: utils.go — SafeJSONLoads + LoadJSONFile（带错误码）

**Files:**
- Modify: `/home/opensource/uap-claw-go/internal/agentcore/devtools/agentbuilder/utils/utils.go`
- Modify: `/home/opensource/uap-claw-go/internal/agentcore/devtools/agentbuilder/utils/utils_test.go`

- [ ] **Step 1: 追加失败测试（5+4 用例）**

在 utils_test.go 追加（imports 增加 `"errors"`、`"os"`、`"path/filepath"`）：

```go
// TestSafeJSONLoads 对齐 Python TestSafeJsonLoads（5 用例）
func TestSafeJSONLoads(t *testing.T) {
	t.Run("有效JSON", func(t *testing.T) {
		result, err := SafeJSONLoads(`{"key": "value"}`)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		m, ok := result.(map[string]any)
		if !ok || m["key"] != "value" {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("无效JSON返回默认值", func(t *testing.T) {
		result, err := SafeJSONLoads("invalid json", map[string]any{})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		m, ok := result.(map[string]any)
		if !ok || len(m) != 0 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("空串返回默认值", func(t *testing.T) {
		result, err := SafeJSONLoads("", []any{})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if arr, ok := result.([]any); !ok || len(arr) != 0 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("None转空串等价返回默认值", func(t *testing.T) {
		// Python: safe_json_loads(None, default=[]) → []
		result, err := SafeJSONLoads("", []any{})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if _, ok := result.([]any); !ok {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("JSON数组", func(t *testing.T) {
		result, err := SafeJSONLoads("[1, 2, 3]")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		arr, ok := result.([]any)
		if !ok || len(arr) != 3 || arr[0] != float64(1) {
			t.Errorf("result=%v（JSON number 在 Go 反序列化为 float64）", result)
		}
	})
	t.Run("未传默认值时解析失败返回错误", func(t *testing.T) {
		// Python: default=None 时抛 COMMON_JSON_INPUT_PROCESS_ERROR(188002)
		_, err := SafeJSONLoads("invalid json")
		if err == nil {
			t.Fatal("期望返回错误")
		}
	})

// TestLoadJSONFile Go 增强用例（Python 无直接单测，retriever 层 mock 覆盖；本包补 4 分支）
func TestLoadJSONFile(t *testing.T) {
	t.Run("文件不存在", func(t *testing.T) {
		_, err := LoadJSONFile(filepath.Join(t.TempDir(), "nonexist.json"))
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("期望 fs.ErrNotExist, got %v", err)
		}
	})
	t.Run("空文件返回空map", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.json")
		if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := LoadJSONFile(path)
		if err != nil || len(result) != 0 {
			t.Errorf("result=%v err=%v", result, err)
		}
	})
	t.Run("空白文件返回空map", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "blank.json")
		if err := os.WriteFile(path, []byte("   \n  "), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := LoadJSONFile(path)
		if err != nil || len(result) != 0 {
			t.Errorf("result=%v err=%v", result, err)
		}
	})
	t.Run("顶层非dict报错", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "array.json")
		if err := os.WriteFile(path, []byte("[1, 2, 3]"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadJSONFile(path)
		if err == nil {
			t.Error("期望顶层非 dict 报错")
		}
	})
	t.Run("正常dict返回内容", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ok.json")
		if err := os.WriteFile(path, []byte(`{"key": "value"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := LoadJSONFile(path)
		if err != nil || result["key"] != "value" {
			t.Errorf("result=%v err=%v", result, err)
		}
	})
}
```

注意 imports 需含 `"io/fs"`。TestSafeJSONLoads 的最后一个子用例后需补 `}` 闭合（上面代码块中省略了闭合括号，实施时注意 TestSafeJSONLoads 函数整体闭合）。

- [ ] **Step 2: 运行验证失败**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/devtools/agentbuilder/utils/ 2>&1 | head -5`
Expected: FAIL（undefined: SafeJSONLoads/LoadJSONFile）

- [ ] **Step 3: utils.go 追加两个函数**

在 utils.go 导出函数区追加（imports 增加 `"encoding/json"`、`"errors"`、`"fmt"`、`"io/fs"`、`"os"`、exception、logger）：

```go
// SafeJSONLoads 安全解析 JSON 字符串。
// 对齐 Python: utils.safe_json_loads + JsonUtils.safe_json_loads 三层语义：
//  1. 空文本短路返回 default
//  2. def 未传（nil）→ 解析失败返回 BuildError(StatusCommonJsonInputProcessError)
//  3. def 已传 → 解析失败记录 Error 日志后返回 def
//
// 变长参数承载"是否传了 default"的信息（nil 无法区分"没传"与"传了 nil"）。
func SafeJSONLoads(text string, def ...any) (any, error) {
	var defaultValue any
	hasDefault := len(def) > 0
	if hasDefault {
		defaultValue = def[0]
	}

	if text == "" {
		return defaultValue, nil
	}

	var result any
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		if !hasDefault {
			return nil, exception.BuildError(
				exception.StatusCommonJsonInputProcessError,
				exception.WithMsg("JSON decode error"),
			)
		}
		logger.Error(logComponent).
			Str("error", err.Error()).
			Msg("JSON decode error")
		return defaultValue, nil
	}
	return result, nil
}

// LoadJSONFile 加载 JSON 文件。
// 对齐 Python: load_json_file。
// 不存在 → 包裹 fs.ErrNotExist 的错误；空文件 → 空 map + Warn 日志；
// 顶层非 dict → 错误；解析异常 → BuildError(StatusContextMessageInvalid) + cause 链。
func LoadJSONFile(filePath string) (map[string]any, error) {
	if _, err := os.Stat(filePath); err != nil {
		logger.Error(logComponent).
			Str("file_path", filePath).
			Msg("File not found")
		return nil, fmt.Errorf("File not found: %s: %w", filePath, fs.ErrNotExist)
	}

	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, wrapJSONFileError(filePath, err)
	}

	if len(strings.TrimSpace(string(raw))) == 0 {
		logger.Warn(logComponent).
			Str("file_path", filePath).
			Msg("JSON file is empty")
		return map[string]any{}, nil
	}

	var data any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, wrapJSONFileError(filePath, err)
	}

	m, ok := data.(map[string]any)
	if !ok {
		return nil, wrapJSONFileError(filePath, fmt.Errorf("JSON top level must be object/dict, got: %T", data))
	}
	return m, nil
}
```

utils.go 顶部补包级常量与 imports（完整文件头）：

```go
package utils

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// logComponent 日志组件常量
const logComponent = logger.ComponentAgentCore
```

非导出函数区追加：

```go
// wrapJSONFileError 包装 JSON 文件读取/解析错误。
// 对齐 Python: load_json_file 的 ValidationError(CONTEXT_MESSAGE_INVALID) + cause 链。
func wrapJSONFileError(filePath string, cause error) error {
	logger.Error(logComponent).
		Str("file_path", filePath).
		Str("error", cause.Error()).
		Msg("JSON parse failed")
	return exception.BuildError(
		exception.StatusContextMessageInvalid,
		exception.WithMsg(fmt.Sprintf("JSON parse error: %s", cause)),
		exception.WithDetails(map[string]any{"file_path": filePath, "error": cause.Error()}),
		exception.WithCause(cause),
	)
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/devtools/agentbuilder/utils/ -run "TestSafeJSONLoads|TestLoadJSONFile" -v 2>&1 | tail -8`
Expected: 全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/devtools/agentbuilder/utils/
git commit -m "feat(devtools): 14.1 SafeJSONLoads 三层语义 + LoadJSONFile 四分支 + 11 用例"
```

---

## Task 5: utils.go — DeepMergeDict（死函数实现）+ system 集成用例

**Files:**
- Modify: `/home/opensource/uap-claw-go/internal/agentcore/devtools/agentbuilder/utils/utils.go`
- Modify: `/home/opensource/uap-claw-go/internal/agentcore/devtools/agentbuilder/utils/utils_test.go`

- [ ] **Step 1: 追加 DeepMergeDict 失败测试（5 用例，含嵌套层增强断言）**

```go
// TestDeepMergeDict 对齐 Python TestDeepMergeDict（5 用例 + 嵌套层增强）
func TestDeepMergeDict(t *testing.T) {
	t.Run("简单合并", func(t *testing.T) {
		result := DeepMergeDict(map[string]any{"a": 1}, map[string]any{"b": 2})
		if result["a"] != 1 || result["b"] != 2 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("嵌套合并", func(t *testing.T) {
		base := map[string]any{"a": map[string]any{"b": 1, "c": 2}}
		update := map[string]any{"a": map[string]any{"b": 3, "d": 4}}
		result := DeepMergeDict(base, update)
		inner := result["a"].(map[string]any)
		if inner["b"] != 3 || inner["c"] != 2 || inner["d"] != 4 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("非dict值覆盖", func(t *testing.T) {
		result := DeepMergeDict(map[string]any{"a": 1}, map[string]any{"a": 2})
		if result["a"] != 2 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("不改原map", func(t *testing.T) {
		base := map[string]any{"a": 1}
		result := DeepMergeDict(base, map[string]any{"b": 2})
		if _, exists := base["b"]; exists {
			t.Error("base 不应被修改")
		}
		if _, exists := result["b"]; !exists {
			t.Error("result 应含 b")
		}
	})
	t.Run("嵌套层不改原map_增强断言", func(t *testing.T) {
		// Python 测试只断言顶层；Go 增强为嵌套层也验证（测试增强非偏差）
		base := map[string]any{"a": map[string]any{"x": 1}}
		result := DeepMergeDict(base, map[string]any{"a": map[string]any{"y": 2}})
		inner := result["a"].(map[string]any)
		inner["z"] = 3 // 修改结果嵌套层
		baseInner := base["a"].(map[string]any)
		if _, exists := baseInner["z"]; exists {
			t.Error("base 嵌套层不应被修改（递归必须新建 map）")
		}
		if _, exists := baseInner["y"]; exists {
			t.Error("base 嵌套层不应被合并写入")
		}
	})
	t.Run("双空", func(t *testing.T) {
		result := DeepMergeDict(map[string]any{}, map[string]any{})
		if len(result) != 0 {
			t.Errorf("result=%v", result)
		}
	})
}

// TestUtils集成_JSON提取解析工作流 对齐 Python system test_extract_and_parse_json_workflow
func TestUtils集成_JSON提取解析工作流(t *testing.T) {
	llmResponse := `
        Here is the generated DSL:
        ` + "```json" + `
        {
            "name": "Test Agent",
            "type": "llm_agent",
            "config": {
                "temperature": 0.7,
                "max_tokens": 1000
            }
        }
        ` + "```" + `
        Please review and confirm.
        `
	jsonStr := ExtractJSONFromText(llmResponse)
	if jsonStr == "" {
		t.Fatal("提取结果为空")
	}
	parsed, err := SafeJSONLoads(jsonStr)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	m := parsed.(map[string]any)
	if m["name"] != "Test Agent" {
		t.Errorf("name=%v", m["name"])
	}
	if m["config"].(map[string]any)["temperature"] != 0.7 {
		t.Errorf("temperature=%v", m["config"])
	}
}

// TestUtils集成_对话历史格式化 对齐 Python system test_dialog_history_formatting_integration
func TestUtils集成_对话历史格式化(t *testing.T) {
	history := []map[string]string{
		{"role": "user", "content": "创建一个助手"},
		{"role": "assistant", "content": "好的，请告诉我助手名称"},
		{"role": "user", "content": "叫小助手"},
	}
	formatted := FormatDialogHistory(history)
	for _, expect := range []string{"user: 创建一个助手", "assistant: 好的，请告诉我助手名称", "user: 叫小助手"} {
		if !strings.Contains(formatted, expect) {
			t.Errorf("formatted=%q 缺少 %q", formatted, expect)
		}
	}
}

// TestUtils集成_真实模式SessionID校验 对齐 Python system test_session_id_validation_with_real_patterns
func TestUtils集成_真实模式SessionID校验(t *testing.T) {
	validIDs := []string{"session-12345", "user_001_session", "abc123XYZ", "test-session-2024"}
	for _, id := range validIDs {
		if !ValidateSessionID(id) {
			t.Errorf("%q 应为有效", id)
		}
	}
}
```

- [ ] **Step 2: 运行验证失败**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/devtools/agentbuilder/utils/ 2>&1 | head -3`
Expected: FAIL（undefined: DeepMergeDict）

- [ ] **Step 3: utils.go 追加 DeepMergeDict**

```go
// DeepMergeDict 深度合并字典。
// 对齐 Python: deep_merge_dict（Python 侧死代码，全量对齐实现）。
// 递归合并嵌套 dict，update 覆盖 base 同名键；不修改原 dict（浅拷贝起点 + 递归新建）。
func DeepMergeDict(base, update map[string]any) map[string]any {
	result := maps.Clone(base)
	for key, value := range update {
		if existing, ok := result[key]; ok {
			existingMap, existingIsMap := existing.(map[string]any)
			valueMap, valueIsMap := value.(map[string]any)
			if existingIsMap && valueIsMap {
				result[key] = DeepMergeDict(existingMap, valueMap)
				continue
			}
		}
		result[key] = value
	}
	return result
}
```

imports 增加 `"maps"`。

- [ ] **Step 4: 运行全包测试 + 覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/devtools/agentbuilder/utils/ -cover -v 2>&1 | tail -30`
Expected: 全部 PASS；覆盖率 ≥ 85%（预估 95%+）

- [ ] **Step 5: gofmt 校验**

Run: `cd /home/opensource/uap-claw-go && gofmt -l internal/agentcore/devtools/agentbuilder/utils/`
Expected: 空输出（无格式问题）

- [ ] **Step 6: Commit**

```bash
git add internal/agentcore/devtools/agentbuilder/utils/
git commit -m "feat(devtools): 14.1 DeepMergeDict 死函数实现 + 3 个 system 集成用例，包覆盖率达标"
```

---

## Task 6: 回填闭环（计划状态 + 记忆更新）

**Files:**
- Modify: `/home/opensource/uap-claw-go/IMPLEMENTATION_PLAN.md`
- Modify: `/root/.codebuddy/projects/home-opensource-uap-claw-go/memory/agentcore-test-alignment-gaps.md`

- [ ] **Step 1: IMPLEMENTATION_PLAN.md 14.1 行状态 🔄 → ✅ 并补产出摘要**

将 14.1 行改为：

```markdown
| 14.1 | ✅ | devtools 包骨架 + 共享 utils | `internal/agentcore/devtools/agentbuilder/utils/`：7 函数（ExtractJSONFromText/FormatDialogHistory/SafeJSONLoads三层语义/MergeDictLists原地语义+isTruthy/LoadJSONFile四分支/DeepMergeDict死函数实现/ValidateSessionID）+ 20 常量（13 死常量全量对齐）+ 61 子用例（36 unit+3 system+4 LoadJSONFile增强+21.5 constants；浮点断言 float64）+ 错误码 188002/153003 消费 + 正则 Go/Python 双端 9 边界用例交叉验证；设计：docs/superpowers/specs/2026-10-25-devtools-utils-14.1-design.md | `openjiuwen/dev_tools/agent_builder/utils/` |
```

- [ ] **Step 2: 更新记忆 agentcore-test-alignment-gaps.md 的 A-4 条目**

将 A-4 条目改为：

```markdown
4. dev_tools/agent_builder —— **领域十四已立项（2026-10-25，14.1 utils 已完成）**：14.1 基础层落地 internal/agentcore/devtools/agentbuilder/utils/；14.2-14.10 agent_builder 主体按计划推进；14.11-14.14（prompt_builder/skill_creator/skill_evaluator/tune）处置待裁（tune 与 agent_evolving 9.70-9.72 重叠倾向跳过；skill 两模块依赖 9.34 sys_operation LOCAL + Python 脚本承载方案）
```

并在该文件 "How to apply" 段更新：B 类完结、A 类 ModelBackupRail 已完结、A-4 已立项，余下候选为 WorkflowAgent 8.14-8.24 / RL 9.74-76 / 领域十四 14.2 起主体。

- [ ] **Step 3: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs(plan): 14.1 状态 ✅ 产出摘要回填"
```

（记忆文件在仓库外，无需 git add）

---

## 自审记录（写计划时已完成）

1. **Spec 覆盖**：spec §2 文件结构→Task 2-5（doc.go 并入 constants.go 包注释，spec §2 的 5 文件在计划中修正为 4 文件，与 spec 自审时声明一致）；§3 七函数签名→Task 3（4 函数）+ Task 4（2 函数）+ Task 5（1 函数）；§4 常量→Task 2；§5 测试设计→Task 2-5 各自 Step 1；§6 五偏差→各任务测试代码内注释标注；§7 日志→Task 4 Step 3（wrapJSONFileError + SafeJSONLoads Error 日志）；§8 回填清单→Task 1+Task 6；§9 验收→Task 5 Step 4-5。无缺口。
2. **占位符扫描**：无 TBD/TODO/"类似 Task N"；每步含完整代码与命令。Task 3 Step 1 中 contains 辅助函数标注了"实际实施时用 strings.Contains 替代"——这是显式指令非占位符。
3. **类型一致性**：SafeJSONLoads `(any, error)`、MergeDictLists 变长 uniqueKey、LoadJSONFile `(map[string]any, error)`、isTruthy 六分支——Task 3/4/5 与测试代码签名一致；exception.BuildError 选项（WithMsg/WithDetails/WithCause）与 errors.go L106-131 实际签名核对一致；StatusCommonJsonInputProcessError/StatusContextMessageInvalid 符号名与 codes_framework.go L215/codes_context.go L23 核对一致。
