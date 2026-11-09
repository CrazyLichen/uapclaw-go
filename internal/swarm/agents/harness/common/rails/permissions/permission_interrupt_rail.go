// 带精确持久化与中断标记的 openjiuwen 权限 rail。
//
// 对应 Python 代码：
// jiuwenswarm/agents/harness/common/rails/permissions/permission_interrupt_rail.py（417 行）
//
// 职责：在父类 PermissionInterruptRail 之上加三件事：
//  1. 精确持久化：把「总是允许」交给宿主的 exact-persist 回调，并按审批快照
//     校验参数摘要，防止审批上下文漂移
//  2. 中断归属标记：把本 rail 发起的中断请求登记到 root 权限队列
//  3. 请求级权限覆盖（RUN_PERMISSIONS）：按本次请求的工具权限选择临时改写配置
//
// 语言差异（偏差登记见 spec §12.11）：
//   - Python 用 ContextVar 隐式传递 _EXACT_PERSIST_ATTEMPT / _PERMISSION_SNAPSHOT；
//     Go 用 rail 实例字段承载（父类内部 override 调用点无 ctx 形参，
//     无法从 ctx 读取）。单次 resolve_interrupt 内串行使用，语义等价。
//   - Python 在 before_tool_call 里 `except AbortError` 挂 bind_approval_request；
//     Go 的父类用 panic(cb.NewAbortError(...)) 抛中断，故改用 defer/recover。
//   - Python 的 super() 调用改由父类 override 钩子注入（见
//     tool_security_rail_override.go 的说明）。

package permissions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	secrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// ──────────────────────────── 结构体 ────────────────────────────

// exactPersistAttempt 一次「总是允许」持久化尝试的状态。
//
// Python: @dataclass class _ExactPersistAttempt
type exactPersistAttempt struct {
	// Attempted 是否已尝试持久化
	Attempted bool
	// Approved 是否已批准
	Approved bool
	// SessionOnly 是否仅会话内生效
	SessionOnly bool
	// Persisted 是否已落盘
	Persisted bool
	// Invalidated 是否已失效
	Invalidated bool
	// Accesses 需落盘的访问项（path, action）
	Accesses [][2]string
}

// JiuwenSwarmPermissionInterruptRail 持久化精确规则并标记归属中断的权限 rail。
//
// Python: class JiuwenSwarmPermissionInterruptRail(PermissionInterruptRail)
// Go 用嵌入 + override 注入复刻 Python 的继承 + 动态派发。
type JiuwenSwarmPermissionInterruptRail struct {
	// PermissionInterruptRail 嵌入父类
	*secrail.PermissionInterruptRail
	// exactPersistCallback 宿主精确持久化回调，nil 表示未启用
	exactPersistCallback ExactPermissionPersistCallback
	// currentAttempt 当前 resolve 的持久化尝试
	// （Python _EXACT_PERSIST_ATTEMPT ContextVar 的 Go 等价，见文件头偏差说明）
	currentAttempt *exactPersistAttempt
	// snapshotOwner 持有当前权限快照的 rail
	snapshotOwner *JiuwenSwarmPermissionInterruptRail
	// snapshot 当前权限快照（permission_check_context 期间有效）
	snapshot map[string]any
	// snapshotActive 是否已进入 permission_check_context
	// （Python 判据是 `current is not None`，与 snapshot 值是否为 nil 无关）
	snapshotActive bool
}

// ExactPermissionPersistCallback 宿主精确持久化回调。
//
// Python: ExactPermissionPersistCallback = Callable[..., bool]
// Go 用固定 5 参数（preValidated 表示调用方已校验过参数摘要）。
type ExactPermissionPersistCallback func(
	normalizedName string,
	toolArgs map[string]any,
	accesses [][2]string,
	sessionOnly bool,
	preValidated bool,
) bool

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// planHardRejectExtraKey plan 硬拒绝在回调上下文里的标记键
	// Python: "_jiuwenswarm_plan_hard_reject"
	planHardRejectExtraKey = "_jiuwenswarm_plan_hard_reject"
	// interruptDecisionExtraKey 中断决策在回调上下文里的键
	interruptDecisionExtraKey = "_interrupt_decision"
	// permissionArgumentsSHA256Key 审批参数摘要的 metadata 键
	permissionArgumentsSHA256Key = "permission_arguments_sha256"
	// permissionAskAccessesKey 审批访问项的 metadata 键
	permissionAskAccessesKey = "permission_ask_accesses"
	// permissionInterruptLogComponent 日志组件
	permissionInterruptLogComponent = logger.ComponentAgentCore
	// persistScopeSession 持久化范围：仅本会话
	persistScopeSession = "session"
	// persistScopePermanent 持久化范围：总是允许（永久）
	persistScopePermanent = "permanent"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var _ = llmschema.ToolCall{}

// ──────────────────────────── 导出函数 ────────────────────────────

// MarkPlanHardReject 标记该回调上下文已被 plan 硬拒绝。
//
// Python: mark_plan_hard_reject(ctx)
func MarkPlanHardReject(cbc *agentinterfaces.AgentCallbackContext) {
	if cbc == nil {
		return
	}
	cbc.SetExtra(planHardRejectExtraKey, true)
}

// PlanHardRejected 判定该回调上下文是否已被 plan 硬拒绝。
//
// Python: plan_hard_rejected(ctx)
func PlanHardRejected(ctx any) bool {
	cbc, ok := ctx.(*agentinterfaces.AgentCallbackContext)
	if !ok || cbc == nil {
		return false
	}
	raw, exists := cbc.GetExtra(planHardRejectExtraKey)
	if !exists {
		return false
	}
	marked, _ := raw.(bool)
	return marked
}

// NewJiuwenSwarmPermissionInterruptRail 构造 rail 并注入父类 override。
//
// Python: JiuwenSwarmPermissionInterruptRail.__init__(*args, exact_persist_callback=None, **kwargs)
func NewJiuwenSwarmPermissionInterruptRail(
	config map[string]any,
	engine *harnesssecurity.PermissionEngine,
	toolNames []string,
	llmModel any,
	modelName string,
	host *harnesssecurity.ToolPermissionHost,
	exactPersistCallback ExactPermissionPersistCallback,
) *JiuwenSwarmPermissionInterruptRail {
	parent := secrail.NewPermissionInterruptRail(config, engine, toolNames, nil, modelName, host)
	r := &JiuwenSwarmPermissionInterruptRail{
		PermissionInterruptRail: parent,
		exactPersistCallback:    exactPersistCallback,
	}
	r.installOverride()
	r.projectNativePathGuard()
	r.installSceneHookPreserver()
	return r
}

// InstalledPermissionConfig 返回已安装静态配置的深拷贝。
//
// Python: JiuwenSwarmPermissionInterruptRail.installed_permission_config()
func (r *JiuwenSwarmPermissionInterruptRail) InstalledPermissionConfig() map[string]any {
	return deepCopyMap(r.StaticConfig())
}

// GetPermissionSnapshot 通过执行 rail 的权限钩子返回会话快照。
//
// Python: JiuwenSwarmPermissionInterruptRail.get_permission_snapshot(session_id=None)
func (r *JiuwenSwarmPermissionInterruptRail) GetPermissionSnapshot(sessionID string) (map[string]any, error) {
	snapshot := r.invokePermissionsHook(r.hostSnapshotHook(), sessionID)
	if snapshot == nil {
		return nil, fmt.Errorf("Installed permission snapshot unavailable")
	}
	return snapshot, nil
}

// PermissionCheckContext 让本次调用的既有检查共享一次 provider 读取。
//
// Python: @contextmanager permission_check_context(session_id=None)
// Go 用「进入函数 + 返回 cleanup」等价 contextmanager。
func (r *JiuwenSwarmPermissionInterruptRail) PermissionCheckContext(sessionID string) func() {
	if r.snapshotOwner == r && r.snapshotActive {
		return func() {}
	}
	hook := r.hostSnapshotHook()
	var snapshot map[string]any
	if hook != nil {
		snapshot = r.invokePermissionsHook(hook, sessionID)
	}
	r.snapshotOwner = r
	r.snapshot = snapshot
	r.snapshotActive = true
	return func() {
		r.snapshotOwner = nil
		r.snapshot = nil
		r.snapshotActive = false
	}
}

// BindApprovalRequest 把审批范围冻结在既有待处理请求上。
//
// Python: JiuwenSwarmPermissionInterruptRail.bind_approval_request(request, tool_call)
func (r *JiuwenSwarmPermissionInterruptRail) BindApprovalRequest(
	request saschema.InterruptRequester,
	toolCall *llmschema.ToolCall,
) error {
	if r.exactPersistCallback == nil || toolCall == nil {
		return nil
	}
	name := r.NormalizeToolName(toolCall.Name)
	args := secrail.ParseToolArgs(toolCall)
	hook := r.hostSnapshotHook()
	var permissions map[string]any
	if hook != nil {
		permissions = r.invokePermissionsHook(hook, "")
	} else {
		permissions = r.Engine().Config()
	}
	if permissions == nil {
		return fmt.Errorf("permission_snapshot_unavailable")
	}
	accesses, err := r.collectFileGuardPersistAccesses(name, args, permissions)
	if err != nil {
		return err
	}
	metadata := map[string]any{}
	for k, v := range interruptRequestMetadata(request) {
		metadata[k] = v
	}
	metadata[permissionArgumentsSHA256Key] = ApprovalArgumentsDigest(args)
	metadata[permissionAskAccessesKey] = accesses
	setInterruptRequestMetadata(request, metadata)
	setInterruptRequestAutoConfirmKey(request, "")
	return nil
}

// ResolveInterrupt 覆写父类中断解析，叠加请求级权限策略。
//
// Python: JiuwenSwarmPermissionInterruptRail.resolve_interrupt(...)
// 8 处 RUN_PERMISSIONS 消费点已按 Go 的 runtime 包等价映射。
func (r *JiuwenSwarmPermissionInterruptRail) ResolveInterrupt(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	userInput any,
	autoConfirmConfig map[string]any,
) interrupt.InterruptDecision {
	levels := runtime.RunPermissionsFromCtx(ctx)
	runCtx := runtime.WithRunPermissions(ctx, levels)

	normalizedName := ""
	if toolCall != nil {
		normalizedName = r.NormalizeToolName(toolCall.Name)
	}
	level := ""
	if levels != nil {
		level = levels[normalizedName]
	}
	if level == "deny" {
		return r.Reject("[PERMISSION_DENIED] run policy")
	}

	var originalConfig map[string]any
	if levels != nil {
		originalConfig = r.InstalledPermissionConfig()
	}
	var originalHook harnesssecurity.PermissionSceneHookFn
	if levels != nil && level == "ask" {
		originalHook = r.Host().PermissionSceneHook
	}
	if originalConfig != nil {
		if overlaid, err := runtime.OverlayRunPermissions(runCtx, originalConfig); err == nil && overlaid != nil {
			r.UpdateConfig(overlaid, nil)
		}
	}
	if originalHook != nil {
		hook := originalHook
		r.Host().PermissionSceneHook = func(input harnesssecurity.PermissionSceneHookInput) ([]string, error) {
			outcome, err := hook(input)
			if err != nil {
				return nil, err
			}
			if len(outcome) > 0 && outcome[0] == "reject" {
				return outcome, nil
			}
			return nil, nil
		}
	}
	defer func() {
		if originalHook != nil {
			r.Host().PermissionSceneHook = originalHook
		}
		if originalConfig != nil {
			r.UpdateConfig(originalConfig, nil)
		}
	}()
	return r.resolveInterruptWithRunPolicy(runCtx, cbc, toolCall, userInput, autoConfirmConfig)
}

// UpdateConfig 覆写父类配置热更新：先同步引擎工作区，再刷新原生路径守卫投影。
//
// Python: JiuwenSwarmPermissionInterruptRail.update_config(*args, **kwargs)
func (r *JiuwenSwarmPermissionInterruptRail) UpdateConfig(config map[string]any, toolNames []string) {
	r.syncEngineWorkspaceRoot()
	r.PermissionInterruptRail.UpdateConfig(config, toolNames)
	r.projectNativePathGuard()
}

// SetTrustedDirs 覆写父类受信目录更新：转发后刷新原生路径守卫投影。
//
// Python: JiuwenSwarmPermissionInterruptRail.set_trusted_dirs(*args, **kwargs)
func (r *JiuwenSwarmPermissionInterruptRail) SetTrustedDirs(trustedDirs []string) {
	r.PermissionInterruptRail.SetTrustedDirs(trustedDirs)
	r.projectNativePathGuard()
}

// BeforeToolCall 覆写父类工具调用前钩子。
//
// Python: JiuwenSwarmPermissionInterruptRail.before_tool_call(ctx)
// plan 硬拒绝或非权限续接时直接放行；中断抛出时登记审批请求与中断归属。
func (r *JiuwenSwarmPermissionInterruptRail) BeforeToolCall(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
) error {
	if PlanHardRejected(cbc) || RootNonPermissionResumeFromContext(cbc) != nil {
		return nil
	}
	r.syncEngineWorkspaceRoot()
	defer func() {
		if p := recover(); p != nil {
			r.bindApprovalOnInterrupt(cbc, p)
			panic(p)
		}
	}()
	return r.PermissionInterruptRail.BeforeToolCall(ctx, cbc)
}

// ApprovalArgumentsDigest 计算审批参数摘要（sha256 of 规范化 JSON）。
//
// Python: JiuwenSwarmPermissionInterruptRail._approval_arguments_digest(args)
func ApprovalArgumentsDigest(args map[string]any) string {
	encoded, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// installOverride 把子类覆写注入父类派发钩子。
//
// 每个覆写在 exactPersistCallback 为 nil 时返回 handled=false，回落父类实现
// （对齐 Python 的 `if callback is None: return super().xxx()`）。
func (r *JiuwenSwarmPermissionInterruptRail) installOverride() {
	r.SetOverride(&secrail.PermissionInterruptRailOverride{
		PersistAllowAlways: func(ctx context.Context, normalizedName string, toolArgs map[string]any) (bool, bool) {
			if r.exactPersistCallback == nil {
				return false, false
			}
			return r.persistAllowAlways(ctx, normalizedName, toolArgs), true
		},
		PersistSessionAllow: func(
			ctx context.Context, normalizedName string, toolArgs map[string]any, sessionID string,
		) (bool, bool) {
			if r.exactPersistCallback == nil {
				return false, false
			}
			return r.persistAllowAlways(ctx, normalizedName, toolArgs), true
		},
		PersistMergedAllow: func(
			ctx context.Context, normalizedName string, toolArgs map[string]any,
			persistHook func(map[string]any) bool, writeYAMLFallback bool,
		) (bool, bool) {
			// 子类不覆写 merged：run 策略下拒绝，其余回落父类默认
			if runtime.RunPermissionsFromCtx(ctx) != nil {
				return false, true
			}
			return false, false
		},
		CollectFileGuardPersistAccesses: func(
			normalizedName string, toolArgs map[string]any, permissionsCfg map[string]any,
		) ([]harnesssecurity.PathAction, bool) {
			if r.exactPersistCallback == nil {
				return nil, false
			}
			accesses, err := r.collectFileGuardPersistAccesses(normalizedName, toolArgs, permissionsCfg)
			if err != nil {
				return nil, false
			}
			return accesses, true
		},
		GetUserInput: func(cbc *agentinterfaces.AgentCallbackContext, toolCallID string) (any, bool) {
			resume := RootPermissionResumeFromContext(cbc)
			if resume != nil && resume.Card.Key.ToolCallID == toolCallID {
				return resume.UserInput, true
			}
			return nil, false
		},
	})
}

// installSceneHookPreserver 在既有场景钩子外再包一层，保留审批确认语义。
//
// Python: __init__ 里的 preserve_confirmation 闭包
func (r *JiuwenSwarmPermissionInterruptRail) installSceneHookPreserver() {
	sceneHook := r.Host().PermissionSceneHook
	if r.exactPersistCallback == nil || sceneHook == nil {
		return
	}
	original := sceneHook
	r.Host().PermissionSceneHook = func(input harnesssecurity.PermissionSceneHookInput) ([]string, error) {
		outcome, err := original(input)
		if err != nil {
			return nil, err
		}
		attempt := r.currentAttempt
		if attempt == nil || !attempt.Approved {
			return outcome, nil
		}
		if len(outcome) > 0 && outcome[0] == "approve" {
			return nil, nil
		}
		return outcome, nil
	}
}

// projectNativePathGuard 把既有 checker 包进 NativePathGuardProjection。
//
// Python: JiuwenSwarmPermissionInterruptRail._project_native_path_guard()
func (r *JiuwenSwarmPermissionInterruptRail) projectNativePathGuard() {
	if r.exactPersistCallback == nil {
		return
	}
	engine := r.Engine()
	if engine == nil {
		return
	}
	checker := engine.FileGuard()
	if checker == nil {
		return
	}
	if _, already := checker.(*NativePathGuardProjection); already {
		return
	}
	engine.SetFileGuard(NewNativePathGuardProjection(checker))
}

// syncEngineWorkspaceRoot 在宿主工作区解析器变化时刷新 file_guard 工作区。
//
// Python: JiuwenSwarmPermissionInterruptRail._sync_engine_workspace_root()
func (r *JiuwenSwarmPermissionInterruptRail) syncEngineWorkspaceRoot() {
	host := r.Host()
	if host == nil || host.ResolveWorkspaceDir == nil {
		return
	}
	workspace := host.ResolveWorkspaceDir()
	if workspace == "" {
		return
	}
	normalized, err := resolveWorkspaceForSync(workspace)
	if err != nil {
		logger.Debug(permissionInterruptLogComponent).
			Err(err).
			Msg("permission.rail.workspace_resolve_failed")
		return
	}
	engine := r.Engine()
	if engine == nil {
		return
	}
	if current := engine.WorkspaceRoot(); current != "" {
		if existing, err := resolveWorkspaceForSync(current); err == nil && existing == normalized {
			return
		}
	}
	engine.SetWorkspaceRoot(normalized)
	engine.RebuildFileGuard()
	r.projectNativePathGuard()
}

// collectFileGuardPersistAccesses 收集需持久化的路径访问项。
//
// Python: JiuwenSwarmPermissionInterruptRail._collect_file_guard_persist_accesses(...)
// send_file_to_user 走 SendFilePathGuardEvaluator；其余走原生路径投影或父类默认。
func (r *JiuwenSwarmPermissionInterruptRail) collectFileGuardPersistAccesses(
	normalizedName string,
	toolArgs map[string]any,
	permissionsCfg map[string]any,
) ([]harnesssecurity.PathAction, error) {
	if r.exactPersistCallback != nil && normalizedName == SendFileToolName {
		var workspaceRoot any
		if host := r.Host(); host != nil && host.ResolveWorkspaceDir != nil {
			workspaceRoot = host.ResolveWorkspaceDir()
		}
		result := NewSendFilePathGuardEvaluator().Evaluate(
			permissionsCfg, workspaceRoot, []string{}, toolArgs["abs_file_path_list"],
		)
		if result.Status == SendFileGuardStatusUnevaluable || result.Level == "deny" {
			return nil, fmt.Errorf("%s", result.Reason)
		}
		accesses := make([]harnesssecurity.PathAction, 0, len(result.AskAccesses))
		for _, item := range result.AskAccesses {
			accesses = append(accesses, harnesssecurity.PathAction{Path: item.Path, Action: item.Action})
		}
		return accesses, nil
	}
	access := CurrentNativePathAccess(normalizedName, toolArgs)
	if r.exactPersistCallback != nil && access != nil {
		return r.PermissionInterruptRail.CollectFileGuardPersistAccessesDefault(
			access.GuardTool(), access.GuardArgs(), permissionsCfg,
		), nil
	}
	return r.PermissionInterruptRail.CollectFileGuardPersistAccessesDefault(
		normalizedName, toolArgs, permissionsCfg,
	), nil
}

// invokePermissionsHook 调用权限快照钩子并叠加请求级覆盖。
//
// Python: JiuwenSwarmPermissionInterruptRail._invoke_permissions_hook(...)
func (r *JiuwenSwarmPermissionInterruptRail) invokePermissionsHook(
	hook func() map[string]any,
	sessionID string,
) map[string]any {
	_ = sessionID
	if r.snapshotOwner == r && r.snapshotActive {
		return deepCopyMap(r.snapshot)
	}
	var snapshot map[string]any
	if hook != nil {
		snapshot = hook()
	}
	if r.exactPersistCallback == nil {
		if snapshot != nil {
			overlaid, err := runtime.OverlayRunPermissions(context.Background(), snapshot)
			if err == nil {
				return overlaid
			}
		}
		return snapshot
	}
	if snapshot == nil {
		return nil
	}
	overlaid, err := runtime.OverlayRunPermissions(context.Background(), snapshot)
	if err != nil {
		return nil
	}
	return overlaid
}

// resolveInterruptWithRunPolicy 在请求级策略下解析中断。
//
// Python: JiuwenSwarmPermissionInterruptRail._resolve_interrupt_with_run_policy(...)
func (r *JiuwenSwarmPermissionInterruptRail) resolveInterruptWithRunPolicy(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	userInput any,
	autoConfirmConfig map[string]any,
) interrupt.InterruptDecision {
	if r.exactPersistCallback == nil {
		return r.PermissionInterruptRail.ResolvePermissionInterrupt(
			ctx, cbc, toolCall, userInput, autoConfirmConfig)
	}
	if hook := r.hostSnapshotHook(); hook != nil && r.invokePermissionsHook(hook, "") == nil {
		return r.Reject("[PERMISSION_DENIED] permission_snapshot_unavailable")
	}
	if r.hostSnapshotHook() == nil && toolCall != nil &&
		CurrentNativePathAccess(toolCall.Name, secrail.ParseToolArgs(toolCall)) != nil {
		return r.Reject("[PERMISSION_DENIED] native_path_snapshot_unavailable")
	}

	payload := secrail.ParseConfirmPayload(userInput)
	attempt := &exactPersistAttempt{}
	previousAttempt := r.currentAttempt
	r.currentAttempt = attempt
	defer func() { r.currentAttempt = previousAttempt }()

	if payload != nil && payload.Approved {
		if payload.AutoConfirm && runtime.RunPermissionsFromCtx(ctx) != nil {
			return r.Reject("[PERMISSION_DENIED] run_policy_remember_unsupported")
		}
		request := approvalRequestFromContext(cbc)
		metadata := interruptRequestMetadata(request)
		args := secrail.ParseToolArgs(toolCall)
		if metadataString(metadata, permissionArgumentsSHA256Key) != ApprovalArgumentsDigest(args) {
			r.removeExactAutoConfirm(cbc, toolCall)
			return r.Reject("[PERMISSION_DENIED] approval_context_changed")
		}
		attempt.Accesses = metadataAccessPairs(metadata)
		attempt.Approved = true
		attempt.SessionOnly = payloadApprovedSessionOnly(payload)
		if !r.callExactPersist(toolCall, args, attempt, true) {
			r.removeExactAutoConfirm(cbc, toolCall)
			return r.Reject("[PERMISSION_DENIED] approval_invalidated")
		}
	}
	decision := r.PermissionInterruptRail.ResolvePermissionInterrupt(
		ctx, cbc, toolCall, userInput, autoConfirmConfig)
	if attempt.Invalidated {
		r.removeExactAutoConfirm(cbc, toolCall)
		return r.Reject("[PERMISSION_DENIED] approval_invalidated")
	}
	if payload != nil && payloadWantsPermanentPersist(payload) && attempt.Persisted {
		r.removeExactAutoConfirm(cbc, toolCall)
	}
	return decision
}

// persistAllowAlways 执行精确「总是允许」持久化。
//
// Python: JiuwenSwarmPermissionInterruptRail._persist_allow_always(...)
func (r *JiuwenSwarmPermissionInterruptRail) persistAllowAlways(
	ctx context.Context,
	normalizedName string,
	toolArgs map[string]any,
) bool {
	// Python: if RUN_PERMISSIONS.get() is not None: return False
	if runtime.RunPermissionsFromCtx(ctx) != nil {
		return false
	}
	attempt := r.currentAttempt
	if attempt == nil {
		return false
	}
	attempt.Attempted = true
	if r.exactPersistCallback == nil {
		return false
	}
	persisted := r.exactPersistCallback(
		normalizedName, copyMap(toolArgs), attempt.Accesses, attempt.SessionOnly, false)
	attempt.Persisted = persisted
	return persisted
}

// callExactPersist 调宿主精确持久化回调并把异常转为 invalidated。
func (r *JiuwenSwarmPermissionInterruptRail) callExactPersist(
	toolCall *llmschema.ToolCall,
	args map[string]any,
	attempt *exactPersistAttempt,
	preValidated bool,
) (ok bool) {
	defer func() {
		if p := recover(); p != nil {
			attempt.Invalidated = true
			ok = false
		}
	}()
	name := ""
	if toolCall != nil {
		name = r.NormalizeToolName(toolCall.Name)
	}
	return r.exactPersistCallback(name, args, attempt.Accesses, attempt.SessionOnly, preValidated)
}

// removeExactAutoConfirm 从会话状态移除本次调用的自动确认条目。
//
// Python: JiuwenSwarmPermissionInterruptRail._remove_exact_auto_confirm(ctx, tool_call)
func (r *JiuwenSwarmPermissionInterruptRail) removeExactAutoConfirm(
	cbc *agentinterfaces.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
) {
	if cbc == nil {
		return
	}
	sess := cbc.Session()
	key := r.AutoConfirmKey(toolCall)
	if sess == nil || key == "" {
		return
	}
	raw, err := sess.GetState(sessionstate.StringKey(saschema.InterruptAutoConfirmKey))
	if err != nil || raw == nil {
		return
	}
	config, ok := raw.(map[string]any)
	if !ok {
		return
	}
	if _, exists := config[key]; !exists {
		return
	}
	updated := make(map[string]any, len(config))
	for k, v := range config {
		if k == key {
			continue
		}
		updated[k] = v
	}
	sess.UpdateState(map[string]any{
		saschema.InterruptAutoConfirmKey: updated,
	})
}

// bindApprovalOnInterrupt 在中断抛出时登记审批请求与中断归属。
//
// Python: before_tool_call 里的 except AbortError 分支
func (r *JiuwenSwarmPermissionInterruptRail) bindApprovalOnInterrupt(
	cbc *agentinterfaces.AgentCallbackContext,
	recovered any,
) {
	if cbc == nil {
		return
	}
	abortErr, ok := recovered.(*cb.AbortError)
	if !ok || abortErr == nil {
		return
	}
	interruptErr, ok := abortErr.Cause.(*saschema.ToolInterruptException)
	if !ok || interruptErr == nil || interruptErr.Request == nil {
		return
	}
	var toolCall *llmschema.ToolCall
	if inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs); ok {
		toolCall = inputs.ToolCall
	}
	if err := r.BindApprovalRequest(interruptErr.Request, toolCall); err != nil {
		logger.Warn(permissionInterruptLogComponent).
			Err(err).
			Msg("permission.rail.bind_approval_failed")
	}
	if concrete, ok := interruptErr.Request.(*saschema.InterruptRequest); ok {
		if err := MarkPermissionInterruptRequest(cbc, concrete); err != nil {
			logger.Warn(permissionInterruptLogComponent).
				Err(err).
				Msg("permission.rail.mark_interrupt_failed")
		}
	}
}

// hostSnapshotHook 返回宿主权限快照钩子。
func (r *JiuwenSwarmPermissionInterruptRail) hostSnapshotHook() func() map[string]any {
	host := r.Host()
	if host == nil {
		return nil
	}
	return host.GetPermissionsSnapshot
}

// ──────────────────────────── 辅助 ────────────────────────────

// deepCopyMap 深拷贝 map（Python deepcopy）。
func deepCopyMap(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = deepCopyValue(v)
	}
	return out
}

// deepCopyValue 递归深拷贝任意值。
func deepCopyValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return deepCopyMap(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = deepCopyValue(item)
		}
		return out
	default:
		return value
	}
}

// interruptRequestMetadata 读取中断请求的 metadata。
func interruptRequestMetadata(request saschema.InterruptRequester) map[string]any {
	if request == nil {
		return nil
	}
	return request.GetMetadata()
}

// setInterruptRequestMetadata 覆盖中断请求的 metadata。
func setInterruptRequestMetadata(request saschema.InterruptRequester, metadata map[string]any) {
	if request == nil {
		return
	}
	request.SetMetadata(metadata)
}

// setInterruptRequestAutoConfirmKey 清空中断请求的自动确认键。
//
// Python: request.auto_confirm_key = ""
// 走 InterruptRequester.SetAutoConfirmKey 接口方法（9.90d-1 补齐），
// 不再依赖类型断言到具体结构体。
func setInterruptRequestAutoConfirmKey(request saschema.InterruptRequester, key string) {
	if request == nil {
		return
	}
	request.SetAutoConfirmKey(key)
}

// metadataString 从 metadata 取字符串值。
func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	raw, exists := metadata[key]
	if !exists || raw == nil {
		return ""
	}
	text, _ := raw.(string)
	return text
}

// metadataAccessPairs 从 metadata 取访问项。
//
// Python: tuple(tuple(item) for item in metadata.get("permission_ask_accesses", ()))
func metadataAccessPairs(metadata map[string]any) [][2]string {
	if metadata == nil {
		return nil
	}
	raw, exists := metadata[permissionAskAccessesKey]
	if !exists || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case [][2]string:
		out := make([][2]string, len(v))
		copy(out, v)
		return out
	case []any:
		out := make([][2]string, 0, len(v))
		for _, item := range v {
			pair, ok := item.([2]string)
			if ok {
				out = append(out, pair)
				continue
			}
			if nested, ok := item.([]any); ok && len(nested) == 2 {
				path, _ := nested[0].(string)
				action, _ := nested[1].(string)
				out = append(out, [2]string{path, action})
			}
		}
		return out
	default:
		return nil
	}
}

// payloadApprovedSessionOnly 判定载荷是否要求「仅会话内记住」。
//
// Python: payload.wants_session_persist()
// 优先读 PersistScope（"session"）；为空时回退到 AutoConfirm（兼容旧载荷）。
func payloadApprovedSessionOnly(payload *harnesssecurity.PermissionConfirmResponse) bool {
	if payload == nil {
		return false
	}
	switch payload.PersistScope {
	case persistScopeSession:
		return true
	case persistScopePermanent:
		return false
	default:
		return payload.AutoConfirm
	}
}

// payloadWantsPermanentPersist 判定载荷是否要求「永久记住」。
//
// Python: payload.wants_permanent_persist()
// 优先读 PersistScope（"permanent"）；为空时回退到 AutoConfirm（兼容旧载荷）。
func payloadWantsPermanentPersist(payload *harnesssecurity.PermissionConfirmResponse) bool {
	if payload == nil {
		return false
	}
	switch payload.PersistScope {
	case persistScopePermanent:
		return true
	case persistScopeSession:
		return false
	default:
		return payload.AutoConfirm
	}
}

// approvalRequestFromContext 从回调上下文取待审批请求。
//
// Python: resume.card.request if resume else getattr(ctx.extra["_interrupt_decision"], "request", None)
func approvalRequestFromContext(cbc *agentinterfaces.AgentCallbackContext) saschema.InterruptRequester {
	if cbc == nil {
		return nil
	}
	raw, exists := cbc.GetExtra(interruptDecisionExtraKey)
	if !exists || raw == nil {
		return nil
	}
	if result, ok := raw.(*interrupt.InterruptResult); ok && result != nil {
		return result.Request
	}
	return nil
}

// resolveWorkspaceForSync 规范化工作区路径（Path.expanduser().resolve(strict=False)）。
func resolveWorkspaceForSync(value string) (string, error) {
	expanded := expandHomeString(strings.TrimSpace(value))
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Clean(abs)), nil
}
