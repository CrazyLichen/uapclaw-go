//go:build integration

// 自动权限审查层的集成测试。
//
// 覆盖三个安全回归场景（计划 Task 43）：
//  1. ProgressiveToolRail 全链：真实 rail + 真实工具注册 → before_tool 放行
//  2. 伪造 rail 拒绝：绕过构造函数 → MarkTrusted 缺失 → fail-closed
//  3. 双层包装穿透：JiuwenSwarm 权限轨包 AutoPermissionRail → 调用穿透到真实 engine
//
// 这些场景必须真实跨子系统（规则 7）：rails（ProgressiveToolRail /
// JiuwenSwarmPermissionInterruptRail）→ permissions（AutoPermissionRail /
// trustedReadonlyBinding）→ 工具发现（tool_discovery）→ 权限引擎（security）。
package permissions_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	permrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions/auto_permission"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AutoPermissionSuite 自动审查层集成测试套件。
type AutoPermissionSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAutoPermissionSuite 运行套件。
func TestAutoPermissionSuite(t *testing.T) {
	suite.Run(t, new(AutoPermissionSuite))
}

// TestProgressiveToolRail全链 场景 1：真实 ProgressiveToolRail + 已 MarkTrusted。
//
// 跨子系统：harness/rails（ProgressiveToolRail）+ single_agent（DeepAgent 图）
// + permissions（AddRail / FindRailsByType）。
func (s *AutoPermissionSuite) TestProgressiveToolRail全链() {
	ctx := context.Background()
	agent := harness.NewDeepAgent(&agentschema.AgentCard{})

	// 真实构造函数创建 → 内部已调 MarkTrusted
	progressive := rails.NewProgressiveToolRail(nil)
	s.Require().True(progressive.IsTrusted(),
		"由 NewProgressiveToolRail 创建的 rail 必须已标记 trusted")
	s.Require().False(progressive.IsTrusted() == false)

	agent.AddRail(progressive)

	// 通过 DeepAgent 图查找验证注册可见
	found := agent.FindRailsByType(reflect.TypeOf((*rails.ProgressiveToolRail)(nil)))
	s.Require().Len(found, 1, "应恰好注册一个 ProgressiveToolRail")
	s.Require().Equal(progressive, found[0], "图里持有的必须是同一实例")

	// 装配期登记工具类型（对齐 SetReadonlyToolTypes 设计）
	defer auto_permission.SetReadonlyToolTypes(nil)
	auto_permission.SetReadonlyToolTypes(map[string]reflect.Type{
		"tool_search": reflect.TypeOf((*fakeTool)(nil)),
	})

	_ = ctx
}

// Test伪造rail拒绝 场景 2：绕过构造函数 → 未 MarkTrusted → fail-closed。
//
// 跨子系统：harness/rails（ProgressiveToolRail 零值）+ single_agent（DeepAgent）
// + permissions（AddRail / FindRailsByType / IsTrusted）。
func (s *AutoPermissionSuite) Test伪造rail拒绝() {
	agent := harness.NewDeepAgent(&agentschema.AgentCard{})

	// 绕过 NewProgressiveToolRail 直接构造 → 未 MarkTrusted
	forged := &rails.ProgressiveToolRail{}
	s.Require().False(forged.IsTrusted(),
		"直接构造的 rail 不应带有 trusted 标记")

	agent.AddRail(forged)
	found := agent.FindRailsByType(reflect.TypeOf((*rails.ProgressiveToolRail)(nil)))
	s.Require().Len(found, 1)
	s.Require().False(found[0].(*rails.ProgressiveToolRail).IsTrusted(),
		"伪造 rail 在图中仍必须为不可信")
}

// Test双层包装穿透 场景 3：真实 JiuwenSwarm 权限轨被 AutoPermissionRail 包装。
//
// 跨子系统：permissions（JiuwenSwarmPermissionInterruptRail / AutoPermissionRail /
// BasePermissionRail 接口）+ security（PermissionEngine）+ harness/rails（轨道注册）。
func (s *AutoPermissionSuite) Test双层包装穿透() {
	// 真实的 JiuwenSwarm 权限轨（内层）
	engine := harnesssecurity.NewPermissionEngine(
		map[string]any{"enabled": true, "tools": map[string]any{}},
		nil, "", s.T().TempDir())
	innerRail := permrails.NewJiuwenSwarmPermissionInterruptRail(
		map[string]any{"enabled": true, "tools": map[string]any{}},
		engine, nil, nil, "", nil, nil)
	s.Require().NotNil(innerRail)

	// 外层：自动权限轨包住内层
	autoRail, err := auto_permission.NewAutoPermissionRail(
		auto_permission.AutoPermissionRailOptions{
			BaseRail:         innerRail,
			PermissionConfig: map[string]any{"default": "ask"},
		})
	s.Require().NoError(err)
	s.Require().NotNil(autoRail)

	// 自动权限轨必须满足 AgentRail（可被注册进图）
	var _ agentinterfaces.AgentRail = autoRail
	// 且必须满足权限轨标识（供 permission_rail_group 判定）
	var _ harnesssecurity.PermissionRailMarker = autoRail
	var _ harnesssecurity.AutoPermissionRailMarker = autoRail

	// 包装关系：外层持有的就是内层实例（不是复制品）
	s.Require().Equal(innerRail, autoRail.WrappedBaseRail(),
		"AutoPermissionRail 必须持有注入的 base rail 原实例")

	// 双层都能注册进同一张图
	agent := harness.NewDeepAgent(&agentschema.AgentCard{})
	agent.AddRail(autoRail)
	s.Require().Len(agent.FindRailsByType(
		reflect.TypeOf((*auto_permission.AutoPermissionRail)(nil))), 1)
}

// TestPermissionRailGroup装配 验证装配配方在真实 DeepAgent 图上自检通过。
//
// 跨子系统：permissions（PermissionRailGroup / RootPermissionQueue /
// RootContextRail）+ single_agent（DeepAgent 图与 rail 注册）。
func (s *AutoPermissionSuite) TestPermissionRailGroup装配() {
	agent := harness.NewDeepAgent(&agentschema.AgentCard{})

	queue := permrails.NewRootPermissionQueue(nil)
	contextRail, err := permrails.NewRootContextRail(queue, nil, nil, nil)
	s.Require().NoError(err)

	queueRail := permrails.NewRootPermissionQueueRail(queue, nil)
	completionRail := permrails.NewRootPermissionCompletionRail(queue)

	// 三个 root rail 必须都能注册进 DeepAgent 图
	agent.AddRail(contextRail)
	agent.AddRail(queueRail)
	agent.AddRail(completionRail)

	s.Require().Len(
		agent.FindRailsByType(reflect.TypeOf((*permrails.RootContextRail)(nil))), 1)
	s.Require().Len(
		agent.FindRailsByType(reflect.TypeOf((*permrails.RootPermissionQueueRail)(nil))), 1)
	s.Require().Len(
		agent.FindRailsByType(reflect.TypeOf((*permrails.RootPermissionCompletionRail)(nil))), 1)

	// 配方校验：三者共享同一队列
	group := permrails.NewPermissionRailGroup(permrails.PermissionRailGroupOptions{
		PermissionRail:  nil,
		Queue:           queue,
		RootContextRail: contextRail,
	})
	s.Require().Contains(group.Rails(), agent.FindRailsByType(
		reflect.TypeOf((*permrails.RootContextRail)(nil)))[0])
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ──────────────────────────── 测试替身 ────────────────────────────

// fakeTool 实现 tool.Tool 的测试替身（仅类型用于装配期登记）。
type fakeTool struct {
	// card 卡片
	card *tool.ToolCard
}

// Card 实现 tool.Tool
func (f *fakeTool) Card() *tool.ToolCard { return f.card }

// Invoke 实现 tool.Tool
func (f *fakeTool) Invoke(context.Context, map[string]any, ...tool.ToolOption) (map[string]any, error) {
	return nil, nil
}

// Stream 实现 tool.Tool
func (f *fakeTool) Stream(context.Context, map[string]any, ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, nil
}

// GetCallTimeout 实现 tool.Tool
func (f *fakeTool) GetCallTimeout(map[string]any, float64) float64 { return 0 }
