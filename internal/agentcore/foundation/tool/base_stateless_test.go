package tool_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// TestToolCard_Stateless默认false 零值即有状态（对齐上游 stateless: bool = False 默认）。
//
// Python: ToolCard.stateless (core/foundation/tool/base.py:109-115)
// 「无状态工具（模块级单例）在裸 id 下跨 agent 共享；
//
//	有状态工具由单个 agent 独占，注册时得到 agent 限定的 id」。
func TestToolCard_Stateless默认false(t *testing.T) {
	card := &tool.ToolCard{}

	assert.False(t, card.Stateless, "零值必须有状态（默认独占）")
}

// TestToolCard_Stateless可显式置true 共享单例工具可标记为无状态。
func TestToolCard_Stateless可显式置true(t *testing.T) {
	card := &tool.ToolCard{Stateless: true}

	assert.True(t, card.Stateless)
}
