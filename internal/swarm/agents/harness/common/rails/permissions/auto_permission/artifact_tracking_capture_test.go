package auto_permission

import (
	"context"
	"testing"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	runtimesandbox "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
)

// TestAfterToolCall_nilCbc 验证 nil 上下文安全。
func TestAfterToolCall_nilCbc(t *testing.T) {
	rail := newBridgeRail(t)
	if err := rail.AfterToolCall(context.Background(), nil); err != nil {
		t.Errorf("err = %v", err)
	}
}

// TestAfterToolCall_正常转发 验证转发给父轨且不报错。
func TestAfterToolCall_正常转发(t *testing.T) {
	rail := newBridgeRail(t)
	inputs := &agentinterfaces.ToolCallInputs{ToolName: "bash"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
	if err := rail.AfterToolCall(context.Background(), cbc); err != nil {
		t.Errorf("err = %v", err)
	}
}

// TestAfterToolCall_清理执行域标记 验证调用不 panic 且 ctx 派生不污染入参。
func TestAfterToolCall_清理执行域标记(t *testing.T) {
	rail := newBridgeRail(t)
	required := runtimesandbox.RequireNoHostFallback(context.Background())
	inputs := &agentinterfaces.ToolCallInputs{ToolName: "bash"}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	if err := rail.AfterToolCall(required, cbc); err != nil {
		t.Errorf("err = %v", err)
	}
	// 偏差 V-16：派生 ctx 无法回传，上游标记仍为 true
	if !runtimesandbox.NoHostFallbackRequired(required) {
		t.Error("上游 ctx 不应被修改（ctx 不可变）")
	}
}
