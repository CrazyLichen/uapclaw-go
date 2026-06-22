package processor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	common_schema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockModelContext 测试用 ModelContext mock
type mockModelContext struct {
	sessionID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

func (m *mockModelContext) Len() int                                            { return 0 }
func (m *mockModelContext) GetMessages(_ int, _ bool) []llm_schema.BaseMessage { return nil }
func (m *mockModelContext) SetMessages(_ []llm_schema.BaseMessage, _ bool)      {}
func (m *mockModelContext) PopMessages(_ int, _ bool) []llm_schema.BaseMessage  { return nil }
func (m *mockModelContext) ClearMessages(_ context.Context, _ bool, _ ...iface.Option) error { return nil }
func (m *mockModelContext) AddMessages(_ context.Context, _ llm_schema.BaseMessage, _ ...iface.Option) ([]llm_schema.BaseMessage, error) {
	return nil, nil
}
func (m *mockModelContext) GetContextWindow(_ context.Context, _ []llm_schema.BaseMessage, _ []*common_schema.ToolInfo, _ int, _ int, _ ...iface.Option) (*iface.ContextWindow, error) {
	return nil, nil
}
func (m *mockModelContext) Statistic() *iface.ContextStats   { return nil }
func (m *mockModelContext) SessionID() string                { return m.sessionID }
func (m *mockModelContext) ContextID() string                { return "ctx-123" }
func (m *mockModelContext) TokenCounter() token.TokenCounter { return nil }
func (m *mockModelContext) ReloaderTool() tool.Tool          { return nil }
func (m *mockModelContext) WorkspaceDir() string             { return "" }
func (m *mockModelContext) SetSessionRef(_ *session.Session) {}
func (m *mockModelContext) GetSessionRef() *session.Session  { return nil }
func (m *mockModelContext) OffloadMessages(_ string, _ []llm_schema.BaseMessage) {}
func (m *mockModelContext) SaveState() map[string]any        { return nil }
func (m *mockModelContext) LoadState(_ map[string]any)       {}
func (m *mockModelContext) CompressContext(_ context.Context, _ ...iface.CompressContextOption) (string, error) {
	return "", nil
}
// TestOffloadMessages_in_memory模式正常 验证 in_memory 模式正常流程
func TestOffloadMessages_in_memory模式正常(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
	mc := &mockModelContext{sessionID: "test-session"}

	result, err := p.OffloadMessages(context.Background(), mc, "user", "摘要", msgs,
		iface.WithOffloadType("in_memory"),
		iface.WithOffloadHandle("test-handle"),
	)
	if err != nil {
		t.Fatalf("OffloadMessages 返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("in_memory 模式应返回 OffloadMessage")
	}
	offloadable, ok := result.(schema.Offloadable)
	if !ok {
		t.Fatal("结果应实现 Offloadable 接口")
	}
	info := offloadable.GetOffloadInfo()
	if info.OffloadType != "in_memory" {
		t.Errorf("OffloadType = %q, want in_memory", info.OffloadType)
	}
	if info.OffloadHandle != "test-handle" {
		t.Errorf("OffloadHandle = %q, want test-handle", info.OffloadHandle)
	}
}

// TestOffloadMessages_filesystem模式正常 验证 filesystem 模式正常流程
func TestOffloadMessages_filesystem模式正常(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
	mc := &mockModelContext{sessionID: "test-session"}
	tmpDir := t.TempDir()
	offloadPath := filepath.Join(tmpDir, "offload", "test.json")

	result, err := p.OffloadMessages(context.Background(), mc, "assistant", "摘要", msgs,
		iface.WithOffloadType("filesystem"),
		iface.WithOffloadHandle("test-handle"),
		iface.WithOffloadPath(offloadPath),
	)
	if err != nil {
		t.Fatalf("OffloadMessages 返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("filesystem 模式应返回 OffloadMessage")
	}
	offloadable, ok := result.(schema.Offloadable)
	if !ok {
		t.Fatal("结果应实现 Offloadable 接口")
	}
	info := offloadable.GetOffloadInfo()
	if info.OffloadType != "filesystem" {
		t.Errorf("OffloadType = %q, want filesystem", info.OffloadType)
	}
	// 验证文件已写入
	data, err := os.ReadFile(offloadPath)
	if err != nil {
		t.Fatalf("读取 offload 文件失败: %v", err)
	}
	if len(data) == 0 {
		t.Error("offload 文件内容不应为空")
	}
}

// TestOffloadMessages_filesystem失败fallback 验证 filesystem 写入失败后 fallback 到 in_memory
func TestOffloadMessages_filesystem失败fallback(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
	mc := &mockModelContext{sessionID: "test-session"}

	// 使用相对路径，写入会失败，fallback 到 in_memory
	result, err := p.OffloadMessages(context.Background(), mc, "user", "摘要", msgs,
		iface.WithOffloadType("filesystem"),
		iface.WithOffloadHandle("test-handle"),
		iface.WithOffloadPath("relative/path.json"),
	)
	if err != nil {
		t.Fatalf("OffloadMessages 返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("fallback 后应返回 in_memory OffloadMessage")
	}
	offloadable, ok := result.(schema.Offloadable)
	if !ok {
		t.Fatal("结果应实现 Offloadable 接口")
	}
	info := offloadable.GetOffloadInfo()
	if info.OffloadType != "in_memory" {
		t.Errorf("fallback 后 OffloadType = %q, want in_memory", info.OffloadType)
	}
}

// TestOffloadMessages_自动生成Handle 验证未指定 Handle 时自动生成 UUID
func TestOffloadMessages_自动生成Handle(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
	mc := &mockModelContext{sessionID: "test-session"}

	result, err := p.OffloadMessages(context.Background(), mc, "user", "摘要", msgs,
		iface.WithOffloadType("in_memory"),
	)
	if err != nil {
		t.Fatalf("OffloadMessages 返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("应返回 OffloadMessage")
	}
	offloadable, ok := result.(schema.Offloadable)
	if !ok {
		t.Fatal("结果应实现 Offloadable 接口")
	}
	info := offloadable.GetOffloadInfo()
	if info.OffloadHandle == "" {
		t.Error("OffloadHandle 不应为空（应自动生成 UUID）")
	}
}

// TestOffloadMessages_默认filesystem 验证默认使用 filesystem 模式
func TestOffloadMessages_默认filesystem(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
	mc := &mockModelContext{sessionID: "test-session"}
	tmpDir := t.TempDir()
	offloadPath := filepath.Join(tmpDir, "offload", "default.json")

	// 不指定 OffloadType，默认应为 filesystem
	result, err := p.OffloadMessages(context.Background(), mc, "assistant", "摘要", msgs,
		iface.WithOffloadPath(offloadPath),
	)
	if err != nil {
		t.Fatalf("OffloadMessages 返回错误: %v", err)
	}
	if result == nil {
		t.Fatal("默认 filesystem 模式应返回 OffloadMessage")
	}
}

// TestGenerateOffloadPath 通过 BaseProcessor 方法测试 验证路径生成
func TestGenerateOffloadPath_通过BaseProcessor(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	// 有工作目录
	path := p.GenerateOffloadPath("/workspace", "session123", "handle456")
	expected := filepath.Join("/workspace", "context", "session123_context", "offload", "handle456.json")
	if path != expected {
		t.Errorf("path = %q, want %q", path, expected)
	}
	// 无工作目录
	path2 := p.GenerateOffloadPath("", "session123", "handle456")
	expected2 := filepath.Join("memory", "offloads", "session123", "handle456.json")
	if path2 != expected2 {
		t.Errorf("path2 = %q, want %q", path2, expected2)
	}
}

// TestOffloadMessages_各角色 验证不同角色类型的 offload
func TestOffloadMessages_各角色(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
	mc := &mockModelContext{sessionID: "test-session"}

	roles := []string{"system", "tool", "assistant"}
	for _, role := range roles {
		result, err := p.OffloadMessages(context.Background(), mc, role, "摘要", msgs,
			iface.WithOffloadType("in_memory"),
			iface.WithOffloadHandle("handle-"+role),
		)
		if err != nil {
			t.Errorf("角色 %q OffloadMessages 返回错误: %v", role, err)
		}
		if result == nil {
			t.Errorf("角色 %q 应返回 OffloadMessage", role)
		}
	}
}

// TestOffloadMessages_filesystem无路径 验证 filesystem 模式无 offloadPath 时自动生成
func TestOffloadMessages_filesystem无路径(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)
	msgs := []llm_schema.BaseMessage{llm_schema.NewUserMessage("hello")}
	mc := &mockModelContext{sessionID: "test-session"}

	// filesystem 模式但使用自动生成的路径（相对路径，写入会失败，fallback）
	result, err := p.OffloadMessages(context.Background(), mc, "user", "摘要", msgs,
		iface.WithOffloadType("filesystem"),
	)
	if err != nil {
		t.Fatalf("OffloadMessages 返回错误: %v", err)
	}
	// 自动生成路径是相对路径 → 写入失败 → fallback 到 in_memory
	if result == nil {
		t.Fatal("fallback 后应返回 OffloadMessage")
	}
}
