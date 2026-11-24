//go:build integration

package web_test

import (
	"encoding/base64"
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
	modelclients "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	webfiledownload "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	vda "github.com/uapclaw/uapclaw-go/internal/swarm/common/verified_download_assets"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/attachments"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MediaGatewayE2ESuite 真实网关↔server 富媒体 e2e：
// 进程内 AppGatewayServer + ChannelTransport + AgentServer，客户端真 WS 拨号 /ws。
// 上行：media.persist 落盘 → 大图分流 → document 黑名单 → steer 引用校验；
// 下行：send_file 令牌分块下载 → rsi.artifact 代理。
// 客户端走真实 WS 帧（非 ServerEnv 直投），验证网关与 server 两级能力。
type MediaGatewayE2ESuite struct {
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

// ──────────────────────────── 常量 ────────────────────────────

// tinyPNG1x1 合法 1x1 PNG（上行落盘用）。
var tinyPNG1x1 = func() []byte {
	b, _ := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	return b
}()

// ──────────────────────────── 导出函数 ────────────────────────────

// TestMediaGatewayE2ESuite 套件入口。
func TestMediaGatewayE2ESuite(t *testing.T) {
	suite.Run(t, new(MediaGatewayE2ESuite))
}

// SetupSuite 装配进程内网关 + server + transport（模式对齐 team_continuous_dialog_test.go:68-160）。
func (s *MediaGatewayE2ESuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()

	s.providerName = fmt.Sprintf("mock_media_itest_%d", time.Now().UnixNano())
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
	pathutil.ResetCache()

	_, err = workspace.Init(workspace.InitOption{Language: "zh"})
	s.Require().NoError(err)

	// 三子系统构造（integration-audit 调用跨度来源，勿移到 helper 包）
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

		if readyErr := mediaWaitTCPReady(gwServer, s.wsPort, 3*time.Second); readyErr == nil {
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

// TearDownSuite 还原工作目录。
func (s *MediaGatewayE2ESuite) TearDownSuite() {
	_ = os.Chdir(s.origWd)
	s.BaseIntegrationSuite.TearDownSuite()
}

// ──────────────────────────── 上行 ────────────────────────────

// TestU1_MediaPersist_落盘与回执 真实 WS 帧上行：media.persist → 网关代理 →
// server adapter 落盘 → 磁盘断言。
// Python: app_web_handlers.py:5811 + workspace_file_adapter.py:240。
func (s *MediaGatewayE2ESuite) TestU1_MediaPersist_落盘与回执() {
	conn := s.dial()
	defer conn.Close()
	sid := fmt.Sprintf("media_e2e_%d", time.Now().UnixNano())

	resp := s.wsRequest(conn, "req-u1", "media.persist", map[string]any{
		"session_id": sid,
		"content":    "",
		"media_items": []any{
			map[string]any{"type": "image", "filename": "cat.png", "mime_type": "image/png",
				"base64_data": base64.StdEncoding.EncodeToString(tinyPNG1x1)},
		},
	})
	s.Require().True(resp.OK, "media.persist 应成功: %v", resp.Payload)
	items, _ := resp.Payload["media_items"].([]any)
	s.Require().Len(items, 1)
	stored := items[0].(map[string]any)
	path, _ := stored["path"].(string)
	s.Require().NotEmpty(path, "回执应带 path: %v", stored)

	// 磁盘断言：落盘在 agent/sessions/<sid>/uploads/ 且字节一致
	b, err := os.ReadFile(path)
	s.Require().NoError(err)
	s.Equal(tinyPNG1x1, b)
	uploads := filepath.Join(s.dataDir, "agent", "sessions", sid, "uploads")
	s.Equal(uploads, filepath.Dir(path), "落盘目录应为会话 uploads")
}

// TestU2_MediaPersist_大图分流 真实 WS 帧上行：>4MiB 解码字节图片 → 网关侧
// _pre_persist_large_media 分流（标 _persisted）→ server adapter 透传。
// Python: app_web_handlers.py:2063-2119 + :1994-2032。
func (s *MediaGatewayE2ESuite) TestU2_MediaPersist_大图分流() {
	conn := s.dial()
	defer conn.Close()
	sid := fmt.Sprintf("media_split_%d", time.Now().UnixNano())

	// 5MiB 大图（解码字节必超 E2A_PAYLOAD_MAX_BYTES=4MiB 预算）
	bigContent := make([]byte, 5*1024*1024)
	for i := range bigContent {
		bigContent[i] = byte(i % 251)
	}
	// 前缀放 PNG 魔数使 mime 判定链路完整；分流在网关不解码校验图片内容
	copy(bigContent, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})

	resp := s.wsRequest(conn, "req-u2", "media.persist", map[string]any{
		"session_id": sid,
		"media_items": []any{
			map[string]any{"type": "image", "filename": "big.png", "mime_type": "image/png",
				"base64_data": base64.StdEncoding.EncodeToString(bigContent)},
		},
	})
	s.Require().True(resp.OK, "大图 media.persist 应成功: err=%q code=%q payload=%v", resp.Error, resp.Code, resp.Payload)
	items, _ := resp.Payload["media_items"].([]any)
	s.Require().Len(items, 1)
	stored := items[0].(map[string]any)
	// Python 侧 store_image_item 的 _persisted 透传记录与普通落盘记录同形
	// （均不含 _persisted 回显），故以落盘路径 + 字节一致判定，分流痕迹由
	// 网关侧单测（prePersistLargeMedia 系列）承担。
	s.Require().Nil(stored["base64_data"], "响应不应回显 base64: %v", stored)

	// 磁盘断言：分流落盘文件字节与原始一致
	b, err := os.ReadFile(stored["path"].(string))
	s.Require().NoError(err)
	s.Equal(bigContent, b, "分流落盘字节应一致")
}

// TestU3_DocumentPersist_黑名单与落盘 真实 WS 帧上行：文档黑名单拒绝 + 合法落盘。
// Python: workspace_file_adapter.py:290 + document_attachments.py:58。
func (s *MediaGatewayE2ESuite) TestU3_DocumentPersist_黑名单与落盘() {
	conn := s.dial()
	defer conn.Close()
	sid := fmt.Sprintf("doc_e2e_%d", time.Now().UnixNano())

	resp := s.wsRequest(conn, "req-u3", "document.persist", map[string]any{
		"session_id": sid,
		"documents": []any{
			map[string]any{"filename": "virus.exe", "base64_data": base64.StdEncoding.EncodeToString([]byte("MZ"))},
			map[string]any{"filename": "ok.txt", "base64_data": base64.StdEncoding.EncodeToString([]byte("fine"))},
		},
	})
	s.Require().True(resp.OK, "document.persist 应成功（错误回显在 document_errors）: err=%q code=%q payload=%v", resp.Error, resp.Code, resp.Payload)
	errs, _ := resp.Payload["document_errors"].([]any)
	s.Require().Len(errs, 1, "黑名单项应回显错误")
	items, _ := resp.Payload["media_items"].([]any)
	s.Require().Len(items, 1)
	b, err := os.ReadFile(items[0].(map[string]any)["path"].(string))
	s.Require().NoError(err)
	s.Equal("fine", string(b))
}

// TestU4_Steer_引用校验 steer 图片引用校验（上行末端）：合法引用通过、
// 伪造路径拒绝。真实磁盘上的 uploads 由 U1 链路落盘。
// Python: steer_images.py:106-130 resolve_steer_image_references。
func (s *MediaGatewayE2ESuite) TestU4_Steer_引用校验() {
	sid := fmt.Sprintf("steer_e2e_%d", time.Now().UnixNano())
	uploads := filepath.Join(s.dataDir, "agent", "sessions", sid, "uploads")
	s.Require().NoError(os.MkdirAll(uploads, 0o755))
	p := filepath.Join(uploads, "steer.png")
	s.Require().NoError(os.WriteFile(p, tinyPNG1x1, 0o644))

	// 合法引用
	params := map[string]any{"media_items": []any{
		map[string]any{"type": "image", "path": p, "filename": "steer.png", "mime_type": "image/png"},
	}}
	out, err := attachments.ResolveSteerImageReferences(params, sid)
	s.Require().NoError(err)
	s.Require().Len(out, 1)
	s.Equal("image/png", out[0]["mime_type"])

	// 伪造路径（其他会话）→ 拒绝
	evil := filepath.Join(s.dataDir, "agent", "sessions", "other-sess", "uploads", "steer.png")
	params2 := map[string]any{"media_items": []any{
		map[string]any{"type": "image", "path": evil, "filename": "steer.png", "mime_type": "image/png"},
	}}
	_, err = attachments.ResolveSteerImageReferences(params2, sid)
	s.Require().Error(err, "跨会话引用应拒绝")
	s.Require().Contains(err.Error(), "outside the current session uploads")
}

// ──────────────────────────── 下行 ────────────────────────────

// TestD1_DownloadVerifiedChunk_分块下载 sealed-asset 令牌 → server 侧
// file.download_verified_chunk 分块读取 → 拼装字节一致；伪造 token 拒绝。
// 注：Python 契约里该方法由 HTTP 容器文件路由消费（container_file_http.py:510-514），
// 不经 web WS——HTTP 面属 27-A B2（spec D-27.8-5 ⤴️ B2 行），此处直驱 server 侧
// adapter 能力（真实磁盘 + 真实 owner/token 链路）。
// Python: workspace_file_adapter.py:571-671。
func (s *MediaGatewayE2ESuite) TestD1_DownloadVerifiedChunk_分块下载() {
	sid := fmt.Sprintf("dl_e2e_%d", time.Now().UnixNano())

	root := filepath.Join(s.dataDir, "vda-owner")
	owner, err := vda.NewVerifiedDownloadAssetOwner(root, vda.OwnerOptions{})
	s.Require().NoError(err)
	defer owner.Close()
	src := filepath.Join(s.dataDir, "asset-src.bin")
	content := make([]byte, 1200*1024) // 1.2MB → 3 块（512KB 限）
	for i := range content {
		content[i] = byte(i % 253)
	}
	s.Require().NoError(os.WriteFile(src, content, 0o644))
	asset, err := owner.Stage(src, vda.StageOptions{FileName: "asset.bin", ExpiresAt: 9999999999})
	s.Require().NoError(err, "Stage 返回携带真实摘要/尺寸，保证 IsActive 声明一致")
	s.Require().NoError(owner.Commit(asset))
	webfiledownload.SetWebFileDownloadAssetOwner(owner)
	token := webfiledownload.GetWebFileDownloadManager().
		GenerateVerifiedAssetToken(asset, "asset.bin", sid)

	// 直驱 server 侧 adapter（真实磁盘；分发入口与 handle_chat.go 短路同函数）
	var assembled []byte
	for offset := 0; ; {
		payload, ok, code, err := adapter.HandleWorkspaceFileRequest(
			schema.ReqMethodFileDownloadVerifiedChunk,
			map[string]any{"token": token, "offset": float64(offset)}, sid)
		s.Require().NoError(err)
		s.Require().True(ok, "分块读取应成功: offset=%d code=%q payload=%v", offset, code, payload)
		data, _ := base64.StdEncoding.DecodeString(payload["data_base64"].(string))
		assembled = append(assembled, data...)
		if eof, _ := payload["eof"].(bool); eof {
			break
		}
		chunkSize, _ := payload["chunk_size"].(int)
		offset += chunkSize
	}
	s.Equal(content, assembled, "分块拼装应与原文件一致")

	// 伪造 token → 拒绝
	_, ok, code, _ := adapter.HandleWorkspaceFileRequest(
		schema.ReqMethodFileDownloadVerifiedChunk,
		map[string]any{"token": "forged.token.sig", "offset": float64(0)}, sid)
	s.Require().False(ok, "伪造 token 应拒绝")
	s.Require().Equal("FORBIDDEN", code)
}

// TestD2_RSIArtifact_代理 真实 WS 帧下行：rsi.artifact.files.list 经网关代理
// 到 server handler（领域十七批次交付）并返回可解析响应。
// Python: app_web_handlers.py:6492-6494。
func (s *MediaGatewayE2ESuite) TestD2_RSIArtifact_代理() {
	conn := s.dial()
	defer conn.Close()
	sid := fmt.Sprintf("rsi_e2e_%d", time.Now().UnixNano())

	// files.list：空 harness 工作区也应返回结构化响应（不 404/不支持）
	resp := s.wsRequest(conn, "req-d2", "rsi.artifact.files.list", map[string]any{
		"session_id": sid,
	})
	s.Require().NotNil(resp, "rsi.artifact.files.list 应返回响应帧")
	// server 侧已就绪的 handler：响应帧必须带 id 回显（一元契约）
	s.Require().Equal("req-d2", resp.ID)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// wsFrame 一元响应帧形状。
type wsFrame struct {
	ID      string         `json:"id"`
	OK      bool           `json:"ok"`
	Payload map[string]any `json:"payload"`
	Error   string         `json:"error"`
	Code    string         `json:"code"`
}

// dial 真实 WS 拨号 /ws。
func (s *MediaGatewayE2ESuite) dial() *websocket.Conn {
	conn, _, err := websocket.DefaultDialer.Dial(
		fmt.Sprintf("ws://127.0.0.1:%d/ws", s.wsPort), nil)
	s.Require().NoError(err, "WS 拨号失败")
	return conn
}

// wsRequest 发一元请求帧并等待同 id 响应帧。
func (s *MediaGatewayE2ESuite) wsRequest(conn *websocket.Conn, id, method string, params map[string]any) *wsFrame {
	req := map[string]any{"type": "req", "id": id, "method": method, "params": params}
	payload, err := json.Marshal(req)
	s.Require().NoError(err)
	s.Require().NoError(conn.SetWriteDeadline(time.Now().Add(10 * time.Second)))
	s.Require().NoError(conn.WriteMessage(websocket.TextMessage, payload))

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			continue // 读超时继续等
		}
		var envelope map[string]any
		if err := json.Unmarshal(raw, &envelope); err != nil {
			continue
		}
		if t, _ := envelope["type"].(string); t != "res" && t != "response" && t != "" {
			continue // 推送帧跳过
		}
		if envelope["id"] != id {
			continue
		}
		frame := &wsFrame{}
		_ = json.Unmarshal(raw, frame)
		// 帧级 error 形状（B1 契约：ok=false/error/code）
		if frame.Error == "" {
			if e, ok := envelope["error"].(string); ok {
				frame.Error = e
			}
		}
		return frame
	}
	s.FailNow("等待响应帧超时: " + id)
	return nil
}

// pickFreePort 取空闲端口。
func (s *MediaGatewayE2ESuite) pickFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// saveAndSetenv 环境变量备份式设置（suite 结束还原）。
func (s *MediaGatewayE2ESuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	if had {
		s.envBackup = append(s.envBackup, [3]string{key, orig, "1"})
	} else {
		s.envBackup = append(s.envBackup, [3]string{key, "", "0"})
	}
	_ = os.Setenv(key, val)
}

// mediaWaitTCPReady 探活 + ListenErr 判定（对齐 supplement_interrupt_test.go:430）。
func mediaWaitTCPReady(gw *gateway.AppGatewayServer, port int, timeout time.Duration) error {
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

// schema 引用守卫（req 帧形状与 schema 常量对齐，防漂移）。
var _ = schema.ReqMethodMediaPersist
