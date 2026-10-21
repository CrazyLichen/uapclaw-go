package extensions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions/plugin"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 结构体 ────────────────────────────

// memPluginClient 内存插件客户端（测试用：不走 gRPC）
type memPluginClient struct {
	setDirCalls   []string
	shutdownCalls []string
	handledEvents []string
	transport     transport.AgentTransport
}

func (m *memPluginClient) SetExtensionDir(ctx context.Context, path string) error {
	m.setDirCalls = append(m.setDirCalls, path)
	return nil
}

func (m *memPluginClient) HandleEvent(ctx context.Context, event string, data map[string]any) (map[string]any, error) {
	m.handledEvents = append(m.handledEvents, event)
	if _, ok := data["memory_blocks"]; ok {
		data["memory_blocks"] = append(data["memory_blocks"].([]any), "来自插件")
	}
	return data, nil
}

func (m *memPluginClient) Shutdown(ctx context.Context) error {
	m.shutdownCalls = append(m.shutdownCalls, "shutdown")
	return nil
}

func (m *memPluginClient) Transport() transport.AgentTransport { return m.transport }

func (m *memPluginClient) Kill() {}

func (m *memPluginClient) RegisterDeclarations(ctx context.Context) ([]plugin.Declaration, error) {
	return nil, nil
}

// fakeHost 返回内存客户端的工厂
type fakeHost struct {
	clients map[string]PluginClientFace
	errs    map[string]error
}

func (f *fakeHost) Load(root string) (PluginClientFace, error) {
	if err, ok := f.errs[root]; ok {
		return nil, err
	}
	return f.clients[root], nil
}

// newChanAgentTransportT 最简 AgentTransport（ch 双向复用）
type newChanAgentTransportT struct{ ch chan []byte }

func newChanAgentTransport(ch chan []byte) *newChanAgentTransportT {
	return &newChanAgentTransportT{ch: ch}
}

func (c *newChanAgentTransportT) Send(ctx context.Context, data []byte) error {
	c.ch <- data
	return nil
}

func (c *newChanAgentTransportT) Recv() (<-chan []byte, error) { return c.ch, nil }

func (c *newChanAgentTransportT) Close() error { close(c.ch); return nil }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewExtensionLoader 测试 ExtensionLoader 构造
func TestNewExtensionLoader(t *testing.T) {
	loader := NewExtensionLoader(nil)
	if loader == nil {
		t.Error("NewExtensionLoader(nil) = nil, want non-nil")
	}
}

// TestExtensionLoader_AddSearchPath 测试目录存在才加入
func TestExtensionLoader_AddSearchPath(t *testing.T) {
	dir := t.TempDir()
	loader := NewExtensionLoader(nil)
	loader.AddSearchPath(dir)
	loader.AddSearchPath(filepath.Join(dir, "不存在"))
	if got := len(loader.searchPaths); got != 1 {
		t.Errorf("searchPaths 数 = %d, 期望 1（不存在的目录不加）", got)
	}
}

// TestExtensionLoader_DiscoverExtensionRoots 测试发现语义（yaml 或 ext-plugin 即根）
func TestExtensionLoader_DiscoverExtensionRoots(t *testing.T) {
	base := t.TempDir()
	// extA: 有 extension.yaml
	extA := filepath.Join(base, "extA")
	_ = os.MkdirAll(extA, 0o755)
	_ = os.WriteFile(filepath.Join(extA, "extension.yaml"), []byte("id: a\n"), 0o644)
	// extB: 有 ext-plugin
	extB := filepath.Join(base, "extB")
	_ = os.MkdirAll(extB, 0o755)
	_ = os.WriteFile(filepath.Join(extB, "ext-plugin"), []byte("bin"), 0o755)
	// plain: 两者皆无
	plain := filepath.Join(base, "plain")
	_ = os.MkdirAll(plain, 0o755)
	// 文件（非目录）不参与
	_ = os.WriteFile(filepath.Join(base, "file.txt"), []byte("x"), 0o644)

	loader := NewExtensionLoader(nil)
	loader.AddSearchPath(base)
	roots := loader.DiscoverExtensionRoots()
	if len(roots) != 2 {
		t.Fatalf("发现根数 = %d, 期望 2（extA/extB）: %v", len(roots), roots)
	}
}

// TestExtensionLoader_LoadExtension_无入口 返回错误
func TestExtensionLoader_LoadExtension_无入口(t *testing.T) {
	loader := NewExtensionLoader(nil)
	root := t.TempDir() // 无 manifest 也无 ext-plugin
	_, err := loader.LoadExtension(context.Background(), root)
	if err == nil {
		t.Error("无入口二进制的 root 应返回错误")
	}
}

// TestExtensionLoader_LoadExtension_加载失败返回错误 测试 host.Load 失败传播
func TestExtensionLoader_LoadExtension_加载失败返回错误(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "ext-plugin"), []byte("bin"), 0o755)
	host := &fakeHost{errs: map[string]error{root: errors.New("boom")}}
	loader := NewExtensionLoader(nil, WithPluginHost(host))
	_, err := loader.LoadExtension(context.Background(), root)
	if err == nil {
		t.Error("host.Load 失败应返回错误")
	}
}

// TestExtensionLoader_RegisterDeclarations 测试声明注册编排（事件 + transport + SetExtensionDir）
func TestExtensionLoader_RegisterDeclarations(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	ch := make(chan []byte, 4)
	memTransport := newChanAgentTransport(ch) // 测试用 AgentTransport（send/recv 都走 ch）
	pc := &memPluginClient{transport: memTransport}
	host := &fakeHost{clients: map[string]PluginClientFace{"/tmp/extA": pc}}

	loader := NewExtensionLoader(reg, WithPluginHost(host))
	decls := []plugin.Declaration{{
		Metadata:          plugin.Metadata{ID: "extA", Name: "A", Version: "1.0"},
		EventSubs:         []plugin.EventSubscription{{Event: AgentServerMemoryBeforeChat, Priority: 100}},
		ProvidesTransport: true,
	}}
	loaded, err := loader.loadAndRegister(context.Background(), "/tmp/extA", pc, decls)
	if err != nil {
		t.Fatalf("loadAndRegister 失败: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded = %d, 期望 1", len(loaded))
	}
	if len(pc.setDirCalls) != 1 || pc.setDirCalls[0] != "/tmp/extA" {
		t.Errorf("SetExtensionDir 调用不符: %v", pc.setDirCalls)
	}
	// transport 注册进 registry
	if reg.GetAgentServerClient() == nil {
		t.Error("ProvidesTransport 声明应注册 AgentServerClient")
	}
	// 事件桥注册：触发后数据写回
	memCtx := &MemoryHookContext{}
	data := memCtx.ToMap()
	data["memory_blocks"] = []any{}
	reg.Trigger(context.Background(), AgentServerMemoryBeforeChat, data)
	if len(pc.handledEvents) != 1 {
		t.Fatalf("HandleEvent 调用数 = %d, 期望 1", len(pc.handledEvents))
	}
	blocks := data["memory_blocks"].([]any)
	if len(blocks) != 1 || blocks[0] != "来自插件" {
		t.Errorf("事件桥写回不符: %v", blocks)
	}
}

// TestExtensionManager_LoadAllExtensions_单失败继续 测试失败容错
func TestExtensionManager_LoadAllExtensions_单失败继续(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	base := t.TempDir()
	// extOK: 有 manifest
	extOK := filepath.Join(base, "extOK")
	_ = os.MkdirAll(extOK, 0o755)
	_ = os.WriteFile(filepath.Join(extOK, "extension.yaml"), []byte("id: ok\n"), 0o644)
	// extBad: 有 ext-plugin 标记但 host 加载失败
	extBad := filepath.Join(base, "extBad")
	_ = os.MkdirAll(extBad, 0o755)
	_ = os.WriteFile(filepath.Join(extBad, "ext-plugin"), []byte("bin"), 0o755)

	okPC := &memPluginClient{}
	host := &fakeHost{
		clients: map[string]PluginClientFace{extOK: okPC},
		errs:    map[string]error{extBad: errors.New("boom")},
	}
	mgr := NewExtensionManager(reg, WithSearchPaths([]string{base}), WithPluginHostForManager(host))
	if err := mgr.LoadAllExtensions(context.Background()); err != nil {
		t.Fatalf("单个失败不应整体报错: %v", err)
	}
	listed := mgr.ListExtensions()
	if len(listed) != 0 {
		t.Errorf("ListExtensions = %v, 期望 0（memPluginClient 返回空声明）", listed)
	}
	// extBad 失败被吞掉；extOK 加载成功但空声明（RegisterDeclarations 返回 nil）
	if len(mgr.loaded) != 0 {
		t.Errorf("loaded = %d, 期望 0", len(mgr.loaded))
	}
}

// TestExtensionManager_LoadAllExtensions_加载成功 测试完整编排路径
func TestExtensionManager_LoadAllExtensions_加载成功(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	base := t.TempDir()
	extDir := filepath.Join(base, "extOK")
	_ = os.MkdirAll(extDir, 0o755)
	_ = os.WriteFile(filepath.Join(extDir, "extension.yaml"), []byte("id: ok\n"), 0o644)

	okPC := &declPluginClient{decls: []plugin.Declaration{{
		Metadata: plugin.Metadata{ID: "ok", Name: "OK 扩展", Version: "1.0"},
	}}}
	host := &fakeHost{clients: map[string]PluginClientFace{extDir: okPC}}
	mgr := NewExtensionManager(reg, WithSearchPaths([]string{base}), WithPluginHostForManager(host))
	if err := mgr.LoadAllExtensions(context.Background()); err != nil {
		t.Fatalf("LoadAllExtensions 失败: %v", err)
	}
	listed := mgr.ListExtensions()
	if len(listed) != 1 || listed[0]["id"] != "ok" || listed[0]["version"] != "1.0" {
		t.Errorf("ListExtensions = %v, 期望 extOK 声明", listed)
	}
	if len(mgr.loaded) != 1 {
		t.Errorf("loaded = %d, 期望 1", len(mgr.loaded))
	}
}

// TestExtensionManager_ShutdownAllExtensions 测试逐个 shutdown + 失败仅记录
func TestExtensionManager_ShutdownAllExtensions(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg, _ := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	pc := &memPluginClient{}
	host := &fakeHost{clients: map[string]PluginClientFace{"/tmp/extS": pc}}

	mgr := NewExtensionManager(reg, WithSearchPaths([]string{}), WithPluginHostForManager(host))
	mgr.loaded = []LoadedExtension{{Metadata: plugin.Metadata{ID: "s1"}, Root: "/tmp/extS", client: pc}}

	if err := mgr.ShutdownAllExtensions(context.Background()); err != nil {
		t.Fatalf("ShutdownAllExtensions 失败: %v", err)
	}
	if len(pc.shutdownCalls) != 1 {
		t.Errorf("shutdown 调用数 = %d, 期望 1", len(pc.shutdownCalls))
	}
	if len(mgr.loaded) != 0 {
		t.Errorf("shutdown 后 loaded 应清空, 实际 %d", len(mgr.loaded))
	}
	// 二次调用幂等
	if err := mgr.ShutdownAllExtensions(context.Background()); err != nil {
		t.Errorf("二次 shutdown 应无异常: %v", err)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// declPluginClient 返回固定声明的内存客户端（LoadAllExtensions 完整路径测试用）
type declPluginClient struct {
	memPluginClient
	decls []plugin.Declaration
}

func (d *declPluginClient) RegisterDeclarations(ctx context.Context) ([]plugin.Declaration, error) {
	return d.decls, nil
}
