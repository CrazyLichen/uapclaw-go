package adapter

import (
	"testing"

	ateamschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	evolutionlogic "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter/evolution/logic"
)

// ──────────────────────────── 非导出函数（测试辅助） ────────────────────────────

// chunkToMapTestStrPtr 测试辅助：字符串转指针。
func chunkToMapTestStrPtr(s string) *string { return &s }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestChunkToMap_TeamOutputSchema_形状验证 S-20 修复后 *TeamOutputSchema 的输出形状。
// 方案A 半展平形状（对齐 Python typed chunk 属性访问语义）：
//   - payload 键保留嵌套（utilsParseStreamChunk 的 typed 分支从 "payload" 读，对齐 getattr(chunk,"payload")）
//   - type/index 顶层（对齐 getattr(chunk,"type")）
//   - role/source_member 顶层（对齐 getattr(chunk,"role"/"source_member")）
//
// isLeaderOutput/isTeammateOutput/enrichTeammateEvent 依赖此形状读键。
func TestChunkToMap_TeamOutputSchema_形状验证(t *testing.T) {
	leaderRole := ateamschema.TeamRole("leader")
	readyChunk := ateamschema.NewTeamOutputSchema(
		agentschema.OutputSchema{
			Type:  "message",
			Index: 0,
			Payload: map[string]any{
				"event_type":      "team.runtime_ready",
				"team_name":       "myteam",
				"session_id":      "s1",
				"activation_kind": "resume",
			},
		},
		chunkToMapTestStrPtr("leader-agent"),
		&leaderRole,
	)

	m := chunkToMap(readyChunk)
	if m == nil {
		t.Fatal("chunkToMap(*TeamOutputSchema) 返回 nil，团队流将被整体丢弃")
	}

	// 消费链第一道：isLeaderOutput 读 chunk["role"]
	role, _ := m["role"].(string)
	if role != "leader" {
		t.Fatalf("m[\"role\"] = %q, 期望 \"leader\"（isLeaderOutput 依赖 string 比较）", role)
	}

	// 消费链第二道：enrichTeammateEvent 读 chunk["source_member"]
	sm, _ := m["source_member"].(string)
	if sm != "leader-agent" {
		t.Fatalf("m[\"source_member\"] = %q, 期望 \"leader-agent\"", sm)
	}

	// 消费链第三道：isLeaderOutput 的 message 分支读 chunk["type"] 与 chunk["payload"]
	typ, _ := m["type"].(string)
	if typ != "message" {
		t.Fatalf("m[\"type\"] = %q, 期望 \"message\"", typ)
	}
	payloadMap, ok := m["payload"].(map[string]any)
	if !ok {
		t.Fatalf("m[\"payload\"] 类型 %T 非 map[string]any，typed 形状断裂", m["payload"])
	}
	evtType, _ := payloadMap["event_type"].(string)
	if evtType != "team.runtime_ready" {
		t.Fatalf("payload.event_type = %q, 期望 \"team.runtime_ready\"", evtType)
	}
}

// TestChunkToMap_TeamOutputSchema_消费链端到端 验证消费循环完整链路（S-20 完整落地）：
// chunkToMap → isLeader/isTeammate → utilsParseStreamChunk → runtime_ready 分支判定。
// 原实现 utilsParseStreamChunk 是 identity 桩，parsed 无顶层 event_type，
// runtime_ready 分支与 S-04 接入点成为死代码——本测试锁定修复后的完整链路。
func TestChunkToMap_TeamOutputSchema_消费链端到端(t *testing.T) {
	teammateRole := ateamschema.TeamRole("teammate")
	deltaChunk := ateamschema.NewTeamOutputSchema(
		agentschema.OutputSchema{
			Type:    "content_chunk",
			Index:   0,
			Payload: map[string]any{"content": "hello from teammate"},
		},
		chunkToMapTestStrPtr("researcher-1"),
		&teammateRole,
	)

	m := chunkToMap(deltaChunk)
	if m == nil {
		t.Fatal("chunkToMap(teammate chunk) 返回 nil")
	}

	isLeader := isLeaderOutput(m)
	isTeammate := isTeammateOutput(m)
	if !isTeammate {
		t.Fatalf("isTeammateOutput(m) = false, 期望 true（role=%q）", m["role"])
	}
	if isLeader {
		t.Fatalf("isLeaderOutput(m) = true, 期望 false")
	}

	// S-20 核心断言：utilsParseStreamChunk 必须派生顶层 event_type（content_chunk → chat.delta）
	parsed := utilsParseStreamChunk(m)
	if parsed == nil {
		t.Fatal("utilsParseStreamChunk 返回 nil（identity 桩回归？），content_chunk 应派生 chat.delta")
	}
	if evtType, _ := parsed["event_type"].(string); evtType != "chat.delta" {
		t.Fatalf("parsed[\"event_type\"] = %q, 期望 \"chat.delta\"", evtType)
	}
	if content, _ := parsed["content"].(string); content != "hello from teammate" {
		t.Fatalf("parsed[\"content\"] = %q, 期望 \"hello from teammate\"", content)
	}

	parsed = enrichTeammateEvent(parsed, m)
	if parsed["member_name"] != "researcher-1" {
		t.Fatalf("parsed[\"member_name\"] = %v, 期望 \"researcher-1\"", parsed["member_name"])
	}
	if parsed["role"] != "teammate" {
		t.Fatalf("parsed[\"role\"] = %v, 期望 \"teammate\"", parsed["role"])
	}
}

// TestChunkToMap_TeamOutputSchema_runtime_ready端到端 验证 leader runtime_ready 控制事件的
// 完整解析链：message type + payload.event_type=team.runtime_ready → team. 前缀直传（不包 chat.）。
// 这是消费循环 runtime_ready 分支（S-04 接入点激活）的先决条件。
func TestChunkToMap_TeamOutputSchema_runtime_ready端到端(t *testing.T) {
	leaderRole := ateamschema.TeamRole("leader")
	readyChunk := ateamschema.NewTeamOutputSchema(
		agentschema.OutputSchema{
			Type:  "message",
			Index: 0,
			Payload: map[string]any{
				"event_type":      "team.runtime_ready",
				"team_name":       "myteam",
				"session_id":      "s1",
				"activation_kind": "new",
			},
		},
		chunkToMapTestStrPtr("team_leader"),
		&leaderRole,
	)

	m := chunkToMap(readyChunk)
	if m == nil {
		t.Fatal("chunkToMap(runtime_ready chunk) 返回 nil")
	}
	if !isLeaderOutput(m) {
		t.Fatal("isLeaderOutput(runtime_ready chunk) = false, 期望 true")
	}

	parsed := utilsParseStreamChunk(m)
	if parsed == nil {
		t.Fatal("utilsParseStreamChunk(runtime_ready) 返回 nil")
	}
	// Python 兜底分支：payload.event_type 以 team. 开头 → 直传键值（不包 chat.message 前缀）
	if evtType, _ := parsed["event_type"].(string); evtType != "team.runtime_ready" {
		t.Fatalf("parsed[\"event_type\"] = %q, 期望 \"team.runtime_ready\"（team. 命名空间直传）", evtType)
	}
	if tn, _ := parsed["team_name"].(string); tn != "myteam" {
		t.Fatalf("parsed[\"team_name\"] = %q, 期望 \"myteam\"", tn)
	}
	if ak, _ := parsed["activation_kind"].(string); ak != "new" {
		t.Fatalf("parsed[\"activation_kind\"] = %q, 期望 \"new\"", ak)
	}
}

// TestTeamHelperEventAsMap_ApprovalEvent S-21 修复验证：审批事件 *stream.OutputSchema
// （Type="chat.ask_user_question"，payload 含 request_id/questions）经 teamHelperEventAsMap
// 展平后 approvalChunkFromEvent 能读到顶层 event_type。
// 原实现走 ToMap() 嵌套语义，approvalChunkFromEvent 读 evt["event_type"] 恒空——审批弹框缺失。
func TestTeamHelperEventAsMap_ApprovalEvent(t *testing.T) {
	approvalEvent := &agentschema.OutputSchema{
		Type:  "chat.ask_user_question",
		Index: 0,
		Payload: map[string]any{
			"request_id": "req-evolve-1",
			"questions": []any{
				map[string]any{
					"question":     "是否接受演进？",
					"header":       "团队技能演进审批",
					"options":      []any{map[string]any{"label": "接收"}, map[string]any{"label": "拒绝"}},
					"multi_select": false,
				},
			},
		},
	}

	m := teamHelperEventAsMap(approvalEvent)
	if m == nil {
		t.Fatal("teamHelperEventAsMap(ApprovalEvent) 返回 nil，审批弹框必然缺失")
	}

	chunk := approvalChunkFromEvent(m)
	if chunk == nil {
		t.Fatalf("approvalChunkFromEvent 展平后仍返回 nil——event_type=%q request_id=%v", m["event_type"], m["request_id"])
	}
	if chunk["request_id"] != "req-evolve-1" {
		t.Fatalf("chunk[\"request_id\"] = %v, 期望 \"req-evolve-1\"", chunk["request_id"])
	}
	if questions, _ := chunk["questions"].([]any); len(questions) != 1 {
		t.Fatalf("chunk[\"questions\"] len = %d, 期望 1", len(questions))
	}
}

// TestParseTypedChunkMap_主要分支 逐分支验证 parseTypedChunkMap 与 Python
// _parse_typed_chunk（utils 层版本）的语义对齐。
func TestParseTypedChunkMap_主要分支(t *testing.T) {
	tests := []struct {
		name    string
		chunk   map[string]any
		wantNil bool
		wantEvt string
	}{
		{
			name:    "llm_output→chat.delta",
			chunk:   map[string]any{"type": "llm_output", "payload": map[string]any{"content": "hi"}},
			wantEvt: "chat.delta",
		},
		{
			name:    "llm_output空白→nil",
			chunk:   map[string]any{"type": "llm_output", "payload": map[string]any{"content": "  "}},
			wantNil: true,
		},
		{
			name:    "llm_reasoning→chat.reasoning",
			chunk:   map[string]any{"type": "llm_reasoning", "payload": map[string]any{"content": "thinking..."}},
			wantEvt: "chat.reasoning",
		},
		{
			name:    "llm_reasoning无content有output",
			chunk:   map[string]any{"type": "llm_reasoning", "payload": map[string]any{"output": "alt"}},
			wantEvt: "chat.reasoning",
		},
		{
			name:    "thinking→chat.processing_status",
			chunk:   map[string]any{"type": "thinking", "payload": map[string]any{}},
			wantEvt: "chat.processing_status",
		},
		{
			name:    "tool_call→chat.tool_call",
			chunk:   map[string]any{"type": "tool_call", "payload": map[string]any{"tool_call": map[string]any{"name": "t1"}}},
			wantEvt: "chat.tool_call",
		},
		{
			name:    "error→chat.error",
			chunk:   map[string]any{"type": "error", "payload": map[string]any{"error": "boom"}},
			wantEvt: "chat.error",
		},
		{
			name:    "带点类型直传event_type",
			chunk:   map[string]any{"type": "context.usage", "payload": map[string]any{"rate": 0.5, "tokens_used": 100.0}},
			wantEvt: "context.usage",
		},
		{
			name:    "chat.ask_user_question分支",
			chunk:   map[string]any{"type": "chat.ask_user_question", "payload": map[string]any{"request_id": "r1", "questions": []any{}}},
			wantEvt: "chat.ask_user_question",
		},
		{
			name:    "controller_output task_completion→nil",
			chunk:   map[string]any{"type": "controller_output", "payload": map[string]any{"type": "task_completion"}},
			wantNil: true,
		},
		{
			name:    "controller_output task_failed→chat.error",
			chunk:   map[string]any{"type": "controller_output", "payload": map[string]any{"type": "task_failed", "data": []any{map[string]any{"text": "任务失败"}}}},
			wantEvt: "chat.error",
		},
		{
			name:    "answer error→chat.error",
			chunk:   map[string]any{"type": "answer", "payload": map[string]any{"result_type": "error", "output": "查询失败"}},
			wantEvt: "chat.error",
		},
		{
			name:    "answer chunked→chat.delta",
			chunk:   map[string]any{"type": "answer", "payload": map[string]any{"output": map[string]any{"output": "部分内容", "chunked": true}}},
			wantEvt: "chat.delta",
		},
		{
			name:    "answer 非chunked→chat.final",
			chunk:   map[string]any{"type": "answer", "payload": map[string]any{"output": map[string]any{"output": "完整内容", "chunked": false}}},
			wantEvt: "chat.final",
		},
		{
			name:    "未知type兜底chat.前缀",
			chunk:   map[string]any{"type": "weird_type", "payload": map[string]any{"content": "x"}},
			wantEvt: "chat.weird_type",
		},
		{
			name:    "未知type空payload→content兜底",
			chunk:   map[string]any{"type": "weird_type", "payload": map[string]any{}},
			wantEvt: "chat.weird_type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseTypedChunkMap(tt.chunk)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("parseTypedChunkMap() = %v, 期望 nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("parseTypedChunkMap() = nil, 期望非 nil")
			}
			if evt, _ := got["event_type"].(string); evt != tt.wantEvt {
				t.Fatalf("event_type = %q, 期望 %q", evt, tt.wantEvt)
			}
		})
	}
}

// TestParseTypedChunkMap_team命名空间直传 Python 兜底分支：payload.event_type
// 以 team. 开头时不包 chat. 前缀、键值原样直传（team.completed 等控制事件路径）。
func TestParseTypedChunkMap_team命名空间直传(t *testing.T) {
	chunk := map[string]any{
		"type": "message",
		"payload": map[string]any{
			"event_type":   "team.completed",
			"member_count": 3.0,
			"task_count":   2.0,
		},
	}
	got := parseTypedChunkMap(chunk)
	if got == nil {
		t.Fatal("parseTypedChunkMap() = nil")
	}
	if evt, _ := got["event_type"].(string); evt != "team.completed" {
		t.Fatalf("event_type = %q, 期望 team.completed（team. 直传，非 chat.message）", evt)
	}
	if mc, _ := got["member_count"].(float64); mc != 3.0 {
		t.Fatalf("member_count = %v, 期望 3.0", got["member_count"])
	}
}

// TestParseDictChunkMap_主要分支 验证 dict 形状解析（evolution 展平事件等无 payload 键输入）。
func TestParseDictChunkMap_主要分支(t *testing.T) {
	// event_type 键存在 → 原样返回（serialize）
	m := parseDictChunkMap(map[string]any{"event_type": "chat.delta", "content": "x"})
	if m == nil || m["event_type"] != "chat.delta" {
		t.Fatalf("event_type 键直传失败: %v", m)
	}

	// type 键 → event_type 派生 + 其余键透传
	m = parseDictChunkMap(map[string]any{"type": "tool_call", "name": "t1"})
	if m == nil || m["event_type"] != "tool_call" {
		t.Fatalf("type→event_type 派生失败: %v", m)
	}
	if _, hasType := m["type"]; hasType {
		t.Fatal("type 键应被剔除（Python 剔除语义）")
	}
	if m["name"] != "t1" {
		t.Fatalf("其余键透传失败: %v", m)
	}

	// content 键空白 → nil
	m = parseDictChunkMap(map[string]any{"content": "  "})
	if m != nil {
		t.Fatalf("空白 content 应返回 nil, got %v", m)
	}

	// content 键有值 → chat.delta
	m = parseDictChunkMap(map[string]any{"content": "hello"})
	if m == nil || m["event_type"] != "chat.delta" {
		t.Fatalf("content→chat.delta 失败: %v", m)
	}

	// output 键 error → chat.error
	m = parseDictChunkMap(map[string]any{"output": "失败信息", "result_type": "error"})
	if m == nil || m["event_type"] != "chat.error" {
		t.Fatalf("output error→chat.error 失败: %v", m)
	}

	// 无匹配键 → 原样返回（Python 兜底 return chunk）
	m = parseDictChunkMap(map[string]any{"other": 1})
	if m == nil || m["other"] != 1 {
		t.Fatalf("无匹配键应原样返回: %v", m)
	}
}

// TestEvolutionLogic_EventType_shape 对齐 Python event_type() 优先级：
// 先读 evt.type 属性（Go: "type" 键，chunkToMap 展平写入），
// 再读 payload.event_type（Go: "event_type" 键）。
// S-20 关联修复：审批事件 Type="chat.ask_user_question" 且 payload 无 event_type 键。
func TestEvolutionLogic_EventType_shape(t *testing.T) {
	// typed 形状（chunkToMap native 展平产出）：type 键 + payload 键提升
	typedEvt := map[string]any{
		"type":            "chat.ask_user_question",
		"request_id":      "r1",
		"questions":       []any{},
		"_evolution_meta": map[string]any{"event_kind": "approval"},
	}
	if got := evolutionlogic.EventType(typedEvt); got != "chat.ask_user_question" {
		t.Fatalf("EventType(typed) = %q, 期望 chat.ask_user_question（type 键优先）", got)
	}
	if !evolutionlogic.IsEvolutionApprovalEvent(typedEvt) {
		t.Fatal("IsEvolutionApprovalEvent(typed) = false——审批事件被误判")
	}

	// dict 形状：event_type 键
	dictEvt := map[string]any{"event_type": "chat.ask_user_question"}
	if got := evolutionlogic.EventType(dictEvt); got != "chat.ask_user_question" {
		t.Fatalf("EventType(dict) = %q, 期望 chat.ask_user_question（event_type 键兜底）", got)
	}

	// type 键优先于 event_type 键（对齐 Python getattr(evt, "type") 优先）
	bothEvt := map[string]any{"type": "llm_reasoning", "event_type": "chat.delta"}
	if got := evolutionlogic.EventType(bothEvt); got != "llm_reasoning" {
		t.Fatalf("EventType(both) = %q, 期望 llm_reasoning（type 键优先，对齐 Python）", got)
	}
}

// TestEvolutionOutputSchemasToMaps_消费链端到端 验证 evolution 展平事件经
// EventType/IsEvolutionApprovalEvent 判定的完整链路（native 展平形状）。
func TestEvolutionOutputSchemasToMaps_消费链端到端(t *testing.T) {
	events := []*agentschema.OutputSchema{
		{Type: "chat.ask_user_question", Payload: map[string]any{
			"request_id":      "req-1",
			"questions":       []any{map[string]any{"question": "接受演进?"}},
			"_evolution_meta": map[string]any{"event_kind": "approval"},
		}},
		{Type: "llm_reasoning", Payload: map[string]any{
			"content":         "[Evolution] 进度更新",
			"_evolution_meta": map[string]any{"event_kind": "progress", "stage": "analyzing"},
		}},
	}
	result := evolutionOutputSchemasToMaps(events)
	if len(result) != 2 {
		t.Fatalf("len(result) = %d, 期望 2", len(result))
	}

	// 审批事件：IsEvolutionApprovalEvent 必须为 true（ EventType 读 type 键）
	if !evolutionlogic.IsEvolutionApprovalEvent(result[0]) {
		t.Fatalf("审批事件判定失败: type=%q event_type=%q", result[0]["type"], result[0]["event_type"])
	}
	// request_id 从展平 payload 顶层读取
	rid := evolutionlogic.ExtractEvolutionRequestID(result[0])
	if rid == nil || *rid != "req-1" {
		t.Fatalf("ExtractEvolutionRequestID = %v, 期望 req-1", rid)
	}
	// 进度事件：progress 判定
	if evolutionlogic.IsEvolutionOutcomeEvent(result[1]) {
		t.Fatal("进度事件被误判为 outcome")
	}
	// watch 循环的 parsed 推送：llm_reasoning → chat.reasoning
	parsed := utilsParseStreamChunk(result[1])
	if parsed == nil {
		t.Fatal("utilsParseStreamChunk(进度事件) 返回 nil")
	}
	if evt, _ := parsed["event_type"].(string); evt != "chat.reasoning" {
		t.Fatalf("parsed event_type = %q, 期望 chat.reasoning", evt)
	}
}

// TestChunkToMap_ControllerOutputPayload桥接 S-03 回归测试。
// 生产端 executor.go/task_scheduler.go 产出 *OutputSchema{Payload: *ControllerOutputPayload}——
// 原 map-only 断言返回 nil 导致 task_failed 事件整块丢弃。
// 桥接后形状对齐 Python model_dump(mode="json")：{type,data:[{text}],metadata}
// 并提升顶层（type/index/payload）。
func TestChunkToMap_ControllerOutputPayload桥接(t *testing.T) {
	cop := &cschema.ControllerOutputPayload{
		Type: "task_failed",
		Data: []cschema.DataFrame{
			&cschema.TextDataFrame{Text: "工具执行失败 [error_code=5001]"},
		},
		Metadata: map[string]any{"task_id": "t-1"},
	}
	chunk := &agentschema.OutputSchema{
		Type:    "task_failed",
		Index:   7,
		Payload: cop,
	}
	m := chunkToMap(chunk)
	if m == nil {
		t.Fatal("结构体 payload 应被桥接而非返回 nil（S-03）")
	}
	// 顶层提升键
	if m["type"] != "task_failed" {
		t.Errorf("顶层 type 应为 task_failed，实际 %v", m["type"])
	}
	if idx, _ := m["index"].(int); idx != 7 {
		t.Errorf("顶层 index 应为 7，实际 %v", m["index"])
	}
	// payload 嵌套键（对齐 model_dump）
	payload, ok := m["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload 应为 map，实际 %T", m["payload"])
	}
	if payload["type"] != "task_failed" {
		t.Errorf("payload.type 应为 task_failed，实际 %v", payload["type"])
	}
	data, ok := payload["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("payload.data 应为 1 元素切片，实际 %v", payload["data"])
	}
	dm, _ := data[0].(map[string]any)
	if dm["text"] != "工具执行失败 [error_code=5001]" {
		t.Errorf("data[0].text 应为错误文本，实际 %v", dm["text"])
	}
	if payload["metadata"].(map[string]any)["task_id"] != "t-1" {
		t.Errorf("metadata.task_id 应保留，实际 %v", payload["metadata"])
	}
	// payload 内容键同时提升顶层（方案A 半展平，evolutionlogic 判定函数读顶层键）
	if m["metadata"].(map[string]any)["task_id"] != "t-1" {
		t.Errorf("顶层 metadata.task_id 应保留，实际 %v", m["metadata"])
	}
}

// TestChunkToMap_ControllerOutputPayload值类型 值类型（非指针）桥接分支。
func TestChunkToMap_ControllerOutputPayload值类型(t *testing.T) {
	cop := cschema.ControllerOutputPayload{
		Type: "task_completion",
		Data: []cschema.DataFrame{
			&cschema.JsonDataFrame{Data: map[string]any{"result": "done"}},
		},
	}
	chunk := agentschema.OutputSchema{Type: "task_completion", Payload: cop}
	m := chunkToMap(chunk)
	if m == nil {
		t.Fatal("值类型结构体 payload 也应被桥接（S-03）")
	}
	if m["type"] != "task_completion" {
		t.Errorf("type 应为 task_completion，实际 %v", m["type"])
	}
	payload := m["payload"].(map[string]any)
	data, _ := payload["data"].([]any)
	dm, _ := data[0].(map[string]any)
	if dm["data"].(map[string]any)["result"] != "done" {
		t.Errorf("JsonDataFrame.data 应保留，实际 %v", dm)
	}
}

// TestChunkToMap_NilControllerOutputPayload nil 指针防御分支。
func TestChunkToMap_NilControllerOutputPayload(t *testing.T) {
	if m := payloadToMap((*cschema.ControllerOutputPayload)(nil)); m != nil {
		t.Errorf("nil 指针应返回 nil，实际 %v", m)
	}
	if m := payloadToMap(nil); m != nil {
		t.Errorf("nil payload 应返回 nil，实际 %v", m)
	}
}
