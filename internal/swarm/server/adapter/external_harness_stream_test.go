package adapter

import (
	"context"
	"strings"
	"testing"

	stream "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	saschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers"
	schema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestExternalQuestionOptions 选项归一化 + Other 兜底。
// Python: _external_question_options :676-693。
func TestExternalQuestionOptions(t *testing.T) {
	// 非 list → nil
	if got := externalQuestionOptions("x"); got != nil {
		t.Fatalf("非 list 应 nil: %v", got)
	}
	// label 缺失丢弃 + description 保留 + Other 兜底
	got := externalQuestionOptions([]any{
		map[string]any{"label": "A", "description": "desc"},
		map[string]any{"description": "no label"},
		"junk",
	})
	if len(got) != 2 || got[0]["label"] != "A" || got[0]["description"] != "desc" {
		t.Fatalf("归一化不符: %+v", got)
	}
	if got[1]["label"] != "Other" || got[1]["description"] != "Custom input" {
		t.Fatalf("Other 兜底不符: %+v", got[1])
	}
	// 全部无效 → 无 Other
	if got := externalQuestionOptions([]any{map[string]any{}}); got != nil {
		t.Fatalf("全无效应 nil: %+v", got)
	}
}

// TestExternalAskQuestions questions 归一化 + prompt 回退。
// Python: _external_ask_questions :696-740。
func TestExternalAskQuestions(t *testing.T) {
	// provider_data.questions 优先
	got := externalAskQuestions(map[string]any{
		"provider_data": map[string]any{
			"questions": []any{map[string]any{
				"question": "Q?", "header": "H", "multi_select": true,
				"options": []any{map[string]any{"label": "Yes"}},
			}},
		},
	})
	if len(got) != 1 || got[0]["question"] != "Q?" || got[0]["multi_select"] != true {
		t.Fatalf("questions 不符: %+v", got)
	}
	// input.questions 回退
	got = externalAskQuestions(map[string]any{
		"provider_data": map[string]any{
			"input": map[string]any{"questions": []any{map[string]any{"question": "Q2"}}},
		},
	})
	if len(got) != 1 || got[0]["question"] != "Q2" || got[0]["header"] != "Question" {
		t.Fatalf("input.questions 回退不符: %+v", got)
	}
	// prompt + choices 兜底
	got = externalAskQuestions(map[string]any{"prompt": "如何?", "choices": []any{"A", "", "B"}})
	if len(got) != 1 || got[0]["question"] != "如何?" || got[0]["multi_select"] != false {
		t.Fatalf("prompt 回退不符: %+v", got)
	}
	options := got[0]["options"].([]map[string]string)
	if len(options) != 2 || options[0]["label"] != "A" || options[1]["label"] != "B" {
		t.Fatalf("choices 过滤不符: %+v", options)
	}
}

// TestProjectExternalUserInput user_input 投影 + 非用户输入不投影。
// Python: _project_external_user_input :743-766。
func TestProjectExternalUserInput(t *testing.T) {
	// 非 interaction 类型 → nil
	if got := projectExternalUserInput(stream.OutputSchema{Type: "llm_output"}); got != nil {
		t.Fatalf("非交互应 nil: %v", got)
	}
	// user_input → ask_user 卡片
	got := projectExternalUserInput(stream.OutputSchema{
		Type: interactionChunkType,
		Payload: map[string]any{
			"id": "req-1",
			"value": map[string]any{
				"kind":   "user_input",
				"prompt": "选一个",
			},
		},
	})
	if got == nil || storedString(got, "event_type") != "chat.ask_user_question" ||
		storedString(got, "request_id") != "req-1" ||
		storedString(got, "kind") != saschema.InterruptKindAskUser ||
		storedString(got, "interrupt_id") != "req-1" {
		t.Fatalf("投影不符: %+v", got)
	}
	// kind != user_input → nil
	got = projectExternalUserInput(stream.OutputSchema{
		Type: interactionChunkType,
		Payload: map[string]any{
			"id": "req-2", "value": map[string]any{"kind": "tool_approval"},
		},
	})
	if got != nil {
		t.Fatalf("非 user_input 应 nil: %+v", got)
	}
}

// TestProcessExternalHarnessInterrupt 中断五分支。
// Python: process_external_harness_interrupt :975-1020。
func TestProcessExternalHarnessInterrupt(t *testing.T) {
	d := &DeepAdapter{}
	request := &schema.AgentRequest{RequestID: "r1", ChannelID: "c1",
		Params: []byte(`{"intent":"stop"}`)}
	state := &ExternalHarnessSessionState{}

	// 非 cancel 意图 → 拒绝
	got := d.ProcessExternalHarnessInterrupt(context.Background(), state, request)
	if got.Payload["success"] != false ||
		!strings.Contains(got.Payload["message"].(string), "不支持中断意图") {
		t.Fatalf("非 cancel 应拒绝: %+v", got.Payload)
	}

	// intent 缺省 → cancel；无 io → nothing_to_cancel
	request.Params = []byte(`{}`)
	got = d.ProcessExternalHarnessInterrupt(context.Background(), state, request)
	if got.Payload["success"] != true ||
		!strings.Contains(got.Payload["message"].(string), "没有可取消") {
		t.Fatalf("无任务应成功空转: %+v", got.Payload)
	}

	// payload 形状
	if storedString(got.Payload, "event_type") != "chat.interrupt_result" ||
		storedString(got.Payload, "intent") != "cancel" {
		t.Fatalf("payload 形状不符: %+v", got.Payload)
	}
}

// TestBindExternalWorkspace 工作区采纳（一次）。
// Python: _bind_external_workspace :625-634。
func TestBindExternalWorkspace(t *testing.T) {
	state := &ExternalHarnessSessionState{}
	bindExternalWorkspace(state, map[string]any{"project_dir": " /tmp/proj "})
	if state.ProjectDir != "/tmp/proj" {
		t.Fatalf("应采纳去空白: %q", state.ProjectDir)
	}
	// 已有 → 不覆盖
	bindExternalWorkspace(state, map[string]any{"project_dir": "/other"})
	if state.ProjectDir != "/tmp/proj" {
		t.Fatalf("已有应保留: %q", state.ProjectDir)
	}
	// 非 string → 不动
	state2 := &ExternalHarnessSessionState{}
	bindExternalWorkspace(state2, map[string]any{"project_dir": 42})
	if state2.ProjectDir != "" {
		t.Fatalf("非 string 应不动: %q", state2.ProjectDir)
	}
}

// TestExternalWorkspace 项目目录优先 / 默认会话工作区。
// Python: _external_workspace :637-641。
func TestExternalWorkspace(t *testing.T) {
	isolateWorkspace(t)
	state := &ExternalHarnessSessionState{ProjectDir: "/tmp/someproj"}
	got, err := ExternalWorkspace(state, "sess_w")
	if err != nil || !strings.HasSuffix(got, "/tmp/someproj") {
		t.Fatalf("项目目录优先: %q, %v", got, err)
	}
	state2 := &ExternalHarnessSessionState{}
	got, err = ExternalWorkspace(state2, "sess_w2")
	if err != nil || !strings.Contains(got, "projects") || !strings.Contains(got, "sess_w2") {
		t.Fatalf("默认工作区: %q, %v", got, err)
	}
}

// TestStreamTurnOutput_边界 流边界（own 圈定 / 终态收口 / interaction 收口）。
// 用真实 IOAdapter + 手工构造的 harness 桩事件不可行——本用例以集成覆盖为主，
// 这里仅验证终态 kind 集与常量契约。
func TestStreamTurnOutput_常量契约(t *testing.T) {
	if !terminalLifecycleKinds["finished"] || !terminalLifecycleKinds["failed"] ||
		!terminalLifecycleKinds["aborted"] {
		t.Fatalf("终态集不符")
	}
	if interactionChunkType != "__interaction__" {
		t.Fatalf("交互块类型不符: %s", interactionChunkType)
	}
	if harnessproviders.TurnLifecycleChunkType != "turn_lifecycle" {
		t.Fatalf("生命周期块类型不符: %s", harnessproviders.TurnLifecycleChunkType)
	}
	if harnessprotocol.ResumePolicyNew != "new" {
		t.Fatalf("resume policy 常量漂移")
	}
}
