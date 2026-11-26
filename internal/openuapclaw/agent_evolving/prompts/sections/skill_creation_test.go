package sections

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBuildSkillCreationGuidanceSection 对齐 Python build_skill_creation_guidance_section
// （skill_creation.py:186-192）：name=SKILL_CREATION_GUIDANCE / priority=106 / 双语内容为原文。
func TestBuildSkillCreationGuidanceSection(t *testing.T) {
	cn := BuildSkillCreationGuidanceSection("cn")
	assert.Equal(t, "skill_creation_guidance", cn.Name)
	assert.Equal(t, 106, cn.Priority)
	assert.True(t, strings.HasPrefix(cn.Content["cn"], "## 技能沉淀自检"))
	assert.True(t, strings.HasPrefix(cn.Content["en"], "## Skill Capture Self-Check"))
	// 原文逐字锚点抽查（决策顺序条目数与等价语义句）
	assert.Contains(t, cn.Content["cn"], "不要按关键词机械匹配")
	assert.Contains(t, cn.Content["en"], "do not keyword-match them")
}

// TestBuildTeamSkillCreationGuidanceSection 对齐 skill_creation.py:195-201（priority=108）。
func TestBuildTeamSkillCreationGuidanceSection(t *testing.T) {
	s := BuildTeamSkillCreationGuidanceSection("cn")
	assert.Equal(t, "team_skill_creation_guidance", s.Name)
	assert.Equal(t, 108, s.Priority)
	assert.True(t, strings.HasPrefix(s.Content["cn"], "## 团队技能沉淀自检"))
	assert.True(t, strings.HasPrefix(s.Content["en"], "## Team Skill Capture Self-Check"))
}

// TestBuildTeamSkillCreationNudgeSection 对齐 skill_creation.py:204-210
// （priority=109；del skills_dir 形态保留，入参不消费）。
func TestBuildTeamSkillCreationNudgeSection(t *testing.T) {
	s := BuildTeamSkillCreationNudgeSection("/tmp/unused", "cn")
	assert.Equal(t, "team_skill_creation_nudge", s.Name)
	assert.Equal(t, 109, s.Priority)
	assert.Contains(t, s.Content["cn"], "本轮团队技能沉淀检查")
	assert.Contains(t, s.Content["en"], "Team Skill Capture Check For This Round")
}
