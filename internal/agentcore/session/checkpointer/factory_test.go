package checkpointer

import (
	"context"
	"testing"
)

// ──────────────────────────── CheckpointerFactory 测试 ────────────────────────────

// TestNewCheckpointerFactory 测试创建工厂
func TestNewCheckpointerFactory(t *testing.T) {
	f := NewCheckpointerFactory()
	if f == nil {
		t.Fatal("NewCheckpointerFactory 返回 nil")
	}
	if f.registry == nil {
		t.Error("registry 未初始化")
	}
	// 验证 in_memory provider 已注册
	if _, ok := f.registry["in_memory"]; !ok {
		t.Error("in_memory provider 未自动注册")
	}
}

// TestCheckpointerFactory_Register 测试注册自定义 Provider
func TestCheckpointerFactory_Register(t *testing.T) {
	f := NewCheckpointerFactory()
	provider := &mockProvider{}
	f.Register("custom", provider)

	if _, ok := f.registry["custom"]; !ok {
		t.Error("自定义 provider 未注册成功")
	}
}

// TestCheckpointerFactory_Create_inMemory 测试创建 in_memory 类型
func TestCheckpointerFactory_Create_inMemory(t *testing.T) {
	f := NewCheckpointerFactory()
	ctx := context.Background()

	cp, err := f.Create(ctx, CheckpointerConfigStruct{Type: "in_memory"})
	if err != nil {
		t.Fatalf("Create 返回错误：%v", err)
	}
	if cp == nil {
		t.Fatal("Create 返回 nil")
	}
}

// TestCheckpointerFactory_Create_未知类型 测试未知类型报错
func TestCheckpointerFactory_Create_未知类型(t *testing.T) {
	f := NewCheckpointerFactory()
	ctx := context.Background()

	_, err := f.Create(ctx, CheckpointerConfigStruct{Type: "unknown"})
	if err == nil {
		t.Error("未知类型应返回错误")
	}
}

// TestCheckpointerFactory_SetDefaultCheckpointer 测试设置默认检查点器
func TestCheckpointerFactory_SetDefaultCheckpointer(t *testing.T) {
	f := NewCheckpointerFactory()
	mockCP := &mockCheckpointer{}
	f.SetDefaultCheckpointer(mockCP)

	if f.defaultCheckpointer != mockCP {
		t.Error("默认检查点器设置失败")
	}
}

// TestCheckpointerFactory_SetCheckpointer 测试设置类型缓存
func TestCheckpointerFactory_SetCheckpointer(t *testing.T) {
	f := NewCheckpointerFactory()
	mockCP := &mockCheckpointer{}
	f.SetCheckpointer("redis", mockCP)

	if f.typeCheckpointers["redis"] != mockCP {
		t.Error("类型缓存设置失败")
	}
}

// TestCheckpointerFactory_GetCheckpointer_类型缓存 测试从类型缓存获取
func TestCheckpointerFactory_GetCheckpointer_类型缓存(t *testing.T) {
	f := NewCheckpointerFactory()
	mockCP := &mockCheckpointer{}
	f.SetCheckpointer("redis", mockCP)

	cp := f.GetCheckpointer("redis")
	if cp != mockCP {
		t.Error("应从类型缓存获取")
	}
}

// TestCheckpointerFactory_GetCheckpointer_inMemory 测试获取 in_memory 类型
func TestCheckpointerFactory_GetCheckpointer_inMemory(t *testing.T) {
	f := NewCheckpointerFactory()

	cp := f.GetCheckpointer("in_memory")
	if cp == nil {
		t.Error("in_memory 类型应返回默认实例")
	}
	if cp != defaultInMemoryCheckpointer {
		t.Error("in_memory 类型应返回 defaultInMemoryCheckpointer")
	}
}

// TestCheckpointerFactory_GetCheckpointer_默认 测试获取默认实例
func TestCheckpointerFactory_GetCheckpointer_默认(t *testing.T) {
	f := NewCheckpointerFactory()
	mockCP := &mockCheckpointer{}
	f.SetDefaultCheckpointer(mockCP)

	cp := f.GetCheckpointer()
	if cp != mockCP {
		t.Error("应返回设置的默认检查点器")
	}
}

// TestCheckpointerFactory_GetCheckpointer_无默认 测试无默认时返回 InMemory
func TestCheckpointerFactory_GetCheckpointer_无默认(t *testing.T) {
	f := NewCheckpointerFactory()

	cp := f.GetCheckpointer()
	if cp != defaultInMemoryCheckpointer {
		t.Error("无默认时应返回 defaultInMemoryCheckpointer")
	}
}

// ──────────────────────────── 全局便捷函数测试 ────────────────────────────

// TestGetCheckpointer 测试全局获取检查点器
func TestGetCheckpointer(t *testing.T) {
	cp := GetCheckpointer()
	if cp == nil {
		t.Error("全局获取检查点器不应返回 nil")
	}
}

// TestGetCheckpointer_inMemory 测试全局获取 in_memory 类型
func TestGetCheckpointer_inMemory(t *testing.T) {
	cp := GetCheckpointer("in_memory")
	if cp == nil {
		t.Error("in_memory 类型不应返回 nil")
	}
}

// TestSetDefaultCheckpointer 测试全局设置默认检查点器
func TestSetDefaultCheckpointer(t *testing.T) {
	original := defaultFactory.defaultCheckpointer
	mockCP := &mockCheckpointer{}
	SetDefaultCheckpointer(mockCP)

	cp := GetCheckpointer()
	if cp != mockCP {
		t.Error("全局设置默认检查点器失败")
	}

	// 恢复
	SetDefaultCheckpointer(original)
}

// TestSetCheckpointer 测试全局设置类型缓存
func TestSetCheckpointer(t *testing.T) {
	mockCP := &mockCheckpointer{}
	SetCheckpointer("custom_test", mockCP)

	cp := GetCheckpointer("custom_test")
	if cp != mockCP {
		t.Error("全局设置类型缓存失败")
	}

	// 清理
	defaultFactory.mu.Lock()
	delete(defaultFactory.typeCheckpointers, "custom_test")
	defaultFactory.mu.Unlock()
}

// TestCreateCheckpointer 测试全局创建检查点器
func TestCreateCheckpointer(t *testing.T) {
	ctx := context.Background()
	cp, err := CreateCheckpointer(ctx, CheckpointerConfigStruct{Type: "in_memory"})
	if err != nil {
		t.Fatalf("CreateCheckpointer 返回错误：%v", err)
	}
	if cp == nil {
		t.Fatal("CreateCheckpointer 返回 nil")
	}
}

// TestRegisterCheckpointer 测试全局注册 Provider
func TestRegisterCheckpointer(t *testing.T) {
	provider := &mockProvider{}
	RegisterCheckpointer("mock_test", provider)

	if _, ok := defaultFactory.registry["mock_test"]; !ok {
		t.Error("全局注册 Provider 失败")
	}

	// 清理
	defaultFactory.mu.Lock()
	delete(defaultFactory.registry, "mock_test")
	defaultFactory.mu.Unlock()
}

// ──────────────────────────── inMemoryProvider 测试 ────────────────────────────

// TestInMemoryProvider_Create 测试 InMemory Provider 创建
func TestInMemoryProvider_Create(t *testing.T) {
	provider := &inMemoryProvider{}
	ctx := context.Background()

	cp, err := provider.Create(ctx, nil)
	if err != nil {
		t.Fatalf("Create 返回错误：%v", err)
	}
	if cp != defaultInMemoryCheckpointer {
		t.Error("应返回 defaultInMemoryCheckpointer")
	}
}

// ──────────────────────────── CheckpointerConfigStruct 测试 ────────────────────────────

// TestCheckpointerConfigStruct 测试配置结构体
func TestCheckpointerConfigStruct(t *testing.T) {
	conf := CheckpointerConfigStruct{
		Type: "in_memory",
		Conf: map[string]any{"key": "value"},
	}
	if conf.Type != "in_memory" {
		t.Errorf("Type 期望 'in_memory'，实际=%s", conf.Type)
	}
	if conf.Conf["key"] != "value" {
		t.Error("Conf 内容不正确")
	}
}

// ──────────────────────────── 测试辅助类型 ────────────────────────────

// mockCheckpointer 用于测试的模拟检查点器
type mockCheckpointer struct{}

func (m *mockCheckpointer) GetThreadID(session CheckpointerSession) string { return "" }
func (m *mockCheckpointer) PreWorkflowExecute(ctx context.Context, session CheckpointerSession, inputs any) error {
	return nil
}
func (m *mockCheckpointer) PostWorkflowExecute(ctx context.Context, session CheckpointerSession, result any, exception error) error {
	return nil
}
func (m *mockCheckpointer) PreAgentExecute(ctx context.Context, session CheckpointerSession, inputs any) error {
	return nil
}
func (m *mockCheckpointer) PreAgentTeamExecute(ctx context.Context, session CheckpointerSession, inputs any) error {
	return nil
}
func (m *mockCheckpointer) InterruptAgentExecute(ctx context.Context, session CheckpointerSession) error {
	return nil
}
func (m *mockCheckpointer) PostAgentExecute(ctx context.Context, session CheckpointerSession) error {
	return nil
}
func (m *mockCheckpointer) PostAgentTeamExecute(ctx context.Context, session CheckpointerSession) error {
	return nil
}
func (m *mockCheckpointer) SessionExists(ctx context.Context, sessionID string) (bool, error) {
	return false, nil
}
func (m *mockCheckpointer) Release(ctx context.Context, sessionID string) error { return nil }
func (m *mockCheckpointer) GraphStore() any                                     { return nil }

// mockProvider 用于测试的模拟 Provider
type mockProvider struct{}

func (p *mockProvider) Create(ctx context.Context, conf map[string]any) (Checkpointer, error) {
	return &mockCheckpointer{}, nil
}
