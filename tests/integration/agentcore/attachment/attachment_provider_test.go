//go:build integration

package attachment

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
)

// ProviderSuite 覆盖 IT-4 投影矩阵、IT-6 隔离、IT-7 跨轮插入。
type ProviderSuite struct {
	suite.Suite
}

// TestProviderSuite 运行 ProviderSuite。
func TestProviderSuite(t *testing.T) { suite.Run(t, new(ProviderSuite)) }

// TestIT4_投影矩阵且历史不变 验证百炼换 System、Anthropic 保持 User，历史始终 User。
func (s *ProviderSuite) TestIT4_投影矩阵且历史不变() {
	m := prompts.NewManager()
	mc := newContext(s.T(), "s1")
	_, _ = m.AddSection("s1", "mode", "A", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(m.Sync(context.Background(), mc))

	w, err := mc.GetContextWindow(
		context.Background(),
		[]llmschema.BaseMessage{llmschema.NewSystemMessage("sys")},
		nil, 0, 0,
		ceiface.WithWindowMutators(prompts.BuildWindowMutator("s1", true)),
	)
	s.Require().NoError(err)
	s.Require().NotEmpty(w.ContextMessages)
	s.Require().IsType(&llmschema.SystemMessage{}, w.ContextMessages[0], "百炼路由换 System")

	hist, _ := mc.GetMessages(0, true)
	s.Require().IsType(&llmschema.UserMessage{}, hist[0], "历史不得被改写")

	w2, err := mc.GetContextWindow(
		context.Background(),
		[]llmschema.BaseMessage{llmschema.NewSystemMessage("sys")},
		nil, 0, 0,
		ceiface.WithWindowMutators(prompts.BuildWindowMutator("s1", false)),
	)
	s.Require().NoError(err)
	s.Require().IsType(&llmschema.UserMessage{}, w2.ContextMessages[0], "Anthropic 保持 User")
}

// TestIT6_多会话隔离 验证两会话并发写入互不串味。
func (s *ProviderSuite) TestIT6_多会话隔离() {
	m := prompts.NewManager()
	c1 := newContext(s.T(), "s1")
	c2 := newContext(s.T(), "s2")

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = m.AddSection("s1", "a", "AAA", prompts.AttachmentRuntime, "r") }()
	go func() { defer wg.Done(); _, _ = m.AddSection("s2", "b", "BBB", prompts.AttachmentRuntime, "r") }()
	wg.Wait()

	s.Require().NoError(m.Sync(context.Background(), c1))
	s.Require().NoError(m.Sync(context.Background(), c2))

	m1, _ := c1.GetMessages(0, true)
	m2, _ := c2.GetMessages(0, true)
	s.Require().Len(m1, 1)
	s.Require().Len(m2, 1)
	s.Require().Contains(m1[0].GetContent().Text(), "AAA")
	s.Require().NotContains(m1[0].GetContent().Text(), "BBB")
	s.Require().Contains(m2[0].GetContent().Text(), "BBB")
}

// TestIT7_跨轮插入增量 锁定消息顺序：[snapshot][user1][assistant][delta][user2]。
func (s *ProviderSuite) TestIT7_跨轮插入增量() {
	m := prompts.NewManager()
	mc := newContext(s.T(), "s1")

	_, _ = m.AddSection("s1", "mode", "A", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(m.Sync(context.Background(), mc))
	_, err := mc.AddMessages(context.Background(), []llmschema.BaseMessage{
		llmschema.NewUserMessage("第1轮"),
		llmschema.NewDefaultMessage(llmschema.RoleTypeAssistant, "回答1"),
	})
	s.Require().NoError(err)

	_, _ = m.AddSection("s1", "mode", "B", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(m.Sync(context.Background(), mc))
	_, err = mc.AddMessages(context.Background(), []llmschema.BaseMessage{llmschema.NewUserMessage("第2轮")})
	s.Require().NoError(err)

	items, _ := mc.GetMessages(0, true)
	s.Require().Len(items, 5)
	s.Require().Equal(true, items[3].GetMetadata()[prompts.MetadataKeyAttachmentHistory], "第 4 条应为 delta 附件消息")
	s.Require().Equal(prompts.ModeDelta, items[3].GetMetadata()[prompts.MetadataKeyMode])
	s.Require().Equal("第2轮", items[4].GetContent().Text())

	// 模型实际可见窗口：delta 必须已进入本轮窗口
	w, err := mc.GetContextWindow(context.Background(), nil, nil, 0, 0)
	s.Require().NoError(err)
	s.Require().Contains(w.GetMessages()[len(w.GetMessages())-2].GetContent().Text(), "以下动态上下文已经变化")
}
