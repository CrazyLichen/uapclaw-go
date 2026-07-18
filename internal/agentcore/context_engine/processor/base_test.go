package processor

import (
	"encoding/json"
	"fmt"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 结构体 ────────────────────────────

// testConfig 测试用处理器配置
type testConfig struct {
	Name  string
	Value int
}

// ──────────────────────────── 导出函数 ────────────────────────────

func (c *testConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("name 不能为空")
	}
	return nil
}

func (c *testConfig) SetModelDefaults(_ *llm_schema.ModelRequestConfig, _ *llm_schema.ModelClientConfig) {}

func (c *testConfig) GetModel() *llm_schema.ModelRequestConfig { return nil }

// TestContextEvent_字段默认值 验证 ContextEvent 零值
func TestContextEvent_字段默认值(t *testing.T) {
	var e iface.ContextEvent
	if e.EventType != "" {
		t.Errorf("EventType 零值应为空串，实际 %q", e.EventType)
	}
	if e.MessagesToModify != nil {
		t.Errorf("MessagesToModify 零值应为 nil，实际 %v", e.MessagesToModify)
	}
	if e.CompactSummary != "" {
		t.Errorf("CompactSummary 零值应为空串，实际 %q", e.CompactSummary)
	}
	if e.CompressionUsage != nil {
		t.Errorf("CompressionUsage 零值应为 nil，实际 %v", e.CompressionUsage)
	}
}

// TestContextEvent_构造 验证结构体字面量构造
func TestContextEvent_构造(t *testing.T) {
	e := &iface.ContextEvent{
		EventType:        "DialogueCompressor",
		MessagesToModify: []int{0, 1, 2},
		CompactSummary:   "压缩了3条消息",
		CompressionUsage: map[string]any{
			"calls":        1,
			"total_tokens": 500,
		},
	}
	if e.EventType != "DialogueCompressor" {
		t.Errorf("EventType = %q, want DialogueCompressor", e.EventType)
	}
	if len(e.MessagesToModify) != 3 {
		t.Errorf("MessagesToModify 长度 = %d, want 3", len(e.MessagesToModify))
	}
	if e.CompactSummary != "压缩了3条消息" {
		t.Errorf("CompactSummary = %q, want 压缩了3条消息", e.CompactSummary)
	}
	if e.CompressionUsage["calls"] != 1 {
		t.Errorf("CompressionUsage[calls] = %v, want 1", e.CompressionUsage["calls"])
	}
}

// TestContextEvent_JSON序列化 验证 JSON 序列化/反序列化
func TestContextEvent_JSON序列化(t *testing.T) {
	original := &iface.ContextEvent{
		EventType:        "MessageOffloader",
		MessagesToModify: []int{5},
		CompactSummary:   "卸载了1条消息",
		CompressionUsage: map[string]any{"input_tokens": float64(100)},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored iface.ContextEvent
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if restored.EventType != original.EventType {
		t.Errorf("EventType = %q, want %q", restored.EventType, original.EventType)
	}
	if len(restored.MessagesToModify) != 1 || restored.MessagesToModify[0] != 5 {
		t.Errorf("MessagesToModify = %v, want [5]", restored.MessagesToModify)
	}
	if restored.CompactSummary != original.CompactSummary {
		t.Errorf("CompactSummary = %q, want %q", restored.CompactSummary, original.CompactSummary)
	}
}

// TestContextEvent_JSON省略空字段 验证 omitempty 行为
func TestContextEvent_JSON省略空字段(t *testing.T) {
	e := iface.ContextEvent{EventType: "test"}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	// CompressionUsage 为 nil 时应省略
	if _, ok := m["compression_usage"]; ok {
		t.Error("compression_usage 应被 omitempty 省略")
	}
	// EventType 应保留
	if _, ok := m["event_type"]; !ok {
		t.Error("event_type 不应被省略")
	}
}

// TestProcessorConfig_校验通过 验证合法配置
func TestProcessorConfig_校验通过(t *testing.T) {
	c := &testConfig{Name: "test", Value: 10}
	if err := c.Validate(); err != nil {
		t.Errorf("Validate() 不应返回错误，实际: %v", err)
	}
}

// TestProcessorConfig_校验失败 验证非法配置
func TestProcessorConfig_校验失败(t *testing.T) {
	c := &testConfig{Name: "", Value: 10}
	if err := c.Validate(); err == nil {
		t.Error("Validate() 应返回错误，实际 nil")
	}
}

// TestNewBaseProcessor 验证 BaseProcessor 构造
func TestNewBaseProcessor(t *testing.T) {
	c := &testConfig{Name: "compressor", Value: 5}
	p := NewBaseProcessor(c)
	if p == nil {
		t.Fatal("NewBaseProcessor 返回 nil")
	}
	if p.Config() != c {
		t.Error("Config() 应返回传入的配置")
	}
	if p.compressionUsage != nil {
		t.Error("compressionUsage 初始值应为 nil")
	}
}

// TestProcessorOption_默认值 验证 ProcessorOption 零值
func TestProcessorOption_默认值(t *testing.T) {
	po := iface.NewProcessorOption()
	if po.SysOperation != nil {
		t.Error("SysOperation 默认应为 nil")
	}
	if po.OffloadHandle != "" {
		t.Error("OffloadHandle 默认应为空")
	}
	if po.OffloadType != "" {
		t.Error("OffloadType 默认应为空")
	}
	if po.OffloadPath != "" {
		t.Error("OffloadPath 默认应为空")
	}
	if po.Extra != nil {
		t.Error("Extra 默认应为 nil")
	}
}

// TestProcessorOption_选项函数 验证 With* 选项函数
func TestProcessorOption_选项函数(t *testing.T) {
	po := iface.NewProcessorOption(
		iface.WithOffloadHandle("abc123"),
		iface.WithOffloadType("filesystem"),
		iface.WithOffloadPath("/tmp/offload.json"),
		iface.WithExtra("key1", "value1"),
	)
	if po.OffloadHandle != "abc123" {
		t.Errorf("OffloadHandle = %q, want abc123", po.OffloadHandle)
	}
	if po.OffloadType != "filesystem" {
		t.Errorf("OffloadType = %q, want filesystem", po.OffloadType)
	}
	if po.OffloadPath != "/tmp/offload.json" {
		t.Errorf("OffloadPath = %q, want /tmp/offload.json", po.OffloadPath)
	}
	if po.Extra["key1"] != "value1" {
		t.Errorf("Extra[key1] = %v, want value1", po.Extra["key1"])
	}
}

// TestWithSysOperation 验证 SysOperation 选项
func TestWithSysOperation(t *testing.T) {
	var op sysop.SysOperation = &sysop.BaseSysOperation{}
	po := iface.NewProcessorOption(iface.WithSysOperation(op))
	if po.SysOperation == nil {
		t.Error("SysOperation 不应为 nil")
	}
}
