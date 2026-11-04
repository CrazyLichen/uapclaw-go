//go:build integration

package adapter_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DeepAdapterReloadSuite 测试 DeepAdapter 热重载（agent.reload_config）之后的
// DeepAgent 装配完整性。
//
// 覆盖：
//   - 热重载后 deepConfig 仍带 workspace / sys_operation
//   - 热重载后 pending rail 重建出的 fs 工具持有可用的 sys_operation（read_file 真读）
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/interface_deep.py
// （reload_agent_config → _make_deep_agent_config）
type DeepAdapterReloadSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境（数据目录 / MockLLM provider / env 备份）
	env *testhelpers.AdapterTestEnv
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestDeepAdapterReloadSuite(t *testing.T) {
	suite.Run(t, new(DeepAdapterReloadSuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每个测试方法前构建隔离环境。
func (s *DeepAdapterReloadSuite) SetupTest() {
	s.env = testhelpers.NewAdapterTestEnv(s.T(), s.MockLLM, "mock_deep_reload_itest")
}

// newDeepAdapter 构造 DeepAdapter 并执行 CreateInstance 装配链，挂 Cleanup 收尾。
func (s *DeepAdapterReloadSuite) newDeepAdapter() *adapter.DeepAdapter {
	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })
	return d
}

// toolByName 从 AbilityManager 取卡片、再从 ResourceMgr 取工具实例。
func (s *DeepAdapterReloadSuite) toolByName(agent *harness.DeepAgent, name string) tool.Tool {
	s.T().Helper()
	am := agent.AbilityManager()
	s.Require().NotNil(am, "AbilityManager 不应为 nil")

	raw := am.Get(name)
	card, ok := raw.(*tool.ToolCard)
	s.Require().True(ok, "工具 %s 应已注册到 AbilityManager，实际 %T", name, raw)

	tools, err := runner.GetResourceMgr().GetTool([]string{card.ID})
	s.Require().NoError(err)
	s.Require().NotEmpty(tools, "ResourceMgr 应能取到工具实例: %s", card.ID)
	return tools[0]
}

// TestReloadAgentConfig_重载后read_file仍可真实读取 回归：热重载配置若漏传
// workspace / sys_operation（Python _make_deep_agent_config 两者都传），
// 下一轮 ensureInitialized 会把 nil 注入所有 pending rail ——
// SysOperationRail 重建出的 fs/shell 工具持有 nil op，read_file 执行即
// nil 解引用 panic；panic 逃出 Rail 包装层后 AFTER_TOOL_CALL 不触发，
// 前端工具卡永久停在「执行中」。
func (s *DeepAdapterReloadSuite) TestReloadAgentConfig_重载后read_file仍可真实读取() {
	s.env.WriteConfigYAML(s.T(), "")

	d := s.newDeepAdapter()
	inst := d.Instance()
	s.Require().NotNil(inst)

	// 工作区文件（workspaceDir = {DATA_DIR}/agent/workspace）
	wsFile := filepath.Join(s.env.DataDir, "agent", "workspace", "reload_probe.txt")
	s.Require().NoError(os.WriteFile(wsFile, []byte("hello-after-reload"), 0o644))

	// 热重载：等价于 Web 设置页保存配置 → gateway 发 agent.reload_config
	s.Require().NoError(d.ReloadAgentConfig(s.Ctx, nil, nil))

	// 断言 1：重载后的 deepConfig 仍带 workspace / sys_operation
	cfg := inst.DeepConfig()
	s.Require().NotNil(cfg, "重载后 DeepConfig 不应为 nil")
	s.NotNil(cfg.Workspace, "重载后 workspace 不应为 nil（否则 pending rail 会被注入 nil）")
	s.NotNil(cfg.SysOperation, "重载后 sys_operation 不应为 nil（否则 fs 工具持有 nil op）")

	// 断言 2：消费 pending（ConfigureDeepConfig 已把 initialized 置 false）
	initCtx, err := inst.EnsureInitialized(s.Ctx)
	s.Require().NoError(err)

	readTool := s.toolByName(inst, "read_file")
	res, err := readTool.Invoke(initCtx, map[string]any{"file_path": wsFile})
	s.Require().NoError(err)
	s.Equal(true, res["success"], "重载后 read_file 应成功，实际: %+v", res)
	data, ok := res["data"].(map[string]any)
	s.Require().True(ok, "data 字段应为 map，实际 %T", res["data"])
	content, _ := data["content"].(string)
	s.Contains(content, "hello-after-reload", "应读到文件真实内容")
}
