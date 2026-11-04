//go:build integration

package attachment

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
)

// FlowSuite 覆盖 IT-1 全链路与 IT-2 顺序契约。
type FlowSuite struct {
	suite.Suite
}

// TestFlowSuite 运行 FlowSuite。
func TestFlowSuite(t *testing.T) { suite.Run(t, new(FlowSuite)) }

// TestIT1_SnapshotDeltaRemoved 验证 snapshot → 无变化 → delta → removed。
//
// Python: sync_to_context 三种投递形态。
func (s *FlowSuite) TestIT1_SnapshotDeltaRemoved() {
	m := prompts.NewManager()
	mc := newContext(s.T(), "s1")

	_, err := m.AddSection("s1", "mode_instructions", "A", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(err)
	s.Require().NoError(m.Sync(context.Background(), mc))
	items, err := mc.GetMessages(0, true)
	s.Require().NoError(err)
	s.Require().Len(items, 1)
	s.Require().Equal(prompts.ModeSnapshot, items[0].GetMetadata()[prompts.MetadataKeyMode])

	s.Require().NoError(m.Sync(context.Background(), mc))
	items, _ = mc.GetMessages(0, true)
	s.Require().Len(items, 1, "内容未变不应新增")

	_, err = m.AddSection("s1", "mode_instructions", "B", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(err)
	s.Require().NoError(m.Sync(context.Background(), mc))
	items, _ = mc.GetMessages(0, true)
	s.Require().Len(items, 2)
	s.Require().Equal(prompts.ModeDelta, items[1].GetMetadata()[prompts.MetadataKeyMode])
	s.Require().Contains(items[1].GetContent().Text(), "B")

	_, err = m.ClearSection("s1", "mode_instructions")
	s.Require().NoError(err)
	s.Require().NoError(m.Sync(context.Background(), mc))
	items, _ = mc.GetMessages(0, true)
	s.Require().Len(items, 3)
	s.Require().Contains(items[2].GetContent().Text(), "- `mode_instructions`")
}

// TestIT2_顺序契约与幂等 验证附件消息恒在用户消息之前，且同轮双 Sync 不重复。
func (s *FlowSuite) TestIT2_顺序契约与幂等() {
	m := prompts.NewManager()
	mc := newContext(s.T(), "s1")

	_, _ = m.AddSection("s1", "mode", "A", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(m.Sync(context.Background(), mc))
	_, err := mc.AddMessages(context.Background(), []llmschema.BaseMessage{llmschema.NewUserMessage("真实用户")})
	s.Require().NoError(err)

	items, _ := mc.GetMessages(0, true)
	s.Require().Len(items, 2)
	s.Require().Equal(true, items[0].GetMetadata()[prompts.MetadataKeyAttachmentHistory])
	s.Require().Equal("真实用户", items[1].GetContent().Text())

	s.Require().NoError(m.Sync(context.Background(), mc))
	items, _ = mc.GetMessages(0, true)
	s.Require().Len(items, 2, "同轮重复 sync 不得产生第三条")
}
