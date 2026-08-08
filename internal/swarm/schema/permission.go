package schema

import (
	"context"
	"fmt"
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
}

// ──────────────────────────── 枚举 ────────────────────────────

// PermissionContextOption 权限上下文构造选项函数。
type PermissionContextOption func(*PermissionContext)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// toolPermChannelIDKey 权限上下文 channelID 的 context key
type toolPermChannelIDKey struct{}

// toolPermContextKey 权限上下文的 context key
type toolPermContextKey struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// NewPermissionContext 创建权限上下文实例。
func NewPermissionContext(opts ...PermissionContextOption) *PermissionContext {
	pc := &PermissionContext{}
	for _, opt := range opts {
		opt(pc)
	}
	return pc
}

// NewPermissionContextFromDict 从字典创建权限上下文实例。
func NewPermissionContextFromDict(data map[string]any) *PermissionContext {
	pc := &PermissionContext{}
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

// Scene 返回权限场景类型（web/group_digital_avatar/normal_im）。
func (p *PermissionContext) Scene() string {
	if p.ChannelID == "web" {
		return "web"
	}
	if p.GroupDigitalAvatar {
		return "group_digital_avatar"
	}
	return "normal_im"
}

// OwnerScopeKey 返回权限所有者范围键（channelID + principalUserID）。
func (p *PermissionContext) OwnerScopeKey() [2]string {
	return [2]string{p.ChannelID, p.PrincipalUserID}
}

// ToDict 将权限上下文转换为字典。
func (p *PermissionContext) ToDict() map[string]any {
	return map[string]any{
		"principal_user_id":    p.PrincipalUserID,
		"triggering_user_id":   p.TriggeringUserID,
		"channel_id":           p.ChannelID,
		"group_digital_avatar": p.GroupDigitalAvatar,
		"web_user_id":          p.WebUserID,
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
// 对齐 Python: TOOL_PERMISSION_CHANNEL_ID.set(channel_id)
// Go 使用 context.WithValue 不可变值模式，无需 reset/cleanup。
func WithToolPermissionChannelID(ctx context.Context, channelID string) context.Context {
	return context.WithValue(ctx, toolPermChannelIDKey{}, channelID)
}

// ToolPermissionChannelIDFromCtx 从 context 中获取 channelID。
// 对齐 Python: TOOL_PERMISSION_CHANNEL_ID.get()
func ToolPermissionChannelIDFromCtx(ctx context.Context) string {
	if v, ok := ctx.Value(toolPermChannelIDKey{}).(string); ok {
		return v
	}
	return ""
}

// WithPermissionContextValue 将 PermissionContext 注入 context。
// 对齐 Python: TOOL_PERMISSION_CONTEXT.set(permission_context)
// Go 使用 context.WithValue 不可变值模式，无需 reset/cleanup。
func WithPermissionContextValue(ctx context.Context, pc *PermissionContext) context.Context {
	return context.WithValue(ctx, toolPermContextKey{}, pc)
}

// PermissionContextFromCtx 从 context 中获取 PermissionContext。
// 对齐 Python: TOOL_PERMISSION_CONTEXT.get()
func PermissionContextFromCtx(ctx context.Context) *PermissionContext {
	if v, ok := ctx.Value(toolPermContextKey{}).(*PermissionContext); ok {
		return v
	}
	return nil
}
