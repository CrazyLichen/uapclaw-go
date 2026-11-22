//go:build integration && llm

// 15.18 RPC 入口全链用例（用户裁决补口子①）：从 web WS chat.send RPC 进来，
// 经 AgentServer.handleMessage → handleStream → agentRuntime.Stream →
// DeepAdapter 根入口（读章/兜底解析盖章 D-18-13）→ session child external 分支
// （R-2 挂载）→ ProcessExternalHarnessStream → 真 codebuddy CLI 子进程 →
// 帧写回 WS 客户端。前置半区（RPC 层之前）由 tui_cli_route 套件覆盖。
package real_llm_test

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	routing "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	transport "github.com/uapclaw/uapclaw-go/internal/swarm/transport"
	gateway "github.com/uapclaw/uapclaw-go/internal/swarm/gateway"
	server "github.com/uapclaw/uapclaw-go/internal/swarm/server"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExternalHarnessRPCSuite 15.18 RPC 入口全链套件（真 CLI，双守卫）。
type ExternalHarnessRPCSuite struct {
	isuite.AgentSuite
	wsPort    int
	dataDir   string
	agentSrv  *server.AgentServer
	gwServer  *gateway.AppGatewayServer
	envBackup [][3]string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestExternalHarnessRPCSuite 套件入口。
func TestExternalHarnessRPCSuite(t *testing.T) {
	suite.Run(t, new(ExternalHarnessRPCSuite))
}

// ──────────────────────────── 测试 ────────────────────────────

// TestRealCBC_RPCEntryExternalTurn RPC 入口发绑定专家的 chat.send，整轮流帧回写。
func (s *ExternalHarnessRPCSuite) TestRealCBC_RPCEntryExternalTurn() {
	cliPath := os.Getenv("CODEBUDDY_CLI_PATH")
	if cliPath == "" {
		cliPath = "codebuddy"
	}
	if _, err := exec.LookPath(cliPath); err != nil {
		s.T().Skip("codebuddy CLI 不在 PATH，跳过真跑")
	}

	// 专家包（真 CLI + 真模型）
	pkgDir := filepath.Join(s.dataDir, "agent", "workspace", "plugins",
		"agent_templates", "local", "expert_rpc")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		s.T().Fatal(err)
	}
	model := os.Getenv("CODEBUDDY_MODEL")
	if model == "" {
		model = "hy4flash-free"
	}
	manifest := `{"name":"expert_rpc","description":"rpc entry expert","package_type":"agent_template",` +
		`"agent_card":{"name":"expert_rpc"},` +
		`"runtime":{"provider_name":"codebuddy","config":{"cli_path":"` + cliPath + `","model":"` + model + `"}}}`
	if err := os.WriteFile(filepath.Join(pkgDir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		s.T().Fatal(err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(
		fmt.Sprintf("ws://127.0.0.1:%d/ws", s.wsPort), nil)
	s.Require().NoError(err, "WS 拨号失败")
	defer conn.Close()

	sessionID := fmt.Sprintf("sess_rpc_cbc_%d", time.Now().UnixNano())
	reqFrame := map[string]any{
		"type":      "req",
		"id":        "req-rpc-cbc-1",
		"method":    "chat.send",
		"is_stream": true,
		"params": map[string]any{
			"session_id":          sessionID,
			"mode":                "agent",
			"content":             "只回复两个字：好的",
			"agent_template_name": "expert_rpc",
		},
	}
	payload, err := json.Marshal(reqFrame)
	s.Require().NoError(err)
	s.Require().NoError(conn.WriteMessage(websocket.TextMessage, payload))

	// 确定性契约：① 流被受理（有帧回写）② 链路级错误（绑定冲突/面板未启用/
	// 启动失败/包缺失）即失败 ③ final 非空。模型行为不断言。
	var finalContent string
	var chainBroken []string
	sawAnyFrame := false
	s.Require().NoError(conn.SetReadDeadline(time.Now().Add(180 * time.Second)))
readLoop:
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			s.T().Logf("读帧结束: %v", err)
			break readLoop
		}
		sawAnyFrame = true
		var frame struct {
			Event   string         `json:"event"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(raw, &frame); err != nil {
			continue
		}
		eventType := frame.Event
		switch eventType {
		case "chat.final":
			if text, _ := frame.Payload["content"].(string); text != "" {
				finalContent = text
			}
		case "chat.error":
			if text, _ := frame.Payload["error"].(string); text != "" {
				chainBroken = append(chainBroken, text)
			}
		}
		if finalContent != "" || len(chainBroken) > 0 {
			break readLoop
		}
	}
	s.Require().True(sawAnyFrame, "流应被受理并回帧")
	for _, text := range chainBroken {
		for _, marker := range []string{"已绑定三方专家", "请先在配置界面启用", "启动失败", "not found", "必须是字符串"} {
			if strings.Contains(text, marker) {
				s.T().Fatalf("链路级错误: %s", text)
			}
		}
	}
	s.Require().NotEmpty(finalContent, "final 内容应非空（真 CLI 整轮）")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupSuite 装配 RPC 三件套（transport + AgentServer + AppGatewayServer）。
// 对齐 team_continuous_dialog_real_test.go 的最小三件套形态。
func (s *ExternalHarnessRPCSuite) SetupSuite() {
	s.AgentSuite.SetupSuite()

	cliPath := os.Getenv("CODEBUDDY_CLI_PATH")
	if cliPath == "" {
		cliPath = "codebuddy"
	}
	if _, err := exec.LookPath(cliPath); err != nil {
		s.T().Skip("codebuddy CLI 不在 PATH，跳过真跑")
	}

	// 数据目录 + 面板启用条目 + 专家包（SetupSuite 建，测试体只发帧）
	s.dataDir = s.T().TempDir()
	s.saveAndSetenv("UAPCLAW_DATA_DIR", s.dataDir)
	path.ResetCache()
	s.T().Cleanup(path.ResetCache)

	cfgDir := filepath.Join(s.dataDir, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		s.T().Fatal(err)
	}
	// models.defaults 给根适配器装配兜底（external child 不消费，但根初始化链要过）
	yaml := `models:
  defaults:
    - model_client_config:
        model_name: rpc-entry-default
        client_provider: codebuddy
        api_key: rpc-placeholder
        api_base: http://127.0.0.1:1
        client_id: rpc_entry
        verify_ssl: false
      is_default: true
modes:
  team:
    jiuwen_team:
      external_cli_agents:
        - cli_agent: codebuddy
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(yaml), 0o644); err != nil {
		s.T().Fatal(err)
	}

	// UAPCLAW_WEB_PORT 须在 AppGatewayServer 构造前设置（对齐 real dialog 套件顺序）
	s.wsPort = freePort()
	s.saveAndSetenv("UAPCLAW_WEB_PORT", fmt.Sprintf("%d", s.wsPort))

	cfg, err := config.New("")
	s.Require().NoError(err)
	_, err = cfg.Load()
	s.Require().NoError(err)

	tr := transport.NewChannelTransport()
	s.agentSrv = server.NewAgentServer(cfg, tr)
	agentClient := routing.NewAgentClient(tr)
	s.gwServer, err = gateway.NewAppGatewayServer(cfg, agentClient)
	s.Require().NoError(err)

	s.Require().NoError(s.agentSrv.Start(s.Ctx))
	s.Require().NoError(s.gwServer.Start(s.Ctx))
}

// TearDownSuite 逆序停机 + 环境还原。
func (s *ExternalHarnessRPCSuite) TearDownSuite() {
	if s.gwServer != nil {
		_ = s.gwServer.Stop()
	}
	if s.agentSrv != nil {
		_ = s.agentSrv.Stop()
	}
	s.AgentSuite.TearDownSuite()
	for _, item := range s.envBackup {
		if item[2] == "1" {
			_ = os.Setenv(item[0], item[1])
		} else {
			_ = os.Unsetenv(item[0])
		}
	}
}

// saveAndSetenv 记录并设置环境变量。
func (s *ExternalHarnessRPCSuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	flag := "0"
	if had {
		flag = "1"
	}
	s.envBackup = append(s.envBackup, [3]string{key, orig, flag})
	_ = os.Setenv(key, val)
}

// freePort 选空闲端口。
func freePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
