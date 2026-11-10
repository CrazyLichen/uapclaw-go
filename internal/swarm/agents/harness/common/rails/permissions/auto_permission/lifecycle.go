// AutoPermissionRail 的生命周期：构造、初始化、配置更新。
//
// 对应 Python 代码：_auto_permission/lifecycle.py（165 行 / 1 mixin / 11 方法）
// Python: class AutoPermissionLifecycleMixin
//
// 方案 C（spec §3.3）：Go 侧用**单结构体** AutoPermissionRail 承载 Python 12 个
// mixin 共享的 self，方法按 Python 文件分文件挂载，每个方法块标注所属 mixin。
//
// 本文件定义结构体本身 + 构造 + 生命周期钩子，是后续 11 个文件的基础。

package auto_permission

import (
	"context"
	"errors"
	"sort"
	"strings"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AutoPermissionRail 自动权限审查层（单结构体承载 12 个 mixin 的共享状态）。
//
// 嵌入 *agentinterfaces.BaseRail 提供 14 个生命周期钩子的默认 no-op，
// 本类型只覆盖真正需要的 4 个（Init / Uninit / BeforeToolCall / AfterToolCall）。
//
// Python: AutoPermissionRail（12 个 mixin 的 MRO 组合，见 auto_permission_rail.py:52）
type AutoPermissionRail struct {
	*agentinterfaces.BaseRail

	// baseRail 被包装的权限轨（9.90d-1 的 JiuwenSwarmPermissionInterruptRail）
	baseRail permissions.BasePermissionRail
	// permissionConfig 当前生效的权限配置（深拷贝）
	permissionConfig map[string]any
	// workspaceRoot 工作区根（已 resolve）
	workspaceRoot string
	// platformTrustedRoot 平台受信根（已 resolve）
	platformTrustedRoot string
	// sandbox 沙箱描述符
	sandbox *permissions.SandboxDescriptor
	// sysOperation 系统操作接口
	sysOperation any
	// policyEvaluator 策略评估器
	policyEvaluator *permissions.OpenJiuwenPolicyEvaluator
	// autoOptions 归一后的 auto 段选项
	autoOptions map[string]any
	// autoReviewer LLM 审查器
	autoReviewer *permissions.AutoReviewer
	// trustedSearchUrls 会话可信搜索 URL 台账
	trustedSearchUrls *permissions.SessionTrustedSearchUrls
	// persistentAuditWriter 持久化审计写入器
	persistentAuditWriter any
	// browserRuntimeSecurityProfile 浏览器运行时安全档位
	browserRuntimeSecurityProfile any
	// ownerAgent 当前归属的 Agent（Init 后非 nil）
	ownerAgent agentinterfaces.BaseAgent
	// writtenPermissions 宿主已写权限的读取回调
	writtenPermissions func() map[string]any
}

// AutoPermissionRailOptions 构造参数。
//
// Python 的 __init__ 全是关键字参数（12 个），Go 用 options 结构体表达，
// 避免长位置参数列表（登记为偏差：参数名与 Python 一一对应，顺序无意义）。
type AutoPermissionRailOptions struct {
	// BaseRail 被包装的权限轨（必填）
	BaseRail permissions.BasePermissionRail
	// PermissionConfig 权限配置（必填，内部深拷贝）
	PermissionConfig map[string]any
	// WorkspaceRoot 工作区根（空串表示未声明）
	WorkspaceRoot string
	// PlatformTrustedRoot 平台受信根
	PlatformTrustedRoot string
	// Sandbox 沙箱描述符
	Sandbox *permissions.SandboxDescriptor
	// SysOperation 系统操作接口
	SysOperation any
	// PolicyEvaluator 策略评估器（nil 时自动构造）
	PolicyEvaluator *permissions.OpenJiuwenPolicyEvaluator
	// AutoReviewer LLM 审查器
	AutoReviewer *permissions.AutoReviewer
	// TrustedSearchUrls 会话可信搜索 URL 台账
	TrustedSearchUrls *permissions.SessionTrustedSearchUrls
	// PersistentAuditWriter 持久化审计写入器
	PersistentAuditWriter any
	// BrowserRuntimeSecurityProfile 浏览器运行时安全档位
	BrowserRuntimeSecurityProfile any
	// WrittenPermissions 宿主已写权限的读取回调
	WrittenPermissions func() map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultAutoPermissionPriority 未从 baseRail 取到优先级时的默认值
	//
	// Python: int(getattr(base_rail, "priority", 90))
	defaultAutoPermissionPriority = 90
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAutoPermissionRail 构造自动权限审查层。
//
// Python: AutoPermissionLifecycleMixin.__init__(*, base_rail, permission_config,
// workspace_root, platform_trusted_root=None, sandbox=None, sys_operation=None,
// policy_evaluator=None, auto_reviewer=None, trusted_search_urls=None,
// persistent_audit_writer=None, browser_runtime_security_profile=None,
// written_permissions=None)
func NewAutoPermissionRail(opts AutoPermissionRailOptions) (*AutoPermissionRail, error) {
	if opts.BaseRail == nil {
		return nil, errors.New("auto_permission_base_rail_required")
	}

	workspaceRoot, err := resolveRoot(opts.WorkspaceRoot)
	if err != nil {
		return nil, err
	}
	platformRoot, err := resolveRoot(opts.PlatformTrustedRoot)
	if err != nil {
		return nil, err
	}

	priority := defaultAutoPermissionPriority
	if opts.BaseRail != nil {
		// Python 从 base_rail 读 priority 属性；Go 侧 BasePermissionRail 无该访问器，
		// 用 agentinterfaces.AgentRail 断言（被包装的权限轨同时实现了 AgentRail）
		if rail, ok := opts.BaseRail.(interface{ Priority() int }); ok {
			priority = rail.Priority()
		}
	}

	permissionConfig := permissions.DeepCopyPermissionConfig(opts.PermissionConfig)
	autoOptions := permissions.NormalizeAutoPermissionOptions(permissionConfig["auto"])

	policyEvaluator := opts.PolicyEvaluator
	if policyEvaluator == nil {
		policyEvaluator = permissions.NewOpenJiuwenPolicyEvaluator(
			newPolicySurfaceAdapter(opts.BaseRail),
			func() map[string]any { return permissionConfig },
			workspaceRoot,
		)
	}

	return &AutoPermissionRail{
		BaseRail:                      agentinterfaces.NewBaseRail().WithPriority(priority),
		baseRail:                      opts.BaseRail,
		permissionConfig:              permissionConfig,
		workspaceRoot:                 workspaceRoot,
		platformTrustedRoot:           platformRoot,
		sandbox:                       opts.Sandbox,
		sysOperation:                  opts.SysOperation,
		policyEvaluator:               policyEvaluator,
		autoOptions:                   autoOptions,
		autoReviewer:                  opts.AutoReviewer,
		trustedSearchUrls:             opts.TrustedSearchUrls,
		persistentAuditWriter:         opts.PersistentAuditWriter,
		browserRuntimeSecurityProfile: opts.BrowserRuntimeSecurityProfile,
		writtenPermissions:            opts.WrittenPermissions,
	}, nil
}

// Init 把初始化委托给被包装的权限轨。
//
// Python: AutoPermissionLifecycleMixin.init(agent)
// 顺序对齐：先清 owner → 转发 → 再置 owner（防止 init 内部读到半初始化状态）。
func (r *AutoPermissionRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error {
	r.ownerAgent = nil
	if initializer, ok := r.baseRail.(interface {
		Init(context.Context, agentinterfaces.BaseAgent) error
	}); ok {
		if err := initializer.Init(ctx, agent); err != nil {
			return err
		}
	}
	r.ownerAgent = agent
	return nil
}

// Uninit 把注销委托给被包装的权限轨。
//
// Python: AutoPermissionLifecycleMixin.uninit(agent)
// 无论转发是否失败，owner 都必须清空（Python 用 try/finally）。
func (r *AutoPermissionRail) Uninit(agent agentinterfaces.BaseAgent) error {
	var err error
	if uninitializer, ok := r.baseRail.(interface {
		Uninit(agentinterfaces.BaseAgent) error
	}); ok {
		err = uninitializer.Uninit(agent)
	}
	r.ownerAgent = nil
	return err
}

// GetTools 返回被包装权限轨声明的工具标签集合。
//
// Python: AutoPermissionLifecycleMixin.get_tools()
// AgentRail 接口无此方法，属本类型额外能力（Python 侧 rail 基类提供）。
func (r *AutoPermissionRail) GetTools() map[string]struct{} {
	if provider, ok := r.baseRail.(interface{ GetTools() []string }); ok {
		out := map[string]struct{}{}
		for _, name := range provider.GetTools() {
			out[name] = struct{}{}
		}
		return out
	}
	return map[string]struct{}{}
}

// InstalledPermissionConfig 返回本运行期安装的完整策略快照（深拷贝）。
//
// Python: AutoPermissionLifecycleMixin.installed_permission_config()
func (r *AutoPermissionRail) InstalledPermissionConfig() map[string]any {
	return permissions.DeepCopyPermissionConfig(r.permissionConfig)
}

// SetTrustedDirs 替换请求级受信目录，同时保留平台受信根。
//
// Python: AutoPermissionLifecycleMixin.set_trusted_dirs(trusted_dirs)
// 未声明平台根时直接转发；声明了则把平台根置于首位并去重。
func (r *AutoPermissionRail) SetTrustedDirs(trustedDirs []string) {
	if r.platformTrustedRoot == "" {
		r.baseRail.SetTrustedDirs(trustedDirs)
		return
	}
	roots := make([]string, 0, len(trustedDirs)+1)
	roots = append(roots, r.platformTrustedRoot)
	for _, raw := range trustedDirs {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		resolved, err := resolveRoot(raw)
		if err != nil {
			continue
		}
		roots = append(roots, resolved)
	}
	// 保序去重（Python 用 dict.fromkeys）
	seen := make(map[string]struct{}, len(roots))
	deduped := make([]string, 0, len(roots))
	for _, root := range roots {
		if _, exists := seen[root]; exists {
			continue
		}
		seen[root] = struct{}{}
		deduped = append(deduped, root)
	}
	r.baseRail.SetTrustedDirs(deduped)
}

// ReplaceInstalledGlobalPermissions 转发宿主的 Global 层 setter。
//
// Python: AutoPermissionLifecycleMixin.replace_installed_global_permissions(config)
// 本类型不自己持有第二份 Global 层；基轨无该能力时返回 nil。
func (r *AutoPermissionRail) ReplaceInstalledGlobalPermissions(config map[string]any) (map[string]any, error) {
	if replacer, ok := r.baseRail.(interface {
		ReplaceInstalledGlobalPermissions(map[string]any) (map[string]any, error)
	}); ok {
		return replacer.ReplaceInstalledGlobalPermissions(config)
	}
	return nil, nil
}

// UpdateConfig 通过标准基轨应用生效权限，并同步审查器与审计写入器。
//
// Python: AutoPermissionLifecycleMixin.update_config(config)
// 顺序对齐：先归一 auto 段 → 决定审计写入器 → 转发基轨 → 更新审查器 →
// 最后写回本结构体字段（避免中途失败留下半新半旧状态）。
func (r *AutoPermissionRail) UpdateConfig(config map[string]any, toolNames []string) {
	permissionConfig := permissions.DeepCopyPermissionConfig(config)
	autoOptions := permissions.NormalizeAutoPermissionOptions(permissionConfig["auto"])
	timeoutMs := permissions.AutoOptionTimeoutMs(autoOptions)

	var auditWriter any
	if enabled, _ := autoOptions["persistent_audit_enabled"].(bool); enabled {
		auditRoot := permissions.ResolvePersistentAuditRoot(config)
		if current, ok := r.persistentAuditWriter.(*permissions.PersistentAuditWriter); ok &&
			current != nil && sameAuditRoot(current.DataRoot(), auditRoot) {
			auditWriter = current
		} else {
			auditWriter = permissions.NewPersistentAuditWriter(auditRoot)
		}
	}

	r.baseRail.UpdateConfig(permissionConfig, toolNames)

	if r.autoReviewer != nil {
		outputFormat := permissions.AutoOptionOutputFormat(autoOptions)
		r.autoReviewer.UpdateRuntimeOptions(timeoutMs, &outputFormat)
	}
	r.permissionConfig = permissionConfig
	r.autoOptions = autoOptions
	r.persistentAuditWriter = auditWriter
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// recentURLSources 返回当前 root 会话的可信搜索来源。
//
// Python: AutoPermissionLifecycleMixin._recent_url_sources(session_id)
func (r *AutoPermissionRail) recentURLSources(sessionID string) []permissions.RecentURLSource {
	if r.trustedSearchUrls == nil || sessionID == "" {
		return nil
	}
	return r.trustedSearchUrls.Sources(sessionID)
}

// resolveRoot 归一路径根（Path(x).resolve(strict=False) 等价）。
//
// 空串表示未声明，返回空串而非错误。
func resolveRoot(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	return permissions.ResolvePathStrictFalse(raw)
}

// sameAuditRoot 两个审计根是否等价（含 nil 语义）。
func sameAuditRoot(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// sortedEffectNames 把副作用集合转为有序切片（供摘要输出）。
func sortedEffectNames(effects map[string]struct{}) []string {
	out := make([]string, 0, len(effects))
	for name := range effects {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// policySurfaceAdapter 把 BasePermissionRail 适配成 PolicyBaseRail。
//
// 为什么需要适配器：Python 的 policy_eval.py 对 base_rail 全部用
// `getattr(self._base_rail, "_xxx", None)` 容忍能力缺失（缺失时用默认值），
// 是鸭子类型；而 Go 的 PolicyBaseRail（9.90b 落地）是强类型接口，要求 9 个
// 方法全部实现。BasePermissionRail（9.90d-1）只有 7 个方法，缺其中的
// 6 个能力（Engine / StaticConfig / ParseToolArgs / PermissionSceneHook /
// PermissionsSnapshot / HasPermissionsSnapshot / TrustedDirs），
// 故仍需适配器补齐；UpdateConfig 的签名冲突已通过改名 UpdatePolicyConfig 消除。
//
// 本适配器用类型断言复刻 Python 的鸭子类型语义：能力存在则转发，
// 缺失则返回 Python 的默认值。这样既不破坏两个既有接口，
// 又保持与上游一致的行为。
type policySurfaceAdapter struct {
	// base 被适配的权限轨
	base permissions.BasePermissionRail
}

// newPolicySurfaceAdapter 构造适配器。
func newPolicySurfaceAdapter(base permissions.BasePermissionRail) permissions.PolicyBaseRail {
	return &policySurfaceAdapter{base: base}
}

// NormalizeToolName 转发工具名归一，缺失时返回原名（Python 默认）。
func (a *policySurfaceAdapter) NormalizeToolName(toolName string) string {
	if provider, ok := a.base.(interface{ NormalizeToolName(string) string }); ok {
		return provider.NormalizeToolName(toolName)
	}
	return toolName
}

// ParseToolArgs 转发参数解析，缺失时返回 nil（Python 用 invocation.tool_args 兜底）。
func (a *policySurfaceAdapter) ParseToolArgs(toolCall any) map[string]any {
	if provider, ok := a.base.(interface{ ParseToolArgs(any) map[string]any }); ok {
		return provider.ParseToolArgs(toolCall)
	}
	return nil
}

// Engine 转发权限引擎，缺失时返回 nil。
func (a *policySurfaceAdapter) Engine() permissions.PolicyEngine {
	if provider, ok := a.base.(interface {
		Engine() permissions.PolicyEngine
	}); ok {
		return provider.Engine()
	}
	return nil
}

// StaticConfig 转发静态配置，缺失时返回 nil。
func (a *policySurfaceAdapter) StaticConfig() map[string]any {
	if provider, ok := a.base.(interface{ StaticConfig() map[string]any }); ok {
		return provider.StaticConfig()
	}
	return nil
}

// UpdatePolicyConfig 转发配置热更新（补 nil toolNames 以匹配 BasePermissionRail 签名）。
func (a *policySurfaceAdapter) UpdatePolicyConfig(config map[string]any) {
	a.base.UpdateConfig(config, nil)
}

// PermissionSceneHook 转发场景钩子，缺失时返回 nil。
func (a *policySurfaceAdapter) PermissionSceneHook() permissions.PolicySceneHookFn {
	if provider, ok := a.base.(interface {
		PermissionSceneHook() permissions.PolicySceneHookFn
	}); ok {
		return provider.PermissionSceneHook()
	}
	return nil
}

// PermissionsSnapshot 转发宿主快照，缺失时返回 nil。
func (a *policySurfaceAdapter) PermissionsSnapshot() map[string]any {
	if provider, ok := a.base.(interface{ PermissionsSnapshot() map[string]any }); ok {
		return provider.PermissionsSnapshot()
	}
	return nil
}

// HasPermissionsSnapshot 宿主是否提供快照钩子，缺失时返回 false。
func (a *policySurfaceAdapter) HasPermissionsSnapshot() bool {
	if provider, ok := a.base.(interface{ HasPermissionsSnapshot() bool }); ok {
		return provider.HasPermissionsSnapshot()
	}
	return false
}

// TrustedDirs 转发受信目录，缺失时返回 nil。
func (a *policySurfaceAdapter) TrustedDirs() []string {
	if provider, ok := a.base.(interface{ TrustedDirs() []string }); ok {
		return provider.TrustedDirs()
	}
	return nil
}

// 编译期断言
var _ permissions.PolicyBaseRail = (*policySurfaceAdapter)(nil)
