package controller

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── fakeDataContainer ────────────────────────────

// fakeDataContainer 测试用的 DataContainer 实现
type fakeDataContainer struct {
	data map[string]any
}

func newFakeDataContainer() *fakeDataContainer {
	return &fakeDataContainer{data: make(map[string]any)}
}

func (f *fakeDataContainer) Get(key any) map[string]any {
	cp := make(map[string]any, len(f.data))
	for k, v := range f.data {
		cp[k] = v
	}
	return cp
}

func (f *fakeDataContainer) Update(data map[string]any) bool {
	for k, v := range data {
		f.data[k] = v
	}
	return true
}

func (f *fakeDataContainer) Dump() (any, error) {
	return f.data, nil
}

// ──────────────────────────── 创建实例测试 ────────────────────────────

func TestNewChainSession(t *testing.T) {
	dc := newFakeDataContainer()
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", dc, "/tmp/test")
	if cs.AgentID != "a1" {
		t.Errorf("AgentID = %q, want %q", cs.AgentID, "a1")
	}
	if cs.SessionID != "s1" {
		t.Errorf("SessionID = %q, want %q", cs.SessionID, "s1")
	}
	if len(cs.downstreamPolicies) != 0 {
		t.Errorf("初始下游关系应为空")
	}
}

// ──────────────────────────── 下游关系测试 ────────────────────────────

func TestChainSession_AddDownstream(t *testing.T) {
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), "/tmp")
	cs.AddDownstream("a2", "s2", SharingPolicy{Permission: PermissionRead})
	if !cs.HasDownstream("a2", "s2") {
		t.Error("添加后应存在下游关系")
	}
}

func TestChainSession_RemoveDownstream(t *testing.T) {
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), "/tmp")
	cs.AddDownstream("a2", "s2", SharingPolicy{Permission: PermissionRead})
	cs.RemoveDownstream("a2", "s2")
	if cs.HasDownstream("a2", "s2") {
		t.Error("移除后不应存在下游关系")
	}
}

func TestChainSession_GetDownstreams_返回副本(t *testing.T) {
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), "/tmp")
	cs.AddDownstream("a2", "s2", SharingPolicy{Permission: PermissionRead})
	ds := cs.GetDownstreams()
	delete(ds, [2]string{"a2", "s2"})
	// 原始不受影响
	if !cs.HasDownstream("a2", "s2") {
		t.Error("GetDownstreams 应返回副本，删除副本不应影响原始")
	}
}

func TestChainSession_GetDownstreamPolicy(t *testing.T) {
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), "/tmp")
	cs.AddDownstream("a2", "s2", SharingPolicy{Permission: PermissionRead})

	policy := cs.GetDownstreamPolicy("a2", "s2")
	require.NotNil(t, policy, "已添加的下游关系应返回策略")
	assert.Equal(t, PermissionRead, policy.Permission, "权限应为 PermissionRead")

	// 不存在的下游关系应返回 nil
	policy2 := cs.GetDownstreamPolicy("a3", "s3")
	assert.Nil(t, policy2, "不存在的下游关系应返回 nil")
}

func TestChainSession_RemoveAllDownstreams(t *testing.T) {
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), "/tmp")
	cs.AddDownstream("a2", "s2", SharingPolicy{Permission: PermissionRead})
	cs.AddDownstream("a3", "s3", SharingPolicy{Permission: PermissionRead})
	cs.RemoveAllDownstreams()
	if len(cs.GetDownstreams()) != 0 {
		t.Error("清空后应无下游关系")
	}
}

// ──────────────────────────── CanSee 测试 ────────────────────────────

func TestChainSession_CanSee_自身(t *testing.T) {
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), "/tmp")
	if !cs.CanSee("a1", "s1") {
		t.Error("自身应可见")
	}
}

func TestChainSession_CanSee_有下游(t *testing.T) {
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), "/tmp")
	cs.AddDownstream("a2", "s2", SharingPolicy{Permission: PermissionRead})
	if !cs.CanSee("a2", "s2") {
		t.Error("有下游关系时应可见")
	}
}

func TestChainSession_CanSee_无下游(t *testing.T) {
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), "/tmp")
	if cs.CanSee("a2", "s2") {
		t.Error("无下游关系时不应可见")
	}
}

// ──────────────────────────── 数据访问测试 ────────────────────────────

func TestChainSession_UpdateData(t *testing.T) {
	dc := newFakeDataContainer()
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", dc, "/tmp")
	ok := cs.UpdateData(map[string]any{"key": "val"})
	if !ok {
		t.Error("UpdateData 应返回 true")
	}
	if dc.data["key"] != "val" {
		t.Errorf("data[key] = %v, want %q", dc.data["key"], "val")
	}
}

func TestChainSession_GetData(t *testing.T) {
	dc := newFakeDataContainer()
	dc.data["foo"] = "bar"
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", dc, "/tmp")
	got := cs.GetData()
	if got["foo"] != "bar" {
		t.Errorf("GetData()[foo] = %v, want %q", got["foo"], "bar")
	}
}

func TestChainSession_GetData_无容器(t *testing.T) {
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", nil, "/tmp")
	got := cs.GetData()
	assert.Nil(t, got, "无 DataContainer 时应返回 nil")
}

func TestChainSession_UpdateData_无容器(t *testing.T) {
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", nil, "/tmp")
	ok := cs.UpdateData(map[string]any{"key": "val"})
	assert.False(t, ok, "无 DataContainer 时应返回 false")
}

// ──────────────────────────── 元数据测试 ────────────────────────────

func TestChainSession_ToSessionMeta(t *testing.T) {
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), "/tmp")
	meta := CreateNewSessionMeta("s1", "agent")
	cs.UpdateFromMeta(meta)
	got := cs.ToSessionMeta()
	if got.SessionID != "s1" {
		t.Errorf("SessionID = %q, want %q", got.SessionID, "s1")
	}
	if got.Version != 1 {
		t.Errorf("Version = %d, want 1", got.Version)
	}
}

func TestChainSession_UpdateFromMeta(t *testing.T) {
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), "/tmp")
	meta := CreateNewSessionMeta("s1", "custom")
	cs.UpdateFromMeta(meta)
	if cs.dataContainerType != "custom" {
		t.Errorf("dataContainerType = %q, want %q", cs.dataContainerType, "custom")
	}
}

func TestChainSession_SetIsActive(t *testing.T) {
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), "/tmp")
	cs.SetIsActive(true)
	if !cs.IsActive() {
		t.Error("SetIsActive(true) 后应为活跃")
	}
}

func TestChainSession_SessionKey(t *testing.T) {
	scope := SessionScope{Scope: MainScope{}, Subject: DirectSubject{UserID: "u1"}}
	cs := NewChainSession("a1", scope, "s1", newFakeDataContainer(), "/tmp")
	key := cs.SessionKey()
	assert.Equal(t, "a1", key.AgentID, "AgentID 应匹配")
	assert.Equal(t, scope, key.SessionScope, "SessionScope 应匹配")
}

func TestChainSession_CreatedAt(t *testing.T) {
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), "/tmp")
	// 新创建的 ChainSession 的 createdAt 初始为 0，需要通过 UpdateFromMeta 设置
	meta := CreateNewSessionMeta("s1", "agent")
	cs.UpdateFromMeta(meta)
	createdAt := cs.CreatedAt()
	assert.Greater(t, createdAt, float64(0), "UpdateFromMeta 后 CreatedAt 应大于 0")
}

func TestChainSession_UpdatedAt(t *testing.T) {
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), "/tmp")
	// 新创建的 ChainSession 的 updatedAt 初始为 0，需要通过 UpdateFromMeta 设置
	meta := CreateNewSessionMeta("s1", "agent")
	cs.UpdateFromMeta(meta)
	updatedAt := cs.UpdatedAt()
	assert.Greater(t, updatedAt, float64(0), "UpdateFromMeta 后 UpdatedAt 应大于 0")
}

// ──────────────────────────── 持久化往返测试 ────────────────────────────

func TestChainSession_FlushAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "s1")
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))

	dc := newFakeDataContainer()
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", dc, sessionDir)
	meta := CreateNewSessionMeta("s1", "agent")
	cs.UpdateFromMeta(meta)
	cs.AddDownstream("a2", "s2", SharingPolicy{Permission: PermissionRead})

	// Flush
	if err := cs.Flush(); err != nil {
		t.Fatalf("Flush() 返回错误: %v", err)
	}

	// 验证 state.data 文件存在
	stateFile := filepath.Join(sessionDir, "state.data")
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		t.Fatal("Flush 后 state.data 应存在")
	}

	// 验证 .link 文件存在
	linkFile := filepath.Join(sessionDir, "downstreams", "a2_s2.link")
	if _, err := os.Stat(linkFile); os.IsNotExist(err) {
		t.Fatal("Flush 后 a2_s2.link 应存在")
	}

	// 创建新实例并 Load
	cs2 := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), sessionDir)
	if err := cs2.Load(); err != nil {
		t.Fatalf("Load() 返回错误: %v", err)
	}

	// 验证下游关系恢复
	if !cs2.HasDownstream("a2", "s2") {
		t.Error("Load 后下游关系应恢复")
	}
	// 验证元数据恢复
	if cs2.Version() != 1 {
		t.Errorf("Load 后 Version = %d, want 1", cs2.Version())
	}
}

func TestChainSession_Flush_删除下游关系(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "s1")
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))

	dc := newFakeDataContainer()
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", dc, sessionDir)
	meta := CreateNewSessionMeta("s1", "agent")
	cs.UpdateFromMeta(meta)
	cs.AddDownstream("a2", "s2", SharingPolicy{Permission: PermissionRead})
	require.NoError(t, cs.Flush())

	// 删除下游关系再 Flush
	cs.RemoveDownstream("a2", "s2")
	if err := cs.Flush(); err != nil {
		t.Fatalf("Flush() 返回错误: %v", err)
	}

	// .link 文件应被清理
	linkFile := filepath.Join(sessionDir, "downstreams", "a2_s2.link")
	if _, err := os.Stat(linkFile); !os.IsNotExist(err) {
		t.Error("删除下游关系后 .link 文件应被清理")
	}
}

func TestChainSession_Load_跳过removed标记(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "s1")
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))
	downstreamsDir := filepath.Join(sessionDir, "downstreams")
	require.NoError(t, os.MkdirAll(downstreamsDir, 0o755))

	// 创建一个 removed:true 的 .link 文件
	linkData := map[string]any{
		"permission": map[string]any{"level": 1},
		"removed":    true,
	}
	linkBytes, _ := json.MarshalIndent(linkData, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(downstreamsDir, "a2_s2.link"), linkBytes, 0o644))

	// 也写一个 state.data
	stateData := map[string]any{
		"meta": map[string]any{
			"created_at": 1000.0,
			"updated_at": 2000.0,
			"version":    1,
			"is_active":  true,
		},
		"data": map[string]any{},
	}
	stateBytes, _ := json.MarshalIndent(stateData, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "state.data"), stateBytes, 0o644))

	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), sessionDir)
	if err := cs.Load(); err != nil {
		t.Fatalf("Load() 返回错误: %v", err)
	}

	// removed 的下游关系不应被加载
	if cs.HasDownstream("a2", "s2") {
		t.Error("标记 removed 的下游关系不应被加载")
	}
}

func TestChainSession_Load_目录不存在(t *testing.T) {
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), "/nonexistent/path")
	err := cs.Load()
	assert.NoError(t, err, "目录不存在时 Load 应返回 nil")
}

func TestChainSession_Load_下游关系带FieldScopes(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "s1")
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))
	downstreamsDir := filepath.Join(sessionDir, "downstreams")
	require.NoError(t, os.MkdirAll(downstreamsDir, 0o755))

	// 创建一个带 field_scopes 的 .link 文件
	linkData := map[string]any{
		"permission": map[string]any{
			"level":        1,
			"field_scopes": []any{"name", "age"},
		},
	}
	linkBytes, _ := json.MarshalIndent(linkData, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(downstreamsDir, "a2_s2.link"), linkBytes, 0o644))

	// 写 state.data
	stateData := map[string]any{
		"meta": map[string]any{
			"created_at": 1000.0,
			"updated_at": 2000.0,
			"version":    1,
			"is_active":  true,
		},
		"data": map[string]any{},
	}
	stateBytes, _ := json.MarshalIndent(stateData, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "state.data"), stateBytes, 0o644))

	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), sessionDir)
	require.NoError(t, cs.Load())

	policy := cs.GetDownstreamPolicy("a2", "s2")
	require.NotNil(t, policy, "应加载下游关系")
	assert.NotNil(t, policy.FieldScopes, "FieldScopes 应不为 nil")
	assert.Len(t, policy.FieldScopes, 2, "应有 2 个字段范围")
}

func TestChainSession_Flush_DataContainerDump失败(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "s1")
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))

	// 使用一个 Dump 失败的容器
	failingDC := &failingDumpContainer{}
	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", failingDC, sessionDir)
	cs.UpdateFromMeta(CreateNewSessionMeta("s1", "agent"))

	err := cs.Flush()
	require.Error(t, err, "Dump 失败时应返回错误")
}

func TestChainSession_Load_stateData损坏(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "s1")
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))

	// 写损坏的 state.data
	os.WriteFile(filepath.Join(sessionDir, "state.data"), []byte("invalid json {{{"), 0o644)

	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), sessionDir)
	err := cs.Load()
	require.Error(t, err, "损坏的 state.data 应返回错误")
}

func TestChainSession_Load_无下划线link文件(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "s1")
	require.NoError(t, os.MkdirAll(sessionDir, 0o755))
	downstreamsDir := filepath.Join(sessionDir, "downstreams")
	require.NoError(t, os.MkdirAll(downstreamsDir, 0o755))

	// 创建一个没有下划线的 .link 文件（应被跳过）
	linkData := map[string]any{
		"permission": map[string]any{"level": 1},
	}
	linkBytes, _ := json.MarshalIndent(linkData, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(downstreamsDir, "nounderline.link"), linkBytes, 0o644))

	// 写 state.data
	stateData := map[string]any{
		"meta": map[string]any{
			"created_at": 1000.0,
			"updated_at": 2000.0,
			"version":    1,
			"is_active":  true,
		},
		"data": map[string]any{},
	}
	stateBytes, _ := json.MarshalIndent(stateData, "", "  ")
	require.NoError(t, os.WriteFile(filepath.Join(sessionDir, "state.data"), stateBytes, 0o644))

	cs := NewChainSession("a1", SessionScope{Scope: MainScope{}}, "s1", newFakeDataContainer(), sessionDir)
	require.NoError(t, cs.Load())
	// 没有下划线的 link 文件应被跳过，不应有下游关系
	assert.Equal(t, 0, len(cs.GetDownstreams()), "无效 link 文件应被跳过")
}

// ──────────────────────────── 辅助类型 ────────────────────────────

// failingDumpContainer 测试用的 DataContainer 实现，Dump 总是失败
type failingDumpContainer struct{}

func (f *failingDumpContainer) Get(key any) map[string]any { return nil }
func (f *failingDumpContainer) Update(data map[string]any) bool { return false }
func (f *failingDumpContainer) Dump() (any, error) {
	return nil, assert.AnError
}
