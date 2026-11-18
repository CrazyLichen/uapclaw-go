package workflow

import (
	"testing"

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
	spec := NewNodeSpec()
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
	spec := NewNodeSpec()
	spec.Abilities = []components.ComponentAbility{components.ComponentAbilityCollect, components.ComponentAbilityTransform}
	if len(spec.Abilities) != 2 || spec.Abilities[0] != components.ComponentAbilityCollect {
		t.Fatal("Abilities 读写失败")
	}
}
