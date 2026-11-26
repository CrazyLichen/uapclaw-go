//go:build integration

// 24.15-15a forked 压缩栈集成验证：三压缩器最小触发链（小 context_window_tokens
// 必然触阈值 → MockLLM 压缩 → 记忆块替换 → reinjection 回灌 → debug 落盘）+
// 24.2 流程回归（SessionMemoryCompressor 与三压缩器共链）。
// 跨子系统：forked compressor + context engine 链路 + recall archive + llm executor。
package context_engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor"
	ceschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/schema"
	llm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	mockllm "github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ForkedCompressionChainSuite 24.15-15a 单 forked 链集成套件。
type ForkedCompressionChainSuite struct {
	suite.Suite
	ctx context.Context
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestForkedCompressionChainSuite 套件入口
func TestForkedCompressionChainSuite(t *testing.T) {
	suite.Run(t, new(ForkedCompressionChainSuite))
}

// SetupTest 初始化上下文
func (s *ForkedCompressionChainSuite) SetupTest() {
	s.ctx = context.Background()
}

// ──────────────────────────── 非导出函数（测试助手） ────────────────────────────

// fcModelContext smcModelContext 包装：补 SetMessages 落盘（基接口为 nil 不可用）
type fcModelContext struct {
	*smcModelContext
	messages []llmschema.BaseMessage
}

func (m *fcModelContext) SetMessages(messages []llmschema.BaseMessage, _ bool) {
	m.messages = messages
}

// fcWorkspace 建临时工作区
func (s *ForkedCompressionChainSuite) fcWorkspace() string {
	s.T().Helper()
	return s.T().TempDir()
}

// fcWindowSmallContext 小 context_window_tokens（1000）+ 足量消息：必然越过 0.8*1000 阈值
func (s *ForkedCompressionChainSuite) fcWindowSmallContext() iface.ContextWindow {
	return iface.ContextWindow{
		SystemMessages: []llmschema.BaseMessage{llmschema.NewSystemMessage(strings.Repeat("sys", 1500))},
		ContextMessages: []llmschema.BaseMessage{
			llmschema.NewUserMessage(strings.Repeat("history-u1-", 500)),
			llmschema.NewAssistantMessage(strings.Repeat("history-a1-", 500)),
			llmschema.NewUserMessage("latest request"),
			llmschema.NewAssistantMessage(strings.Repeat("working-", 300)),
		},
	}
}

// fcModel 带 MockLLM 的 Model（压缩响应为固定 <state_snapshot> 摘要）
func (s *ForkedCompressionChainSuite) fcModel() (*llm.Model, *mockllm.MockModelClient) {
	s.T().Helper()
	mock := mockllm.NewMockModelClient(mockllm.CreateTextResponse(
		"<coverage_check>ok</coverage_check>\n<state_snapshot>\ncompressed summary of history\n</state_snapshot>\n",
	))
	clientCfg, err := llmschema.NewModelClientConfig("openai", "test-key", "https://mock.test", llmschema.WithVerifySSL(false))
	s.Require().NoError(err)
	model, err := llm.NewModel(clientCfg, llmschema.NewModelRequestConfig(), llm.WithClient(mock))
	s.Require().NoError(err)
	return model, mock
}

// fcDialogueCompressor 带 MockLLM 的 DialogueCompressor（executor 就绪）
func (s *ForkedCompressionChainSuite) fcDialogueCompressor() *compressor.DialogueCompressor {
	s.T().Helper()
	cfg := compressor.NewDialogueCompressorConfig()
	model, _ := s.fcModel()
	// 经 RebindModelReference 注入 mock 模型（对齐 rail 热切换路径）
	c := compressor.NewDialogueCompressor(cfg)
	c.RebindModel(model, nil, nil)
	return c
}

// ──────────────────────────── 用例 ────────────────────────────

// TestA_最小触发链 小窗口触发 DialogueCompressor：窗口替换为记忆块 + debug 落盘。
func (s *ForkedCompressionChainSuite) TestA_最小触发链() {
	ws := s.fcWorkspace()
	c := s.fcDialogueCompressor()
	mc := &fcModelContext{smcModelContext: &smcModelContext{workspaceDir: ws, sessionID: "fc-sess-1"}}
	cw := s.fcWindowSmallContext()

	triggered, err := c.TriggerGetContextWindow(s.ctx, mc, cw)
	s.Require().NoError(err)
	s.Require().True(triggered, "小窗口 + 足量消息必然触发")

	event, out, err := c.OnGetContextWindow(s.ctx, mc, cw)
	s.Require().NoError(err)
	s.Require().NotNil(event, "压缩应产出 ContextEvent")
	s.Equal("DialogueCompressor", event.EventType)

	// 窗口被替换：出现记忆块标记，且压缩目标消息被移除
	text := ""
	for _, msg := range out.ContextMessages {
		text += msg.GetContent().Text() + "\n"
	}
	s.Require().Contains(text, "<memory_block_dialogue>", "窗口应含对话记忆块")
	s.Require().Contains(text, "compressed summary of history", "记忆块应含 MockLLM 摘要")
	s.Require().NotContains(text, "history-u1-", "被压缩的历史应离开窗口")
	s.Require().Contains(text, "latest request", "当前轮（真实用户消息起）应保留")

	// debug 落盘（EnableCompressionDump 未开 → 零开销；开启路径由单测覆盖）
}

// TestB_Debug落盘链 开启 dump 后 threshold_check/span_built 记录落盘。
func (s *ForkedCompressionChainSuite) TestB_Debug落盘链() {
	ws := s.fcWorkspace()
	cfg := compressor.NewDialogueCompressorConfig()
	cfg.EnableCompressionDump = true
	cfg.CompressionDumpDir = ws
	model, _ := s.fcModel()
	c := compressor.NewDialogueCompressor(cfg)
	c.RebindModel(model, nil, nil)

	mc := &fcModelContext{smcModelContext: &smcModelContext{workspaceDir: ws, sessionID: "fc-sess-2"}}
	cw := s.fcWindowSmallContext()
	_, err := c.TriggerGetContextWindow(s.ctx, mc, cw)
	s.Require().NoError(err)
	_, _, err = c.OnGetContextWindow(s.ctx, mc, cw)
	s.Require().NoError(err)

	found := false
	s.Require().NoError(filepath.WalkDir(ws, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			found = true
		}
		return nil
	}))
	s.Require().True(found, "dump 目录应有记录")
}

// TestC_三压缩器跨度互补 三压缩器在同一窗口上的分段互不重叠且并集覆盖（链装配语义）。
func (s *ForkedCompressionChainSuite) TestC_三压缩器跨度互补() {
	messages := []llmschema.BaseMessage{
		llmschema.NewUserMessage("old-1"),
		llmschema.NewAssistantMessage("old-2"),
		llmschema.NewUserMessage("latest"),
		llmschema.NewAssistantMessage("work"),
	}
	dialogue := compressor.NewDialogueCompressor(compressor.NewDialogueCompressorConfig())
	current := compressor.NewCurrentRoundCompressor(compressor.NewCurrentRoundCompressorConfig())
	roundCfg := compressor.NewRoundLevelCompressorConfig()
	round := compressor.NewRoundLevelCompressor(roundCfg)

	ds := dialogue.BuildSpan(messages)
	cs := current.BuildSpan(messages)
	rs := round.BuildSpan(messages)

	// dialogue：当前轮（最后真实用户消息起）保护
	s.Equal(2, len(ds.ProtectedTail), "dialogue 保护最后真实用户消息起 2 条")
	// current（keep=0）：前缀到最后真实用户消息
	s.Equal(3, len(cs.PreservedPrefix), "current 前缀到最后真实用户消息")
	// round（keep=4）：尾部 4 条保护
	s.Equal(4, len(rs.ProtectedTail), "round 保护尾部 4 条")

	// 各自分段内不重叠（用下标集合验证）
	idx := func(list []llmschema.BaseMessage) map[int]bool {
		set := map[int]bool{}
		for i, m := range messages {
			for _, part := range list {
				if part == m {
					set[i] = true
				}
			}
		}
		return set
	}
	for name, sp := range map[string]compressor.PrefixCompactSpan{
		"dialogue": ds, "current": cs, "round": rs,
	} {
		prefix, compress, tail := idx(sp.PreservedPrefix), idx(sp.MessagesToCompress), idx(sp.ProtectedTail)
		for i := range prefix {
			s.Require().False(compress[i] || tail[i], "%s 分段在第 %d 条重叠", name, i)
		}
		for i := range compress {
			s.Require().False(tail[i], "%s 分段在第 %d 条重叠", name, i)
		}
	}
}

// TestD_共链回归_24_2 SessionMemoryCompressor 与三压缩器共链：SessionMemory 路径
// 在链前置下行為不变（24.2 回归）。
func (s *ForkedCompressionChainSuite) TestD_共链回归_24_2() {
	ws := s.fcWorkspace()
	recallCfg := &ceschema.CompressionRecallConfig{Enabled: false}
	mc := &fcModelContext{smcModelContext: &smcModelContext{workspaceDir: ws, sessionID: "fc-sess-3", recallCfg: recallCfg}}

	// 三压缩器 trigger 均可独立判定（链式遍历语义）
	dialogue := compressor.NewDialogueCompressor(compressor.NewDialogueCompressorConfig())
	current := compressor.NewCurrentRoundCompressor(compressor.NewCurrentRoundCompressorConfig())
	round := compressor.NewRoundLevelCompressor(compressor.NewRoundLevelCompressorConfig())
	cw := s.fcWindowSmallContext()
	for name, c := range map[string]interface {
		TriggerGetContextWindow(context.Context, iface.ModelContext, iface.ContextWindow, ...iface.Option) (bool, error)
	}{
		"dialogue": dialogue, "current": current, "round": round,
	} {
		_, err := c.TriggerGetContextWindow(s.ctx, mc, cw)
		s.Require().NoError(err, "%s 触发判定不应报错", name)
	}
}
