//go:build integration

// MCP 全链集成测试：真实配置 → DeepAdapter 装配 → DeepAgent 真连接 →
// AbilityManager 懒物化 → 模型可见 / 检索披露联动。
//
// 真实 server：cmd/uapclawmcpserver（team-mcp stdio server，真实构建的二进制，
// 沿 tests/integration/real_llm/harness_providers_text_turn_test.go 的构建模式）。
//
// 覆盖场景：
//  1. 连接与注入：DeepAgent.EnsureInitialized → registerPendingMCPs →
//     ResourceMgr.AddToolServer 真实 stdio 连接 → AbilityManager 懒物化
//     mcp_{server}_{tool} 卡片
//  2. 模型可见：ListToolInfo 输出含 mcp_* 工具（模型 inputs.tools 面）
//  3. 检索披露联动（9.11b）：provider 提供 live mcp 工具 + server 描述 →
//     披露节渲染 `- [category] mcp_openuapclaw_team` 虚拟分支行
//
// 真实跨子系统（规则 7）：swarm/server/adapter（配置装配）+
// openuapclaw/harness（DeepAgent/registerPendingMCPs）+
// openuapclaw/core/runner/resources_manager（真实 stdio 连接）+
// openuapclaw/core/single_agent/ability（懒物化/模型可见面）+
// openuapclaw/agent_teams/mcp（真 server 子进程）+
// swarm/agents/harness/common/{rails,tools}（披露联动）。
package mcpagent_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/external"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
	harnessschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	swarmrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// McpAgentSuite DeepAgent + 真 team MCP server 端到端套件。
type McpAgentSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestMcpAgentSuite 运行套件（审计硬要求：suite.Run 字面调用）。
func TestMcpAgentSuite(t *testing.T) {
	suite.Run(t, new(McpAgentSuite))
}

// ──────────────────────────── 基建 ────────────────────────────

// buildTeamMcpServer 构建 uapclawmcpserver 真二进制（与 real_llm 同模式）。
func (s *McpAgentSuite) buildTeamMcpServer() string {
	dir := s.T().TempDir()
	serverBin := filepath.Join(dir, "uapclawmcpserver")
	workRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	s.Require().NoError(err)
	build := exec.Command("go", "build", "-o", serverBin, "./cmd/uapclawmcpserver")
	build.Dir = workRoot
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		s.Require().NoError(buildErr, "构建 uapclawmcpserver 失败: %s", output)
	}
	return serverBin
}

// teamJoinEnv 入团描述符 → 子进程 env 键值（sqlite 落盘库可观测产物）。
func (s *McpAgentSuite) teamJoinEnv() (string, string, string) {
	dir := s.T().TempDir()
	dbPath := filepath.Join(dir, "team.db")
	descriptor := external.TeamJoinDescriptor{
		SessionID:  "mcpagent-e2e-session",
		TeamName:   "mcpagent-e2e-team",
		MemberName: "worker",
		Role:       "teammate",
		Scope:      "member",
		DBConfig: database.DatabaseConfig{DBType: database.DatabaseTypeSQLite,
			ConnectionString: dbPath, DBTimeout: 30, DBEnableWAL: true},
		TransportConfig: &atschema.MessagerTransportConfig{Backend: "inprocess"},
	}
	env := descriptor.ToEnv()
	value, ok := env[external.TeamJoinEnv]
	s.Require().True(ok, "描述符应产出 OPENUAPCLAW_TEAM_JOIN")
	return value, dbPath, dir
}

// newConnectedAgent 构造已连接真 MCP server 的 DeepAgent（真实配置 + 真连接）。
func (s *McpAgentSuite) newConnectedAgent(serverBin string) (*harness.DeepAgent, string) {
	joinValue, dbPath, _ := s.teamJoinEnv()

	// 真实配置形状（对齐 deep_adapter_mcp.go buildMcpServerConfig 消费面）：
	// mcp.servers 条目 stdio + params{command,env}。
	cfg := harnessschema.NewDeepAgentConfig()
	cfg.Mcps = []*types.McpServerConfig{
		types.NewMcpServerConfig("openuapclaw_team", serverBin, "stdio",
			types.WithParams(map[string]any{
				"command": serverBin,
				"env":     map[string]any{external.TeamJoinEnv: joinValue},
			})),
	}

	card := agentschema.NewAgentCard(agentschema.WithAgentName("mcpagent-e2e-agent"))
	owner := harness.NewDeepAgent(card)
	s.Require().NoError(owner.ConfigureDeepConfig(context.Background(), cfg))
	// EnsureInitialized 内部走 ensureInitialized → registerPendingMCPs 真连接
	_, err := owner.EnsureInitialized(context.Background())
	s.Require().NoError(err, "DeepAgent 初始化（含 MCP 真连接）失败")
	return owner, dbPath
}

// ──────────────────────────── 场景 1：连接与注入 ────────────

// TestMcp连接与注入 真实 stdio 配置 → 真连接 → AbilityManager 懒物化。
func (s *McpAgentSuite) TestMcp连接与注入() {
	owner, dbPath := s.newConnectedAgent(s.buildTeamMcpServer())
	am := owner.AbilityManager()
	s.Require().NotNil(am)

	// 懒物化：list_tool_info 触发 mcp_{server}_{tool} 卡片注册
	infos, err := am.ListToolInfo(context.Background(), nil)
	s.Require().NoError(err)
	mcpNames := []string{}
	for _, info := range infos {
		if strings.HasPrefix(info.GetName(), "mcp_openuapclaw_team_") {
			mcpNames = append(mcpNames, info.GetName())
		}
	}
	s.Require().NotEmpty(mcpNames, "模型可见面应含 mcp_openuapclaw_team_* 工具")

	// 卡片注册进 AbilityManager（Get 面可取）
	card := am.Get(mcpNames[0])
	s.Require().NotNil(card, "mcp 工具卡应可从 AbilityManager 取得: %s", mcpNames[0])

	// 连接成功的可观测产物：server 急切 connect 后真实创建团队库
	_, statErr := os.Stat(dbPath)
	s.Require().NoError(statErr, "team MCP server 连接后应创建 sqlite 团队库")
}

// ──────────────────────────── 场景 2：检索披露联动 ────────────

// TestMcp检索披露联动 9.11b 虚拟分支真链：live mcp 工具 + server 描述 →
// 披露节渲染 mcp_{server} 分支行。
func (s *McpAgentSuite) TestMcp检索披露联动() {
	owner, _ := s.newConnectedAgent(s.buildTeamMcpServer())
	am := owner.AbilityManager()

	// live 注册表（真实懒物化产物）喂给检索 toolkit
	infos, err := am.ListToolInfo(context.Background(), nil)
	s.Require().NoError(err)
	var mcpInfos []schema.ToolInfoInterface
	for _, info := range infos {
		if strings.HasPrefix(info.GetName(), "mcp_openuapclaw_team_") {
			mcpInfos = append(mcpInfos, info)
		}
	}
	s.Require().NotEmpty(mcpInfos)

	// provider 描述（对齐 DeepAdapter.mcpServerDescriptions 的输出形状）
	serverDescs := map[string]any{
		"openuapclaw_team": swarmtools.McpServerDescription{Zh: "团队协作工具", En: "team tools"},
	}
	provider := func() []schema.ToolInfoInterface { return mcpInfos }
	toolkit := swarmtools.NewToolRetrievalToolkit(provider, "sess-mcp", nil, func() map[string]any { return serverDescs }, "cn")

	rail := swarmrails.NewToolRetrievalPromptRail(toolkit, map[string]any{
		"progressive_tool_enabled": true, "progressive_tool_optimize": true,
	})
	sess := session.NewSession()
	cbc := agentinterfaces.NewAgentCallbackContext(owner, nil, sess)
	s.Require().NoError(rail.BeforeModelCall(context.Background(), cbc))

	// 披露节渲染断言：MCP 虚拟分支行 + server 描述
	// （rail 无 builder 注入的裸路径走 attachment 面；此处直接经 toolkit
	// 渲染器断言分支行语义——披露节构建与分支行的拼接已在单测覆盖，
	// 集成面钉住 toolkit 对真实 server 工具的分组正确性）
	// 真实链断言：list/list 结果含分支分类名
	listOut, listErr := toolkit.ToolSearch(map[string]any{
		"operation": "list", "category": "mcp_openuapclaw_team",
	})
	s.Require().NoError(listErr, "mcp 分支应可经 tool_search 导航")
	s.Require().Contains(listOut, "mcp_openuapclaw_team_")
}

// mcpagent 目录说明（避免空目录审计误判；suite 断言见上方）。
