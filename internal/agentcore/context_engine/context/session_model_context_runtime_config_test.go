package context

import (
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
)

// TestSessionModelContext_ApplyRuntimeConfig 刷新引擎配置派生的原料与处理器实例。
func TestSessionModelContext_ApplyRuntimeConfig(t *testing.T) {
	oldConfig := ceschema.NewContextEngineConfig()
	oldConfig.ModelName = "model-a"
	oldConfig.ModelContextWindowTokens = map[string]int{"model-a": 800}
	oldConfig.MaxContextMessageNum = 10
	oldConfig.DefaultWindowMessageNum = 3
	oldConfig.DefaultWindowRoundNum = 2
	oldConfig.EnableReload = false

	oldProcessor := processor.NewBaseProcessor(nil)
	mc := NewSessionModelContext(
		"ctx_apply", "sess_apply", oldConfig, nil,
		[]iface.ContextProcessor{oldProcessor}, nil, nil, nil, nil)

	if got := mc.ContextMaxTokens(); got != 800 {
		t.Fatalf("初始上下文窗口应为 800，实际 %d", got)
	}
	if got := mc.messageBuffer.maxBufferSize; got != 10 {
		t.Fatalf("初始消息缓冲上限应为 10，实际 %d", got)
	}

	newConfig := ceschema.NewContextEngineConfig()
	newConfig.ModelName = "model-b"
	newConfig.ModelContextWindowTokens = map[string]int{"model-a": 800, "model-b": 600}
	newConfig.MaxContextMessageNum = 20
	newConfig.DefaultWindowMessageNum = 5
	newConfig.DefaultWindowRoundNum = 4
	newConfig.EnableReload = true

	newProcessor := processor.NewBaseProcessor(nil)
	mc.ApplyRuntimeConfig(newConfig, []iface.ContextProcessor{newProcessor})

	if got := mc.ContextMaxTokens(); got != 600 {
		t.Errorf("上下文窗口应刷新为 600，实际 %d", got)
	}
	if got := mc.modelName; got != "model-b" {
		t.Errorf("modelName 应刷新为 model-b，实际 %q", got)
	}
	if got := mc.messageBuffer.maxBufferSize; got != 20 {
		t.Errorf("消息缓冲上限应刷新为 20，实际 %d", got)
	}
	if got := mc.defaultWindowSize; got != 5 {
		t.Errorf("默认窗口消息数应刷新为 5，实际 %d", got)
	}
	if got := mc.defaultDialogueRound; got != 4 {
		t.Errorf("默认对话轮数应刷新为 4，实际 %d", got)
	}
	if !mc.enableReload {
		t.Error("enableReload 应刷新为 true")
	}
	if len(mc.Processors()) != 1 || mc.Processors()[0] != newProcessor {
		t.Error("处理器实例应替换为新切片")
	}
}
