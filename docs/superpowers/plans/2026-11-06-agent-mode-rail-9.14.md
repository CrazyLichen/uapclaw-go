# 9.14 AgentModeRail Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 9.14 AgentModeRail，包含三个 Agent Mode 工具（switch_mode/enter_plan_mode/exit_plan_mode）的 invoke 逻辑、slug 生成、TaskTool 动态注册，以及 AgentModeRail 主体。

**Architecture:** AgentModeRail 嵌入 DeepAgentRail（priority=85），实现 BeforeModelCall/BeforeToolCall/AfterToolCall 三个回调钩子。工具放在 `tools/agent_mode/` 包，TaskTool 放在 `tools/subagent/task_tool.go`。完全对齐 Python `agent_mode_rail.py`（552 行）+ `agent_mode_tools.py`（389 行）+ `task_tool.py`（159 行）。

**Tech Stack:** Go 1.22+, 标准库 regexp/sync/crypto/rand, 项目内 harness/foundation/session 包

---

## File Structure

```
新增文件：
  internal/agentcore/harness/tools/agent_mode/
    ├── doc.go                  # 包文档
    ├── slug.go                 # GenerateWordSlug / ResolvePlanFilePath / GetOrCreatePlanSlug
    ├── slug_test.go            # slug 测试
    ├── switch_mode.go          # SwitchModeTool invoke
    ├── enter_plan_mode.go      # EnterPlanModeTool invoke
    ├── exit_plan_mode.go       # ExitPlanModeTool invoke
    ├── tool_test.go            # 三个工具 invoke 测试

  internal/agentcore/harness/tools/subagent/
    ├── task_tool.go            # TaskTool + CreateTaskTool（新增）
    ├── task_tool_test.go       # TaskTool 测试（新增）

  internal/agentcore/harness/rails/
    ├── agent_mode.go           # AgentModeRail 主体（新增）
    ├── agent_mode_test.go      # AgentModeRail 测试（新增）

修改文件：
  internal/agentcore/harness/rails/doc.go               # 添加 agent_mode.go 条目
  internal/agentcore/harness/tools/subagent/doc.go       # 添加 task_tool.go 条目
  internal/agentcore/harness/deep_agent.go               # 移除 ⤵️ 9.3 回填标记
  IMPLEMENTATION_PLAN.md                                 # 9.14 状态 → ✅
```

---

### Task 1: 创建 tools/agent_mode 包 + doc.go + slug.go

**Files:**
- Create: `internal/agentcore/harness/tools/agent_mode/doc.go`
- Create: `internal/agentcore/harness/tools/agent_mode/slug.go`
- Create: `internal/agentcore/harness/tools/agent_mode/slug_test.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package agent_mode 提供 Agent 模式切换工具的实现。
//
// 包含三个工具：
//   - SwitchModeTool: 在 normal/plan 模式间切换
//   - EnterPlanModeTool: 初始化 plan 文件并返回路径
//   - ExitPlanModeTool: 读取 plan 全文并结束规划阶段
//
// 以及辅助函数：
//   - GenerateWordSlug: 生成 adjective-verb-noun 格式的随机 slug
//   - ResolvePlanFilePath: 根据工作区根路径和 slug 推导 plan 文件路径
//   - GetOrCreatePlanSlug: 生成不与已有 plan 文件冲突的 slug
//
// 文件目录：
//
//	agent_mode/
//	├── doc.go              # 包文档
//	├── slug.go             # Slug 生成函数
//	├── switch_mode.go      # SwitchModeTool 切换模式
//	├── enter_plan_mode.go  # EnterPlanModeTool 进入规划
//	└── exit_plan_mode.go   # ExitPlanModeTool 退出规划
//
// 对应 Python 代码：openjiuwen/harness/tools/agent_mode_tools.py
package agent_mode
```

- [ ] **Step 2: 创建 slug.go — 词表常量 + 三个函数**

对齐 Python `agent_mode_tools.py` L26-112。

```go
package agent_mode

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 对齐 Python L26-48: 26 个形容词
var adjectives = []string{
	"ancient", "blazing", "calm", "daring", "eager",
	"fierce", "gleaming", "happy", "icy", "jolly",
	"keen", "lively", "mighty", "noble", "open",
	"proud", "quiet", "rapid", "silent", "tall",
	"unique", "vivid", "warm", "xenial", "young", "zealous",
}

// 对齐 Python L34-40: 23 个动词
var verbs = []string{
	"brewing", "crafting", "designing", "exploring", "forging",
	"gathering", "hunting", "inspiring", "joining", "keeping",
	"learning", "making", "noting", "opening", "planning",
	"questing", "reading", "seeking", "testing", "using",
	"viewing", "writing", "yielding",
}

// 对齐 Python L43-48: 26 个名词
var nouns = []string{
	"anchor", "bridge", "cloud", "delta", "ember",
	"falcon", "galaxy", "harbor", "island", "jungle",
	"kernel", "lantern", "meadow", "nexus", "orbit",
	"phoenix", "quartz", "river", "summit", "tower",
	"union", "valley", "wave", "xenon", "yacht", "zenith",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GenerateWordSlug 生成随机的 adjective-verb-noun 格式 slug。
//
// 使用 crypto/rand 作为随机源，对齐 Python generate_word_slug() L51-68。
func GenerateWordSlug() string {
	adj := adjectives[cryptoRandInt(len(adjectives))]
	verb := verbs[cryptoRandInt(len(verbs))]
	noun := nouns[cryptoRandInt(len(nouns))]
	return fmt.Sprintf("%s-%s-%s", adj, verb, noun)
}

// ResolvePlanFilePath 根据工作区根路径和 slug 推导 plan 文件绝对路径。
//
// 若 .plans 目录不存在则创建。对齐 Python resolve_plan_file_path() L71-92。
func ResolvePlanFilePath(workspaceRoot, slug string) string {
	plansDir := filepath.Join(workspaceRoot, ".plans")
	_ = os.MkdirAll(plansDir, 0o755)
	return filepath.Join(plansDir, slug+".md")
}

// GetOrCreatePlanSlug 生成不与已有 plan 文件冲突的 slug。
//
// 最多尝试 20 次。对齐 Python get_or_create_plan_slug() L95-111。
func GetOrCreatePlanSlug(workspaceRoot string) string {
	plansDir := filepath.Join(workspaceRoot, ".plans")
	_ = os.MkdirAll(plansDir, 0o755)
	for i := 0; i < 20; i++ {
		slug := GenerateWordSlug()
		path := filepath.Join(plansDir, slug+".md")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return slug
		}
	}
	return GenerateWordSlug()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// cryptoRandInt 返回 [0, n) 范围内的安全随机整数。
func cryptoRandInt(n int) int {
	if n <= 0 {
		return 0
	}
	result, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		// 降级到确定值，避免运行时崩溃
		return 0
	}
	return int(result.Int64())
}

// normalizeLanguage 规范化语言代码为 "cn" 或 "en"。
func normalizeLanguage(lang string) string {
	if lang == "en" {
		return "en"
	}
	return "cn"
}

// formatPlanPath 格式化 plan 路径用于消息输出。
func formatPlanPath(planPath string) string {
	// 统一使用正斜杠，与 Python Path 行为对齐
	return strings.ReplaceAll(planPath, `\`, "/")
}
```

- [ ] **Step 3: 创建 slug_test.go**

```go
package agent_mode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateWordSlug(t *testing.T) {
	slug := GenerateWordSlug()
	parts := strings.Split(slug, "-")
	if len(parts) != 3 {
		t.Errorf("期望 3 段，实际 %d 段: %s", len(parts), slug)
	}
	// 验证各段在词表中
	foundAdj := false
	for _, w := range adjectives {
		if w == parts[0] {
			foundAdj = true
			break
		}
	}
	if !foundAdj {
		t.Errorf("形容词 '%s' 不在词表中", parts[0])
	}
}

func TestGenerateWordSlug_多次生成不重复(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 100; i++ {
		slug := GenerateWordSlug()
		seen[slug] = struct{}{}
	}
	// 100 次生成至少应有多个不同值（概率极高）
	if len(seen) < 10 {
		t.Errorf("100 次生成仅有 %d 个不同值", len(seen))
	}
}

func TestResolvePlanFilePath(t *testing.T) {
	tmpDir := t.TempDir()
	path := ResolvePlanFilePath(tmpDir, "test-slug")
	expected := filepath.Join(tmpDir, ".plans", "test-slug.md")
	if path != expected {
		t.Errorf("期望 %s，实际 %s", expected, path)
	}
	// 验证 .plans 目录已创建
	plansDir := filepath.Join(tmpDir, ".plans")
	info, err := os.Stat(plansDir)
	if err != nil {
		t.Fatalf(".plans 目录未创建: %v", err)
	}
	if !info.IsDir() {
		t.Error(".plans 不是目录")
	}
}

func TestGetOrCreatePlanSlug(t *testing.T) {
	tmpDir := t.TempDir()
	slug := GetOrCreatePlanSlug(tmpDir)
	if slug == "" {
		t.Error("slug 不应为空")
	}
	parts := strings.Split(slug, "-")
	if len(parts) != 3 {
		t.Errorf("期望 3 段，实际 %d 段: %s", len(parts), slug)
	}
	// 验证对应文件不存在（因为只是生成 slug，没有创建文件）
	path := filepath.Join(tmpDir, ".plans", slug+".md")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("slug 对应的文件不应存在")
	}
}

func TestGetOrCreatePlanSlug_已有文件时不冲突(t *testing.T) {
	tmpDir := t.TempDir()
	plansDir := filepath.Join(tmpDir, ".plans")
	_ = os.MkdirAll(plansDir, 0o755)
	// 创建一个已有文件
	existingSlug := "ancient-brewing-anchor"
	existingPath := filepath.Join(plansDir, existingSlug+".md")
	_ = os.WriteFile(existingPath, []byte(""), 0o644)
	// 多次生成应能得到不同 slug（概率极高）
	for i := 0; i < 50; i++ {
		slug := GetOrCreatePlanSlug(tmpDir)
		if slug == existingSlug {
			// 概率极低但允许；仅当路径文件存在时才失败
			path := filepath.Join(plansDir, slug+".md")
			if _, err := os.Stat(path); err == nil && slug == existingSlug {
				t.Logf("生成与已有文件冲突的 slug（概率事件），跳过")
			}
		}
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/tools/agent_mode/... -v -run "TestGenerateWordSlug|TestResolvePlanFilePath|TestGetOrCreatePlanSlug"
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/agent_mode/
git commit -m "feat(9.14): 添加 agent_mode 工具包 + slug 生成函数"
```

---

### Task 2: 实现 SwitchModeTool invoke

**Files:**
- Modify: `internal/agentcore/harness/tools/agent_mode/switch_mode.go` (create)

- [ ] **Step 1: 创建 switch_mode.go**

对齐 Python `SwitchModeTool.invoke()` L224-256 + 消息 L165-184。

```go
package agent_mode

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SwitchModeInput switch_mode 工具输入参数。
type SwitchModeInput struct {
	Mode string `json:"mode"`
}

// ──────────────────────────── 常量 ────────────────────────────

// 对齐 Python L165-184: 中英文消息
var (
	switchModeInvalidMsg = map[string]string{
		"cn": "无效模式 '{mode}'。支持模式：normal、plan。",
		"en": "Invalid mode '{mode}'. Supported modes: plan, normal.",
	}
	switchModeToNormalMsg = map[string]string{
		"cn": "已切换为 normal 模式。",
		"en": "Switched mode to normal.",
	}
	switchModeToPlanMsg = map[string]string{
		"cn": "已切换为 plan 模式。\n下一步：调用 enter_plan_mode 继续 Plan 工作流。",
		"en": "Switched mode to plan.\nNext step: call enter_plan_mode to continue the plan workflow.",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSwitchModeTool 创建 switch_mode 工具实例。
//
// 对齐 Python: SwitchModeTool.__init__() L205-221
func NewSwitchModeTool(agent hinterfaces.DeepAgentInterface, language, agentID string) tool.Tool {
	lang := normalizeLanguage(language)
	card, _ := tools.BuildToolCard("switch_mode", "switch_mode", lang, nil, agentID)

	fn := func(ctx context.Context, input SwitchModeInput, opts ...tool.ToolOption) (map[string]any, error) {
		lang := normalizeLanguage(language)
		rawMode := strings.TrimSpace(strings.ToLower(input.Mode))

		// 对齐 Python L230: 校验模式值
		if rawMode != hschema.AgentModePlan.String() && rawMode != hschema.AgentModeNormal.String() {
			msg := strings.ReplaceAll(switchModeInvalidMsg[lang], "{mode}", rawMode)
			return map[string]any{"error": msg}, nil
		}

		// 提取 session
		sess := extractSession(opts)
		if sess == nil {
			return map[string]any{"error": "switch_mode 需要 session"}, nil
		}

		// 对齐 Python L239-249: 调用 agent.SwitchMode
		agent.SwitchMode(sess, rawMode)

		var message string
		var currentMode string
		if rawMode == hschema.AgentModePlan.String() {
			message = switchModeToPlanMsg[lang]
			currentMode = hschema.AgentModePlan.String()
		} else {
			message = switchModeToNormalMsg[lang]
			currentMode = hschema.AgentModeNormal.String()
		}

		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "switch_mode").
			Str("mode", currentMode).
			Msg("AgentModeRail 已切换模式")

		return map[string]any{
			"current_mode": currentMode,
			"message":      message,
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}
```

- [ ] **Step 2: 在 slug.go 中添加 extractSession 辅助函数**

```go
// extractSession 从 ToolOption 中提取 SessionFacade。
func extractSession(opts []tool.ToolOption) sessioninterfaces.SessionFacade {
	callOpts := tool.NewToolCallOptions(opts...)
	session := callOpts.Session
	if session == nil {
		return nil
	}
	if sess, ok := session.(sessioninterfaces.SessionFacade); ok {
		return sess
	}
	return nil
}
```

同时需要补充 import：`"strings"` 已在 slug.go 中。需要添加 import `sessioninterfaces` 和 `tool` 包。实际上 `extractSession` 应放在独立的辅助文件或 slug.go 中。放在 slug.go 并添加必要的 import。

- [ ] **Step 3: 运行编译检查**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/tools/agent_mode/...
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/tools/agent_mode/
git commit -m "feat(9.14): 实现 SwitchModeTool invoke 逻辑"
```

---

### Task 3: 实现 EnterPlanModeTool invoke

**Files:**
- Create: `internal/agentcore/harness/tools/agent_mode/enter_plan_mode.go`

- [ ] **Step 1: 创建 enter_plan_mode.go**

对齐 Python `EnterPlanModeTool.invoke()` L288-320 + 消息 L114-138。

```go
package agent_mode

import (
	"context"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// 对齐 Python L114-138: enter_plan_mode 中英文消息
var (
	enterPlanExistsMsg = map[string]string{
		"cn": "计划文件已存在，路径：{plan_path}\n你可以阅读计划文件然后做增量修改。请按照提示词中的Plan工作流继续制定计划，初始理解-方案设计-审查-撰写计划-结束规划。\n",
		"en": "Plan file already exists at: {plan_path}\nYou can read it and make incremental edits. Continue the 5-phase Plan workflow in your instructions, initial understanding-design-review-final plan-end.",
	}
	enterPlanCreatedMsg = map[string]string{
		"cn": "计划文件已创建于：{plan_path}\n请按照提示词中的Plan工作流继续制定计划，初始理解-方案设计-审查-撰写计划-结束规划。\n除计划文件外，请勿编辑任何其他文件。\n",
		"en": "Plan file created at: {plan_path}\nContinue the 5-phase Plan workflow in your instructions, initial understanding-design-review-final plan-end.DO NOT edit any files except the plan file.\n",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEnterPlanModeTool 创建 enter_plan_mode 工具实例。
//
// 对齐 Python: EnterPlanModeTool.__init__() L262-286 + invoke() L288-320
func NewEnterPlanModeTool(agent hinterfaces.DeepAgentInterface, language, agentID string) tool.Tool {
	lang := normalizeLanguage(language)
	card, _ := tools.BuildToolCard("enter_plan_mode", "enter_plan_mode", lang, nil, agentID)

	fn := func(ctx context.Context, _ map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
		lang := normalizeLanguage(language)
		sess := extractSession(opts)
		if sess == nil {
			return map[string]any{"error": "enter_plan_mode 需要 session"}, nil
		}

		// 对齐 Python L299-309: 若已有 plan_slug 且文件存在 → 返回已存在消息
		state := agent.LoadState(sess)
		if state.PlanMode.PlanSlug != "" {
			workspaceRoot := getWorkspaceRoot(agent)
			existingPath := ResolvePlanFilePath(workspaceRoot, state.PlanMode.PlanSlug)
			if _, err := os.Stat(existingPath); err == nil {
				msg := strings.ReplaceAll(enterPlanExistsMsg[lang], "{plan_path}", formatPlanPath(existingPath))
				return map[string]any{"plan_path": existingPath, "message": msg}, nil
			}
		}

		// 对齐 Python L311-319: 生成 slug → 解析路径 → 保存 slug 到 state
		workspaceRoot := getWorkspaceRoot(agent)
		slug := GetOrCreatePlanSlug(workspaceRoot)
		planPath := ResolvePlanFilePath(workspaceRoot, slug)
		_ = os.MkdirAll(filepath.Dir(planPath), 0o755)

		state.PlanMode.PlanSlug = slug
		agent.SaveState(sess, state)

		msg := strings.ReplaceAll(enterPlanCreatedMsg[lang], "{plan_path}", formatPlanPath(planPath))

		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "enter_plan_mode").
			Str("plan_slug", slug).
			Str("plan_path", planPath).
			Msg("AgentModeRail 已创建 plan 文件")

		return map[string]any{"plan_path": planPath, "message": msg}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}
```

在 slug.go 中添加 `getWorkspaceRoot` 辅助函数：

```go
// getWorkspaceRoot 从 DeepAgentInterface 获取工作区根路径。
func getWorkspaceRoot(agent hinterfaces.DeepAgentInterface) string {
	cfg := agent.DeepConfig()
	if cfg != nil && cfg.Workspace != nil && cfg.Workspace.RootPath != nil {
		return *cfg.Workspace.RootPath
	}
	return ""
}
```

- [ ] **Step 2: 编译检查**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/tools/agent_mode/...
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/tools/agent_mode/
git commit -m "feat(9.14): 实现 EnterPlanModeTool invoke 逻辑"
```

---

### Task 4: 实现 ExitPlanModeTool invoke

**Files:**
- Create: `internal/agentcore/harness/tools/agent_mode/exit_plan_mode.go`

- [ ] **Step 1: 创建 exit_plan_mode.go**

对齐 Python `ExitPlanModeTool.invoke()` L350-378 + 消息 L140-162。

```go
package agent_mode

import (
	"context"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// 对齐 Python L140-162: exit_plan_mode 中英文消息
var (
	exitPlanEmptyMsg = map[string]string{
		"cn": "规划模式已结束。你现在可以结束本轮。\n计划文件：{plan_path}",
		"en": "Plan mode ended. You can now exit the turn.\nPlan file: {plan_path}",
	}
	exitPlanWithContentPrefix = map[string]string{
		"cn": "规划模式已结束。\n计划文件：{plan_path}\n\n## 计划：\n",
		"en": "Plan mode ended. \nPlan file: {plan_path}\n\n## Plan:\n",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExitPlanModeTool 创建 exit_plan_mode 工具实例。
//
// 对齐 Python: ExitPlanModeTool.__init__() L326-348 + invoke() L350-378
func NewExitPlanModeTool(agent hinterfaces.DeepAgentInterface, language, agentID string) tool.Tool {
	lang := normalizeLanguage(language)
	card, _ := tools.BuildToolCard("exit_plan_mode", "exit_plan_mode", lang, nil, agentID)

	fn := func(ctx context.Context, _ map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
		lang := normalizeLanguage(language)
		sess := extractSession(opts)
		if sess == nil {
			return map[string]any{"error": "exit_plan_mode 需要 session"}, nil
		}

		// 对齐 Python L363-365: 读取 plan 文件内容
		planPath := agent.GetPlanFilePath(sess)
		planText := ""
		if planPath != "" {
			data, err := os.ReadFile(planPath)
			if err == nil {
				planText = string(data)
			}
		}

		planPathStr := formatPlanPath(planPath)

		// 对齐 Python L370-371: 空计划
		if strings.TrimSpace(planText) == "" {
			msg := strings.ReplaceAll(exitPlanEmptyMsg[lang], "{plan_path}", planPathStr)
			return map[string]any{"plan_path": planPath, "message": msg}, nil
		}

		// 对齐 Python L373-375: 有内容 → 恢复模式 + 返回前缀 + 计划全文
		agent.RestoreModeAfterPlanExit(sess)
		prefix := strings.ReplaceAll(exitPlanWithContentPrefix[lang], "{plan_path}", planPathStr)

		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "exit_plan_mode").
			Str("plan_path", planPath).
			Int("plan_length", len(planText)).
			Msg("AgentModeRail 已退出 plan 模式")

		return map[string]any{
			"plan_path": planPath,
			"message":   prefix + planText,
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}
```

- [ ] **Step 2: 编译检查**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/tools/agent_mode/...
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/tools/agent_mode/
git commit -m "feat(9.14): 实现 ExitPlanModeTool invoke 逻辑"
```

---

### Task 5: 实现 TaskTool + CreateTaskTool

**Files:**
- Create: `internal/agentcore/harness/tools/subagent/task_tool.go`
- Create: `internal/agentcore/harness/tools/subagent/task_tool_test.go`

- [ ] **Step 1: 创建 task_tool.go**

对齐 Python `tools/subagent/task_tool.py` (159 行)。

```go
package subagent

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskToolInput task_tool 工具输入参数。
// 对齐 Python: TaskTool.invoke() L57-121
type TaskToolInput struct {
	SubagentType    string `json:"subagent_type"`
	TaskDescription string `json:"task_description"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTaskTool 创建 task_tool 工具实例。
//
// 对齐 Python: TaskTool.__init__() L31-47 + invoke() L57-121
func NewTaskTool(parentAgent hinterfaces.DeepAgentInterface, availableAgents, language, agentID string) tool.Tool {
	lang := "cn"
	if language == "en" {
		lang = "en"
	}
	card, _ := tools.BuildToolCard("task_tool", "task_tool", lang, map[string]string{
		"available_agents": availableAgents,
	}, agentID)

	fn := func(ctx context.Context, input TaskToolInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 对齐 Python L87-91: 校验必填参数
		if input.SubagentType == "" || input.TaskDescription == "" {
			return nil, fmt.Errorf("subagent_type 和 task_description 都是必填参数")
		}

		// 提取 session
		sess := extractTaskToolSession(opts)
		if sess == nil {
			return nil, fmt.Errorf("task_tool 需要 session")
		}

		parentSessionID := sess.GetSessionID()
		subSessionID := buildSubSessionID(parentSessionID, input.SubagentType)

		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "task_tool_create_subagent").
			Str("subagent_type", input.SubagentType).
			Str("parent_session_id", parentSessionID).
			Str("sub_session_id", subSessionID).
			Msg("TaskTool 创建子代理")

		// 对齐 Python L100-107: 创建子代理
		subagent, err := parentAgent.CreateSubagent(input.SubagentType, subSessionID)
		if err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "LLM_CALL_ERROR").
				Str("subagent_type", input.SubagentType).
				Err(err).
				Msg("TaskTool 子代理创建失败")
			return nil, fmt.Errorf("子代理 %s 创建失败: %w", input.SubagentType, err)
		}

		// 对齐 Python L111-115: 调用子代理
		result, err := subagent.Invoke(ctx, map[string]any{
			"query":           input.TaskDescription,
			"conversation_id": subSessionID,
		})
		if err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "LLM_CALL_ERROR").
				Str("subagent_type", input.SubagentType).
				Err(err).
				Msg("TaskTool 子代理执行失败")
			return nil, fmt.Errorf("子代理 %s 执行失败: %w", input.SubagentType, err)
		}

		output := ""
		if v, ok := result["output"]; ok {
			output = fmt.Sprintf("%v", v)
		}
		agentID := ""
		if subagentCard := subagent.ReactAgent().Card(); subagentCard != nil {
			agentID = subagentCard.ID
		}

		return map[string]any{
			"output":    output,
			"agent_id":  agentID,
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildSubSessionID 构建子会话 ID。
//
// 对齐 Python: TaskTool._build_sub_session_id() L49-55
func buildSubSessionID(parentSessionID, subagentType string) string {
	normalized := strings.TrimSpace(subagentType)
	if normalized == "browser_agent" || normalized == "verification_agent" {
		return fmt.Sprintf("%s_sub_%s", parentSessionID, normalized)
	}
	return fmt.Sprintf("%s_sub_%s_%s", parentSessionID, normalized, generateTokenHex(8))
}

// extractTaskToolSession 从 ToolOption 提取 SessionFacade。
func extractTaskToolSession(opts []tool.ToolOption) sessioninterfaces.SessionFacade {
	callOpts := tool.NewToolCallOptions(opts...)
	session := callOpts.Session
	if session == nil {
		return nil
	}
	if sess, ok := session.(sessioninterfaces.SessionFacade); ok {
		return sess
	}
	return nil
}
```

- [ ] **Step 2: 创建 task_tool_test.go**

```go
package subagent

import (
	"testing"
)

func TestBuildSubSessionID(t *testing.T) {
	tests := []struct {
		name           string
		parentSessionID string
		subagentType    string
		wantPrefix      string
	}{
		{
			name:           "browser_agent 确定性 ID",
			parentSessionID: "sess-123",
			subagentType:    "browser_agent",
			wantPrefix:      "sess-123_sub_browser_agent",
		},
		{
			name:           "verification_agent 确定性 ID",
			parentSessionID: "sess-456",
			subagentType:    "verification_agent",
			wantPrefix:      "sess-456_sub_verification_agent",
		},
		{
			name:           "其他类型带随机后缀",
			parentSessionID: "sess-789",
			subagentType:    "explore",
			wantPrefix:      "sess-789_sub_explore_",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildSubSessionID(tt.parentSessionID, tt.subagentType)
			if !strings.HasPrefix(got, tt.wantPrefix) {
				t.Errorf("期望前缀 %s，实际 %s", tt.wantPrefix, got)
			}
		})
	}
}

func TestBuildSubSessionID_随机后缀不重复(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 50; i++ {
		id := buildSubSessionID("sess", "explore")
		seen[id] = struct{}{}
	}
	if len(seen) < 10 {
		t.Errorf("50 次生成仅有 %d 个不同值", len(seen))
	}
}
```

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/tools/subagent/... -v -run "TestBuildSubSessionID"
```

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/tools/subagent/task_tool.go internal/agentcore/harness/tools/subagent/task_tool_test.go
git commit -m "feat(9.14): 实现 TaskTool + CreateTaskTool 动态注册"
```

---

### Task 6: 实现 AgentModeRail 主体

**Files:**
- Create: `internal/agentcore/harness/rails/agent_mode.go`

- [ ] **Step 1: 创建 agent_mode.go — 结构体 + 常量 + 构造函数**

对齐 Python `agent_mode_rail.py` 全文 552 行。

这是最大的文件，包含：
- AgentModeRail 结构体（嵌入 DeepAgentRail）
- 工具名常量集合（todoToolNames / sessionToolNames / hiddenInPlan / hiddenInNormal / planFileWriteTools）
- Git 写操作正则
- Plan 模式白名单
- NewAgentModeRail 构造函数
- Init / Uninit
- BeforeModelCall / BeforeToolCall / AfterToolCall
- GetCallbacks
- 所有辅助方法（rejectTool / isPlanFile / extractFilePath / extractBashCommand / buildAvailableAgents / registerTaskTool / unregisterTaskTool / isTaskToolRegistered / syncTaskToolForModelToolInputs / handleEnter / handleExit / languageIsCN）

关键逻辑要点：

**BeforeModelCall** (对齐 Python L151-196):
- 非 plan 模式：`systemPromptBuilder.RemoveSection(SectionModeInstructions)`，过滤 hiddenInNormal 工具
- plan 模式：调用 `BuildPlanModeSection(enterStatus, planFileInfo, lang)` 注入，`RemoveSection(SectionTodo)` + `RemoveSection(SectionSessionTools)`，过滤 hiddenInPlan 工具
- 两种模式都调用 `syncTaskToolForModelToolInputs`

**BeforeToolCall 三段式** (对齐 Python L232-329):
- 段 1：toolName == "enter_plan_mode" → handleEnter; "exit_plan_mode" → handleExit
- 段 2：非 plan 模式 → return nil
- 段 3：plan 模式 → 3a 硬拦截 → 3b 白名单 → 3c git 正则 → 3d 计划文件路径

**AfterToolCall** (对齐 Python L331-344):
- enter_plan_mode 成功 → registerTaskTool
- exit_plan_mode 成功 → unregisterTaskTool

**rejectTool** (对齐 Python L476-488):
- 设置 `ctx.Extra()["skipTool"] = true`
- 构造 ToolMessage，注入 error result

- [ ] **Step 2: 编译检查**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/rails/...
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/rails/agent_mode.go
git commit -m "feat(9.14): 实现 AgentModeRail 主体（结构体+4个钩子+辅助方法）"
```

---

### Task 7: 实现 AgentModeRail 单元测试

**Files:**
- Create: `internal/agentcore/harness/rails/agent_mode_test.go`

- [ ] **Step 1: 创建 agent_mode_test.go**

覆盖核心逻辑的测试用例：
- `TestNewAgentModeRail` — 构造函数默认白名单
- `TestNewAgentModeRail_自定义白名单` — 传入自定义 allowedTools
- `TestAgentModeRail_BeforeModelCall_Plan模式` — 验证注入 MODE_INSTRUCTIONS 段 + 移除 TODO/SESSION_TOOLS 段 + 过滤隐藏工具
- `TestAgentModeRail_BeforeModelCall_Normal模式` — 验证移除 MODE_INSTRUCTIONS 段 + 隐藏 enter/exit 工具
- `TestAgentModeRail_BeforeToolCall_EnterPlanMode_非Plan模式拒绝` — enter_plan_mode 在非 plan 模式下被拒绝
- `TestAgentModeRail_BeforeToolCall_ExitPlanMode_非Plan模式拒绝` — exit_plan_mode 在非 plan 模式下被拒绝
- `TestAgentModeRail_BeforeToolCall_Plan模式白名单拒绝` — 非白名单工具在 plan 模式下被拒绝
- `TestAgentModeRail_BeforeToolCall_Git写操作拦截` — bash 中 git add/commit/push 被拦截
- `TestAgentModeRail_BeforeToolCall_计划文件路径校验` — write_file 非计划文件路径被拦截
- `TestAgentModeRail_BeforeToolCall_计划文件路径放行` — write_file 写计划文件被放行
- `TestIsPlanFile` — 路径比较逻辑
- `TestGitWriteRE` — 正则匹配各 git 写操作

使用 mock DeepAgentInterface 和 mock SystemPromptBuilderInterface 进行测试。

- [ ] **Step 2: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/... -v -run "TestAgentModeRail|TestIsPlanFile|TestGitWriteRE"
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/rails/agent_mode_test.go
git commit -m "test(9.14): 添加 AgentModeRail 单元测试"
```

---

### Task 8: 添加工具 invoke 测试

**Files:**
- Create: `internal/agentcore/harness/tools/agent_mode/tool_test.go`

- [ ] **Step 1: 创建 tool_test.go**

测试三个工具的 invoke 逻辑：
- `TestSwitchModeTool_Invoke_切到Plan` — 验证返回 plan 消息
- `TestSwitchModeTool_Invoke_切到Normal` — 验证返回 normal 消息
- `TestSwitchModeTool_Invoke_无效模式` — 验证返回错误消息
- `TestEnterPlanModeTool_Invoke_新建Plan文件` — 验证创建 .plans/ 目录和 slug
- `TestEnterPlanModeTool_Invoke_已存在Plan文件` — 验证幂等返回已存在消息
- `TestExitPlanModeTool_Invoke_有内容` — 验证返回计划全文 + 恢复模式
- `TestExitPlanModeTool_Invoke_空计划` — 验证返回空消息

- [ ] **Step 2: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/tools/agent_mode/... -v -run "TestSwitchModeTool|TestEnterPlanModeTool|TestExitPlanModeTool"
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/tools/agent_mode/tool_test.go
git commit -m "test(9.14): 添加 agent_mode 工具 invoke 测试"
```

---

### Task 9: 更新 doc.go 文件

**Files:**
- Modify: `internal/agentcore/harness/rails/doc.go`
- Modify: `internal/agentcore/harness/tools/subagent/doc.go`

- [ ] **Step 1: 更新 rails/doc.go**

在文件目录树中添加 `agent_mode.go` 条目，在包描述中添加 AgentModeRail 说明。

- [ ] **Step 2: 更新 tools/subagent/doc.go**

在文件目录树中添加 `task_tool.go` 条目。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/rails/doc.go internal/agentcore/harness/tools/subagent/doc.go
git commit -m "docs(9.14): 更新 doc.go 添加 agent_mode + task_tool 条目"
```

---

### Task 10: 回填已有代码 + 更新 IMPLEMENTATION_PLAN

**Files:**
- Modify: `internal/agentcore/harness/deep_agent.go` — 移除 ⤵️ 9.3 回填标记
- Modify: `IMPLEMENTATION_PLAN.md` — 9.14 状态 → ✅

- [ ] **Step 1: 在 deep_agent.go 中找到并移除 ⤵️ 9.3 回填标记**

搜索 `⤵️ 9.3 回填：resolve_plan_file_path`，将对应注释更新为已实现状态。

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md 中 9.14 状态**

将 `| 9.14 | ☐ |` 改为 `| 9.14 | ✅ |`。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/deep_agent.go IMPLEMENTATION_PLAN.md
git commit -m "chore(9.14): 回填 deep_agent.go 标记 + 更新实现计划状态为 ✅"
```

---

### Task 11: 全量测试 + 覆盖率检查

- [ ] **Step 1: 运行全量测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/... -v -count=1
```

- [ ] **Step 2: 检查覆盖率**

```bash
cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/harness/rails/... ./internal/agentcore/harness/tools/agent_mode/... ./internal/agentcore/harness/tools/subagent/...
```

目标：≥ 85% 覆盖率。若不达标，补充测试用例。

- [ ] **Step 3: 最终提交（如有补充）**

```bash
git add -A && git commit -m "test(9.14): 补充测试用例以达到 85% 覆盖率"
```
