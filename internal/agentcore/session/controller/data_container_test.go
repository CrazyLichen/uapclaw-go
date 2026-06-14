package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── mock StateAccessor ────────────────────────────

// fakeStateAccessor 测试用的 StateAccessor 实现
type fakeStateAccessor struct {
	state map[string]any
}

func newFakeStateAccessor() *fakeStateAccessor {
	return &fakeStateAccessor{state: make(map[string]any)}
}

func (f *fakeStateAccessor) UpdateState(data map[string]any) {
	for k, v := range data {
		f.state[k] = v
	}
}

func (f *fakeStateAccessor) GetState(key state.StateKey) (any, error) {
	// 简化实现，返回全部状态
	return f.state, nil
}

func (f *fakeStateAccessor) DumpState() map[string]any {
	cp := make(map[string]any, len(f.state))
	for k, v := range f.state {
		cp[k] = v
	}
	return cp
}

func (f *fakeStateAccessor) PreRun(ctx context.Context, inputs ...map[string]any) error {
	return nil
}

// ──────────────────────────── DataContainerFactory 测试 ────────────────────────────

func TestDataContainerFactory_Has(t *testing.T) {
	factory := GetFactory()
	if !factory.Has(DefaultDataContainerType) {
		t.Errorf("默认类型 %q 应已注册", DefaultDataContainerType)
	}
	if factory.Has("nonexistent") {
		t.Errorf("未注册类型不应存在")
	}
}

func TestDataContainerFactory_Create(t *testing.T) {
	factory := GetFactory()
	container, err := factory.Create(DefaultDataContainerType)
	if err != nil {
		t.Fatalf("Create() 返回错误: %v", err)
	}
	if container == nil {
		t.Fatal("Create() 返回 nil")
	}
	asc, ok := container.(*AgentSessionContainer)
	if !ok {
		t.Fatal("Create() 应返回 *AgentSessionContainer")
	}
	if asc.session != nil {
		t.Error("新建的 AgentSessionContainer 的 session 应为 nil")
	}
}

func TestDataContainerFactory_Create_未注册类型(t *testing.T) {
	factory := GetFactory()
	_, err := factory.Create("nonexistent")
	if err == nil {
		t.Errorf("创建未注册类型应返回错误")
	}
}

func TestDataContainerFactory_Load(t *testing.T) {
	factory := GetFactory()
	container, err := factory.Load(DefaultDataContainerType, "a1", "s1", nil)
	if err != nil {
		t.Fatalf("Load() 返回错误: %v", err)
	}
	if container == nil {
		t.Fatal("Load() 返回 nil")
	}
}

func TestDataContainerFactory_Load_未注册类型(t *testing.T) {
	factory := GetFactory()
	_, err := factory.Load("nonexistent", "a1", "s1", nil)
	if err == nil {
		t.Errorf("加载未注册类型应返回错误")
	}
}

func TestDataContainerFactory_ListTypes(t *testing.T) {
	factory := GetFactory()
	types := factory.ListTypes()
	found := false
	for _, t := range types {
		if t == DefaultDataContainerType {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ListTypes() 应包含 %q", DefaultDataContainerType)
	}
}

// ──────────────────────────── AgentSessionContainer 测试 ────────────────────────────

func TestAgentSessionContainer_SetSession(t *testing.T) {
	c := NewAgentSessionContainer()
	if c.session != nil {
		t.Error("新建时 session 应为 nil")
	}
	sa := newFakeStateAccessor()
	c.SetSession(sa)
	if c.session == nil {
		t.Error("SetSession 后 session 不应为 nil")
	}
}

func TestAgentSessionContainer_Update(t *testing.T) {
	c := NewAgentSessionContainer()
	// 无 session 时返回 false
	if c.Update(map[string]any{"key": "val"}) {
		t.Error("无 session 时 Update 应返回 false")
	}
	// 注入 session 后委托
	sa := newFakeStateAccessor()
	c.SetSession(sa)
	if !c.Update(map[string]any{"key": "val"}) {
		t.Error("有 session 时 Update 应返回 true")
	}
	if sa.state["key"] != "val" {
		t.Errorf("state[key] = %v, want %q", sa.state["key"], "val")
	}
}

func TestAgentSessionContainer_Get(t *testing.T) {
	c := NewAgentSessionContainer()
	// 无 session 时返回 nil
	if got := c.Get(nil); got != nil {
		t.Error("无 session 时 Get 应返回 nil")
	}
	// 注入 session 后委托
	sa := newFakeStateAccessor()
	sa.state["foo"] = "bar"
	c.SetSession(sa)
	got := c.Get(nil)
	if got["foo"] != "bar" {
		t.Errorf("Get(nil)[foo] = %v, want %q", got["foo"], "bar")
	}
}

func TestAgentSessionContainer_Get_带StateKey(t *testing.T) {
	c := NewAgentSessionContainer()
	sa := newFakeStateAccessor()
	sa.state["key1"] = "value1"
	c.SetSession(sa)

	// 使用 StateKey 参数
	got := c.Get(state.StringKey("key1"))
	assert.NotNil(t, got, "使用 StateKey 参数应返回非 nil")
}

func TestAgentSessionContainer_Get_非StateKey参数(t *testing.T) {
	c := NewAgentSessionContainer()
	sa := newFakeStateAccessor()
	sa.state["key1"] = "value1"
	c.SetSession(sa)

	// 使用非 StateKey 类型的 key 参数，应走 DumpState 路径
	got := c.Get("not-a-state-key")
	assert.NotNil(t, got, "使用非 StateKey 参数应返回 DumpState 结果")
}

func TestAgentSessionContainer_Dump(t *testing.T) {
	c := NewAgentSessionContainer()
	dump, err := c.Dump()
	if err != nil {
		t.Fatalf("Dump() 返回错误: %v", err)
	}
	m, ok := dump.(map[string]any)
	if !ok {
		t.Fatal("Dump() 应返回 map[string]any")
	}
	if len(m) != 0 {
		t.Errorf("Dump() 应返回空 map，得到 %v", m)
	}
}

// ──────────────────────────── Permission 测试 ────────────────────────────

func TestPermission_Read(t *testing.T) {
	if PermissionRead != 1 {
		t.Errorf("PermissionRead = %d, want 1", PermissionRead)
	}
}

// ──────────────────────────── SharingPolicy 测试 ────────────────────────────

func TestSharingPolicy_默认值(t *testing.T) {
	p := SharingPolicy{}
	if p.Permission != 0 {
		t.Errorf("默认 Permission 应为 0（零值）")
	}
	if p.FieldScopes != nil {
		t.Errorf("默认 FieldScopes 应为 nil（全部字段可访问）")
	}
}

func TestSharingPolicy_有FieldScopes(t *testing.T) {
	p := SharingPolicy{
		Permission:  PermissionRead,
		FieldScopes: map[string]struct{}{"name": {}, "age": {}},
	}
	if p.Permission != PermissionRead {
		t.Errorf("Permission = %d, want %d", p.Permission, PermissionRead)
	}
	if len(p.FieldScopes) != 2 {
		t.Errorf("FieldScopes 长度 = %d, want 2", len(p.FieldScopes))
	}
}
