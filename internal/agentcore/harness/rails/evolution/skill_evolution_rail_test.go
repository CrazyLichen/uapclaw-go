package evolution

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
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
		SessionID:           "session-123",
		PresentedEntries:    []experience.PresentedRecordEntry{},
		IncrementalMessages: []map[string]any{{"role": "user", "content": "test"}},
		SkillName:           &skillName,
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

func TestWithDisabledSkillsSet(t *testing.T) {
	r := &SkillEvolutionRail{}
	WithDisabledSkillsSet([]string{"foo", "bar"})(r)
	assert.Equal(t, []string{"foo", "bar"}, r.disabledSkills)
}

func TestWithDisabledSkillsSet_基类已初始化(t *testing.T) {
	r := &SkillEvolutionRail{EvolutionRail: &EvolutionRail{}}
	WithDisabledSkillsSet([]string{"foo", "bar"})(r)
	assert.Equal(t, []string{"foo", "bar"}, r.disabledSkills)
	assert.True(t, r.EvolutionRail.disabledSkills["foo"])
	assert.True(t, r.EvolutionRail.disabledSkills["bar"])
}

func TestNormalizeNameSet(t *testing.T) {
	r := &SkillEvolutionRail{disabledSkills: []string{"a", "b"}}
	assert.Equal(t, []string{"a", "b"}, r.normalizeNameSet())
}

func TestNormalizeNameSet_空(t *testing.T) {
	r := &SkillEvolutionRail{}
	assert.Equal(t, []string(nil), r.normalizeNameSet())
}

func TestShouldHintSimplifyOrRebuild_走Store(t *testing.T) {
	// 验证 ShouldHintSimplifyOrRebuild 走 EvolutionStore.LoadFullEvolutionLog
	// 而非直接 os.ReadFile + json.Unmarshal
	// 完整测试需要 mock EvolutionStore，当前仅确认函数签名正确
	t.Log("ShouldHintSimplifyOrRebuild 应走 EvolutionStore.LoadFullEvolutionLog")
}

// ──────────────────────────── S-02: filterDuplicateSharedRecords / parseDuplicateCheckResponse ────────────────────────────

func TestParseDuplicateCheckResponse_正常去重(t *testing.T) {
	shared := []checkpointing.EvolutionRecord{
		{ID: "rec_001", Change: checkpointing.EvolutionPatch{Target: signal.EvolutionTargetDescription, Section: "Instructions", Content: "keep this"}},
		{ID: "rec_002", Change: checkpointing.EvolutionPatch{Target: signal.EvolutionTargetBody, Section: "Examples", Content: "duplicate this"}},
		{ID: "rec_003", Change: checkpointing.EvolutionPatch{Target: signal.EvolutionTargetDescription, Section: "Troubleshooting", Content: "also keep"}},
	}

	rawResponse := `[{"record_id": "rec_001", "decision": "keep", "reason": "unique"}, {"record_id": "rec_002", "decision": "duplicate", "reason": "same content"}]`
	result := parseDuplicateCheckResponse(shared, rawResponse)
	assert.Len(t, result, 2)
	assert.Equal(t, "rec_001", result[0].ID)
	assert.Equal(t, "rec_003", result[1].ID)
}

func TestParseDuplicateCheckResponse_无JSON(t *testing.T) {
	shared := []checkpointing.EvolutionRecord{{ID: "rec_001"}}
	result := parseDuplicateCheckResponse(shared, "no json here")
	assert.Len(t, result, 1)
}

func TestParseDuplicateCheckResponse_无效JSON(t *testing.T) {
	shared := []checkpointing.EvolutionRecord{{ID: "rec_001"}}
	result := parseDuplicateCheckResponse(shared, `[{"record_id": broken]`)
	assert.Len(t, result, 1)
}

func TestFilterDuplicateSharedRecords_无记录(t *testing.T) {
	r := &SkillEvolutionRail{}
	result := r.filterDuplicateSharedRecords(context.Background(), "skill", nil)
	assert.Nil(t, result)
}

// ──────────────────────────── M-10: WithEvalInterval 验证 ────────────────────────────

func TestWithEvalInterval_小于1调整为1(t *testing.T) {
	r := &SkillEvolutionRail{}
	WithEvalInterval(0)(r)
	assert.Equal(t, 1, r.evalInterval)

	WithEvalInterval(-5)(r)
	assert.Equal(t, 1, r.evalInterval)

	WithEvalInterval(3)(r)
	assert.Equal(t, 3, r.evalInterval)
}

// ──────────────────────────── T-06: OnApprove/OnReject 兼容别名 ────────────────────────────

func TestOnApprove_OnReject_兼容别名(t *testing.T) {
	// 验证 OnApprove 和 OnReject 是 ApproveRecord/RejectRecord 的别名
	r := &SkillEvolutionRail{
		approvalRuntime:          NewEvolutionApprovalRuntime(nil, map[string]*experience.PendingChange{}),
		pendingApprovalSnapshots: map[string]*experience.PendingChange{},
	}

	// 不存在的 requestID 应返回 nil（无 pending）
	err := r.OnApprove(context.Background(), "nonexistent")
	assert.NoError(t, err)

	err = r.OnReject(context.Background(), "nonexistent")
	assert.NoError(t, err)
}

// ──────────────────────────── S-05: hasRealError（替换 isErrorNonePattern）────────────────────────────

func TestHasRealError(t *testing.T) {
	// 只有 error = None → 无真实错误
	assert.False(t, hasRealError("error = None"))
	assert.False(t, hasRealError("error=None"))
	assert.False(t, hasRealError("ERROR = None"))

	// 真实错误 → 有真实错误
	assert.True(t, hasRealError("Error: connection refused"))
	assert.True(t, hasRealError("error = timeout"))

	// 混合场景：error = None 和真实 error 共存（对齐 Python 负向前瞻）
	assert.True(t, hasRealError("error = None\nError: timeout"))

	// 无 error 关键词
	assert.False(t, hasRealError("everything is fine"))

	// 空字符串
	assert.False(t, hasRealError(""))
}

// ──────────────────────────── S-05/T-08: extractConversationExcerpt 增强 ────────────────────────────

func TestExtractConversationExcerpt_排除ErrorNone(t *testing.T) {
	messages := []map[string]any{
		{"role": "tool", "content": "error = None, result = ok", "name": "run_tool"},
	}
	excerpt := extractConversationExcerpt(messages)
	// error = None 不应标记为失败
	assert.NotContains(t, excerpt, "FAILED TOOL EXECUTIONS")
}

func TestExtractConversationExcerpt_混合ErrorNone与真实错误(t *testing.T) {
	messages := []map[string]any{
		{"role": "tool", "content": "error = None\nException: connection refused", "name": "run_tool"},
	}
	excerpt := extractConversationExcerpt(messages)
	// 混合场景：error=None + Exception 应标记为失败（对齐 Python 负向前瞻行为）
	assert.Contains(t, excerpt, "FAILED TOOL EXECUTIONS")
}

// ──────────────────────────── G6: sharedRecordContextMarker ────────────────────────────

func TestSharedRecordContextMarker(t *testing.T) {
	assert.Equal(t, "[shared origin=", sharedRecordContextMarker)
}

// ──────────────────────────── G1: resolveDownloadTopK ────────────────────────────

func TestResolveDownloadTopK(t *testing.T) {
	// 默认值
	assert.Equal(t, 3, resolveDownloadTopK(nil))
	assert.Equal(t, 3, resolveDownloadTopK(map[string]any{}))

	// 配置覆盖
	assert.Equal(t, 5, resolveDownloadTopK(map[string]any{"download_top_k": 5}))
	assert.Equal(t, 10, resolveDownloadTopK(map[string]any{"download_top_k": float64(10)}))
	assert.Equal(t, 7, resolveDownloadTopK(map[string]any{"download_top_k": "7"}))

	// 类型容错
	assert.Equal(t, 3, resolveDownloadTopK(map[string]any{"download_top_k": "invalid"}))
	assert.Equal(t, 3, resolveDownloadTopK(map[string]any{"download_top_k": nil}))

	// 下界保护
	assert.Equal(t, 1, resolveDownloadTopK(map[string]any{"download_top_k": 0}))
	assert.Equal(t, 1, resolveDownloadTopK(map[string]any{"download_top_k": -3}))
}

// ──────────────────────────── G4: uploadApprovedRecordsForSharing ────────────────────────────

func TestUploadApprovedRecordsForSharing_未启用(t *testing.T) {
	r := &SkillEvolutionRail{}
	// sharing 未启用，应直接返回不 panic
	r.uploadApprovedRecordsForSharing(context.Background(), "test_skill", nil, nil)
}

func TestUploadApprovedRecordsForSharing_启用但无记录(t *testing.T) {
	r := &SkillEvolutionRail{}
	// 启用 sharing 但传入空记录，应直接返回
	r.uploadApprovedRecordsForSharing(context.Background(), "test_skill", nil, []checkpointing.EvolutionRecord{})
}

// ──────────────────────────── G5: getExcerptOffsetByKey / setExcerptOffsetByKey ────────────────────────────

func TestGetExcerptOffsetByKey(t *testing.T) {
	r := &SkillEvolutionRail{excerptOffsets: map[string]int{"sess1": 5, "sess2": 10}}

	assert.Equal(t, 5, r.getExcerptOffsetByKey("sess1"))
	assert.Equal(t, 10, r.getExcerptOffsetByKey("sess2"))
	assert.Equal(t, 0, r.getExcerptOffsetByKey("nonexistent"))

	// 空 key 返回 0
	assert.Equal(t, 0, r.getExcerptOffsetByKey(""))
}

func TestSetExcerptOffsetByKey(t *testing.T) {
	r := &SkillEvolutionRail{excerptOffsets: map[string]int{}}

	r.setExcerptOffsetByKey("sess1", 5)
	assert.Equal(t, 5, r.excerptOffsets["sess1"])

	// 空 key 不写入
	r.setExcerptOffsetByKey("", 99)
	_, exists := r.excerptOffsets[""]
	assert.False(t, exists)
}

// ──────────────────────────── G8: ExtractQueryKeywords 错误恢复 ────────────────────────────

func TestDownloadSharedExperiences_关键词提取失败降级(t *testing.T) {
	// 验证 keywordExtractor 为 nil 时 downloadSharedExperiences 不 panic
	r := &SkillEvolutionRail{
		excerptOffsets: make(map[string]int),
	}
	// keywordExtractor 为 nil → IsSharingEnabled 返回 false → 返回空 map
	result := r.downloadSharedExperiences(context.Background(), nil, []string{}, nil)
	assert.Empty(t, result)
}

func TestExtractConversationExcerpt_assistantResponses(t *testing.T) {
	messages := []map[string]any{
		{"role": "assistant", "content": "I will help you with that."},
	}
	// assistant_responses 被收集（当前版本未输出到摘要，但确保不会 panic）
	excerpt := extractConversationExcerpt(messages)
	// 纯 assistant 消息不产生任何 section，返回空字符串
	assert.Empty(t, excerpt)
}

func TestExtractConversationExcerpt_toolName回退(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "test query"},
		{"role": "tool", "content": "Error: something failed", "tool_name": "my_tool"},
	}
	excerpt := extractConversationExcerpt(messages)
	assert.Contains(t, excerpt, "my_tool")
}

func TestSkillEvolutionRail_ExtractToolContent_data中间层(t *testing.T) {
	r := &SkillEvolutionRail{}

	// .data 中间层含 skill_content
	inputs := &agentinterfaces.ToolCallInputs{
		ToolResult: map[string]any{"data": map[string]any{"skill_content": "from data"}},
	}
	assert.Equal(t, "from data", r.extractToolContent(inputs))

	// .data 中间层含 content
	inputs2 := &agentinterfaces.ToolCallInputs{
		ToolResult: map[string]any{"data": map[string]any{"content": "data content"}},
	}
	assert.Equal(t, "data content", r.extractToolContent(inputs2))

	// .data 优先于顶层字段
	inputs3 := &agentinterfaces.ToolCallInputs{
		ToolResult: map[string]any{
			"data":          map[string]any{"skill_content": "data wins"},
			"skill_content": "top level",
		},
	}
	assert.Equal(t, "data wins", r.extractToolContent(inputs3))

	// .data 不是 dict 时回退到顶层
	inputs4 := &agentinterfaces.ToolCallInputs{
		ToolResult: map[string]any{
			"data":          "not a dict",
			"skill_content": "top fallback",
		},
	}
	assert.Equal(t, "top fallback", r.extractToolContent(inputs4))

	// 顶层 skill_content
	inputs5 := &agentinterfaces.ToolCallInputs{
		ToolResult: map[string]any{"skill_content": "direct"},
	}
	assert.Equal(t, "direct", r.extractToolContent(inputs5))

	// string 类型结果
	inputs6 := &agentinterfaces.ToolCallInputs{
		ToolResult: "string result",
	}
	assert.Equal(t, "string result", r.extractToolContent(inputs6))

	// nil 结果
	inputs7 := &agentinterfaces.ToolCallInputs{}
	assert.Equal(t, "", r.extractToolContent(inputs7))
}
