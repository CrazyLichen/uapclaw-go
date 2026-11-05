package ability

import (
	"context"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// callbackSpyTool 记录是否被注入回调上下文。
type callbackSpyTool struct {
	// card 工具卡片
	card *tool.ToolCard
	// accepts 是否声明需要回调上下文
	accepts bool
	// accepted 实际是否收到回调上下文
	accepted bool
	// gotExtra 收到的 extra 内容
	gotExtra map[string]any
}

// Card 返回工具卡片。
func (c *callbackSpyTool) Card() *tool.ToolCard { return c.card }

// AcceptsToolCallbackContext 声明是否需要回调上下文。
func (c *callbackSpyTool) AcceptsToolCallbackContext() bool { return c.accepts }

// Invoke 记录注入结果。
func (c *callbackSpyTool) Invoke(_ context.Context, _ map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	options := &tool.ToolCallOptions{}
	for _, opt := range opts {
		opt(options)
	}
	if options.CallbackContext != nil {
		c.accepted = true
		c.gotExtra = options.CallbackContext.Extra()
	}
	return map[string]any{"ok": true}, nil
}

// Stream 不支持流式。
func (c *callbackSpyTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// TestExecuteTool_注入回调上下文 验证声明 acceptor 的工具收到回调上下文。
func TestExecuteTool_注入回调上下文(t *testing.T) {
	card := tool.NewToolCard("spy_tool", "回调上下文工具", nil, nil)
	spy := &callbackSpyTool{card: card, accepts: true}
	am := NewAbilityManager(newTestResourceMgr(map[string]tool.Tool{card.ID: spy}, nil, nil))
	am.Add(card)

	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.InvokeInputs{}, nil)
	cbc.SetExtra("mark", "1")

	_, err := am.executeSingleToolCall(context.Background(), cbc,
		llmschema.NewToolCall("c1", "spy_tool", `{}`), nil, "")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !spy.accepted {
		t.Fatal("实现 CallbackContextAcceptor 的工具应收到回调上下文")
	}
	if spy.gotExtra["mark"] != "1" {
		t.Fatalf("extra 应透传，got %v", spy.gotExtra)
	}
}

// TestExecuteTool_普通工具不注入 验证未声明的工具拿不到回调上下文。
func TestExecuteTool_普通工具不注入(t *testing.T) {
	card := tool.NewToolCard("plain_tool", "普通工具", nil, nil)
	spy := &callbackSpyTool{card: card, accepts: false}
	am := NewAbilityManager(newTestResourceMgr(map[string]tool.Tool{card.ID: spy}, nil, nil))
	am.Add(card)

	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.InvokeInputs{}, nil)
	_, err := am.executeSingleToolCall(context.Background(), cbc,
		llmschema.NewToolCall("c2", "plain_tool", `{}`), nil, "")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if spy.accepted {
		t.Fatal("未声明 acceptor 的工具不应收到回调上下文")
	}
}
