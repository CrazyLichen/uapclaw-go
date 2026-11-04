package prompts

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// Attachment 会话级动态提示词片段。
//
// Python: PromptAttachment (prompt_attachment_manager.py:36-53)
type Attachment struct {
	// ID 片段标识，由 session 与 section 派生
	ID string
	// Section 片段名（同名覆盖）
	Section string
	// Kind 片段类型
	Kind AttachmentKind
	// Content 模型可见内容
	Content string
	// Priority 渲染排序优先级（越小越靠前）
	Priority int
	// Source 写入方标识，用于按来源清空
	Source string
	// SessionID 归属会话
	SessionID string
	// CreatedAt 首次写入时间（UTC RFC3339）
	CreatedAt string
	// UpdatedAt 最近更新时间（UTC RFC3339）
	UpdatedAt string
	// ExpiresAt 过期时间（UTC RFC3339），空表示不过期
	ExpiresAt string
	// Metadata 业务元数据（写入时补 section/source）
	Metadata map[string]any
	// ContentKind 内容类型（text/plain、text/markdown 等）
	ContentKind string
	// ContentPath 内容对应的文件路径
	ContentPath string
	// ContentSHA256 内容原始哈希
	ContentSHA256 string
}

// AttachmentOption Attachment 写入选项。
type AttachmentOption func(*Attachment)

// AttachmentFilter 查询/删除过滤器，零值字段表示不参与过滤。
type AttachmentFilter struct {
	// SessionID 限定会话
	SessionID string
	// Section 限定片段名
	Section string
	// Kind 限定类型
	Kind AttachmentKind
	// Source 限定来源
	Source string
	// AllowAll 允许无过滤条件的破坏性操作（对齐上游 allow_all）
	AllowAll bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// AttachmentKind 动态提示词片段类型。
//
// Python: PromptAttachmentKind
type AttachmentKind string

const (
	// AttachmentGeneric 通用片段
	AttachmentGeneric AttachmentKind = "generic"
	// AttachmentText 纯文本片段
	AttachmentText AttachmentKind = "text"
	// AttachmentRuntime 运行时片段（plan 指引等）
	AttachmentRuntime AttachmentKind = "runtime"
	// AttachmentMemory 记忆片段
	AttachmentMemory AttachmentKind = "memory"
	// AttachmentFile 文件引用片段
	AttachmentFile AttachmentKind = "file"
	// AttachmentTool 工具相关片段
	AttachmentTool AttachmentKind = "tool"
	// AttachmentSkill 技能片段
	AttachmentSkill AttachmentKind = "skill"
	// AttachmentDiagnostic 诊断片段
	AttachmentDiagnostic AttachmentKind = "diagnostic"
	// AttachmentTodoReminder 待办提醒片段
	AttachmentTodoReminder AttachmentKind = "todo_reminder"
	// AttachmentWorkspaceDelta 工作区变更片段
	AttachmentWorkspaceDelta AttachmentKind = "workspace_delta"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultAttachmentPriority 默认优先级
	DefaultAttachmentPriority = 100
	// DefaultAttachmentContentKind 默认内容类型
	DefaultAttachmentContentKind = "text/plain"
	// MaxAttachmentChars 单节截断阈值
	MaxAttachmentChars = 12000
	// MaxRenderedChars 整体渲染截断阈值
	MaxRenderedChars = 48000

	// MetadataKeyAttachmentHistory 附件消息身份标记
	MetadataKeyAttachmentHistory = "_openjiuwen_prompt_attachment_history"
	// MetadataKeyMode 附件消息模式（snapshot/delta）
	MetadataKeyMode = "mode"
	// MetadataKeySessionID 附件消息会话 id
	MetadataKeySessionID = "session_id"
	// MetadataKeyState 交付账本（section → hash）
	MetadataKeyState = "state"
	// ModeSnapshot 全量快照
	ModeSnapshot = "snapshot"
	// ModeDelta 增量
	ModeDelta = "delta"

	// CommitCallbacksExtraKey 投递成功回调的 extra 键
	CommitCallbacksExtraKey = "_openjiuwen_prompt_attachment_commit_callbacks"

	// MetadataKeyMessageOrigin 消息来源标记（对齐上游 _openjiuwen_message_origin）
	MetadataKeyMessageOrigin = "_openjiuwen_message_origin"
	// MetadataKeyMessageSourceKind 消息来源细分（对齐上游 _openjiuwen_message_source_kind）
	MetadataKeyMessageSourceKind = "_openjiuwen_message_source_kind"
	// MessageOriginExternalUser 外部真实用户输入
	MessageOriginExternalUser = "external_user"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithAttachmentPriority 设置优先级。
func WithAttachmentPriority(p int) AttachmentOption {
	return func(a *Attachment) { a.Priority = p }
}

// WithAttachmentMetadata 合并元数据。
func WithAttachmentMetadata(m map[string]any) AttachmentOption {
	return func(a *Attachment) {
		if a.Metadata == nil {
			a.Metadata = map[string]any{}
		}
		for k, v := range m {
			a.Metadata[k] = v
		}
	}
}

// WithAttachmentContentKind 设置内容类型。
func WithAttachmentContentKind(k string) AttachmentOption {
	return func(a *Attachment) {
		if k != "" {
			a.ContentKind = k
		}
	}
}

// WithAttachmentExpiresAt 设置过期时间（UTC RFC3339）。
func WithAttachmentExpiresAt(t string) AttachmentOption {
	return func(a *Attachment) { a.ExpiresAt = t }
}

// WithAttachmentContentPath 设置内容文件路径。
func WithAttachmentContentPath(p string) AttachmentOption {
	return func(a *Attachment) { a.ContentPath = p }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateAttachment 写入前校验。
func validateAttachment(a *Attachment) error {
	if a.SessionID == "" {
		return fmt.Errorf("prompt attachment requires session_id")
	}
	if a.Section == "" {
		return fmt.Errorf("prompt attachment requires section")
	}
	return nil
}
