package team

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
	externalcli "github.com/uapclaw/uapclaw-go/internal/agent_teams/runtime/externalcli"
	swarmcommon "github.com/uapclaw/uapclaw-go/internal/swarm/common"
)

// ExternalCliPrepareSuite 外部 CLI agent 预处理测试。
// Python 对照：config_loader.py:33-71 _prepare_external_cli_agents。
type ExternalCliPrepareSuite struct{ suite.Suite }

func TestExternalCliPrepareSuite(t *testing.T) {
	suite.Run(t, new(ExternalCliPrepareSuite))
}

// withReport 在真实 WorkspaceDir 报告路径落报告（WorkspaceDir 进程级缓存，
// 包内已有测试可能已定型，故直接写真实路径并在收尾清理）。
func (s *ExternalCliPrepareSuite) withReport(report map[string]map[string][]string) {
	path := externalcli.ReportedModelsPath()
	s.Require().NoError(os.MkdirAll(filepath.Dir(path), 0o755))
	data := "{}"
	if report != nil {
		b, err := json.Marshal(report)
		s.Require().NoError(err)
		data = string(b)
	}
	s.Require().NoError(os.WriteFile(path, []byte(data), 0o644))
}

func (s *ExternalCliPrepareSuite) TestPrepare无条目直接返回() {
	specDict := map[string]any{}
	PrepareExternalCliAgents(specDict) // 不 panic
	_, ok := specDict["external_cli_agents"]
	s.False(ok)
}

func (s *ExternalCliPrepareSuite) TestPrepare目录对拍() {
	s.withReport(map[string]map[string][]string{"dsh": {"m1": {"low"}}})
	specDict := map[string]any{"external_cli_agents": []any{
		map[string]any{"cli_agent": "dsh", "builtin_models": []any{
			map[string]any{"name": "gone"}, map[string]any{"name": "m1"},
		}},
	}}
	PrepareExternalCliAgents(specDict)
	entries := specDict["external_cli_agents"].([]any)
	entry := entries[0].(map[string]any)
	corrected := entry["builtin_models"].([]any)
	s.Require().Len(corrected, 1) // gone 被剔除
	s.Equal("m1", corrected[0].(map[string]any)["name"])
}

func (s *ExternalCliPrepareSuite) TestPrepare自定义Agent补MCPCommand() {
	s.withReport(nil)
	// 编译期注册缝口径（D-15.16-d3）：注册表登记自定义 agent（写 agents.json）
	// + ActivateCustomAgentPaths 校验注册缝。
	// 注册表落真实 WorkspaceDir 下的 runtime/custom-agents。
	root := swarmcommon.CustomAgentsRoot()
	s.Require().NoError(os.MkdirAll(root, 0o755))
	registry := map[string]map[string]string{
		"dsh": {"name": "dsh", "version": "1", "summary": "", "cli_path": ""},
	}
	data, err := json.Marshal(registry)
	s.Require().NoError(err)
	s.Require().NoError(os.WriteFile(filepath.Join(root, "agents.json"), data, 0o644))

	specDict := map[string]any{"external_cli_agents": []any{
		map[string]any{"cli_agent": "dsh"},
	}}
	PrepareExternalCliAgents(specDict)
	entry := specDict["external_cli_agents"].([]any)[0].(map[string]any)
	cmd, has := entry["mcp_server_command"].([]string)
	s.Require().True(has) // setdefault 注入
	s.Equal("uapclawmcpserver", cmd[0])
}

func (s *ExternalCliPrepareSuite) TestPrepare已有MCPCommand不覆盖() {
	s.withReport(nil)
	specDict := map[string]any{"external_cli_agents": []any{
		map[string]any{"cli_agent": "dsh", "mcp_server_command": []any{"/custom/server"}},
	}}
	PrepareExternalCliAgents(specDict)
	entry := specDict["external_cli_agents"].([]any)[0].(map[string]any)
	s.Equal("/custom/server", entry["mcp_server_command"].([]any)[0]) // setdefault 语义
}
