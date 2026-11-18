package workflow

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
)

// TestNewWorkflowSpec_默认值 对齐 Python pydantic default_factory=dict
func TestNewWorkflowSpec_默认值(t *testing.T) {
	spec := NewWorkflowSpec()
	if spec.Edges == nil || spec.StreamEdges == nil || spec.CompConfigs == nil ||
		spec.StreamSourceGroups == nil || spec.StartNodes == nil {
		t.Fatal("默认字段应初始化为空容器（default_factory=dict/list）")
	}
	if len(spec.StreamEdges) != 0 {
		t.Fatal("StreamEdges 应为空")
	}
}

// TestNodeSpec_默认值 对齐 Python NodeSpec Field 默认值
func TestNodeSpec_默认值(t *testing.T) {
	spec, err := NewNodeSpec()
	if err != nil {
		t.Fatalf("默认构造不应报错: %v", err)
	}
	if spec.Abilities == nil || len(spec.Abilities) != 0 {
		t.Fatal("Abilities 默认应为空列表")
	}
	if spec.MaxRetries != 0 {
		t.Fatal("MaxRetries 默认 0")
	}
	if spec.Timeout != -1.0 {
		t.Fatal("Timeout 默认 -1.0（<=0 无超时）")
	}
}

// TestNodeSpec_Abilities字段 供 ActorManager 消费
func TestNodeSpec_Abilities字段(t *testing.T) {
	spec, _ := NewNodeSpec()
	spec.Abilities = []components.ComponentAbility{components.ComponentAbilityCollect, components.ComponentAbilityTransform}
	if len(spec.Abilities) != 2 || spec.Abilities[0] != components.ComponentAbilityCollect {
		t.Fatal("Abilities 读写失败")
	}
}

// TestNewWorkflowConfig_默认值 card 必填 + spec 补建 + 深度默认 5
func TestNewWorkflowConfig_默认值(t *testing.T) {
	wfc, err := NewWorkflowConfig(&WorkflowCard{Version: "v1"})
	if err != nil {
		t.Fatalf("合法构造不应报错: %v", err)
	}
	if wfc.Spec == nil || wfc.Spec.Edges == nil {
		t.Fatal("spec 为 nil 应补建空 spec（default_factory）")
	}
	if wfc.WorkflowMaxNestingDepth != 5 {
		t.Fatalf("默认嵌套深度应 5, got %d", wfc.WorkflowMaxNestingDepth)
	}
	if wfc.Card.Version != "v1" {
		t.Fatal("card 应原样保留")
	}
}

// TestNewWorkflowConfig_校验拒绝 复刻 pydantic ValidationError 拒绝语义
func TestNewWorkflowConfig_校验拒绝(t *testing.T) {
	if _, err := NewWorkflowConfig(nil); err == nil {
		t.Fatal("card nil 应 error（缺必填）")
	}
	if _, err := NewWorkflowConfig(&WorkflowCard{}, WithMaxNestingDepth(-1)); err == nil {
		t.Fatal("depth -1 应 error（ge=0）")
	}
	if _, err := NewWorkflowConfig(&WorkflowCard{}, WithMaxNestingDepth(11)); err == nil {
		t.Fatal("depth 11 应 error（le=10）")
	}
	wfc, err := NewWorkflowConfig(&WorkflowCard{}, WithMaxNestingDepth(0))
	if err != nil || wfc.WorkflowMaxNestingDepth != 0 {
		t.Fatalf("depth 0 是合法值: err=%v depth=%d", err, wfc.WorkflowMaxNestingDepth)
	}
}

// TestNewWorkflowConfig_WithWorkflowSpec nil 补建 / 非 nil 保留
func TestNewWorkflowConfig_WithWorkflowSpec(t *testing.T) {
	wfc, _ := NewWorkflowConfig(&WorkflowCard{}, WithWorkflowSpec(nil))
	if wfc.Spec == nil {
		t.Fatal("显式 nil spec 应补建")
	}
	spec := NewWorkflowSpec()
	spec.StartNodes = []string{"a"}
	wfc2, _ := NewWorkflowConfig(&WorkflowCard{}, WithWorkflowSpec(spec))
	if len(wfc2.Spec.StartNodes) != 1 {
		t.Fatal("非 nil spec 应原样保留")
	}
}

// TestNewNodeSpec_校验 max_retries ge=0 越界拒绝
func TestNewNodeSpec_校验(t *testing.T) {
	if _, err := NewNodeSpec(WithMaxRetries(-1)); err == nil {
		t.Fatal("负 retries 应 error")
	}
}

// TestNodeSpec_序列化键名 json tag 对齐 pydantic snake_case
func TestNodeSpec_序列化键名(t *testing.T) {
	name := "comp"
	ns, _ := NewNodeSpec(WithMaxRetries(3))
	ns.Name = &name
	ns.IOConfigs = &CompIOConfig{InputsSchema: map[string]any{"x": "1"}}
	bs, err := json.Marshal(ns)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"io_configs"`, `"max_retries"`, `"timeout"`, `"name"`, `"abilities"`, `"stream_io_configs"`, `"exception_config"`} {
		if !strings.Contains(string(bs), key) {
			t.Fatalf("缺键 %s: %s", key, bs)
		}
	}
}

// TestWorkflowConfig_序列化键名 snake_case tag 对齐 pydantic
func TestWorkflowConfig_序列化键名(t *testing.T) {
	card := &WorkflowCard{Version: "v1"}
	card.Name = "wf"
	wfc, _ := NewWorkflowConfig(card, WithMaxNestingDepth(3))
	bs, _ := json.Marshal(wfc)
	for _, key := range []string{`"card"`, `"spec"`, `"workflow_max_nesting_depth"`} {
		if !strings.Contains(string(bs), key) {
			t.Fatalf("缺键 %s: %s", key, bs)
		}
	}
}

// TestCompIOConfig_Transformer可承载 any 槽（D-15-1：Dict | Transformer 无 Go 对应）
func TestCompIOConfig_Transformer可承载(t *testing.T) {
	io := &CompIOConfig{InputsSchema: utils.MessageTransformer(
		func(m map[string]any) map[string]any { return m })}
	if _, ok := io.InputsSchema.(utils.MessageTransformer); !ok {
		t.Fatal("Transformer 应可放入 any 槽")
	}
}

// TestExceptionConfig_平铺序列化 extra=allow 平铺进 JSON
func TestExceptionConfig_平铺序列化(t *testing.T) {
	ec := &ExceptionConfig{HandleType: "retry", Extra: map[string]any{"strategy": "fallback"}}
	bs, err := json.Marshal(ec)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(bs, &m); err != nil {
		t.Fatal(err)
	}
	if m["handle_type"] != "retry" || m["strategy"] != "fallback" {
		t.Fatalf("Extra 应平铺: %s", bs)
	}
	// 往返
	var ec2 ExceptionConfig
	if err := json.Unmarshal(bs, &ec2); err != nil {
		t.Fatal(err)
	}
	if ec2.HandleType != "retry" || ec2.Extra["strategy"] != "fallback" {
		t.Fatalf("往返失败: handle=%q extra=%v", ec2.HandleType, ec2.Extra)
	}
	// 无 Extra：handle_type 归位、Extra 空
	var ec3 ExceptionConfig
	if err := json.Unmarshal([]byte(`{"handle_type":"interrupt"}`), &ec3); err != nil {
		t.Fatal(err)
	}
	if ec3.HandleType != "interrupt" || len(ec3.Extra) != 0 {
		t.Fatalf("无附加字段: handle=%q extra=%v", ec3.HandleType, ec3.Extra)
	}
}

// TestNewExceptionConfig_默认值 对齐 Python handle_type 默认 "interrupt"
func TestNewExceptionConfig_默认值(t *testing.T) {
	ec := NewExceptionConfig()
	if ec.HandleType != "interrupt" {
		t.Fatalf("handle_type 默认应为 interrupt, got %q", ec.HandleType)
	}
	if ec.Extra != nil {
		t.Fatal("Extra 默认应为 nil")
	}
	// 序列化应输出 Python 默认值形态
	bs, _ := json.Marshal(ec)
	if !strings.Contains(string(bs), `"handle_type":"interrupt"`) {
		t.Fatalf("序列化应含默认 handle_type: %s", bs)
	}
}
