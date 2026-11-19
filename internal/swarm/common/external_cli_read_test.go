package common

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
)

// ExternalCliReadSuite 面板 external_cli_agent_* 四键读展平测试。
// Python 对照：jiuwenswarm/common/config_panel/external_cli_read.py 语义。
type ExternalCliReadSuite struct{ suite.Suite }

func TestExternalCliReadSuite(t *testing.T) {
	suite.Run(t, new(ExternalCliReadSuite))
}

func (s *ExternalCliReadSuite) TestExternalCliAgentKey() {
	s.Equal("external_cli_agent_claude_enabled", ExternalCliAgentKey("claude", "enabled"))
}

func (s *ExternalCliReadSuite) TestExternalCliAgentKinds含注册表() {
	kinds := ExternalCliAgentKinds()
	s.Contains(kinds, "claude")
	s.Contains(kinds, "codex")
	s.NotContains(kinds, "ghost-agent")
}

func (s *ExternalCliReadSuite) TestFlatten四键投影() {
	raw := map[string]any{
		"modes": map[string]any{"team": map[string]any{"jiuwen_team": map[string]any{
			"external_cli_agents": []any{
				map[string]any{"cli_agent": "claude", "cli_path": ""},
				map[string]any{"cli_agent": "codex", "cli_path": "/usr/bin/codex"},
			},
			"external_cli_path_histories": map[string]any{
				"codex": []any{"/usr/bin/codex", "/usr/bin/codex", "/opt/codex", "", "   "},
			},
		}}},
	}
	flat := FlattenExternalCliAgentsForConfigPanel(raw, []string{"claude", "codex"}, map[string]string{})
	s.Equal("true", flat["external_cli_agent_claude_enabled"])
	// cli_path 为空 → builtin（Python :85 空串语义）
	s.Equal("true", flat["external_cli_agent_claude_use_builtin"])
	s.Equal("", flat["external_cli_agent_claude_cli_path"])
	s.Equal("true", flat["external_cli_agent_codex_enabled"])
	s.Equal("false", flat["external_cli_agent_codex_use_builtin"])
	s.Equal("/usr/bin/codex", flat["external_cli_agent_codex_cli_path"])
	// 首现去重 + 空白滤除（:87-93）
	s.Equal(`["/usr/bin/codex","/opt/codex"]`, flat["external_cli_agent_codex_path_histories"])
	// kinds 只投影传入集合；未列入 kinds 的 kind 不产生键（Python :81 遍历 kinds）
	_, ok := flat["external_cli_agent_myagent_enabled"]
	s.False(ok)
}

func (s *ExternalCliReadSuite) TestFlatten上传CLI即内建() {
	raw := map[string]any{
		"modes": map[string]any{"team": map[string]any{"jiuwen_team": map[string]any{
			"external_cli_agents": []any{
				map[string]any{"cli_agent": "myagent", "cli_path": "/uploaded/foo"},
			},
		}}},
	}
	// 注册表声明 myagent 的上传 CLI 即 /uploaded/foo → use_builtin=true 且 cli_path 清空
	flat := FlattenExternalCliAgentsForConfigPanel(raw, []string{"claude", "codex", "myagent"},
		map[string]string{"myagent": "/uploaded/foo"})
	s.Equal("true", flat["external_cli_agent_myagent_use_builtin"])
	s.Equal("", flat["external_cli_agent_myagent_cli_path"])
}

func (s *ExternalCliReadSuite) TestFlatten缺段与字符串条目() {
	// entries 项为纯字符串形态（Python :70-72）
	raw := map[string]any{
		"modes": map[string]any{"team": map[string]any{"jiuwen_team": map[string]any{
			"external_cli_agents": []any{"codex"},
		}}},
	}
	flat := FlattenExternalCliAgentsForConfigPanel(raw, []string{"claude", "codex"}, nil)
	s.Equal("true", flat["external_cli_agent_codex_enabled"])
	s.Equal("true", flat["external_cli_agent_codex_use_builtin"])
	// 整段缺失 → 全 false
	empty := FlattenExternalCliAgentsForConfigPanel(map[string]any{}, []string{"claude"}, nil)
	s.Equal("false", empty["external_cli_agent_claude_enabled"])
}

func (s *ExternalCliReadSuite) TestCustomAgentsConfigValue() {
	// 注册表为共享真实路径（WorkspaceDir），先落已知条目再断言其出现在串中
	root := CustomAgentsRoot()
	s.Require().NoError(os.MkdirAll(root, 0o755))
	seed := `{"dsh":{"name":"dsh","version":"1","summary":"","cli_path":""}}`
	s.Require().NoError(os.WriteFile(filepath.Join(root, "agents.json"), []byte(seed), 0o644))
	var decoded []map[string]any
	s.Require().NoError(json.Unmarshal([]byte(CustomAgentsConfigValue()), &decoded))
	s.Require().Len(decoded, 1)
	s.Equal("dsh", decoded[0]["name"])
	s.Equal(false, decoded[0]["has_cli"])
}

func (s *ExternalCliReadSuite) TestPathHistoriesJSON形状() {
	// ensure_ascii=False 等价：Go json.Marshal 对非 ASCII 直接输出 UTF-8
	// 去重经 historyPaths（Flatten 内部链路），JSON 层只负责序列化
	histories := pathHistoriesJSON(historyPaths([]any{"/路径/一", "/路径/一", "/路径/二"}))
	var decoded []string
	s.Require().NoError(json.Unmarshal([]byte(histories), &decoded))
	s.Equal([]string{"/路径/一", "/路径/二"}, decoded)
	s.False(strings.Contains(histories, "\\u"))
}
