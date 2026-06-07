package tool

import (
	"fmt"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewToolCard(t *testing.T) {
	card := NewToolCard("weather", "查询天气", nil, nil)
	if card.Name != "weather" {
		t.Errorf("Name = %q, want %q", card.Name, "weather")
	}
	if card.Description != "查询天气" {
		t.Errorf("Description = %q, want %q", card.Description, "查询天气")
	}
	if card.ID == "" {
		t.Error("ID 不应为空")
	}
	if card.InputParams != nil {
		t.Errorf("InputParams = %v, want nil", card.InputParams)
	}
	if card.Properties == nil {
		t.Error("Properties 不应为 nil")
	}
	if len(card.Properties) != 0 {
		t.Errorf("Properties 应为空 map，实际有 %d 项", len(card.Properties))
	}
}

func TestNewToolCard_带参数(t *testing.T) {
	params := []*schema.Param{
		schema.NewStringParam("city", "城市名", true),
	}
	props := map[string]any{"source": "openweather"}
	card := NewToolCard("weather", "查询天气", params, props)
	if len(card.InputParams) != 1 {
		t.Errorf("InputParams 长度 = %d, want 1", len(card.InputParams))
	}
	if card.Properties["source"] != "openweather" {
		t.Errorf("Properties[source] = %v, want openweather", card.Properties["source"])
	}
}

func TestNewToolCard_ID自动生成(t *testing.T) {
	card1 := NewToolCard("a", "", nil, nil)
	card2 := NewToolCard("b", "", nil, nil)
	if card1.ID == card2.ID {
		t.Error("两个 ToolCard 的 ID 不应相同")
	}
}

func TestToolCard_String(t *testing.T) {
	card := NewToolCard("weather", "查询天气", nil, nil)
	s := card.String()
	if s != "id="+card.ID+",name=weather" {
		t.Errorf("String() = %q, want %q", s, "id="+card.ID+",name=weather")
	}
}

func TestNewToolCallOptions(t *testing.T) {
	opts := NewToolCallOptions(
		WithSkipNoneValue(true),
		WithTimeout(30.0),
		WithMaxResponseBytes(1024),
		WithRaiseForStatus(true),
	)
	if !opts.SkipNoneValue {
		t.Error("SkipNoneValue 应为 true")
	}
	if opts.SkipInputsValidate {
		t.Error("SkipInputsValidate 默认应为 false")
	}
	if opts.Timeout != 30.0 {
		t.Errorf("Timeout = %f, want 30.0", opts.Timeout)
	}
	if opts.MaxResponseBytes != 1024 {
		t.Errorf("MaxResponseBytes = %d, want 1024", opts.MaxResponseBytes)
	}
	if !opts.RaiseForStatus {
		t.Error("RaiseForStatus 应为 true")
	}
}

func TestNewToolCallOptions_空选项(t *testing.T) {
	opts := NewToolCallOptions()
	if opts.SkipNoneValue {
		t.Error("SkipNoneValue 默认应为 false")
	}
	if opts.Timeout != 0 {
		t.Errorf("Timeout 默认应为 0, 实际 %f", opts.Timeout)
	}
}

func TestValidateToolCard_NilCard(t *testing.T) {
	err := ValidateToolCard(nil)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误类型应为 *BaseError，实际 %T", err)
	}
	if baseErr.Code() != 182000 {
		t.Errorf("Code = %d, want 182000", baseErr.Code())
	}
}

func TestValidateToolCard_空ID(t *testing.T) {
	card := &ToolCard{
		BaseCard: schema.BaseCard{ID: "", Name: "test"},
	}
	err := ValidateToolCard(card)
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误类型应为 *BaseError，实际 %T", err)
	}
	if baseErr.Code() != 182000 {
		t.Errorf("Code = %d, want 182000", baseErr.Code())
	}
}

func TestValidateToolCard_合法(t *testing.T) {
	card := NewToolCard("test", "测试", nil, nil)
	err := ValidateToolCard(card)
	if err != nil {
		t.Errorf("合法 ToolCard 不应返回错误: %v", err)
	}
}

func TestNewErrStreamNotSupported(t *testing.T) {
	err := NewErrStreamNotSupported("weather_tool")
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
	if err.Code() != 182010 {
		t.Errorf("Code = %d, want 182010", err.Code())
	}
}

func TestStreamChunk_数据块(t *testing.T) {
	chunk := StreamChunk{Data: map[string]any{"result": "ok"}}
	if chunk.Done {
		t.Error("Done 应为 false")
	}
	if chunk.Error != nil {
		t.Error("Error 应为 nil")
	}
}

func TestStreamChunk_结束标记(t *testing.T) {
	chunk := StreamChunk{Done: true}
	if !chunk.Done {
		t.Error("Done 应为 true")
	}
	if chunk.Data != nil {
		t.Error("Data 应为 nil")
	}
}

func TestStreamChunk_错误标记(t *testing.T) {
	chunk := StreamChunk{Error: fmt.Errorf("timeout")}
	if chunk.Error == nil {
		t.Error("Error 不应为 nil")
	}
}
