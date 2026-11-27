//go:build integration

package server_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionPinSuite session.pin 控制面链路：AgentServer 信封分发 →
// repositories 控制面仓库 → session 元数据磁盘写回（紧凑重编号 1..N）。
//
// 跨子系统：swarm/server（E2A 信封分发）+ swarm/server/control/repositories
// （控制面分派与 lifecycle guard）+ swarm/server/session（metadata.json 落盘）
// + swarm/runtime/session/lifecycle（guard 状态读）。
type SessionPinSuite struct {
	isuite.BaseIntegrationSuite
	// env 进程内 AgentServer 环境（E2A 信封投递 + 帧收集）
	env *testhelpers.ServerEnv
	// dataDir 本用例隔离的数据根目录
	dataDir string
}

// SetupTest 隔离数据目录与会话元数据缓存（对齐 SessionLifecycleSuite 口径）。
func (s *SessionPinSuite) SetupTest() {
	s.dataDir = s.T().TempDir()
	s.T().Setenv(pathutil.EnvDataDir, s.dataDir)
	pathutil.ResetCache()
	swarmsession.ClearAllSessionMetadataCache()
}

// TearDownTest 刷盘并还原环境。
func (s *SessionPinSuite) TearDownTest() {
	swarmsession.FlushMetadataQueue(5.0)
	swarmsession.ClearAllSessionMetadataCache()
	pathutil.ResetCache()
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSessionPinSuite 套件入口。
func TestSessionPinSuite(t *testing.T) {
	suite.Run(t, new(SessionPinSuite))
}

// SetupSuite 启动进程内 AgentServer。
func (s *SessionPinSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
	cfg, err := config.New("")
	s.Require().NoError(err)
	s.env = testhelpers.NewServerEnv(s.T(), cfg, nil)
}

// writeMeta 直接落一个会话目录（三个置顶 + 两个普通）。
func (s *SessionPinSuite) writeMeta(sessionID string, meta map[string]any) {
	s.T().Helper()
	dir := filepath.Join(s.sessionsDir(), sessionID)
	s.Require().NoError(os.MkdirAll(dir, 0o755))
	if meta == nil {
		return
	}
	data, err := json.Marshal(meta)
	s.Require().NoError(err)
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "metadata.json"), data, 0o644))
}

// sessionsDir 当前会话目录（经 workspace 解析，与 server 一致）。
func (s *SessionPinSuite) sessionsDir() string {
	return filepath.Join(s.dataDir, "agent", "sessions")
}

// readMeta 读回磁盘元数据。
func (s *SessionPinSuite) readMeta(sessionID string) map[string]any {
	data, err := os.ReadFile(filepath.Join(s.sessionsDir(), sessionID, "metadata.json"))
	s.Require().NoError(err)
	var meta map[string]any
	s.Require().NoError(json.Unmarshal(data, &meta))
	return meta
}

// pin 调用 session.pin 并断言成功。
func (s *SessionPinSuite) pin(sessionID string, pinned bool) map[string]any {
	rid := s.env.Send("session.pin", map[string]any{"session_id": sessionID, "pinned": pinned}, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().NotNil(resp, "应收到响应")
	s.Require().True(resp.OK, "session.pin 应成功: %+v", resp)
	_ = s.T()
	return resp.Payload
}

// TestPin_紧凑重编号 置顶/取消置顶后所有置顶会话重编号为 1..N。
func (s *SessionPinSuite) TestPin_紧凑重编号() {
	// 初始：s1 已置顶(order=1)，s2/s3 未置顶
	s.writeMeta("pin_s1", map[string]any{"channel_id": "web", "pinned": true, "pin_order": 1.0})
	s.writeMeta("pin_s2", map[string]any{"channel_id": "web", "pinned": false, "pin_order": 0.0})
	s.writeMeta("pin_s3", map[string]any{"channel_id": "web", "pinned": false, "pin_order": 0.0})
	s.writeMeta("pin_plain", map[string]any{"channel_id": "web", "pinned": false})

	// 置顶 s3 → compact：s3=1, s1=2
	payload := s.pin("pin_s3", true)
	s.Require().Equal(true, payload["pinned"])
	s.Require().Equal(float64(1), payload["pin_order"])
	s.Assert().Equal(float64(1), s.readMeta("pin_s3")["pin_order"])
	s.Assert().Equal(float64(2), s.readMeta("pin_s1")["pin_order"])
	s.Assert().Equal(false, s.readMeta("pin_s2")["pinned"])

	// 再置顶 s2 → s2=1, s3=2, s1=3
	payload = s.pin("pin_s2", true)
	s.Require().Equal(float64(1), payload["pin_order"])
	s.Assert().Equal(float64(1), s.readMeta("pin_s2")["pin_order"])
	s.Assert().Equal(float64(2), s.readMeta("pin_s3")["pin_order"])
	s.Assert().Equal(float64(3), s.readMeta("pin_s1")["pin_order"])

	// 取消置顶 s2 → s3=1, s1=2，s2 归零
	payload = s.pin("pin_s2", false)
	s.Require().Equal(false, payload["pinned"])
	s.Require().Equal(float64(0), payload["pin_order"])
	s.Assert().Equal(false, s.readMeta("pin_s2")["pinned"])
	s.Assert().Equal(float64(0), s.readMeta("pin_s2")["pin_order"])
	s.Assert().Equal(float64(1), s.readMeta("pin_s3")["pin_order"])
	s.Assert().Equal(float64(2), s.readMeta("pin_s1")["pin_order"])
}

// TestPin_幂等 对已处于目标状态的会话重复操作返回现值且不重排。
func (s *SessionPinSuite) TestPin_幂等() {
	s.writeMeta("pin_idem", map[string]any{"channel_id": "web", "pinned": true, "pin_order": 1.0})
	payload := s.pin("pin_idem", true)
	s.Require().Equal(true, payload["pinned"])
	s.Require().Equal(float64(1), payload["pin_order"])
	s.Assert().Equal(float64(1), s.readMeta("pin_idem")["pin_order"])
}

// TestPin_校验与未知会话 缺参/非法类型/未知会话的结构化报错。
func (s *SessionPinSuite) TestPin_校验与未知会话() {
	// 缺 session_id
	rid := s.env.Send("session.pin", map[string]any{"pinned": true}, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().NotNil(resp)
	s.Assert().False(resp.OK)
	s.Assert().Equal("BAD_REQUEST", resp.Payload["code"])

	// pinned 非布尔
	rid = s.env.Send("session.pin", map[string]any{"session_id": "pin_x", "pinned": "yes"}, "", false)
	resp = s.env.AwaitUnary(rid)
	s.Require().NotNil(resp)
	s.Assert().False(resp.OK)
	s.Assert().Equal("BAD_REQUEST", resp.Payload["code"])

	// 未知会话
	rid = s.env.Send("session.pin", map[string]any{"session_id": "pin_missing", "pinned": true}, "", false)
	resp = s.env.AwaitUnary(rid)
	s.Require().NotNil(resp)
	s.Assert().False(resp.OK)
	s.Assert().Equal("NOT_FOUND", resp.Payload["code"])
}

// TestPin_探活会话不参与重编号 heartbeat_ 会话即使被误置顶也不进入排序。
func (s *SessionPinSuite) TestPin_探活会话不参与重编号() {
	s.writeMeta("pin_a", map[string]any{"channel_id": "web", "pinned": true, "pin_order": 1.0})
	s.writeMeta("heartbeat_x", map[string]any{"channel_id": "web", "pinned": true, "pin_order": 2.0})
	s.writeMeta("pin_b", map[string]any{"channel_id": "web", "pinned": false})
	payload := s.pin("pin_b", true)
	// heartbeat_x 不参与排序：pin_b（pin_order=0）排序在最前 → 1，pin_a → 2
	s.Require().Equal(float64(1), payload["pin_order"])
	// heartbeat 会话的元数据未被重写
	s.Assert().Equal(float64(2), s.readMeta("heartbeat_x")["pin_order"])
	s.Assert().Equal(float64(2), s.readMeta("pin_a")["pin_order"])
}

// 编译期兜底：schema 常量在查找表中。
var _ = schema.ReqMethodSessionPin
