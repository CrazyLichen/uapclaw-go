# 9.90a-2 auto_permission models + tool_binding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `internal/swarm/agents/harness/common/rails/permissions/auto_permission/` 新建子包，实现 `models.py`（三个数据契约 + 11 常量）与 `tool_binding.py`（实现身份背书）的 Go 对齐，并把计划表 9.90a 收口为 ✅。

**Architecture:** Python 的 `_auto_permission/models.py`（101 行）是自动审查层的数据契约与共享常量，`tool_binding.py`（25 行）是「这个工具实现是不是我信任的那个」的身份校验。两者都不产生决策，只提供类型与判定原语。Go 侧按规则 9.1 建同名子包（去下划线前缀），一文件对一文件。`tool_binding` 的两处 Python `is` 语义（`type(x) is T` 精确类型、`resource.card is card` 指针同一性）在 Go 用 `reflect.TypeOf` 与指针 `==` 表达。

**Tech Stack:** Go 1.x、`reflect`、`regexp`（RE2）、`testing`、`github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool`、`internal/agentcore/runner`、`internal/agentcore/single_agent/{interfaces,schema}`

**Spec:** `docs/superpowers/specs/2026-11-07-permissions-models-tool-binding-design.md`

---

## 文件结构

| 文件 | 职责 |
|---|---|
| `internal/swarm/agents/harness/common/rails/permissions/auto_permission/doc.go` | 包文档：本包在权限链路中的位置、与 9.90b/d 的关系、文件目录、零消费常量的 ⤵️ 去向 |
| `internal/swarm/agents/harness/common/rails/permissions/auto_permission/models.go` | `ToolInvocation` / `PermissionHandlingResult` / `PermissionInterruptRequest` 三个类型 + 11 个共享常量（含 3 个 RE2 正则） |
| `internal/swarm/agents/harness/common/rails/permissions/auto_permission/models_test.go` | models 的单元测试 |
| `internal/swarm/agents/harness/common/rails/permissions/auto_permission/tool_binding.go` | `MatchesBoundMethod` / `ResolveToolBinding` 两个函数 + `resolveToolByID` 注入点 |
| `internal/swarm/agents/harness/common/rails/permissions/auto_permission/tool_binding_test.go` | tool_binding 的单元测试（含 fake 设施） |
| `internal/swarm/agents/harness/common/rails/permissions/doc.go` | Modify：文件目录补 `auto_permission/` 子包条目 + ⤵️ 标注 |
| `IMPLEMENTATION_PLAN.md` | Modify：9.90a 行 `☐` → `✅`，注明提交来源，4 个零消费常量挂 ⤵️ |

**依赖方向**：`auto_permission` → `agentcore`（无环，已实测 `agentcore` 无任何文件 import `internal/swarm`）。与父包 `permissions` **互不 import**（父包不知道子包存在，子包也不需要父包）。

---

## Task 1: 建子包骨架与 doc.go

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/doc.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package auto_permission 提供自动权限审查层（Auto Permission）的共享契约与身份原语。
//
// 本包对齐 Python jiuwenswarm/agents/harness/common/rails/permissions/_auto_permission/
// 与同级 tool_binding.py。目录名去掉 Python 的下划线前缀——下划线在 Python 表示
// "内部子包"，Go 无此约定且导出包名不应以下划线开头。
//
// # 在权限链路中的位置
//
// 本包是 9.90a（基础类型层）的最后一块，位于 PermissionEngine 规则判定返回 ASK
// 之后、9.90b（决策事实层）之前：
//
//	PermissionEngine 规则判定
//	        ↓  ASK
//	【本包】数据契约 + 身份背书        ← 不产生决策
//	        ↓
//	9.90b 决策事实层（tool_capabilities / network_scope / policy_eval）
//	        ↓
//	9.90c root 五文件（串行队列 + 意图窗口）  ← 已完成
//	        ↓
//	9.90d 自动审查层（auto_reviewer / before_tool / adaptive 系列）
//
// # 两个文件的分工
//
//   - models.go：三个数据契约（ToolInvocation 工具调用快照、PermissionHandlingResult
//     处理结果 + 可信来源、PermissionInterruptRequest 权限中断请求）与 11 个共享常量。
//   - tool_binding.go：实现身份校验。回答"我要放行的这个工具，是不是我以为的那个
//     实现"——实现类必须精确匹配、卡片必须指针同一，任一不满足即判不合格。
//     Python 原注释：Subclasses can replace execution; this is implementation
//     identity, not assignability.
//
// # 已知缺口
//
// ⤵️ 以下 4 个常量在 Python 全仓（含 tests）零消费，本包照抄但不被引用，
// 待 9.90d 的 reviewer_redaction / deterministic 启用后成为活跃代码：
//   - ShellDisplayPathPattern
//   - ShellDisplayPreSplitPathPattern
//   - SensitiveDisplayPathPattern
//   - DeterministicReadonlyPublicWebFetchSourceKinds
//
// 保留理由：规则 9.2-4 要求 Enum/常量成员集合完整映射，不得因"Go 侧用不到"裁剪。
//
// ⤵️ 其余常量（ReviewerUITextMaxLength / ReviewerUIShortTextMaxLength /
// ReviewerDenialGuidance / ProhibitedFileDeliveryReason /
// DeterministicBoundedScopeDecisionSource / DeterministicReadonlyPublicWebReason）
// 的消费方在 9.90d 的 reviewer_metadata / deny_response / before_tool / deterministic。
//
// 文件目录：
//
//	auto_permission/
//	├── doc.go              # 包文档
//	├── models.go           # ToolInvocation / PermissionHandlingResult / PermissionInterruptRequest + 11 常量
//	└── tool_binding.go     # MatchesBoundMethod / ResolveToolBinding——实现身份背书
//
// 对应 Python 代码：
//   - jiuwenswarm/agents/harness/common/rails/permissions/_auto_permission/models.py（101 行）
//   - jiuwenswarm/agents/harness/common/rails/permissions/tool_binding.py（25 行）
package auto_permission
```

- [ ] **Step 2: 验证包可编译（此时无实现文件，仅有 doc.go）**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/agents/harness/common/rails/permissions/auto_permission/`
Expected: 无输出（成功）。若报 "no Go files" 说明 doc.go 未落盘或包名不匹配

- [ ] **Step 3: Commit**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/swarm/agents/harness/common/rails/permissions/auto_permission/doc.go
git commit -m "feat(permissions): 建立 auto_permission 子包 + doc.go

对齐 Python _auto_permission/ 子目录（去下划线前缀）。
doc.go 记录本包在权限链路中的位置（PermissionEngine ASK 之后、9.90b 之前）、
两个文件的分工、以及 4 个零消费常量的 ⤵️ 去向（待 9.90d 启用）。"
```

---

## Task 2: models.go 的三个数据契约（TDD）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/models.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/models_test.go`

先写类型，常量在 Task 3、4 补。本任务只做三个类型。

- [ ] **Step 1: 写失败测试**

创建 `models_test.go`：

```go
package auto_permission

import (
	"testing"

	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// TestToolInvocation_字段可赋值 验证四个字段的类型与读写
func TestToolInvocation_字段可赋值(t *testing.T) {
	inv := ToolInvocation{
		Ctx:      nil,
		ToolCall: map[string]any{"name": "read_file"},
		ToolName: "read_file",
		ToolArgs: map[string]any{"path": "/tmp/a.txt"},
	}
	if inv.ToolName != "read_file" {
		t.Fatalf("ToolName 应为 read_file，实际 %q", inv.ToolName)
	}
	if got := inv.ToolArgs["path"]; got != "/tmp/a.txt" {
		t.Fatalf("ToolArgs[path] 应为 /tmp/a.txt，实际 %v", got)
	}
}

// TestToolInvocation_ToolCall接受鸭子类型 验证 ToolCall 是全 any
//
// Python invocation_context.py:240 会塞 SimpleNamespace 替身而非真 ToolCall，
// 故 Go 侧必须是 any。
func TestToolInvocation_ToolCall接受鸭子类型(t *testing.T) {
	type simpleNamespace struct {
		name      string
		arguments map[string]any
	}
	inv := ToolInvocation{
		ToolCall: simpleNamespace{name: "read_file", arguments: map[string]any{"p": "x"}},
		ToolName: "read_file",
	}
	if _, ok := inv.ToolCall.(simpleNamespace); !ok {
		t.Fatalf("ToolCall 应能承载任意类型（鸭子类型替身），实际 %T", inv.ToolCall)
	}
}

// TestNewPermissionHandlingResult_正常构造 验证未处理且无来源时可构造
func TestNewPermissionHandlingResult_正常构造(t *testing.T) {
	res, err := NewPermissionHandlingResult(false, nil, "")
	if err != nil {
		t.Fatalf("handled=false 且 source 为空应允许，实际报错 %v", err)
	}
	if res.Handled {
		t.Fatalf("Handled 应为 false")
	}
	if res.Result != nil {
		t.Fatalf("Result 应为 nil")
	}
	if res.DecisionSource != "" {
		t.Fatalf("DecisionSource 应为空串")
	}
}

// TestNewPermissionHandlingResult_handled无来源报错 验证 post_init 校验
//
// Python: if self.handled and not self.decision_source: raise ValueError
func TestNewPermissionHandlingResult_handled无来源报错(t *testing.T) {
	res, err := NewPermissionHandlingResult(true, nil, "")
	if err == nil {
		t.Fatalf("handled=true 且 source 为空必须报错")
	}
	if res != nil {
		t.Fatalf("报错时应返回 nil，实际 %v", res)
	}
	if err.Error() != "handled permission result requires a decision source" {
		t.Fatalf("错误文案应与 Python 逐字一致，实际 %q", err.Error())
	}
}

// TestNewPermissionHandlingResult_handled有来源 验证正常路径
func TestNewPermissionHandlingResult_handled有来源(t *testing.T) {
	res, err := NewPermissionHandlingResult(true, map[string]any{"ok": true}, "deterministic_bounded_scope")
	if err != nil {
		t.Fatalf("handled=true 且有来源应允许，实际报错 %v", err)
	}
	if !res.Handled || res.DecisionSource != "deterministic_bounded_scope" {
		t.Fatalf("字段值不符：%+v", res)
	}
}

// TestPermissionInterruptRequest_嵌入字段可见 验证嵌入 InterruptRequest 后字段可读写
func TestPermissionInterruptRequest_嵌入字段可见(t *testing.T) {
	req := &PermissionInterruptRequest{
		InterruptRequest: saschema.InterruptRequest{
			Message:       "Approve",
			InterruptKind: saschema.InterruptKindPermission,
		},
	}
	if req.Message != "Approve" {
		t.Fatalf("嵌入字段 Message 应可见，实际 %q", req.Message)
	}
	if req.InterruptKind != saschema.InterruptKindPermission {
		t.Fatalf("InterruptKind 应为 permission，实际 %q", req.InterruptKind)
	}
}

// TestPermissionInterruptRequest_满足InterruptRequester 验证可当接口用
func TestPermissionInterruptRequest_满足InterruptRequester(t *testing.T) {
	req := &PermissionInterruptRequest{
		InterruptRequest: saschema.InterruptRequest{InterruptKind: saschema.InterruptKindPermission},
	}
	var _ saschema.InterruptRequester = req
	if req.GetInterruptKind() != saschema.InterruptKindPermission {
		t.Fatalf("GetInterruptKind 应为 permission，实际 %q", req.GetInterruptKind())
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/auto_permission/ -run TestToolInvocation -v`
Expected: FAIL，编译错误 `undefined: ToolInvocation`

- [ ] **Step 3: 写实现**

创建 `models.go`：

```go
// 自动权限审查层的共享数据契约。
//
// 三个类型分别是：工具调用快照、权限处理结果、权限中断请求。
// 它们都不含判定逻辑，只是 9.90d 各文件之间传递的载体。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/permissions/_auto_permission/models.py
package auto_permission

import (
	"errors"

	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolInvocation 从 rail 回调输入中提取的规范化工具调用。
//
// 四个字段的类型取舍（详见 spec §3.1.2）：
//   - Ctx 收窄为 *AgentCallbackContext：消费方全部直接使用 .Agent() / .Session()，
//     与 ToolCallInputs.Ctx 同源。
//   - ToolCall 必须保留 any：Python invocation_context.py:240 会塞
//     SimpleNamespace(name=..., arguments=...) 替身，消费方只用 getattr 式读，
//     收窄为 *llmschema.ToolCall 将无法表达该替身。
//   - ToolArgs 收窄为 map[string]any：对齐 ToolCallInputs.ToolArgs，构造点全是 dict。
//
// Python: @dataclass(frozen=True) ToolInvocation
type ToolInvocation struct {
	// Ctx 回调上下文
	Ctx *agentinterfaces.AgentCallbackContext
	// ToolCall 原始工具调用对象（可能是鸭子类型替身，故为 any）
	ToolCall any
	// ToolName 工具名称
	ToolName string
	// ToolArgs 工具参数字典
	ToolArgs map[string]any
}

// PermissionHandlingResult 已处理结果，以及该决策的可信宿主来源。
//
// Handled 为 true 时 DecisionSource 必须非空，用 NewPermissionHandlingResult 保证。
//
// Python: @dataclass(frozen=True) PermissionHandlingResult
type PermissionHandlingResult struct {
	// Handled 是否已处理（true 表示不再走后续判定）
	Handled bool
	// Result 处理结果（具体形状由决策来源决定）
	Result any
	// DecisionSource 该决策的可信宿主来源标识
	DecisionSource string
}

// PermissionInterruptRequest 保留权限元数据供 UI 渲染的中断请求。
//
// 构造约定：InterruptKind 置为 saschema.InterruptKindPermission。
// 若需挂动态字段，走已存在的 InterruptRequest.Metadata，不要新增字段。
//
// Python: class PermissionInterruptRequest(InterruptRequest)
//
//	model_config = {"extra": "allow"}
//	interrupt_kind: str = InterruptKind.PERMISSION
type PermissionInterruptRequest struct {
	saschema.InterruptRequest
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPermissionHandlingResult 构造并校验权限处理结果。
//
// 校验与 Python 的 __post_init__ 逐条一致：handled 为 true 而来源为空即报错，
// 防止下游拿到"已处理"却无从判断是谁处理的。
//
// Python: PermissionHandlingResult.__post_init__
func NewPermissionHandlingResult(handled bool, result any, decisionSource string) (*PermissionHandlingResult, error) {
	if handled && decisionSource == "" {
		return nil, errors.New("handled permission result requires a decision source")
	}
	return &PermissionHandlingResult{
		Handled:        handled,
		Result:         result,
		DecisionSource: decisionSource,
	}, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/auto_permission/ -v`
Expected: 全部 PASS（6 个用例）

若 `PermissionInterruptRequest` 未自动获得 `GetInterruptKind` 等接口方法，说明 `InterruptRequest` 的方法是值接收者且嵌入正确——`saschema.InterruptRequester` 断言失败会编译报错，需检查 `response.go` 中方法签名。

- [ ] **Step 5: Commit**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/swarm/agents/harness/common/rails/permissions/auto_permission/models.go internal/swarm/agents/harness/common/rails/permissions/auto_permission/models_test.go
git commit -m "feat(permissions): models 三个数据契约

对齐 Python _auto_permission/models.py 的三个类型：
- ToolInvocation：Ctx 收窄为 *AgentCallbackContext，ToolCall 保留 any
  （Python invocation_context.py:240 会塞 SimpleNamespace 替身）
- PermissionHandlingResult + NewPermissionHandlingResult：承载 __post_init__
  校验（handled 而来源为空即报错）
- PermissionInterruptRequest：嵌入 InterruptRequest，构造时置 InterruptKind"
```

---

## Task 3: models.go 的 8 个标量常量（TDD）

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/models.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/models_test.go`

- [ ] **Step 1: 写失败测试**

在 `models_test.go` 末尾追加：

```go
// ──────────────────────────── 常量测试 ────────────────────────────

// TestReviewerActionTargetMaxLength_取值 验证与 Python 一致
func TestReviewerActionTargetMaxLength_取值(t *testing.T) {
	if ReviewerActionTargetMaxLength != 160 {
		t.Fatalf("应 160，实际 %d", ReviewerActionTargetMaxLength)
	}
}

// TestReviewerUITextMaxLength_取值 验证与 Python 一致
func TestReviewerUITextMaxLength_取值(t *testing.T) {
	if ReviewerUITextMaxLength != 1024 {
		t.Fatalf("应 1024，实际 %d", ReviewerUITextMaxLength)
	}
}

// TestReviewerUIShortTextMaxLength_取值 验证与 Python 一致
func TestReviewerUIShortTextMaxLength_取值(t *testing.T) {
	if ReviewerUIShortTextMaxLength != 240 {
		t.Fatalf("应 240，实际 %d", ReviewerUIShortTextMaxLength)
	}
}

// TestReviewerDenialGuidance_内容 验证与 Python 逐字一致
func TestReviewerDenialGuidance_内容(t *testing.T) {
	want := "Do not retry, rephrase, split, or switch tools to bypass this denial. " +
		"A materially changed user intent requires a new tool call and full review."
	if ReviewerDenialGuidance != want {
		t.Fatalf("内容与 Python 不一致\nwant: %q\ngot:  %q", want, ReviewerDenialGuidance)
	}
}

// TestProhibitedFileDeliveryReason_取值 验证与 Python 一致
func TestProhibitedFileDeliveryReason_取值(t *testing.T) {
	if ProhibitedFileDeliveryReason != "file_delivery_prohibited_by_user" {
		t.Fatalf("应 file_delivery_prohibited_by_user，实际 %q", ProhibitedFileDeliveryReason)
	}
}

// TestDeterministicBoundedScopeDecisionSource_取值 验证与 Python 一致
func TestDeterministicBoundedScopeDecisionSource_取值(t *testing.T) {
	if DeterministicBoundedScopeDecisionSource != "deterministic_bounded_scope" {
		t.Fatalf("应 deterministic_bounded_scope，实际 %q", DeterministicBoundedScopeDecisionSource)
	}
}

// TestDeterministicReadonlyPublicWebReason_取值 验证与 Python 一致
func TestDeterministicReadonlyPublicWebReason_取值(t *testing.T) {
	if DeterministicReadonlyPublicWebReason != "deterministic_readonly_public_web" {
		t.Fatalf("应 deterministic_readonly_public_web，实际 %q", DeterministicReadonlyPublicWebReason)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/auto_permission/ -run 'TestReviewer|TestProhibited|TestDeterministic' -v`
Expected: FAIL，编译错误 `undefined: ReviewerActionTargetMaxLength`

- [ ] **Step 3: 写实现**

在 `models.go` 的 `// ──────────────────────────── 结构体 ────────────────────────────` 之前插入常量区块（Go 规范要求结构体在前、常量在后，故插在文件末尾的「导出函数」之后、或单独常量区块——本包按项目规范放在结构体之后）：

在 `models.go` 末尾追加：

```go
// ──────────────────────────── 常量 ────────────────────────────

const (
	// ReviewerActionTargetMaxLength 审查动作目标描述的最大长度
	// Python: _REVIEWER_ACTION_TARGET_MAX_LENGTH
	ReviewerActionTargetMaxLength = 160

	// ReviewerUITextMaxLength 审查 UI 文本的最大长度
	// Python: _REVIEWER_UI_TEXT_MAX_LENGTH
	ReviewerUITextMaxLength = 1024

	// ReviewerUIShortTextMaxLength 审查 UI 短文本的最大长度
	// Python: _REVIEWER_UI_SHORT_TEXT_MAX_LENGTH
	ReviewerUIShortTextMaxLength = 240

	// ReviewerDenialGuidance 拒绝时给模型的引导语，防止绕过拒绝
	// Python: _REVIEWER_DENIAL_GUIDANCE
	ReviewerDenialGuidance = "Do not retry, rephrase, split, or switch tools to bypass this denial. " +
		"A materially changed user intent requires a new tool call and full review."

	// ProhibitedFileDeliveryReason 用户禁止文件投递时的拒绝原因
	// Python: PROHIBITED_FILE_DELIVERY_REASON
	ProhibitedFileDeliveryReason = "file_delivery_prohibited_by_user"

	// DeterministicBoundedScopeDecisionSource 确定性有界范围判定的决策来源
	// Python: DETERMINISTIC_BOUNDED_SCOPE_DECISION_SOURCE
	DeterministicBoundedScopeDecisionSource = "deterministic_bounded_scope"

	// DeterministicReadonlyPublicWebReason 确定性放行只读公开 Web 的原因码
	// Python: DETERMINISTIC_READONLY_PUBLIC_WEB_REASON
	DeterministicReadonlyPublicWebReason = "deterministic_readonly_public_web"
)
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/auto_permission/ -v`
Expected: 全部 PASS（13 个用例）

- [ ] **Step 5: Commit**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/swarm/agents/harness/common/rails/permissions/auto_permission/models.go internal/swarm/agents/harness/common/rails/permissions/auto_permission/models_test.go
git commit -m "feat(permissions): models 七个标量常量

对齐 Python _auto_permission/models.py 的标量常量：三个 UI 长度上限、
拒绝引导语、文件投递禁止原因、两个确定性决策来源标识。
私有名 _REVIEWER_* 转导出 Reviewer*（Go 包级无同模块私有概念，spec D4）。"
```

---

## Task 4: models.go 的 3 个正则 + 1 个集合常量（TDD）

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/models.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/models_test.go`

这三个正则在 Python 全仓零消费，但按规则 9.2-4「不得裁剪成员」照抄。已实测三个模式串在 Go RE2 下均可编译。

- [ ] **Step 1: 写失败测试**

在 `models_test.go` 追加：

```go
// ──────────────────────────── 正则与集合常量测试 ────────────────────────────

// TestShellDisplayPathPattern_编译与匹配 验证三个 shell 路径场景能抽出路径
func TestShellDisplayPathPattern_编译与匹配(t *testing.T) {
	if ShellDisplayPathPattern == nil {
		t.Fatal("ShellDisplayPathPattern 不应为 nil")
	}
	cases := []string{
		"/Users/alice/work/a.txt",
		"/home/bob/project/x",
		"~/work/file.txt",
		"/private/tmp/z",
		"/tmp/y.log",
		`C:\Users\me\a.txt`,
	}
	for _, s := range cases {
		if !ShellDisplayPathPattern.MatchString(s) {
			t.Errorf("应匹配 %q", s)
		}
	}
}

// TestShellDisplayPreSplitPathPattern_编译与匹配 验证只匹配 Windows 盘符路径
func TestShellDisplayPreSplitPathPattern_编译与匹配(t *testing.T) {
	if ShellDisplayPreSplitPathPattern == nil {
		t.Fatal("ShellDisplayPreSplitPathPattern 不应为 nil")
	}
	if !ShellDisplayPreSplitPathPattern.MatchString(`C:\Users\me\a.txt`) {
		t.Error(`应匹配 C:\Users\me\a.txt`)
	}
	if ShellDisplayPreSplitPathPattern.MatchString("/Users/alice/a.txt") {
		t.Error("不应匹配 Unix 路径")
	}
}

// TestSensitiveDisplayPathPattern_编译与匹配 验证敏感文件名与目录识别
func TestSensitiveDisplayPathPattern_编译与匹配(t *testing.T) {
	if SensitiveDisplayPathPattern == nil {
		t.Fatal("SensitiveDisplayPathPattern 不应为 nil")
	}
	yes := []string{
		"/a/.env",
		"/a/.env.local",
		"/a/credentials",
		"/a/id_rsa",
		"/a/id_ed25519",
		"/a/known_hosts",
		"/a/.ssh/config",
		"/a/.aws/credentials",
		"/a/.gnupg/key",
		"/a/my_secret.txt",
		"/a/token.json",
		"/a/api_key.txt",
		"/a/private_key.pem",
	}
	for _, s := range yes {
		if !SensitiveDisplayPathPattern.MatchString(s) {
			t.Errorf("应匹配 %q", s)
		}
	}
	no := []string{"/a/normal.txt", "/a/readme.md", "/a/public.html"}
	for _, s := range no {
		if SensitiveDisplayPathPattern.MatchString(s) {
			t.Errorf("不应匹配 %q", s)
		}
	}
}

// TestDeterministicReadonlyPublicWebFetchSourceKinds_内容 验证集合成员
func TestDeterministicReadonlyPublicWebFetchSourceKinds_内容(t *testing.T) {
	if len(DeterministicReadonlyPublicWebFetchSourceKinds) != 1 {
		t.Fatalf("应只有 1 个成员，实际 %d", len(DeterministicReadonlyPublicWebFetchSourceKinds))
	}
	if _, ok := DeterministicReadonlyPublicWebFetchSourceKinds["recent_search_result"]; !ok {
		t.Fatal("应包含 recent_search_result")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/auto_permission/ -run 'TestShellDisplay|TestSensitiveDisplay|TestDeterministicReadonly' -v`
Expected: FAIL，编译错误 `undefined: ShellDisplayPathPattern`

- [ ] **Step 3: 写实现**

在 `models.go` 顶部 import 块改为：

```go
import (
	"errors"
	"regexp"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)
```

在 `models.go` 末尾（常量区块之后）追加全局变量区块：

```go
// ──────────────────────────── 全局变量 ────────────────────────────

// ShellDisplayPathPattern 从 shell 命令里抽出需要脱敏显示的路径。
//
// 覆盖 macOS（/Users/、/private/、/tmp/）、Linux（/home/x/）、
// HOME 相对（~/...）与 Windows 盘符（C:\Users\、Documents and Settings）四类。
//
// ⤵️ Python 全仓零消费，待 9.90d 的 reviewer_redaction 启用。
//
// Python: _SHELL_DISPLAY_PATH_PATTERN
var ShellDisplayPathPattern = regexp.MustCompile(
	`(?:` +
		`(?:/Users/|/private/|/tmp/)[^\s\"']+` +
		`|/home/[^/\s\"']+/[^\s\"']+` +
		`|~(?:[/\\][^\s\"']+)+` +
		`|[A-Za-z]:[\\/](?:Users|Documents and Settings)[\\/]` +
		`[^'\";|&<>` + "`" + `,，。；、\r\n]+` +
		`)`,
)

// ShellDisplayPreSplitPathPattern 仅匹配 Windows 盘符路径，用于预切分。
//
// ⤵️ Python 全仓零消费，待 9.90d 的 reviewer_redaction 启用。
//
// Python: _SHELL_DISPLAY_PRE_SPLIT_PATH_PATTERN
var ShellDisplayPreSplitPathPattern = regexp.MustCompile(
	`[A-Za-z]:[\\/](?:Users|Documents and Settings)[\\/]` +
		`[^'\";|&<>` + "`" + `,，。；、\r\n]+`,
)

// SensitiveDisplayPathPattern 识别敏感文件名与目录（.env / 凭证 / 密钥）。
//
// 三类：点文件类（.env*、credentials、id_*、known_hosts）、
// 敏感目录（.ssh / .aws / .gnupg / .jiuwenswarm/config）、
// 关键词类（secret / token / api_key / private_key / credential / password）。
//
// ⤵️ Python 全仓零消费，待 9.90d 的 reviewer_redaction 启用。
//
// Python: _SENSITIVE_DISPLAY_PATH_PATTERN
var SensitiveDisplayPathPattern = regexp.MustCompile(
	`(?i)(?:` +
		`(^|[/\\])(?:\.env(?:\.[^/\\]+)?|credentials?|id_rsa|id_dsa|id_ecdsa|` +
		`id_ed25519|known_hosts)$` +
		`|(^|[/\\])(?:\.ssh|\.aws|\.gnupg|\.jiuwenswarm[/\\]config)(?:[/\\]|$)` +
		`|(?:secret|token|api[_-]?key|private[_-]?key|credential|password)` +
		`)`,
)

// DeterministicReadonlyPublicWebFetchSourceKinds 允许确定性放行的 fetch 来源类型集合。
//
// 只读用途，本包内不得对其赋值。
//
// ⤵️ Python 全仓零消费，待 9.90d 的 deterministic 启用。
//
// Python: DETERMINISTIC_READONLY_PUBLIC_WEB_FETCH_SOURCE_KINDS = frozenset({"recent_search_result"})
var DeterministicReadonlyPublicWebFetchSourceKinds = map[string]struct{}{
	"recent_search_result": {},
}
```

**注意**：反引号在 Go raw string 里无法直接写，上面用 `` ` + "`" + ` `` 拼接。若觉得别扭，可把这三个模式串改为普通双引号字符串并转义 `\s`（`\\s`）——但**必须保持正则语义完全相同**，且写完后跑 Step 4 验证。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/auto_permission/ -v`
Expected: 全部 PASS（17 个用例）

若某个正则 `MatchString` 结果不符预期，先确认模式串是否与 Python 原文逐字一致（比对 `jiuwenswarm/agents/harness/common/rails/permissions/_auto_permission/models.py` L33-57）。

- [ ] **Step 5: 检查覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/auto_permission/ -cover -coverprofile=/tmp/ap_models.out && go tool cover -func=/tmp/ap_models.out`
Expected: `models.go` 相关函数 100%（常量与 var 不计入函数覆盖率，`NewPermissionHandlingResult` 应 100%）

- [ ] **Step 6: Commit**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/swarm/agents/harness/common/rails/permissions/auto_permission/models.go internal/swarm/agents/harness/common/rails/permissions/auto_permission/models_test.go
git commit -m "feat(permissions): models 三个正则 + 一个集合常量

Python _auto_permission/models.py 的三个 re.Pattern 与一个 frozenset 逐字照抄：
- ShellDisplayPathPattern / ShellDisplayPreSplitPathPattern / SensitiveDisplayPathPattern
  均在 Go RE2 下编译通过（RE2 不支持反向引用与 lookaround，三个模式串均未使用）
- DeterministicReadonlyPublicWebFetchSourceKinds 用 map[string]struct{} 表达 frozenset

四者在 Python 全仓（含 tests）零消费，按规则 9.2-4「不得裁剪成员」保留，
doc.go 已挂 ⤵️ 待 9.90d 的 reviewer_redaction / deterministic 启用。"
```

---

## Task 5: tool_binding.go 的 MatchesBoundMethod（TDD）

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/tool_binding.go`
- Test: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/tool_binding_test.go`

- [ ] **Step 1: 写失败测试**

创建 `tool_binding_test.go`：

```go
package auto_permission

import "testing"

// boundOwner 用于测试匹配的接收者
type boundOwner struct {
	// tag 区分实例
	tag string
}

// Do 目标方法
func (o *boundOwner) Do() {}

// Other 另一个方法，用于验证函数不同
func (o *boundOwner) Other() {}

// TestMatchesBoundMethod_正常 验证接收者与函数都相同时为 true
func TestMatchesBoundMethod_正常(t *testing.T) {
	owner := &boundOwner{tag: "a"}
	if !MatchesBoundMethod(owner, (*boundOwner).Do, owner, (*boundOwner).Do) {
		t.Fatal("接收者与函数均相同应返回 true")
	}
}

// TestMatchesBoundMethod_owner为nil 验证 expectedOwner 为 nil 时短路
func TestMatchesBoundMethod_owner为nil(t *testing.T) {
	owner := &boundOwner{tag: "a"}
	if MatchesBoundMethod(owner, (*boundOwner).Do, nil, (*boundOwner).Do) {
		t.Fatal("expectedOwner 为 nil 应返回 false")
	}
}

// TestMatchesBoundMethod_func为nil 验证 expectedFunc 为 nil 时短路
func TestMatchesBoundMethod_func为nil(t *testing.T) {
	owner := &boundOwner{tag: "a"}
	if MatchesBoundMethod(owner, (*boundOwner).Do, owner, nil) {
		t.Fatal("expectedFunc 为 nil 应返回 false")
	}
}

// TestMatchesBoundMethod_receiver不同 验证不同实例的同名方法判不匹配
//
// 这是本函数存在的核心理由：Go 里 a.Do 与 b.Do 的代码指针相同，
// 只有显式比较 receiver 才能区分。
func TestMatchesBoundMethod_receiver不同(t *testing.T) {
	a := &boundOwner{tag: "a"}
	b := &boundOwner{tag: "b"}
	if MatchesBoundMethod(a, (*boundOwner).Do, b, (*boundOwner).Do) {
		t.Fatal("接收者不同应返回 false")
	}
}

// TestMatchesBoundMethod_函数不同 验证同一接收者的不同方法判不匹配
func TestMatchesBoundMethod_函数不同(t *testing.T) {
	owner := &boundOwner{tag: "a"}
	if MatchesBoundMethod(owner, (*boundOwner).Other, owner, (*boundOwner).Do) {
		t.Fatal("函数不同应返回 false")
	}
}

// TestMatchesBoundMethod_非指针接收者 验证值类型接收者的同一性判断
func TestMatchesBoundMethod_非指针接收者(t *testing.T) {
	type valueOwner struct{ tag string }
	if MatchesBoundMethod(valueOwner{tag: "a"}, valueOwner{tag: "a"}, valueOwner{tag: "a"}, valueOwner{tag: "a"}) {
		t.Fatal("值类型不同实例（即使字段相等）应返回 false")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/auto_permission/ -run TestMatchesBoundMethod -v`
Expected: FAIL，编译错误 `undefined: MatchesBoundMethod`

- [ ] **Step 3: 写实现**

创建 `tool_binding.go`：

```go
// 读取当前可执行绑定的身份，不做权限决策。
//
// 本文件回答一个问题：我要放行的那个工具，是不是我原以为的那个实现？
// 它只做身份校验，判断结果交给调用方。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/permissions/tool_binding.py
package auto_permission

import (
	"reflect"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// MatchesBoundMethod 比较调用方提供的两个绑定身份。
//
// Python 的 matches_bound_method 读方法对象上的 __self__ 与 __func__；
// Go 的函数值不携带接收者信息（a.Do 与 b.Do 的代码指针相同），
// 因此接收者必须由调用方显式传入，这是与 Python 的签名差异（spec D1）。
//
// 判定顺序与 Python 一致：
//  1. expectedOwner 为 nil → false
//  2. expectedFunc 为 nil → false
//  3. receiver 与 expectedOwner 非同一对象 → false
//  4. method 与 expectedFunc 代码指针不同 → false
//  5. 否则 true
//
// Python: matches_bound_method(method, *, expected_owner, expected_func)
func MatchesBoundMethod(receiver any, method any, expectedOwner any, expectedFunc any) bool {
	if expectedOwner == nil || expectedFunc == nil {
		return false
	}
	if !identityEqual(receiver, expectedOwner) {
		return false
	}
	return funcPtrEqual(method, expectedFunc)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// identityEqual 判断两个值是否为同一对象（对应 Python 的 is）。
//
// 指针、map、slice、func 取地址指针比较；其余可比较类型退化为值相等
// （Python 中值类型无 is 语义差别的场景，Go 用 == 承载）。
// 任一方为 nil 视为不相等（nil 的同一性在调用方已短路）。
func identityEqual(a, b any) bool {
	if a == nil || b == nil {
		return false
	}
	av := reflect.ValueOf(a)
	bv := reflect.ValueOf(b)
	if av.Type() != bv.Type() {
		return false
	}
	switch av.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return av.Pointer() == bv.Pointer()
	default:
		return a == b
	}
}

// funcPtrEqual 比较两个函数值的代码指针。
//
// Go 函数值不可 == 比较，只能取代码指针。
// 先例：agentcore/harness/rails/model_anomaly.go funcPtrEqual、sys_operation/registry.go:73
func funcPtrEqual(a, b any) bool {
	if a == nil || b == nil {
		return false
	}
	av := reflect.ValueOf(a)
	bv := reflect.ValueOf(b)
	if av.Kind() != reflect.Func || bv.Kind() != reflect.Func {
		return false
	}
	if av.Type() != bv.Type() {
		return false
	}
	return av.Pointer() == bv.Pointer()
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/auto_permission/ -run TestMatchesBoundMethod -v`
Expected: 6 个用例全部 PASS

- [ ] **Step 5: Commit**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/swarm/agents/harness/common/rails/permissions/auto_permission/tool_binding.go internal/swarm/agents/harness/common/rails/permissions/auto_permission/tool_binding_test.go
git commit -m "feat(permissions): tool_binding MatchesBoundMethod 绑定身份校验

对齐 Python matches_bound_method：接收者同一性 + 函数代码指针双重校验。

签名与 Python 有一处差异（spec D1）：Go 函数值不携带接收者（a.Do 与 b.Do
代码指针相同），故 receiver 由调用方显式传入，参数从 2 个变 4 个。"
```

---

## Task 6: tool_binding.go 的 ResolveToolBinding 与注入点（TDD）

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/tool_binding.go`
- Modify: `internal/swarm/agents/harness/common/rails/permissions/auto_permission/tool_binding_test.go`

- [ ] **Step 1: 写失败测试与 fake 设施**

在 `tool_binding_test.go` 追加（**注意**：Task 5 的 `tool_binding_test.go` 已有 `import "testing"`，此处要把整个 import 块替换掉，不是追加）：

把 `tool_binding_test.go` 顶部的：

```go
import "testing"
```

替换为：

```go
import (
	"context"
	"reflect"
	"testing"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)
```

// ──────────────────────────── fake 设施 ────────────────────────────

// fakeAbilityManager 只实现 Get，其余方法返回零值。
//
// AbilityManagerInterface 共 10 个方法，按规则 10.1-2「测试替身自己补齐」
// 逐个空写。
type fakeAbilityManager struct {
	// abilities 按名称索引的能力
	abilities map[string]commonschema.Ability
}

func (f *fakeAbilityManager) Add(commonschema.Ability) saschema.AddAbilityResult {
	return saschema.AddAbilityResult{}
}

func (f *fakeAbilityManager) AddMany([]commonschema.Ability) []saschema.AddAbilityResult {
	return nil
}

func (f *fakeAbilityManager) Remove(string) commonschema.Ability { return nil }

func (f *fakeAbilityManager) RemoveMany([]string) []commonschema.Ability { return nil }

func (f *fakeAbilityManager) Get(name string) commonschema.Ability {
	return f.abilities[name]
}

func (f *fakeAbilityManager) List() []commonschema.Ability { return nil }

func (f *fakeAbilityManager) ListToolInfo(context.Context, []string, ...string) ([]commonschema.ToolInfoInterface, error) {
	return nil, nil
}

func (f *fakeAbilityManager) Execute(
	context.Context,
	*agentinterfaces.AgentCallbackContext,
	[]*llmschema.ToolCall,
	sessioninterfaces.SessionFacade,
	string,
) []saschema.ExecuteResult {
	return nil
}

func (f *fakeAbilityManager) SetContextEngine(ceinterface.ContextEngine) {}

func (f *fakeAbilityManager) ReorderTools([]string) {}

// fakeTool 最小工具实现，用于精确类型校验
type fakeTool struct {
	// card 工具卡片
	card *tool.ToolCard
}

func (f *fakeTool) Card() *tool.ToolCard { return f.card }

func (f *fakeTool) Invoke(context.Context, map[string]any, ...tool.ToolOption) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

func (f *fakeTool) Stream(context.Context, map[string]any, ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, nil
}

// fakeToolSub fakeTool 的子类，用于验证「子类不算合格」
type fakeToolSub struct {
	fakeTool
}

// ──────────────────────────── ResolveToolBinding 测试 ────────────────────────────

// withResolveToolByID 替换工具查找实现并在测试结束恢复
func withResolveToolByID(t *testing.T, fn func(string) tool.Tool) {
	t.Helper()
	orig := resolveToolByID
	SetResolveToolByIDFunc(fn)
	t.Cleanup(func() { resolveToolByID = orig })
}

// TestResolveToolBinding_正常 验证类型与卡片都匹配时返回该实例
func TestResolveToolBinding_正常(t *testing.T) {
	card := tool.NewToolCardWithID("t1", "read_file", "读文件", nil, nil)
	impl := &fakeTool{card: card}
	am := &fakeAbilityManager{abilities: map[string]commonschema.Ability{"read_file": card}}
	withResolveToolByID(t, func(string) tool.Tool { return impl })

	got := ResolveToolBinding(am, "read_file", reflectTypeOfTool(&fakeTool{}))
	if got != tool.Tool(impl) {
		t.Fatalf("应返回注册的实现，实际 %v", got)
	}
}

// TestResolveToolBinding_能力不存在 验证 am.Get 返回 nil 时返回 nil
func TestResolveToolBinding_能力不存在(t *testing.T) {
	am := &fakeAbilityManager{abilities: map[string]commonschema.Ability{}}
	withResolveToolByID(t, func(string) tool.Tool {
		t.Fatal("能力不存在时不应查找工具")
		return nil
	})

	got := ResolveToolBinding(am, "nope", reflectTypeOfTool(&fakeTool{}))
	if got != nil {
		t.Fatalf("应返回 nil，实际 %v", got)
	}
}

// TestResolveToolBinding_类型不匹配 验证 expectedType 不符时返回 nil
func TestResolveToolBinding_类型不匹配(t *testing.T) {
	card := tool.NewToolCardWithID("t1", "read_file", "读文件", nil, nil)
	impl := &fakeTool{card: card}
	am := &fakeAbilityManager{abilities: map[string]commonschema.Ability{"read_file": card}}
	withResolveToolByID(t, func(string) tool.Tool { return impl })

	// 用 fakeToolSub 的类型做期望，实际实现是 fakeTool
	got := ResolveToolBinding(am, "read_file", reflectTypeOfTool(&fakeToolSub{}))
	if got != nil {
		t.Fatalf("应返回 nil，实际 %v", got)
	}
}

// TestResolveToolBinding_子类不算合格 验证子类实现在期望父类类型时不通过
func TestResolveToolBinding_子类不算合格(t *testing.T) {
	card := tool.NewToolCardWithID("t1", "read_file", "读文件", nil, nil)
	impl := &fakeToolSub{fakeTool: fakeTool{card: card}}
	am := &fakeAbilityManager{abilities: map[string]commonschema.Ability{"read_file": card}}
	withResolveToolByID(t, func(string) tool.Tool { return impl })

	got := ResolveToolBinding(am, "read_file", reflectTypeOfTool(&fakeTool{}))
	if got != nil {
		t.Fatalf("子类实现不应被判为父类，实际返回 %v", got)
	}
}

// TestResolveToolBinding_卡片不同一 验证实现持有的卡片与注册卡片不同时判不合格
func TestResolveToolBinding_卡片不同一(t *testing.T) {
	registered := tool.NewToolCardWithID("t1", "read_file", "读文件", nil, nil)
	other := tool.NewToolCardWithID("t1", "read_file", "读文件", nil, nil)
	impl := &fakeTool{card: other}
	am := &fakeAbilityManager{abilities: map[string]commonschema.Ability{"read_file": registered}}
	withResolveToolByID(t, func(string) tool.Tool { return impl })

	got := ResolveToolBinding(am, "read_file", reflectTypeOfTool(&fakeTool{}))
	if got != nil {
		t.Fatalf("卡片不同一应返回 nil，实际 %v", got)
	}
}

// TestResolveToolBinding_非ToolCard能力 验证能力不是 *tool.ToolCard 时返回 nil
//
// Python 此时 get_tool 返回 None、resolve_tool_binding 安静返回 None，
// 与 Go 返 nil 一致（spec §3.2.2 印证表）。
func TestResolveToolBinding_非ToolCard能力(t *testing.T) {
	am := &fakeAbilityManager{abilities: map[string]commonschema.Ability{
		"some_workflow": fakeAbility{name: "some_workflow", id: "w1"},
	}}
	withResolveToolByID(t, func(string) tool.Tool {
		t.Fatal("非 ToolCard 能力不应查找工具")
		return nil
	})

	got := ResolveToolBinding(am, "some_workflow", reflectTypeOfTool(&fakeTool{}))
	if got != nil {
		t.Fatalf("应返回 nil，实际 %v", got)
	}
}

// TestResolveToolBinding_工具不存在 验证卡在而工具不在时返回 nil
//
// Python 此时 get_tool 返回 None（不抛异常），Go 同样返回 nil。
func TestResolveToolBinding_工具不存在(t *testing.T) {
	card := tool.NewToolCardWithID("t1", "read_file", "读文件", nil, nil)
	am := &fakeAbilityManager{abilities: map[string]commonschema.Ability{"read_file": card}}
	withResolveToolByID(t, func(string) tool.Tool { return nil })

	got := ResolveToolBinding(am, "read_file", reflectTypeOfTool(&fakeTool{}))
	if got != nil {
		t.Fatalf("应返回 nil，实际 %v", got)
	}
}
```

**同时**在 `tool_binding_test.go` 顶部追加两个辅助：

```go
// fakeAbility 仅实现 Ability 三方法，用于构造非 ToolCard 能力
type fakeAbility struct {
	name string
	id   string
}

func (f fakeAbility) AbilityName() string             { return f.name }
func (f fakeAbility) AbilityID() string               { return f.id }
func (f fakeAbility) AbilityKind() commonschema.AbilityKind { return commonschema.AbilityKindTool }

// reflectTypeOfTool 取实例的 reflect.Type
func reflectTypeOfTool(v any) reflect.Type { return reflect.TypeOf(v) }
```

并把 import 补齐为：

```go
import (
	"context"
	"reflect"
	"testing"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/auto_permission/ -run TestResolveToolBinding -v`
Expected: FAIL，编译错误 `undefined: ResolveToolBinding` 与 `undefined: resolveToolByID`

- [ ] **Step 3: 写实现**

在 `tool_binding.go` 的 import 块改为：

```go
import (
	"reflect"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)
```

在 `tool_binding.go` 的结构体区块之前插入：

```go
// ──────────────────────────── 全局变量 ────────────────────────────

// resolveToolByID 按 ID 取工具实例，默认走全局 ResourceMgr。
//
// 不传 WithSession，等价于 Python 的 session=None——不加 trace 装饰，
// 拿到的就是裸实现；若加了装饰会变成 *decorator.TracedTool，
// 精确类型校验必然失败。
//
// 返回 nil 表示工具不存在（Python 同样返回 None，不抛异常）。
// 变量形式供测试注入，对齐 common/tools/send_file.go 的 SetXxxFunc 惯例。
//
// Python: Runner.resource_mgr.get_tool(tool_id, session=None)
var resolveToolByID = func(id string) tool.Tool {
	tools, err := runner.GetResourceMgr().GetTool([]string{id})
	if err != nil || len(tools) == 0 {
		return nil
	}
	return tools[0]
}
```

在 `// ──────────────────────────── 导出函数 ────────────────────────────` 区块内、`MatchesBoundMethod` 之后追加：

```go
// SetResolveToolByIDFunc 替换工具查找实现，仅供测试注入。
//
// 对齐 common/tools/send_file.go 的 SetSendFilePushFunc 成对注入惯例。
func SetResolveToolByIDFunc(fn func(id string) tool.Tool) {
	if fn != nil {
		resolveToolByID = fn
	}
}

// ResolveToolBinding 返回确切注册的实现，不匹配一律返回 nil。
//
// 三重校验（与 Python 逐条对应，步骤顺序一字不差）：
//  1. 实现类型必须精确等于 expectedType——子类不算合格，
//     因为子类可以重写 Invoke。
//     Python 原注释：Subclasses can replace execution; this is
//     implementation identity, not assignability.
//  2. 卡片必须是同一个对象（指针同一），不是字段相等的另一张卡。
//  3. 查找时不带 session，避免拿到 trace 装饰后的包装类型。
//
// 六档返回语义与 Python 完全一致（spec §3.2.2 印证表）：
// 能力不存在 / 能力非 ToolCard / 工具不存在 / 类型不符 / 卡片不同一 → nil；
// 全部通过 → resource。
//
// Python: resolve_tool_binding(agent, tool_name, expected_type)
func ResolveToolBinding(
	am agentinterfaces.AbilityManagerInterface,
	toolName string,
	expectedType reflect.Type,
) tool.Tool {
	// 1. 查能力卡片（Python: card = agent.ability_manager.get(tool_name)）
	card := am.Get(toolName)
	if card == nil {
		return nil
	}

	// 2. 按卡片 ID 查实现（Python: get_tool(card.id, session=None)）
	//
	// 用 schema.Ability 接口自带的 AbilityID()，无需先断言成 *tool.ToolCard，
	// 保证与 Python 的步骤顺序完全一致。
	resource := resolveToolByID(card.AbilityID())
	if resource == nil {
		return nil
	}

	// 3. 精确类型校验（Python: exact_type = type(resource) is expected_type）
	if reflect.TypeOf(resource) != expectedType {
		return nil
	}

	// 4. 卡片同一性校验（Python: resource.card is card）
	//
	// 指针同一性比较必须拿到 *tool.ToolCard 具体值，断言在此处才需要。
	cardPtr, ok := card.(*tool.ToolCard)
	if !ok {
		return nil
	}
	if resource.Card() != cardPtr {
		return nil
	}

	return resource
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/auto_permission/ -v`
Expected: 全部 PASS（24 个用例）

若 `commonschema.AbilityKindTool` 名称不符，查 `internal/common/schema/card.go` 的 `AbilityKind` 常量定义后修正 `fakeAbility` 的 `AbilityKind` 返回值。

- [ ] **Step 5: 检查覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/auto_permission/ -cover -coverprofile=/tmp/ap_all.out && go tool cover -func=/tmp/ap_all.out | grep tool_binding`
Expected: `tool_binding.go:MatchesBoundMethod` / `identityEqual` / `funcPtrEqual` / `ResolveToolBinding` / `SetResolveToolByIDFunc` 均为 100.0%，包总覆盖率 ≥ 90%

若未达 90%，补 `SetResolveToolByIDFunc(nil)` 的用例（不应覆盖原值）。

- [ ] **Step 6: Commit**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/swarm/agents/harness/common/rails/permissions/auto_permission/tool_binding.go internal/swarm/agents/harness/common/rails/permissions/auto_permission/tool_binding_test.go
git commit -m "feat(permissions): tool_binding ResolveToolBinding 实现身份背书

对齐 Python resolve_tool_binding 的三重校验：精确类型 + 卡片指针同一 + 不带
session 查找（避免 trace 装饰成 TracedTool 导致类型校验失败）。

六档返回语义与 Python 完全一致（能力不存在 / 非 ToolCard / 工具不存在 /
类型不符 / 卡片不同一 → nil；全部通过 → resource）。曾考虑把「工具不存在」
做成 error 以保留可观测性，读源码核实 Python 的 get_tool 在 exact_match=True
时因 finally 分支 elif resource or exact_match 而返回 None（不抛异常），
故撤销该偏差，保持单返回值。

resolveToolByID 为包级变量，配 SetResolveToolByIDFunc 供测试注入
（对齐 common/tools/send_file.go 的 SetXxxFunc 惯例）。"
```

---

## Task 7: 父包 doc.go 与计划表回填

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/permissions/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新父包 doc.go 的包注释与文件目录**

把 `permissions/doc.go` 第 4-6 行：

```go
// 本包对齐 Python jiuwenswarm/agents/harness/common/rails/permissions/（38 个 .py / 11,463 行），
// 当前已完成权限上下文与配置 RPC 部分，以及 root 五文件（9.90c）。剩余约 10,000 行
// 按批次推进，见 IMPLEMENTATION_PLAN.md 的 9-H 节。
```

改为：

```go
// 本包对齐 Python jiuwenswarm/agents/harness/common/rails/permissions/（38 个 .py / 11,463 行），
// 当前已完成权限上下文与配置 RPC 部分、root 五文件（9.90c）、以及基础类型层
// （9.90a：tool_invocation_key / protected_paths / auto_permission 子包）。剩余约
// 9,700 行按批次推进，见 IMPLEMENTATION_PLAN.md 的 9-H 节。
```

在「已知缺口」段之后、`文件目录：` 之前插入：

```go
// ⤵️ auto_permission 子包中 4 个常量（ShellDisplayPathPattern /
// ShellDisplayPreSplitPathPattern / SensitiveDisplayPathPattern /
// DeterministicReadonlyPublicWebFetchSourceKinds）在 Python 全仓零消费，
// 按规则 9.2-4 照抄保留，待 9.90d 的 reviewer_redaction / deterministic 启用。
// 其余 7 个常量的消费方同样在 9.90d。
```

在文件目录中 `└── root_context_rail.go` 之后追加：

```
//	└── auto_permission/                  # 自动权限审查层共享契约 + 身份原语（9.90a）
//	    ├── models.go                    # ToolInvocation / PermissionHandlingResult / PermissionInterruptRequest + 11 常量
//	    └── tool_binding.go              # MatchesBoundMethod / ResolveToolBinding——实现身份背书
```

注意：原目录树最后一项是 `└──`，需改为 `├──` 后再加新项。

- [ ] **Step 2: 更新计划表 9.90a 行**

把 `IMPLEMENTATION_PLAN.md` 中：

```
| 9.90a | ☐ | permissions 基础类型层 | ToolInvocationKeyV1 + normalize_tool_invocation_text + MAX_STRUCTURED_QUESTIONS + TOOL_PERMISSION_REQUEST_ID + SKILLS_REBUILD_SILENT + CommandExecutionBinding + protected_paths + tool_binding + models；约 400 行。详见 9-H 批次 | `.../rails/permissions/tool_invocation_key.py` · `tool_permission_context.py` · `protected_paths.py` · `tool_binding.py` · `models.py` · `rails/ask_user_contract.py` · `common/tools/command_execution_context.py` |
```

改为：

```
| 9.90a | ✅ | permissions 基础类型层 | ✅ 七文件中五项已于 `337007d0` 落地（`tool_invocation_key.py` 94 行 → `permissions/tool_invocation_key.go`；`ask_user_contract.py` 4 行 → `rails/ask_user_contract.go`；`tool_permission_context.py` 40 行 → `swarm/schema/permission.go` 的 6 个 ctx 函数；`command_execution_context.py` 59 行 → `common/tools/command_execution_context.go`；`protected_paths.py` 26 行 → `permissions/protected_paths.go`），`bce0bd92` 修正 `resolveSandboxed` 的 mode 判定；✅ 剩余两项本批落地（`tool_binding.py` 25 行 → `permissions/auto_permission/tool_binding.go`；`models.py` 101 行 → `permissions/auto_permission/models.go`，含 3 个 RE2 正则与 1 个 frozenset）；⤵️ **4 个零消费常量待 9.90d 启用**（`ShellDisplayPathPattern` / `ShellDisplayPreSplitPathPattern` / `SensitiveDisplayPathPattern` / `DeterministicReadonlyPublicWebFetchSourceKinds`，按规则 9.2-4 照抄保留）；Spec: `docs/superpowers/specs/2026-11-07-permissions-models-tool-binding-design.md` | `.../rails/permissions/tool_invocation_key.py` · `tool_permission_context.py` · `protected_paths.py` · `tool_binding.py` · `_auto_permission/models.py` · `rails/ask_user_contract.py` · `common/tools/command_execution_context.py` |
```

- [ ] **Step 3: 验证 doc.go 与计划表改动**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/agents/harness/common/rails/permissions/... && grep -n "auto_permission" internal/swarm/agents/harness/common/rails/permissions/doc.go && grep -n "9.90a | ✅" IMPLEMENTATION_PLAN.md`
Expected: 编译无输出；两条 grep 均有命中

- [ ] **Step 4: Commit**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/swarm/agents/harness/common/rails/permissions/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(permissions): 9.90a 收口——计划表回填 + 父包 doc.go 补子包条目

计划表 9.90a 行 ☐ → ✅，注明已完成五项来源（337007d0 前置件、bce0bd92
resolveSandboxed 修正）与本批新增两项（models + tool_binding）。
4 个零消费常量挂 ⤵️ 到 9.90d。

父包 doc.go 文件树补 auto_permission/ 子包条目。"
```

---

## Task 8: 全量回归与交付

**Files:** 无（仅验证）

- [ ] **Step 1: 编译与 vet**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./... && go vet ./internal/swarm/agents/harness/common/rails/permissions/...`
Expected: 均无输出

- [ ] **Step 2: 子包覆盖率复核**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/permissions/auto_permission/ -cover`
Expected: `coverage: 90.0%` 或更高（规则 3 要求 ≥ 85%）

- [ ] **Step 3: 全量回归（必须带 CI 同款构建标签）**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | grep -E "^(FAIL|ok.*permissions)" | head -20`
Expected: 无 `FAIL` 行；`permissions` 相关包显示 ok

不带 `sqlite_fts5` 标签会让 memory 包的 FTS 用例假失败，不要裸跑。

- [ ] **Step 4: 推送**

```bash
cd /home/opensource/uapclaw-gateway
git log --oneline -6
git push
```

Expected: 推送成功。按规则 11，**不追踪 CI 结果**。

- [ ] **Step 5: 清理临时覆盖率文件**

Run: `rm -f /tmp/ap_models.out /tmp/ap_all.out && rm -rf /tmp/re2check`
Expected: 无输出

---

## 自审记录

**Spec 覆盖检查**：

| Spec 章节 | 对应 Task |
|---|---|
| §3.1 #1-#4（三个长度常量 + 拒绝引导语） | Task 3 |
| §3.1 #5-#7（三个正则） | Task 4 |
| §3.1 #8-#10（三个原因/来源常量） | Task 3 |
| §3.1 #11（frozenset） | Task 4 |
| §3.1 #12（ToolInvocation） | Task 2 |
| §3.1 #13（PermissionHandlingResult + 构造校验） | Task 2 |
| §3.1 #14（PermissionInterruptRequest） | Task 2 |
| §3.2.1（MatchesBoundMethod） | Task 5 |
| §3.2.2（ResolveToolBinding + 注入点） | Task 6 |
| §4.1（子包结构 + doc.go） | Task 1 |
| §4.3（日志：本批不引入） | Task 2-6（均无日志） |
| §5（测试设计 + 覆盖率） | Task 2-6、Task 8 Step 2 |
| §7（提交分层） | Task 1-7 的 commit 步骤 |
| §7 第 4 项（计划表回填 + 父包 doc.go） | Task 7 |
| §8（删除清单，确认无删除项） | 无删除操作，已在 spec 记录 |
| §9（验收标准） | Task 8 |

**占位符扫描**：无 TBD / TODO / "类似 Task N" / "适当处理错误"。每个代码步骤均给出完整代码。

**类型一致性检查**：
- `ResolveToolBinding(am, toolName, expectedType)` 三参数签名在 Task 6 测试与实现中一致
- `MatchesBoundMethod(receiver, method, expectedOwner, expectedFunc)` 四参数在 Task 5 测试与实现中一致
- `resolveToolByID func(string) tool.Tool` 在 Task 6 实现与注入测试中一致
- `ResolveToolBinding` 单返回值 `tool.Tool` 在 Task 6 测试与实现中一致
- `NewPermissionHandlingResult(handled, result, decisionSource) (*PermissionHandlingResult, error)` 在 Task 2 测试与实现中一致
- `ReviewerActionTargetMaxLength` 等 7 个常量名在 Task 3 定义、Task 8 验收中一致
- `ShellDisplayPathPattern` 等 4 个变量名在 Task 4 定义、Task 7 doc.go 中一致

**实施前已核实的事实**（写计划时逐个验证过，不是待办）：

1. `commonschema.AbilityKindTool` 存在，且是 `iota` 首值（`internal/common/schema/card.go:63-64`）——`fakeAbility.AbilityKind()` 返回值正确。
2. `commonschema.ToolInfoInterface` 存在于 `internal/common/schema`（`card.go:130` 的 `ToolInfo()` 返回它）——`fakeAbilityManager.ListToolInfo` 签名正确。
3. `saschema.AddAbilityResult` 在 `single_agent/schema`（`abilitymgr.go:9` 的别名是 `agentschema`，指向同一包）——`fakeAbilityManager.Add` 签名正确。
4. `InterruptRequest` 的 5 个 `InterruptRequester` 方法均为**指针接收者**（`response.go:208/236` 等）——Task 2 的 `var _ saschema.InterruptRequester = req` 中 `req` 必须是 `*PermissionInterruptRequest`，计划里已如此书写。
5. `tool.NewToolCardWithID(id, name, description, inputParams, properties)` 存在（`foundation/tool/base.go:173`）——用于构造「同 ID 但不同指针」的卡片测试。
6. `tool.WithCard(card)` 选项存在（`invoke_function.go:50`）——本计划用 `fakeTool` 直接持有 card 字段，未使用该选项，无影响。
7. `logger.ComponentPermissions` 存在（父包 `owner_scopes.go:55` 已在用）——本批不用日志，仅备查。

**唯一剩余风险**：Task 4 正则的 raw string 拼接写法。计划已给出 `` ` + "`" + ` `` 方案并附双引号转义备选，写完后必须跑 Task 4 Step 4 验证匹配行为。
