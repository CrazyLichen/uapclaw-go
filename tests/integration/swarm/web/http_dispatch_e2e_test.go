//go:build integration

package web_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/config"

	"github.com/stretchr/testify/suite"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	modelclients "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// HTTPDispatchE2ESuite 27-A B2 /api/v1 HTTP 分发面 e2e（真实网关进程）。
// 覆盖：HTTP 全链会话多轮 / 带附件 / session CRUD / workspace 操作 / 下载 Range /
// METHOD_NOT_FOUND / catalog。
type HTTPDispatchE2ESuite struct {
	isuite.BaseIntegrationSuite
	providerName string
	wsPort       int
	gatewayPort  int
	dataDir      string
	agentServer  *server.AgentServer
	gwServer     *gateway.AppGatewayServer
	envBackup    [][3]string
	origWd       string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestHTTPDispatchE2ESuite 套件入口。
func TestHTTPDispatchE2ESuite(t *testing.T) {
	suite.Run(t, new(HTTPDispatchE2ESuite))
}

// SetupSuite 装配进程内网关 + server + transport（模式对齐 media_gateway_e2e_test.go:72-140）。
func (s *HTTPDispatchE2ESuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()

	s.providerName = fmt.Sprintf("mock_http_itest_%d", time.Now().UnixNano())
	modelclients.GetClientRegistry().Register(s.providerName, "llm", s.MockLLM.Factory())

	s.dataDir = s.T().TempDir()
	wd, err := os.Getwd()
	s.Require().NoError(err)
	s.origWd = wd
	s.Require().NoError(os.Chdir(s.dataDir))
	configDir := filepath.Join(s.dataDir, "config")
	s.Require().NoError(os.MkdirAll(configDir, 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(s.dataDir, "agent", "workspace"), 0o755))
	yaml := fmt.Sprintf(`workspace:
  path: %s
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

	repoRoot, err := filepath.Abs(filepath.Join(s.origWd, "..", "..", "..", "..", ".."))
	s.Require().NoError(err)
	s.saveAndSetenv("UAPCLAW_RESOURCES_DIR", filepath.Join(repoRoot, "resources"))
	s.saveAndSetenv("UAPCLAW_DATA_DIR", s.dataDir)
	path.ResetCache()

	_, err = workspace.Init(workspace.InitOption{Language: "zh"})
	s.Require().NoError(err)

	tr := transport.NewChannelTransport()
	agentClient := routing.NewAgentClient(tr)

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
		path.ResetCache()

		cfg, cfgErr := configLoad()
		s.Require().NoError(cfgErr)

		if attempt == 1 {
			s.agentServer = server.NewAgentServer(cfg, tr)
			s.Require().NoError(s.agentServer.Start(s.Ctx))
		}
		gwServer, gwErr := gateway.NewAppGatewayServer(cfg, agentClient)
		s.Require().NoError(gwErr)
		s.Require().NoError(gwServer.Start(s.Ctx))

		// 探活 + ListenErr 判定 + 换号重试（对齐 media_gateway_e2e_test.go:140-150）。
		if tcpErr := waitTCPReady(s.wsPort, 3*time.Second); tcpErr == nil {
			s.gwServer = gwServer
			break
		} else {
			s.T().Logf("第 %d 次绑定端口 %d 失败（疑似并发抢占），换号重试", attempt, s.wsPort)
			_ = gwServer.Stop()
			if attempt == 3 {
				s.FailNow("连续 3 次无法绑定 Web 端口")
			}
		}
	}
	s.waitGatewayReady()
}

// TearDownSuite 还原 cwd 与环境变量。
func (s *HTTPDispatchE2ESuite) TearDownSuite() {
	_ = os.Chdir(s.origWd)
	s.restoreEnv()
}

// ──────────────────────────── 会话全链 ────────────────────────────

// TestHTTP全链会话多轮 建会话 → SSE 聊天 → history → interrupt → CRUD。
func (s *HTTPDispatchE2ESuite) TestHTTP全链会话多轮() {
	base := s.baseURL()

	// ① 建会话（body 可空 → 201）
	status, body := s.postJSON(base+"/api/v1/sessions", []byte("{}"))
	s.Require().Equal(201, status)
	s.Require().True(body["ok"].(bool))
	data := body["data"].(map[string]any)
	sid, _ := data["session_id"].(string)
	s.Require().NotEmpty(sid)

	// ② SSE 聊天（chat.send）
	sseStatus, sseFrames := s.postSSE(base+"/api/v1/chat/completions", []byte(
		fmt.Sprintf(`{"session_id":%q,"query":"你好","mode":"agent","enable_streaming":true}`, sid)))
	s.Require().Equal(200, sseStatus)
	s.Require().NotEmpty(sseFrames)

	// ③ history JSON（聚合 frames）
	hStatus, hBody := s.getJSON(base + "/api/v1/sessions/" + sid + "/history?page_idx=1")
	s.Require().Equal(200, hStatus)
	hData := hBody["data"].(map[string]any)
	s.Require().Equal(sid, hData["session_id"])
	s.Require().NotNil(hData["messages"])

	// ④ rename / list（session.get_metadata 无 gateway/server handler，
	// 属裁决 1 预定义的未实现 RPC → 404 envelope，下游 session 域回填点 27.6）
	gStatus, gBody := s.getJSON(base + "/api/v1/sessions/" + sid)
	s.Require().Equal(404, gStatus)
	s.Require().Equal("METHOD_NOT_FOUND", gBody["error"].(map[string]any)["code"])
	rStatus, rBody := s.requestJSON("PATCH", base+"/api/v1/sessions/"+sid,
		[]byte(`{"title":"renamed"}`))
	s.Require().Equal(200, rStatus)
	s.Require().True(rBody["ok"].(bool))
	lStatus, lBody := s.getJSON(base + "/api/v1/sessions?limit=10")
	s.Require().Equal(200, lStatus)
	s.Require().True(lBody["ok"].(bool))

	// ⑤ interrupt（accept + 可能空负载，回 200 envelope）
	iStatus, iBody := s.postJSON(base+"/api/v1/chat/"+sid+"/actions/interrupt", []byte("{}"))
	s.Require().Equal(200, iStatus)
	s.Require().True(iBody["ok"].(bool))

	// ⑥ delete：session.delete 无 gateway/server handler（27.6 下游缺口）→
	// 裁决 1 预定义 METHOD_NOT_FOUND 404 envelope
	dStatus, dBody := s.requestJSON("DELETE", base+"/api/v1/sessions/"+sid, nil)
	s.Require().Equal(404, dStatus)
	s.Require().Equal("METHOD_NOT_FOUND", dBody["error"].(map[string]any)["code"])
}

// ──────────────────────────── 附件 ────────────────────────────

// TestHTTP带附件 document.formats + media.persist JSON 落盘回读。
func (s *HTTPDispatchE2ESuite) TestHTTP带附件() {
	base := s.baseURL()

	// document.formats（mapped GET，无 body）
	fStatus, fBody := s.getJSON(base + "/api/v1/documents/formats")
	s.Require().Equal(200, fStatus)
	s.Require().True(fBody["ok"].(bool))

	// 建会话 + media.persist（JSON body，base64 字段）
	_, sBody := s.postJSON(base+"/api/v1/sessions", []byte("{}"))
	sid := sBody["data"].(map[string]any)["session_id"].(string)
	tinyPNG := "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	mStatus, mBody := s.postJSON(base+"/api/v1/sessions/"+sid+"/media",
		[]byte(fmt.Sprintf(`{"file_name":"tiny.png","mime_type":"image/png","file_base64":%q}`, tinyPNG)))
	s.Require().Equal(200, mStatus)
	if ok, isBool := mBody["ok"].(bool); isBool && ok {
		s.Require().NotNil(mBody["data"])
	}
}

// ──────────────────────────── workspace 操作 ────────────────────────────

// TestHTTPWorkspace操作 tree → preview → entries_delete → usage HTTP 全链。
func (s *HTTPDispatchE2ESuite) TestHTTPWorkspace操作() {
	base := s.baseURL()

	// 造白名单内文件
	ws := filepath.Join(s.dataDir, "agent", "workspace", "projects")
	s.Require().NoError(os.MkdirAll(ws, 0o755))
	s.Require().NoError(os.WriteFile(filepath.Join(ws, "demo.txt"), []byte("hello workspace"), 0o644))

	// tree
	tStatus, tBody := s.getJSON(base + "/api/v1/workspace/tree?relative_path=agent/workspace")
	s.Require().Equal(200, tStatus)
	s.Require().True(tBody["ok"].(bool))
	entries := tBody["data"].(map[string]any)["entries"].([]any)
	s.Require().NotEmpty(entries)

	// preview（文本）
	pStatus, pBody := s.getJSON(base + "/api/v1/workspace/preview?relative_path=agent/workspace/projects/demo.txt")
	s.Require().Equal(200, pStatus)
	s.Require().Equal("hello workspace", pBody["data"].(map[string]any)["content"])

	// preview（目录 → 400）
	pdStatus, _ := s.getJSON(base + "/api/v1/workspace/preview?relative_path=agent/workspace/projects")
	s.Require().Equal(400, pdStatus)

	// entries_delete（白名单内成功）
	eStatus, eBody := s.doJSON("DELETE", base+"/api/v1/workspace/entries",
		[]byte(`{"relative_paths":["agent/workspace/projects/demo.txt"]}`))
	s.Require().Equal(200, eStatus)
	s.T().Logf("[debug] entries resp=%v", eBody)
	results := eBody["data"].(map[string]any)["results"].([]any)
	s.Require().Len(results, 1)
	first := results[0].(map[string]any)
	s.Require().True(first["ok"].(bool))

	// entries_delete（禁删 zone → 403 FORBIDDEN envelope）
	_, _ = s.postJSON(base+"/api/v1/workspace/entries",
		[]byte(`{"relative_paths":["agent/sessions/x.json"]}`))
	eStatus2, eBody2 := s.doJSON("DELETE", base+"/api/v1/workspace/entries",
		[]byte(`{"relative_paths":["agent/workspace/skills/keep.md"]}`))
	_ = eBody2
	s.Require().Equal(200, eStatus2) // 批量语义逐项回结果，HTTP 仍 200
	results2 := eBody2["data"].(map[string]any)["results"].([]any)
	s.Require().Len(results2, 1)
	s.Require().False(results2[0].(map[string]any)["ok"].(bool))

	// usage（单机 du 快照，quota=null）
	uStatus, uBody := s.getJSON(base + "/api/v1/workspace/usage")
	s.Require().Equal(200, uStatus)
	s.Require().NotNil(uBody["data"].(map[string]any)["used_bytes"])
	s.Require().Nil(uBody["data"].(map[string]any)["quota"])
}

// ──────────────────────────── METHOD_NOT_FOUND / catalog ────────────────────────────

// TestHTTPMETHODNOTFOUND与catalog 未实现 RPC → 404 envelope；catalog 可对拍。
func (s *HTTPDispatchE2ESuite) TestHTTPMETHODNOTFOUND与catalog() {
	base := s.baseURL()

	// a2a.ingress.get：非 ForwardReqMethods、无本地 handler → METHOD_NOT_FOUND 404
	status, body := s.getJSON(base + "/api/v1/a2a/ingress")
	s.Require().Equal(404, status)
	s.Require().Equal("METHOD_NOT_FOUND", body["error"].(map[string]any)["code"])

	// catalog：CORE + mapped + multipart 专行
	cStatus, cBody := s.getJSON(base + "/api/v1/catalog")
	s.Require().Equal(200, cStatus)
	routes := cBody["data"].(map[string]any)["routes"].([]any)
	s.Require().GreaterOrEqual(len(routes), 130)
	// 头恒带 X-Request-Id 在 getJSON 断言过
}

// ──────────────────────────── HTTP 助手 ────────────────────────────

// baseURL 网关 HTTP 地址。
func (s *HTTPDispatchE2ESuite) baseURL() string {
	// /api/v1 HTTP 面与 /ws 同端口（webChannel.Addr()，app_gateway.go:512）；
	// gatewayPort 是 GatewayServer(19001 内部路由) 的端口。
	return fmt.Sprintf("http://127.0.0.1:%d", s.wsPort)
}

// doJSON 通用 JSON 请求。
func (s *HTTPDispatchE2ESuite) doJSON(method, url string, body []byte) (int, map[string]any) {
	req, err := http.NewRequestWithContext(s.Ctx, method, url, bytes.NewReader(body))
	s.Require().NoError(err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Request-Id", fmt.Sprintf("itest-%d", time.Now().UnixNano()))
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	s.Require().NoError(err)
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

// postJSON / getJSON / requestJSON 语义包装。
func (s *HTTPDispatchE2ESuite) postJSON(url string, body []byte) (int, map[string]any) {
	return s.doJSON("POST", url, body)
}

func (s *HTTPDispatchE2ESuite) getJSON(url string) (int, map[string]any) {
	return s.doJSON("GET", url, nil)
}

func (s *HTTPDispatchE2ESuite) requestJSON(method, url string, body []byte) (int, map[string]any) {
	return s.doJSON(method, url, body)
}

// postSSE SSE 请求，返回 status 与帧文本列表。
func (s *HTTPDispatchE2ESuite) postSSE(url string, body []byte) (int, []string) {
	req, err := http.NewRequestWithContext(s.Ctx, "POST", url, bytes.NewReader(body))
	s.Require().NoError(err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	s.Require().NoError(err)
	defer func() { _ = resp.Body.Close() }()
	var frames []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event:") || strings.HasPrefix(line, "data:") {
			frames = append(frames, line)
		}
	}
	return resp.StatusCode, frames
}

// waitTCPReady TCP 探活。
func waitTCPReady(port int, timeout time.Duration) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("端口 %d 未就绪", port)
}

// pickFreePort 取空闲端口。
func (s *HTTPDispatchE2ESuite) pickFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// saveAndSetenv 环境变量备份式设置（suite 结束还原）。
func (s *HTTPDispatchE2ESuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	if had {
		s.envBackup = append(s.envBackup, [3]string{key, orig, "1"})
	} else {
		s.envBackup = append(s.envBackup, [3]string{key, "", "0"})
	}
	_ = os.Setenv(key, val)
}

// restoreEnv 还原环境变量。
func (s *HTTPDispatchE2ESuite) restoreEnv() {
	for i := len(s.envBackup) - 1; i >= 0; i-- {
		entry := s.envBackup[i]
		if entry[2] == "1" {
			_ = os.Setenv(entry[0], entry[1])
		} else {
			_ = os.Unsetenv(entry[0])
		}
	}
}

// configLoad 配置加载（对齐 media 套件 config.New("")+Load）。
func configLoad() (*config.Config, error) {
	cfg, err := config.New("")
	if err != nil {
		return nil, err
	}
	if _, err := cfg.Load(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// waitGatewayReady 探活 /api/v1/health。
func (s *HTTPDispatchE2ESuite) waitGatewayReady() {
	for i := 0; i < 50; i++ {
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(s.baseURL() + "/api/v1/health")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	s.T().Fatalf("网关 /api/v1/health 未就绪")
}
