//go:build integration

package context_engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RestartReloadSuite 验证「进程/服务重启后从 checkpointer 恢复上下文」。
//
// 链路（跨 3 个 internal 二级子系统）：
//
//	context_engine（CreateContext/SaveContexts）
//	→ session（PreRun/PostRun → Commit）
//	→ session/checkpointer（CreateCheckpointer → PersistenceCheckpointer.Save/Recover）
//	→ foundation/store/kv（DbBasedKVStore over gorm+sqlite，落盘 checkpoint.db）
//
// 装配与生产一致：swarm/server 启动时的 EnsurePersistentCheckpointer 就是
// CreateCheckpointer{type: persistence, conf:{db_type: sqlite, db_path: ...}}。
//
// 「重启」用两个独立的检查点器实例 + 两个独立 Session 模拟：内存中的 ContextEngine
// 上下文池、Session 全局状态全部丢弃，只剩磁盘上 checkpoint.db 里的状态。
type RestartReloadSuite struct {
	suite.Suite
	// ctx 测试上下文
	ctx context.Context
	// dbPath 检查点库路径（不带 .db，由 provider 补齐），两次「启动」共享同一文件
	dbPath string
	// card Agent 身份，决定 checkpointer 的实体 ID（缺省会退化为 "Na"）
	card *agentschema.AgentCard
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestRestartReloadSuite 套件入口。
func TestRestartReloadSuite(t *testing.T) {
	suite.Run(t, new(RestartReloadSuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每个用例使用独立的检查点库文件。
func (s *RestartReloadSuite) SetupTest() {
	s.ctx = context.Background()
	s.dbPath = filepath.Join(s.T().TempDir(), "checkpoint")
	s.card = agentschema.NewAgentCard(agentschema.WithAgentID("agent-restart"))
}

// newCheckpointer 按生产装配创建持久化检查点器（SQLite）。
//
// 对齐 swarm/server 的 adapter.EnsurePersistentCheckpointer：
// type=persistence + db_type=sqlite + db_path={CheckpointDir}/checkpoint。
func (s *RestartReloadSuite) newCheckpointer() sessioninterfaces.Checkpointer {
	cp, err := checkpointer.CreateCheckpointer(s.ctx, checkpointer.CheckpointerFactoryConfig{
		Type: "persistence",
		Conf: map[string]any{
			"db_type": "sqlite",
			"db_path": s.dbPath,
		},
	})
	s.Require().NoError(err, "创建持久化检查点器失败")
	return cp
}

// newSession 基于给定检查点器创建会话，等价于重启后重新装配 Session。
func (s *RestartReloadSuite) newSession(sessionID string, cp sessioninterfaces.Checkpointer) *session.Session {
	return session.NewSession(
		session.WithSessionID(sessionID),
		session.WithCard(s.card),
		session.WithCheckpointer(cp),
	)
}

// requireCheckpointFile 断言 checkpoint.db 已落盘且非空（持久化的直接证据）。
func (s *RestartReloadSuite) requireCheckpointFile() {
	info, err := os.Stat(s.dbPath + ".db")
	s.Require().NoError(err, "检查点库文件应已落盘: %s.db", s.dbPath)
	s.Greater(info.Size(), int64(0), "检查点库文件不应为空")
}

// originMessages 构造落盘前的消息：用户问题 → 带工具调用的回答 → 工具结果。
// 覆盖纯文本与工具链两种形状，避免「只恢复纯文本」的假绿。
func originMessages() []llm_schema.BaseMessage {
	assistant := llm_schema.NewAssistantMessage("调用工具",
		llm_schema.WithToolCalls([]*llm_schema.ToolCall{
			llm_schema.NewToolCall("call-restart-1", "read_file", `{"file_path":"a.txt"}`),
		}))
	return []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("重启前的问题"),
		assistant,
		llm_schema.NewToolMessage("call-restart-1", "重启前的工具结果"),
	}
}

// messageSignatures 提取消息的「角色|正文|工具调用 ID」指纹，用于跨重启比对。
func messageSignatures(messages []llm_schema.BaseMessage) []string {
	signatures := make([]string, 0, len(messages))
	for _, msg := range messages {
		signature := msg.GetRole().String() + "|" + msg.GetContent().Text()
		if toolMsg, ok := msg.(*llm_schema.ToolMessage); ok {
			signature += "|" + toolMsg.ToolCallID
		}
		signatures = append(signatures, signature)
	}
	return signatures
}

// TestRestart_上下文从checkpointer恢复 落盘 → 新检查点器/新会话 → 新引擎读取，
// 断言消息条数、角色、正文、工具调用 ID 全部还原。
func (s *RestartReloadSuite) TestRestart_上下文从checkpointer恢复() {
	const sessionID = "restart-session"

	// ── 第一次启动：写消息 → SaveContexts → PostRun 落盘 ──
	ce1 := context_engine.NewContextEngine(ceschema.NewContextEngineConfig())
	sess1 := s.newSession(sessionID, s.newCheckpointer())
	s.Require().NoError(sess1.PreRun(s.ctx))

	mc1, err := ce1.CreateContext(s.ctx, "default_context_id", sess1)
	s.Require().NoError(err)
	_, err = mc1.AddMessages(s.ctx, originMessages())
	s.Require().NoError(err)
	s.Require().Equal(3, mc1.Len(), "落盘前应有 3 条消息")

	states, err := ce1.SaveContexts(s.ctx, sess1, []string{"default_context_id"})
	s.Require().NoError(err)
	s.Require().Contains(states, "default_context_id", "SaveContexts 应返回上下文状态")
	// PostRun → Commit → PostAgentExecute：状态写入 SQLite
	s.Require().NoError(sess1.PostRun(s.ctx))
	s.requireCheckpointFile()

	// ── 第二次启动：全新检查点器 + 全新会话（内存态全部丢弃）──
	sess2 := s.newSession(sessionID, s.newCheckpointer())
	// 恢复前：新会话内存态必须是空的，否则下面的断言无法区分「检查点恢复」与「同进程残留」
	rawBefore, err := sess2.GetState(state.StringKey("context"))
	s.Require().NoError(err)
	s.Nil(rawBefore, "PreRun 之前不应有上下文状态")

	s.Require().NoError(sess2.PreRun(s.ctx), "PreRun 触发检查点恢复失败")
	rawAfter, err := sess2.GetState(state.StringKey("context"))
	s.Require().NoError(err)
	s.Require().NotNil(rawAfter, "PreRun 后应从检查点恢复出 context 状态")

	// 新引擎（上下文池为空）按同一 session 重建上下文，触发 _load_state_from_session
	ce2 := context_engine.NewContextEngine(ceschema.NewContextEngineConfig())
	mc2, err := ce2.CreateContext(s.ctx, "default_context_id", sess2)
	s.Require().NoError(err)

	s.Equal(3, mc2.Len(), "重启后上下文消息数应与落盘前一致")
	restored, err := mc2.GetMessages(0, true)
	s.Require().NoError(err)
	s.Equal(messageSignatures(originMessages()), messageSignatures(restored),
		"重启后消息角色/正文应逐条一致")

	// 工具链还原：tool_calls 与 tool_call_id 必须成对保留，否则模型侧无法配对
	assistantMsg, ok := restored[1].(*llm_schema.AssistantMessage)
	s.Require().True(ok, "第 2 条应为 AssistantMessage，实际 %T", restored[1])
	s.Require().Len(assistantMsg.ToolCalls, 1, "重启后应保留 1 个工具调用")
	s.Equal("call-restart-1", assistantMsg.ToolCalls[0].ID)
	s.Equal("read_file", assistantMsg.ToolCalls[0].Name)

	toolMsg, ok := restored[2].(*llm_schema.ToolMessage)
	s.Require().True(ok, "第 3 条应为 ToolMessage，实际 %T", restored[2])
	s.Equal("call-restart-1", toolMsg.ToolCallID, "工具结果应仍关联原工具调用")
}

// TestRestart_不同会话不串味 检查点按 sessionID + agentID 分键，
// 重启后换一个 sessionID 打开不应读到别的会话上下文。
func (s *RestartReloadSuite) TestRestart_不同会话不串味() {
	ce1 := context_engine.NewContextEngine(ceschema.NewContextEngineConfig())
	sess1 := s.newSession("restart-session-a", s.newCheckpointer())
	s.Require().NoError(sess1.PreRun(s.ctx))

	mc1, err := ce1.CreateContext(s.ctx, "default_context_id", sess1)
	s.Require().NoError(err)
	_, err = mc1.AddMessages(s.ctx, originMessages())
	s.Require().NoError(err)
	_, err = ce1.SaveContexts(s.ctx, sess1, []string{"default_context_id"})
	s.Require().NoError(err)
	s.Require().NoError(sess1.PostRun(s.ctx))
	s.requireCheckpointFile()

	sess2 := s.newSession("restart-session-b", s.newCheckpointer())
	s.Require().NoError(sess2.PreRun(s.ctx))
	raw, err := sess2.GetState(state.StringKey("context"))
	s.Require().NoError(err)
	s.Nil(raw, "不同 sessionID 不应读到别的会话上下文")

	ce2 := context_engine.NewContextEngine(ceschema.NewContextEngineConfig())
	mc2, err := ce2.CreateContext(s.ctx, "default_context_id", sess2)
	s.Require().NoError(err)
	s.Equal(0, mc2.Len(), "不同 sessionID 的上下文应为空")
}
