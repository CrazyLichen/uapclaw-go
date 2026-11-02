//go:build integration

package workspace_test

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InitFlowSuite 工作区初始化链路集成测试套件。
//
// 对齐 Python test_init_workspace.py 的 6 个测试类，按 Go 实际布局断言。
type InitFlowSuite struct {
	isuite.BaseIntegrationSuite
	// dataRoot 本次测试的隔离根目录
	dataRoot string
	// resDir 模板资源目录
	resDir string
	// wsDir 初始化产生的工作区目录
	wsDir string
	// envBackup 环境变量备份（[key, 原值, 是否原本存在]）
	envBackup [][3]string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestInitFlowSuite 套件入口。
func TestInitFlowSuite(t *testing.T) {
	suite.Run(t, new(InitFlowSuite))
}

// SetupTest 每条用例重建隔离环境。
func (s *InitFlowSuite) SetupTest() {
	s.dataRoot = s.T().TempDir()
	s.resDir = filepath.Join(s.dataRoot, "resources")
	s.wsDir = filepath.Join(s.dataRoot, "data")
	s.buildTestResources()

	s.saveAndSetenv(workspace.EnvDataDir, s.wsDir)
	s.saveAndSetenv(workspace.EnvResourcesDir, s.resDir)
	workspace.SetUserHome("")
	pathutil.ResetCache()
}

// TearDownTest 恢复环境变量与缓存。
func (s *InitFlowSuite) TearDownTest() {
	s.restoreEnv()
	workspace.SetUserHome("")
	pathutil.ResetCache()
}

// TestInit_显式语言 对齐 Python test_resolve_explicit_language。
// 显式 Language 参数优先，写回 config.yaml 且 config.New 可读回。
func (s *InitFlowSuite) TestInit_显式语言() {
	s.forceNonInteractive()

	result, err := workspace.Init(workspace.InitOption{Language: "en"})
	s.Require().NoError(err)
	s.Require().False(result.Cancelled)
	s.Require().Equal(s.wsDir, result.WorkspaceDir)
	s.Require().Equal("en", s.readPreferredLanguage())
}

// TestInit_缺省回落zh 对齐 Python test_resolve_default_to_zh。
// 非交互（管道 stdin）且不传语言时回落 zh。
func (s *InitFlowSuite) TestInit_缺省回落zh() {
	s.forceNonInteractive()

	result, err := workspace.Init(workspace.InitOption{})
	s.Require().NoError(err)
	s.Require().False(result.Cancelled)
	s.Require().Equal("zh", s.readPreferredLanguage())
}

// TestInit_首次初始化_结构 对齐 Python test_init_user_workspace_first_time。
//
// 断言按 Go 实际布局（workspace.Prepare）：config/ + agent/{.checkpoint,.logs,
// sessions,workspace}。与 Python conftest.py:165-174 期望的 agent/home、
// agent/skills、agent/memory 存在布局差异（Go 的技能目录挂在
// agent/workspace/skills 下），本批不修改生产代码，仅记录差异。
func (s *InitFlowSuite) TestInit_首次初始化_结构() {
	s.forceNonInteractive()

	result, err := workspace.Init(workspace.InitOption{Language: "zh"})
	s.Require().NoError(err)
	s.Require().False(result.Cancelled)

	dirs := []string{
		filepath.Join(s.wsDir, "config"),
		filepath.Join(s.wsDir, "agent", "workspace"),
		filepath.Join(s.wsDir, "agent", "workspace", "memory"),
		filepath.Join(s.wsDir, "agent", "sessions"),
		filepath.Join(s.wsDir, "agent", ".checkpoint"),
		filepath.Join(s.wsDir, "agent", ".logs"),
	}
	for _, d := range dirs {
		s.Require().DirExists(d, "目录应存在: %s", d)
	}

	files := []string{
		filepath.Join(s.wsDir, "config", "config.yaml"),
		filepath.Join(s.wsDir, "config", ".env"),
		filepath.Join(s.wsDir, "agent", "workspace", "AGENT.md"),
		filepath.Join(s.wsDir, "agent", "workspace", "HEARTBEAT.md"),
		filepath.Join(s.wsDir, "agent", "workspace", "IDENTITY.md"),
		filepath.Join(s.wsDir, "agent", "workspace", "SOUL.md"),
		filepath.Join(s.wsDir, "agent", "workspace", "memory", "MEMORY.md"),
	}
	for _, f := range files {
		s.Require().FileExists(f, "文件应存在: %s", f)
	}
}

// TestInit_force重建 对齐 Python test_main_force_init。
// force 模式清空工作区并重建，脏文件被清除。
func (s *InitFlowSuite) TestInit_force重建() {
	s.forceNonInteractive()
	s.Require().NoError(os.MkdirAll(s.wsDir, 0o755))
	dirty := filepath.Join(s.wsDir, "dirty.txt")
	s.Require().NoError(os.WriteFile(dirty, []byte("stale"), 0o644))

	result, err := workspace.Init(workspace.InitOption{Language: "en", Overwrite: true})
	s.Require().NoError(err)
	s.Require().False(result.Cancelled)
	s.Require().NoFileExists(dirty)
	s.Require().FileExists(filepath.Join(s.wsDir, "config", "config.yaml"))
}

// TestInit_config内容 覆盖 Python 中被 skip 的 test_config_file_content。
// config.yaml 的语言与 models 段能被 config.New 正确解析。
func (s *InitFlowSuite) TestInit_config内容() {
	s.forceNonInteractive()

	_, err := workspace.Init(workspace.InitOption{Language: "en"})
	s.Require().NoError(err)

	cfg, err := config.New("")
	s.Require().NoError(err)
	_, err = cfg.Load()
	s.Require().NoError(err)

	s.Require().Equal("en", cfg.Get("preferred_language"))
	s.Require().NotNil(cfg.Get("models.defaults"), "models.defaults 应可解析")
}

// TestPrompt_选择编号1 对齐 Python test_prompt_select_chinese。
func (s *InitFlowSuite) TestPrompt_选择编号1() {
	s.withPTY("1\n")
	result, err := workspace.Init(workspace.InitOption{})
	s.Require().NoError(err)
	s.Require().False(result.Cancelled)
	s.Require().Equal("zh", s.readPreferredLanguage())
}

// TestPrompt_选择编号2 对齐 Python test_prompt_select_english。
func (s *InitFlowSuite) TestPrompt_选择编号2() {
	s.withPTY("2\n")
	result, err := workspace.Init(workspace.InitOption{})
	s.Require().NoError(err)
	s.Require().False(result.Cancelled)
	s.Require().Equal("en", s.readPreferredLanguage())
}

// TestPrompt_别名zh 对齐 Python test_prompt_select_zh_alias。
func (s *InitFlowSuite) TestPrompt_别名zh() {
	s.withPTY("zh\n")
	result, err := workspace.Init(workspace.InitOption{})
	s.Require().NoError(err)
	s.Require().False(result.Cancelled)
	s.Require().Equal("zh", s.readPreferredLanguage())
}

// TestPrompt_别名en 对齐 Python test_prompt_select_en_alias。
func (s *InitFlowSuite) TestPrompt_别名en() {
	s.withPTY("en\n")
	result, err := workspace.Init(workspace.InitOption{})
	s.Require().NoError(err)
	s.Require().False(result.Cancelled)
	s.Require().Equal("en", s.readPreferredLanguage())
}

// TestPrompt_取消no 对齐 Python test_prompt_cancel_with_no。
func (s *InitFlowSuite) TestPrompt_取消no() {
	s.withPTY("no\n")
	s.requireCancelledWithoutWorkspace()
}

// TestPrompt_取消n 对齐 Python test_prompt_cancel_with_n。
func (s *InitFlowSuite) TestPrompt_取消n() {
	s.withPTY("n\n")
	s.requireCancelledWithoutWorkspace()
}

// TestPrompt_取消q 对齐 Python test_prompt_cancel_with_q。
func (s *InitFlowSuite) TestPrompt_取消q() {
	s.withPTY("q\n")
	s.requireCancelledWithoutWorkspace()
}

// TestPrompt_非法输入 对齐 Python test_prompt_invalid_input_returns_none。
func (s *InitFlowSuite) TestPrompt_非法输入() {
	s.withPTY("xyz\n")
	s.requireCancelledWithoutWorkspace()
}

// TestInit_取消不落盘 对齐 Python test_init_user_workspace_cancel_language_selection。
func (s *InitFlowSuite) TestInit_取消不落盘() {
	s.withPTY("q\n")
	s.requireCancelledWithoutWorkspace()
}

// TestInit_覆盖前拒绝 对齐 Python test_main_cancelled_init。
// force 模式下对 overwrite 确认答 no，原工作区保持不动。
func (s *InitFlowSuite) TestInit_覆盖前拒绝() {
	s.forceNonInteractive()
	_, err := workspace.Init(workspace.InitOption{Language: "zh"})
	s.Require().NoError(err)
	sentinel := filepath.Join(s.wsDir, "sentinel.txt")
	s.Require().NoError(os.WriteFile(sentinel, []byte("keep"), 0o644))

	s.withPTY("no\n")
	result, err := workspace.Init(workspace.InitOption{Language: "zh", Overwrite: true})
	s.Require().NoError(err)
	s.Require().True(result.Cancelled, "overwrite 确认答 no 时应取消")
	s.Require().FileExists(sentinel)
}

// TestInit_链路_服务可启动 覆盖 Python 中被 skip 的 test_full_initialization_flow。
//
// 初始化产物 → 配置加载 → 进程内 transport/server/gateway 装配 → WS 握手
// → initialize 往返。这条用例同时是本目录通过 integration-audit 调用跨度
// 门禁的依据：文件内真实调用 swarm/transport、swarm/server、swarm/gateway
// 三个子系统，跨度 = 3。
func (s *InitFlowSuite) TestInit_链路_服务可启动() {
	s.forceNonInteractive()

	_, err := workspace.Init(workspace.InitOption{Language: "en"})
	s.Require().NoError(err)

	wsPort := s.pickFreePort()
	s.saveAndSetenv("UAPCLAW_WEB_PORT", fmt.Sprintf("%d", wsPort))
	pathutil.ResetCache()

	cfg, err := config.New("")
	s.Require().NoError(err)
	_, err = cfg.Load()
	s.Require().NoError(err)
	s.Require().Equal("en", cfg.Get("preferred_language"))

	ctx := s.Ctx
	tr := transport.NewChannelTransport()
	agentServer := server.NewAgentServer(cfg, tr)
	agentClient := routing.NewAgentClient(tr)
	gwServer, err := gateway.NewAppGatewayServer(cfg, agentClient)
	s.Require().NoError(err)

	s.Require().NoError(agentServer.Start(ctx))
	s.Require().NoError(gwServer.Start(ctx))
	s.T().Cleanup(func() {
		_ = gwServer.Stop()
		_ = agentServer.Stop()
	})

	url := fmt.Sprintf("ws://127.0.0.1:%d/ws", wsPort)
	var conn *websocket.Conn
	s.Require().Eventually(func() bool {
		c, _, dialErr := websocket.DefaultDialer.Dial(url, nil)
		if dialErr != nil {
			return false
		}
		conn = c
		return true
	}, 15*time.Second, 200*time.Millisecond, "WS 拨号超时：%s", url)
	defer conn.Close()

	// initialize 是轻量握手方法，不触发 LLM 调用
	req := map[string]any{
		"type":   "req",
		"id":     "init-probe-1",
		"method": "initialize",
		"params": map[string]any{},
	}
	payload, err := json.Marshal(req)
	s.Require().NoError(err)
	s.Require().NoError(conn.WriteMessage(websocket.TextMessage, payload))

	var respID string
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	for i := 0; i < 50; i++ {
		_, raw, err := conn.ReadMessage()
		s.Require().NoError(err, "等待 initialize 响应超时")
		var frame map[string]any
		if json.Unmarshal(raw, &frame) != nil {
			continue
		}
		if id, ok := frame["id"].(string); ok && id == "init-probe-1" {
			respID = id
			s.T().Logf("initialize 响应: %s", truncateFrame(raw))
			break
		}
	}
	s.Require().Equal("init-probe-1", respID, "应收到 id 匹配的 initialize 响应")
}

// ──────────────────────────── 非导出函数（PTY） ────────────────────────────

// withPTY 打开 PTY、装上 stdin、写入 input，环境不支持时 Skip。
func (s *InitFlowSuite) withPTY(input string) {
	s.T().Helper()
	master, slave, err := testhelpers.OpenPTY()
	if err != nil {
		s.T().Skipf("当前环境不支持 PTY：%v", err)
	}
	s.T().Cleanup(func() {
		_ = master.Close()
		_ = slave.Close()
	})
	testhelpers.WithPTYStdin(s.T(), slave)
	testhelpers.WritePTYInput(s.T(), master, input)
}

// pickFreePort 选一个空闲端口。
func (s *InitFlowSuite) pickFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// truncateFrame 截断帧内容用于日志。
func truncateFrame(raw []byte) string {
	const max = 300
	if len(raw) <= max {
		return string(raw)
	}
	return string(raw[:max]) + "..."
}

// requireCancelledWithoutWorkspace 断言取消且未创建工作区。
func (s *InitFlowSuite) requireCancelledWithoutWorkspace() {
	result, err := workspace.Init(workspace.InitOption{})
	s.Require().NoError(err)
	s.Require().True(result.Cancelled, "应取消初始化")
	s.Require().NoDirExists(filepath.Join(s.wsDir, "config"))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// forceNonInteractive 把 stdin 换成已关闭的管道，强制走非交互分支。
//
// 必要性：isInteractive() 判的是 ModeCharDevice，而测试进程的 stdin
// 常见为 /dev/null——它同样是字符设备，会被误判成"连着终端"，
// 从而走进 overwrite/语言确认的交互分支。
func (s *InitFlowSuite) forceNonInteractive() {
	s.T().Helper()
	old := os.Stdin
	r, w, err := os.Pipe()
	s.Require().NoError(err)
	os.Stdin = r
	_ = w.Close()
	s.T().Cleanup(func() { os.Stdin = old })
}

// buildTestResources 构造模板资源目录。
//
// config.yaml 必须包含 models 段——链路用例要据此装配 mock provider。
func (s *InitFlowSuite) buildTestResources() {
	ws := filepath.Join(s.resDir, "agent", "workspace")
	s.Require().NoError(os.MkdirAll(filepath.Join(ws, "memory"), 0o755))

	configYAML := `preferred_language: zh
models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: mock
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        client_id: mock
      is_default: true
`
	s.Require().NoError(os.WriteFile(filepath.Join(s.resDir, "config.yaml"), []byte(configYAML), 0o644))
	s.Require().NoError(os.WriteFile(filepath.Join(s.resDir, ".env.template"), []byte("API_KEY=sk-test\n"), 0o644))

	for _, name := range []string{"AGENT", "HEARTBEAT", "IDENTITY", "SOUL"} {
		s.Require().NoError(os.WriteFile(filepath.Join(ws, name+"_ZH.md"), []byte("# "+name+" 中文"), 0o644))
		s.Require().NoError(os.WriteFile(filepath.Join(ws, name+"_EN.md"), []byte("# "+name+" English"), 0o644))
	}
	s.Require().NoError(os.WriteFile(filepath.Join(ws, "USER.md"), []byte("# User"), 0o644))
	s.Require().NoError(os.WriteFile(filepath.Join(ws, "memory", "MEMORY_ZH.md"), []byte("记忆中文"), 0o644))
	s.Require().NoError(os.WriteFile(filepath.Join(ws, "memory", "MEMORY_EN.md"), []byte("Memory English"), 0o644))
}

// readPreferredLanguage 读回工作区 config.yaml 中的 preferred_language。
func (s *InitFlowSuite) readPreferredLanguage() any {
	cfg, err := config.New("")
	s.Require().NoError(err)
	_, err = cfg.Load()
	s.Require().NoError(err)
	return cfg.Get("preferred_language")
}

// saveAndSetenv 记录并设置环境变量。
func (s *InitFlowSuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	flag := "0"
	if had {
		flag = "1"
	}
	s.envBackup = append(s.envBackup, [3]string{key, orig, flag})
	_ = os.Setenv(key, val)
}

// restoreEnv 恢复环境变量并清空备份。
func (s *InitFlowSuite) restoreEnv() {
	for _, e := range s.envBackup {
		if e[2] == "1" {
			_ = os.Setenv(e[0], e[1])
		} else {
			_ = os.Unsetenv(e[0])
		}
	}
	s.envBackup = nil
}
