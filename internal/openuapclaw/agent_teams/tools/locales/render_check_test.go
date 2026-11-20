package locales

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
)

// TestRenderDesc_spawnTeammate_omit门控 omit 塌缩使 fork 段消失（e15 审查修复验证）。
func TestRenderDesc_spawnTeammate_omit门控(t *testing.T) {
	full := RenderDesc("spawn_teammate", atschema.LanguageCN, map[string]bool{})
	require.NotEmpty(t, full)
	require.Contains(t, full, "上下文继承（Fork）") // slot 填充自 fragments
	omitted := RenderDesc("spawn_teammate", atschema.LanguageCN, map[string]bool{
		"fork_usage": true, "fork_param_rows": true,
	})
	require.NotContains(t, omitted, "上下文继承（Fork）")
	require.True(t, strings.TrimSpace(omitted) != "")
}
