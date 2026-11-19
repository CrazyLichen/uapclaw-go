package common

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
)

// ExternalCliWriteSuite config 写链测试。
// Python 对照：config_set_handlers.py external_cli 段 + config.py update_* 函数语义。
type ExternalCliWriteSuite struct{ suite.Suite }

func TestExternalCliWriteSuite(t *testing.T) {
	suite.Run(t, new(ExternalCliWriteSuite))
}

func (s *ExternalCliWriteSuite) TestIsExternalCliAgentConfigKey() {
	s.True(IsExternalCliAgentConfigKey("external_cli_agent_claude_enabled"))
	s.True(IsExternalCliAgentConfigKey("external_cli_agent_codex_cli_path"))
	s.False(IsExternalCliAgentConfigKey("external_cli_agent_ghost_enabled"))
	s.False(IsExternalCliAgentConfigKey("swarmflow_enabled"))
}

func (s *ExternalCliWriteSuite) TestFromSwitches组装与未触碰保留() {
	raw := map[string]any{
		"modes": map[string]any{"team": map[string]any{"jiuwen_team": map[string]any{
			"external_cli_agents": []any{
				map[string]any{"cli_agent": "claude", "cli_path": "/persisted/claude"},
				map[string]any{"cli_agent": "codex", "cli_path": "/persisted/codex"},
			},
		}}},
	}
	detect := func(cliAgent, requestedPath string, useBuiltin bool) map[string]any {
		return map[string]any{"status": "ok", "path": requestedPath}
	}
	// 只触碰 claude：codex 未触碰 → 原样保留
	entries, err := ExternalCliAgentsFromSwitches(raw,
		map[string]any{"external_cli_agent_claude_enabled": true, "external_cli_agent_claude_use_builtin": false,
			"external_cli_agent_claude_cli_path": "/new/claude"},
		[]string{"claude", "codex"}, map[string]string{}, detect)
	s.Require().NoError(err)
	s.Require().Len(entries, 2)
	s.Equal("/new/claude", entries[0]["cli_path"])
	s.Equal("/persisted/codex", entries[1]["cli_path"]) // untouched verbatim
}

func (s *ExternalCliWriteSuite) TestFromSwitches禁用与探测失败() {
	raw := map[string]any{}
	detect := func(cliAgent, requestedPath string, useBuiltin bool) map[string]any {
		return map[string]any{"status": "unavailable", "message": "not found"}
	}
	// disabled → 不产条目
	entries, err := ExternalCliAgentsFromSwitches(raw,
		map[string]any{"external_cli_agent_claude_enabled": false, "external_cli_agent_claude_use_builtin": false},
		[]string{"claude"}, nil, detect)
	s.Require().NoError(err)
	s.Empty(entries)
	// 探测失败 → ValueError 语义
	_, err = ExternalCliAgentsFromSwitches(raw,
		map[string]any{"external_cli_agent_claude_enabled": true, "external_cli_agent_claude_use_builtin": false},
		[]string{"claude"}, nil, detect)
	s.Require().Error(err)
	s.Contains(err.Error(), "cli_path is not available")
	// 上传 CLI 即内建：use_builtin 且有上传路径 → 直接取上传路径
	entries, err = ExternalCliAgentsFromSwitches(raw,
		map[string]any{"external_cli_agent_myagent_enabled": true, "external_cli_agent_myagent_use_builtin": true},
		[]string{"claude", "myagent"}, map[string]string{"myagent": "/uploaded/foo"}, detect)
	s.Require().NoError(err)
	s.Require().Len(entries, 1)
	s.Equal("/uploaded/foo", entries[0]["cli_path"])
}

func (s *ExternalCliWriteSuite) TestNewlyEnabled() {
	raw := map[string]any{
		"modes": map[string]any{"team": map[string]any{"jiuwen_team": map[string]any{
			"external_cli_agents": []any{map[string]any{"cli_agent": "codex"}},
		}}},
	}
	entries := []map[string]any{{"cli_agent": "codex"}, {"cli_agent": "dsh"}}
	s.Equal([]string{"dsh"}, NewlyEnabledExternalCliAgents(raw, entries))
}

func (s *ExternalCliWriteSuite) TestHistoriesFromSwitches旧路径追加() {
	raw := map[string]any{
		"modes": map[string]any{"team": map[string]any{"jiuwen_team": map[string]any{
			"external_cli_path_histories": map[string]any{
				"codex": []any{"/old/one"},
			},
		}}},
	}
	merged := ExternalCliPathHistoriesFromSwitches(raw,
		map[string]any{
			"external_cli_agent_codex_cli_path":       "/new/two",
			"external_cli_agent_codex_path_histories": `["/old/one","/old/two"]`,
		},
		[]string{"codex"}, nil)
	// 旧生效路径 /old/two？——旧 cli_path 为空（未配置），当前生效路径不在历史
	s.Equal([]string{"/old/one", "/old/two"}, merged["codex"])
}

func (s *ExternalCliWriteSuite) TestParseConfigSwitchBool() {
	s.True(parseConfigSwitchBool(true))
	s.True(parseConfigSwitchBool("TRUE"))
	s.True(parseConfigSwitchBool("1"))
	s.False(parseConfigSwitchBool("no"))
	s.False(parseConfigSwitchBool(false))
}

func (s *ExternalCliWriteSuite) TestUpdatePathHistories写回() {
	// 独立临时 config（UAPCLAW_CONFIG 走 config.New("") 默认路径不可注入——
	// 写链以默认 config 为载体；本用例经临时 HOME 隔离默认路径）
	cfg, err := config.New(filepath.Join(s.T().TempDir(), "config.yaml"))
	s.Require().NoError(err)
	s.Require().NoError(UpdateExternalCliPathHistoriesInConfig(cfg, map[string][]string{
		"codex": {"/a", "/b"},
	}))
	histories := cfg.Get("modes.team.jiuwen_team.external_cli_path_histories")
	m, ok := histories.(map[string]any)
	s.Require().True(ok)
	s.Equal([]any{"/a", "/b"}, m["codex"])
}
