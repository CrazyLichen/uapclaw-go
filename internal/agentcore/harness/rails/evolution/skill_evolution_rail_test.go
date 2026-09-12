package evolution

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestSkillEvolutionRail_Priority(t *testing.T) {
	// Priority 必须返回 80，对齐 Python: SkillEvolutionRail.priority = 80
	// 不构造完整实例，直接验证常量约定
	r := &SkillEvolutionRail{}
	assert.Equal(t, 80, r.Priority())
}

func TestSkillEvolutionRail_AllowEvolutionTrigger(t *testing.T) {
	r := &SkillEvolutionRail{autoScan: true}
	assert.True(t, r.AllowEvolutionTrigger(TriggerAfterInvoke, nil))

	r.autoScan = false
	assert.False(t, r.AllowEvolutionTrigger(TriggerAfterInvoke, nil))
}

func TestSkillEvolutionRail_AutoScan(t *testing.T) {
	r := &SkillEvolutionRail{autoScan: true}
	assert.True(t, r.AutoScan())

	r.SetAutoScan(false)
	assert.False(t, r.AutoScan())
}

func TestSkillEvolutionRail_AutoSave(t *testing.T) {
	r := &SkillEvolutionRail{autoSave: true}
	assert.True(t, r.AutoSave())

	r.SetAutoSave(false)
	assert.False(t, r.AutoSave())
}

func TestSkillEvolutionRail_ClearProcessedSignals(t *testing.T) {
	r := &SkillEvolutionRail{
		processedSignalKeys: map[[4]string]bool{
			{"a", "b", "c", "d"}: true,
		},
	}
	assert.Len(t, r.ProcessedSignalKeys(), 1)

	r.ClearProcessedSignals()
	assert.Len(t, r.ProcessedSignalKeys(), 0)
}

func TestSkillEvolutionRail_IsSharingEnabled(t *testing.T) {
	r := &SkillEvolutionRail{}
	assert.False(t, r.IsSharingEnabled())

	r.experienceSharer = nil
	r.shareStager = nil
	assert.False(t, r.IsSharingEnabled())
}

func TestAppendUniqueSignal_去重(t *testing.T) {
	sig1 := signal.MakeEvolutionSignal("conversation_review", "", "excerpt1", signal.WithSkillName("test"))
	sig2 := signal.MakeEvolutionSignal("conversation_review", "", "excerpt1", signal.WithSkillName("test"))
	sig3 := signal.MakeEvolutionSignal("user_intent", "", "excerpt2", signal.WithSkillName("test"))

	var signals []*signal.EvolutionSignal
	signals = appendUniqueSignal(signals, sig1)
	assert.Len(t, signals, 1)

	// 重复信号应被忽略
	signals = appendUniqueSignal(signals, sig2)
	assert.Len(t, signals, 1)

	// 不同信号应被追加
	signals = appendUniqueSignal(signals, sig3)
	assert.Len(t, signals, 2)
}

func TestConvertSignalsToValues(t *testing.T) {
	sig1 := signal.MakeEvolutionSignal("conversation_review", "", "excerpt1", signal.WithSkillName("test"))
	sig2 := signal.MakeEvolutionSignal("user_intent", "", "excerpt2", signal.WithSkillName("test2"))

	ptrs := []*signal.EvolutionSignal{sig1, sig2}
	vals := convertSignalsToValues(ptrs)

	assert.Len(t, vals, 2)
	assert.Equal(t, sig1.SkillName, vals[0].SkillName)
	assert.Equal(t, sig2.SkillName, vals[1].SkillName)
}

func TestExtractPresentedRecordIDs(t *testing.T) {
	r := &SkillEvolutionRail{}

	// 正常场景：包含多个 record ID
	content := "## [rec_001] Some experience\n### [rec_002] Another experience\n#### [rec_003] Third"
	ids := r.extractPresentedRecordIDs(content)
	assert.Equal(t, []string{"rec_001", "rec_002", "rec_003"}, ids)

	// 去重场景
	contentDedup := "## [rec_001] First\n## [rec_001] Duplicate\n## [rec_002] Second"
	idsDedup := r.extractPresentedRecordIDs(contentDedup)
	assert.Equal(t, []string{"rec_001", "rec_002"}, idsDedup)

	// 空内容
	idsEmpty := r.extractPresentedRecordIDs("")
	assert.Nil(t, idsEmpty)

	// 无匹配
	idsNoMatch := r.extractPresentedRecordIDs("no headings here")
	assert.Nil(t, idsNoMatch)
}

func TestIsExperienceDetailRelativePath(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"evolution/experience1.md", true},
		{"evolution/scripts/script1.sh", false},
		{"evolution/SKILL.md", false},
		{"SKILL.md", false},
		{"evolution/evolutions.json", false},
		{"evolution/scripts/", false},
		{"evolution/deep_detail.md", true},
		{"../outside.md", false},
		{"skills/evolution/detail.md", false}, // 不以 evolution/ 开头
		{"evolution/scripts/README.md", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.expected, isExperienceDetailRelativePath(tt.path))
		})
	}
}

func TestResolveSharingBool(t *testing.T) {
	assert.True(t, resolveSharingBool(true))
	assert.True(t, resolveSharingBool(1))
	assert.True(t, resolveSharingBool(float64(1.0)))
	assert.True(t, resolveSharingBool("true"))
	assert.True(t, resolveSharingBool("1"))
	assert.True(t, resolveSharingBool("yes"))
	assert.True(t, resolveSharingBool("on"))
	assert.False(t, resolveSharingBool(false))
	assert.False(t, resolveSharingBool(0))
	assert.False(t, resolveSharingBool("false"))
	assert.False(t, resolveSharingBool("0"))
	assert.False(t, resolveSharingBool(nil))
	assert.False(t, resolveSharingBool("random"))
}

func TestExtractToolArgs(t *testing.T) {
	// map[string]any 输入
	args := extractToolArgs(map[string]any{"key": "value"})
	assert.Equal(t, "value", args["key"])

	// JSON 字符串输入
	argsJSON := extractToolArgs(`{"key": "value"}`)
	assert.Equal(t, "value", argsJSON["key"])

	// 无效字符串
	argsInvalid := extractToolArgs("not json")
	assert.Empty(t, argsInvalid)

	// nil 输入
	argsNil := extractToolArgs(nil)
	assert.Empty(t, argsNil)
}

func TestInferSkillFromTexts(t *testing.T) {
	skillNames := []string{"coding", "writing", "analysis"}

	// skill_tool 参数中包含技能名
	payloads := []string{`{"skill_name": "coding", "query": "test"}`}
	texts := []string{}
	result := inferSkillFromTexts(skillNames, payloads, texts)
	assert.Equal(t, "coding", result)

	// 无匹配
	payloadsNoMatch := []string{`{"skill_name": "unknown", "query": "test"}`}
	resultNoMatch := inferSkillFromTexts(skillNames, payloadsNoMatch, texts)
	assert.Equal(t, "", resultNoMatch)
}

func TestAttributeSignalsToSkills(t *testing.T) {
	r := &SkillEvolutionRail{}

	skillA := "skill_a"
	skillB := "skill_b"

	sig1 := signal.MakeEvolutionSignal("conversation_review", "", "excerpt1", signal.WithSkillName(skillA))
	sig2 := signal.MakeEvolutionSignal("conversation_review", "", "excerpt2", signal.WithSkillName(skillA))
	sig3 := signal.MakeEvolutionSignal("user_intent", "", "excerpt3", signal.WithSkillName(skillB))

	signals := []*signal.EvolutionSignal{sig1, sig2, sig3}
	groups := r.attributeSignalsToSkills(signals)

	assert.Len(t, groups, 2)
	assert.Len(t, groups[skillA], 2)
	assert.Len(t, groups[skillB], 1)
}

func TestAttributeSignalsToSkills_单技能fallback(t *testing.T) {
	r := &SkillEvolutionRail{}

	skillA := "skill_a"
	sig1 := signal.MakeEvolutionSignal("conversation_review", "", "excerpt1", signal.WithSkillName(skillA))
	sig2 := signal.MakeEvolutionSignal("user_intent", "", "excerpt2") // 无归属

	signals := []*signal.EvolutionSignal{sig1, sig2}
	groups := r.attributeSignalsToSkills(signals)

	// 单技能 fallback：无归属信号分配给唯一归属技能
	assert.Len(t, groups, 1)
	assert.Len(t, groups[skillA], 2)
}

func TestExtractConversationExcerpt(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "How do I fix the bug?"},
		{"role": "assistant", "content": "Let me check", "tool_calls": []any{
			map[string]any{"id": "tc1", "name": "read_file", "arguments": "{}"},
		}},
		{"role": "tool", "content": "Error: permission denied", "tool_call_id": "tc1"},
	}

	excerpt := extractConversationExcerpt(messages)
	assert.Contains(t, excerpt, "USER QUERIES")
	assert.Contains(t, excerpt, "FAILED TOOL EXECUTIONS")
	assert.Contains(t, excerpt, "TOOL CALLS")
}

func TestParseTopLevelFrontmatter_通过Evolution包(t *testing.T) {
	// 测试 checkpointing.ParseTopLevelFrontmatter 的导出版本
	content := "---\nkind: team-skill\ndescription: test\n---\nBody content"
	result := parseTopLevelFrontmatter(content)
	assert.Equal(t, "team-skill", result["kind"])
	assert.Equal(t, "test", result["description"])

	// 无 frontmatter
	resultEmpty := parseTopLevelFrontmatter("No frontmatter here")
	assert.Empty(t, resultEmpty)
}

func TestIsRegularSkill_非常规技能(t *testing.T) {
	// 对齐 Python: _NON_REGULAR_SKILL_KINDS = {"team-skill", "swarm-skill"}
	// 测试 frontmatter 解析后的 kind 判断逻辑
	teamSkillFM := parseTopLevelFrontmatter("---\nkind: team-skill\n---\nbody")
	assert.Equal(t, "team-skill", teamSkillFM["kind"])
	assert.False(t, teamSkillFM["kind"] != "team-skill" && teamSkillFM["kind"] != "swarm-skill")

	swarmSkillFM := parseTopLevelFrontmatter("---\nkind: swarm-skill\n---\nbody")
	assert.Equal(t, "swarm-skill", swarmSkillFM["kind"])

	regularFM := parseTopLevelFrontmatter("---\nkind: skill\n---\nbody")
	assert.Equal(t, "skill", regularFM["kind"])
	assert.True(t, regularFM["kind"] != "team-skill" && regularFM["kind"] != "swarm-skill")
}

func TestEvolutionSnapshot_扩展字段(t *testing.T) {
	skillName := "test-skill"
	snapshot := EvolutionSnapshot{
		SessionID:          "session-123",
		PresentedEntries:   []experience.PresentedRecordEntry{},
		IncrementalMessages: []map[string]any{{"role": "user", "content": "test"}},
		SkillName:          &skillName,
	}

	// ToLegacyDict 应包含扩展字段
	legacy := snapshot.ToLegacyDict()
	assert.Equal(t, "session-123", legacy["session_id"])
	assert.Equal(t, "test-skill", legacy["skill_name"])
	assert.NotNil(t, legacy["incremental_messages"])
}

func TestStr辅助函数(t *testing.T) {
	assert.Equal(t, "", str(nil))
	assert.Equal(t, "hello", str("hello"))
	assert.Equal(t, "42", str(42))
}

func TestMapKeys(t *testing.T) {
	m := map[string]int{"a": 1, "b": 2, "c": 3}
	keys := mapKeys(m)
	assert.Len(t, keys, 3)
	// 验证所有键都存在
	keySet := make(map[string]bool)
	for _, k := range keys {
		keySet[k] = true
	}
	assert.True(t, keySet["a"])
	assert.True(t, keySet["b"])
	assert.True(t, keySet["c"])
}

func TestTruncateString(t *testing.T) {
	assert.Equal(t, "hello", truncateString("hello", 10))
	assert.Equal(t, "hel", truncateString("hello", 3))
	assert.Equal(t, "", truncateString("", 5))
}
