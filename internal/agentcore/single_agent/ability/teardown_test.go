package ability_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 辅助 ────────────────────────────

// newCard 构造测试用工具卡（id 与 name 相同）。
func newCard(name string, stateless bool) *tool.ToolCard {
	card := &tool.ToolCard{Stateless: stateless}
	card.Name = name
	card.ID = name
	return card
}

// newTestManager 构造带 owner 的 AbilityManager。
func newTestManager(t *testing.T, ownerID string) *ability.AbilityManager {
	t.Helper()
	rm := resources_manager.NewResourceMgr()
	return ability.NewAbilityManagerWithOwner(rm, ownerID)
}

// ──────────────────────────── qualify_tool_id ────────────────────────────

// TestQualifyToolID_改写为name_owner 有状态工具 id 改写为 {name}_{owner}。
//
// Python: AbilityManager.qualify_tool_id (ability_manager.py:793-804)
// 「有状态工具限定为 f"{card.name}_{owner_id}"，使每个 agent 拥有独立注册」。
func TestQualifyToolID_改写为name_owner(t *testing.T) {
	card := newCard("bash", false)

	got := ability.QualifyToolID(card, "alice")

	assert.Equal(t, "bash_alice", got.ID)
}

// TestQualifyToolID_幂等 已限定过的卡片再限定一次结果不变（从 Name 派生，不叠加后缀）。
func TestQualifyToolID_幂等(t *testing.T) {
	card := newCard("bash", false)
	card.ID = "bash_alice"

	got := ability.QualifyToolID(card, "alice")

	assert.Equal(t, "bash_alice", got.ID, "幂等：从 name 派生，不叠加")
}

// TestQualifyToolID_无状态保持裸id 无状态共享单例保持裸 id。
//
// Python: `if card.stateless or not owner_id: return card.id or card.name`（:802-803）
func TestQualifyToolID_无状态保持裸id(t *testing.T) {
	card := newCard("shared", true)

	got := ability.QualifyToolID(card, "alice")

	assert.Equal(t, "shared", got.ID, "无状态工具保持裸 id")
}

// TestQualifyToolID_owner为空保持裸id 无 owner 时不做限定。
func TestQualifyToolID_owner为空保持裸id(t *testing.T) {
	card := newCard("bash", false)

	got := ability.QualifyToolID(card, "")

	assert.Equal(t, "bash", got.ID, "无 owner 时不限定")
}

// TestQualifyToolID_id为空回退name 上游细节：card.id 为空时回退到 card.name。
//
// Python: `return card.id or card.name`（:803）——id 为假值时返回 name。
func TestQualifyToolID_id为空回退name(t *testing.T) {
	card := &tool.ToolCard{Stateless: true}
	card.Name = "only_name"
	card.ID = ""

	got := ability.QualifyToolID(card, "")

	assert.Equal(t, "only_name", got.ID, "id 为空时回退到 name")
}

// TestQualifyToolID_nil安全 nil 卡片不 panic。
func TestQualifyToolID_nil安全(t *testing.T) {
	assert.Nil(t, ability.QualifyToolID(nil, "alice"))
}

// ──────────────────────────── Add 时的限定 ────────────────────────────

// TestAdd_有状态工具id被限定 注册有状态工具时自动限定 id。
//
// Python: add_ability 里 `if self._owner_id: card.id = self.qualify_tool_id(...)`（:837-838）
func TestAdd_有状态工具id被限定(t *testing.T) {
	mgr := newTestManager(t, "alice")
	card := newCard("bash", false)

	result := mgr.Add(card)

	require.True(t, result.Added)
	stored := mgr.Get("bash")
	require.NotNil(t, stored)
	card, ok := stored.(*tool.ToolCard)
	require.True(t, ok, "Get 应返回 *ToolCard")
	assert.Equal(t, "bash_alice", card.ID, "有状态工具注册后 id 应被限定")
}

// TestAdd_无状态工具id保持裸 无状态共享单例保持裸 id。
func TestAdd_无状态工具id保持裸(t *testing.T) {
	mgr := newTestManager(t, "alice")
	card := newCard("shared_tool", true)

	result := mgr.Add(card)

	require.True(t, result.Added)
	stored := mgr.Get("shared_tool")
	require.NotNil(t, stored)
	card, ok := stored.(*tool.ToolCard)
	require.True(t, ok)
	assert.Equal(t, "shared_tool", card.ID, "无状态工具保持裸 id")
}

// TestAdd_无owner不限定 owner 为空时不做限定（对齐上游 not owner_id 分支）。
func TestAdd_无owner不限定(t *testing.T) {
	rm := resources_manager.NewResourceMgr()
	mgr := ability.NewAbilityManager(rm)
	card := newCard("bash", false)

	mgr.Add(card)

	stored := mgr.Get("bash")
	require.NotNil(t, stored)
	card, ok := stored.(*tool.ToolCard)
	require.True(t, ok)
	assert.Equal(t, "bash", card.ID, "无 owner 时不限定")
}

// ──────────────────────────── TeardownTools ────────────────────────────

// TestTeardownTools_只删owner限定有状态工具 删掉本 owner 的有状态工具。
//
// Python: teardown_tools (ability_manager.py:860-883)
func TestTeardownTools_只删owner限定有状态工具(t *testing.T) {
	mgr := newTestManager(t, "alice")
	mgr.Add(newCard("bash", false))
	mgr.Add(newCard("read_file", false))

	require.NotNil(t, mgr.Get("bash"))

	mgr.TeardownTools()

	assert.Nil(t, mgr.Get("bash"), "有状态工具应被摘除")
	assert.Nil(t, mgr.Get("read_file"), "有状态工具应被摘除")
}

// TestTeardownTools_无状态共享单例保留 无状态工具不受影响。
//
// Python: `if card.stateless or ...: continue`（:879）
func TestTeardownTools_无状态共享单例保留(t *testing.T) {
	mgr := newTestManager(t, "alice")
	mgr.Add(newCard("shared", true))
	mgr.Add(newCard("bash", false))

	mgr.TeardownTools()

	assert.NotNil(t, mgr.Get("shared"), "无状态共享单例必须保留（其他 agent 还在用）")
	assert.Nil(t, mgr.Get("bash"))
}

// TestTeardownTools_别的owner的工具不动 关键隔离性：不能误删他人工具。
//
// Python 注释：「Only tools this manager qualified with its own owner id
// (card.id == f"{name}_{owner_id}") are removed」。
func TestTeardownTools_别的owner的工具不动(t *testing.T) {
	rm := resources_manager.NewResourceMgr()
	aliceMgr := ability.NewAbilityManagerWithOwner(rm, "alice")
	bobMgr := ability.NewAbilityManagerWithOwner(rm, "bob")

	aliceMgr.Add(newCard("bash", false))
	bobMgr.Add(newCard("bash", false))

	aliceCard, ok := aliceMgr.Get("bash").(*tool.ToolCard)
	require.True(t, ok)
	bobCard, ok := bobMgr.Get("bash").(*tool.ToolCard)
	require.True(t, ok)
	assert.Equal(t, "bash_alice", aliceCard.ID)
	assert.Equal(t, "bash_bob", bobCard.ID)

	aliceMgr.TeardownTools()

	assert.Nil(t, aliceMgr.Get("bash"), "alice 的应被删")
	assert.NotNil(t, bobMgr.Get("bash"), "bob 的不得被删")
}

// TestTeardownTools_owner为空时noop 无 owner 时不做任何事。
//
// Python: `if not self._owner_id: return`（:876-877）
func TestTeardownTools_owner为空时noop(t *testing.T) {
	rm := resources_manager.NewResourceMgr()
	mgr := ability.NewAbilityManager(rm)
	mgr.Add(newCard("bash", false))

	mgr.TeardownTools()

	assert.NotNil(t, mgr.Get("bash"), "无 ownerID 时不删（不知道哪些是自己的）")
}

// TestTeardownTools_重复调用安全 多次调用不 panic。
func TestTeardownTools_重复调用安全(t *testing.T) {
	mgr := newTestManager(t, "alice")
	mgr.Add(newCard("bash", false))

	mgr.TeardownTools()
	mgr.TeardownTools()
	mgr.TeardownTools()

	assert.Nil(t, mgr.Get("bash"))
}

// TestTeardownTools_同步从ResourceMgr摘除 资源管理器里的实例也要删。
//
// Python: `Runner.resource_mgr.remove_tool(card.id)`（:882）
func TestTeardownTools_同步从ResourceMgr摘除(t *testing.T) {
	rm := resources_manager.NewResourceMgr()
	mgr := ability.NewAbilityManagerWithOwner(rm, "alice")
	mgr.Add(newCard("bash", false))

	// 注册一个真实工具实例到资源管理器（id 与 Add 后限定的 id 一致）
	resourceCard := newCard("bash", false)
	resourceCard.ID = "bash_alice"
	inst, err := tool.NewTool(
		func(ctx context.Context, in map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
			return nil, nil
		},
		tool.WithToolCard(resourceCard),
		// 显式给空 inputParams：NewTool 默认会用反射从泛型 I 提取参数定义，
		// 而 map[string]any 不是 struct 会报「期望 struct 类型」。
		tool.WithToolInputParams([]*commonschema.Param{}),
	)
	require.NoError(t, err)
	require.NoError(t, rm.AddTool(inst))

	before, err := rm.GetTool([]string{"bash_alice"})
	require.NoError(t, err)
	require.NotEmpty(t, before, "前置：实例已注册")

	mgr.TeardownTools()

	after, err := rm.GetTool([]string{"bash_alice"})
	require.NoError(t, err)
	assert.Empty(t, after, "资源管理器里的实例应被摘除")
}
