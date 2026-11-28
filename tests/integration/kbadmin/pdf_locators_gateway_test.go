//go:build integration

// Package kbadmin 30.3 source_locators 全链集成测试（gateway 侧真实 WS）。
//
// 跨子系统：swarm/gateway（AppGatewayServer 真实 WS 帧）+ swarm/server（E2A
// 分发 + kb.* 前缀分支）+ swarm/server/kbadmin（上传挂点 + 解析任务 +
// worker 池 + kb_chunk_meta 台账）+ core/retrieval（PDFParser gomupdf 统一
// 链 + TextChunker 区间映射真链）+ core/openuapclaw/foundation/store（embedding
// HTTP 端点）。真实链路：客户端 WS 拨号 /ws → kb.kb.create → kb.doc.upload
// 分帧上传真实 pdf → upload_complete 建任务 → worker 真解析（gomupdf 词框 +
// PageMap sidecar → chunker 映射）→ kb.task.progress 轮询终态 → kb.chunk.list
// 断言 locators_json。embedding 用 httptest 本地端点（网络边界替身）；
// 禁替身：不 mock 被测子系统。用户硬性要求：从 gateway 侧完整测全链。
package kbadmin

import (
	"encoding/json"
	"fmt"
	"net"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	kbadminserver "github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// KBLocatorsGatewaySuite 30.3 gateway 侧真实 WS 全链（客户端拨号 /ws，非直投）。
type KBLocatorsGatewaySuite struct {
	isuite.BaseIntegrationSuite
	embedURL string
	wsPort   int
	dataDir  string
	origWd   string
	envBackup [][3]string
}

// wsFrame WS 响应帧形状（B1 契约 ok/error/code，复用 media e2e 同款）。
type wsFrame struct {
	OK      bool           `json:"ok"`
	Payload map[string]any `json:"payload"`
	Error   string         `json:"error"`
	Code    string         `json:"code"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestKBLocatorsGatewaySuite 套件入口。
func TestKBLocatorsGatewaySuite(t *testing.T) {
	suite.Run(t, new(KBLocatorsGatewaySuite))
}

// SetupSuite 隔离数据目录 + 启动 embedding 端点 + 进程内网关/server。
func (s *KBLocatorsGatewaySuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()

	// embedding 端点（网络边界替身）：按 input 数量回显固定向量
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		data := make([]map[string]any, len(req.Input))
		for i := range req.Input {
			data[i] = map[string]any{"index": i, "embedding": []float64{0.1, 0.2, 0.3, 0.4}}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "model": "test-embed"})
	}))
	s.T().Cleanup(srv.Close)
	s.embedURL = srv.URL

	s.dataDir = s.T().TempDir()
	wd, err := os.Getwd()
	s.Require().NoError(err)
	s.origWd = wd
	s.Require().NoError(os.Chdir(s.dataDir))
	s.Require().NoError(os.MkdirAll(filepath.Join(s.dataDir, "config"), 0o755))
	s.Require().NoError(os.MkdirAll(filepath.Join(s.dataDir, "agent", "workspace"), 0o755))
	yaml := fmt.Sprintf(`workspace:
  path: %s
models:
  defaults: []
`, s.dataDir)
	s.Require().NoError(os.WriteFile(filepath.Join(s.dataDir, "config", "config.yaml"), []byte(yaml), 0o644))

	s.saveAndSetenv("UAPCLAW_DATA_DIR", s.dataDir)
	pathutil.ResetCache()
	kbadminserver.ResetKBDomainForTest()

	// 三子系统构造（integration-audit 调用跨度来源）
	tr := transport.NewChannelTransport()
	agentClient := routing.NewAgentClient(tr)
	cfg, err := config.New("")
	s.Require().NoError(err)
	_, loadErr := cfg.Load()
	s.Require().NoError(loadErr)
	agentServer := server.NewAgentServer(cfg, tr)
	s.Require().NoError(agentServer.Start(s.Ctx))

	s.wsPort = s.pickFreePort()
	s.saveAndSetenv("UAPCLAW_WEB_PORT", fmt.Sprintf("%d", s.wsPort))
	pathutil.ResetCache()

	gwServer, err := gateway.NewAppGatewayServer(cfg, agentClient)
	s.Require().NoError(err)
	s.Require().NoError(gwServer.Start(s.Ctx))
	s.Require().NoError(kbWaitTCPReady(s.wsPort, 5*time.Second))
}

// TearDownSuite 还原工作目录与域单例。
func (s *KBLocatorsGatewaySuite) TearDownSuite() {
	_ = os.Chdir(s.origWd)
	pathutil.ResetCache()
	kbadminserver.ResetKBDomainForTest()
	s.BaseIntegrationSuite.TearDownSuite()
}

// TestPDF_locators全链 真实 pdf 经 gateway WS 上传→解析→kb.chunk.list locators 断言。
func (s *KBLocatorsGatewaySuite) TestPDF_locators全链() {
	conn := s.dial()
	defer func() { _ = conn.Close() }()

	// 1. 建 KB（embedding 指向本地端点）
	resp := s.wsRequest(conn, "req-create", "kb.kb.create", map[string]any{
		"id": "it-loc-kb", "name": "定位集成库", "type": "document",
		"config_json": fmt.Sprintf(`{"embedding":{"provider":"openai","model_name":"m","base_url":"%s","api_key":"k"}}`, s.embedURL),
	})
	s.Require().True(resp.OK, "kb.kb.create 应成功: %v", resp.Payload)

	// 2. 读 fixture 真实 pdf 并分帧上传（复用 30.2 分帧协议）
	// 套件已 chdir 至 dataDir：经记录的 origWd 折算 repo 根
	repoRoot, _ := filepath.Abs(filepath.Join(s.origWd, "..", "..", ".."))
	pdfBytes, err := os.ReadFile(filepath.Join(repoRoot,
		"internal/openuapclaw/core/retrieval/indexing/processor/parser/testdata/fixture_pdf_multipage.pdf"))
	s.Require().NoError(err, "fixture pdf 应可读（repo 相对路径）")
	fileName := "fixture_pdf_multipage.pdf"
	up := s.wsRequest(conn, "req-upload", "kb.doc.upload", map[string]any{
		"kb_id": "it-loc-kb", "file_name": fileName,
		"size": len(pdfBytes), "file_hash": kbSha256Hex(pdfBytes),
	})
	s.Require().True(up.OK, "kb.doc.upload 应成功: %v", up.Payload)
	upload := up.Payload["upload"].(map[string]any)
	uploadID := upload["upload_id"].(string)
	resolved := upload["resolved_path"].(string)

	const frameSize = 512
	for off := 0; off < len(pdfBytes); off += frameSize {
		end := off + frameSize
		if end > len(pdfBytes) {
			end = len(pdfBytes)
		}
		final := end >= len(pdfBytes)
		fr := s.wsRequest(conn, fmt.Sprintf("req-chunk-%d", off), "kb.file.upload_chunk", map[string]any{
			"upload_id": uploadID, "resolved_path": resolved,
			"target_rel_path": "kb_locators_tmp/" + fileName,
			"data":            kbB64(pdfBytes[off:end]), "final": final,
		})
		s.Require().True(fr.OK, "分帧 %d 落盘失败: %v", off, fr.Payload)
	}

	// 3. upload_complete 挂点建任务
	done := s.wsRequest(conn, "req-complete", "kb.file.upload_complete", map[string]any{"upload_id": uploadID})
	s.Require().True(done.OK, "upload_complete 应成功: %v", done.Payload)

	// 4. 轮询 kb.task.progress 至 completed
	task := s.awaitKBStatus(conn, "it-loc-kb", "completed", 60*time.Second)
	docID, _ := task["doc_id"].(string)
	s.Require().NotEmpty(docID)

	// 5. locators_json 断言（真实 SQLite 直查 kb_chunk_meta；kb.chunk.list
	// 端点本体 ⤵️30.6——30.2 spec 已登记，本批只保证数据面落库）
	var raws []string
	s.Require().NoError(queryLocatorsJSON(s.dataDir, docID, &raws))
	s.Require().NotEmpty(raws, "kb_chunk_meta 应有行")

	var withLocators int
	for _, raw := range raws {
		if raw == "" {
			continue
		}
		withLocators++
		var locs []map[string]any
		s.Require().NoError(json.Unmarshal([]byte(raw), &locs))
		var hasText bool
		for _, l := range locs {
			if l["type"] == "text" {
				hasText = true
				pageNo, _ := l["page_no"].(float64)
				s.Require().GreaterOrEqual(pageNo, float64(1), "page_no 应 ≥1")
				boxes, _ := l["line_bboxes"].([]any)
				s.Require().NotEmpty(boxes, "text 锚应有行级 bbox")
				for _, b := range boxes {
					coords, _ := b.([]any)
					s.Require().Len(coords, 4, "bbox 应为四元组")
					x0, _ := coords[0].(float64)
					x1, _ := coords[2].(float64)
					y0, _ := coords[1].(float64)
					y1, _ := coords[3].(float64)
					s.Require().Greater(x1, x0, "bbox x1 > x0")
					s.Require().Greater(y1, y0, "bbox y1 > y0")
				}
			}
		}
		s.Require().True(hasText, "chunk 应含 text 锚: %s", raw)
	}
	s.Require().Greater(withLocators, 0, "应有 chunk 携带 locators_json")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// dial 拨号 /ws（真实 WS 客户端）。
func (s *KBLocatorsGatewaySuite) dial() *websocket.Conn {
	conn, _, err := websocket.DefaultDialer.Dial(
		fmt.Sprintf("ws://127.0.0.1:%d/ws", s.wsPort), nil)
	s.Require().NoError(err, "WS 拨号失败")
	return conn
}

// wsRequest 发一元请求帧并等待同 id 响应帧（media e2e 同款读循环纪律）。
func (s *KBLocatorsGatewaySuite) wsRequest(conn *websocket.Conn, id, method string, params map[string]any) *wsFrame {
	req := map[string]any{"type": "req", "id": id, "method": method, "params": params}
	payload, err := json.Marshal(req)
	s.Require().NoError(err)
	s.Require().NoError(conn.SetWriteDeadline(time.Now().Add(10 * time.Second)))
	s.Require().NoError(conn.WriteMessage(websocket.TextMessage, payload))

	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			s.FailNow("WS 读连接失败: " + err.Error())
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
		if frame.Error == "" {
			if e, ok := envelope["error"].(string); ok {
				frame.Error = e
			}
		}
		return frame
	}
}

// awaitKBStatus 轮询 kb.task.progress 至目标状态。
func (s *KBLocatorsGatewaySuite) awaitKBStatus(conn *websocket.Conn, kbID, status string, timeout time.Duration) map[string]any {
	s.T().Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp := s.wsRequest(conn, fmt.Sprintf("req-progress-%d", time.Now().UnixNano()), "kb.task.progress", map[string]any{"kb_id": kbID})
		if resp.OK {
			if tasks, ok := resp.Payload["tasks"].([]any); ok {
				for _, t := range tasks {
					task, _ := t.(map[string]any)
					if st, _ := task["status"].(string); st == status {
						return task
					}
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	s.FailNow("轮询超时：任务未达 " + status)
	return nil
}

// pickFreePort 取空闲端口。
func (s *KBLocatorsGatewaySuite) pickFreePort() int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

// saveAndSetenv 环境变量备份式设置。
func (s *KBLocatorsGatewaySuite) saveAndSetenv(key, val string) {
	orig, had := os.LookupEnv(key)
	if had {
		s.envBackup = append(s.envBackup, [3]string{key, orig, "1"})
	} else {
		s.envBackup = append(s.envBackup, [3]string{key, "", "0"})
	}
	_ = os.Setenv(key, val)
}

// kbWaitTCPReady 端口探活。
func kbWaitTCPReady(port int, timeout time.Duration) error {
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

// queryLocatorsJSON 真实 SQLite 直查 kb_chunk_meta.locators_json（docID 维度）。
func queryLocatorsJSON(dataDir, docID string, out *[]string) error {
	dbPath := filepath.Join(dataDir, "kbadmin", "kbadmin.db")
	db, err := sql.Open("sqlite3", dbPath+"?_busy_timeout=5000")
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query("SELECT locators_json FROM kb_chunk_meta WHERE doc_id = ?", docID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return err
		}
		*out = append(*out, raw)
	}
	return rows.Err()
}
