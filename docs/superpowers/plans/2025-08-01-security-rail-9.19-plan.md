# SecurityRail 体系实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 9.19-23 SecurityRail 体系（BaseSecurityRail + SafetyPromptRail + PermissionInterruptRail + 基础设施），对齐 Python openjiuwen

**Architecture:** 双层安全护栏：Prompt 层（SafetyPromptRail 注入安全原则）+ Tool 层（PermissionInterruptRail 分层策略评估 + 中断确认）。基础设施层提供 PermissionEngine（编排）、TieredPolicy（分层评估）、ShellAST（tree-sitter bash 解析）、Patterns（模式匹配+持久化）、Checker（外部目录检测）、Suggestions（建议构建）

**Tech Stack:** Go 1.26, tree-sitter/go-tree-sitter v0.25.0, tree-sitter/tree-sitter-bash v0.25.1, CGO_ENABLED=1

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| Create | `internal/agentcore/harness/rails/security/doc.go` | 包文档 |
| Create | `internal/agentcore/harness/rails/security/base_security_rail.go` | BaseSecurityRail + 4种决策类型 + runAndApply + apply |
| Create | `internal/agentcore/harness/rails/security/base_security_rail_test.go` | BaseSecurityRail 单元测试 |
| Create | `internal/agentcore/harness/rails/security/prompt_security_rail.go` | SafetyPromptRail（别名 SecurityRail） |
| Create | `internal/agentcore/harness/rails/security/prompt_security_rail_test.go` | SafetyPromptRail 单元测试 |
| Create | `internal/agentcore/harness/security/shell_ast.go` | tree-sitter bash 解析 + 保守扫描 fallback |
| Create | `internal/agentcore/harness/security/shell_ast_test.go` | ShellAST 单元测试 |
| Create | `internal/agentcore/harness/security/tiered_policy.go` | 分层策略评估 |
| Create | `internal/agentcore/harness/security/tiered_policy_test.go` | TieredPolicy 单元测试 |
| Create | `internal/agentcore/harness/security/patterns.go` | 通配符/路径/URL/Command 匹配 + YAML 持久化 |
| Create | `internal/agentcore/harness/security/patterns_test.go` | Patterns 单元测试 |
| Create | `internal/agentcore/harness/security/checker.go` | ExternalDirectoryChecker |
| Create | `internal/agentcore/harness/security/checker_test.go` | Checker 单元测试 |
| Create | `internal/agentcore/harness/security/suggestions.go` | "始终允许"建议构建 |
| Create | `internal/agentcore/harness/security/suggestions_test.go` | Suggestions 单元测试 |
| Modify | `internal/agentcore/harness/security/models.go` | 补充 ToolPermissionHost / PermissionSceneHookInput / PermissionConfirmationRequest |
| Modify | `internal/agentcore/harness/security/models_test.go` | 补充新类型测试 |
| Create | `internal/agentcore/harness/security/permission_engine.go` | PermissionEngine 编排层 |
| Create | `internal/agentcore/harness/security/permission_engine_test.go` | PermissionEngine 单元测试 |
| Create | `internal/agentcore/harness/rails/security/tool_security_rail.go` | PermissionInterruptRail |
| Create | `internal/agentcore/harness/rails/security/tool_security_rail_test.go` | PermissionInterruptRail 单元测试 |
| Create | `internal/agentcore/harness/security/factory.go` | BuildPermissionInterruptRail |
| Create | `internal/agentcore/harness/security/factory_test.go` | Factory 单元测试 |
| Modify | `internal/agentcore/harness/security/doc.go` | 更新包文档 |
| Modify | `internal/agentcore/harness/factory.go` | SecurityRail 占位回填 |
| Modify | `internal/agentcore/harness/deep_agent.go` | PermissionInterruptRail 占位回填 |
| Modify | `go.mod` / `go.sum` | 新增 tree-sitter 依赖 |
| Modify | `IMPLEMENTATION_PLAN.md` | 更新 9.19-23 Security 状态 |

---

### Task 1: 新增 tree-sitter 依赖

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: 添加 tree-sitter 依赖**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
go get github.com/tree-sitter/go-tree-sitter@v0.25.0
go get github.com/tree-sitter/tree-sitter-bash@v0.25.1
go mod tidy
```

- [ ] **Step 2: 验证编译**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || true
CGO_ENABLED=1 go build ./internal/agentcore/...
```

Expected: 编译成功，无错误

- [ ] **Step 3: 提交**

```bash
git add go.mod go.sum
git commit -m "chore: 添加 tree-sitter go-tree-sitter v0.25.0 + tree-sitter-bash v0.25.1 依赖"
```

---

### Task 2: BaseSecurityRail + 决策类型

**Files:**
- Create: `internal/agentcore/harness/rails/security/doc.go`
- Create: `internal/agentcore/harness/rails/security/base_security_rail.go`
- Create: `internal/agentcore/harness/rails/security/base_security_rail_test.go`

**参考 Python:** `openjiuwen/harness/rails/security/base_security_rail.py` (748行)

- [ ] **Step 1: 创建 rails/security 包目录**

```bash
mkdir -p internal/agentcore/harness/rails/security
```

- [ ] **Step 2: 创建 doc.go**

创建 `internal/agentcore/harness/rails/security/doc.go`：

```go
// Package security 提供 Agent 安全护栏 Rail 实现。
//
// 包含两层安全机制：
//   - SafetyPromptRail（别名 SecurityRail）：在 LLM 调用前注入安全原则到 system prompt
//   - PermissionInterruptRail：拦截工具调用，通过分层策略评估权限
//
// BaseSecurityRail 为安全 Rail 抽象基类，定义了决策类型（Allow/Reject/Interrupt/Alert）
// 和统一的安全检查→决策应用流程（runAndApply → applySecurityDecision）。
//
// 文件目录：
//
//	security/
//	├── doc.go                   # 包文档
//	├── base_security_rail.go    # BaseSecurityRail + 决策类型 + apply 逻辑
//	├── prompt_security_rail.go  # SafetyPromptRail（别名 SecurityRail）
//	└── tool_security_rail.go    # PermissionInterruptRail
//
// 对应 Python 代码：openjiuwen/harness/rails/security/
package security
```

- [ ] **Step 3: 创建 base_security_rail.go — 决策类型**

创建 `internal/agentcore/harness/rails/security/base_security_rail.go`，首先定义决策类型：

```go
package security

import (
	"context"
	"fmt"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SecurityCheckContext 安全检查上下文
//
// 对齐 Python: SecurityCheckContext (base_security_rail.py L36-44)
type SecurityCheckContext struct {
	// CallbackCtx 回调上下文
	CallbackCtx *agentinterfaces.AgentCallbackContext
	// Event 触发的事件类型
	Event agentinterfaces.AgentCallbackEvent
	// UserInput 用户输入（中断恢复时存在）
	UserInput any
	// AutoConfirmConfig 自动确认配置
	AutoConfirmConfig map[string]any
	// SubjectID 主体标识（用于中断恢复匹配）
	SubjectID string
}

// securityDecisionTag 安全决策基类标记（非导出，仅用于类型区分）
//
// 对齐 Python: SecurityDecision (base_security_rail.py L47-49)
type securityDecisionTag struct{ tag string }

// SecurityAllow 允许执行
//
// 对齐 Python: SecurityAllow (base_security_rail.py L51-55)
type SecurityAllow struct {
	securityDecisionTag
	// NewArgs 可选，替换后的参数
	NewArgs string
}

// SecurityReject 拒绝执行
//
// 对齐 Python: SecurityReject (base_security_rail.py L58-64)
type SecurityReject struct {
	securityDecisionTag
	// Message 拒绝消息
	Message string
	// Result 预设结果（force_finish 场景使用）
	Result any
	// ToolMessage 预设的工具返回消息
	ToolMessage *llmschema.ToolMessage
}

// SecurityInterrupt 中断等待用户输入
//
// 对齐 Python: SecurityInterrupt (base_security_rail.py L67-72)
type SecurityInterrupt struct {
	securityDecisionTag
	// Request 中断请求
	Request saschema.InterruptRequester
	// SubjectID 主体标识
	SubjectID string
}

// SecurityAlertLevel 告警级别枚举
//
// 对齐 Python: SecurityAlertLevel (base_security_rail.py L75-83)
type SecurityAlertLevel int

const (
	// SecurityAlertLevelInfo 信息
	SecurityAlertLevelInfo SecurityAlertLevel = iota
	// SecurityAlertLevelWarning 警告
	SecurityAlertLevelWarning
	// SecurityAlertLevelError 错误
	SecurityAlertLevelError
	// SecurityAlertLevelCritical 严重
	SecurityAlertLevelCritical
)

// SecurityAlert 允许执行但告警
//
// 对齐 Python: SecurityAlert (base_security_rail.py L84-102)
type SecurityAlert struct {
	securityDecisionTag
	// Message 告警消息
	Message string
	// Level 告警级别
	Level SecurityAlertLevel
	// AlertType 告警类型（默认 "security"）
	AlertType string
	// DisplayMode 显示模式（默认 "popup"）
	DisplayMode string
}

// BaseSecurityRail 安全 Rail 抽象基类。
//
// 提供统一的安全检查→决策应用流程：
//   - runAndApply: 从 cbc 构造 SecurityCheckContext → 调用子类 runSecurityCheck → applySecurityDecision
//   - applySecurityDecision: 根据决策类型执行 Allow/Reject/Interrupt/Alert 分支
//   - handleInterruptResume: 中断恢复通用逻辑（auto_confirm 检查 → 解析用户输入 → store）
//
// 子类只需实现 runSecurityCheck 返回具体决策即可。
//
// 对齐 Python: BaseSecurityRail(AgentRail) (base_security_rail.py L105-748)
type BaseSecurityRail struct {
	rails.DeepAgentRail
	// supportedEvents 支持的事件集合
	supportedEvents map[agentinterfaces.AgentCallbackEvent]bool
	// toolNames 关联的工具名集合
	toolNames map[string]struct{}
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// baseSecurityRailPriority BaseSecurityRail 默认优先级
	// 对齐 Python: BaseSecurityRail.priority = 90
	baseSecurityRailPriority = 90
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证
var _ agentinterfaces.AgentRail = (*BaseSecurityRail)(nil)

var securityLogComponent = logger.ComponentAgentCore

// modelEvents 模型调用事件集合
var modelEvents = map[agentinterfaces.AgentCallbackEvent]bool{
	agentinterfaces.CallbackBeforeModelCall: true,
	agentinterfaces.CallbackAfterModelCall:  true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseSecurityRail 创建安全 Rail 基类实例。
//
// 对齐 Python: BaseSecurityRail.__init__(tool_names)
func NewBaseSecurityRail(opts ...SecurityRailOption) *BaseSecurityRail {
	r := &BaseSecurityRail{
		DeepAgentRail:    *rails.NewDeepAgentRail(),
		supportedEvents:  make(map[agentinterfaces.AgentCallbackEvent]bool),
		toolNames:        make(map[string]struct{}),
	}
	r.WithPriority(baseSecurityRailPriority)
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// SecurityRailOption BaseSecurityRail 配置选项
type SecurityRailOption func(*BaseSecurityRail)

// WithSupportedEvents 设置支持的事件集合
func WithSupportedEvents(events ...agentinterfaces.AgentCallbackEvent) SecurityRailOption {
	return func(r *BaseSecurityRail) {
		for _, e := range events {
			r.supportedEvents[e] = true
		}
	}
}

// WithSecurityToolNames 设置关联的工具名
func WithSecurityToolNames(names ...string) SecurityRailOption {
	return func(r *BaseSecurityRail) {
		for _, n := range names {
			r.toolNames[n] = struct{}{}
		}
	}
}

// Allow 返回允许决策。
//
// 对齐 Python: BaseSecurityRail.allow(new_args=None)
func (r *BaseSecurityRail) Allow(newArgs string) *SecurityAllow {
	return &SecurityAllow{NewArgs: newArgs}
}

// Approve 返回允许决策（Allow 别名）。
//
// 对齐 Python: BaseSecurityRail.approve(new_args=None)
func (r *BaseSecurityRail) Approve(newArgs string) *SecurityAllow {
	return r.Allow(newArgs)
}

// Reject 返回拒绝决策。
//
// 对齐 Python: BaseSecurityRail.reject(message, result, tool_result, tool_message)
func (r *BaseSecurityRail) Reject(message string, result any, toolMessage *llmschema.ToolMessage) *SecurityReject {
	return &SecurityReject{
		Message:     message,
		Result:      result,
		ToolMessage: toolMessage,
	}
}

// Interrupt 返回中断决策。
//
// 对齐 Python: BaseSecurityRail.interrupt(request, subject_id)
func (r *BaseSecurityRail) Interrupt(request saschema.InterruptRequester, subjectID string) *SecurityInterrupt {
	return &SecurityInterrupt{
		Request:   request,
		SubjectID: subjectID,
	}
}

// Alert 返回告警决策。
//
// 对齐 Python: BaseSecurityRail.alert(message, level, alert_type, display_mode)
func (r *BaseSecurityRail) Alert(message string, level SecurityAlertLevel, alertType string, displayMode string) *SecurityAlert {
	return &SecurityAlert{
		Message:     message,
		Level:       level,
		AlertType:   alertType,
		DisplayMode: displayMode,
	}
}

// AddTool 添加关联的工具名。
func (r *BaseSecurityRail) AddTool(toolName string) {
	r.toolNames[toolName] = struct{}{}
}

// AddTools 批量添加关联的工具名。
func (r *BaseSecurityRail) AddTools(toolNames []string) {
	for _, n := range toolNames {
		r.toolNames[n] = struct{}{}
	}
}

// GetTools 返回所有关联的工具名集合。
func (r *BaseSecurityRail) GetTools() map[string]struct{} {
	return r.toolNames
}

// GetCallbacks 覆盖基类回调映射，根据 supportedEvents 自动注册钩子。
//
// 对齐 Python: BaseSecurityRail.get_callbacks()
func (r *BaseSecurityRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	eventMethodMap := map[agentinterfaces.AgentCallbackEvent]func(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error{
		agentinterfaces.CallbackBeforeInvoke:  r.BeforeInvoke,
		agentinterfaces.CallbackAfterInvoke:   r.AfterInvoke,
		agentinterfaces.CallbackBeforeToolCall: r.BeforeToolCall,
		agentinterfaces.CallbackAfterToolCall:  r.AfterToolCall,
		agentinterfaces.CallbackBeforeModelCall: r.BeforeModelCall,
		agentinterfaces.CallbackAfterModelCall:  r.AfterModelCall,
	}

	for event, method := range eventMethodMap {
		if r.supportedEvents[event] {
			callbacks[event] = func(ctx context.Context, railCtx any) error {
				return method(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
			}
		}
	}

	return callbacks
}

// BeforeInvoke 调用前钩子（路由到 runAndApply）。
func (r *BaseSecurityRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.supportedEvents[agentinterfaces.CallbackBeforeInvoke] {
		return r.runAndApply(ctx, cbc, agentinterfaces.CallbackBeforeInvoke)
	}
	return nil
}

// AfterInvoke 调用后钩子（路由到 runAndApply）。
func (r *BaseSecurityRail) AfterInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.supportedEvents[agentinterfaces.CallbackAfterInvoke] {
		return r.runAndApply(ctx, cbc, agentinterfaces.CallbackAfterInvoke)
	}
	return nil
}

// BeforeToolCall 工具调用前钩子（路由到 runAndApply）。
func (r *BaseSecurityRail) BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.supportedEvents[agentinterfaces.CallbackBeforeToolCall] {
		return r.runAndApply(ctx, cbc, agentinterfaces.CallbackBeforeToolCall)
	}
	return nil
}

// AfterToolCall 工具调用后钩子（路由到 runAndApply）。
func (r *BaseSecurityRail) AfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.supportedEvents[agentinterfaces.CallbackAfterToolCall] {
		return r.runAndApply(ctx, cbc, agentinterfaces.CallbackAfterToolCall)
	}
	return nil
}

// BeforeModelCall 模型调用前钩子（路由到 runAndApply）。
func (r *BaseSecurityRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.supportedEvents[agentinterfaces.CallbackBeforeModelCall] {
		return r.runAndApply(ctx, cbc, agentinterfaces.CallbackBeforeModelCall)
	}
	return nil
}

// AfterModelCall 模型调用后钩子（路由到 runAndApply）。
func (r *BaseSecurityRail) AfterModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.supportedEvents[agentinterfaces.CallbackAfterModelCall] {
		return r.runAndApply(ctx, cbc, agentinterfaces.CallbackAfterModelCall)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runAndApply 执行安全检查并应用决策。
//
// 对齐 Python: BaseSecurityRail._run_and_apply(ctx, event)
// 1. 从 cbc 构造 SecurityCheckContext
// 2. 调用子类 runSecurityCheck → SecurityDecision
// 3. 调用 applySecurityDecision
func (r *BaseSecurityRail) runAndApply(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	event agentinterfaces.AgentCallbackEvent,
) error {
	securityCtx := r.buildSecurityCheckContext(cbc, event)

	decision, err := r.runSecurityCheck(ctx, securityCtx)
	if err != nil {
		logger.Error(securityLogComponent).
			Str("event", string(event)).
			Err(err).
			Msg("安全检查执行失败")
		return err
	}

	return r.applySecurityDecision(ctx, securityCtx, decision)
}

// runSecurityCheck 执行安全检查（抽象，子类实现）。
//
// 对齐 Python: BaseSecurityRail.run_security_check(security_ctx)
func (r *BaseSecurityRail) runSecurityCheck(_ context.Context, _ *SecurityCheckContext) (SecurityDecision, error) {
	return r.Allow(""), nil
}

// SecurityDecision 安全决策接口（所有决策类型实现此接口）
type SecurityDecision interface {
	isSecurityDecision()
}

func (a *SecurityAllow) isSecurityDecision()     {}
func (r *SecurityReject) isSecurityDecision()    {}
func (i *SecurityInterrupt) isSecurityDecision() {}
func (a *SecurityAlert) isSecurityDecision()     {}

// applySecurityDecision 应用安全决策。
//
// 对齐 Python: BaseSecurityRail.apply_security_decision(security_ctx, decision)
func (r *BaseSecurityRail) applySecurityDecision(
	_ context.Context,
	securityCtx *SecurityCheckContext,
	decision SecurityDecision,
) error {
	switch d := decision.(type) {
	case *SecurityAllow:
		// 允许：不做任何操作
		if d.NewArgs != "" {
			if toolInputs, ok := securityCtx.CallbackCtx.Inputs().(*agentinterfaces.ToolCallInputs); ok {
				toolInputs.ToolArgs = d.NewArgs
			}
		}
		return nil
	case *SecurityAlert:
		return r.applyAlert(securityCtx, d)
	case *SecurityReject:
		return r.applyReject(securityCtx, d)
	case *SecurityInterrupt:
		return r.applyInterrupt(securityCtx, d)
	default:
		return nil
	}
}

// applyReject 拒绝分支处理。
//
// 对齐 Python: BaseSecurityRail._apply_reject(security_ctx, decision)
// MODEL事件 → forceFinish; BEFORE_TOOL_CALL → skipTool; AFTER_TOOL_CALL → forceFinish + toolResult
func (r *BaseSecurityRail) applyReject(securityCtx *SecurityCheckContext, decision *SecurityReject) error {
	cbc := securityCtx.CallbackCtx
	event := securityCtx.Event

	if modelEvents[event] {
		// 模型调用事件：forceFinish
		result := r.buildForceFinishResult(decision)
		cbc.RequestForceFinish(result)
		return nil
	}

	if event == agentinterfaces.CallbackBeforeToolCall {
		// 工具调用前：skipTool
		r.skipTool(cbc, decision)
		return nil
	}

	if event == agentinterfaces.CallbackAfterToolCall {
		// 工具调用后：forceFinish + 设置 toolResult
		if toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs); ok {
			if decision.Result != nil {
				toolInputs.ToolResult = decision.Result
			}
			if decision.ToolMessage != nil {
				toolInputs.ToolMsg = decision.ToolMessage
			}
		}
		result := r.buildForceFinishResult(decision)
		cbc.RequestForceFinish(result)
		return nil
	}

	return nil
}

// applyInterrupt 中断分支处理。
//
// 对齐 Python: BaseSecurityRail._apply_interrupt(security_ctx, decision)
// MODEL事件 → 转为 Reject; TOOL事件 → raiseInterrupt
func (r *BaseSecurityRail) applyInterrupt(securityCtx *SecurityCheckContext, decision *SecurityInterrupt) error {
	cbc := securityCtx.CallbackCtx
	event := securityCtx.Event

	if modelEvents[event] {
		// 模型调用事件无法中断，转为拒绝
		logger.Warn(securityLogComponent).
			Str("event", string(event)).
			Msg("模型调用事件不支持中断，转为拒绝")
		reject := r.Reject(decision.Request.GetMessage(), nil, nil)
		return r.applyReject(securityCtx, reject)
	}

	// 工具调用事件：raiseInterrupt
	r.raiseToolInterrupt(cbc, decision)
	return nil
}

// applyAlert 告警分支处理。
//
// 对齐 Python: BaseSecurityRail._apply_alert(security_ctx, decision)
// 记录日志 + WriteStream OutputSchema with is_security_alert=true → 继续执行
func (r *BaseSecurityRail) applyAlert(securityCtx *SecurityCheckContext, decision *SecurityAlert) error {
	// 记录日志
	logger.Warn(securityLogComponent).
		Str("message", decision.Message).
		Str("level", decision.Level.String()).
		Str("alert_type", decision.AlertType).
		Msg("安全告警")

	// 流式推送到前端
	cbc := securityCtx.CallbackCtx
	if sess := cbc.Session(); sess != nil {
		_ = sess.WriteStream(context.Background(), map[string]any{
			"type": "message",
			"metadata": map[string]any{
				"is_security_alert": true,
				"alert_type":        decision.AlertType,
				"display_mode":      decision.DisplayMode,
				"level":             decision.Level.String(),
			},
			"content": decision.Message,
		})
	}

	return nil
}

// buildForceFinishResult 构建 forceFinish 结果。
//
// 对齐 Python: BaseSecurityRail._build_force_finish_result(decision)
func (r *BaseSecurityRail) buildForceFinishResult(decision *SecurityReject) map[string]any {
	if decision.Result != nil {
		if m, ok := decision.Result.(map[string]any); ok {
			return m
		}
	}
	msg := decision.Message
	if msg == "" {
		msg = "安全策略拒绝执行"
	}
	return map[string]any{
		"response": msg,
	}
}

// raiseToolInterrupt 抛出工具中断异常。
func (r *BaseSecurityRail) raiseToolInterrupt(cbc *agentinterfaces.AgentCallbackContext, decision *SecurityInterrupt) {
	var toolName string
	var toolCall *llmschema.ToolCall
	if toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs); ok {
		toolName = toolInputs.ToolName
		toolCall = toolInputs.ToolCall
	}
	exc := &saschema.ToolInterruptException{
		Request:  decision.Request,
		ToolCall: toolCall,
	}
	panic(cb.NewAbortError(
		fmt.Sprintf("Security interrupt: %s", toolName),
		exc,
	))
}

// skipTool 跳过工具执行。
func (r *BaseSecurityRail) skipTool(cbc *agentinterfaces.AgentCallbackContext, decision *SecurityReject) {
	if toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs); ok {
		cbc.Extra()["_skip_tool"] = true
		if decision.Result != nil {
			toolInputs.ToolResult = decision.Result
		} else {
			toolInputs.ToolResult = decision.Message
		}
		if decision.ToolMessage != nil {
			toolInputs.ToolMsg = decision.ToolMessage
		}
	}
}

// handleInterruptResume 中断恢复通用逻辑。
//
// 对齐 Python: BaseSecurityRail._handle_interrupt_resume(security_ctx, auto_confirm_key)
// 返回 nil 表示首次调用（走 Interrupt），非 nil 表示恢复决策。
func (r *BaseSecurityRail) handleInterruptResume(
	securityCtx *SecurityCheckContext,
	autoConfirmKey string,
) SecurityDecision {
	// 1. 检查 auto_confirm
	if r.isAutoConfirmed(securityCtx.AutoConfirmConfig, autoConfirmKey) {
		return r.Allow("")
	}

	// 2. 获取 userInput
	userInput := securityCtx.UserInput
	if userInput == nil {
		return nil // 首次调用，走 Interrupt
	}

	// 3. 解析 userInput
	// 对齐 Python: _handle_interrupt_resume 中解析交互输入
	if m, ok := userInput.(map[string]any); ok {
		approved, _ := m["approved"].(bool)
		autoConfirm, _ := m["auto_confirm"].(bool)
		feedback, _ := m["feedback"].(string)

		if approved {
			if autoConfirm {
				r.storeAutoConfirm(securityCtx.CallbackCtx, autoConfirmKey)
			}
			return r.Allow("")
		}
		return r.Reject(feedback, nil, nil)
	}

	return r.Reject("用户拒绝", nil, nil)
}

// isAutoConfirmed 检查 auto_confirm 配置。
//
// 对齐 Python: BaseSecurityRail._is_auto_confirmed(config, key)
func (r *BaseSecurityRail) isAutoConfirmed(config map[string]any, key string) bool {
	if config == nil || key == "" {
		return false
	}
	val, ok := config[key]
	if !ok {
		return false
	}
	// 复用 interrupt 包的 truthiness 判断
	return isSecurityAutoConfirmed(val)
}

// isSecurityAutoConfirmed 宽松真值判断（对齐 Python bool(val)）
func isSecurityAutoConfirmed(val any) bool {
	if val == nil {
		return false
	}
	if b, ok := val.(bool); ok {
		return b
	}
	return false
}

// storeAutoConfirm 写入 auto_confirm 到 session 状态。
//
// 对齐 Python: BaseSecurityRail._store_auto_confirm(ctx, auto_confirm_key)
func (r *BaseSecurityRail) storeAutoConfirm(cbc *agentinterfaces.AgentCallbackContext, autoConfirmKey string) {
	sess := cbc.Session()
	if sess == nil || autoConfirmKey == "" {
		return
	}
	var config map[string]any
	if val, err := sess.GetState(state.StringKey(saschema.InterruptAutoConfirmKey)); err == nil && val != nil {
		if m, ok := val.(map[string]any); ok {
			config = m
		}
	}
	if config == nil {
		config = make(map[string]any)
	}
	config[autoConfirmKey] = true
	sess.UpdateState(map[string]any{
		string(state.StringKey(saschema.InterruptAutoConfirmKey)): config,
	})
}

// buildSecurityCheckContext 从回调上下文构造安全检查上下文。
func (r *BaseSecurityRail) buildSecurityCheckContext(
	cbc *agentinterfaces.AgentCallbackContext,
	event agentinterfaces.AgentCallbackEvent,
) *SecurityCheckContext {
	subjectID := r.resolveSubjectID(cbc, event)
	userInput := r.getUserInput(cbc, subjectID)
	autoConfirmConfig := r.getAutoConfirmConfig(cbc)

	return &SecurityCheckContext{
		CallbackCtx:       cbc,
		Event:             event,
		UserInput:         userInput,
		AutoConfirmConfig: autoConfirmConfig,
		SubjectID:         subjectID,
	}
}

// resolveSubjectID 解析 subject_id（用于中断恢复匹配）。
func (r *BaseSecurityRail) resolveSubjectID(cbc *agentinterfaces.AgentCallbackContext, event agentinterfaces.AgentCallbackEvent) string {
	if event == agentinterfaces.CallbackBeforeToolCall || event == agentinterfaces.CallbackAfterToolCall {
		if toolInputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs); ok && toolInputs.ToolCall != nil {
			return toolInputs.ToolCall.ID
		}
	}
	return ""
}

// getUserInput 从 session 获取用户输入。
func (r *BaseSecurityRail) getUserInput(cbc *agentinterfaces.AgentCallbackContext, subjectID string) any {
	rawInput, exists := cbc.Extra()[saschema.ResumeUserInputKey]
	if !exists || rawInput == nil {
		return nil
	}
	if interactive, ok := rawInput.(*sessioninteraction.InteractiveInput); ok {
		if val, found := interactive.UserInputs[subjectID]; found {
			return val
		}
		return nil
	}
	return rawInput
}

// getAutoConfirmConfig 从 session 获取 auto_confirm 配置。
func (r *BaseSecurityRail) getAutoConfirmConfig(cbc *agentinterfaces.AgentCallbackContext) map[string]any {
	sess := cbc.Session()
	if sess == nil {
		return nil
	}
	val, err := sess.GetState(state.StringKey(saschema.InterruptAutoConfirmKey))
	if err != nil || val == nil {
		return nil
	}
	if m, ok := val.(map[string]any); ok {
		return m
	}
	return nil
}

// String 返回告警级别的字符串表示
func (l SecurityAlertLevel) String() string {
	switch l {
	case SecurityAlertLevelInfo:
		return "info"
	case SecurityAlertLevelWarning:
		return "warning"
	case SecurityAlertLevelError:
		return "error"
	case SecurityAlertLevelCritical:
		return "critical"
	default:
		return fmt.Sprintf("unknown(%d)", l)
	}
}
```

- [ ] **Step 4: 创建 base_security_rail_test.go**

创建 `internal/agentcore/harness/rails/security/base_security_rail_test.go`，覆盖 4种决策类型的 apply 行为、runAndApply 调度、handleInterruptResume：

```go
package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// mockBaseSecurityRail 用于测试的 BaseSecurityRail 子类
type mockBaseSecurityRail struct {
	BaseSecurityRail
	decision SecurityDecision
	err      error
}

func newMockBaseSecurityRail(decision SecurityDecision) *mockBaseSecurityRail {
	r := &mockBaseSecurityRail{
		BaseSecurityRail: *NewBaseSecurityRail(
			WithSupportedEvents(agentinterfaces.CallbackBeforeToolCall, agentinterfaces.CallbackBeforeModelCall),
		),
		decision: decision,
	}
	return r
}

func (m *mockBaseSecurityRail) runSecurityCheck(_ context.Context, _ *SecurityCheckContext) (SecurityDecision, error) {
	return m.decision, m.err
}

// TestNewBaseSecurityRail 测试创建 BaseSecurityRail
func TestNewBaseSecurityRail(t *testing.T) {
	r := NewBaseSecurityRail(
		WithSupportedEvents(agentinterfaces.CallbackBeforeModelCall),
		WithSecurityToolNames("bash"),
	)
	assert.Equal(t, baseSecurityRailPriority, r.Priority())
	assert.True(t, r.supportedEvents[agentinterfaces.CallbackBeforeModelCall])
	assert.Contains(t, r.toolNames, "bash")
}

// TestSecurityAllow 测试允许决策
func TestSecurityAllow(t *testing.T) {
	r := NewBaseSecurityRail()
	allow := r.Allow("")
	assert.NotNil(t, allow)
	assert.Equal(t, "", allow.NewArgs)

	allowWithArgs := r.Allow(`{"command": "ls"}`)
	assert.Equal(t, `{"command": "ls"}`, allowWithArgs.NewArgs)
}

// TestSecurityReject 测试拒绝决策
func TestSecurityReject(t *testing.T) {
	r := NewBaseSecurityRail()
	reject := r.Reject("危险操作", "已拒绝", nil)
	assert.Equal(t, "危险操作", reject.Message)
	assert.Equal(t, "已拒绝", reject.Result)
}

// TestSecurityInterrupt 测试中断决策
func TestSecurityInterrupt(t *testing.T) {
	r := NewBaseSecurityRail()
	req := &saschema.InterruptRequest{Message: "确认执行?"}
	interrupt := r.Interrupt(req, "tool-123")
	assert.Equal(t, req, interrupt.Request)
	assert.Equal(t, "tool-123", interrupt.SubjectID)
}

// TestSecurityAlert 测试告警决策
func TestSecurityAlert(t *testing.T) {
	r := NewBaseSecurityRail()
	alert := r.Alert("可疑操作", SecurityAlertLevelWarning, "security", "popup")
	assert.Equal(t, "可疑操作", alert.Message)
	assert.Equal(t, SecurityAlertLevelWarning, alert.Level)
	assert.Equal(t, "security", alert.AlertType)
	assert.Equal(t, "popup", alert.DisplayMode)
}

// TestSecurityAlertLevel_String 测试告警级别字符串表示
func TestSecurityAlertLevel_String(t *testing.T) {
	assert.Equal(t, "info", SecurityAlertLevelInfo.String())
	assert.Equal(t, "warning", SecurityAlertLevelWarning.String())
	assert.Equal(t, "error", SecurityAlertLevelError.String())
	assert.Equal(t, "critical", SecurityAlertLevelCritical.String())
}

// TestBuildForceFinishResult 测试构建 forceFinish 结果
func TestBuildForceFinishResult(t *testing.T) {
	r := NewBaseSecurityRail()

	// 有 map 结果
	reject := r.Reject("", map[string]any{"status": "denied"}, nil)
	result := r.buildForceFinishResult(reject)
	assert.Equal(t, map[string]any{"status": "denied"}, result)

	// 无结果，使用消息
	rejectNoResult := r.Reject("操作被拒绝", nil, nil)
	result2 := r.buildForceFinishResult(rejectNoResult)
	assert.Equal(t, "操作被拒绝", result2["response"])

	// 无结果无消息
	rejectEmpty := r.Reject("", nil, nil)
	result3 := r.buildForceFinishResult(rejectEmpty)
	assert.Equal(t, "安全策略拒绝执行", result3["response"])
}

// TestHandleInterruptResume_首次调用 测试中断恢复首次调用
func TestHandleInterruptResume_首次调用(t *testing.T) {
	r := NewBaseSecurityRail()
	ctx := &SecurityCheckContext{
		UserInput:         nil,
		AutoConfirmConfig: nil,
	}
	decision := r.handleInterruptResume(ctx, "bash")
	assert.Nil(t, decision) // 首次调用，返回 nil
}

// TestHandleInterruptResume_autoConfirm 测试中断恢复已自动确认
func TestHandleInterruptResume_autoConfirm(t *testing.T) {
	r := NewBaseSecurityRail()
	ctx := &SecurityCheckContext{
		UserInput: nil,
		AutoConfirmConfig: map[string]any{
			"bash": true,
		},
	}
	decision := r.handleInterruptResume(ctx, "bash")
	assert.NotNil(t, decision)
	allow, ok := decision.(*SecurityAllow)
	require.True(t, ok)
	assert.NotNil(t, allow)
}

// TestHandleInterruptResume_用户批准 测试中断恢复用户批准
func TestHandleInterruptResume_用户批准(t *testing.T) {
	r := NewBaseSecurityRail()
	ctx := &SecurityCheckContext{
		UserInput: map[string]any{
			"approved":     true,
			"auto_confirm": false,
			"feedback":     "",
		},
		AutoConfirmConfig: nil,
	}
	decision := r.handleInterruptResume(ctx, "bash")
	assert.NotNil(t, decision)
	_, ok := decision.(*SecurityAllow)
	require.True(t, ok)
}

// TestHandleInterruptResume_用户拒绝 测试中断恢复用户拒绝
func TestHandleInterruptResume_用户拒绝(t *testing.T) {
	r := NewBaseSecurityRail()
	ctx := &SecurityCheckContext{
		UserInput: map[string]any{
			"approved": false,
			"feedback": "太危险",
		},
		AutoConfirmConfig: nil,
	}
	decision := r.handleInterruptResume(ctx, "bash")
	assert.NotNil(t, decision)
	reject, ok := decision.(*SecurityReject)
	require.True(t, ok)
	assert.Equal(t, "太危险", reject.Message)
}

// TestIsAutoConfirmed 测试自动确认检查
func TestIsAutoConfirmed(t *testing.T) {
	r := NewBaseSecurityRail()

	assert.False(t, r.isAutoConfirmed(nil, "bash"))
	assert.False(t, r.isAutoConfirmed(map[string]any{}, "bash"))
	assert.False(t, r.isAutoConfirmed(map[string]any{"bash": false}, "bash"))
	assert.True(t, r.isAutoConfirmed(map[string]any{"bash": true}, "bash"))
	assert.False(t, r.isAutoConfirmed(map[string]any{}, ""))
}

// TestAddTool 测试添加工具名
func TestAddTool(t *testing.T) {
	r := NewBaseSecurityRail()
	r.AddTool("bash")
	assert.Contains(t, r.toolNames, "bash")
	r.AddTools([]string{"read_file", "write_file"})
	assert.Contains(t, r.toolNames, "read_file")
	assert.Contains(t, r.toolNames, "write_file")
}
```

- [ ] **Step 5: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || true
CGO_ENABLED=1 go build ./internal/agentcore/harness/rails/security/
```

Expected: 编译成功

- [ ] **Step 6: 运行测试**

```bash
CGO_ENABLED=1 go test -v -tags test ./internal/agentcore/harness/rails/security/
```

Expected: 所有测试通过

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/harness/rails/security/
git commit -m "feat: 实现 BaseSecurityRail + 4种安全决策类型

对齐 Python BaseSecurityRail (base_security_rail.py)
- SecurityAllow/Reject/Interrupt/Alert 决策类型
- runAndApply → applySecurityDecision 调度框架
- handleInterruptResume 中断恢复通用逻辑
- applyReject/applyInterrupt/applyAlert 分支处理
- buildForceFinishResult + skipTool + raiseToolInterrupt"
```

---

### Task 3: SafetyPromptRail（别名 SecurityRail）

**Files:**
- Create: `internal/agentcore/harness/rails/security/prompt_security_rail.go`
- Create: `internal/agentcore/harness/rails/security/prompt_security_rail_test.go`

**参考 Python:** `openjiuwen/harness/rails/security/prompt_security_rail.py` (56行)

- [ ] **Step 1: 创建 prompt_security_rail.go**

创建 `internal/agentcore/harness/rails/security/prompt_security_rail.go`：

```go
package security

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SafetyPromptRail 安全提示词 Rail。
//
// 在每次模型调用前注入安全原则到 system prompt，引导模型自律。
// priority=85，事件集={BeforeModelCall}。
//
// 对齐 Python: SafetyPromptRail(BaseSecurityRail) — prompt_security_rail.py
type SafetyPromptRail struct {
	BaseSecurityRail
	// systemPromptBuilder 系统提示词构建器（init 时获取）
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
}

// SecurityRail 类型别名，对齐 Python: SecurityRail = SafetyPromptRail
type SecurityRail = SafetyPromptRail

// ──────────────────────────── 常量 ────────────────────────────

const (
	// safetyPromptRailPriority 安全提示词 Rail 优先级
	// 对齐 Python: SafetyPromptRail.priority = 85
	safetyPromptRailPriority = 85
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSafetyPromptRail 创建安全提示词 Rail。
//
// 对齐 Python: SafetyPromptRail()
func NewSafetyPromptRail() *SafetyPromptRail {
	r := &SafetyPromptRail{
		BaseSecurityRail: *NewBaseSecurityRail(
			WithSupportedEvents(agentinterfaces.CallbackBeforeModelCall),
		),
	}
	r.WithPriority(safetyPromptRailPriority)
	return r
}

// Init 初始化钩子，获取 systemPromptBuilder 引用。
//
// 对齐 Python: SafetyPromptRail.init(agent)
func (r *SafetyPromptRail) Init(agent agentinterfaces.BaseAgent) error {
	r.systemPromptBuilder = agent.SystemPromptBuilder()
	return nil
}

// Uninit 反初始化钩子，移除 safety section。
//
// 对齐 Python: SafetyPromptRail.uninit(agent)
func (r *SafetyPromptRail) Uninit(agent agentinterfaces.BaseAgent) error {
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection(sections.SectionSafety)
		r.systemPromptBuilder = nil
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runSecurityCheck 注入安全提示词 section，始终返回 Allow。
//
// 对齐 Python: SafetyPromptRail.run_security_check(security_ctx)
func (r *SafetyPromptRail) runSecurityCheck(_ context.Context, _ *SecurityCheckContext) (SecurityDecision, error) {
	if r.systemPromptBuilder != nil {
		section := sections.BuildSafetySection()
		r.systemPromptBuilder.AddSection(section)
		logger.Debug(securityLogComponent).
			Str("section", sections.SectionSafety).
			Msg("已注入安全提示词 section")
	}
	return r.Allow(""), nil
}
```

- [ ] **Step 2: 创建 prompt_security_rail_test.go**

创建 `internal/agentcore/harness/rails/security/prompt_security_rail_test.go`：

```go
package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// mockSystemPromptBuilder 用于测试的 SystemPromptBuilder mock
type mockSystemPromptBuilder struct {
	sections map[string]saprompt.PromptSection
	language  string
}

func newMockSystemPromptBuilder() *mockSystemPromptBuilder {
	return &mockSystemPromptBuilder{
		sections: make(map[string]saprompt.PromptSection),
		language: "cn",
	}
}

func (m *mockSystemPromptBuilder) AddSection(section saprompt.PromptSection) *saprompt.SystemPromptBuilder {
	m.sections[section.Name] = section
	return nil
}

func (m *mockSystemPromptBuilder) RemoveSection(name string) *saprompt.SystemPromptBuilder {
	delete(m.sections, name)
	return nil
}

func (m *mockSystemPromptBuilder) Language() string                                              { return m.language }
func (m *mockSystemPromptBuilder) GetSection(name string) *saprompt.PromptSection                { return nil }
func (m *mockSystemPromptBuilder) HasSection(name string) bool                                   { return false }

// TestNewSafetyPromptRail 测试创建 SafetyPromptRail
func TestNewSafetyPromptRail(t *testing.T) {
	r := NewSafetyPromptRail()
	assert.Equal(t, safetyPromptRailPriority, r.Priority())
	assert.True(t, r.supportedEvents[agentinterfaces.CallbackBeforeModelCall])
}

// TestSafetyPromptRail_InitUninit 测试初始化和反初始化
func TestSafetyPromptRail_InitUninit(t *testing.T) {
	r := NewSafetyPromptRail()
	builder := newMockSystemPromptBuilder()

	// Init
	r.systemPromptBuilder = builder
	assert.NotNil(t, r.systemPromptBuilder)

	// Uninit
	r.Uninit(nil)
	assert.Nil(t, r.systemPromptBuilder)
}

// TestSafetyPromptRail_RunSecurityCheck 测试注入安全提示词
func TestSafetyPromptRail_RunSecurityCheck(t *testing.T) {
	r := NewSafetyPromptRail()
	builder := newMockSystemPromptBuilder()
	r.systemPromptBuilder = builder

	decision, err := r.runSecurityCheck(nil, nil)
	require.NoError(t, err)
	allow, ok := decision.(*SecurityAllow)
	require.True(t, ok)
	assert.NotNil(t, allow)

	// 验证 section 被注入
	_, exists := builder.sections[sections.SectionSafety]
	assert.True(t, exists, "safety section 应被注入到 system prompt builder")
}

// TestSafetyPromptRail_SecurityRailAlias 测试 SecurityRail 类型别名
func TestSafetyPromptRail_SecurityRailAlias(t *testing.T) {
	var _ SecurityRail = &SafetyPromptRail{}
	// 类型别名编译通过即可
	assert.True(t, true)
}
```

- [ ] **Step 3: 确认 sections.SectionSafety 常量存在**

检查 `internal/agentcore/harness/prompts/sections/safety.go` 中是否导出了 `SectionSafety` 常量。如果不存在，需要在 safety.go 中添加：

```go
const SectionSafety = "safety"
```

- [ ] **Step 4: 编译验证**

```bash
CGO_ENABLED=1 go build ./internal/agentcore/harness/rails/security/
```

- [ ] **Step 5: 运行测试**

```bash
CGO_ENABLED=1 go test -v -tags test ./internal/agentcore/harness/rails/security/
```

- [ ] **Step 6: 回填 factory.go SecurityRail 占位**

修改 `internal/agentcore/harness/factory.go`：

将 SecurityRail 占位：
```go
// SecurityRail — 始终添加
// ⤵️ 9.8-9.24 回填：SecurityRail 具体实例化
if !alreadyProvidedByType(userProvidedTypes, nil) {
    agent.AddRail(agentinterfaces.NewBaseRail())
    logger.Debug(logComponent).Msg("已添加默认 SecurityRail 占位，⤵️ 9.8-9.24 回填")
}
```

替换为：
```go
// SecurityRail（SafetyPromptRail）— 始终添加
// ⤴️ 9.19-23 回填：SafetyPromptRail 具体实例化
if !alreadyProvidedByType(userProvidedTypes, reflect.TypeOf(&security2.SafetyPromptRail{})) {
    agent.AddRail(security2.NewSafetyPromptRail())
    logger.Debug(logComponent).Msg("已添加 SafetyPromptRail（SecurityRail）")
}
```

并添加 import：`security2 "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"`

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/harness/rails/security/ internal/agentcore/harness/factory.go
git commit -m "feat: 实现 SafetyPromptRail（SecurityRail 别名）+ factory.go 回填

对齐 Python SafetyPromptRail (prompt_security_rail.py)
- before_model_call 注入 BuildSafetySection() 到 system prompt
- priority=85, SecurityRail = SafetyPromptRail 类型别名
- factory.go SecurityRail 占位替换为 NewSafetyPromptRail()"
```

---

### Task 4: ShellAST — tree-sitter bash 解析

**Files:**
- Create: `internal/agentcore/harness/security/shell_ast.go`
- Create: `internal/agentcore/harness/security/shell_ast_test.go`

**参考 Python:** `openjiuwen/harness/security/shell_ast.py` (~300行)

- [ ] **Step 1: 创建 shell_ast.go**

创建 `internal/agentcore/harness/security/shell_ast.go`，包含 tree-sitter 精确解析 + 保守扫描 fallback。

关键类型：`ShellAstKind` 枚举（Simple/TooComplex/ParseUnavailable）、`ShellStructureFlags`（11 个 bool 标志）、`ShellSubcommand`（Text/Argv/Redirects）、`ShellAstParseResult`、`ParseShellForPermission()` 函数。

tree-sitter 解析流程：
1. 创建 Parser，设置 bash Language
2. 解析代码获取 Tree/RootNode
3. 遍历 AST 收集 StructureFlags（检测 pipeline/command_substitution/process_substitution/parameter_expansion/heredoc/subshell/command_group）
4. 如有风险结构 → TooComplex
5. 否则提取子命令 → Simple

保守扫描 fallback：
1. 正则扫描 `[;&|`<>]|\$[({]|\r?\n` + heredoc
2. 如有风险 → ParseUnavailable
3. 否则 shlex 分词 → Simple

- [ ] **Step 2: 创建 shell_ast_test.go**

测试用例覆盖：
- 简单命令 `"ls -la"` → Simple, 1 subcommand
- 管道 `"echo hello | grep h"` → Simple, 2 subcommands, Pipeline=true
- 复合命令 `"ls && pwd"` → Simple, 2 subcommands, CompoundOperators=true
- 命令替换 `"$(whoami)"` → TooComplex, CommandSubstitution=true
- Heredoc `"cat <<EOF\nhello\nEOF"` → TooComplex, Heredoc=true
- 进程替换 `"<(cat file)"` → TooComplex, ProcessSubstitution=true
- 分号 `"ls; rm -rf /"` → Simple, 2 subcommands
- 空命令 `""` → Simple, 0 subcommands

- [ ] **Step 3: 编译验证**

```bash
CGO_ENABLED=1 go build ./internal/agentcore/harness/security/
```

- [ ] **Step 4: 运行测试**

```bash
CGO_ENABLED=1 go test -v -tags test ./internal/agentcore/harness/security/ -run TestShellAst
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/security/shell_ast.go internal/agentcore/harness/security/shell_ast_test.go
git commit -m "feat: 实现 ShellAST — tree-sitter bash 解析 + 保守扫描 fallback

对齐 Python shell_ast.py
- ParseShellForPermission: 双后端策略（tree-sitter 精确 + 保守正则 fallback）
- ShellStructureFlags: 11 个结构标志（Pipeline/Heredoc/CommandSubstitution 等）
- ShellSubcommand: 子命令提取（Text/Argv/Redirects）
- 风险结构检测 → TooComplex，安全命令 → Simple"
```

---

### Task 5: TieredPolicy — 分层策略评估

**Files:**
- Create: `internal/agentcore/harness/security/tiered_policy.go`
- Create: `internal/agentcore/harness/security/tiered_policy_test.go`

**参考 Python:** `openjiuwen/harness/security/tiered_policy.py` (607行) ★最复杂

- [ ] **Step 1: 创建 tiered_policy.go**

创建 `internal/agentcore/harness/security/tiered_policy.go`，核心函数 `EvaluateTieredPolicy()`。

常量：`shellTools`/`pathTools`/`networkTools`/`pathArgKeys` 集合。

优先级链：
1. tool-level deny baseline → DENY 短路
2. Shell 工具: ParseShellForPermission → shellAstFloor
3. Shell "simple" 含子命令: 逐个 evaluateSingleInvocation → aggregateSubcommandResults
4. evaluateSingleInvocation: builtin-param-rules(DENY短路) → user-param-rules(DENY短路) → approval-overrides(ALLOW) → builtin-hits → user-hits → baseline → defaults → fallback-ASK
5. applyShellAstFloor
6. MaybeEscalateShellOperators（ALLOW→ASK，approval_overrides 豁免）

辅助函数：`collectParamRuleHits`/`collectApprovalOverrideHits`/`baselineLevel`/`finalizeHits`/`shellAstFloor`/`severityToDecision`/`strictest` 等。

- [ ] **Step 2: 创建 tiered_policy_test.go**

测试用例覆盖：
- 无配置 → 默认 ASK
- tool-level deny → DENY 短路
- builtin rule CRITICAL + normal mode → ASK
- builtin rule CRITICAL + strict mode → DENY
- approval_override 匹配 → ALLOW（豁免 shell operators 升级）
- shell 管道命令 → ASK（shell_ast_floor）
- defaults.allow → ALLOW
- shell 元字符（`rm -rf /`）→ ALLOW 升级为 ASK

- [ ] **Step 3: 编译验证**

```bash
CGO_ENABLED=1 go build ./internal/agentcore/harness/security/
```

- [ ] **Step 4: 运行测试**

```bash
CGO_ENABLED=1 go test -v -tags test ./internal/agentcore/harness/security/ -run TestTieredPolicy
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/security/tiered_policy.go internal/agentcore/harness/security/tiered_policy_test.go
git commit -m "feat: 实现 TieredPolicy 分层策略评估

对齐 Python tiered_policy.py (607行)
- EvaluateTieredPolicy: 优先级链（tool-deny > shell-ast > builtin-param > user-param > approval-overrides > baseline > defaults）
- ShellAST floor: 管道/重定向等结构升级权限
- MaybeEscalateShellOperators: shell 元字符 ALLOW→ASK
- severity_to_decision: CRITICAL→ASK(normal)/DENY(strict)"
```

---

### Task 6: Patterns — 通配符/路径匹配 + YAML 持久化

**Files:**
- Create: `internal/agentcore/harness/security/patterns.go`
- Create: `internal/agentcore/harness/security/patterns_test.go`

**参考 Python:** `openjiuwen/harness/security/patterns.py` (617行)

- [ ] **Step 1: 创建 patterns.go**

关键函数：
- `MatchWildcard(value, pattern) bool`：`*` → 安全字符类 `[-a-zA-Z0-9 \\._/:"\']*`，`?` → 单字符，尾部 ` *` → 可选参数，`regexp.FullMatchString` 全串锚定
- `PatternMatcher`/`PathMatcher`/`URLMatcher`/`CommandMatcher` 匹配器
- `BuildCommandAllowPattern(cmd) string`
- `ContainsPath(parent, child) bool`：路径包含检查（防路径遍历）
- `WritePermissionsSectionToAgentConfigYAML(path, permissions) bool`
- `MergePermissionAllowRuleIntoPermissions(permissions, toolName, toolArgs) (PermissionsSection, bool)`
- `MergeExternalDirectoryAllowIntoPermissions(permissions, paths) (PermissionsSection, bool)`
- `PersistCliTrustedDirectory(rawPath, ...) map[string]any`

- [ ] **Step 2: 创建 patterns_test.go**

测试覆盖：
- `MatchWildcard` 边界：注入字符（`$(whoami)` 不匹配）、路径遍历、Unicode
- `ContainsPath`：正常包含、路径遍历、符号链接
- `BuildCommandAllowPattern`：简单命令、带参数
- YAML 读写往返

- [ ] **Step 3: 编译验证**

```bash
CGO_ENABLED=1 go build ./internal/agentcore/harness/security/
```

- [ ] **Step 4: 运行测试**

```bash
CGO_ENABLED=1 go test -v -tags test ./internal/agentcore/harness/security/ -run TestPattern
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/security/patterns.go internal/agentcore/harness/security/patterns_test.go
git commit -m "feat: 实现 Patterns 通配符/路径匹配 + YAML 持久化

对齐 Python patterns.py (617行)
- MatchWildcard: 安全字符类防 shell 注入
- PatternMatcher/PathMatcher/URLMatcher/CommandMatcher
- ContainsPath: 路径包含检查防路径遍历
- WritePermissionsSectionToAgentConfigYAML + Merge 权限持久化"
```

---

### Task 7: Checker + Suggestions

**Files:**
- Create: `internal/agentcore/harness/security/checker.go`
- Create: `internal/agentcore/harness/security/checker_test.go`
- Create: `internal/agentcore/harness/security/suggestions.go`
- Create: `internal/agentcore/harness/security/suggestions_test.go`

**参考 Python:** `openjiuwen/harness/security/checker.py` (198行) + `openjiuwen/harness/security/suggestions.py` (225行)

- [ ] **Step 1: 创建 checker.go**

`ExternalDirectoryChecker`：从 Shell/路径工具提取路径，检测工作空间外路径，检查 `external_directory` 配置。

辅助函数：`extractPathsFromCommand`（shlex 分词 + 路径检测）、`looksLikePath`（启发式路径检测）。

- [ ] **Step 2: 创建 checker_test.go**

测试覆盖：工作空间内路径→nil，工作空间外路径→DENY/ASK，external_directory allow→ALLOW，Shell 命令路径提取。

- [ ] **Step 3: 创建 suggestions.go**

`PermissionSuggestion` 结构体（Tools/MatchType/Pattern/Action/Scope/Reason）。

`BuildPermissionSuggestions`/`BuildShellPermissionSuggestions`：Shell 命令建议（前缀模式/exact 模式）、路径建议。

辅助函数：`buildSingleShellSuggestion`/`extractPrefixBeforeHeredoc`/`buildPrefixPattern`/`buildPathPermissionSuggestion`/`dedupeSuggestions`。

- [ ] **Step 4: 创建 suggestions_test.go**

测试覆盖：Shell 简单命令建议、复杂命令无建议、路径建议、去重。

- [ ] **Step 5: 编译验证**

```bash
CGO_ENABLED=1 go build ./internal/agentcore/harness/security/
```

- [ ] **Step 6: 运行测试**

```bash
CGO_ENABLED=1 go test -v -tags test ./internal/agentcore/harness/security/ -run "TestChecker|TestSuggestion"
```

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/harness/security/checker.go internal/agentcore/harness/security/checker_test.go internal/agentcore/harness/security/suggestions.go internal/agentcore/harness/security/suggestions_test.go
git commit -m "feat: 实现 ExternalDirectoryChecker + PermissionSuggestions

对齐 Python checker.py + suggestions.py
- ExternalDirectoryChecker: 工作空间外路径检测
- extractPathsFromCommand: Shell 命令路径提取
- BuildPermissionSuggestions: Shell/路径权限建议构建"
```

---

### Task 8: models.go 补充 + PermissionEngine

**Files:**
- Modify: `internal/agentcore/harness/security/models.go`
- Modify: `internal/agentcore/harness/security/models_test.go`
- Modify: `internal/agentcore/harness/security/doc.go`
- Create: `internal/agentcore/harness/security/permission_engine.go`
- Create: `internal/agentcore/harness/security/permission_engine_test.go`

**参考 Python:** `openjiuwen/harness/security/core.py` (234行) + `openjiuwen/harness/security/host.py` (110行)

- [ ] **Step 1: 补充 models.go**

在现有类型后追加：
- `PermissionSceneHookInput` 结构体
- `PermissionConfirmationRequest` 结构体
- `ToolPermissionHost` 结构体
- `PermissionSceneHookFn` / `PermissionConfirmationHook` 函数类型

- [ ] **Step 2: 更新 models_test.go**

补充新类型的构造和字段测试。

- [ ] **Step 3: 创建 permission_engine.go**

`PermissionEngine` 结构体：config/llm/modelName/workspaceRoot/permissionChecksActive/enabled。

方法：
- `NewPermissionEngine()`
- `CheckPermission(ctx, toolName, toolArgs) (*PermissionResult, error)`：检查 enabled → EvaluateGlobalPolicyDirectly → ExternalDirectoryChecker → Strictest
- `CheckToolPermissionDirectly(toolName, toolArgs) (PermissionLevel, string)`
- `EvaluateGlobalPolicyDirectly(toolName, toolArgs) (PermissionLevel, string)`
- `UpdateConfig/UpdateLLM/SetPermissionChecksActive/Enabled`

- [ ] **Step 4: 创建 permission_engine_test.go**

测试覆盖：
- enabled=false → Allow
- permissionChecksActive=false → Allow
- TieredPolicy DENY → DENY
- ExternalDirectory DENY + TieredPolicy ALLOW → DENY（取最严格）
- 无匹配 → 默认 ASK

- [ ] **Step 5: 更新 doc.go**

更新文件目录列表。

- [ ] **Step 6: 编译验证**

```bash
CGO_ENABLED=1 go build ./internal/agentcore/harness/security/
```

- [ ] **Step 7: 运行测试**

```bash
CGO_ENABLED=1 go test -v -tags test ./internal/agentcore/harness/security/
```

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/harness/security/
git commit -m "feat: 补充 ToolPermissionHost + 实现 PermissionEngine

对齐 Python core.py + host.py
- ToolPermissionHost: 宿主注入钩子（scene_hook/confirmation/workspace/persistence）
- PermissionEngine: 组合 TieredPolicy + ExternalDirectoryChecker 评估
- CheckPermission: enabled/active 标志 + Strictest 取值"
```

---

### Task 9: PermissionInterruptRail

**Files:**
- Create: `internal/agentcore/harness/rails/security/tool_security_rail.go`
- Create: `internal/agentcore/harness/rails/security/tool_security_rail_test.go`

**参考 Python:** `openjiuwen/harness/rails/security/tool_security_rail.py` (671行)

- [ ] **Step 1: 创建 tool_security_rail.go**

`PermissionInterruptRail` 嵌入 `ConfirmInterruptRail`，重写 `ResolveInterruptFn` → `resolveInterrupt`。

字段：`engine *security.PermissionEngine`/`host *security.ToolPermissionHost`/`config security.PermissionsSection`/`toolNameAliases map[string]string`。

核心方法：
- `NewPermissionInterruptRail(engine, host, config)`
- `resolveInterrupt(ctx, cbc, toolCall, userInput, autoConfirmConfig) InterruptDecision`：按 spec 3.3 的 resolveInterrupt 逻辑实现
- `normalizeToolName`/`getAutoConfirmKey`/`buildShellAutoConfirmKey`/`shouldStoreAutoConfirm`/`persistAllowAlways`
- `updateConfig`/`buildMessage`（CN/EN 双语）/`buildAlwaysAllowHint`

- [ ] **Step 2: 创建 tool_security_rail_test.go**

测试覆盖（mock PermissionEngine + Host）：
- ALLOW → Approve
- DENY → Reject
- ASK + auto_confirm → Approve
- ASK + scene_hook approve → Approve（短路）
- ASK + scene_hook deny → Reject（短路）
- ASK + RequestPermissionConfirmation approved → Approve
- ASK + RequestPermissionConfirmation "interrupt" → Interrupt
- 中断恢复 approved + auto_confirm → persist + Approve
- 中断恢复 rejected → Reject

- [ ] **Step 3: 编译验证**

```bash
CGO_ENABLED=1 go build ./internal/agentcore/harness/rails/security/
```

- [ ] **Step 4: 运行测试**

```bash
CGO_ENABLED=1 go test -v -tags test ./internal/agentcore/harness/rails/security/
```

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/rails/security/tool_security_rail.go internal/agentcore/harness/rails/security/tool_security_rail_test.go
git commit -m "feat: 实现 PermissionInterruptRail

对齐 Python tool_security_rail.py (671行)
- 继承 ConfirmInterruptRail，重写 ResolveInterruptFn
- resolveInterrupt: scene_hook → engine.CheckPermission → auto_confirm → 托管确认 → Interrupt
- buildMessage CN/EN 双语权限请求消息
- persistAllowAlways: 始终允许规则持久化"
```

---

### Task 10: Factory + 回填

**Files:**
- Create: `internal/agentcore/harness/security/factory.go`
- Create: `internal/agentcore/harness/security/factory_test.go`
- Modify: `internal/agentcore/harness/deep_agent.go`
- Modify: `IMPLEMENTATION_PLAN.md`

**参考 Python:** `openjiuwen/harness/security/factory.py` (55行)

- [ ] **Step 1: 创建 factory.go**

```go
package security

// BuildPermissionInterruptRail 构建权限中断 Rail。
// 当 permissions.Enabled=false 时返回 nil。
//
// 对齐 Python: build_permission_interrupt_rail(permissions, llm, model_name, engine, host, workspace_root)
func BuildPermissionInterruptRail(
    permissions PermissionsSection,
    engine *PermissionEngine,
    host *ToolPermissionHost,
) *PermissionInterruptRail {
    if !permissions.Enabled {
        return nil
    }
    return NewPermissionInterruptRail(engine, host, permissions)
}
```

注意：此处 `PermissionInterruptRail` 在 `rails/security` 包中，factory 需要处理跨包引用。实际上 factory 函数应返回 `agentinterfaces.AgentRail` 或将 factory 移到 `rails/security` 包。根据 Python 的 `build_permission_interrupt_rail` 在 `harness/security/factory.py`，Go 侧放在 `security` 包中，返回类型用接口。

- [ ] **Step 2: 创建 factory_test.go**

测试覆盖：
- `permissions.Enabled=false` → 返回 nil
- `permissions.Enabled=true` → 返回非 nil

- [ ] **Step 3: 回填 deep_agent.go**

修改 `internal/agentcore/harness/deep_agent.go` 中的 PermissionInterruptRail 占位：

```go
if config.Permissions != nil {
    // ⤴️ 9.19-23 回填：BuildPermissionInterruptRail
    pRail := security.BuildPermissionInterruptRail(
        *config.Permissions,
        nil, // engine 由 Rail 内部创建
        nil, // host 由调用方注入
    )
    if pRail != nil {
        d.pendingRails = append(d.pendingRails, pRail)
        logger.Debug(logComponent).Msg("PermissionInterruptRail 已创建，⤴️ 9.19-23 回填")
    }
}
```

- [ ] **Step 4: 更新 IMPLEMENTATION_PLAN.md**

将 `9.19-23` 行中 `Security(☐)` 改为 `Security(✅)`。

- [ ] **Step 5: 全量编译验证**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' || true
CGO_ENABLED=1 go build ./...
```

- [ ] **Step 6: 全量测试验证**

```bash
CGO_ENABLED=1 go test -tags test ./internal/agentcore/harness/security/ ./internal/agentcore/harness/rails/security/
```

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/harness/security/factory.go internal/agentcore/harness/security/factory_test.go internal/agentcore/harness/deep_agent.go IMPLEMENTATION_PLAN.md
git commit -m "feat: 实现 BuildPermissionInterruptRail 工厂 + deep_agent.go 回填

对齐 Python factory.py
- BuildPermissionInterruptRail: permissions.Enabled=false 返回 nil
- deep_agent.go: PermissionInterruptRail 占位替换为真实创建
- IMPLEMENTATION_PLAN.md: Security(☐) → Security(✅)"
```

---

## 自审检查

**1. Spec 覆盖率：**
- ✅ BaseSecurityRail + 4种决策类型 → Task 2
- ✅ SafetyPromptRail → Task 3
- ✅ ShellAST → Task 4
- ✅ TieredPolicy → Task 5
- ✅ Patterns → Task 6
- ✅ Checker + Suggestions → Task 7
- ✅ ToolPermissionHost + PermissionEngine → Task 8
- ✅ PermissionInterruptRail → Task 9
- ✅ Factory + 回填 → Task 10
- ✅ tree-sitter 依赖 → Task 1

**2. Placeholder 扫描：** 无 TBD/TODO/"implement later"/"fill in details"/"Add appropriate error handling"

**3. 类型一致性：**
- `SecurityAllow`/`SecurityReject`/`SecurityInterrupt`/`SecurityAlert` → `SecurityDecision` 接口（Task 2 定义，Task 3/9 消费）
- `PermissionEngine.CheckPermission` → `*PermissionResult`（Task 8 定义，Task 9 消费）
- `ToolPermissionHost` → `security` 包（Task 8 定义，Task 9 消费）
- `ParseShellForPermission` → `*ShellAstParseResult`（Task 4 定义，Task 5 消费）
- `EvaluateTieredPolicy` → `(PermissionLevel, string)`（Task 5 定义，Task 8 消费）
