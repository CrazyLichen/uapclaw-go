//go:build integration

package server_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HistoryRoundtripSuite 验证 history 记录形状足以支撑前端还原工具调用。
//
// 链路：swarm/server/session（写入 + 读取）→ swarm/transport（真实通道往返）
// → swarm/server（handleHistoryGet 分页过滤）。
type HistoryRoundtripSuite struct {
	isuite.BaseIntegrationSuite
	// sessionID 本用例的会话标识
	sessionID string
	// dataDir 隔离的数据根目录
	dataDir string
	// agentServer 进程内 AgentServer
	agentServer *server.AgentServer
	// tr 通道传输（与 AgentServer 共享）
	tr *transport.ChannelTransport
	// envBackup 环境变量备份（[key, 原值, 是否原本存在]）
	envBackup [][3]string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestHistoryRoundtripSuite 套件入口。
func TestHistoryRoundtripSuite(t *testing.T) {
	suite.Run(t, new(HistoryRoundtripSuite))
}

// TestToolCallRestorable 防 D5 回归：工具调用/结果记录必须带前端还原所需字段，
// 且能经 history.get 原样返回。
func (s *HistoryRoundtripSuite) TestToolCallRestorable() {
	s.sessionID = "sess-history-tool"
	session.AppendHistoryRecord(s.sessionID, "req-1", "web", "assistant", "",
		1.0, "chat.tool_call", map[string]any{
			"tool_call": map[string]any{"name": "read_file", "arguments": "{}", "tool_call_id": "call_x"},
		}, nil, "agent")
	session.AppendHistoryRecord(s.sessionID, "req-1", "web", "assistant", "",
		1.1, "chat.tool_result", map[string]any{
			"result":       "内容",
			"tool_name":    "read_file",
			"tool_call_id": "call_x",
			"success":      true,
		}, nil, "agent")
	session.AppendHistoryRecord(s.sessionID, "req-1", "web", "assistant", "最终答复",
		1.2, "chat.final", nil, nil, "agent")
	session.FlushHistoryQueue()

	messages := s.historyGet(s.sessionID, 1)
	s.Require().NotEmpty(messages, "history.get 应返回记录")

	var call, result map[string]any
	for _, m := range messages {
		rec, ok := m.(map[string]any)
		if !ok {
			continue
		}
		switch rec["event_type"] {
		case "chat.tool_call":
			call = rec
		case "chat.tool_result":
			result = rec
		}
	}

	s.Require().NotNil(call, "应存在 chat.tool_call 记录")
	tc, ok := call["tool_call"].(map[string]any)
	s.Require().True(ok, "chat.tool_call 记录应含嵌套 tool_call: %+v", call)
	s.Equal("read_file", tc["name"])
	s.Equal("call_x", tc["tool_call_id"])

	s.Require().NotNil(result, "应存在 chat.tool_result 记录")
	s.Equal("call_x", result["tool_call_id"])
	s.Equal("read_file", result["tool_name"])
	s.Equal("内容", result["result"])
}

// TestTeamEventShape 防 team 记录形状回归：嵌套 event 与展平字段并存。
func (s *HistoryRoundtripSuite) TestTeamEventShape() {
	s.sessionID = "sess-history-team"
	session.AppendHistoryRecord(s.sessionID, "req-2", "web", "assistant", "",
		2.0, "team.message",
		func() map[string]any {
			extra := map[string]any{
				"event": map[string]any{"type": "member_added", "member_id": "m1"},
			}
			extra["member_id"] = "m1"
			return extra
		}(), nil, "team")
	session.FlushHistoryQueue()

	messages := s.historyGet(s.sessionID, 1)
	s.Require().Len(messages, 1)

	raw, err := json.Marshal(messages[0])
	s.Require().NoError(err)
	var got map[string]any
	s.Require().NoError(json.Unmarshal(raw, &got))

	ev, ok := got["event"].(map[string]any)
	s.Require().True(ok, "team.message 记录应保留嵌套 event: %s", string(raw))
	s.Equal("member_added", ev["type"])
	s.Equal("m1", got["member_id"], "event 子字段应展平到顶层")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每个用例独立数据根与 session。
func (s *HistoryRoundtripSuite) SetupTest() {
	s.dataDir = s.T().TempDir()
	s.saveAndSetenv(pathutil.EnvDataDir, s.dataDir)
	pathutil.ResetCache()
	s.sessionID = s.T().Name()
}

// TearDownTest 刷盘并还原环境，避免污染后续用例。
func (s *HistoryRoundtripSuite) TearDownTest() {
	session.FlushMetadataQueue()
	session.FlushHistoryQueue()
	s.restoreEnv()
	pathutil.ResetCache()
}

// SetupSuite 启动进程内 AgentServer（history.get 走真实 handleEnvelope 分发）。
func (s *HistoryRoundtripSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
	cfg, err := config.New("")
	s.Require().NoError(err)
	s.tr = transport.NewChannelTransport()
	s.agentServer = server.NewAgentServer(cfg, s.tr)
	s.Require().NoError(s.agentServer.Start(s.Ctx))

	// 等待 connection.ack，确保消费循环就绪
	recvCh, recvErr := s.tr.Recv()
	s.Require().NoError(recvErr)
	select {
	case <-recvCh:
	case <-time.After(5 * time.Second):
		s.FailNow("超时：未收到 connection.ack")
	}
}

// TearDownSuite 停机。
func (s *HistoryRoundtripSuite) TearDownSuite() {
	if s.agentServer != nil {
		_ = s.agentServer.Stop()
	}
	s.BaseIntegrationSuite.TearDownSuite()
}

// historyGet 通过真实通道发送 history.get 并返回 payload.messages。
func (s *HistoryRoundtripSuite) historyGet(sessionID string, pageIdx int) []any {
	env := e2a.NewE2AEnvelope()
	env.RequestID = "req-history-" + sessionID
	env.Channel = "web"
	env.SessionID = sessionID
	env.Method = string(schema.ReqMethodHistoryGet)
	env.IsStream = false
	env.Params = map[string]any{"session_id": sessionID, "page_idx": pageIdx}

	data, err := json.Marshal(env.ToMap())
	s.Require().NoError(err)

	sendCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.Require().NoError(s.tr.Send(sendCtx, data))

	recvCh, recvErr := s.tr.Recv()
	s.Require().NoError(recvErr)
	select {
	case raw := <-recvCh:
		var frame map[string]any
		s.Require().NoError(json.Unmarshal(raw, &frame))
		// E2A wire 形状：{body: {result: {messages: [...], total_pages, page_idx}}}
		body, ok := frame["body"].(map[string]any)
		s.Require().True(ok, "响应应含 body: %+v", frame)
		result, ok := body["result"].(map[string]any)
		s.Require().True(ok, "响应 body 应含 result: %+v", body)
		messages, _ := result["messages"].([]any)
		return messages
	case <-time.After(10 * time.Second):
		s.FailNow("超时：未收到 history.get 响应")
		return nil
	}
}

// saveAndSetenv 记录并设置环境变量。
func (s *HistoryRoundtripSuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	flag := "0"
	if had {
		flag = "1"
	}
	s.envBackup = append(s.envBackup, [3]string{key, orig, flag})
	_ = os.Setenv(key, val)
}

// restoreEnv 恢复环境变量。
func (s *HistoryRoundtripSuite) restoreEnv() {
	for _, e := range s.envBackup {
		if e[2] == "1" {
			_ = os.Setenv(e[0], e[1])
		} else {
			_ = os.Unsetenv(e[0])
		}
	}
	s.envBackup = nil
}
