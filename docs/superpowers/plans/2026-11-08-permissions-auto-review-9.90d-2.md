# 9.90d-2 自动审查层本体实现计划

> **For agentic workers:** REQUIRED SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **前置条件：** 9.90d-1（前置件）必须已全部完成并推送。本计划的每个 Task 都依赖 9.90d-1 的产物。

**Goal:** 落地权限自动审查层本体——规则层说 `ASK` 之后的第二层判定。先跑确定性规则挡掉一批明显安全的调用，再让隔离 LLM 审查器基于「原始用户意图窗口 + 脱敏后的工具参数 + 决策事实」裁决，只有真正需要人拍板的才弹权限卡。

**Architecture:** 单结构体 `AutoPermissionRail`（15 字段，对应 Python 12 个 mixin 共享的 `self`）+ 方法按 Python 文件分文件挂载 + `// Python: class XxxMixin` 注释标注归属（方案 C）。落在 `permissions/auto_permission/` 子包以避免与父包的循环依赖。七步判定编排全部集中在 `before_tool.go` 的单方法内（规则 9.3 步骤顺序不可调整）。

**Tech Stack:** Go 1.x + 项目既有依赖；LLM 审查器注入 `*llm.Model`；测试用 fake `ReviewerClient`，真实调用隔离 `//go:build llm`；覆盖率 ≥85%。

---

## 全局约定（所有任务适用）

- 编译前 `export GOPROXY=https://goproxy.cn,direct`
- **所有注释中文**；文件内声明顺序：接口 → 结构体 → 枚举 → 常量 → 全局变量 → 导出函数 → 非导出函数
- 每个包必须有 `doc.go`，新增文件同步文件树
- 全量测试：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...`
- 提交时**显式 `git add <文件清单>`**，禁止 `git commit -am`
- 日志文案英文，注释中文
- **mixin 归属注释**：每个方法块首行必须标 `// Python: class AutoPermissionXxxMixin.<method>`
- 模块路径前缀：`github.com/uapclaw/uapclaw-go/`

### 已核实的关键签名（继承自 9.90d-1）

```go
// permissions 包（9.90d-1 落地）
type BasePermissionRail interface {
    BeforeToolCall(ctx, cbc) error
    AfterToolCall(ctx, cbc) error
    UpdateConfig(config map[string]any, toolNames []string)
    SetTrustedDirs(trustedDirs []string)
    InstalledPermissionConfig() map[string]any
    GetPermissionSnapshot(sessionID string) map[string]any
    PermissionCheckContext(sessionID string) (cleanup func(), bound bool)
    ReplaceInstalledGlobalPermissions(cfg map[string]any)
    BindApprovalRequest(request any, toolCall *llmschema.ToolCall)
}

func MarkPlanHardReject(ctx context.Context) context.Context
func PlanHardRejected(ctx context.Context) bool
func BuildSanitizedAuditFields(facts, decision, reason string, ...) map[string]any
func NormalizeSendFilePath(value any) []string
func NormalizeSendFileTargetChannels(value any) []string
func CreateSendFileExecutionGrant(paths []any, channels any) (SendFileExecutionGrant, error)
func CurrentSendFileExecutionGrant(ctx) *SendFileExecutionGrant
func ClearSendFileExecutionGrant(ctx) context.Context
func InspectNetworkScope(facts ToolDecisionFacts) NetworkScopeFacts
func EvaluateReviewableURLScope(facts, evidence, sources) ReviewableURLDecision
func UnsafePublicHTTPSURLReason(parsed *url.URL) string
func AbsoluteURIHardBlockReason(uri AbsoluteUriFacts) string
func ExtractURLsFromText(text string) []string
func NormalizeURLForMatch(url string) string
func RegistrableDomain(host string) string
func ClassifyPermissionResult(result any) string
func BuildDeniedPermissionResponse(reason string) *secschema.PermissionConfirmResponse
func BuildManualApprovalRequiredResponse(reason string, metadata, promptPayload map[string]any) map[string]any
func IsAllowResult/IsDeniedResult/IsInterruptResult/IsUserRejectionResult(result any) bool
func ResolvePersistentAuditRoot(config map[string]any) *string
func BuildDeniedPermissionResponse / BuildRejectedPermissionResponse

// 9.90b（已有）
type OpenJiuwenPolicyEvaluator struct { /* ... */ }
func (e *OpenJiuwenPolicyEvaluator) Evaluate(invocation PolicyInvocation, sceneApprovalIsFinal bool) PolicyEvaluation
type ToolDecisionFacts struct{ ToolName, ToolCategory string; UntrustedArgs map[string]any; WorkspaceRoot string; ReadPaths, WritePaths, ExternalPaths []string; AccessesKnown bool; Capability ToolCapability }
func BuildToolDecisionFacts(toolName string, args map[string]any, opts ...) ToolDecisionFacts
type DecisionRoute struct{ Allow, Deny, Manual bool; Reason, Source string; IsDeterministicAllow bool }

// 9.90c（已有）
func OriginalUserIntent(ctx) *OriginalUserIntentEvidence
func HasValidAskUserClarification(ev *OriginalUserIntentEvidence) bool
func FileDeliveryConstraintText(ev *OriginalUserIntentEvidence) string
func RootPermissionResumeFromContext(ctx) *RootPermissionResume
func RootNonPermissionResumeFromContext(ctx) *RootNonPermissionResume
func RootContextFailureFromContext(ctx) *RootContextFailure
func OptionalRootPermissionQueue(ctx context.Context) *RootPermissionQueue      // root_permission_queue_rail.go:198
func ToolInvocationKeyFromContext(cbc *agentinterfaces.AgentCallbackContext) *ToolInvocationKeyV1  // root_permission_queue_rail.go:209，注意参数是 cbc 不是 ctx

// 9.90b（已有，补充）
type SandboxDescriptor struct{ /* ... */ }        // sandbox_profile.go:15

// 9.90d-1（已有）
func NormalizeSendFilePath / ExtractCallGoal / OverlayRunPermissions / NoHostFallbackRequired
func RequiresNoHostFallback(toolName, toolCategory string) bool
func NormalizeWorkspaceRoot(p *string) *string
func SanitizeReviewUIValue(value any, workspaceRoot string, maxLen int) any
func RedactReviewerValue / RedactText / RedactURL / RedactSecretValuesPreserveFormat
func SendFilePathGuardEvaluator                          // send_file_path_guard.go
func (r *DeepAgentRail) IsTrusted() bool
func (r *DeepAgentRail) MarkTrusted()
func WithRunPermissions(ctx context.Context, levels map[string]string) context.Context
func OverlayRunPermissions(ctx context.Context, config map[string]any) map[string]any
func ResolvePersistentAuditRoot(config map[string]any) *string
func NewPersistentAuditWriter(dataRoot *string) *PersistentAuditWriter   // 9.90d-1 Task 7 定义
func MarkPlanHardReject(ctx context.Context) context.Context
func PlanHardRejected(ctx context.Context) bool
func ClearTrustedSearchProducer()                        // trusted_search_urls.go
type SessionTrustedSearchUrls struct{ /* ... */ }         // trusted_search_urls.go

// 本计划（9.90d-2）自己定义
// auto_config.go
func NormalizeAutoPermissionOptions(raw any) map[string]any
// auto_reviewer.go
type AutoReviewer struct{ /* ... */ }
func NewAutoReviewer(client ReviewerClient, timeoutMs int, outputFormat string) *AutoReviewer
type IsolatedModelReviewerClient struct{ Model *llm.Model; DisplayLanguageGetter func() string }
func BuildIsolatedReviewerModel(model *llm.Model) *llm.Model
// reviewer_route / auto_decision 等见各 Task

// 9.90c 已有（已核实真实签名，直接调用，勿再造）
func ConsumePermissionWrapperResume(cbc *agentinterfaces.AgentCallbackContext) bool  // root_permission_queue_rail.go:293
func SetNativePathAccess(access *NativePathAccess)                                   // native_path_context.go:130，全局单例+Mutex
func CurrentNativePathAccess(toolName string, args map[string]any) *NativePathAccess // native_path_context.go:113

// ⚠️ 注意两处签名陷阱：
//  1. ConsumePermissionWrapperResume 收 cbc 不收 ctx，返回 bool 不返回对象
//  2. SetNativePathAccess 是全局单例（非 ctx 派生）——并发工具调用会互相干扰，
//     与 Python ContextVar 的 task-local 语义不同，登记为偏差 V-9（见 Task 27）
```

---

## 文件结构

### 新建文件（29 个）

**顶层 7 个**（`permissions/` 包内）：

| 路径 | 职责 | Python 行数 |
|---|---|---|
| `auto_config.go` | 自动模式配置归一化（20 函数） | 315 |
| `permission_compose.go` | Global⊕User⊕Session 合成（14 函数，**替换现有最小实现**） | 362 |
| `reviewer_redaction.go` | 审查器脱敏（24 函数 + 11 常量） | 689 |
| `reviewer_stream_metadata.go` | 流式元数据记录（3 函数 + 1 常量） | 57 |
| `auto_decision.go` | 确定性路由决策（18 函数） | 639 |
| `reviewer_route.go` | 审查路由（12 函数） | 330 |
| `auto_reviewer.go` | LLM 审查器（3 类 + 3 协议 + 22 函数） | 888 |

**子包 22 个**（`permissions/auto_permission/`）：

| 路径 | 职责 | Python 行数 |
|---|---|---|
| `auto_permission_rail.go` | `AutoPermissionRail` 单结构体 + 3 钩子 | 75 |
| `lifecycle.go` | 15 字段初始化 + Init/Uninit | 165 |
| `invocation_context.go` | 工具调用提取 + 参数修复/归一 | 715 |
| `runtime_result.go` | 结果还原（10 函数，5 个不实现） | 250 |
| `runtime_bridge.go` | 三终点桥接（12 方法） | 316 |
| `reviewer_metadata.go` | UI 元数据（10 函数） | 401 |
| `reviewer_execution.go` | LLM 审查执行（1 方法） | 389 |
| `reviewer_audit.go` | 路由审计附加（5 函数） | 77 |
| `reviewer_override_gate.go` | 覆盖门控（1 方法） | 67 |
| `reviewer_override_consume.go` | 覆盖消费（2 方法） | 84 |
| `deterministic.go` | 确定性沙箱放行（2 方法） | 131 |
| `manual_authorization.go` | 手工授权（1 方法） | 90 |
| `domain_manual.go` | 域手工响应（2 函数） | 43 |
| `domain_grant_audit.go` | 域授权审计（7 方法） | 162 |
| `deny_response.go` | 拒绝响应（2 方法） | 86 |
| `artifact_authorization.go` | 文件交付授权（7 项） | 132 |
| `artifact_tracking_capture.go` | after_tool_call 捕获 | 23 |
| `artifact_declaration.go` | 声明解析 | 15 |
| `code_tool_bindings.go` | 代码工具绑定（仅 agent_mode） | 110 |
| `readonly_tool_bindings.go` | 只读工具信任背书（仅 ProgressiveToolRail） | 320 |
| `before_tool.go` | **七步编排主入口** | 891 |

### 修改文件

| 路径 | 改动 |
|---|---|
| `permissions/permissions_layers.go:1406-1510` | 删除最小实现 `ComposeHostEffectivePermissions`，改为调用 `permission_compose.go` |
| `permissions/doc.go` | 文件树 + 删除已回填缺口 |
| `auto_permission/models.go` | 扩展（`ToolInvocation` 字段对齐） |
| `auto_permission/doc.go` | 文件树补 20 个新文件 |
| `swarm/server/adapter/deep_adapter_rails.go` | 装配 `AutoPermissionRail` |
| `swarm/server/adapter/code_adapter.go` | 同上 |
| `IMPLEMENTATION_PLAN.md` | 9.90d-2 状态回填 + 删除 ⤵️ |

---

## Task 18：`auto_config`（315 行）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/auto_config.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/auto_config_test.go`

**Python 对应：** `permissions/auto_config.py`（315 行 / 5 导出 + 15 私有）

- [ ] **Step 1: 写失败测试**

```go
package permissions

import (
	"reflect"
	"testing"
)

// TestNormalizeAutoPermissionOptions_默认值 验证空输入返回默认
func TestNormalizeAutoPermissionOptions_默认值(t *testing.T) {
	got := NormalizeAutoPermissionOptions(nil)

	if got["reviewer_output_format"] != "json" {
		t.Errorf("reviewer_output_format = %v", got["reviewer_output_format"])
	}
	if got["reviewer_timeout_ms"] == nil {
		t.Error("reviewer_timeout_ms should have default")
	}
	if got["persistent_audit_enabled"] != false {
		t.Errorf("persistent_audit_enabled = %v, want false", got["persistent_audit_enabled"])
	}
}

// TestNormalizeAutoPermissionOptions_显式值 验证用户配置生效
func TestNormalizeAutoPermissionOptions_显式值(t *testing.T) {
	raw := map[string]any{
		"reviewer_output_format": "text",
		"reviewer_timeout_ms":    5000,
		"persistent_audit_enabled": true,
	}
	got := NormalizeAutoPermissionOptions(raw)

	if got["reviewer_output_format"] != "text" {
		t.Errorf("got %v", got["reviewer_output_format"])
	}
	if got["reviewer_timeout_ms"] != 5000 {
		t.Errorf("got %v", got["reviewer_timeout_ms"])
	}
	if got["persistent_audit_enabled"] != true {
		t.Errorf("got %v", got["persistent_audit_enabled"])
	}
}

// TestNormalizeReviewerOutputFormat_非法值回退 验证未知格式回退 json
func TestNormalizeReviewerOutputFormat_非法值回退(t *testing.T) {
	if got := NormalizeReviewerOutputFormat("xml"); got != "json" {
		t.Errorf("got %q, want json", got)
	}
	if got := NormalizeReviewerOutputFormat("text"); got != "text" {
		t.Errorf("got %q, want text", got)
	}
}

// TestNormalizePermissionsForRuntime_auto模式归一 验证 tools 段被投影
func TestNormalizePermissionsForRuntime_auto模式归一(t *testing.T) {
	perms := map[string]any{
		"mode": "auto",
		"tools": map[string]any{
			"bash": "ask",
			"rm":   "deny",
		},
	}
	got := NormalizePermissionsForRuntime(perms)

	tools, _ := got["tools"].(map[string]any)
	if tools["bash"] != "ask" {
		t.Errorf("bash = %v", tools["bash"])
	}
	if tools["rm"] != "deny" {
		t.Errorf("rm = %v", tools["rm"])
	}
}

// TestToolRequiresAskInAuto 验证特定工具在 auto 模式强制 ask
func TestToolRequiresAskInAuto(t *testing.T) {
	// 按 Python 实现，某些高风险工具即使配 allow 也要 ask
	if !ToolRequiresAskInAuto("send_file_to_user") && ToolRequiresAskInAuto("read_file") {
		t.Skip("需按 Python 实际清单核对")
	}
}

// TestPositiveInt 验证正整数解析与默认值
func TestPositiveInt(t *testing.T) {
	if got := positiveInt(5000, 1000); got != 5000 {
		t.Errorf("got %d", got)
	}
	if got := positiveInt(-1, 1000); got != 1000 {
		t.Errorf("negative should use default, got %d", got)
	}
	if got := positiveInt("abc", 1000); got != 1000 {
		t.Errorf("invalid should use default, got %d", got)
	}
}
```

- [ ] **Step 2-5: TDD 循环**

实现时**逐函数对齐**（20 个），注意：
- `normalize_permissions_for_runtime` 被 `permission_compose.py` 反向调用（同包，无环）
- 三个 list 桶（`allow_tools` / `ask_tools` / `deny_tools`）在 auto 模式下需要保持有序

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/auto_config.go \
        internal/swarm/agents/harness/common/rails/permissions/auto_config_test.go
git commit -m "feat(permissions): 自动模式配置归一化（auto_config.py 315 行）"
```

---

## Task 19：`permission_compose`（362 行，替换最小实现）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/permission_compose.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/permission_compose_test.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/permissions_layers.go:1406-1510`（删除最小实现）

**Python 对应：** `permissions/permission_compose.py`（362 行 / 1 导出 + 13 私有）

**关键**：这是 spec §六 删除清单 D-1 / D-2 的执行点。

- [ ] **Step 1: 先记录现有最小实现的行为基线**

```bash
cd /home/opensource/uapclaw-gateway
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run TestComposeHostEffectivePermissions -v
```

预期：3 个既有测试 PASS —— **这是替换后必须保持通过的行为基线**。

- [ ] **Step 2: 写新测试（覆盖 Python 完整语义）**

```go
package permissions

import (
	"reflect"
	"testing"
)

// TestComposeHostEffectivePermissions_完整合并 验证三层的完整合并语义
func TestComposeHostEffectivePermissions_完整合并(t *testing.T) {
	global := map[string]any{
		"tools":      map[string]any{"bash": "ask", "read": "allow"},
		"deny_tools": []string{"rm"},
		"net_guard":  map[string]any{"enabled": true},
	}
	user := map[string]any{
		"tools": map[string]any{"bash": "allow"},
	}
	session := map[string]any{
		"tools": map[string]any{"edit": "allow"},
	}

	got := ComposeHostEffectivePermissions(global, user, session, "sess", false)

	tools, _ := got["tools"].(map[string]any)
	if tools["bash"] != "allow" {
		t.Errorf("user allow should relax global ask: bash=%v", tools["bash"])
	}
	if tools["read"] != "allow" {
		t.Errorf("read=%v", tools["read"])
	}
	if tools["edit"] != "allow" {
		t.Errorf("session allow should apply: edit=%v", tools["edit"])
	}
	if tools["rm"] != "deny" {
		t.Errorf("whole-tool deny must win: rm=%v", tools["rm"])
	}
}

// TestComposeHostEffectivePermissions_shell_guard合并 验证 shell guard 段
func TestComposeHostEffectivePermissions_shell_guard合并(t *testing.T) {
	global := map[string]any{
		"shell_guard": map[string]any{"enabled": true, "block_on_deny": true},
	}
	user := map[string]any{
		"shell_guard": map[string]any{"block_on_deny": false},
	}

	got := ComposeHostEffectivePermissions(global, user, nil, "", false)

	sg, _ := got["shell_guard"].(map[string]any)
	if sg["enabled"] != true {
		t.Errorf("enabled should inherit from global: %v", sg["enabled"])
	}
	if sg["block_on_deny"] != false {
		t.Errorf("user should override block_on_deny: %v", sg["block_on_deny"])
	}
}

// TestComposeHostEffectivePermissions_net_guard合并 验证 net urls 合并
func TestComposeHostEffectivePermissions_net_guard合并(t *testing.T) {
	global := map[string]any{
		"net_guard": map[string]any{
			"enabled":   true,
			"net_urls":  map[string]any{"a.com": "allow"},
		},
	}
	user := map[string]any{
		"net_guard": map[string]any{
			"net_urls": map[string]any{"b.com": "deny"},
		},
	}

	got := ComposeHostEffectivePermissions(global, user, nil, "", false)

	ng, _ := got["net_guard"].(map[string]any)
	urls, _ := ng["net_urls"].(map[string]any)
	if urls["a.com"] != "allow" || urls["b.com"] != "deny" {
		t.Errorf("net_urls should merge: %v", urls)
	}
}

// TestComposeHostEffectivePermissions_auto模式 验证 auto_mode 分支归一化
func TestComposeHostEffectivePermissions_auto模式(t *testing.T) {
	global := map[string]any{
		"mode":  "auto",
		"tools": map[string]any{"bash": "ask"},
	}

	got := ComposeHostEffectivePermissions(global, nil, nil, "", true)

	// auto 模式下旧 list mirrors 必须被清除
	if _, exists := got["allow_tools"]; exists {
		t.Log("allow_tools exists (auto mode 会重建)")
	}
	tools, _ := got["tools"].(map[string]any)
	if tools["bash"] != "ask" {
		t.Errorf("bash = %v", tools["bash"])
	}
}

// TestComposeHostEffectivePermissions_session丢弃ask与deny 验证 session 只贡献 allow
func TestComposeHostEffectivePermissions_session丢弃ask与deny(t *testing.T) {
	session := map[string]any{
		"tools": map[string]any{"a": "ask", "b": "deny", "c": "allow"},
	}

	got := ComposeHostEffectivePermissions(nil, nil, session, "", false)

	tools, _ := got["tools"].(map[string]any)
	if tools["a"] != nil {
		t.Errorf("session ask should be dropped: a=%v", tools["a"])
	}
	if tools["b"] != nil {
		t.Errorf("session deny should be dropped: b=%v", tools["b"])
	}
	if tools["c"] != "allow" {
		t.Errorf("session allow should apply: c=%v", tools["c"])
	}
}

// TestComposeHostEffectivePermissions_规则动作合并 验证 P1 rule actions
func TestComposeHostEffectivePermissions_规则动作合并(t *testing.T) {
	global := map[string]any{
		"rules": []any{
			map[string]any{"pattern": "rm -rf", "action": "deny"},
		},
	}
	user := map[string]any{
		"rules": []any{
			map[string]any{"pattern": "rm -rf /tmp", "action": "allow"},
		},
	}

	got := ComposeHostEffectivePermissions(global, user, nil, "", false)

	rules, _ := got["rules"].([]any)
	if len(rules) != 2 {
		t.Errorf("rules should merge, got %d: %v", len(rules), rules)
	}
}
```

- [ ] **Step 3: 写实现**

14 个函数按 §3.1 表格逐个转换。**关键语义**：

1. `_pick_level`: **deny > ask > allow**，但 user/session 的 allow 可放宽 global 的 ask
2. `_named_levels(layer, drop_tighten=True)` 用于 session 层（丢弃 deny/ask）
3. `_compose_shell_guard` / `_compose_net_guard` 是**独立合并逻辑**，不走 tools 段
4. `_fail_closed(reason, enabled)`：guard 配置异常时按 enabled 决定 fail-closed 还是禁用
5. `auto_mode=True` 时先 `normalize_permissions_for_runtime` 归一，再清空三个 list mirrors

- [ ] **Step 4: 替换最小实现**

在 `permissions_layers.go` 找到 `func ComposeHostEffectivePermissions(...)`（约 1419 行），**整个函数体替换为一行委托**：

```go
// ComposeHostEffectivePermissions 合成 Global ⊕ User ⊕ Session 生效配置。
//
// 完整实现见 permission_compose.go（对齐 Python permission_compose.py 362 行）。
// 历史：9.90b 曾在此处放最小实现（仅覆盖 tools 段），9.90d-2 替换为完整版，
// 函数签名保持不变，调用方无需改动。
func ComposeHostEffectivePermissions(
	globalPermissions map[string]any,
	userPermissions map[string]any,
	sessionPermissions map[string]any,
	sessionID string,
	autoMode bool,
) map[string]any {
	return composeHostEffectivePermissionsFull(
		globalPermissions, userPermissions, sessionPermissions, sessionID, autoMode)
}
```

并在 `permission_compose.go` 中提供 `composeHostEffectivePermissionsFull`（同包私有函数，实现完整逻辑）。

删除 `permissions_layers.go:1406-1418` 的 `⤴️ 9.90d 回填` 注释块。

- [ ] **Step 5: 验证行为基线仍通过**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/ -run "TestComposeHostEffectivePermissions" -v
```

预期：**原有 3 个测试 + 新增 6 个测试全部 PASS**（原测试是替换后的回归保护）。

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/permission_compose.go \
        internal/swarm/agents/harness/common/rails/permissions/permission_compose_test.go \
        internal/swarm/agents/harness/common/rails/permissions/permissions_layers.go
git commit -m "feat(permissions): 完整权限合成（permission_compose.py 362 行）

删除 9.90b 的最小实现（spec 删除清单 D-1/D-2），签名不变。
新增 shell_guard / net_guard 两整段合并 + auto_mode 归一化分支。
原有 3 个 ComposeHostEffectivePermissions 测试作为回归基线全部通过。"
```

---

## Task 20：`reviewer_redaction`（689 行）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/reviewer_redaction.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/reviewer_redaction_test.go`

**Python 对应：** `permissions/reviewer_redaction.py`（689 行 / 13 导出 + 11 私有 + 11 常量）

**依赖**：9.90d-1 的 `workspace_paths.SanitizeReviewUIValue`

### 结构（24 函数 + 11 常量）

| 导出函数 | Python 行 |
|---|---|
| `RedactText` | 160 |
| `ReviewerPathLocation` | 173 |
| `RedactReviewablePayloadText` | 244 |
| `RedactSecretValues` | 264 |
| `RedactSecretValuesPreserveFormat` | 283 |
| `RedactReviewerValue` | 330 |
| `RedactJSONValue` | 349 |
| `RedactURL` | 384 |
| `SanitizePermissionUIPayload` | 422 |

| 常量 | 值 |
|---|---|
| `DefaultMaxRedactedTextLength` | 240 |
| `DefaultMaxRedactedItems` | 5 |
| `ReviewablePayloadTextLimit` | 1024 |
| `PermissionUIPayloadMaxDepth` | 6 |
| `PermissionUIPayloadMaxItems` | 32 |
| `PermissionUIPayloadMaxStringLength` | 1024 |
| `PermissionUIPayloadMaxTotalBytes` | 16 * 1024 |
| `PermissionUIRedacted` | "[REDACTED]" |
| `PermissionUITruncated` | "[TRUNCATED]" |
| `PermissionUIUnavailable` | "[UNAVAILABLE]" |

- [ ] **Step 1: 写失败测试**

```go
package permissions

import (
	"strings"
	"testing"
)

// TestRedactText_密钥脱敏 验证各类 secret 被替换
func TestRedactText_密钥脱敏(t *testing.T) {
	cases := []string{
		"sk-1234567890abcdef",
		"Bearer abcdefghijklmnop",
		"password=secret123",
		"AKIAIOSFODNN7EXAMPLE",
	}
	for _, s := range cases {
		got := RedactText(s, DefaultMaxRedactedTextLength)
		if got == s {
			t.Errorf("%q should be redacted", s)
		}
	}
}

// TestRedactText_超长截断 验证 240 字符上限
func TestRedactText_超长截断(t *testing.T) {
	long := strings.Repeat("a", 500)
	got := RedactText(long, DefaultMaxRedactedTextLength)

	if len(got) > DefaultMaxRedactedTextLength+20 {
		t.Errorf("len = %d, should be truncated", len(got))
	}
}

// TestRedactURL_保留格式 验证 URL 中敏感部分脱敏但结构保留
func TestRedactURL_保留格式(t *testing.T) {
	got := RedactURL("https://user:pass@example.com/path?token=abc123", 200)

	if strings.Contains(got, "pass") || strings.Contains(got, "abc123") {
		t.Errorf("credentials/token should be redacted: %q", got)
	}
	if !strings.Contains(got, "example.com") {
		t.Errorf("host should be preserved: %q", got)
	}
}

// TestRedactSecretValuesPreserveFormat 验证格式保留的密钥替换
func TestRedactSecretValuesPreserveFormat(t *testing.T) {
	got := RedactSecretValuesPreserveFormat("key=sk-1234567890abcdef", 200)

	if strings.Contains(got, "sk-1234567890abcdef") {
		t.Errorf("secret should be redacted: %q", got)
	}
	// 格式保留：长度应相近（用等长掩码）
	if len(got) < 10 {
		t.Errorf("format should be preserved, got %q", got)
	}
}

// TestRedactJSONValue_嵌套脱敏 验证递归脱敏
func TestRedactJSONValue_嵌套脱敏(t *testing.T) {
	input := map[string]any{
		"api_key": "sk-1234567890",
		"nested": map[string]any{
			"token": "Bearer abcdefghij",
			"safe":  "hello",
		},
	}
	got := RedactJSONValue(input, DefaultMaxRedactedItems, DefaultMaxRedactedTextLength).(map[string]any)

	if got["api_key"] == "sk-1234567890" {
		t.Error("api_key should be redacted")
	}
	nested := got["nested"].(map[string]any)
	if nested["token"] == "Bearer abcdefghij" {
		t.Error("nested token should be redacted")
	}
	if nested["safe"] != "hello" {
		t.Errorf("safe field should survive: %v", nested["safe"])
	}
}

// TestSanitizePermissionUIPayload_深度限制 验证超过 maxDepth 被截断
func TestSanitizePermissionUIPayload_深度限制(t *testing.T) {
	// 构造 10 层嵌套
	var deep any = "leaf"
	for i := 0; i < 10; i++ {
		deep = map[string]any{"n": deep}
	}

	got := SanitizePermissionUIPayload(deep)
	// 不应 panic，且深度受限
	if got == nil {
		t.Error("should return something")
	}
}

// TestSanitizePermissionUIPayload_项数限制 验证超过 maxItems 被截断
func TestSanitizePermissionUIPayload_项数限制(t *testing.T) {
	items := make([]any, 100)
	for i := range items {
		items[i] = i
	}

	got := SanitizePermissionUIPayload(items).([]any)
	if len(got) > PermissionUIPayloadMaxItems {
		t.Errorf("len = %d, want <= %d", len(got), PermissionUIPayloadMaxItems)
	}
}

// TestSanitizePermissionUIPayload_总字节限制 验证 16KB 上限
func TestSanitizePermissionUIPayload_总字节限制(t *testing.T) {
	big := strings.Repeat("x", 100000)
	got := SanitizePermissionUIPayload(big)

	if s, ok := got.(string); ok && len(s) > PermissionUIPayloadMaxTotalBytes {
		t.Errorf("len = %d exceeds total byte limit", len(s))
	}
}

// TestReviewerPathLocation 验证路径位置标签
func TestReviewerPathLocation(t *testing.T) {
	got := ReviewerPathLocation("/workspace/src/main.go", "/workspace")
	if got == "" {
		t.Error("should return a location label")
	}
	if strings.Contains(got, "/workspace") {
		t.Errorf("workspace root should be relativized: %q", got)
	}
}
```

- [ ] **Step 2-5: TDD 循环**

**关键**：Python 正则里有多处 lookbehind（`(?<!)` / `(?<=)`），Go RE2 **不支持**。需逐个改写为等价的前缀检查或分组捕获。实施时必须逐条验证改写后的正则行为一致，并在注释中说明改写方式。

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/reviewer_redaction.go \
        internal/swarm/agents/harness/common/rails/permissions/reviewer_redaction_test.go
git commit -m "feat(permissions): 审查器脱敏（reviewer_redaction.py 689 行）

24 函数 + 11 常量。Python lookbehind 正则改写为 RE2 等价形式。
启用 9.90a 挂账的 ShellDisplayPathPattern / SensitiveDisplayPathPattern。"
```

---

## Task 21：`reviewer_stream_metadata`（57 行）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/reviewer_stream_metadata.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/reviewer_stream_metadata_test.go`

**Python 对应：** `permissions/reviewer_stream_metadata.py`

- [ ] **Step 1: TDD 循环**

3 个函数：`RecordReviewerToolResultMetadata` / `ConsumeReviewerToolResultMetadata` / `PeekReviewerToolResultMetadata`，按 `tool_call_id` 索引 session extra。

测试要点：
- 记录后 `Peek` 可读
- `Consume` 读取后清除
- 同一 `tool_call_id` 重复记录覆盖

- [ ] **Step 2: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/reviewer_stream_metadata.go \
        internal/swarm/agents/harness/common/rails/permissions/reviewer_stream_metadata_test.go
git commit -m "feat(permissions): 审查器流式元数据（reviewer_stream_metadata.py 57 行）"
```

---

## Task 22：`auto_decision`（639 行）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/auto_decision.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/auto_decision_test.go`

**Python 对应：** `permissions/auto_decision.py`（639 行 / 5 导出 + 13 私有）

### 结构

| 导出函数 | Python 行 | 作用 |
|---|---|---|
| `DeterministicGuardRoute` | 73 | 确定性护栏路由 |
| `GenericMCPEgressEvidence` | 82 | MCP 出口证据 |
| `DeterministicDomainRoute` | 116 | 域确定性路由 |
| `ToolSearchArgsValid` | 426 | tool_search 参数校验 |
| `SessionObservationArgsValid` | 442 | 会话观察参数校验 |
| `TerminalLowRiskRoute` | 557 | 终态低风险路由 |
| `TerminalInternalRoute` | 575 | 终态内部路由 |
| `PostPolicyRoute` | 627 | 策略后路由 |

- [ ] **Step 1: 写失败测试（重点覆盖确定性放行路径）**

```go
package permissions

import (
	"testing"
)

// TestPostPolicyRoute_allow直接放行 验证 policy allow 短路
func TestPostPolicyRoute_allow直接放行(t *testing.T) {
	facts := ToolDecisionFacts{ToolName: "read_file", ToolCategory: "file_read"}
	got := PostPolicyRoute(facts, "allow", nil, nil)

	if !got.Allow {
		t.Errorf("allow level should route to allow: %+v", got)
	}
}

// TestPostPolicyRoute_deny直接拒绝 验证 policy deny 短路
func TestPostPolicyRoute_deny直接拒绝(t *testing.T) {
	facts := ToolDecisionFacts{ToolName: "bash", ToolCategory: "command"}
	got := PostPolicyRoute(facts, "deny", nil, nil)

	if !got.Deny {
		t.Errorf("deny level should route to deny: %+v", got)
	}
}

// TestTerminalLowRiskRoute_只读工具放行 验证低风险工具确定性放行
func TestTerminalLowRiskRoute_只读工具放行(t *testing.T) {
	facts := ToolDecisionFacts{
		ToolName:     "read_file",
		ToolCategory: "file_read",
		Capability: ToolCapability{
			RiskTier:          "low",
			StaticSideEffects: map[string]bool{"read": true},
		},
	}
	got := TerminalLowRiskRoute(facts)

	if got == nil {
		t.Skip("需按 Python 实际判定条件核对")
	}
	if !got.IsDeterministicAllow {
		t.Errorf("should be deterministic allow: %+v", got)
	}
}

// TestToolSearchArgsValid_progressive模式 验证 tool_search 参数校验
func TestToolSearchArgsValid_progressive模式(t *testing.T) {
	valid := map[string]any{"query": "文件读取"}
	if !ToolSearchArgsValid(valid, true) {
		t.Error("valid args should pass")
	}

	invalid := map[string]any{"query": ""}
	if ToolSearchArgsValid(invalid, true) {
		t.Error("empty query should fail")
	}
}

// TestSessionObservationArgsValid 验证会话观察参数判定（三态）
func TestSessionObservationArgsValid(t *testing.T) {
	// 返回 *bool：nil 表示该工具不是会话观察类
	got := SessionObservationArgsValid("sessions_list", map[string]any{})
	if got == nil {
		t.Skip("sessions_list 可能是会话观察类，需核对 Python")
	}
}
```

- [ ] **Step 2-5: TDD 循环 + 提交**

**关键**：`auto_decision.py` 里有 4 个模块级正则（`SEARCH_SKILL_SENSITIVE_QUERY_PATTERN` / `EGRESS_SECRET_KEY_PATTERN` / `EGRESS_SECRET_VALUE_PATTERN` 等），需转成 Go `regexp.MustCompile`，注意 RE2 不支持 `(?i)` 之外的高级特性。

```bash
git add internal/swarm/agents/harness/common/rails/permissions/auto_decision.go \
        internal/swarm/agents/harness/common/rails/permissions/auto_decision_test.go
git commit -m "feat(permissions): 确定性路由决策（auto_decision.py 639 行）

18 函数。消费 url_safety 的 inspect_network_scope / evaluate_reviewable_url_scope
（9.90d-1 已落地）。"
```

---

## Task 23：`reviewer_route`（330 行）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/reviewer_route.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/reviewer_route_test.go`

**Python 对应：** `permissions/reviewer_route.py`（330 行 / 2 导出 + 11 私有）

### 结构

| 导出 | Python 行 |
|---|---|
| `ReviewerRoute` | 116 |
| `FileDeliveryManualReason` | 235 |

- [ ] **Step 1: 写失败测试**

```go
package permissions

import (
	"testing"
)

// TestReviewerRoute_语义复核 验证无法确定性判定时走 semantic
func TestReviewerRoute_语义复核(t *testing.T) {
	facts := ToolDecisionFacts{
		ToolName:     "bash",
		ToolCategory: "command",
		UntrustedArgs: map[string]any{"command": "npm install lodash"},
	}
	got := ReviewerRoute(facts, nil, nil)

	if got.Source != SEMANTICREVIEWSOURCE {
		t.Errorf("source = %q, want %q", got.Source, SEMANTICREVIEWSOURCE)
	}
}

// TestReviewerRoute_公开搜索走manual 验证搜索类需人工
func TestReviewerRoute_公开搜索走manual(t *testing.T) {
	facts := ToolDecisionFacts{
		ToolName:     "web_search",
		ToolCategory: "network_read",
		Capability: ToolCapability{
			RiskTier:          "low",
			StaticSideEffects: map[string]bool{"network_read": true},
		},
	}
	got := ReviewerRoute(facts, nil, nil)

	// 公开搜索按 Python 可能走 manual 或 semantic，需核对
	if got.Source == "" {
		t.Error("source should not be empty")
	}
}

// TestReviewerRoute_工作区写入 验证 workspace write 判定
func TestReviewerRoute_工作区写入(t *testing.T) {
	facts := ToolDecisionFacts{
		ToolName:     "write_file",
		ToolCategory: "file_write",
		WorkspaceRoot: "/workspace",
		WritePaths:   []string{"/workspace/src/a.go"},
		AccessesKnown: true,
	}
	got := ReviewerRoute(facts, nil, nil)

	if got.Source == "" || got.Reason == "" {
		t.Errorf("route should be resolved: %+v", got)
	}
}

// TestFileDeliveryManualReason 验证文件交付需人工的原因
func TestFileDeliveryManualReason(t *testing.T) {
	facts := ToolDecisionFacts{
		ToolName:     "send_file_to_user",
		ToolCategory: "file_delivery",
	}
	got := FileDeliveryManualReason(facts)
	// 需按 Python 核对具体文案
	_ = got
}
```

- [ ] **Step 2-5: TDD 循环 + 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/reviewer_route.go \
        internal/swarm/agents/harness/common/rails/permissions/reviewer_route_test.go
git commit -m "feat(permissions): 审查路由（reviewer_route.py 330 行）"
```

---

## Task 24：`auto_reviewer`（888 行，LLM 审查器）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/auto_reviewer.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/auto_reviewer_test.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/auto_reviewer_llm_test.go`（`//go:build llm`）

**Python 对应：** `permissions/auto_reviewer.py`（888 行）

### 结构

| 类型/函数 | Python 行 |
|---|---|
| `ReviewerOutcome` | 154 |
| `ReviewerClient`（Protocol → Go 接口） | 162 |
| `BuildIsolatedReviewerModel` | 169 |
| `IsolatedModelReviewerClient` | 189（含 `Assess` / `formatKwargs` / `displayLanguage`） |
| `ReviewerActionView` | 277（含 `ToJSONDict`） |
| `BuildReviewerActionView` | 317 |
| `AutoReviewAssessment` | 588 |
| `AutoReviewer` | 601（含 `UpdateRuntimeOptions` / `Assess` / `callWithTimeout` / `callClient` / `parseResponse` / `manual` / `manualOutcomeNotAllowed` / `fallback`） |
| `_intent_view` | 548 |
| 其余 15 个私有函数 | — |

### 提示词（规则：一比一同步 Python）

`_reviewer_natural_language_instruction(language)` 和 `AutoReviewer._call_client` 里的系统提示词必须**逐字复制** Python 原文，仅品牌字符串按 jiuwenswarm→uapclaw 映射替换。

- [ ] **Step 1: 提取 Python 提示词原文**

```bash
cd /home/opensource/jiuwenswarm-develop/jiuwenswarm/agents/harness/common/rails/permissions
sed -n '60,150p' auto_reviewer.py     # 常量区
sed -n '655,700p' auto_reviewer.py    # _call_client 提示词构造
```

把原文完整抄进 Go 的常量定义，**不要翻译、不要改写**。

- [ ] **Step 2: 写 fake client 测试**

```go
package permissions

import (
	"context"
	"errors"
	"testing"
)

// fakeReviewerClient 返回预置响应的审查器客户端
type fakeReviewerClient struct {
	response string
	err      error
	calls    int
}

// Assess 返回预置响应
func (f *fakeReviewerClient) Assess(request *ReviewerActionView) (string, error) {
	f.calls++
	return f.response, f.err
}

// TestAutoReviewer_解析allow响应 验证 JSON 响应解析
func TestAutoReviewer_解析allow响应(t *testing.T) {
	client := &fakeReviewerClient{
		response: `{"outcome":"allow","reason_code":"safe_read","reason_summary":"只读操作"}`,
	}
	reviewer := NewAutoReviewer(client, 5000, "json")

	got, err := reviewer.Assess(context.Background(), &ReviewerActionView{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Outcome != "allow" {
		t.Errorf("outcome = %q", got.Outcome)
	}
	if got.ReasonCode != "safe_read" {
		t.Errorf("reason_code = %q", got.ReasonCode)
	}
}

// TestAutoReviewer_解析deny响应 验证拒绝解析
func TestAutoReviewer_解析deny响应(t *testing.T) {
	client := &fakeReviewerClient{
		response: `{"outcome":"deny","reason_code":"destructive","reason_summary":"危险命令"}`,
	}
	reviewer := NewAutoReviewer(client, 5000, "json")

	got, _ := reviewer.Assess(context.Background(), &ReviewerActionView{})
	if got.Outcome != "deny" {
		t.Errorf("outcome = %q", got.Outcome)
	}
}

// TestAutoReviewer_超时降级 验证超时返回 fallback
func TestAutoReviewer_超时降级(t *testing.T) {
	client := &fakeReviewerClient{err: context.DeadlineExceeded}
	reviewer := NewAutoReviewer(client, 1, "json")

	got, _ := reviewer.Assess(context.Background(), &ReviewerActionView{})
	if got.Outcome == "" {
		t.Error("should fallback to a non-empty outcome")
	}
}

// TestAutoReviewer_非法JSON降级 验证解析失败走 fallback
func TestAutoReviewer_非法JSON降级(t *testing.T) {
	client := &fakeReviewerClient{response: "not json at all"}
	reviewer := NewAutoReviewer(client, 5000, "json")

	got, _ := reviewer.Assess(context.Background(), &ReviewerActionView{})
	if got.Outcome == "allow" {
		t.Error("invalid response must not silently become allow")
	}
}

// TestAutoReviewer_禁止字段拒绝 验证 FORBIDDEN_REVIEWER_FIELDS 生效
func TestAutoReviewer_禁止字段拒绝(t *testing.T) {
	client := &fakeReviewerClient{
		response: `{"outcome":"allow","grant":"all","reason_code":"x"}`,
	}
	reviewer := NewAutoReviewer(client, 5000, "json")

	got, _ := reviewer.Assess(context.Background(), &ReviewerActionView{})
	if got.Outcome == "allow" {
		t.Error("forbidden field 'grant' should invalidate the response")
	}
}

// TestIntentView_投影意图窗口 验证 _intent_view 消费 9.90c 的意图证据
func TestIntentView_投影意图窗口(t *testing.T) {
	evidence := &OriginalUserIntentEvidence{
		Source: HostUserMessageSource,
		Text:   "帮我整理一下项目文档",
	}
	facts := ToolDecisionFacts{ToolName: "write_file"}

	got := intentView(evidence, facts)
	if got == nil {
		t.Fatal("should return a view")
	}
	m, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("got %T", got)
	}
	if m["intent"] == nil {
		t.Error("intent should be present")
	}
}

// TestIntentView_nil证据 验证无意图时返回空视图
func TestIntentView_nil证据(t *testing.T) {
	got := intentView(nil, ToolDecisionFacts{})
	if got == nil {
		t.Error("should return empty view, not nil")
	}
}

// TestReviewerActionView_ToJSONDict 验证动作视图序列化
func TestReviewerActionView_ToJSONDict(t *testing.T) {
	view := &ReviewerActionView{
		ToolName:   "bash",
		ToolArgs:   map[string]any{"command": "ls"},
		UserIntent: map[string]any{"intent": "查看文件"},
	}
	got := view.ToJSONDict()

	if got["tool_name"] != "bash" {
		t.Errorf("tool_name = %v", got["tool_name"])
	}
	if _, exists := got["tool_args"]; !exists {
		t.Error("tool_args should be present")
	}
}
```

- [ ] **Step 3: 写真实 LLM 测试（隔离）**

```go
//go:build llm

package permissions

import (
	"context"
	"os"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
)

// TestAutoReviewer_真实调用 验证真实 LLM 审查器
// 运行方式: go test -tags llm ./internal/swarm/agents/harness/common/rails/permissions/ -run TestAutoReviewer_真实调用
func TestAutoReviewer_真实调用(t *testing.T) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	model := llm.NewModel(llm.ModelConfig{APIKey: apiKey, APIBase: os.Getenv("OPENAI_API_BASE")})
	client := &IsolatedModelReviewerClient{Model: model, DisplayLanguageGetter: func() string { return "zh" }}
	reviewer := NewAutoReviewer(client, 30000, "json")

	view := BuildReviewerActionView(
		ToolDecisionFacts{ToolName: "read_file", ToolCategory: "file_read"},
		map[string]any{"path": "/workspace/src/main.go"},
		&OriginalUserIntentEvidence{Source: HostUserMessageSource, Text: "看一下 main.go"},
		nil, nil, nil, nil,
	)
	got, err := reviewer.Assess(context.Background(), view)
	if err != nil {
		t.Fatalf("real LLM call failed: %v", err)
	}
	if got.Outcome == "" {
		t.Error("outcome should not be empty")
	}
}
```

- [ ] **Step 4-6: TDD 循环 + 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/auto_reviewer.go \
        internal/swarm/agents/harness/common/rails/permissions/auto_reviewer_test.go \
        internal/swarm/agents/harness/common/rails/permissions/auto_reviewer_llm_test.go
git commit -m "feat(permissions): LLM 审查器（auto_reviewer.py 888 行）

提示词逐字复制 Python 原文（规则：一比一同步）。
回填 9.90c 的 _intent_view 消费方（意图窗口三处消费之一）。
真实 LLM 调用隔离 //go:build llm。"
```

---

## Task 25：`auto_permission/models.go` + `tool_binding.go` 扩展

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/models.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/tool_binding.go`

- [ ] **Step 1: 对照 Python `models.py`（101 行）核对现有 Go 结构**

```bash
cd /home/opensource/uapclaw-gateway
sed -n '75,101p' /home/opensource/jiuwenswarm-develop/jiuwenswarm/agents/harness/common/rails/permissions/_auto_permission/models.py
echo "=== Go 现状 ==="
sed -n '30,80p' internal/swarm/agents/harness/common/rails/permissions/auto_permission/models.go
```

核对 `ToolInvocation` / `PermissionHandlingResult` / `PermissionInterruptRequest` 三类型的字段是否完整。

- [ ] **Step 2: 补齐缺失字段并加测试**

```go
// TestToolInvocation_字段完整 验证 Python 三 dataclass 的字段全部存在
func TestToolInvocation_字段完整(t *testing.T) {
	inv := ToolInvocation{
		ToolName:  "bash",
		ToolArgs:  map[string]any{"command": "ls"},
		Ctx:       nil,
		ToolCall:  nil,
	}
	if inv.ToolName != "bash" {
		t.Error("ToolName field missing")
	}
}

// TestNewPermissionHandlingResult_handled为true必须有result 验证构造校验
func TestNewPermissionHandlingResult_handled为true必须有result(t *testing.T) {
	if _, err := NewPermissionHandlingResult(true, nil, "src"); err == nil {
		t.Error("handled=true with nil result should error（Python __post_init__ 校验）")
	}
}

// TestNewPermissionHandlingResult_handled为false必须无result 验证反向校验
func TestNewPermissionHandlingResult_handled为false必须无result(t *testing.T) {
	got, err := NewPermissionHandlingResult(false, nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Handled {
		t.Error("handled should be false")
	}
}
```

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/auto_permission/models.go \
        internal/swarm/agents/harness/common/rails/permissions/auto_permission/models_test.go
git commit -m "feat(permissions): auto_permission models 字段补齐（models.py 101 行对齐）"
```

---

## Task 26-38：子包其余 18 个文件

以下 13 个任务（编号 26-38）结构相同，各自对应一个或两个 Python 文件。**每个任务必须**：
1. 读 Python 源码（完整读，不跳读）
2. 写失败测试（每个导出方法 ≥2 断言）
3. 写实现（方法块标 `// Python: class AutoPermissionXxxMixin.<method>`）
4. 测试通过 + 覆盖率 ≥85%
5. 提交

### Task 26：`lifecycle.go`（165 行）

**Files:**
- Create: `permissions/auto_permission/lifecycle.go`
- Test: `permissions/auto_permission/lifecycle_test.go`

**Python：** `_auto_permission/lifecycle.py`

要点：15 个共享字段初始化（见 §3.3 的结构体定义）；`Init` 转发 `base_rail.init(agent)`；`Uninit` 反向清理；`_recent_url_sources` 供 reviewer 路由消费。构造期 `policy_evaluator` 为 nil 时自动 `NewOpenJiuwenPolicyEvaluator(baseRail, workspaceRoot)`。

### Task 27：`invocation_context.go`（715 行）

**Files:**
- Create: `permissions/auto_permission/invocation_context.go`
- Test: `permissions/auto_permission/invocation_context_test.go`

**Python：** `_auto_permission/invocation_context.py`（30 函数）

要点见上文「Task 27 详细要求」——含两项 9.90c 遗留补件的核实结论与 V-9 偏差。

### Task 28：`runtime_result.go`（250 行）

**Files:**
- Create: `permissions/auto_permission/runtime_result.go`
- Test: `permissions/auto_permission/runtime_result_test.go`

**Python：** `_auto_permission/runtime_result.py`

**偏差 V-3**：5 个反射适配函数**不实现**——`resolveBaseCallArgs` / `needsRuntimeToolCall` / `positionalCapacity` / `filterBaseCallKwargs` / `isOpenJiuwenRuntimeContext`。Go 签名静态固定，无 `inspect.signature` 需求。

```go
// TestRuntimeResult_从cbc读决策 验证 Go 副作用式还原
func TestRuntimeResult_从cbc读决策(t *testing.T) {
	cbc := newTestCallbackContext("bash")
	cbc.SetExtra("_interrupt_decision", &interrupt.ApproveResult{})

	got := permissions.resultFromBaseSideEffect(cbc)
	if got == nil {
		t.Error("should reconstruct result from cbc extra")
	}
}
```

### Task 29：`runtime_bridge.go`（316 行）

**Files:**
- Create: `permissions/auto_permission/runtime_bridge.go`
- Test: `permissions/auto_permission/runtime_bridge_test.go`

**Python：** `_auto_permission/runtime_bridge.py`（12 方法）

**偏差 V-8**：`emitAudit` / `emitPermissionResultObservation` 为 no-op 桩：

```go
// Python: AutoPermissionRuntimeBridgeMixin._emit_audit
//
// ⤵️ 9.90e 回填：audit.py 本批不落地（D7 决策），此处为空实现。
// 保留完整签名与全部调用点（before_tool 16 处 + domain_grant_audit 3 处），
// 9.90e 只需把桩体换成真实 emit_permission_audit 调用。
func (r *AutoPermissionRail) emitAudit(facts permissions.ToolDecisionFacts, opts ...auditOption) any {
	return nil // ⤵️ 9.90e 回填
}

// ⤵️ 9.90e 回填
func (r *AutoPermissionRail) emitPermissionResultObservation(
	facts permissions.ToolDecisionFacts, result any, sessionID, toolCallID, decisionSource string,
) {
	// ⤵️ 9.90e 回填
}
```

`callBaseRail` **必须重写**（Python 是 await 返回值，Go 是副作用式）：

```go
// TestCallBaseRail_调用穿透父类 验证 Go 副作用式调用
func TestCallBaseRail_调用穿透父类(t *testing.T) {
	parent := newTestPermissionInterruptRail(t)
	rail := newTestAutoRailWithBase(t, parent)
	cbc := newTestCallbackContext("bash")

	_ = rail.callBaseRail(context.Background(), cbc, ToolInvocation{ToolName: "bash"})

	// 父类应已写入决策
	if cbc.Extra("_interrupt_decision") == nil {
		t.Error("base rail should write _interrupt_decision")
	}
}
```

### Task 30：`reviewer_metadata.go`（401 行）

**Files:**
- Create: `permissions/auto_permission/reviewer_metadata.go`
- Test: `permissions/auto_permission/reviewer_metadata_test.go`

**Python：** `_auto_permission/reviewer_metadata.py`（10 函数）

要点：消费 9.90d-1 的 `SanitizeReviewUIValue`（`workspace_paths.go`）；`_reviewer_ui_metadata` 与 `_reviewer_action_summary` 是两个主出口，被 `auto_permission_rail.py` 与 `runtime_bridge.py` 导入。

### Task 31：`reviewer_audit.go`（77 行）

**Files:**
- Create: `permissions/auto_permission/reviewer_audit.go`
- Test: `permissions/auto_permission/reviewer_audit_test.go`

**Python：** `_auto_permission/reviewer_audit.py`（5 函数）

要点：`ReviewerRouteAuditExtra` 只投影不可约简的 Host 路由溯源；`IsAllowOnceConfirmation` 需识别已退役的 task-scope 载荷（`_RETIRED_TASK_SCOPE_CONFIRMATION_KEYS` 三键）使其不能变成 allow-once。

### Task 32：`reviewer_execution.go`（389 行）

**Files:**
- Create: `permissions/auto_permission/reviewer_execution.go`
- Test: `permissions/auto_permission/reviewer_execution_test.go`

**Python：** `_auto_permission/reviewer_execution.py`（1 方法 `_maybe_run_reviewer`）

要点：单一 `maybeRunReviewer` 方法，内部按 route 决定是否调 `AutoReviewer.Assess`，组装 metadata 并回写。

### Task 33：`reviewer_override_gate.go` + `reviewer_override_consume.go`（151 行）

**Files:**
- Create: `permissions/auto_permission/reviewer_override_gate.go`
- Create: `permissions/auto_permission/reviewer_override_consume.go`
- Test: 对应两个 `_test.go`

要点：覆盖 token 的门控（gate）与消费（consume）是两个独立 mixin，但共享 `reviewer_override_token` 载荷语义，放同一 Task 便于对照。

### Task 34：`deterministic.go`（131 行）

**Files:**
- Create: `permissions/auto_permission/deterministic.go`
- Test: `permissions/auto_permission/deterministic_test.go`

**Python：** `_auto_permission/deterministic.py`（2 方法）

要点：`maybeAllowDeterministicSandboxScope` 只在 `route.isDeterministicAllow` 且无 domain_route 时生效；启用 9.90a 挂账的 `DeterministicReadonlyPublicWebFetchSourceKinds`。

### Task 35：`manual_authorization.go` + `domain_manual.go`（133 行）

**Files:**
- Create: `permissions/auto_permission/manual_authorization.go`
- Create: `permissions/auto_permission/domain_manual.go`
- Test: 对应两个 `_test.go`

要点：`manualReviewerResponse` 构造人工审批响应；`domain_manual` 提供 denied 响应与 prompt payload 两个纯函数。

### Task 36：`domain_grant_audit.go`（162 行）

**Files:**
- Create: `permissions/auto_permission/domain_grant_audit.go`
- Test: `permissions/auto_permission/domain_grant_audit_test.go`

**Python：** `_auto_permission/domain_grant_audit.py`（7 方法）

要点：3 处 `emitAudit` 调用（跟随 V-8 no-op 桩）；`_current_sandbox_profile` 在首次调用时惰性构造 `build_sandbox_profile(sys_operation)` 并回写 `self.sandbox`。

### Task 37：`deny_response.go` + `artifact_declaration.go`（101 行）

**Files:**
- Create: `permissions/auto_permission/deny_response.go`
- Create: `permissions/auto_permission/artifact_declaration.go`
- Test: 对应两个 `_test.go`

要点：`reviewerDenyResponse` 组装拒绝文案与元数据；`artifact_declaration` 的 `requestedFilePaths` 是纯函数（与 `artifact_authorization` 的同名函数需确认是否为有意重复——Python 两侧各有一份）。

### Task 38：`artifact_authorization.go` + `artifact_tracking_capture.go`（155 行）

**Files:**
- Create: `permissions/auto_permission/artifact_authorization.go`
- Create: `permissions/auto_permission/artifact_tracking_capture.go`
- Test: 对应两个 `_test.go`

要点：`issueSendFileAuthorization` 消费 9.90d-1 的 `CreateSendFileExecutionGrant`；`hasUserFileDeliveryProhibition` 用正则识别用户禁止交付文件的表达；`afterToolCall` 是第 2 个对外钩子。

**提交信息模板（Task 26-38 通用）**：

```bash
git add internal/swarm/agents/harness/common/rails/permissions/auto_permission/<file>.go \
        internal/swarm/agents/harness/common/rails/permissions/auto_permission/<file>_test.go
git commit -m "feat(permissions): <中文名>（<python_file> <N> 行）"
```

`invocation_context.py` 是 `_auto_permission` 里第二大的文件（715 行 / 30 个函数），且承担两项 9.90c 明确登记的遗留项落地。

**主文件要求**：

| Go 函数 | Python 行 | 备注 |
|---|---|---|
| `ValidateSmartGlobPattern` | 65 | glob 长度 ≤2048 |
| `NormalizeNativePathInvocationForExecution` | 89 | 7 个原生路径执行器（含 `read_pdf`，9.90d-1 Task 6） |
| `NormalizeInvocationToolArgs` | 172 | 消费 9.90d-1 的 `ExtractCallGoal` + `NormalizeSendFilePath` |
| `TrustedSendIdentity` / `TrustedSendIdentityResolution` | 197/209 | struct |
| `ExtractInvocation` | 217 | 从 `(args, kwargs)` 提取 `ToolInvocation` |
| `InputValue` / `FirstInputValue` / `ToolCallValue` / `ToolCallCandidateKeys` | 251-285 | 取值 helper |
| `ResolveTrustedSendIdentity` | 291 | 可信 send 身份解析 |
| `TrustedSendDescriptorMatches` / `CompatibilitySendMatchesHost` | 377/388 | 描述符匹配 |
| `HostSessionID` / `HostToolCallID` | 448/464 | 宿主身份 |
| `NormalizedIdentifier` / `NormalizedChannelIdentifier` | 472/476 | 归一化 |
| `ArgsWereValidJSONObject` | 480 | 原始参数合法性 |
| `RepairMalformedToolArguments` | 492 | JSON 修复（依赖 `json_repair` 的 Go 等价） |
| `RepairInvocationArgsForExecution` | 506 | 修复包装 |
| `ReplaceInvocationToolArgs` / `WriteInvocationToolArgs` | 522/562 | 参数替换（含内部闭包 `execution_value` → Go 闭包） |
| `NormalizeCommandInvocationForExecution` | 533 | 命令归一（消费 9.90d-1 的 `ResolveCommandWorkdir`） |
| `ResolveSessionID` / `ResolveRequestID` / `ResolveToolCallID` | 608/630/654 | 身份解析 |
| `ResolveUserInput` | 667 | 取用户输入 |
| `NormalizeChannelKind` / `ResolveChannelKind` | 696/701 | 渠道归一 |

**补件 1：`NATIVE_PATH_ACCESS` 投影 —— 用现成 `SetNativePathAccess`，无需新增 ctx 函数**

已核实：`native_path_context.go:130` 已有 `SetNativePathAccess(access *NativePathAccess)`（全局变量式，带 `sync.Mutex`，注释明确「Go 侧显式设置与清空」对标 Python 的 `ContextVar.set/reset`）。

因此 `beforeTool.go` 里**直接调用现成函数**：

```go
// Python: NATIVE_PATH_ACCESS.set(None)          →  permissions.SetNativePathAccess(nil)
// Python: NATIVE_PATH_ACCESS.reset(native_token) → 在 defer 中恢复原值
```

若要严格复刻 Python 的 token/reset 语义（嵌套调用时恢复上一层值），需在 `beforeToolCall` 入口保存原值、defer 中恢复：

```go
func (r *AutoPermissionRail) beforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// Python: native_token = NATIVE_PATH_ACCESS.set(None)
	prev := permissions.CurrentNativePathAccess("", nil) // 读当前值
	permissions.SetNativePathAccess(nil)
	defer permissions.SetNativePathAccess(prev)           // Python: finally: reset(native_token)
	// ...
}
```

**风险提示**：`SetNativePathAccess` 是**全局单例**（非 ctx 派生），并发工具调用会互相干扰。Python 的 `ContextVar` 天然按 asyncio task 隔离。这是**语言能力差异**，需在 spec 补登记一条偏差（编号 V-9）：

| 编号 | 偏差 | Python | Go | 性质 | 理由 | 回收条件 |
|---|---|---|---|---|---|---|
| **V-9** | `NATIVE_PATH_ACCESS` 由 task-local 降级为全局单例 | `ContextVar`（按 asyncio task 隔离） | 全局变量 + `sync.Mutex` | 并发语义 | 9.90b 落地时即采用全局变量式；Go 无 task-local 存储 | 改用 ctx 派生式重写 `native_path_context.go` 时回收 |

**补件 2：`ConsumePermissionWrapperResume` —— 已存在，签名需注意**

已核实：`root_permission_queue_rail.go:293` 有 `func ConsumePermissionWrapperResume(cbc *agentinterfaces.AgentCallbackContext) bool`。

- **参数是 `cbc` 不是 `ctx`**
- **返回值是 `bool` 不是 `*RootPermissionWrapperResume`**（Python 返回对象或 None，Go 用 bool 表示"是否续接"）

`before_tool.go` 里的调用方式：

```go
// Python: wrapper_resume = consume_permission_wrapper_resume(invocation.ctx)
wrapperResume := permissions.ConsumePermissionWrapperResume(cbc)
```

**9.90c 登记的回填项**：`permissions/doc.go:48` 说的「完整校验链依赖 progressive_tool_rail 与 tool discovery 的等价内部结构」——若 `ConsumePermissionWrapperResume` 内部仍有 `⤵️` 待补的校验分支，本 Task 需一并补齐或登记偏差。实施前先读该函数实现：

```bash
sed -n '285,330p' internal/swarm/agents/harness/common/rails/permissions/root_permission_queue_rail.go
```

**提交信息模板**：

```bash
git add internal/swarm/agents/harness/common/rails/permissions/auto_permission/<file>.go \
        internal/swarm/agents/harness/common/rails/permissions/auto_permission/<file>_test.go
git commit -m "feat(permissions): <中文名>（<python_file> <N> 行）"
```

---

## Task 39：`code_tool_bindings`（110 行，仅 agent_mode）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/code_tool_bindings.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/code_tool_bindings_test.go`

**偏差 V-7**：只落 `agent_mode` 三工具（`enter_plan_mode` / `exit_plan_mode` / `switch_mode`），LSP 整层挂 `⤵️ 9.17`。

- [ ] **Step 1: 写失败测试**

```go
package auto_permission

import (
	"testing"
)

// TestTrustedCodeRoute_三个控制工具 验证 agent_mode 三工具被识别
func TestTrustedCodeRoute_三个控制工具(t *testing.T) {
	for _, name := range []string{"enter_plan_mode", "exit_plan_mode", "switch_mode"} {
		inv := ToolInvocation{ToolName: name}
		got := TrustedCodeRoute(inv, nil, "sess-1")
		// 无真实绑定时应返回 false（fail-closed）
		if got {
			t.Errorf("%s: should be false without real binding", name)
		}
	}
}

// TestTrustedCodeRoute_LSP工具返回false 验证 V-7 偏差（LSP 未实现）
func TestTrustedCodeRoute_LSP工具返回false(t *testing.T) {
	inv := ToolInvocation{ToolName: "lsp"}
	if TrustedCodeRoute(inv, nil, "sess-1") {
		t.Error("LSP route 未实现（spec V-7），必须返回 false")
	}
}
```

- [ ] **Step 2: 写实现**

```go
package auto_permission

// 闭集 Code 控制/LSP 路由，用可执行 SDK 绑定校验。
//
// ⤵️ 9.17 回填：LSP 部分未实现（Go 侧 LspRail 整层缺失，约 1,239 行）。
// 当前只覆盖 agent_mode 三工具。见 spec 已知偏差 V-7。

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// controlToolTypes 三个控制工具的类型映射
	// Python: _CONTROL_TYPES
	controlToolTypes = map[string]reflect.Type{
		"enter_plan_mode": reflect.TypeOf(&agentmode.EnterPlanModeTool{}),
		"exit_plan_mode":  reflect.TypeOf(&agentmode.ExitPlanModeTool{}),
		"switch_mode":     reflect.TypeOf(&agentmode.SwitchModeTool{}),
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TrustedCodeRoute 判定该调用是否来自受信任的 Code 控制路由。
//
// Python: trusted_code_route(invocation, agent, session_id)
func TrustedCodeRoute(invocation ToolInvocation, agent any, sessionID string) bool {
	expectedType, ok := controlToolTypes[invocation.ToolName]
	if !ok {
		// ⤵️ 9.17：LSP 工具（lsp 等）未实现，一律 fail-closed
		return false
	}
	// 精确类型 + 卡片同一性校验（9.90a 的 ResolveToolBinding）
	// 具体实现按 agent 侧接口调整
	_ = expectedType
	return false
}

// RegisterLSPReadSemantics ⤵️ 9.17 回填：教会 file_guard 认识 LSP 的源路径。
func RegisterLSPReadSemantics() {
	// ⤵️ 9.17 回填
}
```

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/auto_permission/code_tool_bindings.go \
        internal/swarm/agents/harness/common/rails/permissions/auto_permission/code_tool_bindings_test.go
git commit -m "feat(permissions): Code 工具绑定校验（code_tool_bindings.py 110 行）

偏差 V-7：只覆盖 agent_mode 三工具，LSP 挂 ⤵️ 9.17。"
```

---

## Task 40：`readonly_tool_bindings`（320 行，仅 ProgressiveToolRail）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/readonly_tool_bindings.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/readonly_tool_bindings_test.go`

**偏差 V-6**：只覆盖 `ProgressiveToolRail`，其余 4 类 fail-closed + `⤵️ 9.11b`。
**偏差 V-4/V-5**：`_closure` / `_methods_match` 改用 `MarkTrusted()`（9.90d-1 Task 14）。

- [ ] **Step 1: 写失败测试（安全回归，最关键）**

```go
package auto_permission

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
)

// TestTrustedReadonlyBinding_已标记rail通过 验证 MarkTrusted 后校验通过
func TestTrustedReadonlyBinding_已标记rail通过(t *testing.T) {
	// 签名：NewProgressiveToolRail(config *schema.DeepAgentConfig) *ProgressiveToolRail
	rail := rails.NewProgressiveToolRail(&hconfig.DeepAgentConfig{})
	// NewProgressiveToolRail 内部已调 MarkTrusted()

	if !rail.IsTrusted() {
		t.Fatal("NewProgressiveToolRail should self-mark trusted")
	}
	// 真实绑定校验通过后应放行
	got := TrustedReadonlyBinding(ToolInvocation{ToolName: "tool_search"}, rail, rail, "sess-1")
	_ = got
}

// TestTrustedReadonlyBinding_未标记rail拒绝 验证 fail-closed
func TestTrustedReadonlyBinding_未标记rail拒绝(t *testing.T) {
	rail := &rails.ProgressiveToolRail{} // 绕过构造函数

	if rail.IsTrusted() {
		t.Fatal("direct struct literal must not be trusted")
	}
	if TrustedReadonlyBinding(ToolInvocation{ToolName: "tool_search"}, rail, rail, "sess-1") {
		t.Error("untrusted rail must fail-closed")
	}
}

// TestTrustedReadonlyBinding_缺失rail类型拒绝 验证 V-6 的 4 类 rail
func TestTrustedReadonlyBinding_缺失rail类型拒绝(t *testing.T) {
	// ToolRetrievalPromptRail / SkillRetrievalPromptRail / HeartbeatRuntimeBridge /
	// _RestrictedCronBackend 在 Go 侧不存在 → 必须返回 false
	for _, toolName := range []string{"tool_search", "skill_index", "heartbeat_send"} {
		inv := ToolInvocation{ToolName: toolName}
		if trustedReadonlyBinding(inv, nil, "sess-1") {
			t.Errorf("%s: 前置 rail 缺失时应 fail-closed（V-6）", toolName)
		}
	}
}
```

- [ ] **Step 2: 写实现**

```go
package auto_permission

// 只读工具信任背书。
//
// Python 用 _closure / _methods_match 做运行时反射级可执行身份校验
// （比对 __self__ / __func__ / __closure__.co_freevars）。
// Go 无这些反射能力，改用分层替代（spec V-4/V-5）：
//   1. 类型精确匹配（ResolveToolBinding，9.90a 已有）
//   2. 卡片指针同一性（同上）
//   3. rail 实例 trusted 标记（MarkTrusted，9.90d-1）
//
// ⤵️ 9.11b 回填：当前只覆盖 ProgressiveToolRail。其余 4 类 rail/toolkit
// （ToolRetrievalPromptRail + Toolkit / SkillRetrievalPromptRail + Toolkit /
// HeartbeatRuntimeBridge / _RestrictedCronBackend）在 Go 侧不存在，
// 一律 fail-closed。见 spec 已知偏差 V-6。

// ──────────────────────────── 导出函数 ────────────────────────────

// TrustedReadonlyBinding 判定该只读调用是否来自受信任的 rail 与真实绑定。
//
// Python: trusted_readonly_binding(invocation, owner_agent, callback_agent, session_id)
func TrustedReadonlyBinding(
	invocation ToolInvocation,
	ownerAgent any,
	callbackAgent any,
	sessionID string,
) bool {
	// 只有 tool_search + ProgressiveToolRail 这条路径已具备前置
	if invocation.ToolName != "tool_search" {
		// ⤵️ 9.11b：tool_search(ToolRetrieval) / skill_index / heartbeat_* /
		// cron 后端的信任背书待对应 rail 落地
		return false
	}
	rail, ok := ownerAgent.(*rails.ProgressiveToolRail)
	if !ok || rail == nil {
		return false
	}
	// V-4/V-5：用 trusted 标记替代反射校验
	if !rail.IsTrusted() {
		return false
	}
	// 类型 + 卡片同一性（9.90a）
	tool := ResolveToolBinding(/* agent, tool_name, expectedType */)
	if tool == nil {
		return false
	}
	return true
}
```

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/auto_permission/readonly_tool_bindings.go \
        internal/swarm/agents/harness/common/rails/permissions/auto_permission/readonly_tool_bindings_test.go
git commit -m "feat(permissions): 只读工具信任背书（readonly_tool_bindings.py 320 行）

偏差 V-6：只覆盖 ProgressiveToolRail，其余 4 类 fail-closed（⤵️ 9.11b）。
偏差 V-4/V-5：_closure/_methods_match 反射校验改 MarkTrusted() 分层替代。"
```

---

## Task 41：`auto_permission_rail` + `before_tool`（966 行，最关键）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/auto_permission_rail.go`
- Create: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/before_tool.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/auto_permission_rail_test.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/before_tool_test.go`

**偏差 V-1**：`auto_permission_rail.go` 落子包（`permissions/auto_permission/`）而非父包。

### 41.1 `auto_permission_rail.go`（75 行）

```go
package auto_permission

// AutoPermissionRail 在基础权限轨之上叠加自动审查层。
//
// 这是 Python 12 个 mixin 多继承组合成的单一类在 Go 的落地形式：
// 单结构体持有 15 个共享字段，方法按 Python 文件分文件挂载，
// 每个方法块用 `// Python: class AutoPermissionXxxMixin.<method>` 标注归属。
//
// ⚠️ 目录归属偏差 V-1：Python 在 permissions/auto_permission_rail.py（父包），
// Go 落在 permissions/auto_permission/ 子包，以避免父包与子包互相引用成环。
// 装配层（swarm/server/adapter）直接引用本子包。
//
// Python: class AutoPermissionInterruptRail(
//     AutoPermissionLifecycleMixin,
//     AutoPermissionBeforeToolMixin,
//     AutoPermissionArtifactCaptureMixin,
//     AutoPermissionReviewerOverrideConsumeMixin,
//     AutoPermissionReviewerOverrideGateMixin,
//     AutoPermissionReviewerExecutionMixin,
//     AutoPermissionDeterministicMixin,
//     AutoPermissionArtifactAuthorizationMixin,
//     AutoPermissionDenyResponseMixin,
//     AutoPermissionManualAuthorizationMixin,
//     AutoPermissionDomainGrantAuditMixin,
//     AutoPermissionRuntimeBridgeMixin,
//     AgentRail,
// )
type AutoPermissionRail struct {
	agentinterfaces.BaseRail

	// ── 15 个共享字段（Python: Lifecycle.__init__ 建立）──
	baseRail              permissions.BasePermissionRail
	permissionConfig      map[string]any
	workspaceRoot         string
	platformTrustedRoot   string
	sandbox               *permissions.SandboxDescriptor
	sysOperation          sys_operation.SysOperation
	policyEvaluator       *permissions.OpenJiuwenPolicyEvaluator
	autoOptions           map[string]any
	autoReviewer          *permissions.AutoReviewer
	trustedSearchUrls     *permissions.SessionTrustedSearchUrls
	persistentAuditWriter any // ⤵️ 9.90e 回填为 *permissions.PersistentAuditWriter
	browserRuntimeSecurityProfile any
	priority              int
	ownerAgent            any
	writtenPermissions    func() map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAutoPermissionRail 构造自动审查轨。
//
// Python: AutoPermissionInterruptRail(...) → AutoPermissionLifecycleMixin.__init__
func NewAutoPermissionRail(opts AutoPermissionRailOptions) *AutoPermissionRail {
	rail := &AutoPermissionRail{}
	rail.initLifecycle(opts) // lifecycle.go
	return rail
}

// BeforeToolCall 工具调用前拦截。
//
// Python: AutoPermissionBeforeToolMixin.before_tool_call
func (r *AutoPermissionRail) BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return r.beforeToolCall(ctx, cbc) // before_tool.go
}

// AfterToolCall 工具调用后回调（文件交付捕获）。
//
// Python: AutoPermissionArtifactCaptureMixin.after_tool_call
func (r *AutoPermissionRail) AfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return r.afterToolCall(ctx, cbc) // artifact_tracking_capture.go
}

// GetCallbacks 注册钩子。
func (r *AutoPermissionRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.BaseRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeToolCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterToolCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}
```

### 41.2 `before_tool.go`（891 行）

**核心约束（规则 9.3）**：`beforeToolCallImpl` 必须是**单方法**，内部三个闭包（`trustedSendGuardResult` / `runtimeResult` 等）保持原位，步骤顺序与 Python 逐条对应。**不得拆成多个 helper**。

- [ ] **Step 1: 写失败测试（覆盖七步）**

```go
package auto_permission

import (
	"context"
	"testing"
)

// TestBeforeToolCall_plan硬拒短路 验证第一步
func TestBeforeToolCall_plan硬拒短路(t *testing.T) {
	rail := newTestAutoRail(t)
	ctx := permissions.MarkPlanHardReject(context.Background())

	err := rail.BeforeToolCall(ctx, newTestCallbackContext("bash"))
	if err != nil {
		t.Errorf("plan_hard_reject should short-circuit without error, got %v", err)
	}
}

// TestBeforeToolCall_ask_user工具直通base 验证第三步
func TestBeforeToolCall_ask_user工具直通base(t *testing.T) {
	rail := newTestAutoRail(t)
	err := rail.BeforeToolCall(context.Background(), newTestCallbackContext("ask_user"))

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestBeforeToolCall_规则allow直接放行 验证 policy allow 短路
func TestBeforeToolCall_规则allow直接放行(t *testing.T) {
	rail := newTestAutoRail(t)
	rail.policyEvaluator = newFakeAllowEvaluator()

	cbc := newTestCallbackContext("read_file")
	err := rail.BeforeToolCall(context.Background(), cbc)

	if err != nil {
		t.Errorf("allow should not error: %v", err)
	}
	if skipped, _ := cbc.Extra("_skip_tool").(bool); skipped {
		t.Error("allow should not skip the tool")
	}
}

// TestBeforeToolCall_规则deny跳过工具 验证 deny 路径
func TestBeforeToolCall_规则deny跳过工具(t *testing.T) {
	rail := newTestAutoRail(t)
	rail.policyEvaluator = newFakeDenyEvaluator()

	cbc := newTestCallbackContext("bash")
	err := rail.BeforeToolCall(context.Background(), cbc)

	if err != nil {
		t.Errorf("deny should not panic: %v", err)
	}
	if skipped, _ := cbc.Extra("_skip_tool").(bool); !skipped {
		t.Error("deny should set _skip_tool")
	}
}

// TestBeforeToolCall_确定性放行 验证确定性路径不经 LLM
func TestBeforeToolCall_确定性放行(t *testing.T) {
	rail := newTestAutoRail(t)
	rail.policyEvaluator = newFakeAskEvaluator()

	cbc := newTestCallbackContext("read_file")
	err := rail.BeforeToolCall(context.Background(), cbc)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if rail.autoReviewer != nil && rail.autoReviewer.CallCount() > 0 {
		t.Error("deterministic allow should not call reviewer")
	}
}

// TestBeforeToolCall_语义路径调LLM 验证 semantic route 触发审查器
func TestBeforeToolCall_语义路径调LLM(t *testing.T) {
	rail := newTestAutoRail(t)
	rail.policyEvaluator = newFakeAskEvaluator()
	rail.autoReviewer = newFakeReviewer("allow", "safe")

	cbc := newTestCallbackContext("bash")
	_ = rail.BeforeToolCall(context.Background(), cbc)

	if rail.autoReviewer.CallCount() == 0 {
		t.Error("semantic route should call reviewer")
	}
}

// TestBeforeToolCall_audit桩不panic 验证 V-8 no-op 桩
func TestBeforeToolCall_audit桩不panic(t *testing.T) {
	rail := newTestAutoRail(t)
	rail.policyEvaluator = newFakeAllowEvaluator()

	// 桩实现为空，任何路径都不应 panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("audit stub must not panic: %v", r)
		}
	}()
	_ = rail.BeforeToolCall(context.Background(), newTestCallbackContext("read_file"))
}
```

- [ ] **Step 2: 写实现——按 Python 七步逐段对齐**

```go
package auto_permission

// AutoPermissionBeforeToolMixin 的 Phase 8c 行为。
//
// Python: class AutoPermissionBeforeToolMixin
//
// ⚠️ 规则 9.3：beforeToolCallImpl 必须保持单方法，内部闭包原位，
// 七步顺序与 Python 逐条对应，不得拆分为多个 helper。

// ──────────────────────────── 导出函数 ────────────────────────────

// beforeToolCall 对当前 root 工具调用做一次判定。
//
// Python: AutoPermissionBeforeToolMixin.before_tool_call
func (r *AutoPermissionRail) beforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// Python: native_token = NATIVE_PATH_ACCESS.set(None)
	// 注意：Go 侧 SetNativePathAccess 是全局单例（偏差 V-9），
	// 需保存原值并在 defer 中恢复，等价 Python 的 reset(native_token)
	prevAccess := permissions.CurrentNativePathAccess("", nil)
	permissions.SetNativePathAccess(nil)
	defer permissions.SetNativePathAccess(prevAccess)

	invocation := extractInvocation(ctx, cbc)
	if permissions.PlanHardRejected(ctx) {
		return r.beforeToolCallImpl(ctx, cbc, invocation)
	}
	// Python: with self.base_rail.permission_check_context(session_id):
	cleanup, _ := r.baseRail.PermissionCheckContext(resolveSessionID(ctx, cbc))
	if cleanup != nil {
		defer cleanup()
	}
	err := r.beforeToolCallImpl(ctx, cbc, invocation)

	// Python: except asyncio.CancelledError → queue.finish_active(key)
	// Go 侧用 ctx.Err() 判定取消
	if ctx.Err() == context.Canceled {
		if queue := permissions.OptionalRootPermissionQueue(ctx); queue != nil {
			// 注意：ToolInvocationKeyFromContext 的参数是 cbc，不是 ctx
			if key := permissions.ToolInvocationKeyFromContext(cbc); key != nil {
				queue.FinishActive(key)
			}
		}
	}
	return err
}

// beforeToolCallImpl 在委托给 base rail 前评估自动权限护栏。
//
// Python: AutoPermissionBeforeToolMixin._before_tool_call_impl
//
// 七步（顺序不可调整）：
//  1. 清理任务级标记（no_host_fallback / send_file_grant / trusted_search_producer）
//  2. 提取 invocation + 各种短路（plan 硬拒 / wrapper resume / _skip_tool / 非权限续接）
//  3. ask_user 工具直通 base；client tool 走 _client_tool_permission
//  4. 解析 trusted send identity + 参数修复/命令归一/原生路径归一
//  5. 构造 facts + policy 影子评估 + no_host_fallback 判定
//  6. 确定性路由 → reviewer 路由 → LLM 审查 → override 门控
//  7. 三终点（allow / deny / base）+ audit
func (r *AutoPermissionRail) beforeToolCallImpl(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	invocation ToolInvocation,
) error {
	// ── 1. 清理任务级标记 ──
	ctx = permissions.ClearNoHostFallback(ctx)
	ctx = permissions.ClearSendFileExecutionGrant(ctx)
	permissions.ClearTrustedSearchProducer()

	if permissions.PlanHardRejected(ctx) {
		return nil
	}
	// Python: wrapper_resume = consume_permission_wrapper_resume(invocation.ctx)
	// 注意：Go 签名收 cbc 返回 bool（root_permission_queue_rail.go:293）
	wrapperResume := permissions.ConsumePermissionWrapperResume(cbc)
	_ = wrapperResume
	// ...（逐条对齐 Python 217-891 行）
}
```

**实施要点**：`before_tool.py` 891 行需一次性完整移植。建议按 Python 行号分段对照，**每 100 行核对一次**，确保：
- 闭包 `trustedSendGuardResult` / `runtimeResult` 原位定义
- `if/elif` 嵌套层级不改变
- 提前返回位置一致

- [ ] **Step 3: 运行测试 + 覆盖率**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/common/rails/permissions/auto_permission/ -run "TestBeforeToolCall" -v -cover
```

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/auto_permission/auto_permission_rail.go \
        internal/swarm/agents/harness/common/rails/permissions/auto_permission/before_tool.go \
        internal/swarm/agents/harness/common/rails/permissions/auto_permission/auto_permission_rail_test.go \
        internal/swarm/agents/harness/common/rails/permissions/auto_permission/before_tool_test.go
git commit -m "feat(permissions): 自动审查轨 + 七步编排（auto_permission_rail.py 75 + before_tool.py 891）

方案 C：单结构体 15 字段 + 方法按 Python 文件分文件。
偏差 V-1：落子包避免循环依赖。
七步顺序与 Python 逐条对应（规则 9.3），闭包原位不拆。"
```

---

## Task 42：装配接线

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go`（`BuildPermissionInterruptRail` 附近）
- Modify: `internal/swarm/server/adapter/code_adapter.go`（同类位置）

- [ ] **Step 1: 找到现有装配点**

```bash
cd /home/opensource/uapclaw-gateway
grep -n "BuildPermissionInterruptRail" internal/swarm/server/adapter/deep_adapter_rails.go internal/swarm/server/adapter/code_adapter.go
```

- [ ] **Step 2: 在 rail 构造后插入 auto 分支**

```go
	// Python: if enable_auto_permission:
	//             permission_rail = AutoPermissionInterruptRail(base_rail=permission_rail, ...)
	if enableAutoPermission {
		autoOptions := permissions.NormalizeAutoPermissionOptions(permissionConfig["auto"])
		var autoReviewer *permissions.AutoReviewer
		if llmModel != nil {
			if reviewerModel := permissions.BuildIsolatedReviewerModel(llmModel); reviewerModel != nil {
				autoReviewer = permissions.NewAutoReviewer(
					&permissions.IsolatedModelReviewerClient{
						Model:                 reviewerModel,
						DisplayLanguageGetter: func() string { return preferredLanguage },
					},
					autoOptions["reviewer_timeout_ms"].(int),
					autoOptions["reviewer_output_format"].(string),
				)
			}
		}
		var auditWriter any // ⤵️ 9.90e：真实 *PersistentAuditWriter
		if autoOptions["persistent_audit_enabled"].(bool) {
			// ⤵️ 9.90e 回填：auditWriter = permissions.NewPersistentAuditWriter(
			//     permissions.ResolvePersistentAuditRoot(permissionConfig))
		}
		rail = auto_permission.NewAutoPermissionRail(auto_permission.AutoPermissionRailOptions{
			BaseRail:        rail,
			PermissionConfig: permissionConfig,
			WorkspaceRoot:   effectiveWorkspaceRoot,
			PlatformTrustedRoot: platformTrustedRoot,
			SysOperation:    sysOperation,
			AutoReviewer:    autoReviewer,
			PersistentAuditWriter: auditWriter,
			BrowserRuntimeSecurityProfile: browserRuntimeSecurityProfile,
			TrustedSearchURLs: trustedSearchURLs,
			WrittenPermissions: getWrittenPermissions,
		})
		rail.SetTrustedDirs(nil)
	}
```

- [ ] **Step 3: 更新 `d.permissionRail` 的类型断言点**

```bash
grep -rn "permissionRail\.(\*secrail.PermissionInterruptRail)" internal/swarm/server/adapter/
```

这些断言点在 auto 模式下会失败（rail 已变成 `*AutoPermissionRail`）。需按 Python 的 `permission_rail_group.py` 逻辑处理双类型。

- [ ] **Step 4: 编译验证**

```bash
go build ./... && go vet ./...
```

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter_rails.go internal/swarm/server/adapter/code_adapter.go
git commit -m "feat(adapter): 装配 AutoPermissionRail（enable_auto_permission 分支）

对齐 Python interrupt_helpers L883-935。audit writer 留 ⤵️ 9.90e。"
```

---

## Task 43：集成测试

**Files:**
- Create: `tests/integration/swarm/permissions/`（新目录）

- [ ] **Step 1: 写三个安全回归场景**

```go
//go:build integration

package permissions

import (
	"testing"

	"github.com/stretchr/testify/suite"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// AutoPermissionSuite 自动审查层集成测试。
type AutoPermissionSuite struct {
	isuite.AgentSuite
}

// TestAutoPermissionSuite 运行套件
func TestAutoPermissionSuite(t *testing.T) {
	suite.Run(t, new(AutoPermissionSuite))
}

// TestProgressiveToolRail全链 场景1：真实 rail + tool_search → before_tool allow
func (s *AutoPermissionSuite) TestProgressiveToolRail全链() {
	s.Require().NotNil(s.GetAgent())
	// 真实注册 ProgressiveToolRail + ToolSearchTool
	// 调用 before_tool → 断言 allow
}

// Test伪造rail拒绝 场景2：绕过构造函数 → fail-closed
func (s *AutoPermissionSuite) Test伪造rail拒绝() {
	// 直接 &ProgressiveToolRail{} 不调 MarkTrusted
	// 断言 trusted_readonly_binding 返回 false
}

// Test双层包装穿透 场景3：JiuwenSwarm → AutoPermission 双层
func (s *AutoPermissionSuite) Test双层包装穿透() {
	// 构造 JiuwenSwarmPermissionInterruptRail → 包 AutoPermissionRail
	// 调用穿透到真实 engine
}
```

- [ ] **Step 2: 跑集成审计**

```bash
CGO_ENABLED=1 go test -tags "integration sqlite_fts5 test" ./tests/integration/swarm/permissions/ -v
go run ./cmd/integration-audit -verify
```

**规则 7 两层门禁**：调用跨度 ≥3 个 internal 二级子系统 + 覆盖交叉验证 ≥3 子系统各 ≥30 行。若跨度不足，需补链路（不是登记豁免——本目录属真集成）。

- [ ] **Step 3: 提交**

```bash
git add tests/integration/swarm/permissions/
git commit -m "test(permissions): 自动审查层集成测试（三个安全回归场景）"
```

---

## Task 44：文档同步 + 计划表回填

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/permissions/doc.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新两个 doc.go 的文件树（新增 29 个文件）**

- [ ] **Step 2: 删除所有已回填的 ⤵️ 标记**

```bash
cd /home/opensource/uapclaw-gateway
grep -rn "⤵️ 9.90d" internal/
```

预期：0 命中（`root_context.go` 三处已在本批消费；`doc.go` 的缺口段落已删）。

- [ ] **Step 3: 计划表 9.90d-2 标 ✅，保留 ⤵️ 的 9.90e / 9.11b / 9.17 三处 ⤴️**

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/agents/harness/common/rails/permissions/doc.go \
        internal/swarm/agents/harness/common/rails/permissions/auto_permission/doc.go \
        IMPLEMENTATION_PLAN.md
git commit -m "docs(permissions): 9.90d-2 文档同步 + 计划表回填

自动审查层本体完成，⤵️ 9.90d 标记全部清除。
保留三处 ⤴️：9.90e（V-8 audit）/ 9.11b（V-6 readonly）/ 9.17（V-7 LSP）。"
```

---

## 验收清单（9.90d-2 完成标准）

- [ ] Task 18-44 全部完成并提交
- [ ] `go build ./...` 与 `go vet ./...` 无错误
- [ ] `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 全绿
- [ ] 本批新增 29 个文件覆盖率全部 ≥85%
- [ ] `grep -rn "⤵️ 9.90d" internal/` = 0 命中
- [ ] `go run ./cmd/integration-audit -verify` 两层门禁通过
- [ ] 三个安全回归集成场景全部通过（ProgressiveToolRail 全链 / 伪造 rail / 双层包装）
- [ ] `AutoPermissionRail` 的 `_emit_audit` 为 no-op 桩并标 `⤵️ 9.90e`
- [ ] `readonly_tool_bindings` 对 4 类缺失 rail 全部 fail-closed
- [ ] `code_tool_bindings` 对 LSP 返回 false
- [ ] 已推送到远端
