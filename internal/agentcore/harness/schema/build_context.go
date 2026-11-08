package schema

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BuildContextInterface 交给能力 provider 的运行时载体契约。
//
// 用接口而非直接传 *BuildContext 的唯一原因是：上游 BuildContext.derive 用
// copy.copy 保留子类（jiuwenswarm 的 CodeBuildContext 靠这个活），
// Go 无 copy.copy，只能把 Derive 的返回类型做成接口，让子类自己实现以保类型。
//
// Python: BuildContext (openjiuwen/harness/schema/build_context.py:51-115)
type BuildContextInterface interface {
	// GetLanguage 返回解析后的语言码（如 "cn"/"en"）
	GetLanguage() string
	// GetMemberName 返回团队成员名（非团队成员为空串）
	GetMemberName() string
	// GetRole 返回团队角色值（如 "leader"/"teammate"）
	GetRole() string
	// GetWorkspace 返回活的 Workspace（可能为 nil）
	GetWorkspace() *workspace.Workspace
	// GetMemberCardID 返回解析后的 card id（工具 id 以此做命名空间）
	GetMemberCardID() string
	// GetProjectDir 返回项目根目录（LSP 等 provider 以此定根，可能为空）
	GetProjectDir() string
	// GetTeamOutputsDir 返回共享最终产物目录（无项目成员才有值）
	GetTeamOutputsDir() string
	// GetExtras 返回逃逸口袋，平台活句柄都在这
	GetExtras() map[string]any
	// GetMoa 返回仅对根 agent 生效的调用策略（可能为 nil）
	GetMoa() any
	// Derive 返回带 overrides 的浅拷贝；实现方必须保留自身具体类型。
	Derive(o BuildContextOverrides) BuildContextInterface
}

// BuildContextOverrides Derive 的覆盖项。
//
// 用指针字段区分"未指定"（nil，继承原值）与"显式置空"（指向空值）。
// Workspace 本身不可空，故另设 HasWorkspace 位表达"显式清空"。
//
// Python 侧用 **overrides 关键字参数表达同一语义（build_context.py:85-95）。
type BuildContextOverrides struct {
	// Language 覆盖语言，nil 表示继承
	Language *string
	// MemberName 覆盖成员名，nil 表示继承
	MemberName *string
	// Role 覆盖角色，nil 表示继承
	Role *string
	// Workspace 覆盖工作空间；仅在 HasWorkspace 为真时生效
	Workspace *workspace.Workspace
	// HasWorkspace 是否应用 Workspace 覆盖（含显式置 nil 清空）
	HasWorkspace bool
	// MemberCardID 覆盖 card id，nil 表示继承
	MemberCardID *string
	// ProjectDir 覆盖项目目录，nil 表示继承
	ProjectDir *string
	// TeamOutputsDir 覆盖产物目录，nil 表示继承
	TeamOutputsDir *string
	// Moa 覆盖调用策略；nil 表示不动
	Moa any
}

// BuildContext 交给能力 provider 的运行时载体。
//
// 持有 provider 物化 rail / tool / sub-agent 所需的活句柄（已解析的
// workspace、成员身份、平台子类附加的句柄）。它刻意**不是**可序列化模型：
// 与 TeamRuntimeContext 一样，它是 Spec → Runtime 的边界对象，
// 从不参与 JSON 序列化。
//
// 平台通过**子类**添加类型化句柄（首选）；通用 extras 映射是
// 叠加多个平台时的逃生口。
//
// Python: BuildContext (openjiuwen/harness/schema/build_context.py:51-115)
type BuildContext struct {
	// Language 解析后的语言码（如 "cn"/"en"）
	Language string
	// MemberName 团队成员名（非团队成员为空）
	MemberName string
	// Role 团队角色值（如 "leader"/"teammate"）
	Role string
	// Workspace 活的工作空间（由 spec 解析后回填）
	Workspace *workspace.Workspace
	// MemberCardID 解析后的 card id（工具 id 以此做命名空间）
	MemberCardID string
	// ProjectDir 项目根目录（成员绑定到代码项目时才有）
	ProjectDir string
	// TeamOutputsDir 无项目团队的共享最终产物目录
	TeamOutputsDir string
	// Extras 逃生口袋（平台活句柄）
	Extras map[string]any
	// Moa 仅对根 agent 生效的调用策略
	Moa any
}

// BuildContextFactory 从可序列化种子重建活上下文的工厂。
//
// 跨序列化边界（子进程 spawn / 分布式远端 / 冷恢复）时 BuildContext 不可
// 序列化，平台注册本工厂，spec 携带种子，接收侧用本地句柄重建。
//
// Python: _BUILD_CONTEXT_FACTORY 的类型（build_context.py:124）
type BuildContextFactory func(seed map[string]any) (BuildContextInterface, error)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ParentSysOperationExtrasKey 父 agent 解析出的 SysOperation 在 extras 里的键。
	//
	// DeepAgentSpec.resolve_parts 在这里发布它，使子 agent 工厂能把它交给自己的
	// spec，而不是让 create_deep_agent 现造一个全新的 LOCAL 实例。
	// 与 _parent_model 约定同构。
	//
	// 值必须与上游逐字一致（checkpoint 契约）。
	// Python: PARENT_SYS_OPERATION_EXTRAS_KEY (build_context.py:33)
	ParentSysOperationExtrasKey = "_parent_sys_operation"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// buildContextFactoryMu 保护 buildContextFactory。
	// 注册发生在平台初始化（低频），读取发生在成员重建（相对高频）。
	buildContextFactoryMu sync.RWMutex
	// buildContextFactory 进程级唯一工厂；nil 表示未注册
	buildContextFactory BuildContextFactory
)

// ──────────────────────────── 导出函数 ────────────────────────────

// StringPtr 返回字符串指针（供 BuildContextOverrides 表达"显式指定"）。
func StringPtr(s string) *string { return &s }

// ParentSysOperation 返回发布在 context 上的父 SysOperation（未发布则 nil）。
//
// 容忍 nil 与没有 extras 的上下文，让 provider 可以无条件调用。
//
// Python: parent_sys_operation (build_context.py:36-48)
func ParentSysOperation(context BuildContextInterface) any {
	if context == nil {
		return nil
	}
	extras := context.GetExtras()
	if extras == nil {
		return nil
	}
	return extras[ParentSysOperationExtrasKey]
}

// RegisterBuildContextFactory 注册种子重建工厂（后者覆盖前者，幂等）。
//
// Python: register_build_context_factory (build_context.py:127-143)
func RegisterBuildContextFactory(f BuildContextFactory) {
	buildContextFactoryMu.Lock()
	defer buildContextFactoryMu.Unlock()
	buildContextFactory = f
}

// ResetBuildContextFactoryForTest 清空工厂（仅测试用）。
//
// 生产不得调用：清空会让跨序列化边界的成员重建失效。
func ResetBuildContextFactoryForTest() {
	buildContextFactoryMu.Lock()
	defer buildContextFactoryMu.Unlock()
	buildContextFactory = nil
}

// BuildContextFromSeed 通过已注册的工厂从可序列化种子重建上下文。
//
// 未注册工厂或 seed 为空时**静默返回 nil**（不报错），让调用方回落
// 到既有路径——这是上游的明确语义，不是遗漏。
//
// Python: build_context_from_seed (build_context.py:146-160)
func BuildContextFromSeed(seed map[string]any) (BuildContextInterface, error) {
	buildContextFactoryMu.RLock()
	f := buildContextFactory
	buildContextFactoryMu.RUnlock()
	if f == nil || len(seed) == 0 {
		return nil, nil
	}
	return f(seed)
}

// Derive 返回带 overrides 的浅拷贝。
//
// 用浅拷贝对齐上游 copy.copy：具体子类与全部平台字段都被保留，只有
// 具名字段被覆盖；原上下文不被修改，所以一个每团队基础上下文可以
// 扇出给多个成员。
//
// Extras 刻意**不**深拷贝（与 copy.copy 一致）：需要隔离的调用方自己复制
// 一层，对应上游 agent_configurator.py:867 的
// `member_build_context.extras = dict(member_build_context.extras)`。
//
// Python: BuildContext.derive (build_context.py:85-95)
func (c *BuildContext) Derive(o BuildContextOverrides) BuildContextInterface {
	clone := *c
	if o.Language != nil {
		clone.Language = *o.Language
	}
	if o.MemberName != nil {
		clone.MemberName = *o.MemberName
	}
	if o.Role != nil {
		clone.Role = *o.Role
	}
	if o.HasWorkspace {
		clone.Workspace = o.Workspace
	}
	if o.MemberCardID != nil {
		clone.MemberCardID = *o.MemberCardID
	}
	if o.ProjectDir != nil {
		clone.ProjectDir = *o.ProjectDir
	}
	if o.TeamOutputsDir != nil {
		clone.TeamOutputsDir = *o.TeamOutputsDir
	}
	if o.Moa != nil {
		clone.Moa = o.Moa
	}
	return &clone
}

// ResolveMemberWorkDir 返回本成员隔离的临时工作目录（基类恒 nil）。
//
// 基类 BuildContext 没有平台工作目录约定，只有平台子类才会分配；
// configurator 对无项目/无 worktree 的成员读它作为 shell cwd。
//
// Python: BuildContext.resolve_member_work_dir (build_context.py:97-115)
func (c *BuildContext) ResolveMemberWorkDir() *string { return nil }

// GetLanguage 返回解析后的语言码。
// Python: BuildContext.language
func (c *BuildContext) GetLanguage() string { return c.Language }

// GetMemberName 返回团队成员名。
// Python: BuildContext.member_name
func (c *BuildContext) GetMemberName() string { return c.MemberName }

// GetRole 返回团队角色值。
// Python: BuildContext.role
func (c *BuildContext) GetRole() string { return c.Role }

// GetWorkspace 返回活的工作空间。
// Python: BuildContext.workspace
func (c *BuildContext) GetWorkspace() *workspace.Workspace { return c.Workspace }

// GetMemberCardID 返回解析后的 card id。
// Python: BuildContext.member_card_id
func (c *BuildContext) GetMemberCardID() string { return c.MemberCardID }

// GetProjectDir 返回项目根目录。
// Python: BuildContext.project_dir
func (c *BuildContext) GetProjectDir() string { return c.ProjectDir }

// GetTeamOutputsDir 返回共享最终产物目录。
// Python: BuildContext.team_outputs_dir
func (c *BuildContext) GetTeamOutputsDir() string { return c.TeamOutputsDir }

// GetExtras 返回逃生口袋（懒初始化）。
//
// 返回内部 map 本身而非副本：provider 侧只读，写入口是显式注入函数。
// 与上游 extras 默认 dict 同语义。
//
// Python: BuildContext.extras
func (c *BuildContext) GetExtras() map[string]any {
	if c.Extras == nil {
		c.Extras = make(map[string]any)
	}
	return c.Extras
}

// GetMoa 返回调用策略。
// Python: BuildContext.moa
func (c *BuildContext) GetMoa() any { return c.Moa }

// _ 编译期断言：*BuildContext 必须满足接口。
var _ BuildContextInterface = (*BuildContext)(nil)
