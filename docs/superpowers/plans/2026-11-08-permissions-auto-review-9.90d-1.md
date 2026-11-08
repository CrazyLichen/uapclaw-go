# 9.90d-1 前置件实现计划

> **For agentic workers:** REQUIRED SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 9.90d-2 自动审查层所依赖的全部前置件——5 个从 9.90e 提前的护栏/审计文件、6 个原盘点遗漏的跨包工具、`JiuwenSwarmPermissionInterruptRail` 子类（417 行，计划表此前 0 章节）、`openjiuwen_contract` 契约层，以及 `MarkTrusted()` 基础设施。

**Architecture:** 本批不产生任何审批决策，纯前置。按依赖层次分三组：(a) 无依赖的 6 个跨包工具（tool_display / command_runtime / sandbox_no_host_fallback / run_permissions / workspace_paths / pdf_tools）；(b) `permissions` 包内 7 个文件，依赖 9.90a/b/c 已落地的 `ToolDecisionFacts` / `network_scope` / `root_context` / `fileguard`；(c) 两个基础设施改造（`rails/base.go` 加 `MarkTrusted()`、`base_rail.go` 定义 `BasePermissionRail` 接口）。

**Tech Stack:** Go 1.x + 项目既有依赖（无新增第三方库）；测试用标准库 `testing` + `t.TempDir()`；日志用 `logger.ComponentCommon` / `logger.ComponentAgentCore`；覆盖率 ≥85%。

---

## 全局约定（所有任务适用）

- 编译前 `export GOPROXY=https://goproxy.cn,direct`
- **所有注释中文**（规则 go-code-conventions 规范 1）
- 文件内声明顺序：接口 → 结构体 → 枚举 → 常量 → 全局变量 → 导出函数 → 非导出函数，各类间用分隔注释
- 每个包必须有 `doc.go`，新增文件必须同步文件树（project_ruler 规则 1）
- 全量测试必须带 CI 同款标签：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`
- 提交时**显式 `git add <文件清单>`**，禁止 `git commit -am`（工作区可能混有其他任务的改动）
- 日志文案统一英文，注释保持中文
- 模块路径前缀：`github.com/uapclaw/uapclaw-go/`

### 已核实的关键签名（实现时直接引用，勿再猜）

```go
// internal/agentcore/sys_operation/cwd/cwd.go
func GetCwd(ctx context.Context) string
func GetProjectRoot(ctx context.Context) string
func GetWorkspace(ctx context.Context) string   // 空串表示未设置（Python None）

// internal/swarm/agents/harness/common/rails/permissions/network_scope.go
func NormalizeNetworkHost(host string) string
func NetworkHostRejectionReason(host string) string
func HostMatchesAllowedDomain(host, allowedDomain string) bool
func HasSecretQuery(query string) bool

// internal/swarm/agents/harness/common/rails/permissions/root_context.go
type OriginalUserIntentEvidence struct {
    Source  string
    Text    string
    Context *RootDecisionContext
}
func OriginalUserIntent(ctx context.Context) *OriginalUserIntentEvidence
func HasValidAskUserClarification(evidence *OriginalUserIntentEvidence) bool
func FileDeliveryConstraintText(evidence *OriginalUserIntentEvidence) string
const HostUserMessageSource = "host_user_message"
const UserIntentSourceHostUserMessage = HostUserMessageSource

// internal/swarm/agents/harness/common/rails/permissions/tool_decision_facts.go
type ToolDecisionFacts struct { /* 9.90b 已落地 */ }
// 字段：ToolName / ToolCategory / UntrustedArgs / WorkspaceRoot /
//       ReadPaths / WritePaths / ExternalPaths / AccessesKnown / Capability

// internal/agentcore/harness/rails/security/tool_security_rail.go
type PermissionInterruptRail struct { /* 基础权限轨 */ }
func (r *PermissionInterruptRail) BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error
func (r *PermissionInterruptRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc
func (r *PermissionInterruptRail) UpdateConfig(config map[string]any, toolNames []string)
func (r *PermissionInterruptRail) Engine() *harnesssecurity.PermissionEngine
func (r *PermissionInterruptRail) SetTrustedDirs(trustedDirs []string)

// internal/agentcore/harness/rails/base.go
type DeepAgentRail struct {
    agentinterfaces.BaseRail
    workspace    *workspace.Workspace
    sysOperation sys_operation.SysOperation
}
```

---

## 文件结构

### 新建文件

| 路径 | 职责 | 行数（Python 对应） |
|---|---|---|
| `internal/common/tool_display/tool_display.go` | `call_goal` schema 注入 + 剥离 | 87 |
| `internal/common/tool_display/doc.go` | 包文档 | — |
| `internal/swarm/agents/harness/common/tools/command_runtime.go` | 命令工作目录三根白名单解析 | 98 |
| `internal/swarm/server/runtime/sandbox_no_host_fallback.go` | ContextVar 三函数（沙箱禁宿主回退） | 38 |
| `internal/swarm/runtime/run_permissions.py` → `run_permissions.go` | 请求级权限覆盖层 | 54 |
| `internal/swarm/agents/harness/common/workspace_paths.go` | 工作区路径相对化 + 脱敏（13 函数） | 518 |
| `internal/swarm/agents/harness/common/tools/pdf_tools.go` | PDF 路径解析 + 读取 | 275 |
| `internal/swarm/agents/harness/common/rails/permissions/openjiuwen_contract.go` | 结果分类 + 响应构造（12 函数） | 183 |
| `internal/swarm/agents/harness/common/rails/permissions/permission_interrupt_rail.go` | `JiuwenSwarmPermissionInterruptRail` 子类（22 方法） | 417 |
| `internal/swarm/agents/harness/common/rails/permissions/persistent_audit.go` | 审计字段脱敏 + JSONL 落盘 | 296 |
| `internal/swarm/agents/harness/common/rails/permissions/url_safety.go` | URL 安全性判定（25 函数） | 541 |
| `internal/swarm/agents/harness/common/rails/permissions/generated_artifact_delivery.go` | send_file 授权租约 | 175 |
| `internal/swarm/agents/harness/common/rails/permissions/send_file_path_guard.go` | send_file 路径守卫评估器 | 315 |
| `internal/swarm/agents/harness/common/rails/permissions/trusted_search_urls.go` | 会话可信搜索 URL（producer 租约） | 236 |
| `internal/swarm/agents/harness/common/rails/permissions/base_rail.go` | `BasePermissionRail` 接口定义 | ~60 |

### 修改文件

| 路径 | 改动 |
|---|---|
| `internal/agentcore/harness/rails/base.go` | 新增 `trusted uint32` 字段 + `MarkTrusted()` / `IsTrusted()` |
| `internal/swarm/agents/harness/common/rails/permissions/policy_eval.go:234` | 删除 `⤵️ 9.90e 回填` 注释（`SendFilePathGuardEvaluator` 本批落地） |
| `internal/swarm/agents/harness/common/rails/permissions/doc.go` | 更新文件树 + 删除已回填的缺口条目 |
| `internal/swarm/agents/harness/common/doc.go` | 新增 `workspace_paths.go` / `pdf_tools.go` / `command_runtime.go` 条目 |
| `internal/swarm/agents/harness/common/tools/doc.go` | 新增 `command_runtime.go` / `pdf_tools.go` 条目 |
| `internal/common/doc.go`（若存在） | 新增 `tool_display/` 子包条目 |
| `IMPLEMENTATION_PLAN.md` | 9.90d 拆为 9.90d-1 / 9.90d-2；新增 9.11b 行；9.17 与 9.90e 行追加 ⤴️ |

---

## Task 0：前置核实（不改代码）

**Files:** 无（只读核实 + 写入 spec §12）

- [ ] **Step 1: 核实 `internal/common` 是否已有 `doc.go` 汇总子包**

```bash
ls /home/opensource/uapclaw-gateway/internal/common/doc.go 2>/dev/null || echo "无顶层 doc.go"
```

预期：无顶层 `doc.go`（`internal/common` 下都是子包）。若存在，Task 1 需同步更新它。

- [ ] **Step 2: 核实 `tool_display` 的两个消费者是否真的存在**

```bash
cd /home/opensource/uapclaw-gateway
grep -rn "ExtractCallGoal\|CallGoal" internal/ --include=*.go | head
```

预期：0 命中（确认无需兼容既有实现，纯新建）。

- [ ] **Step 3: 核实 Python `pdf_tools.py` 的实际函数清单**

```bash
grep -n "^def \|^class " /home/opensource/jiuwenswarm-develop/jiuwenswarm/agents/harness/common/tools/pdf_tools.py
```

预期输出（写入 spec §12）：

```
def _resolve_pdf_path(...)
def read_pdf(...)
（其余为私有 helper）
```

- [ ] **Step 4: 核实 `PermissionInterruptRail` 缺哪 5 个成员**

```bash
cd /home/opensource/uapclaw-gateway
for m in PermissionCheckContext GetPermissionSnapshot InstalledPermissionConfig ReplaceInstalledGlobalPermissions BindApprovalRequest; do
  echo -n "$m: "; grep -c "func (r \*PermissionInterruptRail) $m" internal/agentcore/harness/rails/security/tool_security_rail.go
done
```

预期：全部为 0（确认需在子类中新增）。

- [ ] **Step 5: 核实 `BaseRail` 是否可加字段**

```bash
sed -n '145,165p' /home/opensource/uapclaw-gateway/internal/agentcore/single_agent/interfaces/callback.go
```

预期：`BaseRail` 只有 `priority int` 一个字段，可安全增补 `trusted uint32`。

- [ ] **Step 6: 把核实结果写入 spec §12 并提交**

```bash
cd /home/opensource/uapclaw-gateway
git add docs/superpowers/specs/2026-11-08-permissions-auto-review-9.90d-design.md
git commit -m "docs(permissions): 9.90d-1 Task 0 前置核实结果"
```

---

## Task 1：`tool_display` 包（87 行）

**Files:**
- Create: `internal/common/tool_display/tool_display.go`
- Create: `internal/common/tool_display/doc.go`
- Test: `internal/common/tool_display/tool_display_test.go`

**Python 对应：** `jiuwenswarm/common/tool_display.py`（87 行）

- [ ] **Step 1: 写失败测试**

```go
package tool_display

import (
	"encoding/json"
	"testing"
)

// TestInjectCallGoalSchema 验证 call_goal 注入且不覆盖已有定义
func TestInjectCallGoalSchema(t *testing.T) {
	params := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
	InjectCallGoalSchema(params)

	props := params["properties"].(map[string]any)
	goal, ok := props["call_goal"].(map[string]any)
	if !ok {
		t.Fatalf("call_goal not injected")
	}
	if goal["type"] != "string" {
		t.Errorf("call_goal type = %v, want string", goal["type"])
	}
}

// TestInjectCallGoalSchema_已有定义不覆盖 验证不覆盖工具自带的 call_goal
func TestInjectCallGoalSchema_已有定义不覆盖(t *testing.T) {
	existing := map[string]any{"type": "string", "description": "custom"}
	params := map[string]any{
		"properties": map[string]any{"call_goal": existing},
	}
	InjectCallGoalSchema(params)

	got := params["properties"].(map[string]any)["call_goal"].(map[string]any)
	if got["description"] != "custom" {
		t.Errorf("existing call_goal overwritten: %v", got)
	}
}

// TestInjectCallGoalSchema_清理误注入的display_name 验证 UI 文案型 display_name 被移除
func TestInjectCallGoalSchema_清理误注入的display_name(t *testing.T) {
	params := map[string]any{
		"properties": map[string]any{
			"display_name": map[string]any{"description": "仅界面展示"},
			"keep_me":      map[string]any{"description": "业务字段"},
		},
	}
	InjectCallGoalSchema(params)

	props := params["properties"].(map[string]any)
	if _, exists := props["display_name"]; exists {
		t.Error("UI-style display_name should be removed")
	}
	if _, exists := props["keep_me"]; !exists {
		t.Error("business field keep_me should be kept")
	}
}

// TestInjectCallGoalSchema_非map输入不panic 验证非法输入安全返回
func TestInjectCallGoalSchema_非map输入不panic(t *testing.T) {
	InjectCallGoalSchema(nil)
	InjectCallGoalSchema("not a map")
	InjectCallGoalSchema(42)
}

// TestExtractCallGoal_从map提取 验证提取并剥离 call_goal
func TestExtractCallGoal_从map提取(t *testing.T) {
	args := map[string]any{"call_goal": "调研官网", "path": "/tmp/x"}
	name, cleaned := ExtractCallGoal(args)

	if name != "调研官网" {
		t.Errorf("name = %q, want 调研官网", name)
	}
	m := cleaned.(map[string]any)
	if _, exists := m["call_goal"]; exists {
		t.Error("call_goal should be stripped")
	}
	if m["path"] != "/tmp/x" {
		t.Errorf("path = %v, want /tmp/x", m["path"])
	}
}

// TestExtractCallGoal_兼容callGoal驼峰 验证两个键名都识别
func TestExtractCallGoal_兼容callGoal驼峰(t *testing.T) {
	name, _ := ExtractCallGoal(map[string]any{"callGoal": "驼峰写法"})
	if name != "驼峰写法" {
		t.Errorf("name = %q, want 驼峰写法", name)
	}
}

// TestExtractCallGoal_从JSON字符串提取 验证字符串输入被解析后返回 JSON
func TestExtractCallGoal_从JSON字符串提取(t *testing.T) {
	raw := `{"call_goal":"字符串输入","x":1}`
	name, cleaned := ExtractCallGoal(raw)

	if name != "字符串输入" {
		t.Errorf("name = %q", name)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(cleaned.(string)), &m); err != nil {
		t.Fatalf("cleaned is not valid JSON: %v", err)
	}
	if _, exists := m["call_goal"]; exists {
		t.Error("call_goal should be stripped from JSON string")
	}
	if m["x"] != float64(1) {
		t.Errorf("x = %v, want 1", m["x"])
	}
}

// TestExtractCallGoal_截断到200字符 验证超长被截断并加省略号
func TestExtractCallGoal_截断到200字符(t *testing.T) {
	long := ""
	for i := 0; i < 300; i++ {
		long += "a"
	}
	name, _ := ExtractCallGoal(map[string]any{"call_goal": long})

	if len([]rune(name)) != 201 { // 200 字符 + 省略号
		t.Errorf("len = %d, want 201", len([]rune(name)))
	}
	if name[len(name)-3:] != "…" {
		t.Errorf("should end with ellipsis, got %q", name[len(name)-3:])
	}
}

// TestExtractCallGoal_不触碰display_name 验证业务字段 display_name 保留
func TestExtractCallGoal_不触碰display_name(t *testing.T) {
	name, cleaned := ExtractCallGoal(map[string]any{
		"display_name": "player-x",
		"call_goal":    "通知落子",
	})

	if name != "通知落子" {
		t.Errorf("name = %q", name)
	}
	m := cleaned.(map[string]any)
	if m["display_name"] != "player-x" {
		t.Errorf("display_name = %v, want player-x（业务字段不可剥离）", m["display_name"])
	}
}

// TestExtractCallGoal_空白与非法输入 验证返回空名与原始值
func TestExtractCallGoal_空白与非法输入(t *testing.T) {
	if name, raw := ExtractCallGoal("  "); name != "" || raw != "  " {
		t.Errorf("whitespace: name=%q raw=%v", name, raw)
	}
	if name, raw := ExtractCallGoal("{invalid json"); name != "" || raw != "{invalid json" {
		t.Errorf("invalid json: name=%q raw=%v", name, raw)
	}
	if name, raw := ExtractCallGoal(nil); name != "" || raw != nil {
		t.Errorf("nil: name=%q raw=%v", name, raw)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/common/tool_display/ -v
```

预期：`FAIL` — `undefined: InjectCallGoalSchema` / `undefined: ExtractCallGoal`

- [ ] **Step 3: 写实现**

```go
// Package tool_display 提供工具调用展示辅助：call_goal schema 注入与参数剥离。
//
// 优先使用主模型在 tool_call 参数里一并给出的 call_goal（一句目标说明，
// 如「调研 openJiuwen 官网信息」）；执行前会从 arguments 剥掉该字段以免
// schema 校验失败。
//
// 注意：绝不能用 display_name 作为 UI 字段名——team 的 spawn_teammate /
// build_team 等工具本身就有必填的 display_name（成员展示名），撞名会导致
// 参数被误剥、持续报错。
//
// 文件目录：
//
//	tool_display/
//	├── doc.go           # 包文档
//	└── tool_display.go  # call_goal schema 注入 + 剥离
//
// 对应 Python 代码：jiuwenswarm/common/tool_display.py
package tool_display

import (
	"encoding/json"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// callGoalMaxLen call_goal 最大长度（超出截断）
	callGoalMaxLen = 200
)

var (
	// callGoalKeys call_goal 的可接受键名
	callGoalKeys = []string{"call_goal", "callGoal"}

	// callGoalSchema call_goal 的 JSON Schema 定义
	callGoalSchema = map[string]any{
		"type": "string",
		"description": "一句简短说明，讲清这次工具调用要达成的目标（仅界面展示）。" +
			"必须使用当前对话所用的语言，不要固定用中文。" +
			"Write it in the same language as the conversation with the user; " +
			"do not default to Chinese. " +
			"中文对话写「调研 openJiuwen 官网信息」「创建三子棋对战团队」「通知 player-x 落子」，" +
			"英文对话写 \"Research the openJiuwen website\" \"Create a tic-tac-toe team\"。" +
			"不要只写工具名或裸 URL；不影响工具实际执行。" +
			"与工具自带的 display_name（成员/团队展示名）以及 send_message.summary 都不是同一个字段，" +
			"调用 spawn_member / spawn_teammate / send_message / build_team 时也必须填写。",
	}

	// uiDisplayNameMarkers 判定 display_name 是否为误注入的 UI 文案
	uiDisplayNameMarkers = []string{"界面展示", "UI only", "不影响工具实际执行"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// InjectCallGoalSchema 给工具 JSON Schema 注入可选 call_goal，
// 供主模型随 tool_call 一并产出。
//
// 若工具已有 call_goal 定义则不覆盖；永远不要碰 display_name（除非
// 它明显是本模块曾误注入的 UI 文案）。
//
// Python: inject_call_goal_schema(parameters)
func InjectCallGoalSchema(parameters any) {
	params, ok := parameters.(map[string]any)
	if !ok {
		return
	}
	props, ok := params["properties"].(map[string]any)
	if !ok {
		props = map[string]any{}
		params["properties"] = props
	}

	// 若工具已有 call_goal 定义则不覆盖
	if _, exists := props["call_goal"]; !exists {
		props["call_goal"] = copyMap(callGoalSchema)
	}

	// 清理此前误注入的 UI 用 display_name（仅当描述像我们的 UI 文案时）
	if existing, ok := props["display_name"].(map[string]any); ok {
		desc, _ := existing["description"].(string)
		for _, marker := range uiDisplayNameMarkers {
			if strings.Contains(desc, marker) {
				delete(props, "display_name")
				break
			}
		}
	}
}

// ExtractCallGoal 从 arguments 取出 call_goal，并返回剥掉该字段后的
// arguments（保持原类型风格）。
//
// 不会触碰 display_name（team 成员名等业务字段）。call_goal 截断到
// callGoalMaxLen，避免异常超长撑爆展示/事件负载。
//
// Python: extract_call_goal(arguments) -> tuple[str, Any]
func ExtractCallGoal(arguments any) (string, any) {
	switch v := arguments.(type) {
	case map[string]any:
		cleaned := make(map[string]any, len(v))
		for k, val := range v {
			cleaned[k] = val
		}
		name := ""
		for _, key := range callGoalKeys {
			raw, exists := cleaned[key]
			if !exists {
				delete(cleaned, key)
				continue
			}
			delete(cleaned, key)
			s, ok := raw.(string)
			if !ok {
				continue
			}
			s = strings.TrimSpace(s)
			if s != "" && name == "" {
				name = truncate(s, callGoalMaxLen)
			}
		}
		return name, cleaned

	case string:
		if strings.TrimSpace(v) == "" {
			return "", arguments
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(v), &parsed); err != nil {
			return "", arguments
		}
		name, cleanedMap := ExtractCallGoal(parsed)
		encoded, err := json.Marshal(cleanedMap)
		if err != nil {
			return "", arguments
		}
		return name, string(encoded)

	default:
		return "", arguments
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// truncate 压成单行后按字符数截断，超长追加省略号。
//
// Python: _truncate(value, max_len)
func truncate(value string, maxLen int) string {
	oneLine := strings.Join(strings.Fields(value), " ")
	runes := []rune(oneLine)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "…"
	}
	return oneLine
}

// copyMap 浅拷贝 map，避免注入的 schema 被调用方修改后污染全局定义。
func copyMap(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/common/tool_display/ -v -cover
```

预期：全部 PASS，覆盖率 ≥85%

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/common/tool_display/
git commit -m "feat(tool_display): call_goal schema 注入与剥离（tool_display.py 87 行）

9.90d-1 B1：原计划表 0 章节的缺失件，9.90d-2 的 invocation_context /
readonly_tool_bindings 硬依赖。"
```

---

## Task 2：`command_runtime`（98 行）

**Files:**
- Create: `internal/swarm/agents/harness/common/tools/command_runtime.go`
- Test: `internal/swarm/agents/harness/common/tools/command_runtime_test.go`

**Python 对应：** `jiuwenswarm/agents/harness/common/tools/command_runtime.py`（98 行）

**关键差异**：Python 用 `ContextVar`，Go 用 `ctx` 显式传递（语言层差异，不登记偏差，属惯用法转换）。

- [ ] **Step 1: 写失败测试**

```go
package tools

import (
	"context"
	"path/filepath"
	"testing"
)

// TestCurrentCommandRuntimePaths 验证四个路径锚点均非空
func TestCurrentCommandRuntimePaths(t *testing.T) {
	ctx := context.Background()
	paths := CurrentCommandRuntimePaths(ctx, false)

	if paths.CurrentCwd == "" {
		t.Error("CurrentCwd should not be empty")
	}
	if paths.ProjectRoot == "" {
		t.Error("ProjectRoot should not be empty")
	}
	if paths.AgentWorkspaceRoot == "" {
		t.Error("AgentWorkspaceRoot should not be empty")
	}
}

// TestCurrentCommandRuntimePaths_requireRuntimeCwd 验证强制要求 cwd 时不回退
func TestCurrentCommandRuntimePaths_requireRuntimeCwd(t *testing.T) {
	ctx := context.Background()
	// 无 CwdState 绑定 → require=true 也没 cwd 可读，回退到 AgentWorkspace
	paths := CurrentCommandRuntimePaths(ctx, true)
	if paths.CurrentCwd == "" {
		t.Error("CurrentCwd should fall back, not be empty")
	}
}

// TestResolveCommandWorkdir_空workdir用cwd 验证空值取当前 cwd
func TestResolveCommandWorkdir_空workdir用cwd(t *testing.T) {
	dir := t.TempDir()
	paths := CommandRuntimePaths{
		CurrentCwd:         dir,
		ProjectRoot:        dir,
		WorkspaceRoot:      "",
		AgentWorkspaceRoot: dir,
	}

	got, err := ResolveCommandWorkdir("", paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != filepath.Clean(dir) {
		t.Errorf("got %q, want %q", got, filepath.Clean(dir))
	}
}

// TestResolveCommandWorkdir_相对路径基于cwd 验证相对路径拼接
func TestResolveCommandWorkdir_相对路径基于cwd(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	paths := CommandRuntimePaths{
		CurrentCwd:         dir,
		ProjectRoot:        dir,
		WorkspaceRoot:      "",
		AgentWorkspaceRoot: dir,
	}

	got, err := ResolveCommandWorkdir("sub", paths)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != filepath.Clean(sub) {
		t.Errorf("got %q, want %q", got, filepath.Clean(sub))
	}
}

// TestResolveCommandWorkdir_越界拒绝 验证三根之外的绝对路径被拒
func TestResolveCommandWorkdir_越界拒绝(t *testing.T) {
	dir := t.TempDir()
	paths := CommandRuntimePaths{
		CurrentCwd:         dir,
		ProjectRoot:        dir,
		WorkspaceRoot:      "",
		AgentWorkspaceRoot: dir,
	}

	_, err := ResolveCommandWorkdir("/etc/passwd", paths)
	if err == nil {
		t.Fatal("expected error for path outside workspace")
	}
	if err.Error() != "workdir is outside project workspace" {
		t.Errorf("error = %q, want 中文/原文对齐", err.Error())
	}
}

// TestResolveCommandWorkdir_非字符串拒绝 验证类型错误
func TestResolveCommandWorkdir_非字符串拒绝(t *testing.T) {
	dir := t.TempDir()
	paths := CommandRuntimePaths{
		CurrentCwd: dir, ProjectRoot: dir, AgentWorkspaceRoot: dir,
	}

	_, err := ResolveCommandWorkdir(42, paths)
	if err == nil {
		t.Fatal("expected error for non-string workdir")
	}
}

// TestResolveCommandWorkdir_workspaceRoot参与白名单 验证 workspace_root 也允许
func TestResolveCommandWorkdir_workspaceRoot参与白名单(t *testing.T) {
	projectDir := t.TempDir()
	wsDir := t.TempDir()
	paths := CommandRuntimePaths{
		CurrentCwd:         projectDir,
		ProjectRoot:        projectDir,
		WorkspaceRoot:      wsDir,
		AgentWorkspaceRoot: projectDir,
	}

	got, err := ResolveCommandWorkdir(wsDir, paths)
	if err != nil {
		t.Fatalf("workspace root should be allowed: %v", err)
	}
	if got != filepath.Clean(wsDir) {
		t.Errorf("got %q, want %q", got, filepath.Clean(wsDir))
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/tools/ -run TestCurrentCommandRuntimePaths -v
```

预期：`FAIL` — `undefined: CurrentCommandRuntimePaths`

- [ ] **Step 3: 写实现**

```go
package tools

// 命令工作目录的运行时契约。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/tools/command_runtime.py
//
// 语言差异：Python 用 ContextVar 隐式传递 CwdState，Go 用 context.Context
// 显式传递（sys_operation/cwd 包已按此设计），因此本文件所有函数首个参数
// 为 ctx。

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CommandRuntimePaths 命令执行使用的主机自有路径锚点。
//
// Python: @dataclass(frozen=True) CommandRuntimePaths
type CommandRuntimePaths struct {
	// CurrentCwd 当前工作目录
	CurrentCwd string
	// ProjectRoot 项目根目录
	ProjectRoot string
	// WorkspaceRoot 工作区根目录，空串表示未设置（Python None）
	WorkspaceRoot string
	// AgentWorkspaceRoot Agent 工作区根目录
	AgentWorkspaceRoot string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CurrentCommandRuntimePaths 读取当前命令路径上下文一次。
//
// requireRuntimeCwd 为 true 时，若读不到 cwd 则返回 error 而非回退。
//
// Python: current_command_runtime_paths(*, require_runtime_cwd=False)
func CurrentCommandRuntimePaths(
	ctx context.Context,
	requireRuntimeCwd bool,
) CommandRuntimePaths {
	currentCwd := currentCwdPath(ctx, requireRuntimeCwd)
	return CommandRuntimePaths{
		CurrentCwd:         currentCwd,
		ProjectRoot:        currentProjectRoot(ctx, currentCwd),
		WorkspaceRoot:      currentWorkspaceRoot(ctx),
		AgentWorkspaceRoot: agentWorkspaceDir(),
	}
}

// ResolveCommandWorkdir 按工具既有的根目录契约解析一个命令工作目录。
//
// 白名单三根：ProjectRoot / WorkspaceRoot（非空时）/ AgentWorkspaceRoot。
// 越界返回 error（文案对齐 Python）。
//
// Python: resolve_command_workdir(workdir, *, runtime_paths)
func ResolveCommandWorkdir(workdir any, paths CommandRuntimePaths) (string, error) {
	var candidate string

	switch v := workdir.(type) {
	case nil:
		candidate = paths.CurrentCwd
	case string:
		if v == "" {
			candidate = paths.CurrentCwd
		} else {
			candidate = v
		}
	default:
		return "", fmt.Errorf("workdir must be a string")
	}

	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(paths.CurrentCwd, candidate)
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("workdir is outside project workspace")
	}
	candidate = filepath.Clean(abs)

	allowedRoots := []string{paths.ProjectRoot}
	if paths.WorkspaceRoot != "" {
		allowedRoots = append(allowedRoots, paths.WorkspaceRoot)
	}
	allowedRoots = append(allowedRoots, paths.AgentWorkspaceRoot)

	for _, root := range allowedRoots {
		if root == "" {
			continue
		}
		if isRelativeTo(candidate, root) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("workdir is outside project workspace")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// currentCwdPath 读当前 cwd；读不到时按 requireRuntimeCwd 决定报错或回退。
//
// Python: _current_cwd(*, require_runtime_cwd)
func currentCwdPath(ctx context.Context, requireRuntimeCwd bool) string {
	if got := cwd.GetCwd(ctx); got != "" {
		return resolvePath(got)
	}
	if requireRuntimeCwd {
		return ""
	}
	return agentWorkspaceDir()
}

// currentProjectRoot 读项目根；读不到时回退到 cwd。
//
// Python: _current_project_root(current_cwd)
func currentProjectRoot(ctx context.Context, currentCwd string) string {
	if got := cwd.GetProjectRoot(ctx); got != "" {
		return resolvePath(got)
	}
	return currentCwd
}

// currentWorkspaceRoot 读工作区根；未设置返回空串。
//
// Python: _current_workspace_root()
func currentWorkspaceRoot(ctx context.Context) string {
	got := cwd.GetWorkspace(ctx)
	if got == "" {
		return ""
	}
	return resolvePath(got)
}

// agentWorkspaceDir 返回 Agent 工作区目录的绝对路径。
//
// Python: get_agent_workspace_dir().resolve()
func agentWorkspaceDir() string {
	// 复用 sys_operation/workspace 的既有实现；若尚未提供则回退到进程 cwd。
	return resolvePath(currentProcessCwd())
}

// isRelativeTo 判定 path 是否在 root 之下（含相等）。
//
// Python: _is_relative_to(path, root)
func isRelativeTo(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (!filepath.IsAbs(rel) && rel != ".." &&
		!startsWithDotDot(rel))
}

// startsWithDotDot 判定相对路径是否以 .. 开头。
func startsWithDotDot(rel string) bool {
	return len(rel) >= 2 && rel[0] == '.' && rel[1] == '.'
}
```

**实现注意**：`resolvePath` 与 `currentProcessCwd` 需按项目既有工具补齐——
```go
// resolvePath 展开并规整为绝对路径；失败时原样返回。
func resolvePath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return filepath.Clean(abs)
	}
	return p
}

// currentProcessCwd 返回进程当前工作目录。
func currentProcessCwd() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
```
需在文件 import 中加 `"os"`。

- [ ] **Step 4: 运行测试确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/tools/ -run "TestCurrentCommandRuntimePaths|TestResolveCommandWorkdir" -v -cover
```

预期：全部 PASS

- [ ] **Step 5: 更新 doc.go**

在 `internal/swarm/agents/harness/common/tools/doc.go` 的文件树中加：

```
//	├── command_runtime.go   # CommandRuntimePaths + 命令工作目录三根白名单解析
```

- [ ] **Step 6: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/swarm/agents/harness/common/tools/command_runtime.go \
        internal/swarm/agents/harness/common/tools/command_runtime_test.go \
        internal/swarm/agents/harness/common/tools/doc.go
git commit -m "feat(tools): 命令工作目录运行时契约（command_runtime.py 98 行）

9.90d-1 B5：原计划表 0 章节的缺失件，invocation_context 硬依赖。
ContextVar → ctx 显式传递（Go 惯用法，非偏差）。"
```

---

## Task 3：`sandbox_no_host_fallback`（38 行）

**Files:**
- Create: `internal/swarm/server/runtime/sandbox_no_host_fallback.go`
- Test: `internal/swarm/server/runtime/sandbox_no_host_fallback_test.go`

**Python 对应：** `jiuwenswarm/server/runtime/sandbox_no_host_fallback.py`（38 行）

**语义**：任务级执行域标记——标记后沙箱操作必须失败而非回退到宿主。Python 用 `ContextVar[bool]`，Go 用 `ctx` 值传递。

- [ ] **Step 1: 写失败测试**

```go
package runtime

import (
	"context"
	"testing"
)

// TestNoHostFallbackRequired_默认false 验证未标记时为 false
func TestNoHostFallbackRequired_默认false(t *testing.T) {
	ctx := context.Background()
	if NoHostFallbackRequired(ctx) {
		t.Error("default should be false")
	}
}

// TestRequireNoHostFallback_标记后为true 验证标记生效
func TestRequireNoHostFallback_标记后为true(t *testing.T) {
	ctx := RequireNoHostFallback(context.Background())
	if !NoHostFallbackRequired(ctx) {
		t.Error("should be true after RequireNoHostFallback")
	}
}

// TestClearNoHostFallback_清除后为false 验证清除生效
func TestClearNoHostFallback_清除后为false(t *testing.T) {
	ctx := RequireNoHostFallback(context.Background())
	ctx = ClearNoHostFallback(ctx)
	if NoHostFallbackRequired(ctx) {
		t.Error("should be false after ClearNoHostFallback")
	}
}

// TestNoHostFallback_不污染父ctx 验证 ctx 派生隔离
func TestNoHostFallback_不污染父ctx(t *testing.T) {
	parent := context.Background()
	child := RequireNoHostFallback(parent)

	if NoHostFallbackRequired(parent) {
		t.Error("parent context should not be affected")
	}
	if !NoHostFallbackRequired(child) {
		t.Error("child context should be marked")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/runtime/ -run TestNoHostFallback -v
```

预期：`FAIL` — `undefined: NoHostFallbackRequired`

- [ ] **Step 3: 写实现**

```go
package runtime

// 任务级执行域：禁止沙箱到宿主的回退。
//
// 对应 Python 代码：jiuwenswarm/server/runtime/sandbox_no_host_fallback.py
//
// 语言差异：Python 用 ContextVar[bool] 隐式传递，Go 用 context.Context
// 派生值显式传递（与 sys_operation/cwd 同款设计）。
// 因此 Python 的三个函数在 Go 侧分别是读值函数与返回新 ctx 的派生函数。

import "context"

// ──────────────────────────── 常量 ────────────────────────────

// noHostFallbackKey 标记值的 context key 类型（避免与其他包冲突）。
type noHostFallbackKey struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NoHostFallbackRequired 返回当前沙箱操作是否必须留在非宿主域。
//
// Python: no_host_fallback_required() -> bool
func NoHostFallbackRequired(ctx context.Context) bool {
	v, ok := ctx.Value(noHostFallbackKey{}).(bool)
	return ok && v
}

// RequireNoHostFallback 要求下一次沙箱操作失败而非回退到宿主，
// 返回派生后的 ctx。
//
// Python: require_no_host_fallback() -> None（设置 ContextVar）
func RequireNoHostFallback(ctx context.Context) context.Context {
	return context.WithValue(ctx, noHostFallbackKey{}, true)
}

// ClearNoHostFallback 在终态操作后清除任务级执行域，返回派生后的 ctx。
//
// Python: clear_no_host_fallback() -> None（清除 ContextVar）
func ClearNoHostFallback(ctx context.Context) context.Context {
	return context.WithValue(ctx, noHostFallbackKey{}, false)
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/runtime/ -run TestNoHostFallback -v -cover
```

预期：全部 PASS

- [ ] **Step 5: 更新 doc.go 并提交**

在 `internal/swarm/server/runtime/doc.go` 文件树加：
```
//	├── sandbox_no_host_fallback.go   # 沙箱禁宿主回退的任务级执行域标记
```

```bash
cd /home/opensource/uapclaw-gateway
git add internal/swarm/server/runtime/sandbox_no_host_fallback.go \
        internal/swarm/server/runtime/sandbox_no_host_fallback_test.go \
        internal/swarm/server/runtime/doc.go
git commit -m "feat(runtime): 沙箱禁宿主回退标记（sandbox_no_host_fallback.py 38 行）

9.90d-1 B3：原计划表 0 章节。ContextVar → ctx 派生（Go 惯用法）。
before_tool / artifact_tracking_capture 硬依赖。"
```

---

## Task 4：`run_permissions`（54 行）

**Files:**
- Create: `internal/swarm/runtime/run_permissions.go`
- Test: `internal/swarm/runtime/run_permissions_test.go`

**Python 对应：** `jiuwenswarm/runtime/run_permissions.py`（54 行）

**依赖**：`normalize_permission_tool_name`（Go 已有 `internal/common/permission_tools`）

- [ ] **Step 1: 核实 Go 侧 `NormalizePermissionToolName` 签名**

```bash
grep -n "func NormalizePermissionToolName" /home/opensource/uapclaw-gateway/internal/common/permission_tools/permission_tools.go
```

预期：`func NormalizePermissionToolName(name string) string`

- [ ] **Step 2: 写失败测试**

```go
package runtime

import (
	"context"
	"reflect"
	"testing"
)

// TestOverlayRunPermissions_无覆盖返回原config 验证空 levels 时原样返回
func TestOverlayRunPermissions_无覆盖返回原config(t *testing.T) {
	cfg := map[string]any{"tools": map[string]any{"bash": "ask"}}
	got := OverlayRunPermissions(context.Background(), cfg)

	if !reflect.DeepEqual(got, cfg) {
		t.Errorf("got %v, want %v", got, cfg)
	}
}

// TestOverlayRunPermissions_覆盖为allow 验证请求级放行
func TestOverlayRunPermissions_覆盖为allow(t *testing.T) {
	ctx := WithRunPermissions(context.Background(), map[string]string{"bash": "allow"})
	cfg := map[string]any{"tools": map[string]any{"bash": "ask"}}

	got := OverlayRunPermissions(ctx, cfg)

	tools := got["tools"].(map[string]any)
	if tools["bash"] != "allow" {
		t.Errorf("bash = %v, want allow", tools["bash"])
	}
	allowTools := got["allow_tools"].([]string)
	if len(allowTools) != 1 || allowTools[0] != "bash" {
		t.Errorf("allow_tools = %v", allowTools)
	}
}

// TestOverlayRunPermissions_deny不可被放宽 验证整工具 deny 优先
func TestOverlayRunPermissions_deny不可被放宽(t *testing.T) {
	ctx := WithRunPermissions(context.Background(), map[string]string{"rm": "allow"})
	cfg := map[string]any{
		"tools":      map[string]any{"rm": "deny"},
		"deny_tools": []string{"rm"},
	}

	got := OverlayRunPermissions(ctx, cfg)

	tools := got["tools"].(map[string]any)
	if tools["rm"] != "deny" {
		t.Errorf("rm = %v, want deny (deny 不可放宽)", tools["rm"])
	}
}

// TestOverlayRunPermissions_切换level清理旧桶 验证 allow→deny 时从 allow_tools 移除
func TestOverlayRunPermissions_切换level清理旧桶(t *testing.T) {
	ctx := WithRunPermissions(context.Background(), map[string]string{"edit": "deny"})
	cfg := map[string]any{
		"tools":       map[string]any{"edit": "allow"},
		"allow_tools": []string{"edit"},
	}

	got := OverlayRunPermissions(ctx, cfg)

	if allowTools, _ := got["allow_tools"].([]string); len(allowTools) != 0 {
		t.Errorf("allow_tools should be empty, got %v", allowTools)
	}
	denyTools := got["deny_tools"].([]string)
	if len(denyTools) != 1 || denyTools[0] != "edit" {
		t.Errorf("deny_tools = %v, want [edit]", denyTools)
	}
}

// TestOverlayRunPermissions_不修改原config 验证深拷贝隔离
func TestOverlayRunPermissions_不修改原config(t *testing.T) {
	ctx := WithRunPermissions(context.Background(), map[string]string{"bash": "deny"})
	cfg := map[string]any{"tools": map[string]any{"bash": "ask"}}

	_ = OverlayRunPermissions(ctx, cfg)

	tools := cfg["tools"].(map[string]any)
	if tools["bash"] != "ask" {
		t.Errorf("original config mutated: %v", tools["bash"])
	}
}
```

- [ ] **Step 3: 运行测试确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/runtime/ -run TestOverlayRunPermissions -v
```

预期：`FAIL` — `undefined: OverlayRunPermissions`

- [ ] **Step 4: 写实现**

```go
package runtime

// 请求级权限覆盖层，叠加在已安装的 Host 策略之上。
//
// 对应 Python 代码：jiuwenswarm/runtime/run_permissions.py
//
// 语义：请求级选择只能「覆盖工具级 level」，不能放宽整工具 deny；
// 所有已安装的安全护栏（file_guard / net_guard / shell_guard）原样保留。
//
// 语言差异：Python 用 ContextVar 存 RUN_PERMISSIONS，Go 用 ctx 派生值。

import (
	"context"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/common/permission_tools"
)

// ──────────────────────────── 常量 ────────────────────────────

// runPermissionsKey 请求级权限覆盖的 context key 类型。
type runPermissionsKey struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// WithRunPermissions 把请求级工具权限选择绑定到 ctx。
//
// Python: RUN_PERMISSIONS.set(levels)
func WithRunPermissions(ctx context.Context, levels map[string]string) context.Context {
	if levels == nil {
		return context.WithValue(ctx, runPermissionsKey{}, map[string]string(nil))
	}
	copied := make(map[string]string, len(levels))
	for k, v := range levels {
		copied[k] = v
	}
	return context.WithValue(ctx, runPermissionsKey{}, copied)
}

// OverlayRunPermissions 应用逐工具选择，同时保留所有已安装的安全护栏。
//
// 规则：
//  1. 整工具 deny（tools[name]=="deny" 或 name 在 deny_tools 中）不可被放宽
//  2. 覆盖 level 时同时清理旧的 ask/allow/deny 桶，再加入新桶
//  3. 三个桶输出时排序（对齐 Python sorted()）
//
// Python: overlay_run_permissions(config) -> dict
func OverlayRunPermissions(ctx context.Context, config map[string]any) map[string]any {
	levels, _ := ctx.Value(runPermissionsKey{}).(map[string]string)
	if len(levels) == 0 {
		return config
	}

	result := deepCopyMap(config)

	toolsRaw, _ := result["tools"].(map[string]any)
	tools := make(map[string]any, len(toolsRaw))
	for k, v := range toolsRaw {
		tools[k] = v
	}

	denied := toStringSet(result["deny_tools"])
	asked := toStringSet(result["ask_tools"])
	allowed := toStringSet(result["allow_tools"])

	for name, level := range levels {
		// 整工具 deny 不可被放宽
		if denied[name] {
			continue
		}
		if tv, ok := tools[name]; ok && tv == "deny" {
			continue
		}
		tools[name] = level
		delete(denied, name)
		delete(asked, name)
		delete(allowed, name)
		switch level {
		case "deny":
			denied[name] = true
		case "ask":
			asked[name] = true
		case "allow":
			allowed[name] = true
		}
	}

	result["tools"] = tools
	result["deny_tools"] = sortedKeys(denied)
	result["ask_tools"] = sortedKeys(asked)
	result["allow_tools"] = sortedKeys(allowed)
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// deepCopyMap 深拷贝 map[string]any（嵌套 map 递归拷贝）。
func deepCopyMap(src map[string]any) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		switch nested := v.(type) {
		case map[string]any:
			dst[k] = deepCopyMap(nested)
		case []string:
			cp := make([]string, len(nested))
			copy(cp, nested)
			dst[k] = cp
		case []any:
			cp := make([]any, len(nested))
			copy(cp, nested)
			dst[k] = cp
		default:
			dst[k] = v
		}
	}
	return dst
}

// toStringSet 把 []any / []string / nil 归一为 set。
func toStringSet(value any) map[string]bool {
	out := make(map[string]bool)
	switch v := value.(type) {
	case []string:
		for _, s := range v {
			out[s] = true
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				out[s] = true
			}
		}
	}
	return out
}

// sortedKeys 返回排序后的键切片（对齐 Python sorted() 输出）。
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// normalizeToolName 归一化工具名（供后续 Task 使用，本文件暂不调用）。
//
// Python: normalize_permission_tool_name(name)
func normalizeToolName(name string) string {
	return permission_tools.NormalizePermissionToolName(name)
}
```

- [ ] **Step 5: 运行测试确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/runtime/ -run TestOverlayRunPermissions -v -cover
```

预期：全部 PASS

- [ ] **Step 6: 更新 doc.go 并提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/swarm/runtime/run_permissions.go \
        internal/swarm/runtime/run_permissions_test.go \
        internal/swarm/runtime/doc.go
git commit -m "feat(runtime): 请求级权限覆盖层（run_permissions.py 54 行）

9.90d-1 B2：原计划表 0 章节。deny 不可放宽 + 三桶重排序。
policy_eval / before_tool 硬依赖。"
```

---

## Task 5：`workspace_paths`（518 行，本批最大的跨包件）

**Files:**
- Create: `internal/swarm/agents/harness/common/workspace_paths.go`
- Test: `internal/swarm/agents/harness/common/workspace_paths_test.go`

**Python 对应：** `jiuwenswarm/agents/harness/common/workspace_paths.py`（518 行 / 22 函数）

**9.90d-2 只用 `sanitize_review_ui_value` 一个入口**，但按规则 9 必须全量落地（不拆文件）。

### 5.1 函数清单（22 个）

| Python | 行 | Go |
|---|---|---|
| `WorkspacePathResolution` | 65 | `WorkspacePathResolution` struct |
| `normalize_workspace_root` | 75 | `NormalizeWorkspaceRoot` |
| `sandbox_artifact_session_root` | 81 | `SandboxArtifactSessionRoot` |
| `is_sandbox_artifact_workspace_root` | 90 | `IsSandboxArtifactWorkspaceRoot` |
| `resolve_workspace_path` | 95 | `ResolveWorkspacePath` |
| `resolve_workspace_path_details` | 148 | `ResolveWorkspacePathDetails` |
| `display_workspace_path` | 183 | `DisplayWorkspacePath` |
| `sanitize_visible_text` | 222 | `SanitizeVisibleText` |
| `sanitize_visible_value` | 246 | `SanitizeVisibleValue` |
| `sanitize_review_ui_value` | 300 | `SanitizeReviewUIValue` ← **9.90d-2 入口** |
| `resolve_workspace_path_arguments` | 314 | `ResolveWorkspacePathArguments` |
| `_resolve_workspace_path_arguments` | 326 | `resolveWorkspacePathArguments` |
| `_normalize_path_text` | 367 | `normalizePathText` |
| `_is_relative_to` | 374 | `isRelativeTo` |
| `_sandbox_artifact_replacement` | 382 | `sandboxArtifactReplacement` |
| `_sanitize_review_ui_text` | 388 | `sanitizeReviewUIText` |
| `_relativize_workspace_root_prefixes` | 410 | `relativizeWorkspaceRootPrefixes` |
| `_workspace_root_traversal_starts` | 447 | `workspaceRootTraversalStarts` |
| `_path_from_root_has_parent_segment` | 468 | `pathFromRootHasParentSegment` |
| `_is_local_path_prefix_character` | 488 | `isLocalPathPrefixCharacter` |
| `_review_ui_path_replacement` | 492 | `reviewUIPathReplacement` |
| `_redact_review_ui_secrets` | 513 | `redactReviewUISecrets` |

### 5.2 实施步骤

- [ ] **Step 1: 读完整 Python 源码并逐函数记录**

```bash
sed -n '1,520p' /home/opensource/jiuwenswarm-develop/jiuwenswarm/agents/harness/common/workspace_paths.py
```

**操作要求**：把 518 行全文读完（不是只读前 120 行）。本文件 22 个函数中 13 个是私有函数且互相调用，漏一个就编译不过。按 §5.1 表格逐个转成 Go，每转一个就用 `grep -c "^func "` 核对计数是否 +1。

- [ ] **Step 2: 写测试（覆盖 22 个函数，每组断言 ≥2）**

```go
package common

import (
	"path/filepath"
	"testing"
)

// TestNormalizeWorkspaceRoot 验证归一化与 nil 处理
func TestNormalizeWorkspaceRoot(t *testing.T) {
	if got := NormalizeWorkspaceRoot(nil); got != nil {
		t.Errorf("nil should return nil, got %v", got)
	}
	dir := t.TempDir()
	got := NormalizeWorkspaceRoot(dir)
	if got == nil || *got != filepath.Clean(dir) {
		t.Errorf("got %v, want %v", got, filepath.Clean(dir))
	}
	// 相对路径转绝对
	got = NormalizeWorkspaceRoot(".")
	if got == nil || !filepath.IsAbs(*got) {
		t.Errorf("relative should become absolute, got %v", got)
	}
}

// TestSanitizeReviewUIValue_相对化工作区路径 验证 9.90d-2 主入口
func TestSanitizeReviewUIValue_相对化工作区路径(t *testing.T) {
	root := "/workspace/proj"
	value := map[string]any{"path": root + "/src/main.go"}

	got := SanitizeReviewUIValue(value, root, 60000)

	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T, want map", got)
	}
	// 工作区根前缀应被相对化
	if m["path"] == root+"/src/main.go" {
		t.Errorf("workspace root prefix should be relativized, got %v", m["path"])
	}
}

// TestSanitizeReviewUIValue_脱敏密钥 验证 secret 被替换
func TestSanitizeReviewUIValue_脱敏密钥(t *testing.T) {
	root := "/workspace"
	value := map[string]any{
		"api_key": "sk-1234567890abcdef",
		"path":    "/workspace/x",
	}

	got := SanitizeReviewUIValue(value, root, 60000).(map[string]any)

	if got["api_key"] == "sk-1234567890abcdef" {
		t.Error("api_key should be redacted")
	}
}

// TestSanitizeReviewUIValue_超长截断 验证 maxLength 生效
func TestSanitizeReviewUIValue_超长截断(t *testing.T) {
	long := ""
	for i := 0; i < 1000; i++ {
		long += "a"
	}
	got := SanitizeReviewUIValue(long, "/workspace", 100)

	s, ok := got.(string)
	if !ok {
		t.Fatalf("got %T, want string", got)
	}
	if len(s) > 200 {
		t.Errorf("should be truncated, len=%d", len(s))
	}
}

// TestResolveWorkspacePath 验证相对/绝对路径解析
func TestResolveWorkspacePath(t *testing.T) {
	root := "/workspace"
	if got := ResolveWorkspacePath("src/a.go", root); got != "/workspace/src/a.go" {
		t.Errorf("got %q", got)
	}
	if got := ResolveWorkspacePath("/abs/b.go", root); got != "/abs/b.go" {
		t.Errorf("absolute should stay, got %q", got)
	}
}

// TestDisplayWorkspacePath 验证展示路径相对化
func TestDisplayWorkspacePath(t *testing.T) {
	root := "/workspace"
	got := DisplayWorkspacePath("/workspace/src/a.go", root)
	if got == "/workspace/src/a.go" {
		t.Errorf("should be relativized, got %q", got)
	}
}

// TestIsSandboxArtifactWorkspaceRoot 验证沙箱产物根判定
func TestIsSandboxArtifactWorkspaceRoot(t *testing.T) {
	if !IsSandboxArtifactWorkspaceRoot("/sandbox/artifacts/session") {
		t.Error("should detect sandbox artifact root")
	}
	if IsSandboxArtifactWorkspaceRoot("/workspace/project") {
		t.Error("normal workspace should not match")
	}
}
```

**注意**：上面 7 个测试只覆盖主干。实施时须为 §5.1 的每个导出函数各写至少 1 个用例，私有函数通过导出函数间接覆盖。

- [ ] **Step 3: 写实现（按 §5.1 逐函数）**

包声明与文件头：

```go
package common

// 工作区路径解析、展示相对化与 UI 脱敏。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/workspace_paths.py
//
// 本文件被 reviewer_metadata 用于审查器 UI 脱敏（sanitize_review_ui_value），
// 也被 project_memory_rail 等多处用于路径展示。

import (
	"path/filepath"
	"regexp"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorkspacePathResolution 工作区路径解析结果。
//
// Python: @dataclass WorkspacePathResolution
type WorkspacePathResolution struct {
	// Resolved 解析后的绝对路径
	Resolved string
	// Display 展示用（相对化后）路径
	Display string
	// InsideWorkspace 是否在工作区内
	InsideWorkspace bool
	// SandboxArtifact 是否属沙箱产物路径
	SandboxArtifact bool
}
```

**其余 21 个函数逐个从 Python 复制转换**，命名按 §5.1 表格，注释标 `// Python: xxx(...)`。

**逐函数实施清单**（每转完一个立刻在括号里打勾，避免漏转）：

| # | Go 函数 | 已转 |
|---|---|---|
| 1 | `NormalizeWorkspaceRoot` | ☐ |
| 2 | `SandboxArtifactSessionRoot` | ☐ |
| 3 | `IsSandboxArtifactWorkspaceRoot` | ☐ |
| 4 | `ResolveWorkspacePath` | ☐ |
| 5 | `ResolveWorkspacePathDetails` | ☐ |
| 6 | `DisplayWorkspacePath` | ☐ |
| 7 | `SanitizeVisibleText` | ☐ |
| 8 | `SanitizeVisibleValue` | ☐ |
| 9 | `SanitizeReviewUIValue`（**9.90d-2 主入口**） | ☐ |
| 10 | `ResolveWorkspacePathArguments` | ☐ |
| 11 | `resolveWorkspacePathArguments` | ☐ |
| 12 | `normalizePathText` | ☐ |
| 13 | `isRelativeTo` | ☐ |
| 14 | `sandboxArtifactReplacement` | ☐ |
| 15 | `sanitizeReviewUIText` | ☐ |
| 16 | `relativizeWorkspaceRootPrefixes` | ☐ |
| 17 | `workspaceRootTraversalStarts` | ☐ |
| 18 | `pathFromRootHasParentSegment` | ☐ |
| 19 | `isLocalPathPrefixCharacter` | ☐ |
| 20 | `reviewUIPathReplacement` | ☐ |
| 21 | `redactReviewUISecrets` | ☐ |

**自检**：转完后 `grep -c "^func " workspace_paths.go` 应为 22（1 个 struct 不算）。

- [ ] **Step 4: 运行测试**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/ -run "TestNormalizeWorkspaceRoot|TestSanitizeReviewUIValue|TestResolveWorkspacePath|TestDisplayWorkspacePath|TestIsSandboxArtifact" -v -cover
```

预期：全部 PASS，包覆盖率 ≥85%

- [ ] **Step 5: 更新 doc.go 并提交**

在 `internal/swarm/agents/harness/common/doc.go` 加：
```
//	├── workspace_paths.go   # 工作区路径解析与 UI 脱敏（22 函数）
```

```bash
cd /home/opensource/uapclaw-gateway
git add internal/swarm/agents/harness/common/workspace_paths.go \
        internal/swarm/agents/harness/common/workspace_paths_test.go \
        internal/swarm/agents/harness/common/doc.go
git commit -m "feat(common): 工作区路径解析与 UI 脱敏（workspace_paths.py 518 行全量）

9.90d-1 B4：原计划表 0 章节。全量 22 函数，不拆文件（规则 9）。
reviewer_metadata 的 sanitize_review_ui_value 是 9.90d-2 主入口。"
```

---

## Task 6：`pdf_tools`（275 行）

**Files:**
- Create: `internal/swarm/agents/harness/common/tools/pdf_tools.go`
- Test: `internal/swarm/agents/harness/common/tools/pdf_tools_test.go`

**Python 对应：** `jiuwenswarm/agents/harness/common/tools/pdf_tools.py`（275 行）

**说明**：9.90d 只需 `read_pdf` 提供「原生路径执行器」身份事实，但按已拍板决策**全量 275 行落地**。

- [ ] **Step 1: 读 Python 源码，确认函数清单**

```bash
grep -n "^def \|^class " /home/opensource/jiuwenswarm-develop/jiuwenswarm/agents/harness/common/tools/pdf_tools.py
```

把结果写入本节（Task 0 Step 3 已核实）。

- [ ] **Step 2: 写测试**

```go
package tools

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResolvePDFPath_存在文件 验证正常路径返回
func TestResolvePDFPath_存在文件(t *testing.T) {
	dir := t.TempDir()
	pdf := filepath.Join(dir, "a.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ResolvePDFPath(pdf, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != filepath.Clean(pdf) {
		t.Errorf("got %q, want %q", got, filepath.Clean(pdf))
	}
}

// TestResolvePDFPath_不存在文件 验证报错
func TestResolvePDFPath_不存在文件(t *testing.T) {
	dir := t.TempDir()
	_, err := ResolvePDFPath(filepath.Join(dir, "missing.pdf"), dir)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestResolvePDFPath_非pdf扩展名 验证扩展名校验
func TestResolvePDFPath_非pdf扩展名(t *testing.T) {
	dir := t.TempDir()
	txt := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(txt, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ResolvePDFPath(txt, dir)
	if err == nil {
		t.Fatal("expected error for non-pdf extension")
	}
}

// TestReadPDF_无效PDF 验证解析失败报错
func TestReadPDF_无效PDF(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.pdf")
	if err := os.WriteFile(bad, []byte("not a real pdf"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ReadPDF(bad, dir, nil)
	if err == nil {
		t.Fatal("expected error for invalid PDF")
	}
}

// TestReadPDF_页范围限制 验证 pages 参数生效
func TestReadPDF_页范围限制(t *testing.T) {
	// 需真实 PDF fixture；若项目无 fixture，用 t.Skip 并在 Task 6 Step 5 补
	t.Skip("需要真实 PDF fixture，见 Task 6 Step 5")
}
```

- [ ] **Step 3: 写实现**

按 Python 逐函数对齐。**关键**：`ReadPDF` 是工具执行器（供 `_NATIVE_PATH_EXECUTORS` 识别），签名须与既有工具一致。

PDF 解析库选型需在实施时确认——若项目已有 PDF 依赖则复用，否则按规则 3.2 用 `//go:build` 隔离真实 PDF 解析测试。

- [ ] **Step 4: 运行测试**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/tools/ -run "TestResolvePDFPath|TestReadPDF" -v
```

预期：PASS（`TestReadPDF_页范围限制` 为 Skip）

- [ ] **Step 5: 补 PDF fixture**

若项目无 PDF 测试文件，在 `internal/swarm/agents/harness/common/tools/testdata/` 下放一个最小 PDF：

```bash
mkdir -p internal/swarm/agents/harness/common/tools/testdata
printf '%%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]>>endobj\ntrailer<</Root 1 0 R>>\n%%%%EOF\n' > internal/swarm/agents/harness/common/tools/testdata/minimal.pdf
```

然后把 `TestReadPDF_页范围限制` 的 `Skip` 换成真实断言。

- [ ] **Step 6: 更新 doc.go 并提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/swarm/agents/harness/common/tools/pdf_tools.go \
        internal/swarm/agents/harness/common/tools/pdf_tools_test.go \
        internal/swarm/agents/harness/common/tools/testdata/ \
        internal/swarm/agents/harness/common/tools/doc.go
git commit -m "feat(tools): PDF 路径解析与读取（pdf_tools.py 275 行全量）

9.90d-1 B6：原计划表 0 章节。全量落地含 read_pdf，
供 invocation_context 的 _NATIVE_PATH_EXECUTORS 识别为原生路径执行器。"
```

---

（计划续下页：Task 7-17 见第二部分）

---

## Task 7：`persistent_audit`（296 行）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/persistent_audit.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/persistent_audit_test.go`

**Python 对应：** `permissions/persistent_audit.py`（296 行）

### 结构（10 项）

| Python | 行 | Go |
|---|---|---|
| `PersistentAuditWriteResult` | 71 | `PersistentAuditWriteResult` struct |
| `PersistentAuditWriter` | 80 | `PersistentAuditWriter` struct + `AuditPath()` + `Write()` + `buildRecord()` |
| `resolve_persistent_audit_root` | 180 | `ResolvePersistentAuditRoot` |
| `build_sanitized_audit_fields` | 198 | `BuildSanitizedAuditFields` |
| `sanitize_audit_value` | 239 | `SanitizeAuditValue` |
| `sanitize_audit_field` | 255 | `SanitizeAuditField` |
| `_sanitize_audit_text` | 262 | `sanitizeAuditText` |
| `_sanitize_natural_language_audit_text` | 273 | `sanitizeNaturalLanguageAuditText` |
| `_audit_text_contains_sensitive_content` | 289 | `auditTextContainsSensitiveContent` |

常量 8 个：`AuditSchemaVersion` / `AuditSubdirectory` / `AuditFilename` / `AuditDigestAlgorithm` / `DataDirEnvName` / `MaxAuditTextLength` / `AuditTextRedacted` / `ExtraAllowlist`。

- [ ] **Step 1: 写失败测试**

```go
package permissions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeFacts 构造最小 ToolDecisionFacts 供审计字段测试用
func fakeFacts() ToolDecisionFacts {
	return ToolDecisionFacts{
		ToolName:     "bash",
		ToolCategory: "command",
		AccessesKnown: true,
	}
}

// TestBuildSanitizedAuditFields_基础字段 验证必填字段齐全
func TestBuildSanitizedAuditFields_基础字段(t *testing.T) {
	got := BuildSanitizedAuditFields(fakeFacts(), "allow", "policy_allow", false, "", "", nil)

	for _, key := range []string{"decision", "degraded", "accesses_known", "path_counts",
		"reason", "risk_tier", "side_effects", "tool_category", "tool_name"} {
		if _, exists := got[key]; !exists {
			t.Errorf("missing required field %q", key)
		}
	}
	if got["tool_name"] != "bash" {
		t.Errorf("tool_name = %v", got["tool_name"])
	}
	if got["decision"] != "allow" {
		t.Errorf("decision = %v", got["decision"])
	}
}

// TestBuildSanitizedAuditFields_可选字段 验证 grant_id/grant_reason 空时不出现
func TestBuildSanitizedAuditFields_可选字段(t *testing.T) {
	got := BuildSanitizedAuditFields(fakeFacts(), "allow", "r", false, "", "", nil)
	if _, exists := got["grant_id"]; exists {
		t.Error("grant_id should be omitted when empty")
	}

	got = BuildSanitizedAuditFields(fakeFacts(), "allow", "r", false, "g1", "gr1", nil)
	if got["grant_id"] != "g1" {
		t.Errorf("grant_id = %v", got["grant_id"])
	}
	if got["grant_reason"] != "gr1" {
		t.Errorf("grant_reason = %v", got["grant_reason"])
	}
}

// TestBuildSanitizedAuditFields_extra白名单 验证只保留白名单键
func TestBuildSanitizedAuditFields_extra白名单(t *testing.T) {
	extra := map[string]any{
		"decision_source": "policy",   // 白名单内
		"secret_field":    "should_be_dropped", // 白名单外
	}
	got := BuildSanitizedAuditFields(fakeFacts(), "allow", "r", false, "", "", extra)

	if got["decision_source"] != "policy" {
		t.Errorf("allowlisted key丢失")
	}
	if _, exists := got["secret_field"]; exists {
		t.Error("non-allowlisted key should be dropped")
	}
}

// TestSanitizeAuditText_绝对路径被脱敏 验证路径检测
func TestSanitizeAuditText_绝对路径被脱敏(t *testing.T) {
	got := sanitizeAuditText("/etc/passwd")
	if got != AuditTextRedacted {
		t.Errorf("got %q, want [redacted]", got)
	}
}

// TestSanitizeAuditText_密钥被脱敏 验证 secret 检测
func TestSanitizeAuditText_密钥被脱敏(t *testing.T) {
	for _, s := range []string{
		"sk-1234567890abcdef",
		"api_key=secret123",
		"Bearer abcdefghijklmnop",
	} {
		if sanitizeAuditText(s) != AuditTextRedacted {
			t.Errorf("%q should be redacted", s)
		}
	}
}

// TestSanitizeAuditText_安全文本保留 验证正常标识符通过
func TestSanitizeAuditText_安全文本保留(t *testing.T) {
	cases := []string{"policy_allow", "semantic_reviewer", "tool:bash", "auto_permission"}
	for _, s := range cases {
		if got := sanitizeAuditText(s); got != s {
			t.Errorf("%q got %q, want unchanged", s, got)
		}
	}
}

// TestSanitizeAuditText_超长截断 验证 160 字符上限
func TestSanitizeAuditText_超长截断(t *testing.T) {
	long := "a" + strings.Repeat("b", 300)
	got := sanitizeAuditText(long)
	if got == AuditTextRedacted {
		t.Skip("长文本按正则不匹配而脱敏，非截断路径")
	}
	if len(got) > MaxAuditTextLength {
		t.Errorf("len = %d, want <= %d", len(got), MaxAuditTextLength)
	}
}

// TestResolvePersistentAuditRoot_配置优先 验证 config 中路径生效
func TestResolvePersistentAuditRoot_配置优先(t *testing.T) {
	cfg := map[string]any{"persistent_audit_root": "/tmp/audit"}
	got := ResolvePersistentAuditRoot(cfg)
	if got == nil || !strings.HasSuffix(*got, "audit") {
		t.Errorf("got %v", got)
	}
}

// TestResolvePersistentAuditRoot_auto子配置 验证 auto.persistent_audit_root
func TestResolvePersistentAuditRoot_auto子配置(t *testing.T) {
	cfg := map[string]any{"auto": map[string]any{"audit_root": "/tmp/a2"}}
	got := ResolvePersistentAuditRoot(cfg)
	if got == nil {
		t.Fatal("should resolve from auto.audit_root")
	}
}

// TestResolvePersistentAuditRoot_环境变量回退 验证 env 生效
func TestResolvePersistentAuditRoot_环境变量回退(t *testing.T) {
	t.Setenv(DataDirEnvName, "/tmp/envdata")
	got := ResolvePersistentAuditRoot(nil)
	if got == nil || !strings.Contains(*got, "envdata") {
		t.Errorf("got %v", got)
	}
}

// TestResolvePersistentAuditRoot_无来源返回nil 验证三处都没有时 nil
func TestResolvePersistentAuditRoot_无来源返回nil(t *testing.T) {
	t.Setenv(DataDirEnvName, "")
	if got := ResolvePersistentAuditRoot(nil); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// TestPersistentAuditWriter_写入JSONL 验证落盘格式与字段
func TestPersistentAuditWriter_写入JSONL(t *testing.T) {
	dir := t.TempDir()
	w := NewPersistentAuditWriter(dir)

	result := w.Write(fakeFacts(), "allow", "policy_allow", false, "", "", nil)
	if !result.Persisted {
		t.Fatalf("should persist: %+v", result)
	}

	path := filepath.Join(dir, AuditSubdirectory, AuditFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}

	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("invalid JSONL: %v", err)
	}
	if record["schema_version"] != AuditSchemaVersion {
		t.Errorf("schema_version = %v", record["schema_version"])
	}
	if _, exists := record["timestamp"]; !exists {
		t.Error("timestamp missing")
	}
}

// TestPersistentAuditWriter_无root降级 验证 data_root=nil 时降级
func TestPersistentAuditWriter_无root降级(t *testing.T) {
	w := NewPersistentAuditWriter(nil)
	result := w.Write(fakeFacts(), "allow", "r", false, "", "", nil)

	if result.Persisted {
		t.Error("should not persist without root")
	}
	if !result.Degraded {
		t.Error("should be degraded")
	}
	if result.Reason != "audit_root_unavailable" {
		t.Errorf("reason = %q", result.Reason)
	}
}

// TestPersistentAuditWriter_追加不覆盖 验证 append 语义
func TestPersistentAuditWriter_追加不覆盖(t *testing.T) {
	dir := t.TempDir()
	w := NewPersistentAuditWriter(dir)

	w.Write(fakeFacts(), "allow", "r1", false, "", "", nil)
	w.Write(fakeFacts(), "deny", "r2", false, "", "", nil)

	data, _ := os.ReadFile(filepath.Join(dir, AuditSubdirectory, AuditFilename))
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Errorf("got %d lines, want 2", len(lines))
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run "TestBuildSanitizedAuditFields|TestSanitizeAuditText|TestResolvePersistentAuditRoot|TestPersistentAuditWriter" -v
```

预期：`FAIL` — `undefined: BuildSanitizedAuditFields`

- [ ] **Step 3: 写实现**

按 Python 逐函数转换。**关键点**：

1. **正则必须用 RE2 语法**（Go `regexp` 不支持 lookbehind）。Python 的 `(?<!\w)/(?:[^\s'\"\\]+/?)+` 需改写为能表达同语义的写法，或在注释中说明用前置字符检查替代。
2. **`AUDIT_SCHEMA_VERSION` 等 8 个常量**全大驼峰导出（`AuditSchemaVersion` 等）。
3. **`ExtraAllowlist`** 用 `map[string]bool`（Python frozenset）。
4. **写盘**：`os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)` + `json.Marshal` + `"\n"`。
5. **`buildRecord`** 里 `timestamp` 用 `float64(time.Now().UnixNano()) / 1e9`（对齐 Python `time.time()` 的秒级浮点）。

#### Python 正则 → Go RE2 改写对照表（必须逐个验证）

| Python 原式 | Go 改写 | 改写说明 |
|---|---|---|
| `(?<!\w)/(?:[^\s'\"\\]+/?)+` | `(?:^\|[^\w])(/[^\s'\"\\]+)` | lookbehind 改为「行首或非单词字符」捕获组，取 group 1 |
| `\b[A-Za-z]:\\[^\s'\"]+` | 同 `\b[A-Za-z]:\\[^\s'"]+` | RE2 支持 `\b`，直接复用 |
| `(?i)(bearer\s+[A-Za-z0-9._~+/-]{8,}\|...)` | 同（含 `(?i)`） | RE2 支持 `(?i)` 内联标志，直接复用 |
| `(&&\|\|\|\|[;<>`$])` | 用 `strings.ContainsAny(s, "&;\|<>`$")` | 单字符集合用字符串包含更直观且等价 |
| `(?i)\b(?:bash\|cat\|...)\s+\S+` | 同（含 `(?i)`） | 直接复用 |
| `(?i)\b(?:a\|some\|the)\s+cat food\b\|\bplease\s+find me\b` | 同 | 直接复用 |
| `(?i)\b(?:amount(?:s)?(?:\s+of)?\|...)\s+\$(?:0\|[1-9]\d*)(?:\.\d{1,2})?\b` | 同 | 直接复用 |

**验证方式**：每条改写后的正则都要用 Python 原式与 Go 新式对同一批测试字符串跑，结果必须一致。测试用例至少覆盖：安全文本、含绝对路径、含密钥、含 shell 控制符、空串。

```go
// ──────────────────────────── 常量 ────────────────────────────

const (
	// AuditSchemaVersion 审计记录 schema 版本
	AuditSchemaVersion = "auto_permission_audit_v1"
	// AuditSubdirectory 审计子目录名
	AuditSubdirectory = "permission_audit"
	// AuditFilename 审计文件名
	AuditFilename = "auto_permission.jsonl"
	// AuditDigestAlgorithm 摘要算法
	AuditDigestAlgorithm = "sha256"
	// DataDirEnvName 数据目录环境变量名（第三方 env，不随品牌映射改）
	DataDirEnvName = "JIUWENSWARM_DATA_DIR"
	// MaxAuditTextLength 审计文本最大长度
	MaxAuditTextLength = 160
	// AuditTextRedacted 脱敏占位符
	AuditTextRedacted = "[redacted]"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ExtraAllowlist extra 字段白名单
	ExtraAllowlist = map[string]bool{
		"authorization_outcome": true, "authorization_stage": true,
		"decision_source": true, "host_context_failure": true,
		"host_route_reason": true, "host_route_source": true,
		"record_kind": true, "reviewer_fallback_reason": true,
		"reviewer_lifecycle": true, "reviewer_outcome": true,
		"reviewer_reason_code": true, "reviewer_reason_summary": true,
		"stage_outcome": true,
	}
)
```

- [ ] **Step 4: 运行测试确认通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run "TestBuildSanitizedAuditFields|TestSanitizeAuditText|TestResolvePersistentAuditRoot|TestPersistentAuditWriter" -v -cover
```

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/swarm/agents/harness/common/rails/permissions/persistent_audit.go \
        internal/swarm/agents/harness/common/rails/permissions/persistent_audit_test.go
git commit -m "feat(permissions): 持久化审计 JSONL（persistent_audit.py 296 行）

9.90d-1 A2：从 9.90e 提前。audit.py 的 build_sanitized_audit_fields 硬依赖。
observe-only 语义：任何异常都不影响授权结果。"
```

---

## Task 8：`url_safety`（541 行，25 函数）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/url_safety.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/url_safety_test.go`

**Python 对应：** `permissions/url_safety.py`（541 行）

**依赖**：`network_scope` 四函数 + `root_context` 三符号（均已存在，见全局约定）

### 结构（25 函数 + 3 类型 + 11 常量）

| Python | 行 | Go |
|---|---|---|
| `AbsoluteUriFacts` | 70 | `AbsoluteUriFacts` struct |
| `NetworkScopeFacts` | 87 | `NetworkScopeFacts` struct |
| `inspect_network_scope` | 97 | `InspectNetworkScope` |
| `inspect_absolute_uris` | 110 | `InspectAbsoluteUris` |
| `RecentUrlSource` | 130 | `RecentURLSource` struct + `ToJSONDict()` |
| `ReviewableUrlDecision` | 151 | `ReviewableURLDecision` struct + `NewReviewableURLDecision()`（含校验） |
| `evaluate_reviewable_url_scope` | 166 | `EvaluateReviewableURLScope` |
| `unsafe_public_https_url_reason` | 231 | `UnsafePublicHTTPSURLReason` |
| `absolute_uri_hard_block_reason` | 249 | `AbsoluteURIHardBlockReason` |
| `extract_urls_from_text` | 267 | `ExtractURLsFromText` |
| `normalize_url_for_match` | 281 | `NormalizeURLForMatch` |
| `registrable_domain` | 296 | `RegistrableDomain` |
| `_resolve_url_source_kind` | 307 | `resolveURLSourceKind` |
| `_has_trusted_host_user_intent` | 324 | `hasTrustedHostUserIntent` |
| `_matches_host_user_intent` | 340 | `matchesHostUserIntent` |
| `_extract_network_intent_tokens` | 357 | `extractNetworkIntentTokens` |
| `_has_invalid_port` | 366 | `hasInvalidPort` |
| `_safe_port_suffix` | 374 | `safePortSuffix` |
| `_network_facts` | 382 | `networkFacts` |
| `_base_summary` | 395 | `baseSummary` |
| `_reject` | 409 | `rejectDecision` |
| `_semantic_unresolved` | 425 | `semanticUnresolved` |
| `_label` | 436 | `label` |
| `_inspect_uri` | 440 | `inspectURI` |
| `_safe_root` | 507 | `safeRoot` |
| `_string_values` | 516 | `stringValues` |
| `_contains_upload` | 532 | `containsUpload` |

- [ ] **Step 1: 写失败测试（按函数分组）**

```go
package permissions

import (
	"testing"
)

// TestUnsafePublicHTTPSURLReason_各类硬拦 验证 9 种拒绝原因
func TestUnsafePublicHTTPSURLReason_各类硬拦(t *testing.T) {
	cases := []struct{ url, want string }{
		{"http://example.com", "network_scheme_not_https"},
		{"https://user:pass@example.com", "network_url_userinfo"},
		{"https://169.254.169.254/latest", "network_metadata_host"},
		{"https://foo.internal/x", "network_internal_hostname"},
		{"https://foo.local/x", "network_internal_hostname"},
		{"https://example.com?api_key=secret", "network_secret_query"},
	}
	for _, c := range cases {
		got := UnsafePublicHTTPSURLReason(parseURL(c.url))
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.url, got, c.want)
		}
	}
}

// TestUnsafePublicHTTPSURLReason_合法URL 验证公网 https 返回空
func TestUnsafePublicHTTPSURLReason_合法URL(t *testing.T) {
	if got := UnsafePublicHTTPSURLReason(parseURL("https://example.com/page")); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

// TestNormalizeURLForMatch 验证归一化（小写 scheme/host、去尾标点）
func TestNormalizeURLForMatch(t *testing.T) {
	cases := []struct{ in, want string }{
		{"HTTPS://Example.COM/Path", "https://example.com/Path"},
		{"https://example.com/x.", "https://example.com/x"},
		{"https://example.com:8443/x", "https://example.com:8443/x"},
		{"", ""},
		{"not a url", ""},
	}
	for _, c := range cases {
		if got := NormalizeURLForMatch(c.in); got != c.want {
			t.Errorf("%q: got %q, want %q", c.in, got, c.want)
		}
	}
}

// TestRegistrableDomain 验证可注册域提取
func TestRegistrableDomain(t *testing.T) {
	cases := []struct{ in, want string }{
		{"www.example.com", "example.com"},
		{"a.b.example.co.uk", "example.co.uk"},
		{"example.com", "example.com"},
		{"localhost", "localhost"},
	}
	for _, c := range cases {
		if got := RegistrableDomain(c.in); got != c.want {
			t.Errorf("%q: got %q, want %q", c.in, got, c.want)
		}
	}
}

// TestExtractURLsFromText 验证文本中 URL 提取与去重
func TestExtractURLsFromText(t *testing.T) {
	text := "看 https://a.com/x 和 https://a.com/x 以及 https://b.com/y。"
	got := ExtractURLsFromText(text)

	if len(got) != 2 {
		t.Fatalf("got %d urls, want 2: %v", len(got), got)
	}
	if got[0] != "https://a.com/x" || got[1] != "https://b.com/y" {
		t.Errorf("got %v", got)
	}
}

// TestEvaluateReviewableURLScope_无URL拒绝 验证 url 缺失
func TestEvaluateReviewableURLScope_无URL拒绝(t *testing.T) {
	facts := ToolDecisionFacts{ToolName: "web_fetch"}
	got := EvaluateReviewableURLScope(facts, nil, nil)

	if got.Accepted {
		t.Error("should reject when no URL")
	}
	if got.Reason != "network_url_missing" {
		t.Errorf("reason = %q", got.Reason)
	}
}

// TestEvaluateReviewableURLScope_多URL拒绝 验证 count != 1
func TestEvaluateReviewableURLScope_多URL拒绝(t *testing.T) {
	facts := ToolDecisionFacts{
		ToolName:      "web_fetch",
		UntrustedArgs: map[string]any{"url": []any{"https://a.com", "https://b.com"}},
	}
	got := EvaluateReviewableURLScope(facts, nil, nil)

	if got.Reason != "network_url_count_not_one" {
		t.Errorf("reason = %q", got.Reason)
	}
}

// TestEvaluateReviewableURLScope_缺用户意图 验证 evidence 为 nil 被拒
func TestEvaluateReviewableURLScope_缺用户意图(t *testing.T) {
	facts := ToolDecisionFacts{
		ToolName:      "web_fetch",
		UntrustedArgs: map[string]any{"url": "https://example.com/x"},
	}
	got := EvaluateReviewableURLScope(facts, nil, nil)

	if got.Reason != "original_user_intent_missing" {
		t.Errorf("reason = %q", got.Reason)
	}
}

// TestEvaluateReviewableURLScope_用户明确意图放行 验证 explicit_user_intent
func TestEvaluateReviewableURLScope_用户明确意图放行(t *testing.T) {
	facts := ToolDecisionFacts{
		ToolName:      "web_fetch",
		UntrustedArgs: map[string]any{"url": "https://example.com/x"},
	}
	evidence := &OriginalUserIntentEvidence{
		Source: HostUserMessageSource,
		Text:   "帮我看看 https://example.com/x",
	}
	got := EvaluateReviewableURLScope(facts, evidence, nil)

	if !got.Accepted {
		t.Errorf("should accept, reason=%q", got.Reason)
	}
	if got.Kind != "readonly_network_fetch" {
		t.Errorf("kind = %q", got.Kind)
	}
}

// TestEvaluateReviewableURLScope_上传字段拒绝 验证 upload_like
func TestEvaluateReviewableURLScope_上传字段拒绝(t *testing.T) {
	facts := ToolDecisionFacts{
		ToolName: "web_fetch",
		UntrustedArgs: map[string]any{
			"url":    "https://example.com/x",
			"upload": "file content",
		},
	}
	got := EvaluateReviewableURLScope(facts, nil, nil)

	if got.Reason != "network_upload_like" {
		t.Errorf("reason = %q", got.Reason)
	}
}

// TestReviewableURLDecision_语义复核不可覆盖硬拒 验证构造校验
func TestReviewableURLDecision_语义复核不可覆盖硬拒(t *testing.T) {
	// semanticReviewRequired=true 且 accepted=false 或 hardBlock=true → 报错
	if _, err := NewReviewableURLDecision(false, "k", "r", nil, false, true); err == nil {
		t.Error("expected error for semantic review on rejection")
	}
	if _, err := NewReviewableURLDecision(true, "k", "r", nil, true, true); err == nil {
		t.Error("expected error for semantic review on hard block")
	}
	if _, err := NewReviewableURLDecision(true, "k", "r", nil, false, true); err != nil {
		t.Errorf("valid case should not error: %v", err)
	}
}

// TestInspectAbsoluteUris 验证 URI 提取与事实字段
func TestInspectAbsoluteUris(t *testing.T) {
	got := InspectAbsoluteUris(map[string]any{
		"a": "https://example.com/x",
		"b": "file:///tmp/y",
	}, nil)

	if len(got) != 2 {
		t.Fatalf("got %d uris, want 2", len(got))
	}
	if got[0].Scheme != "https" {
		t.Errorf("scheme = %q", got[0].Scheme)
	}
	if got[1].Scheme != "file" || !got[1].FileLocal {
		t.Errorf("file uri should be file_local: %+v", got[1])
	}
}

// TestInspectNetworkScope 验证汇总事实
func TestInspectNetworkScope(t *testing.T) {
	facts := ToolDecisionFacts{
		UntrustedArgs: map[string]any{"url": "https://a.com/x"},
	}
	got := InspectNetworkScope(facts)

	if len(got.URLs) != 1 || got.URLs[0] != "https://a.com/x" {
		t.Errorf("urls = %v", got.URLs)
	}
	if len(got.Hosts) != 1 || got.Hosts[0] != "a.com" {
		t.Errorf("hosts = %v", got.Hosts)
	}
	if got.UploadLike {
		t.Error("should not be upload_like")
	}
}
```

- [ ] **Step 2-4: 运行失败 → 写实现 → 运行通过**

实现时注意：
- **`net/url` 解析**：`url.Parse` 后 `parsed.Port()` 在非法端口时**不报错**（Python 会抛 `ValueError`），需自行校验端口数字范围。
- **`unquote_to_bytes`**：Go 用 `url.PathUnescape` 或 `url.QueryUnescape`，注意 `%00` 检测。
- **`Path.expanduser()`**：Go 无内置，用 `os.UserHomeDir()` + 手工替换 `~`。

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/swarm/agents/harness/common/rails/permissions/url_safety.go \
        internal/swarm/agents/harness/common/rails/permissions/url_safety_test.go
git commit -m "feat(permissions): URL 安全性判定（url_safety.py 541 行）

9.90d-1 A1：从 9.90e 提前。25 函数 + 11 常量 + 3 类型。
auto_decision / auto_reviewer / reviewer_route / artifact_authorization 四文件硬依赖。
同时回填 root_context 的 has_valid_ask_user_clarification 消费方。"
```

---

## Task 9：`generated_artifact_delivery`（175 行）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/generated_artifact_delivery.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/generated_artifact_delivery_test.go`

**Python 对应：** `permissions/generated_artifact_delivery.py`（175 行）

### 结构（9 项）

| Python | 行 | Go |
|---|---|---|
| `SendFileAuthorizationItem` | 16 | `SendFileAuthorizationItem` struct |
| `SendFileExecutionGrant` | 23 | `SendFileExecutionGrant` struct |
| `_SendFileExecutionLease` | 31 | `sendFileExecutionLease` struct |
| `normalize_send_file_target_channels` | 44 | `NormalizeSendFileTargetChannels` |
| `normalize_send_file_paths` | 75 | `NormalizeSendFilePath` |
| `create_send_file_execution_grant` | 100 | `CreateSendFileExecutionGrant` |
| `publish_send_file_execution_grant` | 119 | `PublishSendFileExecutionGrant`（ctx 派生） |
| `current_send_file_execution_grant` | 133 | `CurrentSendFileExecutionGrant` |
| `clear_send_file_execution_grant` | 143 | `ClearSendFileExecutionGrant`（ctx 派生） |
| `consume_send_file_execution_grant` | 153 | `ConsumeSendFileExecutionGrant` |

- [ ] **Step 1: 写失败测试**

```go
package permissions

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

// TestNormalizeSendFilePath_字符串JSON 验证 JSON 字符串被解析
func TestNormalizeSendFilePath_字符串JSON(t *testing.T) {
	got := NormalizeSendFilePath(`["/a/b.txt","/c/d.txt"]`)

	want := []string{"/a/b.txt", "/c/d.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestNormalizeSendFilePath_单字符串 验证非 JSON 字符串当单值
func TestNormalizeSendFilePath_单字符串(t *testing.T) {
	got := NormalizeSendFilePath("/a/b.txt")
	if len(got) != 1 || got[0] != "/a/b.txt" {
		t.Errorf("got %v", got)
	}
}

// TestNormalizeSendFilePath_有序保序 验证不去重不排序
func TestNormalizeSendFilePath_有序保序(t *testing.T) {
	got := NormalizeSendFilePath([]any{"/z.txt", "/a.txt", "/z.txt"})

	want := []string{"/z.txt", "/a.txt", "/z.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v（保持原序且不去重）", got, want)
	}
}

// TestNormalizeSendFileTargetChannels_排序去重 验证 channels 排序
func TestNormalizeSendFileTargetChannels_排序去重(t *testing.T) {
	got := NormalizeSendFileTargetChannels([]any{"web", "app", "web"})

	want := []string{"app", "web"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestCreateSendFileExecutionGrant_空路径报错 验证必须至少一个路径
func TestCreateSendFileExecutionGrant_空路径报错(t *testing.T) {
	_, err := CreateSendFileExecutionGrant(nil, nil)
	if err == nil {
		t.Fatal("expected error for empty paths")
	}
}

// TestCreateSendFileExecutionGrant_路径解析 验证 expanduser + resolve
func TestCreateSendFileExecutionGrant_路径解析(t *testing.T) {
	dir := t.TempDir()
	got, err := CreateSendFileExecutionGrant([]any{filepath.Join(dir, "a.txt")}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Items) != 1 {
		t.Fatalf("got %d items", len(got.Items))
	}
	if got.Items[0].ResolvedPath != filepath.Clean(filepath.Join(dir, "a.txt")) {
		t.Errorf("path = %q", got.Items[0].ResolvedPath)
	}
}

// TestSendFileExecutionGrant_发布后读取 验证 ctx 派生链
func TestSendFileExecutionGrant_发布后读取(t *testing.T) {
	dir := t.TempDir()
	grant, err := CreateSendFileExecutionGrant([]any{filepath.Join(dir, "a.txt")}, nil)
	if err != nil {
		t.Fatal(err)
	}

	ctx := PublishSendFileExecutionGrant(context.Background(), grant)
	got := CurrentSendFileExecutionGrant(ctx)

	if got == nil {
		t.Fatal("grant should be readable")
	}
	if len(got.Items) != 1 {
		t.Errorf("got %d items", len(got.Items))
	}
}

// TestSendFileExecutionGrant_清除后为空 验证 Clear 生效
func TestSendFileExecutionGrant_清除后为空(t *testing.T) {
	dir := t.TempDir()
	grant, _ := CreateSendFileExecutionGrant([]any{filepath.Join(dir, "a.txt")}, nil)

	ctx := PublishSendFileExecutionGrant(context.Background(), grant)
	ctx = ClearSendFileExecutionGrant(ctx)

	if CurrentSendFileExecutionGrant(ctx) != nil {
		t.Error("grant should be cleared")
	}
}

// TestConsumeSendFileExecutionGrant_只能消费一次 验证租约语义
func TestConsumeSendFileExecutionGrant_只能消费一次(t *testing.T) {
	dir := t.TempDir()
	grant, _ := CreateSendFileExecutionGrant([]any{filepath.Join(dir, "a.txt")}, nil)
	ctx := PublishSendFileExecutionGrant(context.Background(), grant)

	first, ok := ConsumeSendFileExecutionGrant(ctx)
	if !ok || first == nil {
		t.Fatal("first consume should succeed")
	}

	second, ok := ConsumeSendFileExecutionGrant(ctx)
	if ok || second != nil {
		t.Error("second consume should fail（租约已终结）")
	}
}
```

- [ ] **Step 2-4: 运行失败 → 写实现 → 运行通过**

**关键**：Python 的 `_SendFileExecutionLease` 带 `threading.Lock` + `terminal: bool`，因为 asyncio 的 `contextvars` 在 task 间复制时**共享同一个 lease 对象**。Go 用 `sync.Once` 或 `atomic.Bool` 实现同样的"只能消费一次"语义：

```go
type sendFileExecutionLease struct {
	grant    SendFileExecutionGrant
	terminal atomic.Bool
}
```

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/swarm/agents/harness/common/rails/permissions/generated_artifact_delivery.go \
        internal/swarm/agents/harness/common/rails/permissions/generated_artifact_delivery_test.go
git commit -m "feat(permissions): send_file 执行授权租约（generated_artifact_delivery.py 175 行）

9.90d-1 A5：从 9.90e 提前。invocation_context / artifact_authorization /
before_tool 三处硬依赖。租约只能消费一次（atomic.Bool 对齐 Python terminal）。"
```

---

## Task 10：`send_file_path_guard`（315 行）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/send_file_path_guard.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/send_file_path_guard_test.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/policy_eval.go:234`（删 ⤵️ 注释）

**Python 对应：** `permissions/send_file_path_guard.py`（315 行）

**依赖**：`internal/agentcore/harness/security/fileguard`（9.90b 已落地 `BuildFileGuardChecker` / `FileToolSpec` / `LookupFileToolSpecs` / `RegisterFileTool`）

### 结构（11 项）

| Python | 行 | Go |
|---|---|---|
| `SendFilePathGuardResult` | 39 | 已有（`policy_eval.go:34` 空壳，需扩展字段） |
| `_register_native_send_file_schema` | 50 | `registerNativeSendFileSchema` |
| `_native_send_file_schema_is_valid` | 66 | `nativeSendFileSchemaIsValid` |
| `SendFilePathGuardEvaluator` | 81 | `SendFilePathGuardEvaluator` struct + `Evaluate()` |
| `_canonical_send_paths` | 166 | `canonicalSendPaths` |
| `_canonical_workspace` | 209 | `canonicalWorkspace` |
| `_canonical_trusted_dirs` | 220 | `canonicalTrustedDirs` |
| `_evaluate_paths` | 236 | `evaluatePaths` |
| `_permission_result_level` | 298 | `permissionResultLevel` |
| `_unevaluable` | 306 | `unevaluable` |

- [ ] **Step 1: 先核实 Go fileguard 包的既有签名**

```bash
cd /home/opensource/uapclaw-gateway
grep -n "^func \|^type " internal/agentcore/harness/security/fileguard/file_tool_specs.go | head -20
grep -n "^func BuildFileGuardChecker\|^type FileGuardChecker" internal/agentcore/harness/security/fileguard/file_guard.go | head
```

**已核实的真实签名（直接照抄，勿再猜）**：

```go
// internal/agentcore/harness/security/fileguard/file_tool_specs.go
type FileToolSpec struct {
    ToolName string     // 工具名
    ArgName  string     // 承载路径的参数名
    Action   FileAction // 访问动作
}
type FileAction string
const (
    FileActionRead  FileAction = "read"
    FileActionWrite FileAction = "write"
    FileActionExec  FileAction = "exec"
)
func RegisterFileTool(spec FileToolSpec)          // 同 arg_name 重复注册时替换，不同 arg_name 追加
func LookupFileToolSpecs(toolName string) []FileToolSpec

// internal/agentcore/harness/security/fileguard/file_guard.go
type FileGuardChecker struct { /* ... */ }
func BuildFileGuardChecker(permissions map[string]any, workspaceRoot string, trustedDirs []string) *FileGuardChecker
func (c *FileGuardChecker) Evaluate(toolName string, toolArgs map[string]any) *security.PermissionResult
// 注意：Evaluate 返回 *security.PermissionResult（nil 表示未判定），
// 不是 Python 的 FileGuardEvaluationStatus 三态字符串。
```

**关键**：`fileguard` 包已有 `fileToolRegistryMu sync.RWMutex` 且注释明确写了「9.90e 的 send_file_path_guard 会注册原生 schema」——说明 9.90b 落地时已为本批预留并发保护。

- [ ] **Step 2: 写失败测试**

```go
package permissions

import (
	"path/filepath"
	"testing"
)

// TestSendFilePathGuardEvaluator_正常路径 验证单路径放行
func TestSendFilePathGuardEvaluator_正常路径(t *testing.T) {
	dir := t.TempDir()
	eval := NewSendFilePathGuardEvaluator()

	got := eval.Evaluate(map[string]any{
		SENDFILEPATHPARAM: filepath.Join(dir, "a.txt"),
	}, dir, nil, nil)

	if got.Status == "unevaluable" {
		t.Errorf("should be evaluable: %+v", got)
	}
	if len(got.CanonicalPaths) != 1 {
		t.Errorf("canonical_paths = %v", got.CanonicalPaths)
	}
}

// TestSendFilePathGuardEvaluator_多路径 验证多源聚合取最严
func TestSendFilePathGuardEvaluator_多路径(t *testing.T) {
	dir := t.TempDir()
	eval := NewSendFilePathGuardEvaluator()

	got := eval.Evaluate(map[string]any{
		SENDFILEPATHPARAM: []any{
			filepath.Join(dir, "a.txt"),
			filepath.Join(dir, "b.txt"),
		},
	}, dir, nil, nil)

	if len(got.CanonicalPaths) != 2 {
		t.Errorf("canonical_paths = %v, want 2", got.CanonicalPaths)
	}
}

// TestSendFilePathGuardEvaluator_无路径 验证空路径返回 neutral/unevaluable
func TestSendFilePathGuardEvaluator_无路径(t *testing.T) {
	eval := NewSendFilePathGuardEvaluator()
	got := eval.Evaluate(map[string]any{}, "/workspace", nil, nil)

	if got.Status != "unevaluable" && got.Status != "neutral" {
		t.Errorf("status = %q", got.Status)
	}
}

// TestCanonicalSendPaths_去重保序 验证路径归一
func TestCanonicalSendPaths_去重保序(t *testing.T) {
	paths, reason := canonicalSendPaths([]any{"/a/x.txt", "/a/x.txt", "/b/y.txt"})

	if len(paths) != 2 {
		t.Errorf("got %d paths, want 2: %v", len(paths), paths)
	}
	if reason != "" {
		t.Errorf("reason = %q, want empty", reason)
	}
}

// TestUnaevaluable 验证不可评估结果的字段
func TestUnaevaluable(t *testing.T) {
	got := unevaluable("permissions_missing")
	if got.Status != "unevaluable" {
		t.Errorf("status = %q", got.Status)
	}
	if got.Reason != "permissions_missing" {
		t.Errorf("reason = %q", got.Reason)
	}
}
```

- [ ] **Step 3-5: 运行失败 → 写实现 → 运行通过 → 提交**

实现后**必做**：删除 `policy_eval.go:234` 的 `⤵️ 9.90e 回填` 注释，并把 `path_result` 的 nil 分支改为调用 `SendFilePathGuardEvaluator`。

```bash
cd /home/opensource/uapclaw-gateway
git add internal/swarm/agents/harness/common/rails/permissions/send_file_path_guard.go \
        internal/swarm/agents/harness/common/rails/permissions/send_file_path_guard_test.go \
        internal/swarm/agents/harness/common/rails/permissions/policy_eval.go
git commit -m "feat(permissions): send_file 路径守卫评估器（send_file_path_guard.py 315 行）

9.90d-1 A4：从 9.90e 提前。回填 policy_eval 的 send_file 分支
（path_result 此前恒 nil，语义失真）。删除 policy_eval.go 的 ⤵️ 9.90e 标记。"
```

---

## Task 11：`trusted_search_urls`（236 行）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/trusted_search_urls.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/trusted_search_urls_test.go`

**Python 对应：** `permissions/trusted_search_urls.py`（236 行）

**依赖**：Task 8 的 `url_safety` + `tool_invocation_key`（9.90a 已有）+ `common/tools/search_tools`

### 结构（6 项）

| Python | 行 | Go |
|---|---|---|
| `SessionTrustedSearchUrls` | 30 | `SessionTrustedSearchUrls` struct（含 6 方法） |
| `_TrustedSearchProducerBinding` | 135 | `trustedSearchProducerBinding` struct |
| `_TrustedSearchProducerLease` | 142 | `trustedSearchProducerLease` struct |
| `bind_trusted_search_producer` | 184 | `BindTrustedSearchProducer` |
| `clear_trusted_search_producer` | 208 | `ClearTrustedSearchProducer` |
| `complete_trusted_search_producer` | 215 | `CompleteTrustedSearchProducer` |

- [ ] **Step 1-5: TDD 循环**

测试要点：
- `SessionTrustedSearchUrls` 记录每会话最近搜索 URL（有淘汰策略）
- producer 绑定是**租约**语义（绑定 → 完成/清除，不可重复完成）
- `RecentURLSource` 转换（供 `url_safety.EvaluateReviewableURLScope` 用）

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/trusted_search_urls.go \
        internal/swarm/agents/harness/common/rails/permissions/trusted_search_urls_test.go
git commit -m "feat(permissions): 会话可信搜索 URL（trusted_search_urls.py 236 行）

9.90d-1 A3：从 9.90e 提前。lifecycle 的构造器入参类型，9.90d-2 硬依赖。"
```

---

## Task 12：`openjiuwen_contract`（183 行）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/openjiuwen_contract.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/openjiuwen_contract_test.go`

**Python 对应：** `permissions/openjiuwen_contract.py`（183 行）

### 结构（1 类型 + 12 函数）

| Python | 行 | Go |
|---|---|---|
| `OpenJiuwenPermissionContract` | 13 | `OpenJiuwenPermissionContract` struct |
| `load_openjiuwen_permission_contract` | 24 | `LoadOpenJiuwenPermissionContract` |
| `build_denied_permission_response` | 51 | `BuildDeniedPermissionResponse` |
| `build_rejected_permission_response` | 66 | `BuildRejectedPermissionResponse` |
| `build_manual_approval_required_response` | 81 | `BuildManualApprovalRequiredResponse` |
| `classify_permission_result` | 106 | `ClassifyPermissionResult` |
| `is_allow_result` | 131 | `IsAllowResult` |
| `is_denied_result` | 136 | `IsDeniedResult` |
| `is_interrupt_result` | 141 | `IsInterruptResult` |
| `is_user_rejection_result` | 146 | `IsUserRejectionResult` |
| `_is_user_rejection_payload` | 151 | `isUserRejectionPayload` |
| `_is_permission_denied_payload` | 171 | `isPermissionDeniedPayload` |

**关键偏差**：`load_openjiuwen_permission_contract` 里的 `inspect.iscoroutinefunction` 运行时校验在 Go 退化为**编译期断言**（`var _ = (*secrail.PermissionInterruptRail)(nil)`），`supports_update_config` / `supports_permission_engine_factory` 退化为**常量 true**（Go 类型系统已保证）。需在 spec §五 V-3 下补记一条 V-3b。

- [ ] **Step 1: 写失败测试**

```go
package permissions

import (
	"testing"
)

// TestBuildDeniedPermissionResponse 验证拒绝响应字段
func TestBuildDeniedPermissionResponse(t *testing.T) {
	got := BuildDeniedPermissionResponse("路径越界")
	if got.Approved != false {
		t.Error("approved should be false")
	}
	if got.Feedback != "[PERMISSION_DENIED] 路径越界" {
		t.Errorf("feedback = %q", got.Feedback)
	}
}

// TestBuildRejectedPermissionResponse 验证用户驳回响应字段
func TestBuildRejectedPermissionResponse(t *testing.T) {
	got := BuildRejectedPermissionResponse("用户拒绝")
	if got.Feedback != "[PERMISSION_REJECTED] 用户拒绝" {
		t.Errorf("feedback = %q", got.Feedback)
	}
}

// TestBuildManualApprovalRequiredResponse 验证人工审批中断结构
func TestBuildManualApprovalRequiredResponse(t *testing.T) {
	got := BuildManualApprovalRequiredResponse("需要人工确认", map[string]any{"k": "v"}, nil)

	if got["id"] != "auto_permission_manual_approval_required" {
		t.Errorf("id = %v", got["id"])
	}
	if got["interrupt"] != true {
		t.Errorf("interrupt = %v", got["interrupt"])
	}
	if _, exists := got["metadata"]; !exists {
		t.Error("metadata should be present")
	}
}

// TestClassifyPermissionResult_nil是allow 验证 nil → allow
func TestClassifyPermissionResult_nil是allow(t *testing.T) {
	if got := ClassifyPermissionResult(nil); got != "allow" {
		t.Errorf("got %q, want allow", got)
	}
}

// TestClassifyPermissionResult_approved真值 验证 approved=true → allow
func TestClassifyPermissionResult_approved真值(t *testing.T) {
	got := ClassifyPermissionResult(map[string]any{"approved": true})
	if got != "allow" {
		t.Errorf("got %q", got)
	}
}

// TestClassifyPermissionResult_approved假值 验证 approved=false → denied
func TestClassifyPermissionResult_approved假值(t *testing.T) {
	got := ClassifyPermissionResult(map[string]any{"approved": false})
	if got != "denied" {
		t.Errorf("got %q", got)
	}
}

// TestClassifyPermissionResult_用户驳回优先于拒绝 验证判定顺序
func TestClassifyPermissionResult_用户驳回优先于拒绝(t *testing.T) {
	// 同时含 user_rejection 与 denied 标记时，user_rejection 优先
	got := ClassifyPermissionResult(map[string]any{
		"user_rejection": true,
		"denied":         true,
	})
	if got != "user_rejection" {
		t.Errorf("got %q, want user_rejection（判定顺序：user_rejection 先于 denied）", got)
	}
}

// TestIsInterruptResult 验证中断结果识别
func TestIsInterruptResult(t *testing.T) {
	if !IsInterruptResult(map[string]any{"interrupt": true}) {
		t.Error("should be interrupt")
	}
	if IsInterruptResult(map[string]any{"interrupt": false}) {
		t.Error("should not be interrupt")
	}
}
```

- [ ] **Step 2-5: TDD 循环 + 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/openjiuwen_contract.go \
        internal/swarm/agents/harness/common/rails/permissions/openjiuwen_contract_test.go
git commit -m "feat(permissions): openjiuwen 权限契约（openjiuwen_contract.py 183 行）

9.90d-1：纯契约层无 LLM 依赖，前置先行。
inspect.iscoroutinefunction 退化编译期断言（登记 V-3b）。"
```

---

## Task 13：`permission_interrupt_rail`（417 行，计划表此前 0 章节）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/permission_interrupt_rail.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/permission_interrupt_rail_test.go`

**Python 对应：** `rails/interrupt/permission_interrupt_rail.py`（417 行）

**这是本批最关键的新增件**：`JiuwenSwarmPermissionInterruptRail` 是 9.90d-2 的 `base_rail` 实际类型。

### 结构（1 类 + 2 函数 + 1 类型 + 22 方法/属性）

| Python | 行 | Go |
|---|---|---|
| `mark_plan_hard_reject` | 38 | `MarkPlanHardReject` |
| `plan_hard_rejected` | 42 | `PlanHardRejected` |
| `_ExactPersistAttempt` | 47 | `exactPersistAttempt` struct |
| `JiuwenSwarmPermissionInterruptRail` | 65 | `JiuwenSwarmPermissionInterruptRail` struct（嵌入 `*secrail.PermissionInterruptRail`） |
| `__init__` | 68 | `NewJiuwenSwarmPermissionInterruptRail` |
| `_project_native_path_guard` | 89 | `projectNativePathGuard` |
| `_sync_engine_workspace_root` | 97 | `syncEngineWorkspaceRoot` |
| `update_config` | 131 | `UpdateConfig`（转发父类） |
| `set_trusted_dirs` | 136 | `SetTrustedDirs`（转发父类） |
| `_collect_file_guard_persist_accesses` | 140 | `collectFileGuardPersistAccesses` |
| `installed_permission_config` | 163 | `InstalledPermissionConfig` |
| `get_permission_snapshot` | 166 | `GetPermissionSnapshot` |
| `permission_check_context` | 187 | `PermissionCheckContext` |
| `_get_user_input` | 201 | `getUserInput` |
| `_approval_arguments_digest` | 208 | `approvalArgumentsDigest`（静态方法→包级函数） |
| `bind_approval_request` | 213 | `BindApprovalRequest` |
| `_invoke_permissions_hook` | 231 | `invokePermissionsHook` |
| `resolve_interrupt` | 252 | `ResolveInterrupt` |
| `_resolve_interrupt_with_run_policy` | 292 | `resolveInterruptWithRunPolicy` |
| `_persist_allow_always` | 355 | `persistAllowAlways` |
| `_persist_session_allow` | 377 | `persistSessionAllow` |
| `_persist_merged_allow` | 384 | `persistMergedAllow` |
| `_remove_exact_auto_confirm` | 390 | `removeExactAutoConfirm` |
| `before_tool_call` | 402 | `BeforeToolCall` |

- [ ] **Step 1: 核实父类可嵌入性与既有冲突**

```bash
cd /home/opensource/uapclaw-gateway
echo "=== 父类已有方法（会被子类遮蔽）==="
grep -n "func (r \*PermissionInterruptRail)" internal/agentcore/harness/rails/security/tool_security_rail.go | sed 's/(ctx.*//' | head -30
echo "=== 子类需新增的 5 个 ==="
for m in PermissionCheckContext GetPermissionSnapshot InstalledPermissionConfig ReplaceInstalledGlobalPermissions BindApprovalRequest; do
  echo -n "$m: "; grep -c "func (r \*PermissionInterruptRail) $m" internal/agentcore/harness/rails/security/tool_security_rail.go
done
```

**关键检查**：父类已有 `SetTrustedDirs`（`:931`）和 `UpdateConfig`（`:218`）。Python 子类**重写**这两个方法（转发到父类 + 额外动作）。Go 嵌入时，子类同名方法会**遮蔽**父类，正是我们需要的语义。

- [ ] **Step 2: 写失败测试**

```go
package permissions

import (
	"context"
	"testing"

	secrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
)

// TestPlanHardReject_标记与判定 验证 plan 硬拒标记链路
func TestPlanHardReject_标记与判定(t *testing.T) {
	ctx := context.Background()
	if PlanHardRejected(ctx) {
		t.Error("default should not be rejected")
	}

	ctx = MarkPlanHardReject(ctx)
	if !PlanHardRejected(ctx) {
		t.Error("should be rejected after marking")
	}
}

// TestNewJiuwenSwarmPermissionInterruptRail_嵌入父类 验证构造后父类能力可用
func TestNewJiuwenSwarmPermissionInterruptRail_嵌入父类(t *testing.T) {
	parent := newTestPermissionInterruptRail(t)
	rail := NewJiuwenSwarmPermissionInterruptRail(parent)

	if rail == nil {
		t.Fatal("rail should not be nil")
	}
	// 嵌入后 Engine() 应可访问
	if rail.Engine() == nil {
		t.Error("embedded Engine() should be available")
	}
}

// TestJiuwenSwarm_InstalledPermissionConfig 验证返回当前配置
func TestJiuwenSwarm_InstalledPermissionConfig(t *testing.T) {
	parent := newTestPermissionInterruptRail(t)
	rail := NewJiuwenSwarmPermissionInterruptRail(parent)

	got := rail.InstalledPermissionConfig()
	if got == nil {
		t.Error("InstalledPermissionConfig should not be nil")
	}
}

// TestJiuwenSwarm_GetPermissionSnapshot 验证快照
func TestJiuwenSwarm_GetPermissionSnapshot(t *testing.T) {
	parent := newTestPermissionInterruptRail(t)
	rail := NewJiuwenSwarmPermissionInterruptRail(parent)

	got := rail.GetPermissionSnapshot("sess-1")
	if got == nil {
		t.Error("snapshot should not be nil")
	}
}

// TestJiuwenSwarm_PermissionCheckContext 验证返回清理函数
func TestJiuwenSwarm_PermissionCheckContext(t *testing.T) {
	parent := newTestPermissionInterruptRail(t)
	rail := NewJiuwenSwarmPermissionInterruptRail(parent)

	cleanup, ok := rail.PermissionCheckContext("sess-1")
	if cleanup == nil {
		t.Error("cleanup should not be nil")
	}
	// 清理函数必须可安全调用
	cleanup()
	_ = ok
}

// TestJiuwenSwarm_BindApprovalRequest 验证审批请求绑定
func TestJiuwenSwarm_BindApprovalRequest(t *testing.T) {
	parent := newTestPermissionInterruptRail(t)
	rail := NewJiuwenSwarmPermissionInterruptRail(parent)

	// nil 请求不应 panic
	rail.BindApprovalRequest(nil, nil)
}

// TestJiuwenSwarm_ReplaceInstalledGlobalPermissions 验证全局权限替换
func TestJiuwenSwarm_ReplaceInstalledGlobalPermissions(t *testing.T) {
	parent := newTestPermissionInterruptRail(t)
	rail := NewJiuwenSwarmPermissionInterruptRail(parent)

	rail.ReplaceInstalledGlobalPermissions(map[string]any{"tools": map[string]any{"bash": "deny"}})

	got := rail.InstalledPermissionConfig()
	tools, _ := got["tools"].(map[string]any)
	if tools["bash"] != "deny" {
		t.Errorf("bash = %v, want deny", tools["bash"])
	}
}
```

**测试辅助**：`newTestPermissionInterruptRail(t)` 需在本文件或 `_test.go` 里实现，按 `secrail.NewPermissionInterruptRail` 的实际签名构造（Task 0 Step 4 已核实）。

- [ ] **Step 3-5: TDD 循环**

实现骨架：

```go
// ──────────────────────────── 结构体 ────────────────────────────

// JiuwenSwarmPermissionInterruptRail JiuwenSwarm 侧的权限中断轨。
//
// 包装 openjiuwen 的 PermissionInterruptRail，补上：
//   - 引擎工作区根同步
//   - 原生路径守卫投影
//   - file_guard 持久化访问收集
//   - 权限快照 / 检查上下文
//   - 审批请求绑定
//
// 9.90d-2 的 AutoPermissionRail 以本类型为 base_rail。
//
// Python: class JiuwenSwarmPermissionInterruptRail(PermissionInterruptRail)
type JiuwenSwarmPermissionInterruptRail struct {
	// 嵌入父类（Go 的"继承"）
	*secrail.PermissionInterruptRail

	// workspaceRoot 工作区根
	workspaceRoot string
	// installedPermissions 已安装的全局权限快照
	installedPermissions map[string]any
	// runPolicy 运行时策略
	runPolicy map[string]any
}
```

**注意**：`_project_native_path_guard` / `_sync_engine_workspace_root` 会修改 engine 内部状态，需确认 `PermissionEngine` 是否暴露 setter；若无，需在 Go 侧补（可能是 9.90b 的 `UpdateConfig` 已覆盖）。

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/permission_interrupt_rail.go \
        internal/swarm/agents/harness/common/rails/permissions/permission_interrupt_rail_test.go
git commit -m "feat(permissions): JiuwenSwarm 权限中断轨子类（permission_interrupt_rail.py 417 行）

9.90d-1 新增件：计划表此前 0 章节。嵌入 secrail.PermissionInterruptRail，
补 5 个 AutoPermissionRail 所需的独有成员。"
```

---

## Task 14：`MarkTrusted()` 基础设施

**Files:**
- Modify: `internal/agentcore/harness/rails/base.go`
- Test: `internal/agentcore/harness/rails/base_test.go`（追加）

**依据**：spec §4.4 / 决策 D5 / 偏差 V-4、V-5

- [ ] **Step 1: 写失败测试**

在 `internal/agentcore/harness/rails/base_test.go` 追加：

```go
// TestDeepAgentRail_MarkTrusted 验证标记后 IsTrusted 为 true
func TestDeepAgentRail_MarkTrusted(t *testing.T) {
	r := NewDeepAgentRail()
	if r.IsTrusted() {
		t.Error("new rail should not be trusted")
	}

	r.MarkTrusted()
	if !r.IsTrusted() {
		t.Error("should be trusted after MarkTrusted")
	}
}

// TestDeepAgentRail_MarkTrusted并发安全 验证原子操作无竞态
func TestDeepAgentRail_MarkTrusted并发安全(t *testing.T) {
	r := NewDeepAgentRail()
	done := make(chan struct{})

	for i := 0; i < 50; i++ {
		go func() {
			r.MarkTrusted()
			_ = r.IsTrusted()
			done <- struct{}{}
		}()
	}
	for i := 0; i < 50; i++ {
		<-done
	}

	if !r.IsTrusted() {
		t.Error("should be trusted after concurrent marks")
	}
}

// TestDeepAgentRail_多个实例互不影响 验证 trusted 是实例级而非类型级
func TestDeepAgentRail_多个实例互不影响(t *testing.T) {
	a := NewDeepAgentRail()
	b := NewDeepAgentRail()

	a.MarkTrusted()

	if !a.IsTrusted() {
		t.Error("a should be trusted")
	}
	if b.IsTrusted() {
		t.Error("b should NOT be trusted（实例级标记）")
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/rails/ -run TestDeepAgentRail_MarkTrusted -v
```

预期：`FAIL` — `r.MarkTrusted undefined`

- [ ] **Step 3: 实现**

在 `internal/agentcore/harness/rails/base.go` 的 `DeepAgentRail` 结构体加字段：

```go
type DeepAgentRail struct {
	agentinterfaces.BaseRail
	// workspace 工作空间引用
	workspace *workspace.Workspace
	// sysOperation 系统操作引用（接口值，无需取地址）
	sysOperation sys_operation.SysOperation
	// trusted 是否由受信任构造函数创建（1=是）。
	// 权限审查层据此判定该 rail 的只读工具可免于 LLM 审查。
	trusted uint32
}
```

在「导出函数」区块末尾加：

```go
// MarkTrusted 标记该 rail 实例由受信任构造函数创建。
//
// 权限审查层据此判定该 rail 的只读工具可免于 LLM 审查。
//
// ⚠️ 残余风险：Go 无法阻止调用方绕过构造函数后手工调用本方法。
// 详见 docs/superpowers/specs/2026-11-08-permissions-auto-review-9.90d-design.md
// 已知偏差 V-4。本方法替代 Python readonly_tool_bindings._closure 的
// 闭包链反射校验（Go 无 __code__.co_freevars / __closure__）。
func (d *DeepAgentRail) MarkTrusted() {
	atomic.StoreUint32(&d.trusted, 1)
}

// IsTrusted 返回该 rail 是否已由受信任构造函数标记。
//
// Python: readonly_tool_bindings 的 _closure / _methods_match 校验结果
func (d *DeepAgentRail) IsTrusted() bool {
	return atomic.LoadUint32(&d.trusted) == 1
}
```

import 加 `"sync/atomic"`。

- [ ] **Step 4: 运行确认通过（含 race）**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./internal/agentcore/harness/rails/ -run TestDeepAgentRail -v
```

- [ ] **Step 5: 让 `NewProgressiveToolRail` 调用 `MarkTrusted()`**

```bash
grep -n "func NewProgressiveToolRail" /home/opensource/uapclaw-gateway/internal/agentcore/harness/rails/progressive.go
```

在构造函数 return 之前加：

```go
	rail.MarkTrusted()
	return rail
```

- [ ] **Step 6: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/harness/rails/base.go \
        internal/agentcore/harness/rails/base_test.go \
        internal/agentcore/harness/rails/progressive.go
git commit -m "feat(rails): MarkTrusted/IsTrusted 受信任 rail 标记

9.90d-1 Task 14：替代 Python _closure/_methods_match 的反射级可执行身份校验
（Go 无 __code__.co_freevars / __closure__）。ProgressiveToolRail 构造时自动标记。
残余风险（可被手工标记）登记为 spec V-4。"
```

---

## Task 15：`BasePermissionRail` 接口

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/base_rail.go`
- Test: 编译验证（无需独立测试，接口由 Task 13 的测试间接覆盖）

**依据**：spec §4.2（规则 10.2 允许窄接口）

- [ ] **Step 1: 定义接口**

```go
package permissions

// 自动审查层包装的权限轨协作面。
//
// 依赖倒置判定（规则 10.2）：本接口是「DeepAgent 用来调用别人的」协作方能力，
// 不是 DeepAgent 自己的能力，因此允许单独定义窄接口，不进 DeepAgentInterface。
//
// 9.90d-2 的 AutoPermissionRail.baseRail 字段类型即本接口；
// 实际实现为 JiuwenSwarmPermissionInterruptRail（Task 13）。

import (
	"context"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BasePermissionRail 自动审查层包装的权限轨协作面。
//
// Python: 鸭子类型 getattr(self.base_rail, "before_tool_call", None) + callable()
// Go 用显式接口替代鸭子类型（规则 9.6 语言层差异）。
type BasePermissionRail interface {
	// BeforeToolCall 工具调用前拦截。
	BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error
	// AfterToolCall 工具调用后回调。
	AfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error
	// UpdateConfig 热更新静态权限配置。
	UpdateConfig(config map[string]any, toolNames []string)
	// SetTrustedDirs 设置受信任目录列表。
	SetTrustedDirs(trustedDirs []string)
	// InstalledPermissionConfig 返回已安装的全局权限配置。
	InstalledPermissionConfig() map[string]any
	// GetPermissionSnapshot 返回指定会话的权限快照。
	GetPermissionSnapshot(sessionID string) map[string]any
	// PermissionCheckContext 绑定权限检查上下文，返回清理函数。
	PermissionCheckContext(sessionID string) (cleanup func(), bound bool)
	// ReplaceInstalledGlobalPermissions 替换已安装的全局权限。
	ReplaceInstalledGlobalPermissions(cfg map[string]any)
	// BindApprovalRequest 绑定审批请求到工具调用。
	BindApprovalRequest(request any, toolCall *llmschema.ToolCall)
}
```

- [ ] **Step 2: 加编译期断言（在 Task 13 的文件末尾）**

```go
// 编译期断言：JiuwenSwarmPermissionInterruptRail 必须满足 BasePermissionRail
var _ BasePermissionRail = (*JiuwenSwarmPermissionInterruptRail)(nil)
```

- [ ] **Step 3: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway
go build ./internal/swarm/agents/harness/common/rails/permissions/...
```

预期：无错误。若报「缺少方法」，说明 Task 13 的实现签名与接口不一致，需对齐。

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/base_rail.go \
        internal/swarm/agents/harness/common/rails/permissions/permission_interrupt_rail.go
git commit -m "feat(permissions): BasePermissionRail 接口（依赖倒置，规则 10.2）

9.90d-1 Task 15：替代 Python 鸭子类型的显式接口。
9.90d-2 的 AutoPermissionRail.baseRail 字段类型。"
```

---

## Task 16：本批测试补齐 + 覆盖率达标

**Files:** 各 Task 的 `_test.go`（补齐未覆盖分支）

- [ ] **Step 1: 跑全批覆盖率，找出 <85% 的文件**

```bash
cd /home/opensource/uapclaw-gateway
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" \
  -coverprofile=/tmp/cov-990d1.out \
  ./internal/common/tool_display/... \
  ./internal/swarm/agents/harness/common/... \
  ./internal/swarm/runtime/... \
  ./internal/swarm/server/runtime/... \
  ./internal/agentcore/harness/rails/...

go tool cover -func=/tmp/cov-990d1.out | awk '$3+0 < 85 {print}'
```

预期：无输出（全部 ≥85%）。若有输出，逐个补测试。

- [ ] **Step 2: 为本批新增文件单独核对覆盖率**

```bash
go tool cover -func=/tmp/cov-990d1.out | grep -E "tool_display|workspace_paths|pdf_tools|command_runtime|run_permissions|sandbox_no_host|persistent_audit|url_safety|generated_artifact|send_file_path_guard|trusted_search_urls|openjiuwen_contract|permission_interrupt_rail|base_rail"
```

每个文件必须 ≥85%。

- [ ] **Step 3: 全量回归**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | grep -v "^ok" | tail -20
```

预期：无 `FAIL`。

- [ ] **Step 4: 编译 + vet**

```bash
go build ./... && go vet ./...
```

- [ ] **Step 5: 提交补测**

```bash
git add -u internal/
git commit -m "test(permissions): 9.90d-1 测试补齐，全部文件覆盖率 ≥85%"
```

---

## Task 17：文档同步 + 计划表回填

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/permissions/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 `permissions/doc.go`**

在文件树中加入本批 7 个新文件：

```
//	├── openjiuwen_contract.go             # openjiuwen 权限契约（结果分类 + 响应构造）
//	├── permission_interrupt_rail.go      # JiuwenSwarmPermissionInterruptRail 子类（嵌入父类）
//	├── persistent_audit.go                # 审计字段脱敏 + JSONL 落盘
//	├── url_safety.go                      # URL 安全性判定（25 函数）
//	├── generated_artifact_delivery.go     # send_file 执行授权租约
//	├── send_file_path_guard.go            # send_file 路径守卫评估器
//	├── trusted_search_urls.go             # 会话可信搜索 URL
//	└── base_rail.go                       # BasePermissionRail 接口（依赖倒置）
```

并删除已回填的缺口条目：
- `⤵️ send_file_path_guard（315 行，9.90e）` 段落（Task 10 已落地）

- [ ] **Step 2: 更新 `IMPLEMENTATION_PLAN.md`**

9.90d 行拆为两行：

```markdown
| 9.90d-1 | ☐ | permissions 前置件 | ⤴️ **回填 9.90c 意图窗口消费方之一**（`has_valid_ask_user_clarification` 由 `url_safety` 消费）+ A 类 5 文件从 9.90e 提前（url_safety 541 / persistent_audit 296 / trusted_search_urls 236 / send_file_path_guard 315 / generated_artifact_delivery 175）+ B 类 6 文件（workspace_paths 518 / pdf_tools 275 / command_runtime 98 / tool_display 87 / run_permissions 54 / sandbox_no_host_fallback 38）+ `permission_interrupt_rail.py` 417（计划表此前 0 章节）+ `openjiuwen_contract.py` 183 + `rails/base.go` 新增 MarkTrusted() + `base_rail.go` 接口；约 3,333 行；Spec: `docs/superpowers/specs/2026-11-08-permissions-auto-review-9.90d-design.md` | `.../rails/permissions/` · `common/` · `runtime/` |
| 9.90d-2 | ☐ | permissions 自动审查层本体 | ⤴️ 依赖 9.90d-1 全部前置件；26 文件约 7,923 行；⤴️ 回填 9.90c 剩余两处意图窗口消费方（`_intent_view` / `original_user_intent`）+ `PermissionEngine.llm` 消费 | 同左 |
```

9.90e 行追加 ⤴️：

```markdown
| 9.90e | ☐ | permissions 护栏与审计 | **本批范围缩减**：`url_safety` / `persistent_audit` / `trusted_search_urls` / `send_file_path_guard` / `generated_artifact_delivery` 已提前至 9.90d-1。剩余 `audit.py`(65) + `permission_interaction.py`(54) + `permissions_persist` 缺失 5 函数 + `permissions_config_rpc` 层次重构 + `owner_scopes` 命名修正；⤴️ 需回填 9.90d-2 的 `_emit_audit` no-op 桩（V-8） |
```

9.17 行追加 ⤴️：

```markdown
| 9.17 | ☐ | LSPRail | LSP 集成；⤴️ **需回填 9.90d-2 的 V-7**：`code_tool_bindings` 的 LSP 绑定校验（上游约 1,239 行：lsp_rail 414 + _tool 462 + _formatter 235 + _schemas 128） | `openjiuwen/harness/lsp/` |
```

在 9.x 功能 Rails 区（9.11a 之后）新增 9.11b：

```markdown
| 9.11b | ☐ | 检索工具 Prompt Rails | 检索类只读工具的 Prompt Rail 与 Toolkit；⤴️ **需回填 9.90d-2 的 V-6**：`readonly_tool_bindings` 目前只覆盖 `ProgressiveToolRail`，其余 4 类 fail-closed。前置件：`tool_retrieval_prompt_rail.py`(1,086) + `skill_retrieval_prompt_rail.py`(582) + `tool_retrieval_toolkits.py`(1,030) + `skill_retrieval_toolkits.py`(651) + `HeartbeatRuntimeBridge` + `_CronToolsCronBackend`/`_RestrictedCronBackend`；约 4,286+ 行 | `jiuwenswarm/agents/harness/common/rails/` · `common/tools/` |
```

- [ ] **Step 3: 验证 ⤵️/⤴️ 标记一致性**

```bash
cd /home/opensource/uapclaw-gateway
echo "=== 本批应删除的 9.90d 旧标记 ==="
grep -rn "⤵️ 9.90d\|⤴️ 9.90d" internal/ | grep -v "_test.go"
```

预期：0 命中（`policy_eval.go` 的已在 Task 10 删除；`root_context.go` 三处属 9.90d-2，本批不动）。

```bash
echo "=== 新增的 ⤴️ 标记 ==="
grep -n "⤴️ \*\*需回填 9.90d-2" IMPLEMENTATION_PLAN.md
```

预期：2 处（9.17、9.11b）+ 1 处（9.90e 的 V-8）。

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/doc.go \
        IMPLEMENTATION_PLAN.md
git commit -m "docs(permissions): 9.90d-1 文档同步 + 计划表回填

- doc.go 文件树补 7 个新文件，删除已回填的 send_file_path_guard 缺口
- IMPLEMENTATION_PLAN：9.90d 拆为 9.90d-1 / 9.90d-2
- 新增 9.11b 检索工具 Prompt Rails（V-6 回收）
- 9.17 追加 ⤴️（V-7 LSP 回收）
- 9.90e 追加 ⤴️（V-8 _emit_audit 回收）并缩减范围"
```

---

## 验收清单（9.90d-1 完成标准）

- [ ] 15 个 Task 全部完成并提交
- [ ] `go build ./...` 与 `go vet ./...` 无错误
- [ ] `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 全绿
- [ ] 本批新增文件覆盖率全部 ≥85%
- [ ] `grep -rn "⤵️ 9.90d" internal/` = 0 命中（`root_context.go` 三处留给 9.90d-2）
- [ ] `IMPLEMENTATION_PLAN.md` 已拆分 9.90d 并新增 9.11b / 3 处 ⤴️
- [ ] 已推送到远端
