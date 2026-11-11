package schema

import (
	"context"
	"fmt"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// PublicHTTPSFetchContextAttr 公开 HTTPS 抓取的 per-tool 回调属性键。
	//
	// Python: tool_permission_context.PUBLIC_HTTPS_FETCH_CONTEXT_ATTR
	// （jiuwenswarm/agents/harness/common/rails/permissions/tool_permission_context.py:14）
	//
	// 这是限制标记而非授权，且「从不共享 ctx.extra」——上游刻意不把它放进
	// __all__，只在权限层内部使用。Go 侧落在 schema 包供 permissions 引用。
	PublicHTTPSFetchContextAttr = "_jiuwenswarm_public_https_fetch"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PermissionContext 权限上下文，描述一次权限判断所需的环境信息。
type PermissionContext struct {
	// PrincipalUserID 权限 owner（channel config 的 my_user_id）
	PrincipalUserID string `json:"principal_user_id"`
	// TriggeringUserID 触发者（IM sender）
	TriggeringUserID string `json:"triggering_user_id"`
	// ChannelID 渠道标识
	ChannelID string `json:"channel_id"`
	// GroupDigitalAvatar 是否为数字分身场景
	GroupDigitalAvatar bool `json:"group_digital_avatar"`
	// WebUserID 预留：第二期 web 端本人审批
	WebUserID string `json:"web_user_id"`
	// EnableMemory 是否启用记忆（默认 true）
	// Python: PermissionContext.enable_memory
	EnableMemory bool `json:"enable_memory"`
	// AvatarPrincipalName 数字分身主体名称
	// Python: PermissionContext.avatar_principal_name
	AvatarPrincipalName string `json:"avatar_principal_name"`
	// AvatarMode 是否为群聊消息
	// Python: PermissionContext.avatar_mode
	AvatarMode bool `json:"avatar_mode"`
}

// toolPermChannelIDKey 权限上下文 channelID 的 context key
type toolPermChannelIDKey struct{}

// toolPermContextKey 权限上下文的 context key
type toolPermContextKey struct{}

// toolPermRequestIDKey 宿主请求 ID 的 context key
type toolPermRequestIDKey struct{}

// skillsRebuildSilentKey skills.rebuild 静默标记的 context key
type skillsRebuildSilentKey struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// PermissionContextOption 权限上下文构造选项函数。
type PermissionContextOption func(*PermissionContext)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPermissionContext 创建权限上下文实例。
// EnableMemory 默认为 true，对齐 Python: PermissionContext.enable_memory = True。
func NewPermissionContext(opts ...PermissionContextOption) *PermissionContext {
	pc := &PermissionContext{
		EnableMemory: true,
	}
	for _, opt := range opts {
		opt(pc)
	}
	return pc
}

// NewPermissionContextFromDict 从字典创建权限上下文实例。
// Python: setup_permission_context(request) 中从 metadata 构建 PermissionContext。
func NewPermissionContextFromDict(data map[string]any) *PermissionContext {
	pc := &PermissionContext{EnableMemory: true}
	if v, ok := data["principal_user_id"]; ok {
		if s, ok := v.(string); ok {
			pc.PrincipalUserID = s
		}
	}
	if v, ok := data["triggering_user_id"]; ok {
		if s, ok := v.(string); ok {
			pc.TriggeringUserID = s
		}
	}
	if v, ok := data["channel_id"]; ok {
		if s, ok := v.(string); ok {
			pc.ChannelID = s
		}
	}
	if v, ok := data["group_digital_avatar"]; ok {
		if b, ok := v.(bool); ok {
			pc.GroupDigitalAvatar = b
		}
	}
	if v, ok := data["web_user_id"]; ok {
		if s, ok := v.(string); ok {
			pc.WebUserID = s
		}
	}
	// Python: meta.get("enable_memory", True)
	if v, ok := data["enable_memory"]; ok {
		if b, ok := v.(bool); ok {
			pc.EnableMemory = b
		}
	}
	if v, ok := data["avatar_principal_name"]; ok {
		if s, ok := v.(string); ok {
			pc.AvatarPrincipalName = s
		}
	}
	// Python: avatar_mode = bool(meta.get("avatar_mode", False))
	if v, ok := data["avatar_mode"]; ok {
		if b, ok := v.(bool); ok {
			pc.AvatarMode = b
		}
	}
	return pc
}

// WithPermissionPrincipalUserID 设置主体用户 ID 的选项。
func WithPermissionPrincipalUserID(id string) PermissionContextOption {
	return func(pc *PermissionContext) { pc.PrincipalUserID = id }
}

// WithPermissionTriggeringUserID 设置触发用户 ID 的选项。
func WithPermissionTriggeringUserID(id string) PermissionContextOption {
	return func(pc *PermissionContext) { pc.TriggeringUserID = id }
}

// WithPermissionChannelID 设置通道 ID 的选项。
func WithPermissionChannelID(id string) PermissionContextOption {
	return func(pc *PermissionContext) { pc.ChannelID = id }
}

// WithPermissionGroupDigitalAvatar 设置群组数字化身标志的选项。
func WithPermissionGroupDigitalAvatar(v bool) PermissionContextOption {
	return func(pc *PermissionContext) { pc.GroupDigitalAvatar = v }
}

// WithPermissionWebUserID 设置 Web 用户 ID 的选项。
func WithPermissionWebUserID(id string) PermissionContextOption {
	return func(pc *PermissionContext) { pc.WebUserID = id }
}

// WithPermissionEnableMemory 设置是否启用记忆的选项。
// Python: PermissionContext.enable_memory
func WithPermissionEnableMemory(v bool) PermissionContextOption {
	return func(pc *PermissionContext) { pc.EnableMemory = v }
}

// WithPermissionAvatarPrincipalName 设置数字分身主体名称的选项。
// Python: PermissionContext.avatar_principal_name
func WithPermissionAvatarPrincipalName(name string) PermissionContextOption {
	return func(pc *PermissionContext) { pc.AvatarPrincipalName = name }
}

// WithPermissionAvatarMode 设置群聊消息标志的选项。
// Python: PermissionContext.avatar_mode
func WithPermissionAvatarMode(v bool) PermissionContextOption {
	return func(pc *PermissionContext) { pc.AvatarMode = v }
}

// Scene 返回权限场景类型（web/group_digital_avatar/normal_im）。
// Python: owner_scopes.PermissionContext.scene：先检查 group_digital_avatar，再检查 web
func (p *PermissionContext) Scene() string {
	if p.GroupDigitalAvatar {
		return "group_digital_avatar"
	}
	if strings.TrimSpace(p.ChannelID) == "web" {
		return "web"
	}
	return "normal_im"
}

// OwnerScopeKey 返回权限所有者范围键（channelID + principalUserID）。
// Python: owner_scopes.PermissionContext.owner_scope_key：对两个字段 TrimSpace
func (p *PermissionContext) OwnerScopeKey() [2]string {
	return [2]string{strings.TrimSpace(p.ChannelID), strings.TrimSpace(p.PrincipalUserID)}
}

// ToDict 将权限上下文转换为字典。
func (p *PermissionContext) ToDict() map[string]any {
	return map[string]any{
		"principal_user_id":     p.PrincipalUserID,
		"triggering_user_id":    p.TriggeringUserID,
		"channel_id":            p.ChannelID,
		"group_digital_avatar":  p.GroupDigitalAvatar,
		"web_user_id":           p.WebUserID,
		"enable_memory":         p.EnableMemory,
		"avatar_principal_name": p.AvatarPrincipalName,
		"avatar_mode":           p.AvatarMode,
	}
}

// Validate 校验权限上下文必填字段。
func (p *PermissionContext) Validate() error {
	if p.PrincipalUserID == "" {
		return fmt.Errorf("principal_user_id 不能为空")
	}
	return nil
}

// WithToolPermissionChannelID 将 channelID 注入 context。
// Python: TOOL_PERMISSION_CHANNEL_ID.set(channel_id)
// Go 使用 context.WithValue 不可变值模式，无需 reset/cleanup。
func WithToolPermissionChannelID(ctx context.Context, channelID string) context.Context {
	return context.WithValue(ctx, toolPermChannelIDKey{}, channelID)
}

// ToolPermissionChannelIDFromCtx 从 context 中获取 channelID。
// Python: TOOL_PERMISSION_CHANNEL_ID.get()
func ToolPermissionChannelIDFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(toolPermChannelIDKey{}).(string); ok {
		return v
	}
	return ""
}

// WithToolPermissionRequestID 将宿主请求 ID 注入 context。
// 工具 call id 可用于 UI 关联，但权限来源必须限定在宿主请求范围内。
// Python: TOOL_PERMISSION_REQUEST_ID.set(request_id)
func WithToolPermissionRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, toolPermRequestIDKey{}, requestID)
}

// ToolPermissionRequestIDFromCtx 从 context 中获取宿主请求 ID。
// Python: TOOL_PERMISSION_REQUEST_ID.get()
func ToolPermissionRequestIDFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(toolPermRequestIDKey{}).(string); ok {
		return v
	}
	return ""
}

// WithSkillsRebuildSilent 设置 skills.rebuild 静默标记。
// 静默 follow-up 无 UI 审批，权限轨需自动放行。
// Python: SKILLS_REBUILD_SILENT.set(value)
func WithSkillsRebuildSilent(ctx context.Context, silent bool) context.Context {
	return context.WithValue(ctx, skillsRebuildSilentKey{}, silent)
}

// SkillsRebuildSilentFromCtx 读取 skills.rebuild 静默标记，未设置时为 false。
// Python: SKILLS_REBUILD_SILENT.get()
func SkillsRebuildSilentFromCtx(ctx context.Context) bool {
	if v, ok := ctx.Value(skillsRebuildSilentKey{}).(bool); ok {
		return v
	}
	return false
}

// WithPermissionContextValue 将 PermissionContext 注入 context。
// Python: TOOL_PERMISSION_CONTEXT.set(permission_context)
// Go 使用 context.WithValue 不可变值模式，无需 reset/cleanup。
func WithPermissionContextValue(ctx context.Context, pc *PermissionContext) context.Context {
	return context.WithValue(ctx, toolPermContextKey{}, pc)
}

// PermissionContextFromCtx 从 context 中获取 PermissionContext。
// Python: TOOL_PERMISSION_CONTEXT.get()
func PermissionContextFromCtx(ctx context.Context) *PermissionContext {
	if v, ok := ctx.Value(toolPermContextKey{}).(*PermissionContext); ok {
		return v
	}
	return nil
}

// CurrentPermissionOwnerScope 返回可信的请求 owner token，供 workspace provenance 使用。
//
// Python: current_permission_owner_scope() (owner_scopes.py L104-114)
//
// 无权限上下文、或无 principal_user_id 时返回空串（调用方据此视为「无归属」）。
// channel_id 缺失时用 "default" 占位，保证 token 始终可解析成三段。
//
// ⚠️ 当前无调用方：Python 全仓同样只有定义（零调用），此处按规则 9.2-4 照抄
// 保留，待 workspace provenance 接线时启用。
func CurrentPermissionOwnerScope(ctx context.Context) string {
	permCtx := PermissionContextFromCtx(ctx)
	if permCtx == nil {
		return ""
	}
	channelID := strings.TrimSpace(permCtx.ChannelID)
	principalUserID := strings.TrimSpace(permCtx.PrincipalUserID)
	if principalUserID == "" {
		return ""
	}
	if channelID == "" {
		channelID = "default"
	}
	return "principal:" + channelID + ":" + principalUserID
}

// SetupPermissionContext 从请求的 channel_id 与 metadata 构造权限上下文。
//
// Python: setup_permission_context(request) (owner_scopes.py L61-88)
//
// 返回 nil 表示无需设置权限上下文（非数字分身、且既无 principal_user_id
// 也未显式禁用记忆）。
//
// ⚠️ 命名对齐说明：Python 的 setup_ 前缀来自 ContextVar.set() 的副作用语义。
// Go 侧这里**没有副作用**——它是纯构造函数，调用方拿返回值自行
// WithPermissionContextValue 注入；也不需要有配对的 cleanup（context 不可变）。
func SetupPermissionContext(channelID string, metadata map[string]any) *PermissionContext {
	if metadata == nil {
		metadata = map[string]any{}
	}

	avatarMode := false
	if v, ok := metadata["avatar_mode"].(bool); ok {
		avatarMode = v
	}

	if !avatarMode {
		// 非 avatar_mode：满足以下任一条件才设置上下文
		//   1. 存在 principal_user_id —— workspace authority 需要
		//   2. enable_memory 显式为 false —— 用于禁用记忆
		// 两者都不满足时返回 nil（原有行为不受影响）。
		// Python: if principal_user_id or meta.get("enable_memory") is False:
		principalUserID := strings.TrimSpace(stringFieldOf(metadata, "principal_user_id"))
		enableMemoryExplicitFalse := false
		if v, ok := metadata["enable_memory"].(bool); ok && !v {
			enableMemoryExplicitFalse = true
		}
		if principalUserID == "" && !enableMemoryExplicitFalse {
			return nil
		}
		pc := NewPermissionContextFromDict(metadata)
		pc.AvatarMode = false
		if strings.TrimSpace(pc.ChannelID) == "" {
			pc.ChannelID = channelID
		}
		return pc
	}

	// avatar_mode=True → 从 metadata 构建完整 PermissionContext
	pc := NewPermissionContextFromDict(metadata)
	// Python: channel_id=getattr(request, "channel_id", "") — Python 从 request 取而非 metadata
	if strings.TrimSpace(pc.ChannelID) == "" && channelID != "" {
		pc.ChannelID = channelID
	}
	return pc
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stringFieldOf 读取 map 里的字符串字段，非字符串或缺失返回空串。
//
// Python: str(meta.get(key, "") or "").strip()
func stringFieldOf(data map[string]any, key string) string {
	if data == nil {
		return ""
	}
	text, ok := data[key].(string)
	if !ok {
		return ""
	}
	return text
}
