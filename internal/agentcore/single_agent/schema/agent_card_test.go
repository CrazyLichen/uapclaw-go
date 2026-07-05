package schema

import (
	"encoding/json"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// testAgentInput 反射提取测试用输入结构体
type testAgentInput struct {
	Query string `json:"query" jsonschema:"description=搜索关键词,required"`
	Limit int    `json:"limit,omitempty" jsonschema:"description=返回数量上限"`
}

// testAgentOutput 反射提取测试用输出结构体
type testAgentOutput struct {
	Result string `json:"result" jsonschema:"description=搜索结果,required"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestAgentCard_ToolInfo_有参数(t *testing.T) {
	card := NewAgentCard(
		agentschema.WithAgentName("sub_agent"),
		agentschema.WithAgentDescription("子 Agent"),
		WithInputParams[testAgentInput](),
	)
	info := card.ToolInfo()
	if info.GetName() != "sub_agent" {
		t.Errorf("Name = %q, want sub_agent", info.GetName())
	}
	props, ok := info.GetParameters()["properties"].(map[string]any)
	if !ok {
		t.Fatalf("parameters 缺少 properties，实际 %v", info.GetParameters())
	}
	if _, ok := props["query"]; !ok {
		t.Error("properties 中缺少 query 参数")
	}
}

func TestAgentCard_ToolInfo_无参数(t *testing.T) {
	card := NewAgentCard(agentschema.WithAgentName("no_params_agent"))
	info := card.ToolInfo()
	if info.GetName() != "no_params_agent" {
		t.Errorf("Name = %q, want no_params_agent", info.GetName())
	}
	typ, ok := info.GetParameters()["type"].(string)
	if !ok || typ != "object" {
		t.Errorf("无参数时 type 应为 object，实际 %v", info.GetParameters())
	}
}

func TestAgentCard_能力(t *testing.T) {
	card := NewAgentCard(agentschema.WithAgentName("ag"), agentschema.WithAgentDescription("Agent"))
	if card.AbilityName() != "ag" {
		t.Errorf("AbilityName = %q, want ag", card.AbilityName())
	}
	if card.AbilityKind() != schema.AbilityKindAgent {
		t.Errorf("AbilityKind = %v, want AbilityKindAgent", card.AbilityKind())
	}
	var _ schema.Ability = card // 编译期接口检查
}

func TestAgentCard_AbilityID(t *testing.T) {
	card := NewAgentCard(agentschema.WithAgentName("ag"))
	if got := card.AbilityID(); got != card.ID {
		t.Errorf("AbilityID() = %q, want %q", got, card.ID)
	}
}

func TestAgentCard_反射提取Input(t *testing.T) {
	card := NewAgentCard(
		agentschema.WithAgentName("reflect_agent"),
		WithInputParams[testAgentInput](),
	)
	if len(card.InputParams) != 2 {
		t.Fatalf("InputParams 长度期望 2，实际 %d", len(card.InputParams))
	}
	if card.InputParams[0].Name != "query" {
		t.Errorf("InputParams[0].Name = %q, want query", card.InputParams[0].Name)
	}
	if !card.InputParams[0].Required {
		t.Error("query 应为 required")
	}
	if card.InputParams[1].Name != "limit" {
		t.Errorf("InputParams[1].Name = %q, want limit", card.InputParams[1].Name)
	}
	if card.InputParams[1].Required {
		t.Error("limit 不应为 required（有 omitempty）")
	}
}

func TestAgentCard_反射提取Output(t *testing.T) {
	card := NewAgentCard(
		agentschema.WithAgentName("reflect_agent"),
		WithOutputParams[testAgentOutput](),
	)
	if len(card.OutputParams) != 1 {
		t.Fatalf("OutputParams 长度期望 1，实际 %d", len(card.OutputParams))
	}
	if card.OutputParams[0].Name != "result" {
		t.Errorf("OutputParams[0].Name = %q, want result", card.OutputParams[0].Name)
	}
}

func TestAgentCard_反射提取InputOutput(t *testing.T) {
	card := NewAgentCard(
		agentschema.WithAgentName("full_agent"),
		agentschema.WithAgentDescription("完整参数 Agent"),
		WithInputParams[testAgentInput](),
		WithOutputParams[testAgentOutput](),
	)
	if len(card.InputParams) != 2 {
		t.Errorf("InputParams 长度期望 2，实际 %d", len(card.InputParams))
	}
	if len(card.OutputParams) != 1 {
		t.Errorf("OutputParams 长度期望 1，实际 %d", len(card.OutputParams))
	}
}

func TestAgentCard_JSON序列化(t *testing.T) {
	card := NewAgentCard(
		agentschema.WithAgentName("json_agent"),
		agentschema.WithAgentDescription("JSON 测试"),
		WithInputParams[testAgentInput](),
		WithOutputParams[testAgentOutput](),
	)
	card.InterfaceURL = "http://localhost:8080/a2a"

	data, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}
	var decoded AgentCard
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}
	if decoded.Name != card.Name {
		t.Errorf("反序列化 Name 期望 %q，实际 %q", card.Name, decoded.Name)
	}
	if decoded.InterfaceURL != card.InterfaceURL {
		t.Errorf("反序列化 InterfaceURL 期望 %q，实际 %q", card.InterfaceURL, decoded.InterfaceURL)
	}
}

func TestAgentCard_字段对齐Python(t *testing.T) {
	card := NewAgentCard(
		agentschema.WithAgentName("align_agent"),
		agentschema.WithAgentDescription("对齐测试"),
	)
	if card.InputParams != nil {
		t.Errorf("默认 InputParams 应为 nil，实际 %v", card.InputParams)
	}
	if card.OutputParams != nil {
		t.Errorf("默认 OutputParams 应为 nil，实际 %v", card.OutputParams)
	}
	if card.InterfaceURL != "" {
		t.Errorf("默认 InterfaceURL 应为空，实际 %q", card.InterfaceURL)
	}
}

func TestAgentCard_ToolInfo与ToolCard一致(t *testing.T) {
	card := NewAgentCard(
		agentschema.WithAgentName("consistent_agent"),
		WithInputParams[testAgentInput](),
	)
	info := card.ToolInfo()

	typ, ok := info.GetParameters()["type"].(string)
	if !ok || typ != "object" {
		t.Errorf("type 应为 object，实际 %v", info.GetParameters())
	}
	props, ok := info.GetParameters()["properties"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 properties，实际 %v", info.GetParameters())
	}
	if _, ok := props["query"]; !ok {
		t.Error("properties 中缺少 query")
	}
	if _, ok := props["limit"]; !ok {
		t.Error("properties 中缺少 limit")
	}
	required, ok := info.GetParameters()["required"].([]string)
	if !ok {
		t.Fatalf("required 类型不对，实际 %T", info.GetParameters()["required"])
	}
	if len(required) != 1 || required[0] != "query" {
		t.Errorf("required 期望 [query]，实际 %v", required)
	}
}
