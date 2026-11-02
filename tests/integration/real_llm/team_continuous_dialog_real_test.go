//go:build integration && llm

package real_llm

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamContinuousRealSuite team 模式连续对话真 LLM 套件。
//
// 对齐 Python jiuwenswarm/tests/system_tests/test_team_continuous_dialog.py
// （Python 侧标记 skip "until team continuous dialog is stable"）：
// 首条 chat.send(mode=team) 未结束时发第二条，验证服务端不互斥。
//
//	运行方式：go test -tags "sqlite_fts5 test integration llm" \
//	  -run TestTeamContinuousRealSuite ./tests/integration/real_llm/
//
// 需要 LLM_API_KEY（缺失时 Skip）。
type TeamContinuousRealSuite struct {
	isuite.RealLLMSuite
	// wsPort Web 通道端口
	wsPort int
	// dataDir 隔离数据目录
	dataDir string
	// origWd 套件前工作目录（成员技能/记忆目录未设根路径时落 CWD）
	origWd string
	// agentServer 进程内 AgentServer
	agentServer *server.AgentServer
	// gwServer 进程内 AppGatewayServer
	gwServer *gateway.AppGatewayServer
	// envBackup 环境变量备份
	envBackup [][3]string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTeamContinuousRealSuite 套件入口。
func TestTeamContinuousRealSuite(t *testing.T) {
	suite.Run(t, new(TeamContinuousRealSuite))
}

// SetupSuite 装配真实 LLM 配置 + 进程内 serve。
func (s *TeamContinuousRealSuite) SetupSuite() {
	s.RealLLMSuite.SetupSuite()
	if s.RealClientConfig == nil {
		s.T().Skip("LLM_API_KEY 未设置，跳过真 LLM 套件")
	}

	s.dataDir = s.T().TempDir()
	wd, err := os.Getwd()
	s.Require().NoError(err)
	s.origWd = wd
	s.Require().NoError(os.Chdir(s.dataDir))

	configDir := filepath.Join(s.dataDir, "config")
	s.Require().NoError(os.MkdirAll(configDir, 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(s.dataDir, "agent", "workspace"), 0o755))

	yaml := fmt.Sprintf(`modes:
  team:
    workspace:
      enabled: false
models:
  defaults:
    - model_client_config:
        model_name: %s
        client_provider: %s
        api_key: %s
        api_base: %s
        client_id: real_llm_team
      is_default: true
`, s.RealModelConfig.ModelName, s.RealClientConfig.ClientProvider,
		s.RealClientConfig.APIKey, s.RealClientConfig.APIBase)
	s.Require().NoError(os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(yaml), 0o644))

	s.wsPort = s.pickFreePort()
	s.saveAndSetenv("UAPCLAW_DATA_DIR", s.dataDir)
	s.saveAndSetenv("UAPCLAW_GATEWAY_PORT", fmt.Sprintf("%d", s.wsPort))
	pathutil.ResetCache()

	cfg, err := config.New("")
	s.Require().NoError(err)
	_, err = cfg.Load()
	s.Require().NoError(err)

	// 三子系统构造（审计跨度来源）
	tr := transport.NewChannelTransport()
	s.agentServer = server.NewAgentServer(cfg, tr)
	agentClient := routing.NewAgentClient(tr)
	s.gwServer, err = gateway.NewAppGatewayServer(cfg, agentClient)
	s.Require().NoError(err)

	s.Require().NoError(s.agentServer.Start(s.Ctx))
	s.Require().NoError(s.gwServer.Start(s.Ctx))
}

// TearDownSuite 逆序停机。
func (s *TeamContinuousRealSuite) TearDownSuite() {
	if s.gwServer != nil {
		_ = s.gwServer.Stop()
	}
	if s.agentServer != nil {
		_ = s.agentServer.Stop()
	}
	s.restoreEnv()
	pathutil.ResetCache()
	if s.origWd != "" {
		_ = os.Chdir(s.origWd)
	}
	s.RealLLMSuite.TearDownSuite()
}

// TestTeamContinuousDialog_真实调用 完全对齐 Python 场景（真 LLM）。
//
// 本用例锁的契约是「同一 session 的首条流未结束时，第二条 chat.send 仍被受理」。
// 硬断言只有三条：首条受理、第二条确实在首条结束前发出、第二条被受理。
//
// 团队流程相关观测（runtime_ready / build_team / spawn_member / team.member /
// team.message / chat.final）只记日志：同一句"告诉我你有哪些工具"，
// 模型可能直接回答而不进入团队流程（2026-11-02 实测 rt=false 的一轮即如此），
// 团队建队链路由 TestTeamAutoBuild_自然语言需求触发建队 硬断言覆盖。
func (s *TeamContinuousRealSuite) TestTeamContinuousDialog_真实调用() {
	conn, _, err := websocket.DefaultDialer.Dial(
		fmt.Sprintf("ws://127.0.0.1:%d/ws", s.wsPort), nil)
	s.Require().NoError(err, "WS 拨号失败")
	defer conn.Close()

	sessionID := fmt.Sprintf("sess_team_real_%d", time.Now().UnixNano())
	s.Require().NoError(s.sendChatSend(conn, "req-real-1", sessionID,
		"告诉我你有哪些工具，不要立刻开始任务"))

	var (
		firstAccepted   bool
		secondAccepted  bool
		runtimeReady    bool
		memberSpawned   bool
		sawMember       bool
		sawMessage      bool
		firstFinal      bool
		secondSent      bool
		secondSentAt    time.Time
		firstAcceptedAt time.Time
		firstFinalAt    time.Time
	)

	deadline := time.Now().Add(8 * time.Minute)
	idle := 0
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			idle++
			if idle >= 6 {
				break
			}
			continue
		}
		idle = 0

		var frame map[string]any
		if json.Unmarshal(raw, &frame) != nil {
			continue
		}
		switch {
		case realFrameID(frame) == "req-real-1" && realAccepted(frame):
			if !firstAccepted {
				firstAccepted = true
				firstAcceptedAt = time.Now()
			}
		case realFrameID(frame) == "req-real-2" && realAccepted(frame):
			secondAccepted = true
		}
		switch realEventName(frame) {
		case "team.runtime_ready":
			runtimeReady = true
		case "chat.tool_result":
			// 只认建队/拉人两步的成功结果，避免任意工具（如 bash）成功就算成员已创建
			if realToolSucceeded(frame) && isTeamBuildTool(realFrameToolName(frame)) {
				memberSpawned = true
			}
		case "team.member":
			sawMember = true
		case "team.message":
			sawMessage = true
		case "chat.final":
			if !firstFinal {
				firstFinal = true
				firstFinalAt = time.Now()
			}
		}

		// 首条一被受理就发第二条：不依赖 team.runtime_ready（模型可能压根不建队），
		// 否则用例会因模型路由差异而假红。
		if !secondSent && firstAccepted {
			s.Require().NoError(s.sendChatSend(conn, "req-real-2", sessionID,
				"现在请依次调用工具：先 build_team 建队，再 spawn_member 创建一个名为 anna 的成员，"+
					"然后 send_message 让 anna 汇报她有哪些工具"))
			secondSent = true
			secondSentAt = time.Now()
		}
		// 第二条被受理后，等首条收尾或最多再等 2 分钟——模型不建队时也可能一直没有
		// chat.final，避免用例白等到 8 分钟上限。
		if secondSent && secondAccepted && (firstFinal || time.Since(secondSentAt) > 2*time.Minute) {
			break
		}
	}

	s.T().Logf("观测: firstAccepted=%v runtimeReady=%v memberSpawned=%v secondAccepted=%v firstFinal=%v sawMember=%v sawMessage=%v",
		firstAccepted, runtimeReady, memberSpawned, secondAccepted, firstFinal, sawMember, sawMessage)
	s.True(firstAccepted, "首条 chat.send 应被受理")
	s.True(secondSent, "第二条消息应在首条流未结束时发出")
	s.True(secondAccepted, "第二条 chat.send 应被受理（同 session 不互斥）")
	// 团队流程观测仅记日志：模型可能直接回答而不建队（模型路由差异），
	// 建队链路本身由 TestTeamAutoBuild_自然语言需求触发建队 硬断言。
	s.T().Logf("团队流程观测: runtimeReady=%v memberSpawned=%v sawMember=%v firstFinal=%v sawMessage=%v",
		runtimeReady, memberSpawned, sawMember, firstFinal, sawMessage)
	// 并发证据：第二条必须在首条流结束之前发出（时间差 < 首条流时长）。
	// 原断言 `secondSentAt 非零` 与 secondSent 同分支赋值，是纯推论，失败时还会
	// 把"第一条没被受理"误报成"第二条没发出"。仅在两条都观测到时断言，避免模型
	// 迟迟不收敛导致假红（收敛慢本身不是并发契约）。
	if !firstAcceptedAt.IsZero() && !secondSentAt.IsZero() && !firstFinalAt.IsZero() {
		s.Less(secondSentAt.Sub(firstAcceptedAt), firstFinalAt.Sub(firstAcceptedAt),
			"第二条消息应在首条流结束前发出（实际：首条流 %v，第二条发出于 +%v）",
			firstFinalAt.Sub(firstAcceptedAt), secondSentAt.Sub(firstAcceptedAt))
	} else {
		s.T().Logf("跳过并发时间断言：firstAcceptedAt=%v firstFinalAt=%v secondSentAt=%v",
			firstAcceptedAt, firstFinalAt, secondSentAt)
	}
}

// TestTeamAutoBuild_自然语言需求触发建队 验证"用户只说人话，模型自己建队"。
//
// 关键行为契约：用户不需要说"建个团队"。leader 的系统提示词（leader_workflow.md
// 第 2 步）要求它先 build_team 再拉成员，本用例锁住这条——任何让提示词失效的改动
// （section 未注入、rail 未挂载、模板被改坏）都会让 team.member 消失。
func (s *TeamContinuousRealSuite) TestTeamAutoBuild_自然语言需求触发建队() {
	conn, _, err := websocket.DefaultDialer.Dial(
		fmt.Sprintf("ws://127.0.0.1:%d/ws", s.wsPort), nil)
	s.Require().NoError(err, "WS 拨号失败")
	defer conn.Close()

	sessionID := fmt.Sprintf("sess_auto_%d", time.Now().UnixNano())
	// 纯自然语言需求：不提 build_team、不提 spawn_member
	s.Require().NoError(s.sendChatSend(conn, "req-auto-1", sessionID,
		"帮我做一个登录页：前端表单校验、后端接口、再加一版单元测试。这种三个方向都要有人负责的活，你安排"))

	var (
		memberCreated bool
		toolNames     []string
		sawFinal      bool
	)
	deadline := time.Now().Add(10 * time.Minute)
	idle := 0
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			idle++
			if idle >= 12 {
				break
			}
			continue
		}
		idle = 0

		var frame map[string]any
		if json.Unmarshal(raw, &frame) != nil {
			continue
		}
		payload, _ := frame["payload"].(map[string]any)
		switch realEventName(frame) {
		case "chat.tool_call":
			if tc, ok := payload["tool_call"].(map[string]any); ok {
				name, _ := tc["name"].(string)
				// 流式增量帧可能带空 name，只记录非空 name，避免断言被噪声干扰
				if name != "" {
					toolNames = append(toolNames, name)
					s.T().Logf("工具调用: %s", name)
				}
			}
		case "chat.tool_result":
			// 只有建队/拉人这两步成功才算成员已创建。此处曾用「任意 tool_result
			// success」判定，导致模型先跑一次 bash 就提前退出循环，永远等不到
			// build_team，断言变成假红。
			if realToolSucceeded(frame) && isTeamBuildTool(realToolName(payload)) {
				memberCreated = true
			}
		case "team.member":
			memberCreated = true
			s.T().Logf("收到 team.member: %v", payload["event"])
		case "chat.final":
			sawFinal = true
		}
		if memberCreated {
			break
		}
	}

	s.T().Logf("工具序列=%v memberCreated=%v sawFinal=%v", toolNames, memberCreated, sawFinal)
	s.True(memberCreated, "自然语言团队需求下，leader 应自行建队并创建成员")
	// 建队链路的第一跳必须是 build_team（提示词 leader_workflow.md 第 2 步）。
	// chat.tool_call 的 name 取 ToolCall.Name（工具卡 Name="build_team"），
	// 不是工具卡 ID "team.build_team"；两种写法都接受，避免依赖注册命名细节。
	s.True(hasTeamBuildToolCall(toolNames), "leader 应先调用 build_team 组建团队，实际工具序列=%v", toolNames)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sendChatSend 发送一条 chat.send 请求帧。
func (s *TeamContinuousRealSuite) sendChatSend(conn *websocket.Conn, id, sessionID, content string) error {
	req := map[string]any{
		"type":   "req",
		"id":     id,
		"method": "chat.send",
		"params": map[string]any{
			"session_id": sessionID,
			"mode":       "team",
			"content":    content,
		},
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, payload)
}

// pickFreePort 选一个空闲端口。
func (s *TeamContinuousRealSuite) pickFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// saveAndSetenv 记录并设置环境变量。
func (s *TeamContinuousRealSuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	flag := "0"
	if had {
		flag = "1"
	}
	s.envBackup = append(s.envBackup, [3]string{key, orig, flag})
	_ = os.Setenv(key, val)
}

// restoreEnv 恢复环境变量。
func (s *TeamContinuousRealSuite) restoreEnv() {
	for _, e := range s.envBackup {
		if e[2] == "1" {
			_ = os.Setenv(e[0], e[1])
		} else {
			_ = os.Unsetenv(e[0])
		}
	}
	s.envBackup = nil
}

// realEventName 取帧的事件名。
func realEventName(frame map[string]any) string {
	name, _ := frame["event"].(string)
	return name
}

// realFrameID 取帧的请求 id。
func realFrameID(frame map[string]any) string {
	id, _ := frame["id"].(string)
	return id
}

// realAccepted 判断是否为 accepted=true 的受理响应帧。
func realAccepted(frame map[string]any) bool {
	payload, ok := frame["payload"].(map[string]any)
	if !ok {
		return false
	}
	accepted, ok := payload["accepted"].(bool)
	return ok && accepted
}

// realToolSucceeded 判断 chat.tool_result 帧是否 success=true。
func realToolSucceeded(frame map[string]any) bool {
	payload, ok := frame["payload"].(map[string]any)
	if !ok {
		return false
	}
	success, ok := payload["success"].(bool)
	return ok && success
}

// realToolName 取 chat.tool_result payload 里的工具名。
func realToolName(payload map[string]any) string {
	name, _ := payload["tool_name"].(string)
	return name
}

// realFrameToolName 从整帧取 chat.tool_result 的工具名。
func realFrameToolName(frame map[string]any) string {
	payload, ok := frame["payload"].(map[string]any)
	if !ok {
		return ""
	}
	return realToolName(payload)
}

// normalizeTeamToolName 去掉工具卡 ID 的 "team." 前缀，便于比较 Name 与 ID 两种写法。
func normalizeTeamToolName(name string) string {
	return strings.TrimPrefix(strings.TrimSpace(name), "team.")
}

// isTeamBuildTool 判断是否为建队/拉人工具（成员创建的两条路径）。
func isTeamBuildTool(name string) bool {
	switch normalizeTeamToolName(name) {
	case "build_team", "spawn_member":
		return true
	default:
		return false
	}
}

// hasTeamBuildToolCall 判断工具序列里是否出现 build_team。
func hasTeamBuildToolCall(names []string) bool {
	for _, name := range names {
		if normalizeTeamToolName(name) == "build_team" {
			return true
		}
	}
	return false
}
