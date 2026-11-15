package security

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PermissionResult 权限判定结果
type PermissionResult struct {
	// Permission 权限级别
	Permission PermissionLevel `json:"permission"`
	// MatchedRule 匹配的规则标识
	MatchedRule string `json:"matched_rule,omitempty"`
	// Reason 判定原因
	Reason string `json:"reason,omitempty"`
	// ExternalPaths 涉及的外部路径
	ExternalPaths []string `json:"external_paths,omitempty"`
}

// PermissionConfirmResponse 工具权限确认响应
//
// 在 ASK 场景下用户对「允许一次 / 记住并写回策略 / 拒绝 / 回退到内置中断」的确认结果。
// Approved 且 AutoConfirm 时，护栏走合并 permissions、更新内存并写盘的路径；
// 仅 Approved 则为本次放行；
// Action=ConfirmActionInterrupt 时回退到内置 ConfirmInterrupt 流程。
//
// Python: PermissionConfirmResponse | Literal["interrupt"] | None (host.py L46)
type PermissionConfirmResponse struct {
	// Action 确认动作类型（默认 ConfirmActionConfirm）
	Action ConfirmAction `json:"action,omitempty"`
	// Approved 是否批准
	Approved bool `json:"approved"`
	// Feedback 用户反馈
	Feedback string `json:"feedback,omitempty"`
	// AutoConfirm 是否自动确认（记住并写回策略）
	AutoConfirm bool `json:"auto_confirm,omitempty"`
	// PersistScope 持久化范围："session"（仅本会话）/ "permanent"（总是允许）
	// 空串表示未指定，调用方回退到 AutoConfirm 判定。
	//
	// Python: openjiuwen PermissionConfirmResponse.wants_session_persist() /
	// wants_permanent_persist()（SDK 方法，Go 侧改用显式字段表达）
	PersistScope string `json:"persist_scope,omitempty"`
}

// WantsPermanentPersist 是否永久记住（走 persist_allow_rule）。
// Python: PermissionConfirmResponse.wants_permanent_persist (models.py:67-72)：
// approved 且 auto_confirm 且（persist_allow 缺省或为 True）。
// Go 的 PersistScope 语义映射："session" → false；""/"permanent" → true
// （空串对应 Python persist_allow is None 的旧宿主回退）。
func (p *PermissionConfirmResponse) WantsPermanentPersist() bool {
	if !p.Approved || !p.AutoConfirm {
		return false
	}
	return p.PersistScope != "session"
}

// WantsSessionPersist 是否会话内记住（走 persist_session_allow_rule）。
// Python: PermissionConfirmResponse.wants_session_persist (models.py:74-75)：
// approved 且 auto_confirm 且 persist_allow is False。
func (p *PermissionConfirmResponse) WantsSessionPersist() bool {
	return p.Approved && p.AutoConfirm && p.PersistScope == "session"
}


// PermissionSceneHookInput 传给 PermissionSceneHook 的入参。
//
// Python: PermissionSceneHookInput (host.py L15-23)
type PermissionSceneHookInput struct {
	// GoCtx Go 标准上下文（从 BeforeToolCall 的 ctx 参数传入）
	// Python 端通过 ContextVar 获取权限上下文，Go 端通过 context.WithValue 传递，
	// 需要此字段将 Go context 传递给 scene hook 实现方。
	GoCtx context.Context
	// Ctx 回调上下文
	Ctx *agentinterfaces.AgentCallbackContext
	// ToolCall 工具调用
	ToolCall *llmschema.ToolCall
	// UserInput 用户输入（来源多样：InteractiveInput / map / string）
	UserInput any
	// NormalizedToolName 归一化工具名
	NormalizedToolName string
	// ToolArgs 工具参数
	ToolArgs map[string]any
	// Engine 权限引擎
	Engine *PermissionEngine
}

// PermissionConfirmationRequest 传给 RequestPermissionConfirmationHook 的入参。
//
// Python: PermissionConfirmationRequest (host.py L36-43)
type PermissionConfirmationRequest struct {
	// GoCtx Go 标准上下文（从 BeforeToolCall 的 ctx 参数传入）
	// 用于从 context 中读取 channelID 等权限上下文信息。
	GoCtx context.Context
	// Ctx 回调上下文
	Ctx *agentinterfaces.AgentCallbackContext
	// ToolCall 工具调用
	ToolCall *llmschema.ToolCall
	// Result 权限判定结果
	Result *PermissionResult
	// AutoConfirmKey 自动确认键
	AutoConfirmKey string
}

// ToolPermissionHost 由 Agent 服务或 CLI 在构造 DeepAgent / PermissionInterruptRail 时注入。
//
// Python: ToolPermissionHost (host.py L62-99)
type ToolPermissionHost struct {
	// GetPermissionsSnapshot 返回与 config['permissions'] 同结构的 dict
	GetPermissionsSnapshot func() map[string]any
	// PersistAllowRule 自定义「总是允许」写盘
	PersistAllowRule func(permissions map[string]any) bool
	// PersistSessionAllowRule 自定义「会话内记住」写盘。
	// 产品层应写入该 sessionID 的 Session 文件；未设置时护栏只更新内存。
	PersistSessionAllowRule func(permissions map[string]any, sessionID string) bool
	// ResolveWorkspaceDir 外部路径校验用的 workspace 根目录
	ResolveWorkspaceDir func() string
	// PermissionYAMLPath Agent 配置文件路径
	PermissionYAMLPath string
	// ToolPermissionChecksActive 若返回假则跳过工具权限校验
	ToolPermissionChecksActive func() bool
	// RequestPermissionConfirmation 对 ASK 征求用户确认
	RequestPermissionConfirmation RequestPermissionConfirmationHook
	// PermissionSceneHook 宿主场景钩子
	PermissionSceneHook PermissionSceneHookFn
}

// PermissionSceneHookFn 宿主场景钩子函数类型。
// 在通用 tiered 判定前介入（如数字分身 / owner_scopes）。
// 返回 nil 表示继续走引擎 tiered 判定；
// 返回 ("approve",) 直接放行；("reject", msg) 拒绝。
//
// Python: PermissionSceneHook (host.py L26-33)
type PermissionSceneHookFn func(input PermissionSceneHookInput) ([]string, error)

// RequestPermissionConfirmationHook 对 PermissionLevel.ASK 征求用户确认的钩子。
//
// Python: RequestPermissionConfirmationHook (host.py L48-51)
type RequestPermissionConfirmationHook func(req PermissionConfirmationRequest) (*PermissionConfirmResponse, error)

// PermissionRailConstructor 权限护栏构造器函数类型。
// 由调用方传入具体实现，解耦 security 与 rails/security 的循环依赖。
//
// Python: factory.py 函数内 from...import PermissionInterruptRail
type PermissionRailConstructor func(
	config map[string]any,
	engine *PermissionEngine,
	toolNames []string,
	llm *llm.Model,
	modelName string,
	host *ToolPermissionHost,
) agentinterfaces.AgentRail

// ──────────────────────────── 枚举 ────────────────────────────

// PermissionLevel 权限级别枚举
type PermissionLevel int

// ConfirmAction 权限确认动作类型
//
// Python: PermissionConfirmationResult = PermissionConfirmResponse | Literal["interrupt"] | None (host.py L46)
type ConfirmAction int

const (
	// ConfirmActionConfirm 宿主确认结果（默认）
	ConfirmActionConfirm ConfirmAction = iota
	// ConfirmActionInterrupt 回退到内置 ConfirmInterrupt 流程
	// Python: ext_out == "interrupt" → 跳过 hosted 处理，走标准 interrupt
	ConfirmActionInterrupt
)

const (
	// PermissionLevelNone 无匹配规则（对齐 Python fallback(no_config) 时的 None）
	// 调用方应将 None 提升为 Ask，表示"未配置时默认询问"
	PermissionLevelNone PermissionLevel = iota
	// PermissionLevelAllow 允许执行，无需确认
	PermissionLevelAllow
	// PermissionLevelAsk 弹出确认框，用户决定
	PermissionLevelAsk
	// PermissionLevelDeny 拒绝执行，返回错误
	PermissionLevelDeny
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ParsePermissionLevel 从字符串解析 PermissionLevel
func ParsePermissionLevel(s string) (PermissionLevel, error) {
	switch strings.ToLower(s) {
	case "allow":
		return PermissionLevelAllow, nil
	case "ask":
		return PermissionLevelAsk, nil
	case "deny":
		return PermissionLevelDeny, nil
	default:
		return PermissionLevelNone, fmt.Errorf("无效的权限级别: %q", s)
	}
}

// IsAllowed 判断权限是否为允许
func (r *PermissionResult) IsAllowed() bool {
	return r.Permission == PermissionLevelAllow
}

// IsDenied 判断权限是否为拒绝
func (r *PermissionResult) IsDenied() bool {
	return r.Permission == PermissionLevelDeny
}

// NeedsApproval 判断是否需要用户确认
func (r *PermissionResult) NeedsApproval() bool {
	return r.Permission == PermissionLevelAsk
}

// String 返回 PermissionLevel 的字符串表示
func (l PermissionLevel) String() string {
	switch l {
	case PermissionLevelNone:
		return "none"
	case PermissionLevelAllow:
		return "allow"
	case PermissionLevelAsk:
		return "ask"
	case PermissionLevelDeny:
		return "deny"
	default:
		return fmt.Sprintf("unknown(%d)", l)
	}
}

// MarshalJSON 实现 json.Marshaler 接口
func (l PermissionLevel) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

// UnmarshalJSON 实现 json.Unmarshaler 接口
func (l *PermissionLevel) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("PermissionLevel 应为字符串，解析失败: %w", err)
	}
	parsed, err := ParsePermissionLevel(s)
	if err != nil {
		return err
	}
	*l = parsed
	return nil
}
