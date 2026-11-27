package runtime

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

func TestUapClaw_BuildInputs_基本字段提取(t *testing.T) {
	uc := NewUapClaw()
	params := map[string]any{
		"query": "你好世界",
	}
	paramsJSON, _ := json.Marshal(params)
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodChatSend, paramsJSON)

	inputs, memoryMode, rawQuery := uc.BuildInputs(req)

	assert.Equal(t, "你好世界", rawQuery)
	_ = memoryMode // memoryMode 依赖 config，当前可能为空
	assert.NotNil(t, inputs["query"])
	assert.NotNil(t, inputs["conversation_id"])
	assert.NotNil(t, inputs["channel"])
	assert.NotNil(t, inputs["language"])
}

func TestUapClaw_BuildInputs_projectDir优先级(t *testing.T) {
	uc := NewUapClaw()
	params := map[string]any{
		"query":       "test",
		"project_dir": "/path/from/params",
	}
	paramsJSON, _ := json.Marshal(params)
	metadata := map[string]any{"project_dir": "/path/from/metadata"}
	req := schema.NewAgentRequest("req-2", "web", schema.ReqMethodChatSend, paramsJSON,
		schema.WithAgentMetadata(metadata))

	inputs, _, _ := uc.BuildInputs(req)

	// params 优先
	assert.Equal(t, "/path/from/params", inputs["project_dir"])
}

func TestUapClaw_BuildInputs_cron字段转换(t *testing.T) {
	uc := NewUapClaw()
	params := map[string]any{
		"query": "定时任务",
		"cron":  map[string]any{"schedule": "0 9 * * *"},
	}
	paramsJSON, _ := json.Marshal(params)
	req := schema.NewAgentRequest("req-3", "cron", schema.ReqMethodChatSend, paramsJSON)

	inputs, _, _ := uc.BuildInputs(req)

	runVal, hasRun := inputs["run"]
	assert.True(t, hasRun)
	runMap, ok := runVal.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "cron", runMap["kind"])
}

func TestUapClaw_BuildInputs_trustedDirs提取(t *testing.T) {
	uc := NewUapClaw()
	params := map[string]any{
		"query":        "test",
		"trusted_dirs": []any{"/home/user/proj1", "/home/user/proj2"},
	}
	paramsJSON, _ := json.Marshal(params)
	req := schema.NewAgentRequest("req-4", "web", schema.ReqMethodChatSend, paramsJSON)

	inputs, _, _ := uc.BuildInputs(req)

	dirs, ok := inputs["trusted_dirs"].([]string)
	assert.True(t, ok)
	assert.Len(t, dirs, 2)
}

// ──────────────────────────── buildInteractiveInputFromAnswers 对齐用例 ────────────────────────────
// 对齐 Python: JiuWenSwarm._build_interactive_input_from_answers（interface.py:2251-2486）

// buildII 便捷构造：经 BuildInputs 全路径构建 InteractiveInput。
func buildII(t *testing.T, params map[string]any) *interaction.InteractiveInput {
	t.Helper()
	uc := NewUapClaw()
	paramsJSON, _ := json.Marshal(params)
	req := schema.NewAgentRequest("req-ii", "web", schema.ReqMethodChatSend, paramsJSON)
	inputs, _, _ := uc.BuildInputs(req)
	ii, ok := inputs["query"].(*interaction.InteractiveInput)
	require.True(t, ok, "answers 分支应产出 InteractiveInput")
	return ii
}

func TestBuildInteractiveInput_permission_词表解析(t *testing.T) {
	// interrupt_id 存在 → 按 interrupt_id 回答
	ii := buildII(t, map[string]any{
		"source":       "permission_interrupt",
		"request_id":   "tc1",
		"interrupt_id": "intr_abc",
		"answers":      []any{map[string]any{"selected_options": []any{"approve"}, "custom_input": ""}},
	})
	v, ok := ii.InterruptAnswers["intr_abc"]
	require.True(t, ok, "有 interrupt_id 时应按 interrupt_id 回答")
	payload, isMap := v.(map[string]any)
	require.True(t, isMap)
	assert.Equal(t, true, payload["approved"])
	assert.Equal(t, false, payload["auto_confirm"])
	// 词表别名：中文与大小写混写
	ii2 := buildII(t, map[string]any{
		"source":     "permission_interrupt",
		"request_id": "tc1",
		"answers":    []any{map[string]any{"selected_options": []any{"本次允许"}}},
	})
	v2, ok := ii2.UserInputs["tc1"]
	require.True(t, ok, "无 interrupt_id 时按 request_id 回答")
	assert.Equal(t, true, v2.(map[string]any)["approved"])
}

func TestBuildInteractiveInput_permission_sessionAlways(t *testing.T) {
	ii := buildII(t, map[string]any{
		"source":     "permission_interrupt",
		"request_id": "tc1",
		"answers":    []any{map[string]any{"selected_options": []any{"session_allow"}}},
	})
	payload := ii.UserInputs["tc1"].(map[string]any)
	assert.Equal(t, true, payload["approved"])
	assert.Equal(t, true, payload["auto_confirm"])
	assert.Equal(t, false, payload["persist_allow"])
}

func TestBuildInteractiveInput_permission_planExecuteSkipRevise(t *testing.T) {
	mk := func(value string, custom string) map[string]any {
		ii := buildII(t, map[string]any{
			"source":     "permission_interrupt",
			"request_id": "tc1",
			"answers":    []any{map[string]any{"selected_options": []any{value}, "custom_input": custom}},
		})
		return ii.UserInputs["tc1"].(map[string]any)
	}
	assert.Equal(t, true, mk("plan_execute", "")["plan_execute"])
	assert.Equal(t, true, mk("plan_execute", "")["approved"])
	skip := mk("plan_skip", "")
	assert.Equal(t, true, skip["plan_skip"])
	assert.Equal(t, false, skip["approved"])
	assert.NotEmpty(t, skip["feedback"])
	revise := mk("plan_revise", "加上重试")
	assert.Equal(t, true, revise["plan_revise"])
	assert.Equal(t, "加上重试", revise["feedback"])
}

func TestBuildInteractiveInput_permission_unknown与keepPlanning(t *testing.T) {
	// 继续规划类拒绝 → 反馈文案为"用户希望继续规划"
	ii := buildII(t, map[string]any{
		"source":     "permission_interrupt",
		"request_id": "tc1",
		"answers":    []any{map[string]any{"selected_options": []any{"继续规划"}}},
	})
	assert.Equal(t, "用户希望继续规划", ii.UserInputs["tc1"].(map[string]any)["feedback"])

	// 未知选项 + 自由文本 → 文本当反馈
	ii2 := buildII(t, map[string]any{
		"source":     "permission_interrupt",
		"request_id": "tc1",
		"answers":    []any{map[string]any{"selected_options": []any{"mystery"}, "custom_input": "不要删库"}},
	})
	assert.Equal(t, "不要删库", ii2.UserInputs["tc1"].(map[string]any)["feedback"])

	// 未知选项无文本 → 未知选项兜底
	ii3 := buildII(t, map[string]any{
		"source":     "permission_interrupt",
		"request_id": "tc1",
		"answers":    []any{map[string]any{"selected_options": []any{"mystery"}}},
	})
	assert.Equal(t, "未知选项: mystery", ii3.UserInputs["tc1"].(map[string]any)["feedback"])

	// 空取值 = 渲染端取消，不告警、按拒绝兜底
	ii4 := buildII(t, map[string]any{
		"source":     "permission_interrupt",
		"request_id": "tc1",
		"answers":    []any{map[string]any{"selected_options": []any{""}}},
	})
	assert.Equal(t, false, ii4.UserInputs["tc1"].(map[string]any)["approved"])
}

func TestBuildInteractiveInput_confirmInterrupt白名单(t *testing.T) {
	ii := buildII(t, map[string]any{
		"source":     "confirm_interrupt",
		"request_id": "tc1",
		"answers":    []any{map[string]any{"selected_options": []any{"approve"}}},
	})
	assert.Equal(t, true, ii.UserInputs["tc1"].(map[string]any)["approved"])
}

func TestBuildInteractiveInput_permissionCardId(t *testing.T) {
	ii := buildII(t, map[string]any{
		"source":     "permission_interrupt",
		"request_id": "tc1",
		"answers": []any{map[string]any{
			"selected_options": []any{"approve"},
			"card_id":          " card-9 ",
		}},
	})
	// Smart queue 卡片用它自己的 card id 作答（去空白）
	_, byCard := ii.UserInputs["card-9"]
	assert.True(t, byCard, "有 card_id 时应按 card id 回答")
	_, byRequest := ii.UserInputs["tc1"]
	assert.False(t, byRequest)
}

func TestBuildInteractiveInput_clientToolInterrupt(t *testing.T) {
	ii := buildII(t, map[string]any{
		"source": "client_tool_interrupt",
		"answers": []any{
			map[string]any{"request_id": "tcA", "interrupt_id": "intr_a", "result": map[string]any{"status": "completed", "content": "ok"}},
			map[string]any{"request_id": "", "interrupt_id": "intr_bad", "result": map[string]any{}}, // 缺 request_id → 跳过
			map[string]any{"request_id": "tcC"}, // 缺 result → 跳过
		},
	})
	v, ok := ii.InterruptAnswers["intr_a"]
	require.True(t, ok)
	assert.Equal(t, "completed", v.(map[string]any)["status"])
	assert.NotContains(t, ii.InterruptAnswers, "intr_bad")
}

func TestBuildInteractiveInput_askUser选项合并(t *testing.T) {
	// 多选 + Other 占位剔除 + custom_input 追加
	ii := buildII(t, map[string]any{
		"source":     "ask_user_interrupt",
		"request_id": "tc1",
		"answers": []any{map[string]any{
			"question":        "选择方案",
			"selected_options": []any{"A", "Other"},
			"custom_input":    "自定义",
		}},
	})
	answers := ii.UserInputs["tc1"].(map[string]any)["answers"].(map[string]any)
	assert.Equal(t, []string{"A", "自定义"}, answers["选择方案"])

	// 裸 Other 无文本 → 空（#2330）
	ii2 := buildII(t, map[string]any{
		"source":     "ask_user_interrupt",
		"request_id": "tc1",
		"answers": []any{map[string]any{
			"question":         "选择方案",
			"selected_options": []any{"Other"},
		}},
	})
	assert.Empty(t, ii2.UserInputs["tc1"].(map[string]any)["answers"].(map[string]any)["选择方案"])

	// 纯文本无问题 → __free_text__ 兜底
	ii3 := buildII(t, map[string]any{
		"source":     "ask_user_interrupt",
		"request_id": "tc1",
		"answers": []any{map[string]any{"custom_input": "就这样吧"}},
	})
	assert.Equal(t, "就这样吧", ii3.UserInputs["tc1"].(map[string]any)["answers"].(map[string]any)["__free_text__"])

	// 无 interrupt_id 的 ask_user → 按 request_id 回答
	ii4 := buildII(t, map[string]any{
		"source":       "ask_user_interrupt",
		"request_id":   "tc1",
		"interrupt_id": "intr_q1",
		"answers": []any{map[string]any{
			"question":         "继续吗",
			"selected_options": []any{"是"},
		}},
	})
	_, byInterrupt := ii4.InterruptAnswers["intr_q1"]
	assert.True(t, byInterrupt, "有 interrupt_id 时 ask_user 也应按中断 id 回答")
}

func TestBuildInteractiveInput_evolution审批(t *testing.T) {
	mk := func(value string, custom string) map[string]any {
		ii := buildII(t, map[string]any{
			"source":     "evolution_interrupt",
			"request_id": "tc1",
			"answers":    []any{map[string]any{"selected_options": []any{value}, "custom_input": custom}},
		})
		return ii.UserInputs["tc1"].(map[string]any)
	}
	assert.Equal(t, "allow_once", mk("accept", "")["action"])
	assert.Equal(t, "allow_always", mk("总是允许", "")["action"])
	assert.Equal(t, "reject", mk("mystery", "")["action"])
	revise := mk("allow_once", "改一下提示词")
	assert.Equal(t, "改一下提示词", revise["feedback"])

	// 旧写法 source 同样受理
	ii := buildII(t, map[string]any{
		"source":     "skill_evolution_approval",
		"request_id": "tc1",
		"answers":    []any{map[string]any{"selected_options": []any{"reject"}}},
	})
	assert.Equal(t, "reject", ii.UserInputs["tc1"].(map[string]any)["action"])
}

func TestBuildInteractiveInput_未知source返回nil(t *testing.T) {
	uc := NewUapClaw()
	paramsJSON, _ := json.Marshal(map[string]any{
		"source":     "totally_unknown",
		"request_id": "tc1",
		"answers":    []any{map[string]any{"selected_options": []any{"approve"}}},
	})
	req := schema.NewAgentRequest("req-x", "web", schema.ReqMethodChatSend, paramsJSON)
	inputs, _, _ := uc.BuildInputs(req)
	_, isII := inputs["query"].(*interaction.InteractiveInput)
	assert.False(t, isII, "未知 source 应回退普通消息")
}
