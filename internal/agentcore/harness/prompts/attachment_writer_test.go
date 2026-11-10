package prompts

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	interfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// TestBindContext_取会话ID 验证 BindContext 从 cbc.Session() 解析会话。
func TestBindContext_取会话ID(t *testing.T) {
	m := NewManager()
	cbc := interfaces.NewAgentCallbackContext(nil, nil, newStubSession("sess-1"))

	w := m.BindContext(cbc)
	require.NotNil(t, w)
	require.Equal(t, "sess-1", w.SessionID())
}

// TestBindContext_会话ID从Inputs回落 验证 session 为空时回落到 InvokeInputs.Extra。
func TestBindContext_会话ID从Inputs回落(t *testing.T) {
	m := NewManager()
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.InvokeInputs{
		Extra: map[string]any{"session_id": "from-inputs"},
	}, nil)

	require.Equal(t, "from-inputs", m.BindContext(cbc).SessionID())
}

// TestBindContext_会话ID从ctxExtra回落 验证 inputs 与 session 都空时回落到 cbc.Extra()。
func TestBindContext_会话ID从ctxExtra回落(t *testing.T) {
	m := NewManager()
	cbc := interfaces.NewAgentCallbackContext(nil, nil, nil)
	cbc.SetExtra("session_id", "from-extra")

	require.Equal(t, "from-extra", m.BindContext(cbc).SessionID())
}

// TestBindContext_三个方法在缺会话时返错 验证 ErrSessionRequired。
func TestBindContext_三个方法在缺会话时返错(t *testing.T) {
	m := NewManager()
	w := m.BindContext(interfaces.NewAgentCallbackContext(nil, nil, nil))
	require.Empty(t, w.SessionID())

	_, err := w.AddSection("sec", "body", AttachmentRuntime, "src")
	require.ErrorIs(t, err, ErrSessionRequired)

	_, err = w.AddFromPromptSection(&saprompt.PromptSection{
		Name: "sec", Content: map[string]string{"cn": "正文"},
	}, AttachmentRuntime, "src", "cn")
	require.ErrorIs(t, err, ErrSessionRequired)

	_, err = w.ClearSection("sec")
	require.ErrorIs(t, err, ErrSessionRequired)
}

// TestContextWriter_AddSection写入附件 验证正常路径写到绑定会话。
func TestContextWriter_AddSection写入附件(t *testing.T) {
	m := NewManager()
	cbc := interfaces.NewAgentCallbackContext(nil, nil, newStubSession("sess-2"))

	got, err := m.BindContext(cbc).AddSection("sec", "body", AttachmentRuntime, "src",
		WithAttachmentPriority(88))
	require.NoError(t, err)
	require.Equal(t, "sess-2", got.SessionID)
	require.Equal(t, 88, got.Priority)

	items, err := m.ListByFilter(AttachmentFilter{SessionID: "sess-2", Section: "sec"})
	require.NoError(t, err)
	require.Len(t, items, 1)
}

// TestContextWriter_AddFromPromptSection渲染 验证按 language 渲染并继承 section 优先级。
func TestContextWriter_AddFromPromptSection渲染(t *testing.T) {
	m := NewManager()
	cbc := interfaces.NewAgentCallbackContext(nil, nil, newStubSession("sess-3"))
	section := &saprompt.PromptSection{
		Name:     "mode_instructions",
		Content:  map[string]string{"cn": "中文内容", "en": "english"},
		Priority: 84,
	}

	got, err := m.BindContext(cbc).AddFromPromptSection(section, AttachmentRuntime, "src", "cn")
	require.NoError(t, err)
	require.Equal(t, "中文内容", got.Content)
	require.Equal(t, 84, got.Priority)
}

// TestContextWriter_AddFromPromptSection空内容短路 验证 nil 与空白内容返回 nil,nil。
func TestContextWriter_AddFromPromptSection空内容短路(t *testing.T) {
	m := NewManager()
	w := m.BindContext(interfaces.NewAgentCallbackContext(nil, nil, newStubSession("s")))

	got, err := w.AddFromPromptSection(nil, AttachmentRuntime, "src", "cn")
	require.NoError(t, err)
	require.Nil(t, got)

	got, err = w.AddFromPromptSection(&saprompt.PromptSection{Name: "x", Content: map[string]string{"cn": "   "}},
		AttachmentRuntime, "src", "cn")
	require.NoError(t, err)
	require.Nil(t, got)
}

// TestContextWriter_ClearSection 验证清空绑定会话的 section。
func TestContextWriter_ClearSection(t *testing.T) {
	m := NewManager()
	cbc := interfaces.NewAgentCallbackContext(nil, nil, newStubSession("sess-4"))
	w := m.BindContext(cbc)
	_, err := w.AddSection("sec", "body", AttachmentRuntime, "src")
	require.NoError(t, err)

	n, err := w.ClearSection("sec")
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

// TestBindContextForSession_直接绑定 验证无 cbc 时按会话号构造（progressive 与单测使用）。
func TestBindContextForSession_直接绑定(t *testing.T) {
	m := NewManager()
	w := m.BindContextForSession("sess-5")
	require.Equal(t, "sess-5", w.SessionID())
	require.Equal(t, m, w.Manager())
}

// TestContextWriter_Manager访问器 验证 nil 安全。
func TestContextWriter_Manager访问器(t *testing.T) {
	var w *ContextWriter
	require.Nil(t, w.Manager())
	require.Equal(t, "", w.SessionID())
}

// stubSession 单测用最小会话门面（SessionFacade 全方法实现）。
type stubSession struct{ id string }

// newStubSession 构造最小会话门面。
func newStubSession(id string) *stubSession { return &stubSession{id: id} }

// GetSessionID 返回会话标识。
func (s *stubSession) GetSessionID() string { return s.id }

// UpdateState 空实现。
func (s *stubSession) UpdateState(_ map[string]any) {}

// GetState 空实现。
func (s *stubSession) GetState(_ sessionstate.StateKey) (any, error) { return nil, nil }

// DumpState 空实现。
func (s *stubSession) DumpState() map[string]any { return nil }

// WriteStream 空实现。
func (s *stubSession) WriteStream(_ context.Context, _ any) error { return nil }

// WriteCustomStream 空实现。
func (s *stubSession) WriteCustomStream(_ context.Context, _ any) error { return nil }

// GetEnv 空实现。
func (s *stubSession) GetEnv(_ string, _ ...any) any { return nil }

// Interact 空实现。
func (s *stubSession) Interact(_ context.Context, _ any) error { return nil }

// ClearSession 空实现。
func (s *stubSession) ClearSession(_ context.Context) {}

// GetCacheIdentity 返回零值缓存身份（测试替身，不参与 KV 亲和）。
func (s *stubSession) GetCacheIdentity() kv_cache.KVCacheIdentity {
	return kv_cache.KVCacheIdentity{}
}

// GetParentSessionID 返回空父会话（测试替身）。
func (s *stubSession) GetParentSessionID() string { return "" }

// 编译期验证会话桩满足门面接口。
var _ sessioninterfaces.SessionFacade = (*stubSession)(nil)
