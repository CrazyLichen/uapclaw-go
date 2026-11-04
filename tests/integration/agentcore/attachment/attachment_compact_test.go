//go:build integration

package attachment

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
)

// CompactSuite 覆盖 IT-3 基线回退与 IT-5 重启恢复。
type CompactSuite struct {
	suite.Suite
}

// TestCompactSuite 运行 CompactSuite。
func TestCompactSuite(t *testing.T) { suite.Run(t, new(CompactSuite)) }

// TestIT3_基线丢失回退快照 验证压缩把 snapshot 挤出窗口后回退全量。
//
// Python: has_history_snapshot 为 false 时 render_history_snapshot。
func (s *CompactSuite) TestIT3_基线丢失回退快照() {
	m := prompts.NewManager()
	mc := newContext(s.T(), "s1")
	_, _ = m.AddSection("s1", "mode", "A", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(m.Sync(context.Background(), mc))

	// 等价于压缩把 snapshot 挤出窗口
	mc.SetMessages(nil, true)

	_, _ = m.AddSection("s1", "mode", "B", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(m.Sync(context.Background(), mc))
	items, _ := mc.GetMessages(0, true)
	s.Require().Len(items, 1)
	s.Require().Equal(prompts.ModeSnapshot, items[0].GetMetadata()[prompts.MetadataKeyMode])
	s.Require().Contains(items[0].GetContent().Text(), "B")
}

// TestIT5_重启后不重复投递 验证新 Manager 从历史账本恢复、不重复投递。
func (s *CompactSuite) TestIT5_重启后不重复投递() {
	mc := newContext(s.T(), "s1")
	m1 := prompts.NewManager()
	_, _ = m1.AddSection("s1", "mode", "A", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(m1.Sync(context.Background(), mc))

	m2 := prompts.NewManager()
	_, _ = m2.AddSection("s1", "mode", "A", prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(m2.Sync(context.Background(), mc))

	items, _ := mc.GetMessages(0, true)
	s.Require().Len(items, 1, "重启后同内容不得重复投递")
	s.Require().Equal(prompts.ModeSnapshot, items[0].GetMetadata()[prompts.MetadataKeyMode])
}
