package database

import (
	"encoding/json"
	"testing"
)

// TestTeam_JSON序列化_roundtrip 测试 Team 结构体 JSON 序列化/反序列化。
func TestTeam_JSON序列化_roundtrip(t *testing.T) {
	team := &Team{
		TeamName:         "alpha",
		DisplayName:      "Alpha Team",
		LeaderMemberName: "leader1",
		Desc:             "测试团队",
		Prompt:           "你是一个领导者",
		Created:          1700000000000,
		UpdatedAt:        1700000001000,
	}

	data, err := json.Marshal(team)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var decoded Team
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if decoded.TeamName != team.TeamName {
		t.Errorf("TeamName: got %q, want %q", decoded.TeamName, team.TeamName)
	}
	if decoded.DisplayName != team.DisplayName {
		t.Errorf("DisplayName: got %q, want %q", decoded.DisplayName, team.DisplayName)
	}
	if decoded.LeaderMemberName != team.LeaderMemberName {
		t.Errorf("LeaderMemberName: got %q, want %q", decoded.LeaderMemberName, team.LeaderMemberName)
	}
	if decoded.Desc != team.Desc {
		t.Errorf("Desc: got %q, want %q", decoded.Desc, team.Desc)
	}
	if decoded.Prompt != team.Prompt {
		t.Errorf("Prompt: got %q, want %q", decoded.Prompt, team.Prompt)
	}
	if decoded.Created != team.Created {
		t.Errorf("Created: got %d, want %d", decoded.Created, team.Created)
	}
	if decoded.UpdatedAt != team.UpdatedAt {
		t.Errorf("UpdatedAt: got %d, want %d", decoded.UpdatedAt, team.UpdatedAt)
	}
}

// TestTeam_omitempty零值 空字符串和零值时间戳在 JSON 中应被省略。
func TestTeam_omitempty零值(t *testing.T) {
	team := &Team{
		TeamName:         "beta",
		DisplayName:      "Beta Team",
		LeaderMemberName: "leader2",
		Created:          1700000000000,
	}

	data, err := json.Marshal(team)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	// omitempty 字段不应出现在 JSON 中
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("解析 raw JSON 失败: %v", err)
	}

	if _, exists := raw["desc"]; exists {
		t.Error("desc 空字符串不应出现在 JSON 中（omitempty）")
	}
	if _, exists := raw["prompt"]; exists {
		t.Error("prompt 空字符串不应出现在 JSON 中（omitempty）")
	}
	if _, exists := raw["updated_at"]; exists {
		t.Error("updated_at 零值不应出现在 JSON 中（omitempty）")
	}
}

// TestTeamMember_JSON序列化_roundtrip 测试 TeamMember 全字段序列化/反序列化。
func TestTeamMember_JSON序列化_roundtrip(t *testing.T) {
	member := &TeamMember{
		MemberName:      "agent1",
		TeamName:        "alpha",
		DisplayName:     "Agent One",
		Desc:            "智能助手",
		AgentCard:       `{"name":"agent1"}`,
		Status:          "ready",
		ExecutionStatus: "idle",
		Mode:            "build_mode",
		Role:            "teammate",
		Prompt:          "你是一个助手",
		ModelRefJSON:    `{"model_id":"qwen-max","model_name":"qwen-max"}`,
		UpdatedAt:       1700000002000,
	}

	data, err := json.Marshal(member)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var decoded TeamMember
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if decoded.MemberName != member.MemberName {
		t.Errorf("MemberName: got %q, want %q", decoded.MemberName, member.MemberName)
	}
	if decoded.TeamName != member.TeamName {
		t.Errorf("TeamName: got %q, want %q", decoded.TeamName, member.TeamName)
	}
	if decoded.DisplayName != member.DisplayName {
		t.Errorf("DisplayName: got %q, want %q", decoded.DisplayName, member.DisplayName)
	}
	if decoded.Desc != member.Desc {
		t.Errorf("Desc: got %q, want %q", decoded.Desc, member.Desc)
	}
	if decoded.AgentCard != member.AgentCard {
		t.Errorf("AgentCard: got %q, want %q", decoded.AgentCard, member.AgentCard)
	}
	if decoded.Status != member.Status {
		t.Errorf("Status: got %q, want %q", decoded.Status, member.Status)
	}
	if decoded.ExecutionStatus != member.ExecutionStatus {
		t.Errorf("ExecutionStatus: got %q, want %q", decoded.ExecutionStatus, member.ExecutionStatus)
	}
	if decoded.Mode != member.Mode {
		t.Errorf("Mode: got %q, want %q", decoded.Mode, member.Mode)
	}
	if decoded.Role != member.Role {
		t.Errorf("Role: got %q, want %q", decoded.Role, member.Role)
	}
	if decoded.Prompt != member.Prompt {
		t.Errorf("Prompt: got %q, want %q", decoded.Prompt, member.Prompt)
	}
	if decoded.ModelRefJSON != member.ModelRefJSON {
		t.Errorf("ModelRefJSON: got %q, want %q", decoded.ModelRefJSON, member.ModelRefJSON)
	}
	if decoded.UpdatedAt != member.UpdatedAt {
		t.Errorf("UpdatedAt: got %d, want %d", decoded.UpdatedAt, member.UpdatedAt)
	}
}

// TestTeamMember_omitempty零值 omitempty 字段零值不应出现在 JSON 中。
func TestTeamMember_omitempty零值(t *testing.T) {
	member := &TeamMember{
		MemberName:  "agent2",
		TeamName:    "alpha",
		DisplayName: "Agent Two",
		AgentCard:   `{"name":"agent2"}`,
		Status:      "unstarted",
		Mode:        "build_mode",
		Role:        "teammate",
	}

	data, err := json.Marshal(member)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("解析 raw JSON 失败: %v", err)
	}

	if _, exists := raw["desc"]; exists {
		t.Error("desc 空字符串不应出现在 JSON 中（omitempty）")
	}
	if _, exists := raw["execution_status"]; exists {
		t.Error("execution_status 空字符串不应出现在 JSON 中（omitempty）")
	}
	if _, exists := raw["prompt"]; exists {
		t.Error("prompt 空字符串不应出现在 JSON 中（omitempty）")
	}
	if _, exists := raw["model_ref_json"]; exists {
		t.Error("model_ref_json 空字符串不应出现在 JSON 中（omitempty）")
	}
	if _, exists := raw["updated_at"]; exists {
		t.Error("updated_at 零值不应出现在 JSON 中（omitempty）")
	}
}

// TestTeamDynamicTablePrefixes_完整性 动态表前缀常量应包含所有 4 个前缀。
func TestTeamDynamicTablePrefixes_完整性(t *testing.T) {
	expected := []string{
		"team_task_dependency_",
		"team_task_",
		"team_message_",
		"message_read_status_",
	}
	if len(TeamDynamicTablePrefixes) != len(expected) {
		t.Fatalf("TeamDynamicTablePrefixes 长度: got %d, want %d", len(TeamDynamicTablePrefixes), len(expected))
	}
	for i, prefix := range TeamDynamicTablePrefixes {
		if prefix != expected[i] {
			t.Errorf("prefix[%d]: got %q, want %q", i, prefix, expected[i])
		}
	}
}

// TestTeamStaticTablesToClear_完整性 静态表清空常量应包含 team_info 和 team_member。
func TestTeamStaticTablesToClear_完整性(t *testing.T) {
	expected := []string{"team_info", "team_member"}
	if len(TeamStaticTablesToClear) != len(expected) {
		t.Fatalf("TeamStaticTablesToClear 长度: got %d, want %d", len(TeamStaticTablesToClear), len(expected))
	}
	for i, table := range TeamStaticTablesToClear {
		if table != expected[i] {
			t.Errorf("table[%d]: got %q, want %q", i, table, expected[i])
		}
	}
}

// TestTeamMessageBase_JSON 直发消息序列化测试
func TestTeamMessageBase_JSON(t *testing.T) {
	dm := TeamMessageBase{
		MessageID:      "msg_1",
		TeamName:       "team1",
		FromMemberName: "alice",
		ToMemberName:   "bob",
		Content:        "hello",
		Timestamp:      1000,
		Broadcast:      false,
		IsRead:         BoolPtr(false),
	}
	data, err := json.Marshal(dm)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var got TeamMessageBase
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if got.MessageID != "msg_1" {
		t.Errorf("MessageID = %q, want msg_1", got.MessageID)
	}
	if got.IsRead == nil || *got.IsRead != false {
		t.Errorf("IsRead = %v, want *false", got.IsRead)
	}

	// 广播消息
	bm := TeamMessageBase{
		MessageID:      "msg_2",
		TeamName:       "team1",
		FromMemberName: "alice",
		Content:        "broadcast",
		Timestamp:      2000,
		Broadcast:      true,
		IsRead:         nil,
	}
	data, err = json.Marshal(bm)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var got2 TeamMessageBase
	if err := json.Unmarshal(data, &got2); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if got2.IsRead != nil {
		t.Errorf("IsRead = %v, want nil (广播)", got2.IsRead)
	}
}

// TestMessageReadStatusBase_JSON 测试已读水位模型序列化
func TestMessageReadStatusBase_JSON(t *testing.T) {
	rs := MessageReadStatusBase{
		MemberName: "alice",
		TeamName:   "team1",
		ReadAt:     Int64Ptr(5000),
	}
	data, err := json.Marshal(rs)
	if err != nil {
		t.Fatalf("Marshal 失败: %v", err)
	}
	var got MessageReadStatusBase
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal 失败: %v", err)
	}
	if got.ReadAt == nil || *got.ReadAt != 5000 {
		t.Errorf("ReadAt = %v, want *5000", got.ReadAt)
	}

	// nil ReadAt
	rs2 := MessageReadStatusBase{MemberName: "bob", TeamName: "team1"}
	data2, _ := json.Marshal(rs2)
	var got2 MessageReadStatusBase
	_ = json.Unmarshal(data2, &got2)
	if got2.ReadAt != nil {
		t.Errorf("ReadAt = %v, want nil", got2.ReadAt)
	}
}

// TestBoolPtr 测试 BoolPtr 辅助函数
func TestBoolPtr(t *testing.T) {
	p := BoolPtr(true)
	if p == nil || *p != true {
		t.Errorf("BoolPtr(true) = %v, want *true", p)
	}
	p2 := BoolPtr(false)
	if p2 == nil || *p2 != false {
		t.Errorf("BoolPtr(false) = %v, want *false", p2)
	}
}

// TestInt64Ptr 测试 Int64Ptr 辅助函数
func TestInt64Ptr(t *testing.T) {
	p := Int64Ptr(42)
	if p == nil || *p != 42 {
		t.Errorf("Int64Ptr(42) = %v, want *42", p)
	}
	p2 := Int64Ptr(0)
	if p2 == nil || *p2 != 0 {
		t.Errorf("Int64Ptr(0) = %v, want *0", p2)
	}
}
