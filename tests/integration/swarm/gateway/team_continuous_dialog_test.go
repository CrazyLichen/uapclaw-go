//go:build integration

package gateway_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/suite"

	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamDialogSuite team 模式连续对话集成测试套件。
//
// 对齐 Python jiuwenswarm/tests/system_tests/test_team_continuous_dialog.py
// （Python 侧 @pytest.mark.skip"temporarily skipped until stable"）。
// 进程内变体：不起独立进程，用 ChannelTransport 把 AppGatewayServer 与
// AgentServer 串起来，客户端仍走真实 WS 拨号 /ws。
type TeamDialogSuite struct {
	isuite.BaseIntegrationSuite
	// providerName 注册到 ClientRegistry 的 mock provider 名
	providerName string
	// wsPort Web 通道监听端口
	wsPort int
	// gatewayPort 多路由 WS 服务器端口（默认 19001，必须隔离，否则并行包互撞）
	gatewayPort int
	// dataDir 隔离的数据目录
	dataDir string
	// agentServer 进程内 AgentServer
	agentServer *server.AgentServer
	// gwServer 进程内 AppGatewayServer
	gwServer *gateway.AppGatewayServer
	// envBackup 环境变量备份（[key, 原值, 是否原本存在]）
	envBackup [][3]string
	// origWd 套件开始前的工作目录（成员技能/记忆目录在未设根路径时
	// 会落到进程 CWD，套件期间切到临时目录避免污染仓库）
	origWd string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTeamDialogSuite 套件入口。
func TestTeamDialogSuite(t *testing.T) {
	suite.Run(t, new(TeamDialogSuite))
}

// SetupSuite 装配进程内 serve（gateway + server + transport）。
func (s *TeamDialogSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()

	s.providerName = fmt.Sprintf("mock_team_itest_%d", time.Now().UnixNano())
	model_clients.GetClientRegistry().Register(s.providerName, "llm", s.MockLLM.Factory())

	s.dataDir = s.T().TempDir()
	wd, err := os.Getwd()
	s.Require().NoError(err)
	s.origWd = wd
	s.Require().NoError(os.Chdir(s.dataDir))
	configDir := filepath.Join(s.dataDir, "config")
	s.Require().NoError(os.MkdirAll(configDir, 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(s.dataDir, "agent", "workspace"), 0o755))
	// modes.team 必须存在：team 规格解析依赖它，缺失时 TeamAgentSpec.Transport
	// 为空 → leader messager 为空 → TeamBackend 不装配（kernel 会退化）。
	// workspace 关闭：TeamWorkspace 默认落 CWD，会让测试在包目录写出
	// memory/skills 产物；本用例只验证 chat 流程，不需要工作空间。
	yaml := fmt.Sprintf(`workspace:
  path: %s
modes:
  team:
    workspace:
      enabled: false
models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: %s
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        client_id: %s
      is_default: true
`, s.dataDir, s.providerName, s.providerName)
	s.Require().NoError(os.WriteFile(filepath.Join(configDir, "config.yaml"), []byte(yaml), 0o644))

	// 真实初始化工作区：生产路径下 AgentWorkspaceDir 依赖"已初始化"状态，
	// 未初始化时回退相对路径，会在包目录写出 memory/skills 产物。
	repoRoot, err := filepath.Abs(filepath.Join(s.origWd, "..", "..", "..", ".."))
	s.Require().NoError(err)
	s.saveAndSetenv("UAPCLAW_RESOURCES_DIR", filepath.Join(repoRoot, "resources"))

	s.saveAndSetenv("UAPCLAW_DATA_DIR", s.dataDir)
	pathutil.ResetCache()

	_, err = workspace.Init(workspace.InitOption{Language: "zh"})
	s.Require().NoError(err)

	// 三子系统构造（integration-audit 调用跨度来源，勿移到 helper 包）
	tr := transport.NewChannelTransport()
	agentClient := routing.NewAgentClient(tr)

	// 端口绑定重试：pickFreePort 是"listen :0 → 取号 → 关闭"，
	// 到网关真正 bind 之间有空窗，CI 并行时可能被别的进程抢占。
	// Start 非阻塞且端口被占时仍返回 nil，故必须探活 + 读 ListenErr 才能判定。
	for attempt := 1; attempt <= 3; attempt++ {
		s.wsPort = s.pickFreePort()
		s.gatewayPort = s.pickFreePort()
		if attempt == 1 {
			s.saveAndSetenv("UAPCLAW_WEB_PORT", fmt.Sprintf("%d", s.wsPort))
			s.saveAndSetenv("UAPCLAW_GATEWAY_PORT", fmt.Sprintf("%d", s.gatewayPort))
		} else {
			_ = os.Setenv("UAPCLAW_WEB_PORT", fmt.Sprintf("%d", s.wsPort))
			_ = os.Setenv("UAPCLAW_GATEWAY_PORT", fmt.Sprintf("%d", s.gatewayPort))
		}
		pathutil.ResetCache()

		cfg, cfgErr := config.New("")
		s.Require().NoError(cfgErr)
		_, loadErr := cfg.Load()
		s.Require().NoError(loadErr)

		if attempt == 1 {
			s.agentServer = server.NewAgentServer(cfg, tr)
			s.Require().NoError(s.agentServer.Start(s.Ctx))
		}
		gwServer, gwErr := gateway.NewAppGatewayServer(cfg, agentClient)
		s.Require().NoError(gwErr)
		s.Require().NoError(gwServer.Start(s.Ctx))

		if readyErr := waitTCPReady(gwServer, s.wsPort, 3*time.Second); readyErr == nil {
			s.gwServer = gwServer
			break
		} else {
			s.T().Logf("第 %d 次绑定端口 %d 失败（疑似并发抢占）：%v，换号重试", attempt, s.wsPort, readyErr)
			_ = gwServer.Stop()
			if attempt == 3 {
				s.FailNow("连续 3 次无法绑定 Web WS 端口")
			}
		}
	}
}

// TearDownSuite 逆序停机 + 清理注册。
func (s *TeamDialogSuite) TearDownSuite() {
	if s.gwServer != nil {
		_ = s.gwServer.Stop()
	}
	if s.agentServer != nil {
		_ = s.agentServer.Stop()
	}
	// 先取消后台 ctx，再还原环境变量与路径缓存。
	// 反序（先 restoreEnv 后 cancel）会让仍在运行的 team monitor / 落盘协程
	// 用「默认 home」重新解析路径，撞出 unable to open database file
	// （CI run 41513341744：本套件 kv_store 查询报该错，紧跟上一套件残留的
	// context canceled 写入）。Cancel 幂等，基类 TearDownSuite 仍会再调一次。
	s.Cancel()
	// Stop()/ctx 取消只发信号不 join：team monitor、session 落盘等后台协程可能在
	// 用例收尾后仍在写 agent/sessions，撞上 testing 的 TempDir 清理会报
	// 「unlinkat ...: directory not empty」（CI 41504858262 实证）。
	// 等目录树静默后再交给清理，避免把后台退出时序当成用例失败。
	if !waitDirQuiesce(s.dataDir, 500*time.Millisecond, 5*time.Second) {
		s.T().Logf("停机后 %s 仍在变化：后台协程未及时退出", s.dataDir)
	}
	if s.providerName != "" {
		_ = model_clients.GetClientRegistry().Unregister(s.providerName, "llm")
	}
	s.restoreEnv()
	pathutil.ResetCache()
	if s.origWd != "" {
		_ = os.Chdir(s.origWd)
	}
	s.BaseIntegrationSuite.TearDownSuite()
}

// TestTeamContinuousDialog 对齐 Python test_team_continuous_dialog 场景：
// 首条 chat.send(mode=team) 未结束时发出第二条，验证服务端不互斥、
// Leader 能建队并 spawn 成员、两条请求各自被受理且各有回复。
//
// 断言口径与 Python 原用例一致：出现 team.member 或 team.message
// （monitor 分类事件），且两条 chat.send 在首条流未结束时都被受理。
//
// 触发链（与 Python 一致）：spawn_member 只写成员记录 → send_message 的
// _auto_start_members 启动成员 → 发布 MemberSpawnedEvent → TeamMonitor
// 转 MonitorEvent → TeamMonitorHandler 归类为 team.member 推给前端。
func (s *TeamDialogSuite) TestTeamContinuousDialog() {
	s.MockLLM.ResetCalls()
	s.MockLLM.SetResponses(
		// 团队首次请求要先 build_team：leader 的 team_member 记录由 build_team 注册
		// （Python 同样如此），否则成员 send_message(to=leader) 会过不了 roster 校验。
		mockllm.CreateToolCallResponse("team.build_team",
			`{"display_name":"测试团队","leader_display_name":"队长","leader_desc":"负责协调","team_desc":"集成测试团队"}`),
		mockllm.CreateToolCallResponse("team.spawn_member", `{"member_name":"teammate-itest","display_name":"成员","desc":"测试成员"}`),
		mockllm.CreateToolCallResponse("team.send_message", `{"to":"teammate-itest","content":"请汇报你的工具清单"}`),
		mockllm.CreateTextResponse("第一条消息的响应"),
		// 成员侧轮次：先回消息给 leader，再收尾
		mockllm.CreateToolCallResponse("team.send_message", `{"to":"team_leader","content":"我的工具清单是：spawn_member, send_message"}`),
		mockllm.CreateTextResponse("成员回复完成"),
		mockllm.CreateTextResponse("第二条消息的响应"),
	)

	conn, _, err := websocket.DefaultDialer.Dial(
		fmt.Sprintf("ws://127.0.0.1:%d/ws", s.wsPort), nil)
	s.Require().NoError(err, "WS 拨号失败")
	defer conn.Close()

	sessionID := fmt.Sprintf("sess_team_%d", time.Now().UnixNano())
	s.Require().NoError(s.sendChatSend(conn, "req-team-1", sessionID,
		"告诉我你有哪些工具，不要立刻开始任务"))

	var (
		firstAccepted     bool
		secondAccepted    bool
		runtimeReady      bool
		memberSpawned     bool
		secondSentAt      time.Time
		firstFinalAfter   bool
		firstFinalTime    time.Time
		secondSent        bool
		sawMember         bool
		sawMessage        bool
		sawLeaderToMember bool
		sawMemberToLeader bool
	)

	deadline := time.Now().Add(120 * time.Second)
	var connErr error
	idle := 0
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			// 只有读超时才算"空闲"：团队流事件之间有静默期
			// （对齐 Python _recv_messages 的 except TimeoutError: continue）。
			// 连接被断等其他错误必须立即记录并退出，否则会被 12 次空读
			// 掩盖成一堆 "Should be true"，看不出真实原因（CI 42135386234 实证）。
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				idle++
				if idle >= 12 {
					break
				}
				continue
			}
			connErr = err
			s.T().Logf("WS 连接异常终止: %v", err)
			break
		}
		idle = 0

		var frame map[string]any
		if json.Unmarshal(raw, &frame) != nil {
			continue
		}

		switch {
		case teamFrameID(frame) == "req-team-1" && teamAccepted(frame):
			firstAccepted = true
		case teamFrameID(frame) == "req-team-2" && teamAccepted(frame):
			secondAccepted = true
		}
		if raw, err := json.Marshal(frame["payload"]); err == nil && strings.Contains(string(raw), "member_id") {
			s.T().Logf("成员事件: event=%s payload=%s", teamEventName(frame), string(raw))
		}
		// Python 原断言：出现 team.member 或 team.message（monitor 分类事件）
		if teamEventName(frame) == "team.message" {
			if ev, ok := frame["payload"].(map[string]any)["event"].(map[string]any); ok {
				from, _ := ev["from_member"].(string)
				to, _ := ev["to_member"].(string)
				if from == "team_leader" && to == "teammate-itest" {
					sawLeaderToMember = true
				}
				if from == "teammate-itest" && to == "team_leader" {
					sawMemberToLeader = true
				}
				s.T().Logf("消息方向: %s → %s", from, to)
			}
		}
		switch teamEventName(frame) {
		case "team.member":
			sawMember = true
			s.T().Logf("收到分类事件: team.member")
		case "team.message":
			sawMessage = true
			s.T().Logf("收到分类事件: team.message")
		}
		switch teamEventName(frame) {
		case "team.runtime_ready":
			runtimeReady = true
		case "chat.tool_result":
			if teamToolSucceeded(frame) {
				memberSpawned = true
			}
		case "chat.final":
			if teamFrameSession(frame) == sessionID && secondSent {
				firstFinalAfter = true
				firstFinalTime = time.Now()
			}
		}

		// 首条已建队且成员已 spawn，且流尚未结束 → 此处发第二条
		if !secondSent && runtimeReady && memberSpawned {
			s.Require().NoError(s.sendChatSend(conn, "req-team-2", sessionID,
				"现在创建一个成员叫安娜，让安娜告诉我他有哪些工具"))
			secondSent = true
			secondSentAt = time.Now()
		}
		// 等成员侧回信：成员轮次晚于 leader 第一轮，不能提前收工
		if secondSent && secondAccepted && firstFinalAfter && sawMemberToLeader {
			break
		}
	}

	s.Require().NoError(connErr, "WS 连接异常终止（非空闲超时），后续断言不可信")
	s.T().Logf("观测: firstAccepted=%v runtimeReady=%v memberSpawned=%v secondAccepted=%v firstFinalAfter=%v llmCalls=%d",
		firstAccepted, runtimeReady, memberSpawned, secondAccepted, firstFinalAfter, s.MockLLM.InvokeCallCount())
	s.True(sawMember, "应收到 team.member（成员启动事件）")
	s.True(sawMessage, "应收到 team.message（成员间消息事件）")
	s.True(sawLeaderToMember, "leader 应把消息发给成员（from=team_leader → to=teammate-itest）")
	s.True(sawMemberToLeader, "成员应回消息给 leader（from=teammate-itest → to=team_leader）")
	s.True(firstAccepted, "首条 chat.send 应被受理")
	s.True(runtimeReady, "应收到 team.runtime_ready（团队已建立）")
	s.True(memberSpawned, "Leader 应成功 spawn 成员（chat.tool_result success）")
	s.True(secondSent, "第二条消息应在首条流未结束时发出")
	s.True(secondAccepted, "第二条 chat.send 应被受理（同 session 不互斥）")
	s.True(firstFinalAfter, "首条流应在第二条之后完成（未被第二条打断）")
	s.False(firstFinalTime.Before(secondSentAt), "首条 chat.final 不应早于第二条发出时间")
	s.GreaterOrEqual(s.MockLLM.InvokeCallCount(), 1, "Leader 应至少调用一次模型")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sendChatSend 发送一条 chat.send 请求帧。
func (s *TeamDialogSuite) sendChatSend(conn *websocket.Conn, id, sessionID, content string) error {
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

// teamEventName 取帧的事件名（顶层 event）。
func teamEventName(frame map[string]any) string {
	name, _ := frame["event"].(string)
	return name
}

// teamFrameID 取帧的请求 id。
func teamFrameID(frame map[string]any) string {
	id, _ := frame["id"].(string)
	return id
}

// teamFrameSession 取帧的 session_id（顶层或 payload 内层）。
func teamFrameSession(frame map[string]any) string {
	if sid, ok := frame["session_id"].(string); ok && sid != "" {
		return sid
	}
	if payload, ok := frame["payload"].(map[string]any); ok {
		if sid, ok := payload["session_id"].(string); ok {
			return sid
		}
	}
	return ""
}

// teamAccepted 判断是否为 accepted=true 的受理响应帧。
func teamAccepted(frame map[string]any) bool {
	payload, ok := frame["payload"].(map[string]any)
	if !ok {
		return false
	}
	accepted, ok := payload["accepted"].(bool)
	return ok && accepted
}

// teamToolSucceeded 判断 chat.tool_result 帧是否 success=true。
func teamToolSucceeded(frame map[string]any) bool {
	payload, ok := frame["payload"].(map[string]any)
	if !ok {
		return false
	}
	success, ok := payload["success"].(bool)
	return ok && success
}

// pickFreePort 选一个空闲端口。
func (s *TeamDialogSuite) pickFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// saveAndSetenv 记录并设置环境变量。
func (s *TeamDialogSuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	flag := "0"
	if had {
		flag = "1"
	}
	s.envBackup = append(s.envBackup, [3]string{key, orig, flag})
	_ = os.Setenv(key, val)
}

// restoreEnv 恢复环境变量。
func (s *TeamDialogSuite) restoreEnv() {
	for _, e := range s.envBackup {
		if e[2] == "1" {
			_ = os.Setenv(e[0], e[1])
		} else {
			_ = os.Unsetenv(e[0])
		}
	}
	s.envBackup = nil
}

// waitDirQuiesce 等待目录树停止变化：连续 quiet 时长无变化即视为静默，
// 最多等 max。返回是否在 max 内观察到静默。
func waitDirQuiesce(root string, quiet, max time.Duration) bool {
	deadline := time.Now().Add(max)
	last := dirSignature(root)
	stableSince := time.Now()
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		cur := dirSignature(root)
		if cur != last {
			last = cur
			stableSince = time.Now()
			continue
		}
		if time.Since(stableSince) >= quiet {
			return true
		}
	}
	return false
}

// dirSignature 汇总目录树的文件数与最新修改时间，用于判定「仍在写盘」。
func dirSignature(root string) string {
	count := 0
	var latest int64
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		count++
		if ts := info.ModTime().UnixNano(); ts > latest {
			latest = ts
		}
		return nil
	})
	return fmt.Sprintf("%d/%d", count, latest)
}
