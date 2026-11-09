package ability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
)

// demoInput 最小工具入参（tool.NewTool 要求 struct 而非 map）。
type demoInput struct {
	// Value 任意值
	Value string
}

// newDemoTool 造一个带指定 id 的最小工具实例。
func newDemoTool(t *testing.T, id, name string) tool.Tool {
	t.Helper()
	card := tool.NewToolCardWithID(id, name, "测试工具", nil, nil)
	tl, err := tool.NewTool(
		func(_ context.Context, in demoInput, _ ...tool.ToolOption) (map[string]any, error) {
			return map[string]any{"value": in.Value}, nil
		},
		tool.WithToolCard(card),
	)
	require.NoError(t, err)
	return tl
}

// newAddAbilityFixture 造一个带真实 ResourceMgr 的 AbilityManager。
func newAddAbilityFixture(ownerID string) (*AbilityManager, *resources_manager.ResourceMgr) {
	rm := resources_manager.NewResourceMgr()
	return NewAbilityManagerWithOwner(rm, ownerID), rm
}

// TestAddAbility_有状态限定ID且资源管理器同键 两表 id 必须一致（F-03 的核心契约）。
//
// 上游 add_ability 存在的唯一理由：旧的「qualify 后缀 + add_tool(refresh) + add」
// 三件套里，resource_mgr 用限定前的 id 注册、ability_manager 的 card 随后被改写，
// 导致 teardown_tools 按限定 id 回收时对不上。
func TestAddAbility_有状态限定ID且资源管理器同键(t *testing.T) {
	am, rm := newAddAbilityFixture("agent-1")

	tl := newDemoTool(t, "demo_tool", "demo_tool")
	card := tl.Card()

	res := am.AddAbility(card, tl)
	require.True(t, res.Added)

	// ability_manager 侧：card id 已限定
	assert.Equal(t, "demo_tool_agent-1", card.ID, "有状态工具应限定为 {name}_{owner}")

	// resource_manager 侧：同键可查（证明两表一致）
	got, err := rm.GetTool([]string{"demo_tool_agent-1"})
	require.NoError(t, err)
	assert.Len(t, got, 1, "资源管理器应能用限定 id 取到实例")
}

// TestAddAbility_无状态保留裸ID且幂等 共享单例不限定、重复 add 不覆盖。
func TestAddAbility_无状态保留裸ID且幂等(t *testing.T) {
	am, rm := newAddAbilityFixture("agent-1")

	tl := newDemoTool(t, "shared_tool", "shared_tool")
	card := tl.Card()
	card.Stateless = true

	require.True(t, am.AddAbility(card, tl).Added)
	assert.Equal(t, "shared_tool", card.ID, "无状态工具应保留裸 id")

	// 第二次：skip_if_exists 幂等（不报错、不覆盖）
	tl2 := newDemoTool(t, "shared_tool", "shared_tool")
	card2 := tl2.Card()
	card2.Stateless = true
	// 第二次走 Add 会因重名被拒（duplicate_tool），这是能力表的既有语义
	assert.False(t, am.AddAbility(card2, tl2).Added, "同名单例不应重复加入能力表")

	// 资源管理器里仍是第一次那个实例，且只有一个
	got, err := rm.GetTool([]string{"shared_tool"})
	require.NoError(t, err)
	assert.Len(t, got, 1, "skip_if_exists 不应产生重复注册")
}

// TestAddAbility_无owner时不限定 空 owner 退化为裸 id（共享语义）。
func TestAddAbility_无owner时不限定(t *testing.T) {
	am, _ := newAddAbilityFixture("")

	tl := newDemoTool(t, "demo_tool", "demo_tool")
	card := tl.Card()

	require.True(t, am.AddAbility(card, tl).Added)
	assert.Equal(t, "demo_tool", card.ID, "无 owner 不应限定 id")
}

// TestAddAbility_Teardown回收限定ID 限定过的工具能被 TeardownTools 摘干净。
func TestAddAbility_Teardown回收限定ID(t *testing.T) {
	am, rm := newAddAbilityFixture("agent-1")

	tl := newDemoTool(t, "demo_tool", "demo_tool")
	require.True(t, am.AddAbility(tl.Card(), tl).Added)

	am.TeardownTools()

	assert.Nil(t, am.Get("demo_tool"), "能力表应已摘除")
	got, err := rm.GetTool([]string{"demo_tool_agent-1"})
	require.NoError(t, err)
	assert.Empty(t, got, "资源管理器应已摘除限定 id")
}

// TestAddAbility_nil卡安全 传 nil 不 panic 且返回失败。
func TestAddAbility_nil卡安全(t *testing.T) {
	am, _ := newAddAbilityFixture("agent-1")
	res := am.AddAbility(nil, nil)
	assert.False(t, res.Added)
}

// TestAddAbility_资源管理器为nil仍注册能力 降级路径不 panic。
func TestAddAbility_资源管理器为nil仍注册能力(t *testing.T) {
	am := NewAbilityManagerWithOwner(nil, "agent-1")
	tl := newDemoTool(t, "demo_tool", "demo_tool")

	res := am.AddAbility(tl.Card(), tl)
	assert.True(t, res.Added, "无资源管理器时能力表仍应注册")
	assert.Equal(t, "demo_tool_agent-1", tl.Card().ID, "id 仍应限定")
}

// TestAddAbility_资源注册失败仍入能力表 资源管理器报错时记日志但不阻断能力注册。
//
// 用仅含空白字符的 id 触发 innerValidateResourceID 失败（AddAbility 内部
// 记 Debug 日志后继续走 am.Add），验证降级路径不会让整个注册中断。
func TestAddAbility_资源注册失败仍入能力表(t *testing.T) {
	am, _ := newAddAbilityFixture("")
	tl := newDemoTool(t, "   ", "blank_id_tool")

	res := am.AddAbility(tl.Card(), tl)
	assert.True(t, res.Added, "资源注册失败不应阻断能力表注册")
}
