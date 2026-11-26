//go:build integration

package context_engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine"
	cecontext "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/context"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor"
	ceschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionMemoryCompressorSuite 覆盖 24.2 异步会话记忆读侧半区的端到端链路：
// rail 装配形态（manager + Compressor 同配置）→ 阈值触发 → 记忆块物化替换 →
// 归档落盘 → 取消安全（Shutdown/单飞）。
type SessionMemoryCompressorSuite struct {
	suite.Suite
	// ctx 测试上下文
	ctx context.Context
}

// smcModelContext 真实 SessionModelContext 的薄包装（注入 recall 配置与工作区）
type smcModelContext struct {
	iface.ModelContext
	workspaceDir string
	sessionID    string
	sess         sessioninterfaces.SessionFacade
	recallCfg    *ceschema.CompressionRecallConfig
}

func (m *smcModelContext) GetSessionRef() sessioninterfaces.SessionFacade { return m.sess }

func (m *smcModelContext) SessionID() string                { return m.sessionID }
func (m *smcModelContext) ContextID() string                { return m.sessionID + "-ctx" }
func (m *smcModelContext) TokenCounter() token.TokenCounter { return nil }

// ContextWindowProvider 能力面：小窗口让 char 兜底计数越过阈值（与单测口径一致）
func (m *smcModelContext) ModelName() string                        { return "" }
func (m *smcModelContext) ContextWindowTokens() int                 { return 1000 }
func (m *smcModelContext) ModelContextWindowTokens() map[string]int { return map[string]int{} }

func (m *smcModelContext) WorkspaceDir() string { return m.workspaceDir }
func (m *smcModelContext) CompressionRecallConfig() *ceschema.CompressionRecallConfig {
	return m.recallCfg
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSessionMemoryCompressorSuite 套件入口
func TestSessionMemoryCompressorSuite(t *testing.T) {
	suite.Run(t, new(SessionMemoryCompressorSuite))
}

// SetupTest 初始化上下文
func (s *SessionMemoryCompressorSuite) SetupTest() {
	s.ctx = context.Background()
}

// ──────────────────────────── 非导出函数（测试助手） ────────────────────────────

// buildSMCCompressor 构造 enabled Compressor + 小窗口配置
func (s *SessionMemoryCompressorSuite) buildSMCCompressor(recallEnabled bool) (*compressor.SessionMemoryCompressor, *cecontext.SessionMemoryManager) {
	memCfg := cecontext.NewSessionMemoryConfig()
	cfg := compressor.NewSessionMemoryCompressorConfig()
	cfg.Enabled = true
	cfg.Memory = &memCfg
	comp := compressor.NewSessionMemoryCompressor(cfg)
	mgr := cecontext.NewSessionMemoryManager(memCfg)
	return comp, mgr
}

// bigWindow 构造足以越过 800 token 阈值窗口的上下文
func (s *SessionMemoryCompressorSuite) bigWindow() iface.ContextWindow {
	return iface.ContextWindow{
		SystemMessages: []llm_schema.BaseMessage{llm_schema.NewSystemMessage(strings.Repeat("sys", 1000))},
		ContextMessages: []llm_schema.BaseMessage{
			llm_schema.NewUserMessage(strings.Repeat("ctx", 1000)),
			llm_schema.NewAssistantMessage(strings.Repeat("out", 1000)),
		},
	}
}

// ──────────────────────────── 用例 ────────────────────────────

// IT-1 物化链：rail 装配形态（同配置 manager + Compressor）→ 阈值触发 →
// 记忆块替换锚点前消息 → recall 启用时归档落盘 + 标记 id 一致（看磁盘）。
func (s *SessionMemoryCompressorSuite) TestIT1_物化链与归档落盘() {
	ws := s.T().TempDir()
	recallCfg := ceschema.NewCompressionRecallConfig()
	recallCfg.Enabled = true
	comp, _ := s.buildSMCCompressor(true)

	// 预置笔记文件（workspace 约定路径）
	notesPath := filepath.Join(ws, "context", "sess-it1_context", "session_memory", "session_context.md")
	s.Require().NoError(os.MkdirAll(filepath.Dir(notesPath), 0o755))
	s.Require().NoError(os.WriteFile(notesPath, []byte("已完成任务笔记：24.2 物化链验证"), 0o644))

	// 真实会话 + 锚点
	sess := newMockSessionFacade("sess-it1")
	cw := s.bigWindow()
	// 锚点落在首条 user 消息 → 锚点后保留 assistant（Python preserved 语义）
	if user, ok := cw.ContextMessages[0].(llm_schema.BaseMessage); ok {
		user.SetMetadata(map[string]any{"context_message_id": "anchor-1"})
	}
	sess.UpdateState(map[string]any{
		"__session_memory__": map[string]any{"notes_upto_message_id": "anchor-1"},
	})

	base, err0 := s.newRealModelContext(sess)
	s.Require().NoError(err0)
	mc := &smcModelContext{ModelContext: base, workspaceDir: ws, recallCfg: recallCfg, sessionID: "sess-it1", sess: sess}

	triggered, err := comp.TriggerGetContextWindow(s.ctx, mc, cw)
	s.Require().NoError(err)
	s.Require().True(triggered, "达阈值+有笔记+锚点应触发")

	event, newCw, err := comp.OnGetContextWindow(s.ctx, mc, cw)
	s.Require().NoError(err)
	s.Require().NotNil(event)
	s.Require().Equal("SessionMemoryCompressor", event.EventType)

	content := newCw.ContextMessages[0].GetContent().String()
	s.Require().Contains(content, "<memory_block_session>")
	s.Require().Contains(content, "已完成任务笔记：24.2 物化链验证")
	s.Require().Contains(content, "notes_upto_message_id=anchor-1")
	s.Require().Contains(content, "[[COMPRESSION_RECALL: id=")
	s.Require().NotContains(newCw.ContextMessages[1].GetContent().String(), strings.Repeat("ctx", 1000), "锚点前消息应被替换")

	// 归档真实落盘（看磁盘不看模型输出）+ 标记 id 一致
	hits, _ := filepath.Glob(filepath.Join(ws, "context", "*_context", "compression_recall"))
	s.Require().NotEmpty(hits, "compression_recall 根应存在")
	entries, err := os.ReadDir(hits[0])
	s.Require().NoError(err)
	s.Require().NotEmpty(entries, "归档目录应非空")
	archiveID := strings.SplitN(entries[0].Name(), "_", 2)[0]
	s.Require().Contains(content, "id="+archiveID, "标记 id 应与归档 memory_id 一致")
	manifest, err := os.ReadFile(filepath.Join(hits[0], entries[0].Name(), "manifest.json"))
	s.Require().NoError(err)
	s.Require().Contains(string(manifest), `"processor_type": "SessionMemoryCompressor"`)
}

// blockingAgentEditAgent 阻塞式 fake agent（agent_edit 执行体替身）。
// Invoke 在 release 前阻塞，保证后台任务生命周期可确定性观测。
type blockingAgentEditAgent struct {
	release    chan struct{}
	invoked    chan struct{}
	canceled   bool
	canceledCh chan struct{}
}

func (a *blockingAgentEditAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...cecontext.SessionMemoryAgentOption) (map[string]any, error) {
	close(a.invoked)
	select {
	case <-a.release:
		return map[string]any{}, nil
	case <-ctx.Done():
		a.canceled = true
		close(a.canceledCh)
		return nil, ctx.Err()
	}
}

func (a *blockingAgentEditAgent) SetInheritedSystemPrompt(prompt string) {}

// newBlockingAgentManager 构造 agent_edit 模式 + 阻塞 agent 的 manager
func (s *SessionMemoryCompressorSuite) newBlockingAgentManager() (*cecontext.SessionMemoryManager, *blockingAgentEditAgent) {
	memCfg := cecontext.NewSessionMemoryConfig() // 默认 update_mode=agent_edit
	memCfg.TriggerTokens = 100
	memCfg.Model = &llm_schema.ModelRequestConfig{}
	memCfg.ModelClient = &llm_schema.ModelClientConfig{}
	mgr := cecontext.NewSessionMemoryManager(memCfg)
	agent := &blockingAgentEditAgent{release: make(chan struct{}, 1), invoked: make(chan struct{}), canceledCh: make(chan struct{})}
	mgr.SetAgentEditAgent(agent)
	return mgr, agent
}

// newRealModelContext 经真实 ContextEngine 创建 SessionModelContext
func (s *SessionMemoryCompressorSuite) newRealModelContext(sess sessioninterfaces.SessionFacade) (iface.ModelContext, error) {
	config := ceschema.NewContextEngineConfig()
	engine := context_engine.NewContextEngine(config)
	return engine.CreateContext(s.ctx, "default_context", sess)
}

// bigMessages 构造越过 TriggerTokens 默认 10000 的消息集
func bigMessages() []llm_schema.BaseMessage {
	return []llm_schema.BaseMessage{
		llm_schema.NewUserMessage(strings.Repeat("x", 12000)),
		llm_schema.NewAssistantMessage(strings.Repeat("y", 12000)),
	}
}

// IT-2 取消安全：后台任务执行中 Shutdown → 任务被取消、active 笔记不被脏 pending 覆盖。
func (s *SessionMemoryCompressorSuite) TestIT2_Shutdown取消后台任务() {
	ws := s.T().TempDir()
	mgr, agent := s.newBlockingAgentManager()

	sess := newMockSessionFacade("sess-it2")
	sess.UpdateState(map[string]any{
		"__session_memory__": map[string]any{"notes_upto_message_id": ""},
	})
	activePath := cecontext.GetSessionMemoryPath(ws, "sess-it2")
	s.Require().NoError(os.MkdirAll(filepath.Dir(activePath), 0o755))
	s.Require().NoError(os.WriteFile(activePath, []byte("原始笔记"), 0o644))

	base, err := s.newRealModelContext(sess)
	s.Require().NoError(err)
	mc := &smcModelContext{ModelContext: base, workspaceDir: ws, sessionID: "sess-it2", sess: sess}
	_, err = mc.AddMessages(s.ctx, bigMessages())
	s.Require().NoError(err)

	mgr.MaybeScheduleUpdate(s.ctx, sess, mc, ws)
	select {
	case <-agent.invoked:
	case <-time.After(3 * time.Second):
		s.Fail("后台任务未启动")
	}
	s.Require().Equal(1, mgr.RunningTaskCount())

	// Shutdown → 全表取消（Invoke 经 bgCtx 感知取消，无需 release）
	mgr.Shutdown()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.RunningTaskCount() == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	s.Require().Equal(0, mgr.RunningTaskCount(), "Shutdown 后不应有运行中的任务")
	// 取消感知：等 fake agent 的 canceledCh（bgCtx.Done 传播到 Invoke）
	select {
	case <-agent.canceledCh:
	case <-time.After(3 * time.Second):
		s.Fail("后台任务未感知到取消")
	}
	s.Require().True(agent.canceled, "后台任务应感知到取消")

	// active 笔记不被脏 pending 覆盖
	data, err := os.ReadFile(activePath)
	s.Require().NoError(err)
	s.Require().Equal("原始笔记", string(data), "取消后 active 笔记不得被脏 pending 覆盖")
}

// IT-3 单飞：同 session 连续两次调度 → 第二次跳过（任务表仍 1 条）。
func (s *SessionMemoryCompressorSuite) TestIT3_同会话调度单飞() {
	ws := s.T().TempDir()
	mgr, agent := s.newBlockingAgentManager()
	defer func() {
		agent.release <- struct{}{}
		mgr.Shutdown()
	}()

	sess := newMockSessionFacade("sess-it3")
	sess.UpdateState(map[string]any{
		"__session_memory__": map[string]any{"notes_upto_message_id": ""},
	})
	base, err := s.newRealModelContext(sess)
	s.Require().NoError(err)
	mc := &smcModelContext{ModelContext: base, workspaceDir: ws, sessionID: "sess-it3", sess: sess}
	_, err = mc.AddMessages(s.ctx, bigMessages())
	s.Require().NoError(err)

	mgr.MaybeScheduleUpdate(s.ctx, sess, mc, ws)
	select {
	case <-agent.invoked:
	case <-time.After(3 * time.Second):
		s.Fail("后台任务未启动")
	}
	s.Require().Equal(1, mgr.RunningTaskCount(), "首次调度应产生 1 个后台任务")
	mgr.MaybeScheduleUpdate(s.ctx, sess, mc, ws)
	s.Require().Equal(1, mgr.RunningTaskCount(), "同会话第二次调度应跳过（单飞）")
}
