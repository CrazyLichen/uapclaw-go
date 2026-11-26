package sections

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBuildEvolutionProtocolSection 对齐 Python build_evolution_protocol_section
// （evolution.py:193-201）：name=EVOLUTION_PROTOCOL / priority=98 / 双语原文。
func TestBuildEvolutionProtocolSection(t *testing.T) {
	s := BuildEvolutionProtocolSection("cn")
	assert.Equal(t, "evolution_protocol", s.Name)
	assert.Equal(t, 98, s.Priority)
	assert.True(t, strings.HasPrefix(s.Content["cn"], "## 技能演进自检"))
	assert.True(t, strings.HasPrefix(s.Content["en"], "## Skill Evolution Self-Check"))
	assert.Contains(t, s.Content["cn"], "创建确认不能启动演进")
	assert.Contains(t, s.Content["en"], "Creation confirmation cannot\nstart evolution")
}

// TestBuildTeamEvolutionProtocolSection 对齐 evolution.py:203-209（priority=99）。
func TestBuildTeamEvolutionProtocolSection(t *testing.T) {
	s := BuildTeamEvolutionProtocolSection("cn")
	assert.Equal(t, "evolution_team_protocol", s.Name)
	assert.Equal(t, 99, s.Priority)
	assert.True(t, strings.HasPrefix(s.Content["cn"], "## 团队 Skill 演进自检"))
	assert.True(t, strings.HasPrefix(s.Content["en"], "## Team Skill Evolution Self-Check"))
	assert.Contains(t, s.Content["cn"], "创建确认不能启动团队演进")
}
