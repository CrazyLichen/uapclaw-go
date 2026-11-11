package recommendation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── prompt 双语对拍 ────────────────────────────

// TestUnifiedAnalysisPrompt_双语 按语言返回对应模板，未渲染前含占位符。
func TestUnifiedAnalysisPrompt_双语(t *testing.T) {
	zh := UnifiedAnalysisPrompt("zh")
	en := UnifiedAnalysisPrompt("en")

	assert.Contains(t, zh, "你是用户洞察与推荐助手", "zh 模板应含中文开头")
	assert.Contains(t, zh, "{conversation_summary}", "zh 模板应含未渲染占位符")
	assert.Contains(t, zh, "{decision_rules_text}")

	assert.Contains(t, en, "You are a user-insight and recommendation assistant")
	assert.Contains(t, en, "{conversation_summary}")
	assert.Contains(t, en, "{decision_rules_text}")

	assert.NotEqual(t, zh, en, "zh 与 en 必须是不同模板")
}

// TestUnifiedAnalysisPrompt_语言归一 zh 系（空/大写/空白）都回落中文。
func TestUnifiedAnalysisPrompt_语言归一(t *testing.T) {
	zh := UnifiedAnalysisPrompt("zh")
	for _, lang := range []string{"", "  ", "ZH", "Zh", "cn", "chinese"} {
		assert.Equal(t, zh, UnifiedAnalysisPrompt(lang), "language=%q 应回落中文模板", lang)
	}
	assert.Equal(t, UnifiedAnalysisPrompt("en"), UnifiedAnalysisPrompt("EN"))
	assert.Equal(t, UnifiedAnalysisPrompt("en"), UnifiedAnalysisPrompt(" en "))
}

// TestDirectivePrompt_双语 话术指令模板双语切换。
func TestDirectivePrompt_双语(t *testing.T) {
	zh := DirectivePrompt("zh")
	en := DirectivePrompt("en")

	assert.Contains(t, zh, "[主动推荐指令]")
	assert.Contains(t, zh, "{rec_type}")
	assert.Contains(t, zh, "{target}")
	assert.Contains(t, zh, "{reason}")
	assert.Contains(t, zh, "{style_rules_section}")

	assert.Contains(t, en, "[Proactive recommendation directive]")
	for _, placeholder := range []string{"{rec_type}", "{target}", "{reason}", "{style_rules_section}"} {
		assert.Contains(t, en, placeholder)
	}
	assert.NotEqual(t, zh, en)
}

// TestPrompts_逐字复刻关键段落 上游 prompt 的关键约束不得被改写或本地化。
// 这些段落一旦漂移，决策/话术语义会变，且上游每次变更都无法对照。
func TestPrompts_逐字复刻关键段落(t *testing.T) {
	zhAnalysis := UnifiedAnalysisPrompt("zh")
	// 三类推荐的核心语义
	assert.Contains(t, zhAnalysis, `"task_reminder"：提醒用户没做完、没闭环或快到点的事。`)
	assert.Contains(t, zhAnalysis, `"skill_recommend"：从解决用户已有任务`)
	assert.Contains(t, zhAnalysis, `"need_exploration"：从已有内容延申出用户可能感兴趣的新话题。`)
	// 去重锚点：skill 是按「skill + 用它做的事」而非 skill 名
	assert.Contains(t, zhAnalysis, `去重锚点是「skill + 它用来做的事」，不是 skill 名本身`)
	// task_reminder 的 reason 必须逐字引用
	assert.Contains(t, zhAnalysis, `task_reminder 的 reason 必须逐字引用依据来源`)

	enAnalysis := UnifiedAnalysisPrompt("en")
	assert.Contains(t, enAnalysis, `the dedup anchor is "the skill + what it is used for", not the`)
	assert.Contains(t, enAnalysis, `For task_reminder, reason MUST quote its source verbatim`)

	zhDirective := DirectivePrompt("zh")
	// 硬约束：话术必须包含 target 字面内容
	assert.Contains(t, zhDirective, `话术必须自然地包含「推荐内容」的字面内容，这是硬约束`)
	// directive 本身不进历史，skill 名只能靠话术带进 context
	assert.Contains(t, zhDirective, `directive 指令本身不进历史`)
}

// ──────────────────────────── RenderPromptTemplate ────────────────────────────

// TestRenderPromptTemplate_占位替换 命名占位符被替换，未提供的保持不变。
func TestRenderPromptTemplate_占位替换(t *testing.T) {
	tpl := "A={a} B={b} C={c}"
	got := RenderPromptTemplate(tpl, map[string]string{"a": "1", "b": "2"})
	assert.Equal(t, "A=1 B=2 C={c}", got, "未提供的占位符应原样保留")
}

// TestRenderPromptTemplate_双花括号转义 Python format 的 {{ }} 转义还原为单个花括号。
func TestRenderPromptTemplate_双花括号转义(t *testing.T) {
	tpl := `{"decision": {{"type": "{t}"}}, "done": true}`
	got := RenderPromptTemplate(tpl, map[string]string{"t": "X"})
	assert.Equal(t, `{"decision": {"type": "X"}, "done": true}`, got,
		"{{ }} 应还原成 { }，{t} 应被替换")
}

// TestRenderPromptTemplate_替换值含花括号 替换值里的花括号不应被后续替换影响。
// 这条守护「替换顺序」——先替换占位符再还原哨兵，避免替换值被再次展开。
func TestRenderPromptTemplate_替换值含花括号(t *testing.T) {
	tpl := "X={x} Y={y}"
	got := RenderPromptTemplate(tpl, map[string]string{
		"x": `{"nested": 1}`,
		"y": "{x}",
	})
	assert.Equal(t, `X={"nested": 1} Y={x}`, got, "替换值内容不应被二次展开")
}

// TestRenderPromptTemplate_真实决策prompt 用真实模板验证渲染后可解析为 JSON 形态。
func TestRenderPromptTemplate_真实决策prompt(t *testing.T) {
	got := RenderPromptTemplate(UnifiedAnalysisPrompt("zh"), map[string]string{
		"conversation_summary": "## 当前对话\n[User]: 帮我做个PPT",
		"decision_rules_text":  "- [target] 避免推荐日程类话题",
	})
	assert.Contains(t, got, "## 当前对话")
	assert.Contains(t, got, "避免推荐日程类话题")
	// 模板里的 {{ }} 应还原成单花括号，形成合法 JSON 示例
	assert.Contains(t, got, `"decision": {`)
	assert.NotContains(t, got, "{{", "渲染后不应残留双花括号")
	assert.NotContains(t, got, "}}", "渲染后不应残留双花括号")
}

// ──────────────────────────── SafeUrgency ────────────────────────────

// TestSafeUrgency 各种异常输入回落默认值，正常输入夹到 [0,1]。
// Python: proactive_actions.py:73 _safe_urgency
func TestSafeUrgency(t *testing.T) {
	cases := []struct {
		name     string
		input    any
		want     float64
		fallback float64
	}{
		{"正常浮点", 0.8, 0.8, 0.5},
		{"零", 0.0, 0.0, 0.5},
		{"上界", 1.0, 1.0, 0.5},
		{"超上界夹到1", 1.5, 1.0, 0.5},
		{"负数回落", -0.1, 0.5, 0.5},
		{"字符串数字", "0.9", 0.9, 0.5},
		{"字符串负数回落", "-0.5", 0.5, 0.5},
		{"非数字字符串回落", "abc", 0.5, 0.5},
		{"空串回落", "", 0.5, 0.5},
		{"nil回落", nil, 0.5, 0.5},
		{"bool回落", true, 0.5, 0.5},
		{"int", 1, 1.0, 0.5},
		{"自定义默认值", "abc", 0.3, 0.3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, SafeUrgency(tc.input, tc.fallback))
		})
	}
}

// TestSafeUrgency_NaN NaN 必须回落默认值（Python: f != f 判断）。
func TestSafeUrgency_NaN(t *testing.T) {
	nan := 0.0
	nan = nan / nan // 构造 NaN，避免依赖 math 常量
	assert.Equal(t, 0.5, SafeUrgency(nan, 0.5), "NaN 应回落默认值")
}

// ──────────────────────────── ExtractJSONFromResponse ────────────────────────────

// TestExtractJSONFromResponse 三种形态：fence / 裸花括号 / 尾部脏文本。
// 注意本函数用贪婪正则，与 gradient_updater 的 raw_decode 版本语义不同。
// Python: proactive_actions.py:225
func TestExtractJSONFromResponse(t *testing.T) {
	t.Run("json代码块", func(t *testing.T) {
		text := "这是说明\n```json\n{\"decision\": null}\n```\n尾部"
		assert.Equal(t, `{"decision": null}`, ExtractJSONFromResponse(text))
	})

	t.Run("无语言标记的代码块", func(t *testing.T) {
		text := "```\n{\"a\": 1}\n```"
		assert.Equal(t, `{"a": 1}`, ExtractJSONFromResponse(text))
	})

	t.Run("裸花括号", func(t *testing.T) {
		text := `前缀文本 {"decision": {"type": "task_reminder"}} 后缀文本`
		assert.Equal(t, `{"decision": {"type": "task_reminder"}}`, ExtractJSONFromResponse(text))
	})

	t.Run("尾部含花括号的脏文本", func(t *testing.T) {
		// 贪婪正则会从首个 { 吃到末个 }，把尾部脏文本也包含进来
		// （这正是与 gradient_updater 的 raw_decode 版本的关键差异，不可合并）
		text := `{"a": 1} 说明中含 } 字符`
		got := ExtractJSONFromResponse(text)
		assert.Contains(t, got, `{"a": 1}`)
	})

	t.Run("无JSON返回空", func(t *testing.T) {
		assert.Equal(t, "", ExtractJSONFromResponse("完全没有 JSON 的文本"))
		assert.Equal(t, "", ExtractJSONFromResponse(""))
	})

	t.Run("优先代码块", func(t *testing.T) {
		text := `前 {"outside": 1} 中` + "\n```json\n{\"inside\": 2}\n```"
		assert.Equal(t, `{"inside": 2}`, ExtractJSONFromResponse(text),
			"有代码块时应优先取代码块内容")
	})
}

// ──────────────────────────── ExtractOutputText ────────────────────────────

// TestExtractOutputText 严格只读 output，缺失/类型不符返回空串。
// Python: proactive_actions.py:202
func TestExtractOutputText(t *testing.T) {
	t.Run("字符串output", func(t *testing.T) {
		assert.Equal(t, "hello", ExtractOutputText(map[string]any{"output": "hello"}))
	})

	t.Run("列表output按行拼接", func(t *testing.T) {
		got := ExtractOutputText(map[string]any{"output": []any{"a", "b", 3}})
		assert.Equal(t, "a\nb", got, "非字符串元素应被跳过")
	})

	t.Run("output缺失返回空", func(t *testing.T) {
		assert.Equal(t, "", ExtractOutputText(map[string]any{"result_type": "answer"}),
			"不得把 result_type 当输出")
	})

	t.Run("output类型不符返回空", func(t *testing.T) {
		assert.Equal(t, "", ExtractOutputText(map[string]any{"output": 123}))
	})

	t.Run("nil返回空", func(t *testing.T) {
		assert.Equal(t, "", ExtractOutputText(nil))
	})
}

// ──────────────────────────── ParseProactiveSkillMd ────────────────────────────

// TestParseProactiveSkillMd_三种frontmatter 覆盖标准 YAML / 非标准行 / 无 frontmatter。
// Python: proactive_actions.py:100
func TestParseProactiveSkillMd_三种frontmatter(t *testing.T) {
	t.Run("标准YAML frontmatter", func(t *testing.T) {
		path := writeTempSkillMd(t, "my-skill", "---\nname: my-skill\ndescription: 一个测试技能\n---\n# 标题\n正文内容")
		meta := ParseProactiveSkillMd(path)
		require.NotNil(t, meta)
		assert.Equal(t, "my-skill", meta["name"])
		assert.Equal(t, "一个测试技能", meta["description"])
	})

	t.Run("无frontmatter用文件名与正文", func(t *testing.T) {
		path := writeTempSkillMd(t, "plain-skill", "# 只有正文\n没有 frontmatter")
		meta := ParseProactiveSkillMd(path)
		require.NotNil(t, meta)
		// Python: Path.stem —— 参数是 SKILL.md 的路径，故 stem 得 "SKILL"（上游真实行为）
		assert.Equal(t, "SKILL", meta["name"], "无 frontmatter 时 name 取 Path.stem")
		assert.Contains(t, meta["description"], "只有正文")
	})

	t.Run("frontmatter缺name用文件名", func(t *testing.T) {
		path := writeTempSkillMd(t, "fallback-skill", "---\ndescription: 只有描述\n---\n正文")
		meta := ParseProactiveSkillMd(path)
		require.NotNil(t, meta)
		assert.Equal(t, "SKILL", meta["name"], "缺 name 时回落 Path.stem")
		assert.Equal(t, "只有描述", meta["description"])
	})

	t.Run("frontmatter缺description用正文", func(t *testing.T) {
		path := writeTempSkillMd(t, "no-desc", "---\nname: no-desc\n---\n正文用作描述")
		meta := ParseProactiveSkillMd(path)
		require.NotNil(t, meta)
		assert.Equal(t, "no-desc", meta["name"])
		assert.Contains(t, meta["description"], "正文用作描述")
	})

	t.Run("文件不存在返回nil", func(t *testing.T) {
		assert.Nil(t, ParseProactiveSkillMd(filepath.Join(t.TempDir(), "nope.md")))
	})

	t.Run("描述超500字符被截断", func(t *testing.T) {
		long := strings.Repeat("字", 600)
		path := writeTempSkillMd(t, "long-desc", "# 标题\n"+long)
		meta := ParseProactiveSkillMd(path)
		require.NotNil(t, meta)
		desc, _ := meta["description"].(string)
		assert.Equal(t, 500, len([]rune(desc)), "描述应截断到 500 个字符")
	})
}

// writeTempSkillMd 在临时目录写一个 SKILL.md 并返回路径。
func writeTempSkillMd(t *testing.T, dirName, content string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), dirName)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "SKILL.md")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// ──────────────────────────── RecommendationState ────────────────────────────

// TestRecommendationState_AddRecommendation 追加记录并截断到 20 条。
// Python: profile_extractor.py:48
func TestRecommendationState_AddRecommendation(t *testing.T) {
	state := &RecommendationState{}
	for i := 0; i < 25; i++ {
		state.AddRecommendation(map[string]any{"id": i})
	}
	require.Len(t, state.RecommendationHistory, maxRecommendationHistory,
		"应截断到 20 条")
	assert.Equal(t, 5, state.RecommendationHistory[0]["id"],
		"应保留最新 20 条（最早的 0~4 被丢弃）")
	assert.Equal(t, 24, state.RecommendationHistory[19]["id"])
}

// TestRecommendationState_Touch 更新时间戳。
func TestRecommendationState_Touch(t *testing.T) {
	state := &RecommendationState{}
	assert.Empty(t, state.LastUpdated)
	state.Touch()
	assert.NotEmpty(t, state.LastUpdated)
	_, err := parseRFC3339(state.LastUpdated)
	assert.NoError(t, err, "LastUpdated 应是合法 RFC3339")
}

// TestLoadSaveRecommendationState_往返 保存后加载内容一致。
// Python: profile_extractor.py:67/100
func TestLoadSaveRecommendationState_往返(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recommendation.json")
	original := &RecommendationState{
		RecommendationHistory: []map[string]any{
			{"id": "rec_1", "type": "skill_recommend", "target": "翻译技能", "urgency": 0.8},
		},
		FeedbackBuffer: []map[string]any{
			{"rec_id": "rec_1", "feedback_type": "explicit_like"},
		},
		StrategyGradients: []map[string]any{
			{"gradient_id": "g_1", "rule": "语气简洁", "category": "tone"},
		},
	}
	original.Touch()
	SaveRecommendationState(original, path)

	loaded := LoadRecommendationState(path)
	require.Len(t, loaded.RecommendationHistory, 1)
	assert.Equal(t, "rec_1", loaded.RecommendationHistory[0]["id"])
	assert.Equal(t, "翻译技能", loaded.RecommendationHistory[0]["target"])
	require.Len(t, loaded.FeedbackBuffer, 1)
	assert.Equal(t, "explicit_like", loaded.FeedbackBuffer[0]["feedback_type"])
	require.Len(t, loaded.StrategyGradients, 1)
	assert.Equal(t, "语气简洁", loaded.StrategyGradients[0]["rule"])
	assert.NotEmpty(t, loaded.LastUpdated)
}

// TestLoadRecommendationState_降级 文件缺失/空/损坏/非对象/字段类型错都返回空状态。
// Python: profile_extractor.py:70-97 的 isinstance 守卫
func TestLoadRecommendationState_降级(t *testing.T) {
	t.Run("文件不存在", func(t *testing.T) {
		state := LoadRecommendationState(filepath.Join(t.TempDir(), "absent.json"))
		assert.NotNil(t, state)
		assert.Empty(t, state.RecommendationHistory)
	})

	t.Run("空文件", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.json")
		require.NoError(t, os.WriteFile(path, []byte{}, 0o644))
		state := LoadRecommendationState(path)
		assert.NotNil(t, state)
		assert.Empty(t, state.RecommendationHistory)
	})

	t.Run("损坏JSON", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "broken.json")
		require.NoError(t, os.WriteFile(path, []byte("{不是合法json"), 0o644))
		state := LoadRecommendationState(path)
		assert.NotNil(t, state)
		assert.Empty(t, state.RecommendationHistory)
	})

	t.Run("JSON是数组不是对象", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "array.json")
		require.NoError(t, os.WriteFile(path, []byte(`[1,2,3]`), 0o644))
		state := LoadRecommendationState(path)
		assert.NotNil(t, state)
		assert.Empty(t, state.RecommendationHistory)
	})

	t.Run("字段类型错误回落空列表", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "wrongtype.json")
		payload := `{"recommendation_history": "not-a-list", "feedback_buffer": 123, "strategy_gradients": {"a":1}, "last_updated": 456}`
		require.NoError(t, os.WriteFile(path, []byte(payload), 0o644))
		state := LoadRecommendationState(path)
		assert.NotNil(t, state)
		assert.Empty(t, state.RecommendationHistory, "非数组应回落空列表")
		assert.Empty(t, state.FeedbackBuffer)
		assert.Empty(t, state.StrategyGradients)
		assert.Empty(t, state.LastUpdated, "非字符串应回落空串")
	})

	t.Run("数组里混入非对象元素被跳过", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "mixed.json")
		payload := `{"recommendation_history": [{"id":"a"}, "字符串", 123, {"id":"b"}]}`
		require.NoError(t, os.WriteFile(path, []byte(payload), 0o644))
		state := LoadRecommendationState(path)
		require.Len(t, state.RecommendationHistory, 2, "只保留对象元素")
		assert.Equal(t, "a", state.RecommendationHistory[0]["id"])
		assert.Equal(t, "b", state.RecommendationHistory[1]["id"])
	})
}

// TestSaveRecommendationState_空状态写空数组 Python asdict 对 list 字段输出 [] 而非 null。
func TestSaveRecommendationState_空状态写空数组(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty_state.json")
	SaveRecommendationState(&RecommendationState{}, path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	for _, key := range []string{"recommendation_history", "feedback_buffer", "strategy_gradients"} {
		assert.NotNil(t, parsed[key], "%s 不应是 null", key)
		list, ok := parsed[key].([]any)
		assert.True(t, ok, "%s 应是数组", key)
		assert.Empty(t, list)
	}
}

// TestSaveRecommendationState_不转义HTML 中文与特殊字符原样写入（对齐 ensure_ascii=False）。
func TestSaveRecommendationState_不转义HTML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unescaped.json")
	SaveRecommendationState(&RecommendationState{
		RecommendationHistory: []map[string]any{
			{"content": "中文 <b>标签</b> & 符号"},
		},
	}, path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "中文", "中文不应被转义")
	assert.Contains(t, string(data), "<b>", "HTML 不应被转义")
	assert.NotContains(t, string(data), `\u`, "不应出现 unicode 转义")
}

// ──────────────────────────── 每日计数 ────────────────────────────

// TestDailyCount 计数递增、读取、清理。
// Python: proactive_actions.py:36-60
func TestDailyCount(t *testing.T) {
	ResetDailyCounts()
	t.Cleanup(ResetDailyCounts)

	assert.Equal(t, 0, TodayRecommendCount(), "初始应为 0")
	assert.Equal(t, 1, IncrementDailyCount())
	assert.Equal(t, 2, IncrementDailyCount())
	assert.Equal(t, 2, TodayRecommendCount())

	// 塞一个 2 天前的 key，Prune 应清掉它
	dailyCountsMu.Lock()
	dailyCounts["2000-01-01"] = 99
	dailyCountsMu.Unlock()

	PruneDailyCounts()

	dailyCountsMu.Lock()
	_, staleExists := dailyCounts["2000-01-01"]
	dailyCountsMu.Unlock()
	assert.False(t, staleExists, "过期 key 应被清理")
	assert.Equal(t, 2, TodayRecommendCount(), "当日计数不应被清理")
}

// TestTodayKey 日期键格式为 YYYY-MM-DD。
func TestTodayKey(t *testing.T) {
	key := todayKey()
	assert.Len(t, key, 10)
	_, err := parseDateOnly(key)
	assert.NoError(t, err)
}

// ──────────────────────────── SafeProactiveInt ────────────────────────────

// TestSafeProactiveInt 合法区间 [1,50]，非数字/越界回落默认值。
// Python: proactive_engine.py:52
func TestSafeProactiveInt(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  int
	}{
		{"正常值", 5, 5},
		{"下界", 1, 1},
		{"上界", 50, 50},
		{"超出上界", 51, 20},
		{"下界以下", 0, 20},
		{"负数", -3, 20},
		{"字符串数字", "10", 10},
		{"浮点字符串", "3.5", 20},
		{"科学计数", "1e5", 20},
		{"非数字字符串", "abc", 20},
		{"空串", "", 20},
		{"nil", nil, 20},
		{"bool", true, 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, SafeProactiveInt(tc.input, 20))
		})
	}
}
