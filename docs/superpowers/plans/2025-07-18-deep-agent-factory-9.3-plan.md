# 9.3 DeepAgent Factory 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 `CreateDeepAgent()` 工厂函数，将 DeepAgent 的复杂组装过程（Config、Tools、Rails、SubAgents、SysOperation、Workspace）封装为单一调用

**Architecture:** 对齐 Python `openjiuwen/harness/factory.py` 的 10 步流程。未实现的依赖（Rails、LocalSysOperation、BackendProtocol）用占位+回填标记保留完整逻辑骨架

**Tech Stack:** Go 1.24+, testify, 项目内部包（harness/schema, sys_operation, workspace, rail, resource_mgr 等）

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 新增 | `internal/agentcore/harness/factory.go` | CreateDeepAgent 工厂函数 + 8 个辅助函数 |
| 新增 | `internal/agentcore/harness/factory_test.go` | 工厂函数全部测试 |
| 修改 | `internal/agentcore/harness/prompts/builder.go` | ResolveLanguage 校验修复 + isSupportedLanguage |
| 修改 | `internal/agentcore/harness/prompts/builder_test.go` | ResolveLanguage 校验测试 |
| 修改 | `internal/agentcore/harness/deep_agent.go` | 回填 CreateSubagent default 分支 + 3 处占位 |
| 修改 | `internal/agentcore/harness/harness_config/builder.go` | 回填 Build 方法调用 CreateDeepAgent |
| 修改 | `internal/agentcore/harness/schema/config.go` | Backend 注释修正（2处） |
| 修改 | `internal/agentcore/harness/doc.go` | 更新文件目录 |

---

### Task 1: ResolveLanguage 校验修复

**Files:**
- Modify: `internal/agentcore/harness/prompts/builder.go:99-111`
- Test: `internal/agentcore/harness/prompts/builder_test.go`

- [ ] **Step 1: 写失败测试 — 不支持的语言值回退到默认**

在 `builder_test.go` 中新增测试：

```go
// TestResolveLanguage_不支持的语言回退到默认 测试不支持的语言值回退到默认
func TestResolveLanguage_不支持的语言回退到默认(t *testing.T) {
	// 不支持的语言值应回退到默认语言
	if lang := ResolveLanguage("jp"); lang != DefaultLanguage {
		t.Errorf("不支持的语言应回退到默认，期望 %s，实际 %s", DefaultLanguage, lang)
	}
	if lang := ResolveLanguage("fr"); lang != DefaultLanguage {
		t.Errorf("不支持的语言应回退到默认，期望 %s，实际 %s", DefaultLanguage, lang)
	}
}

// TestResolveLanguage_支持的语言正常返回 测试支持的语言值正常返回
func TestResolveLanguage_支持的语言正常返回(t *testing.T) {
	if lang := ResolveLanguage("cn"); lang != "cn" {
		t.Errorf("期望 cn，实际 %s", lang)
	}
	if lang := ResolveLanguage("en"); lang != "en" {
		t.Errorf("期望 en，实际 %s", lang)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/prompts/... -run "TestResolveLanguage_不支持|TestResolveLanguage_支持" -v
```

预期：`TestResolveLanguage_不支持的语言回退到默认` 失败（当前 "jp" 直接返回而不回退）

- [ ] **Step 3: 实现 isSupportedLanguage + 修改 ResolveLanguage**

在 `builder.go` 中：

```go
// isSupportedLanguage 检查语言是否在支持列表中
//
// 对应 Python: config_language in SUPPORTED_LANGUAGES
func isSupportedLanguage(lang string) bool {
	for _, supported := range SupportedLanguages {
		if lang == supported {
			return true
		}
	}
	return false
}

// ResolveLanguage 按优先级解析提示词语言：
// 配置参数 > AGENT_PROMPT_LANGUAGE 环境变量 > DefaultLanguage。
// 不在 SupportedLanguages 中的值回退到下一优先级。
//
// 对应 Python: resolve_language()
func ResolveLanguage(configLanguage string) string {
	if configLanguage != "" && isSupportedLanguage(configLanguage) {
		return configLanguage
	}
	if envLang := os.Getenv("AGENT_PROMPT_LANGUAGE"); isSupportedLanguage(envLang) {
		return envLang
	}
	return DefaultLanguage
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/prompts/... -v
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/prompts/builder.go internal/agentcore/harness/prompts/builder_test.go
git commit -m "fix(harness): ResolveLanguage 增加 SupportedLanguages 校验，对齐 Python resolve_language"
```

---

### Task 2: 工具规范化辅助函数

**Files:**
- Create: `internal/agentcore/harness/factory.go`（初版，仅工具相关函数）
- Test: `internal/agentcore/harness/factory_test.go`

- [ ] **Step 1: 写失败测试 — normalizeTools 和 isDisabledFreeSearchTool**

在 `factory_test.go` 中：

```go
package harness

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// TestNormalizeTools_空输入 测试空输入返回空列表
func TestNormalizeTools_空输入(t *testing.T) {
	cards, instances := normalizeTools(nil)
	if len(cards) != 0 || len(instances) != 0 {
		t.Errorf("空输入应返回空列表，cards=%d, instances=%d", len(cards), len(instances))
	}
}

// TestNormalizeTools_提取Card 测试从 Tool 实例提取 Card
func TestNormalizeTools_提取Card(t *testing.T) {
	// 使用 fakeTool 测试
	ft := &fakeToolForNormalize{card: &tool.ToolCard{}}
	ft.card.SetName("my_tool")
	ft.card.SetID("tool_1")

	cards, instances := normalizeTools([]tool.Tool{ft})
	if len(cards) != 1 || len(instances) != 1 {
		t.Fatalf("期望 1 card 和 1 instance，实际 cards=%d, instances=%d", len(cards), len(instances))
	}
	if cards[0].GetName() != "my_tool" {
		t.Errorf("Card 名称期望 my_tool，实际 %s", cards[0].GetName())
	}
}

// fakeToolForNormalize 用于测试 normalizeTools 的 fake Tool
type fakeToolForNormalize struct {
	tool.BaseTool
	card *tool.ToolCard
}

func (f *fakeToolForNormalize) Card() *tool.ToolCard { return f.card }
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/... -run "TestNormalizeTools" -v
```

- [ ] **Step 3: 实现 normalizeTools 和 isDisabledFreeSearchTool**

在 `factory.go` 中（首次创建，包含 package 和 import）：

```go
package harness

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeTools 将 Tool 实例列表拆分为 ToolCard 列表和 Tool 实例列表，
// 同时过滤被禁用的 free_search 工具。
//
// 对应 Python: _normalize_tools(tools)
func normalizeTools(tools []tool.Tool) (normalizedCards []*tool.ToolCard, toolInstances []tool.Tool) {
	for _, t := range tools {
		card := t.Card()
		if isDisabledFreeSearchTool(card) {
			continue
		}
		normalizedCards = append(normalizedCards, card)
		toolInstances = append(toolInstances, t)
	}
	return
}

// isDisabledFreeSearchTool 检查工具是否为被禁用的 free_search 工具。
//
// 对应 Python: _is_disabled_free_search_tool(tool)
// ⤵️ 9.1 回填：is_free_search_enabled 检查逻辑
func isDisabledFreeSearchTool(card *tool.ToolCard) bool {
	if card == nil {
		return false
	}
	// 当前 free_search 始终视为启用，不过滤
	// 后续接入 is_free_search_enabled 配置后补全
	return false
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/... -run "TestNormalizeTools" -v
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/factory.go internal/agentcore/harness/factory_test.go
git commit -m "feat(harness): 实现 normalizeTools 工具规范化辅助函数"
```

---

### Task 3: registerToolInstances 辅助函数

**Files:**
- Modify: `internal/agentcore/harness/factory.go`
- Modify: `internal/agentcore/harness/factory_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestRegisterToolInstances_正常注册 测试正常注册 Tool 实例
func TestRegisterToolInstances_正常注册(t *testing.T) {
	ft := &fakeToolForNormalize{card: &tool.ToolCard{}}
	ft.card.SetName("reg_tool")
	ft.card.SetID("reg_tool_1")

	err := registerToolInstances([]tool.Tool{ft}, "test_tag")
	if err != nil {
		t.Errorf("正常注册不应报错: %v", err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 registerToolInstances**

在 `factory.go` 中增加：

```go
import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// registerToolInstances 将 Tool 实例注册到全局资源管理器，
// 使 ToolCard 变为可执行。同 ID 不同实例报错。
//
// 对应 Python: _register_tool_instances(tool_instances, tag=tag)
func registerToolInstances(toolInstances []tool.Tool, tag string) error {
	rm := resources_manager.GetGlobalResourceMgr()
	if rm == nil {
		return fmt.Errorf("全局资源管理器未初始化")
	}
	for _, t := range toolInstances {
		card := t.Card()
		toolID := card.GetID()

		// 检查是否已注册
		existing, err := rm.GetTool([]string{toolID})
		if err == nil && len(existing) > 0 {
			// 同 ID 同实例，仅追加 tag
			_, tagErr := rm.AddResourceTag(toolID, []string{tag})
			if tagErr != nil {
				logger.Warn(logComponent).Str("tool_id", toolID).Err(tagErr).Msg("添加资源标签失败")
			}
			continue
		}

		// 注册新 Tool 实例
		if addErr := rm.AddTool(t, resources_manager.WithTag(tag)); addErr != nil {
			return fmt.Errorf("注册工具失败: tool_id=%s, err=%w", toolID, addErr)
		}
	}
	return nil
}
```

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git commit -m "feat(harness): 实现 registerToolInstances 工具注册辅助函数"
```

---

### Task 4: buildWorkspace 和 buildSysOperation 辅助函数

**Files:**
- Modify: `internal/agentcore/harness/factory.go`
- Modify: `internal/agentcore/harness/factory_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestBuildWorkspace_nil创建默认 测试 Workspace 为 nil 时创建默认实例
func TestBuildWorkspace_nil创建默认(t *testing.T) {
	ws := buildWorkspace(nil, "", "cn")
	if ws == nil {
		t.Fatal("期望非 nil Workspace")
	}
	if ws.RootPath != "./" {
		t.Errorf("期望 RootPath=./，实际 %s", ws.RootPath)
	}
}

// TestBuildWorkspace_已有实例 测试传入已有 Workspace 直接返回
func TestBuildWorkspace_已有实例(t *testing.T) {
	existing := workspace.NewWorkspace("/tmp/test", "en")
	ws := buildWorkspace(existing, "", "en")
	if ws != existing {
		t.Error("期望返回同一实例")
	}
}

// TestBuildSysOperation_提供时直接使用 测试调用方提供 SysOperation 时直接使用
func TestBuildSysOperation_提供时直接使用(t *testing.T) {
	// 提供了 sysOp，应直接返回
	result, err := buildSysOperation(nil, &sysop.BaseSysOperation{}, true)
	if err != nil {
		t.Errorf("不应报错: %v", err)
	}
	if result == nil {
		t.Error("期望非 nil SysOperation")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 buildWorkspace 和 buildSysOperation**

```go
import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// buildWorkspace 构建 Workspace 实例。
// 传入 nil 时创建默认 Workspace(root_path="./")，传入已有实例时直接返回。
//
// 对应 Python: factory.py L260-265 workspace 构建
func buildWorkspace(ws *workspace.Workspace, wsPath string, language string) *workspace.Workspace {
	if ws != nil {
		return ws
	}
	rootPath := wsPath
	if rootPath == "" {
		rootPath = "./"
	}
	return workspace.NewWorkspace(rootPath, language)
}

// buildSysOperation 构建 SysOperation 实例。
// 调用方未提供时，自动创建默认 SysOperationCard（LocalWorkConfig 模式）并注册到 resource_mgr。
//
// 对应 Python: factory.py L267-281 sys_operation 构建
func buildSysOperation(card *schema.AgentCard, sysOp sysop.SysOperation, restrictToWorkDir bool) (sysop.SysOperation, error) {
	if sysOp != nil {
		return sysOp, nil
	}

	// 构建 SysOperationCard
	cardName := "deep_agent"
	cardID := ""
	if card != nil {
		cardName = card.GetName()
		cardID = card.GetID()
	}
	sysopID := fmt.Sprintf("%s_%s", cardName, cardID)

	sysopCard := sysop.NewSysOperationCard(
		sysop.WithSysOpMode(sysop.OperationModeLocal),
		sysop.WithSysOpWorkConfig(&sysop.LocalWorkConfig{
			RestrictToSandbox: restrictToWorkDir,
		}),
	)
	sysopCard.SetID(sysopID)

	// 注册到全局资源管理器
	rm := resources_manager.GetGlobalResourceMgr()
	if rm != nil {
		if addErr := rm.AddSysOperation(sysopID, &sysop.BaseSysOperation{}); addErr != nil {
			logger.Error(logComponent).Str("event_type", "SYS_OPERATION_REGISTER_ERROR").
				Str("card_id", sysopID).Err(addErr).Msg("add_sys_operation 失败")
		}
	}

	// ⤵️ 9.32 回填：LocalSysOperation 实现后，从 resource_mgr 取回真实实例
	// 当前返回 BaseSysOperation 空桩
	return &sysop.BaseSysOperation{}, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git commit -m "feat(harness): 实现 buildWorkspace 和 buildSysOperation 辅助函数"
```

---

### Task 5: injectGeneralPurposeSubagent 辅助函数

**Files:**
- Modify: `internal/agentcore/harness/factory.go`
- Modify: `internal/agentcore/harness/factory_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestInjectGeneralPurposeSubagent_不注入 测试 add=false 时不变
func TestInjectGeneralPurposeSubagent_不注入(t *testing.T) {
	result := injectGeneralPurposeSubagent(nil, false, "cn", nil, "", nil, nil, nil, nil)
	if len(result) != 0 {
		t.Errorf("add=false 不应注入，实际 %d 个", len(result))
	}
}

// TestInjectGeneralPurposeSubagent_注入到头部 测试注入到列表头部
func TestInjectGeneralPurposeSubagent_注入到头部(t *testing.T) {
	existing := []hschema.SubAgentConfig{
		{AgentCard: &schema.AgentCard{}},
	}
	existing[0].AgentCard.SetName("other_agent")

	result := injectGeneralPurposeSubagent(existing, true, "cn", nil, "", nil, nil, nil, nil)
	if len(result) != 2 {
		t.Fatalf("期望 2 个子 Agent，实际 %d", len(result))
	}
	if result[0].AgentCard.GetName() != "general-purpose" {
		t.Errorf("第一个应为 general-purpose，实际 %s", result[0].AgentCard.GetName())
	}
}

// TestInjectGeneralPurposeSubagent_已存在不注入 测试已存在 general-purpose 时不重复注入
func TestInjectGeneralPurposeSubagent_已存在不注入(t *testing.T) {
	existing := []hschema.SubAgentConfig{
		{AgentCard: &schema.AgentCard{}},
	}
	existing[0].AgentCard.SetName("general-purpose")

	result := injectGeneralPurposeSubagent(existing, true, "cn", nil, "", nil, nil, nil, nil)
	if len(result) != 1 {
		t.Errorf("已存在不应重复注入，实际 %d 个", len(result))
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 injectGeneralPurposeSubagent**

```go
import (
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	hpromptstools "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	schema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// injectGeneralPurposeSubagent 当 addGeneralPurposeAgent=True 时注入通用子 Agent。
// 已存在名为 general-purpose 的子 Agent 时不重复注入。
// 从调用方的 rails 中过滤掉 SubagentRail，确保有 SysOperationRail。
//
// 对应 Python: _inject_general_purpose_subagent()
func injectGeneralPurposeSubagent(
	subagents []hschema.SubAgentConfig,
	addGeneralPurposeAgent bool,
	resolvedLanguage string,
	rails []rail.AgentRail,
	systemPrompt string,
	toolInstances []tool.Tool,
	mcps []*mcptypes.McpServerConfig,
	model *llm.Model,
	skills []string,
) []hschema.SubAgentConfig {
	effectiveSubagents := make([]hschema.SubAgentConfig, len(subagents))
	copy(effectiveSubagents, subagents)

	if !addGeneralPurposeAgent {
		return effectiveSubagents
	}

	// 检查是否已存在 general-purpose 子 Agent
	hasGP := false
	for _, s := range effectiveSubagents {
		if s.AgentCard != nil && s.AgentCard.GetName() == "general-purpose" {
			hasGP = true
			break
		}
	}
	if hasGP {
		return effectiveSubagents
	}

	// 获取描述
	desc, ok := hpromptstools.GeneralPurposeAgentDesc[resolvedLanguage]
	if !ok {
		desc = hpromptstools.GeneralPurposeAgentDesc["cn"]
	}

	// 构建 gp_rails：过滤掉 SubagentRail，确保有 SysOperationRail
	// ⤵️ 9.8-9.24 回填：SubagentRail/SysOperationRail 类型断言，当前跳过过滤
	gpRails := make([]rail.AgentRail, 0, len(rails))
	for _, r := range rails {
		gpRails = append(gpRails, r)
	}
	// ⤵️ 9.8-9.24 回填：确保 gpRails 中有 SysOperationRail，当前跳过

	// 从 Tool 实例提取 ToolCard
	toolCards := make([]*tool.ToolCard, 0, len(toolInstances))
	for _, t := range toolInstances {
		toolCards = append(toolCards, t.Card())
	}

	// 注入到列表头部
	gpConfig := hschema.SubAgentConfig{
		AgentCard: func() *schema.AgentCard {
			c := &schema.AgentCard{}
			c.SetName("general-purpose")
			c.SetDescription(desc)
			return c
		}(),
		SystemPrompt:      systemPrompt,
		Tools:             toolCards,
		Mcps:              mcps,
		Model:             model,
		Rails:             gpRails,
		Skills:            skills,
		RestrictToWorkDir: false,
	}

	effectiveSubagents = append([]hschema.SubAgentConfig{gpConfig}, effectiveSubagents...)
	return effectiveSubagents
}
```

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git commit -m "feat(harness): 实现 injectGeneralPurposeSubagent 通用子 Agent 注入"
```

---

### Task 6: alreadyProvided 和 collectDisabledSkillsFromState 辅助函数

**Files:**
- Modify: `internal/agentcore/harness/factory.go`
- Modify: `internal/agentcore/harness/factory_test.go`

- [ ] **Step 1: 写失败测试**

```go
// TestAlreadyProvided_匹配 测试精确类型匹配
func TestAlreadyProvided_匹配(t *testing.T) {
	baseRail := rail.NewBaseRail()
	rails := []rail.AgentRail{baseRail}
	if !alreadyProvided(rails, baseRail) {
		t.Error("应匹配同类型 Rail")
	}
}

// TestAlreadyProvided_不匹配 测试不同类型不匹配
func TestAlreadyProvided_不匹配(t *testing.T) {
	rails := []rail.AgentRail{rail.NewBaseRail()}
	// 用另一个 BaseRail 实例（同类型），应匹配
	otherBase := rail.NewBaseRail()
	if !alreadyProvided(rails, otherBase) {
		t.Error("同类型不同实例应匹配（reflect.TypeOf 精确匹配）")
	}
}

// TestAlreadyProvided_空列表 测试空列表返回 false
func TestAlreadyProvided_空列表(t *testing.T) {
	if alreadyProvided(nil, rail.NewBaseRail()) {
		t.Error("空列表应返回 false")
	}
}

// TestCollectDisabledSkillsFromState_目录不存在 测试目录不存在返回空
func TestCollectDisabledSkillsFromState_目录不存在(t *testing.T) {
	result := collectDisabledSkillsFromState([]string{"/nonexistent/path"})
	if len(result) != 0 {
		t.Errorf("不存在的目录应返回空列表，实际 %d", len(result))
	}
}

// TestCollectDisabledSkillsFromState_正常读取 测试正常读取 skills_state.json
func TestCollectDisabledSkillsFromState_正常读取(t *testing.T) {
	dir := t.TempDir()
	stateContent := `{"skill_configs":{"skill_a":{"enabled":false},"skill_b":{"enabled":true},"skill_c":{"enabled":false}}}`
	statePath := filepath.Join(dir, "skills_state.json")
	if err := os.WriteFile(statePath, []byte(stateContent), 0644); err != nil {
		t.Fatal(err)
	}

	result := collectDisabledSkillsFromState([]string{dir})
	if len(result) != 2 {
		t.Fatalf("期望 2 个禁用技能，实际 %d: %v", len(result), result)
	}
	// 结果应排序
	if result[0] != "skill_a" || result[1] != "skill_c" {
		t.Errorf("期望 [skill_a skill_c]，实际 %v", result)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 alreadyProvided 和 collectDisabledSkillsFromState**

```go
import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
)

// alreadyProvided 检查调用方是否已显式提供了指定类型的 Rail。
// 使用 reflect.TypeOf 精确类型匹配，不匹配子类。
//
// 对应 Python: _already_provided(rail_cls) — Python 使用 issubclass 支持子类匹配，
// Go 端当前使用精确匹配，后续需要可升级为接口断言。
func alreadyProvided(rails []rail.AgentRail, target rail.AgentRail) bool {
	targetType := reflect.TypeOf(target)
	for _, r := range rails {
		if reflect.TypeOf(r) == targetType {
			return true
		}
	}
	return false
}

// collectDisabledSkillsFromState 从每个 skills_dir 读取 skills_state.json，
// 收集 enabled=false 的技能名称。结果按字母排序。
//
// 对应 Python: _collect_disabled_skills_from_state(skills_dirs)
func collectDisabledSkillsFromState(skillsDirs []string) []string {
	disabled := make(map[string]struct{})
	for _, dir := range skillsDirs {
		statePath := filepath.Join(dir, "skills_state.json")
		data, err := os.ReadFile(statePath)
		if err != nil {
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			logger.Warn(logComponent).Str("path", statePath).Err(err).Msg("解析 skills_state.json 失败")
			continue
		}
		configs, ok := parsed["skill_configs"].(map[string]any)
		if !ok {
			continue
		}
		for name, cfg := range configs {
			cfgMap, ok := cfg.(map[string]any)
			if !ok {
				continue
			}
			if enabled, ok := cfgMap["enabled"].(bool); ok && !enabled {
				disabled[name] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(disabled))
	for name := range disabled {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}
```

- [ ] **Step 4: 运行测试确认通过**

- [ ] **Step 5: 提交**

```bash
git commit -m "feat(harness): 实现 alreadyProvided 和 collectDisabledSkillsFromState"
```

---

### Task 7: CreateDeepAgentParams 和 CreateDeepAgent 主函数

**Files:**
- Modify: `internal/agentcore/harness/factory.go`
- Modify: `internal/agentcore/harness/factory_test.go`

- [ ] **Step 1: 写失败测试 — CreateDeepAgent 最小集成**

```go
// TestCreateDeepAgent_最小参数 测试最小参数创建 DeepAgent
func TestCreateDeepAgent_最小参数(t *testing.T) {
	ctx := context.Background()
	params := CreateDeepAgentParams{
		MaxIterations: 15,
	}
	agent, err := CreateDeepAgent(ctx, params)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if agent == nil {
		t.Fatal("期望非 nil DeepAgent")
	}
	if agent.card.GetName() != "deep_agent" {
		t.Errorf("默认名称应为 deep_agent，实际 %s", agent.card.GetName())
	}
}

// TestCreateDeepAgent_自定义Card 测试自定义 AgentCard
func TestCreateDeepAgent_自定义Card(t *testing.T) {
	ctx := context.Background()
	card := &schema.AgentCard{}
	card.SetName("my_agent")
	card.SetID("agent_1")

	params := CreateDeepAgentParams{
		Card:          card,
		MaxIterations: 10,
	}
	agent, err := CreateDeepAgent(ctx, params)
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if agent.card.GetName() != "my_agent" {
		t.Errorf("期望 my_agent，实际 %s", agent.card.GetName())
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 实现 CreateDeepAgentParams 和 CreateDeepAgent**

```go
import (
	"context"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CreateDeepAgentParams 创建 DeepAgent 的参数集。
//
// 对应 Python: create_deep_agent() 的全部关键字参数
type CreateDeepAgentParams struct {
	// Model 预构造的 Model 实例
	Model *llm.Model
	// Card Agent 身份卡片，nil 时创建默认卡片
	Card *agentschema.AgentCard
	// SystemPrompt 内层 ReActAgent 的系统提示词
	SystemPrompt string
	// ToolInstances Tool 实例列表，从中提取 ToolCard + 注册到 resource_mgr
	ToolInstances []tool.Tool
	// Mcps MCP 服务器配置列表
	Mcps []*mcptypes.McpServerConfig
	// Subagents 子 Agent 配置列表
	Subagents []hschema.SubAgentConfig
	// Rails AgentRail 实例列表
	Rails []rail.AgentRail
	// EnableTaskLoop 启用外层任务循环
	EnableTaskLoop bool
	// EnableAsyncSubagent 启用异步子 Agent 模式
	EnableAsyncSubagent bool
	// AddGeneralPurposeAgent 添加通用目的子 Agent
	AddGeneralPurposeAgent bool
	// MaxIterations 每次 invoke 的最大 ReAct 迭代次数
	MaxIterations int
	// Workspace 工作空间，nil 时创建默认
	Workspace *workspace.Workspace
	// Skills 技能定义列表
	Skills []string
	// Backend 后端协议实例（any 占位，P2 预留）
	Backend any
	// SysOperation 系统操作，nil 时自动创建默认
	SysOperation sysop.SysOperation
	// Language 提示词语言
	Language string
	// PromptMode 提示词模式
	PromptMode hschema.PromptMode
	// VisionModelConfig 视觉模型配置
	VisionModelConfig *hschema.VisionModelConfig
	// AudioModelConfig 音频模型配置
	AudioModelConfig *hschema.AudioModelConfig
	// EnableReadImageMultimodal 启用图像多模态读取
	EnableReadImageMultimodal bool
	// EnableTaskPlanning 启用任务规划
	EnableTaskPlanning bool
	// RestrictToWorkDir 限制文件访问到工作空间目录
	RestrictToWorkDir bool
	// DefaultMode 初始 Agent 模式
	DefaultMode hschema.AgentMode
	// ModelSelection 模型选择配置
	ModelSelection []hschema.ModelSelectionEntry
	// EnableSkillDiscovery 启用技能发现
	EnableSkillDiscovery bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateDeepAgent 创建并配置 DeepAgent 实例。
//
// 按以下 10 步流程组装，对齐 Python create_deep_agent()：
//  1. 默认 AgentCard
//  2. 工具规范化 (normalizeTools)
//  3. 语言解析 (ResolveLanguage)
//  4. 通用子 Agent 注入 (injectGeneralPurposeSubagent)
//  5. Workspace 构建 (buildWorkspace)
//  6. SysOperation 构建 (buildSysOperation)
//  7. DeepAgentConfig 组装
//  8. DeepAgent 实例化 (NewDeepAgent + ConfigureDeepConfig)
//  9. 工具注册 (registerToolInstances + ability_manager.add)
// 10. Rail 注册 (显式 + 默认自动添加)
//
// 对应 Python: openjiuwen/harness/factory.py create_deep_agent()
func CreateDeepAgent(ctx context.Context, params CreateDeepAgentParams) (*DeepAgent, error) {
	// ── 步骤 1：默认 AgentCard ──
	card := params.Card
	if card == nil {
		card = &agentschema.AgentCard{}
		card.SetName("deep_agent")
		card.SetDescription("DeepAgent instance")
	}

	// ── 步骤 2：工具规范化 ──
	normalizedCards, toolInstances := normalizeTools(params.ToolInstances)

	// ── 步骤 3：语言解析 ──
	resolvedLanguage := hprompts.ResolveLanguage(params.Language)

	// ── 步骤 4：通用子 Agent 注入 ──
	effectiveSubagents := injectGeneralPurposeSubagent(
		params.Subagents,
		params.AddGeneralPurposeAgent,
		resolvedLanguage,
		params.Rails,
		params.SystemPrompt,
		params.ToolInstances,
		params.Mcps,
		params.Model,
		params.Skills,
	)

	// ── 步骤 5：Workspace 构建 ──
	workspaceObj := buildWorkspace(params.Workspace, "", resolvedLanguage)

	// ── 步骤 6：SysOperation 构建 ──
	sysOp, err := buildSysOperation(card, params.SysOperation, params.RestrictToWorkDir)
	if err != nil {
		return nil, fmt.Errorf("构建 SysOperation 失败: %w", err)
	}

	// ── 步骤 7：DeepAgentConfig 组装 ──
	config := hschema.NewDeepAgentConfig()
	config.Model = params.Model
	config.Card = card
	config.SystemPrompt = params.SystemPrompt
	config.EnableTaskLoop = params.EnableTaskLoop
	config.EnableAsyncSubagent = params.EnableAsyncSubagent
	config.AddGeneralPurposeAgent = params.AddGeneralPurposeAgent
	config.MaxIterations = params.MaxIterations
	if len(effectiveSubagents) > 0 {
		config.Subagents = effectiveSubagents
	}
	if len(normalizedCards) > 0 {
		config.Tools = normalizedCards
	}
	config.Mcps = params.Mcps
	config.Workspace = workspaceObj
	config.Skills = params.Skills
	config.EnableSkillDiscovery = params.EnableSkillDiscovery
	config.Backend = params.Backend
	config.SysOperation = sysOp
	config.Language = resolvedLanguage
	config.PromptMode = params.PromptMode
	config.VisionModelConfig = params.VisionModelConfig
	config.AudioModelConfig = params.AudioModelConfig
	config.EnableReadImageMultimodal = params.EnableReadImageMultimodal
	config.DefaultMode = params.DefaultMode
	config.EnablePlanMode = params.EnableTaskPlanning
	config.ModelSelection = params.ModelSelection

	// ── 步骤 8：DeepAgent 实例化 ──
	agent := NewDeepAgent(card)
	if cfgErr := agent.ConfigureDeepConfig(ctx, config); cfgErr != nil {
		return nil, fmt.Errorf("配置 DeepAgent 失败: %w", cfgErr)
	}

	// ── 步骤 9：工具注册 ──
	if len(toolInstances) > 0 {
		tag := card.GetID()
		if tag == "" {
			tag = card.GetName()
		}
		if regErr := registerToolInstances(toolInstances, tag); regErr != nil {
			logger.Warn(logComponent).Err(regErr).Msg("注册工具实例失败")
		}
	}
	for _, tc := range normalizedCards {
		agent.abilityManager.Add(tc)
	}

	// ── 步骤 10：Rail 注册 ──
	// 10a：显式提供的 Rails
	for _, r := range params.Rails {
		agent.AddRail(r)
	}

	// 10b：默认 Rails 自动添加
	addDefaultRails(agent, params, config, effectiveSubagents, workspaceObj, resolvedLanguage)

	return agent, nil
}
```

- [ ] **Step 4: 实现 addDefaultRails 辅助函数**

```go
// addDefaultRails 自动添加调用方未显式提供的默认 Rail。
//
// 对应 Python: factory.py L358-367 default_rails 自动添加
func addDefaultRails(
	agent *DeepAgent,
	params CreateDeepAgentParams,
	config *hschema.DeepAgentConfig,
	effectiveSubagents []hschema.SubAgentConfig,
	ws *workspace.Workspace,
	resolvedLanguage string,
) {
	userProvidedTypes := make(map[reflect.Type]bool)
	for _, r := range params.Rails {
		userProvidedTypes[reflect.TypeOf(r)] = true
	}

	// SecurityRail — 始终添加
	// ⤵️ 9.8-9.24 回填：SecurityRail 具体实例化
	if !alreadyProvidedByType(userProvidedTypes, "SecurityRail") {
		agent.AddRail(rail.NewBaseRail()) // 占位，待 SecurityRail 实现后替换
	}

	// TaskPlanningRail — 仅当 enable_task_planning=True 时添加
	if params.EnableTaskPlanning && !alreadyProvidedByType(userProvidedTypes, "TaskPlanningRail") {
		// ⤵️ 9.19-9.24 回填：TaskPlanningRail 具体实例化（含 model_selection）
		agent.AddRail(rail.NewBaseRail())
	}

	// SkillUseRail — 仅当有 skills 或启用了 skill_discovery 时添加
	if (len(params.Skills) > 0 || config.EnableSkillDiscovery) &&
		!alreadyProvidedByType(userProvidedTypes, "SkillUseRail") {
		// ⤵️ 9.8-9.24 回填：SkillUseRail 具体实例化（含 skills_dir + disabled_skills）
		agent.AddRail(rail.NewBaseRail())
	}

	// SubagentRail — 仅当有 subagents 时添加
	if len(effectiveSubagents) > 0 && !alreadyProvidedByType(userProvidedTypes, "SubagentRail") {
		// ⤵️ 9.8-9.24 回填：SubagentRail 具体实例化（含 enable_async_subagent）
		agent.AddRail(rail.NewBaseRail())
	}
}

// alreadyProvidedByType 检查用户提供的 Rail 类型映射中是否包含指定类型名称。
// 使用字符串名称而非 reflect.Type，因为具体 Rail 类型尚未定义。
//
// ⤵️ 9.8-9.24 回填：替换为 alreadyProvided(rails, &ConcreteRail{}) 精确类型匹配
func alreadyProvidedByType(typeMap map[reflect.Type]bool, _ string) bool {
	// 当前所有具体 Rail 都未实现，userProvidedTypes 中不会有匹配
	// 等 Rail 实现后，此函数改为真正的类型检查
	return false
}
```

- [ ] **Step 5: 运行测试确认通过**

- [ ] **Step 6: 提交**

```bash
git commit -m "feat(harness): 实现 CreateDeepAgent 工厂主函数（10 步完整流程）"
```

---

### Task 8: 回填已有占位 + 注释修正

**Files:**
- Modify: `internal/agentcore/harness/deep_agent.go`
- Modify: `internal/agentcore/harness/harness_config/builder.go`
- Modify: `internal/agentcore/harness/schema/config.go`

- [ ] **Step 1: 回填 deep_agent.go CreateSubagent default 分支**

将第610行：
```go
default:
    return nil, fmt.Errorf("create_deep_agent 尚未实现，⤵️ 9.3 回填")
```

改为：
```go
default:
    // 通过 CreateDeepAgent 创建子 Agent 实例
    createParams := buildCreateParamsFromSubagentKwargs(kwargs)
    subAgent, createErr := CreateDeepAgent(ctx, createParams)
    if createErr != nil {
        return nil, fmt.Errorf("创建子 Agent 失败: %w", createErr)
    }
    return subAgent, nil
```

需要实现 `buildCreateParamsFromSubagentKwargs` 将 `SubagentCreateParams` 转换为 `CreateDeepAgentParams`。

- [ ] **Step 2: 回填 builder.go Build 方法**

将第83-86行占位改为调用 `CreateDeepAgent`。

- [ ] **Step 3: 修正 config.go Backend 注释**

将2处 `⤵️ 9.3 回填为 BackendProtocol 接口` 修正为 `P2 预留，等 Backend 实现时回填`。

- [ ] **Step 4: 编译验证**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/...
```

- [ ] **Step 5: 提交**

```bash
git commit -m "feat(harness): 回填 CreateSubagent/builder 占位，修正 Backend 注释"
```

---

### Task 9: doc.go 更新 + 格式合规

**Files:**
- Modify: `internal/agentcore/harness/doc.go`

- [ ] **Step 1: 更新 doc.go 文件目录**

在文件目录树中添加 `factory.go` 条目。

- [ ] **Step 2: 运行项目规范检查**

```bash
cd /home/opensource/uap-claw-go && go vet ./internal/agentcore/harness/...
```

- [ ] **Step 3: 提交**

```bash
git commit -m "docs(harness): 更新 doc.go 文件目录，添加 factory.go"
```

---

### Task 10: 全量测试 + 覆盖率验证

**Files:**
- Modify: `internal/agentcore/harness/factory_test.go`（补充测试）

- [ ] **Step 1: 补全 CreateDeepAgent 集成测试**

增加：
- TestCreateDeepAgent_语言解析回退：传入不支持的语言，验证回退到默认
- TestCreateDeepAgent_Workspace构建：验证 nil 时创建默认
- TestCreateDeepAgent_Rail注册：验证显式 + 默认 Rail 注册
- TestCreateDeepAgent_工具注册：验证 ToolCard 注册到 ability_manager

- [ ] **Step 2: 运行全量测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/... -v -cover
```

- [ ] **Step 3: 检查覆盖率**

```bash
cd /home/opensource/uap-claw-go && go test -coverprofile=coverage.out ./internal/agentcore/harness/... && go tool cover -func=coverage.out | grep factory
```

目标：factory.go 覆盖率 ≥ 85%

- [ ] **Step 4: 提交**

```bash
git commit -m "test(harness): 补全 CreateDeepAgent 集成测试，覆盖率 ≥ 85%"
```

---

### Task 11: IMPLEMENTATION_PLAN.md 状态同步

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 9.3 状态**

将 9.3 行的 `☐` 改为 `✅`。

- [ ] **Step 2: 提交**

```bash
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 9.3 状态为 ✅"
```
