package recommendation

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── prompt 双语与逐字复刻 ────────────────────────────

// TestGradientUpdatePrompt_双语 统一模板双语切换，含未渲染占位符。
func TestGradientUpdatePrompt_双语(t *testing.T) {
	zh := GradientUpdatePrompt("zh")
	en := GradientUpdatePrompt("en")

	assert.Contains(t, zh, "你是推荐策略分析师")
	assert.Contains(t, zh, "{feedbacks_text}")
	assert.Contains(t, zh, "{existing_rules_text}")

	assert.Contains(t, en, "You are a recommendation-strategy analyst")
	assert.Contains(t, en, "{feedbacks_text}")

	assert.NotEqual(t, zh, en)
	assert.Equal(t, zh, GradientUpdatePrompt(""), "空语言回落中文")
	assert.Equal(t, en, GradientUpdatePrompt("EN"), "语言大小写不敏感")
	assert.Equal(t, en, GradientUpdatePrompt("  en  "))
}

// TestGradientPrompts_逐字复刻关键段落 上游 prompt 的关键约束不得漂移。
func TestGradientPrompts_逐字复刻关键段落(t *testing.T) {
	// 统一模板：规则分类与长度约束
	zh := GradientUpdatePrompt("zh")
	assert.Contains(t, zh, "- target: 关于\"推什么\"")
	assert.Contains(t, zh, "- relation: 关于\"怎么关联\"")
	assert.Contains(t, zh, "- tone: 关于\"语气风格\"")
	assert.Contains(t, zh, "- structure: 关于\"话术结构\"")
	assert.Contains(t, zh, "每条规则一句话，不超过25字。")
	assert.Contains(t, zh, "⚠️ 规则必须用简体中文书写。")

	// 显式反馈专用：泛化保守约束
	assert.Contains(t, explicitGradientPromptZH, "⚠️ 泛化保守（关键）：")
	assert.Contains(t, explicitGradientPromptZH, "不泛化成\"避免文案生成类话题\"")
	assert.Contains(t, explicitGradientPromptZH, "- 不要生成 tone/structure/relation 类型的规则")
	assert.Contains(t, explicitGradientPromptEN, "⚠️ Conservative generalization (critical):")

	// 隐式反馈专用：情感未预分类 + 相关性检查
	assert.Contains(t, implicitGradientPromptZH, "情感未预分类")
	assert.Contains(t, implicitGradientPromptZH, "⚠️ 相关性判断（配合砍掉的时间窗）：")
	assert.Contains(t, implicitGradientPromptZH, "中性回复返回空 operations")
	assert.Contains(t, implicitGradientPromptEN, "sentiment NOT pre-classified")
	assert.Contains(t, implicitGradientPromptEN, "⚠️ Relevance check (paired with the removed time window):")
}

// ──────────────────────────── ApplyOperations 测试分组 ────────────────────────────

// TestApplyOperations_add 新增梯度，空 rule 被过滤。
// Python: gradient_updater.py:229
func TestApplyOperations_add(t *testing.T) {
	t.Run("正常add", func(t *testing.T) {
		got := ApplyOperations(nil, []map[string]any{
			{"action": "add", "rule": "避免推荐日程", "category": "target"},
		})
		require.Len(t, got, 1)
		assert.Equal(t, "避免推荐日程", got[0]["rule"])
		assert.Equal(t, "target", got[0]["category"])
		assert.NotEmpty(t, got[0]["gradient_id"])
	})

	t.Run("空rule被跳过", func(t *testing.T) {
		got := ApplyOperations(nil, []map[string]any{
			{"action": "add", "rule": "   ", "category": "target"},
			{"action": "add", "rule": "", "category": "tone"},
		})
		assert.Empty(t, got, "空规则不应产生梯度")
	})

	t.Run("缺category默认tone", func(t *testing.T) {
		got := ApplyOperations(nil, []map[string]any{
			{"action": "add", "rule": "简洁"},
		})
		require.Len(t, got, 1)
		assert.Equal(t, defaultGradientCategory, got[0]["category"])
	})

	t.Run("多条add顺序保持", func(t *testing.T) {
		got := ApplyOperations(nil, []map[string]any{
			{"action": "add", "rule": "规则1", "category": "target"},
			{"action": "add", "rule": "规则2", "category": "tone"},
			{"action": "add", "rule": "规则3", "category": "relation"},
		})
		require.Len(t, got, 3)
		assert.Equal(t, "规则1", got[0]["rule"])
		assert.Equal(t, "规则2", got[1]["rule"])
		assert.Equal(t, "规则3", got[2]["rule"])
	})
}

// TestApplyOperations_revise revise 修改内容并挪到末尾（反映新近度）。
func TestApplyOperations_revise(t *testing.T) {
	t.Run("修改并挪到末尾", func(t *testing.T) {
		existing := []map[string]any{
			{"gradient_id": "g1", "rule": "规则1", "category": "target"},
			{"gradient_id": "g2", "rule": "规则2", "category": "tone"},
			{"gradient_id": "g3", "rule": "规则3", "category": "relation"},
		}
		got := ApplyOperations(existing, []map[string]any{
			{"action": "revise", "gradient_id": "g1", "rule": "规则1改", "category": "target"},
		})
		require.Len(t, got, 3)
		assert.Equal(t, "g2", got[0]["gradient_id"], "g1 挪到末尾后 g2 排第一")
		assert.Equal(t, "g1", got[2]["gradient_id"], "被 revise 的项排最后")
		assert.Equal(t, "规则1改", got[2]["rule"], "内容应被修改")
	})

	t.Run("只改rule时category保留", func(t *testing.T) {
		existing := []map[string]any{
			{"gradient_id": "g1", "rule": "旧规则", "category": "target"},
		}
		got := ApplyOperations(existing, []map[string]any{
			{"action": "revise", "gradient_id": "g1", "rule": "新规则"},
		})
		require.Len(t, got, 1)
		assert.Equal(t, "新规则", got[0]["rule"])
		assert.Equal(t, "target", got[0]["category"], "未提供 category 应保留原值")
	})

	t.Run("目标不存在时忽略", func(t *testing.T) {
		existing := []map[string]any{
			{"gradient_id": "g1", "rule": "规则1", "category": "target"},
		}
		got := ApplyOperations(existing, []map[string]any{
			{"action": "revise", "gradient_id": "不存在的id", "rule": "x"},
		})
		require.Len(t, got, 1)
		assert.Equal(t, "规则1", got[0]["rule"], "不应影响已有梯度")
	})
}

// TestApplyOperations_drop 删除梯度。
func TestApplyOperations_drop(t *testing.T) {
	t.Run("正常drop", func(t *testing.T) {
		existing := []map[string]any{
			{"gradient_id": "g1", "rule": "规则1", "category": "target"},
			{"gradient_id": "g2", "rule": "规则2", "category": "tone"},
		}
		got := ApplyOperations(existing, []map[string]any{
			{"action": "drop", "gradient_id": "g1"},
		})
		require.Len(t, got, 1)
		assert.Equal(t, "g2", got[0]["gradient_id"])
	})

	t.Run("目标不存在时忽略", func(t *testing.T) {
		existing := []map[string]any{
			{"gradient_id": "g1", "rule": "规则1", "category": "target"},
		}
		got := ApplyOperations(existing, []map[string]any{
			{"action": "drop", "gradient_id": "不存在"},
		})
		require.Len(t, got, 1)
	})

	t.Run("未知action被忽略", func(t *testing.T) {
		existing := []map[string]any{
			{"gradient_id": "g1", "rule": "规则1", "category": "target"},
		}
		got := ApplyOperations(existing, []map[string]any{
			{"action": "unknown_action"},
		})
		require.Len(t, got, 1)
	})
}

// TestApplyOperations_混合 三种操作混合执行的顺序正确性。
func TestApplyOperations_混合(t *testing.T) {
	existing := []map[string]any{
		{"gradient_id": "g1", "rule": "R1", "category": "target"},
		{"gradient_id": "g2", "rule": "R2", "category": "tone"},
	}
	got := ApplyOperations(existing, []map[string]any{
		{"action": "drop", "gradient_id": "g1"},
		{"action": "add", "rule": "R3", "category": "relation"},
		{"action": "revise", "gradient_id": "g2", "rule": "R2改"},
	})
	require.Len(t, got, 2)
	// 操作按顺序执行：drop(g1) → add(R3) → revise(g2)。
	// revise 会把 g2 挪到末尾，故最终顺序为 [R3, R2改]。
	assert.Equal(t, "R3", got[0]["rule"], "新增项先于被 revise 项")
	assert.Equal(t, "R2改", got[1]["rule"], "被 revise 的项排最后")
}

// TestApplyOperations_缺gradient_id的输入项被跳过。
func TestApplyOperations_输入项缺ID(t *testing.T) {
	existing := []map[string]any{
		{"rule": "无ID的规则", "category": "target"},
		{"gradient_id": "g1", "rule": "R1", "category": "target"},
	}
	got := ApplyOperations(existing, nil)
	require.Len(t, got, 1, "缺 gradient_id 的项无法索引，应被跳过")
	assert.Equal(t, "g1", got[0]["gradient_id"])
}

// ──────────────────────────── AttributeGradients 测试分组 ────────────────────────────

// TestAttributeGradients 决策层/话术层归类，未知分类归决策层。
// Python: gradient_updater.py:621
func TestAttributeGradients(t *testing.T) {
	gradients := []map[string]any{
		{"gradient_id": "g1", "category": "target"},
		{"gradient_id": "g2", "category": "relation"},
		{"gradient_id": "g3", "category": "tone"},
		{"gradient_id": "g4", "category": "structure"},
		{"gradient_id": "g5", "category": "unknown_category"},
		{"gradient_id": "g6"},
	}
	decision, style := AttributeGradients(gradients)

	require.Len(t, decision, 4, "target/relation/未知/缺分类都归决策层")
	assert.Equal(t, "g1", decision[0]["gradient_id"])
	assert.Equal(t, "g2", decision[1]["gradient_id"])
	assert.Equal(t, "g5", decision[2]["gradient_id"])
	assert.Equal(t, "g6", decision[3]["gradient_id"])

	require.Len(t, style, 2)
	assert.Equal(t, "g3", style[0]["gradient_id"])
	assert.Equal(t, "g4", style[1]["gradient_id"])
}

// TestAttributeGradients_空 空输入返回空切片不 panic。
func TestAttributeGradients_空(t *testing.T) {
	decision, style := AttributeGradients(nil)
	assert.Empty(t, decision)
	assert.Empty(t, style)
}

// ──────────────────────────── RenderDecisionRules / RenderStyleRules 测试分组 ────────────────────────────

// TestRenderDecisionRules 只含决策层规则，留最新 10 条。
// Python: gradient_updater.py:646
func TestRenderDecisionRules(t *testing.T) {
	t.Run("空规则", func(t *testing.T) {
		assert.Equal(t, "（暂无）", RenderDecisionRules(nil, "zh"))
		assert.Equal(t, "(none yet)", RenderDecisionRules(nil, "en"))
	})

	t.Run("只含决策层", func(t *testing.T) {
		gradients := []map[string]any{
			{"category": "target", "rule": "避免日程"},
			{"category": "tone", "rule": "简洁"},
			{"category": "relation", "rule": "引用原话"},
		}
		got := RenderDecisionRules(gradients, "zh")
		assert.Contains(t, got, "- [target] 避免日程")
		assert.Contains(t, got, "- [relation] 引用原话")
		assert.NotContains(t, got, "简洁", "话术层规则不应出现在决策层渲染里")
	})

	t.Run("留最新10条", func(t *testing.T) {
		gradients := make([]map[string]any, 0, 15)
		for i := 0; i < 15; i++ {
			gradients = append(gradients, map[string]any{
				"category": "target", "rule": "R" + string(rune('A'+i)),
			})
		}
		got := RenderDecisionRules(gradients, "zh")
		lines := len(splitNonEmpty(got))
		assert.Equal(t, 10, lines, "只喂最新 10 条")
		assert.Contains(t, got, "R"+string(rune('A'+14)), "最后一条应保留")
		assert.NotContains(t, got, "R"+string(rune('A'+4)), "第 5 条应被截掉")
	})

	t.Run("缺category用unknown", func(t *testing.T) {
		got := RenderDecisionRules([]map[string]any{{"rule": "无分类"}}, "zh")
		assert.Contains(t, got, "- [unknown] 无分类")
	})
}

// TestRenderStyleRules 只含话术层规则；无话术规则返回空串。
// Python: gradient_updater.py:661
func TestRenderStyleRules(t *testing.T) {
	t.Run("无话术规则返回空串", func(t *testing.T) {
		assert.Equal(t, "", RenderStyleRules(nil, "zh"))
		assert.Equal(t, "", RenderStyleRules([]map[string]any{{"category": "target", "rule": "x"}}, "zh"))
	})

	t.Run("中文标题", func(t *testing.T) {
		gradients := []map[string]any{
			{"category": "tone", "rule": "简洁克制"},
			{"category": "target", "rule": "不该出现"},
		}
		got := RenderStyleRules(gradients, "zh")
		assert.Contains(t, got, "【话术风格要求】（基于用户历史反馈）")
		assert.Contains(t, got, "- 简洁克制")
		assert.NotContains(t, got, "不该出现")
	})

	t.Run("英文标题", func(t *testing.T) {
		got := RenderStyleRules([]map[string]any{{"category": "structure", "rule": "1-2 sentences"}}, "en")
		assert.Contains(t, got, "[Style rules for the copy] (from the user's past feedback)")
		assert.Contains(t, got, "- 1-2 sentences")
	})
}

// ──────────────────────────── renderFeedbacks / renderGradients 测试分组 ────────────────────────────

// TestRenderFeedbacks 双语标签与空值省略。
// Python: gradient_updater.py:143
func TestRenderFeedbacks(t *testing.T) {
	t.Run("空反馈", func(t *testing.T) {
		assert.Equal(t, "（无反馈）", renderFeedbacks(nil, "zh"))
		assert.Equal(t, "(no feedback)", renderFeedbacks(nil, "en"))
	})

	t.Run("中文标签", func(t *testing.T) {
		got := renderFeedbacks([]map[string]any{
			{"feedback_type": "explicit_like", "rec_content": "推荐A", "user_reply": ""},
			{"feedback_type": "implicit", "rec_content": "推荐B", "user_reply": "不需要"},
		}, "zh")
		assert.Contains(t, got, "1. 反馈类型: explicit_like")
		assert.Contains(t, got, "   推荐内容: 推荐A")
		assert.Contains(t, got, "2. 反馈类型: implicit")
		assert.Contains(t, got, "   用户回复: 不需要")
	})

	t.Run("英文标签", func(t *testing.T) {
		got := renderFeedbacks([]map[string]any{
			{"feedback_type": "explicit_like", "rec_content": "RecA", "user_reply": "no"},
		}, "en")
		assert.Contains(t, got, "1. feedback type: explicit_like")
		assert.Contains(t, got, "   recommendation content: RecA")
		assert.Contains(t, got, "   user reply: no")
	})

	t.Run("空字段不输出对应行", func(t *testing.T) {
		got := renderFeedbacks([]map[string]any{
			{"feedback_type": "explicit_like"},
		}, "zh")
		assert.NotContains(t, got, "推荐内容")
		assert.NotContains(t, got, "用户回复")
	})

	t.Run("缺feedback_type用unknown", func(t *testing.T) {
		got := renderFeedbacks([]map[string]any{{}}, "zh")
		assert.Contains(t, got, "反馈类型: unknown")
	})
}

// TestRenderGradients 梯度渲染格式。
// Python: gradient_updater.py:173
func TestRenderGradients(t *testing.T) {
	assert.Equal(t, "（暂无）", renderGradients(nil))

	got := renderGradients([]map[string]any{
		{"gradient_id": "g1", "category": "target", "rule": "避免日程"},
		{"gradient_id": "g2", "category": "tone", "rule": "简洁"},
	})
	assert.Equal(t, "- [g1] (target) 避免日程\n- [g2] (tone) 简洁", got)

	t.Run("缺字段用unknown", func(t *testing.T) {
		got := renderGradients([]map[string]any{{"rule": "x"}})
		assert.Contains(t, got, "[unknown] (unknown) x")
	})
}

// ──────────────────────────── extractGradientJSON 测试分组 ────────────────────────────

// TestExtractGradientJSON 用 raw_decode 语义：解析首个合法 JSON 后忽略尾部文本。
// 与 proactive_actions 的贪婪正则版本行为不同（这是本函数存在的理由）。
// Python: gradient_updater.py:204
func TestExtractGradientJSON(t *testing.T) {
	t.Run("代码块优先", func(t *testing.T) {
		text := "说明\n```json\n{\"operations\": []}\n```\n尾部"
		assert.Equal(t, `{"operations": []}`, ExtractGradientJSONForTest(text))
	})

	t.Run("尾部含花括号的脏文本能被正确解析", func(t *testing.T) {
		// 这是与贪婪正则的关键差异：贪婪正则会吃到末个 }，导致 JSON 无效
		text := `{"operations": [{"action": "add"}]} 后面还有 } 字符`
		got := ExtractGradientJSONForTest(text)
		assert.Equal(t, `{"operations":[{"action":"add"}]}`, got,
			"raw_decode 语义应只解析首个完整 JSON，忽略尾部")
	})

	t.Run("无JSON返回空", func(t *testing.T) {
		assert.Equal(t, "", ExtractGradientJSONForTest("没有 JSON"))
		assert.Equal(t, "", ExtractGradientJSONForTest(""))
	})

	t.Run("非法JSON返回空", func(t *testing.T) {
		assert.Equal(t, "", ExtractGradientJSONForTest("{不是合法"))
	})
}

// TestExtractGradientJSON_vs_贪婪正则 两个实现确实不同——这是禁止合并的证据。
func TestExtractGradientJSON_vs_贪婪正则(t *testing.T) {
	text := `{"operations": [{"action": "add"}]} 尾部含 } 字符`

	gradientResult := ExtractGradientJSONForTest(text)
	proactiveResult := ExtractJSONFromResponse(text)

	assert.NotEqual(t, proactiveResult, gradientResult,
		"贪婪正则版与 raw_decode 版在同一输入下结果不同，证明必须各自独立实现")
}

// ──────────────────────────── parseGradientOperations 测试分组 ────────────────────────────

// TestParseGradientOperations 解析 operations 数组。
func TestParseGradientOperations(t *testing.T) {
	t.Run("正常解析", func(t *testing.T) {
		ops, ok := parseGradientOperations(`{"operations": [{"action": "add"}, {"action": "drop"}]}`)
		require.True(t, ok)
		assert.Len(t, ops, 2)
	})

	t.Run("operations缺失视为无操作", func(t *testing.T) {
		ops, ok := parseGradientOperations(`{"other": 1}`)
		require.True(t, ok)
		assert.Empty(t, ops)
	})

	t.Run("operations非数组视为无操作", func(t *testing.T) {
		ops, ok := parseGradientOperations(`{"operations": "bad"}`)
		require.True(t, ok)
		assert.Empty(t, ops)
	})

	t.Run("数组中非对象元素被跳过", func(t *testing.T) {
		ops, ok := parseGradientOperations(`{"operations": [{"action":"add"}, "str", 123]}`)
		require.True(t, ok)
		assert.Len(t, ops, 1)
	})

	t.Run("非法JSON返回false", func(t *testing.T) {
		_, ok := parseGradientOperations("{不是json")
		assert.False(t, ok)
	})
}

// ──────────────────────────── UpdateGradients（真 DeepAgent）────────────────────────────

// TestUpdateGradients_空反馈 无反馈直接返回原梯度，不调模型。
func TestUpdateGradients_空反馈(t *testing.T) {
	existing := []map[string]any{{"gradient_id": "g1", "rule": "R1"}}
	got := UpdateGradients(context.Background(), nil, existing, nil)
	assert.Equal(t, existing, got, "无反馈应原样返回")
}

// TestUpdateGradients_显式反馈只生成target 显式反馈走专用 prompt，过滤非 target 操作。
// Python: gradient_updater.py:315
func TestUpdateGradients_显式反馈(t *testing.T) {
	reply := `{"operations": [
		{"action": "add", "rule": "避免推荐日程类话题", "category": "target"},
		{"action": "add", "rule": "语气简洁", "category": "tone"}
	]}`
	agent := newProactiveAgentWithReply(t, reply)

	feedbacks := []map[string]any{
		{"feedback_type": "explicit_dislike", "rec_content": "日程提醒", "user_reply": ""},
	}
	got := UpdateGradients(context.Background(), feedbacks, nil, agent)

	require.Len(t, got, 1, "显式反馈只能产生 target 规则，tone 应被过滤")
	assert.Equal(t, "避免推荐日程类话题", got[0]["rule"])
	assert.Equal(t, "target", got[0]["category"])
}

// TestUpdateGradients_隐式反馈可生成任意类型。
// Python: gradient_updater.py:463
func TestUpdateGradients_隐式反馈(t *testing.T) {
	reply := `{"operations": [
		{"action": "add", "rule": "语气要克制", "category": "tone"},
		{"action": "add", "rule": "话术一两句", "category": "structure"}
	]}`
	agent := newProactiveAgentWithReply(t, reply)

	feedbacks := []map[string]any{
		{"feedback_type": "implicit", "rec_content": "推荐A", "user_reply": "太啰嗦了"},
	}
	got := UpdateGradients(context.Background(), feedbacks, nil, agent)

	require.Len(t, got, 2, "隐式反馈可产生任意类型规则")
	assert.Equal(t, "tone", got[0]["category"])
	assert.Equal(t, "structure", got[1]["category"])
}

// TestUpdateGradients_混合反馈 显式与隐式都处理，按显式→隐式顺序。
func TestUpdateGradients_混合反馈(t *testing.T) {
	reply := `{"operations": [{"action": "add", "rule": "规整", "category": "target"}]}`
	agent := newProactiveAgentWithReply(t, reply)

	feedbacks := []map[string]any{
		{"feedback_type": "explicit_like", "rec_content": "A", "user_reply": ""},
		{"feedback_type": "implicit", "rec_content": "B", "user_reply": "不错"},
	}
	got := UpdateGradients(context.Background(), feedbacks, nil, agent)
	// 两轮各加一条（模型每次返回同一条），故结果 2 条
	assert.Len(t, got, 2, "显式与隐式各走一轮")
}

// TestUpdateGradients_模型返回非JSON 解析失败时保留原梯度。
func TestUpdateGradients_模型返回非JSON(t *testing.T) {
	agent := newProactiveAgentWithReply(t, "我无法分析")
	existing := []map[string]any{{"gradient_id": "g1", "rule": "R1", "category": "target"}}

	got := UpdateGradients(context.Background(), []map[string]any{
		{"feedback_type": "explicit_like", "rec_content": "A"},
	}, existing, agent)

	assert.Equal(t, existing, got, "解析失败应原样返回")
}

// TestUpdateGradients_空operations 模型返回空操作时保留原梯度。
func TestUpdateGradients_空operations(t *testing.T) {
	agent := newProactiveAgentWithReply(t, `{"operations": []}`)
	existing := []map[string]any{{"gradient_id": "g1", "rule": "R1", "category": "target"}}

	got := UpdateGradients(context.Background(), []map[string]any{
		{"feedback_type": "implicit", "rec_content": "A", "user_reply": "嗯"},
	}, existing, agent)

	assert.Equal(t, existing, got)
}

// TestUpdateGradients_revise已有规则 二阶梯度：修改既有规则。
func TestUpdateGradients_revise(t *testing.T) {
	reply := `{"operations": [{"action": "revise", "gradient_id": "g1", "rule": "修正后的规则", "category": "target"}]}`
	agent := newProactiveAgentWithReply(t, reply)

	existing := []map[string]any{
		{"gradient_id": "g1", "rule": "旧规则", "category": "target"},
		{"gradient_id": "g2", "rule": "其他规则", "category": "tone"},
	}
	got := UpdateGradients(context.Background(), []map[string]any{
		{"feedback_type": "explicit_like", "rec_content": "A"},
	}, existing, agent)

	require.Len(t, got, 2)
	// g1 被 revise 后挪到末尾
	assert.Equal(t, "g2", got[0]["gradient_id"])
	assert.Equal(t, "g1", got[1]["gradient_id"])
	assert.Equal(t, "修正后的规则", got[1]["rule"])
}

// TestUpdateGradients_prompt含渲染后内容 真 agent 收到的 prompt 已渲染。
func TestUpdateGradients_prompt含渲染后内容(t *testing.T) {
	captured := ""
	agent := newProactiveAgentCapturing(t, &captured, `{"operations": []}`)

	UpdateGradients(context.Background(), []map[string]any{
		{"feedback_type": "explicit_dislike", "rec_content": "CONTENT_MARKER"},
	}, []map[string]any{
		{"gradient_id": "g1", "category": "target", "rule": "RULE_MARKER"},
	}, agent)

	assert.Contains(t, captured, "CONTENT_MARKER", "反馈内容应渲染进 prompt")
	assert.Contains(t, captured, "RULE_MARKER", "已有规则应渲染进 prompt")
	assert.NotContains(t, captured, "{feedbacks_text}", "占位符应已被替换")
	assert.NotContains(t, captured, "{existing_text}", "占位符应已被替换")
}

// TestUpdateGradients_nilAgent 模型为 nil 时不 panic，返回原梯度。
func TestUpdateGradients_nilAgent(t *testing.T) {
	existing := []map[string]any{{"gradient_id": "g1", "rule": "R1"}}
	got := UpdateGradients(context.Background(), []map[string]any{
		{"feedback_type": "explicit_like", "rec_content": "A"},
	}, existing, nil)
	assert.Equal(t, existing, got)
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// ExtractGradientJSONForTest 暴露非导出函数供测试断言。
func ExtractGradientJSONForTest(text string) string {
	return extractGradientJSON(text)
}

// splitNonEmpty 按行切分并去掉空行。
func splitNonEmpty(s string) []string {
	var result []string
	for _, line := range strings.Split(s, "\n") {
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

// TestReleaseCheckpointID 释放不存在的会话记录时不 panic（失败只记 debug）。
func TestReleaseCheckpointID(t *testing.T) {
	assert.NotPanics(t, func() {
		releaseCheckpointID(context.Background(), "nonexistent-conversation-id-xyz")
	})
}

// TestUpdateGradients_隐式反馈空operations 模型返回空操作保留原梯度。
// 覆盖 updateGradientsFromImplicitFeedback 的 len==0 分支。
func TestUpdateGradients_隐式反馈空operations(t *testing.T) {
	agent := newProactiveAgentWithReply(t, `{"operations": []}`)
	existing := []map[string]any{{"gradient_id": "g1", "rule": "R1", "category": "tone"}}

	got := UpdateGradients(context.Background(), []map[string]any{
		{"feedback_type": "implicit", "rec_content": "A", "user_reply": "随便聊聊"},
	}, existing, agent)

	assert.Equal(t, existing, got, "隐式反馈无操作时原样返回")
}

// TestInvokeGradientAgent_nilAgent nil agent 直接返回 false 不 panic。
func TestInvokeGradientAgent_nilAgent(t *testing.T) {
	_, ok := invokeGradientAgent(context.Background(), nil, "prompt", "conv")
	assert.False(t, ok)
}

// TestIsEnglish 语言判定。
func TestIsEnglish(t *testing.T) {
	assert.True(t, isEnglish("en"))
	assert.True(t, isEnglish("EN"))
	assert.True(t, isEnglish("  en  "))
	assert.False(t, isEnglish("zh"))
	assert.False(t, isEnglish(""))
}
