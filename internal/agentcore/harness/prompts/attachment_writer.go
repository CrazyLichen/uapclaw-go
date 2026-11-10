package prompts

import (
	"errors"

	interfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// renderLanguageMetadataKey 选项携带渲染语言的 metadata 键（仅在 AddFromPromptSection 内部使用）
	renderLanguageMetadataKey = "render_language"
	// defaultRenderLanguage 未指定渲染语言时的缺省值
	// Python: add_from_prompt_section(language="cn")（:231）
	defaultRenderLanguage = "cn"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContextWriter 已绑定会话的附件写入器。
//
// 不增加任何能力：构造期把会话解析一次，方法内原样转调 Manager，
// 只是把 session_id 换成绑定值；会话为空时返回 ErrSessionRequired。
//
// Python: PromptAttachmentContextWriter（prompt_attachment_manager.py:191-265）
type ContextWriter struct {
	// manager 附件管理器
	manager *Manager
	// sessionID 构造期解析出的会话标识（可能为空）
	sessionID string
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrSessionRequired 附件写入缺少会话标识。
//
// Python: raise ValueError("prompt attachment context requires session_id")（:261-264）
var ErrSessionRequired = errors.New("prompt attachment context requires session_id")

// ──────────────────────────── 导出函数 ────────────────────────────

// BindContext 返回绑定到 cbc 会话的写入器（构造期不报错，同上游）。
//
// Python: PromptAttachmentManager.bind_context（:276-279）
func (m *Manager) BindContext(cbc *interfaces.AgentCallbackContext) *ContextWriter {
	return &ContextWriter{manager: m, sessionID: resolveSessionIDFromContext(cbc)}
}

// BindContextForSession 按已知会话标识构造写入器。
//
// Python 无对应方法（上游 rail 一律经 bind_context 从 ctx 解析）；
// 保留此入口供无回调上下文的调用方与单元测试使用。
func (m *Manager) BindContextForSession(sessionID string) *ContextWriter {
	return &ContextWriter{manager: m, sessionID: sessionID}
}

// SessionID 返回绑定的会话标识（可能为空）。
//
// Python: writer.session_id（:196）
func (w *ContextWriter) SessionID() string {
	if w == nil {
		return ""
	}
	return w.sessionID
}

// Manager 返回底层附件管理器。
func (w *ContextWriter) Manager() *Manager {
	if w == nil {
		return nil
	}
	return w.manager
}

// AddSection 在绑定会话里新增或覆盖一个 section。
//
// Python: PromptAttachmentContextWriter.add_section（:198-222）
func (w *ContextWriter) AddSection(
	section, content string,
	kind AttachmentKind,
	source string,
	opts ...AttachmentOption,
) (*Attachment, error) {
	if w == nil || w.manager == nil || w.sessionID == "" {
		return nil, ErrSessionRequired
	}
	return w.manager.AddSection(w.sessionID, section, content, kind, source, opts...)
}

// AddFromPromptSection 把 PromptSection 渲染后写入绑定会话。
//
// promptSection 为 nil、或渲染结果全空白时返回 (nil, nil)，不报错；
// 优先级沿用 section.Priority（同上游 add_from_prompt_section）。
//
// Python: PromptAttachmentContextWriter.add_from_prompt_section（:224-254）
func (w *ContextWriter) AddFromPromptSection(
	section *saprompt.PromptSection,
	kind AttachmentKind,
	source string,
	opts ...AttachmentOption,
) (*Attachment, error) {
	if section == nil {
		return nil, nil
	}
	if w == nil || w.manager == nil || w.sessionID == "" {
		return nil, ErrSessionRequired
	}
	allOpts := withSectionPriority(opts, section.Priority)
	return w.manager.AddFromPromptSection(w.sessionID, section, kind, source, writerLanguage(opts), allOpts...)
}

// ClearSection 删除绑定会话里的指定 section。
//
// Python: PromptAttachmentContextWriter.clear_section（:256-259）
func (w *ContextWriter) ClearSection(section string) (int, error) {
	if w == nil || w.manager == nil || w.sessionID == "" {
		return 0, ErrSessionRequired
	}
	return w.manager.ClearSection(w.sessionID, section)
}

// WithLanguage 指定 PromptSection 渲染语言。
//
// Python: add_from_prompt_section(..., language=...)（:231）
// Go 的选项函数无法携带非 Attachment 字段，故借 Metadata["render_language"] 传递；
// 该键在 AddFromPromptSection 内被剥离，不写入附件 metadata。
func WithLanguage(language string) AttachmentOption {
	return func(a *Attachment) {
		if a.Metadata == nil {
			a.Metadata = map[string]any{}
		}
		a.Metadata[renderLanguageMetadataKey] = language
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveSessionIDFromContext 按上游顺序解析会话标识。
//
// 顺序：cbc.Session().GetSessionID() → InvokeInputs.Extra["session_id"/"_session_id"]
// → cbc.Extra()["session_id"/"_session_id"] → 空串。
//
// Python: _resolve_session_id_from_context（:141-158）
func resolveSessionIDFromContext(cbc *interfaces.AgentCallbackContext) string {
	if cbc == nil {
		return ""
	}
	if sess := cbc.Session(); sess != nil {
		if id := sess.GetSessionID(); id != "" {
			return id
		}
	}
	if inputs, ok := cbc.Inputs().(*interfaces.InvokeInputs); ok && inputs != nil {
		if id := extraSessionID(inputs.Extra); id != "" {
			return id
		}
	}
	return extraSessionID(cbc.Extra())
}

// extraSessionID 从字典里取 session_id / _session_id。
//
// Python: source.get("session_id") or source.get("_session_id")（:155）
func extraSessionID(values map[string]any) string {
	if values == nil {
		return ""
	}
	for _, key := range []string{"session_id", "_session_id"} {
		if id, ok := values[key].(string); ok && id != "" {
			return id
		}
	}
	return ""
}

// writerLanguage 从选项里读渲染语言（未指定时用 cn）。
func writerLanguage(opts []AttachmentOption) string {
	probe := &Attachment{}
	for _, opt := range opts {
		if opt != nil {
			opt(probe)
		}
	}
	if v, ok := probe.Metadata[renderLanguageMetadataKey].(string); ok && v != "" {
		return v
	}
	return defaultRenderLanguage
}

// withSectionPriority 追加 section 优先级，并剥离渲染语言键。
func withSectionPriority(opts []AttachmentOption, priority int) []AttachmentOption {
	cleaned := make([]AttachmentOption, 0, len(opts)+2)
	cleaned = append(cleaned, opts...)
	if priority != 0 {
		cleaned = append(cleaned, WithAttachmentPriority(priority))
	}
	cleaned = append(cleaned, stripRenderLanguage)
	return cleaned
}

// stripRenderLanguage 从附件 metadata 里移除渲染语言键。
func stripRenderLanguage(a *Attachment) {
	if a.Metadata == nil {
		return
	}
	delete(a.Metadata, renderLanguageMetadataKey)
}
