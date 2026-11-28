//go:build integration

// Package kbadmin 30.2 解析任务链集成测试。
//
// 跨子系统：swarm/server（E2A 信封分发 + kb.* 前缀分支 + 推送桥）+
// swarm/server/kbadmin（上传挂点 + 解析任务 + worker 池 + kb_task 台账）+
// core/retrieval（AutoFileParser/TextChunker/ChromaIndexer 真链）+
// core/openuapclaw/foundation/store（embedding HTTP 端点）。真实链路：
// kb.doc.upload 分帧落盘 → upload_complete 挂点建任务 → worker 真解析 →
// kb.task.progress 拉全量 + kb.task.subscribe 推送帧到达 → 终态/重解析/取消/
// 孤儿恢复/上限可调全链覆盖。embedding 用 httptest 本地端点（网络边界替身）。
// 禁替身：不 mock 被测子系统。
package kbadmin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	kbadminserver "github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/kbadmin"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// KBParsePipelineSuite 30.2 解析任务链全链集成（上行 E2A → worker 真链 → 下行推送）。
type KBParsePipelineSuite struct {
	isuite.BaseIntegrationSuite
	env *testhelpers.ServerEnv
	// embedURL httptest embedding 端点（OpenAI 兼容协议）
	embedURL string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestKBParsePipelineSuite 套件入口。
func TestKBParsePipelineSuite(t *testing.T) {
	suite.Run(t, new(KBParsePipelineSuite))
}

// SetupSuite 隔离数据目录、启动 embedding 端点与 AgentServer、建宿主 KB。
func (s *KBParsePipelineSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
	// embedding 端点（网络边界替身）：按请求 input 数量回显固定向量
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

	s.T().Setenv(pathutil.EnvDataDir, s.T().TempDir())
	pathutil.ResetCache()
	kbadminserver.ResetKBDomainForTest()
	cfg, err := config.New("")
	s.Require().NoError(err)
	s.env = testhelpers.NewServerEnv(s.T(), cfg, nil)

	// 建宿主 KB（config 指向本地 embedding 端点）
	rid := s.env.Send("kb.kb.create", map[string]any{
		"id": "it-parse-kb", "name": "解析集成库", "type": "document",
		"config_json": fmt.Sprintf(`{"embedding":{"provider":"openai","model_name":"m","base_url":"%s","api_key":"k"}}`, s.embedURL),
	}, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().True(resp.OK, "create 应成功: %v", resp.Payload)
}

// TearDownSuite 还原路径缓存与域单例。
func (s *KBParsePipelineSuite) TearDownSuite() {
	pathutil.ResetCache()
	kbadminserver.ResetKBDomainForTest()
}

// TestCreateUploadParse_全链 创建→分帧上传→挂点建任务→真解析→completed。
func (s *KBParsePipelineSuite) TestCreateUploadParse_全链() {
	data := []byte("kb parse pipeline integration payload.\n\nSecond paragraph for chunking to produce at least one chunk.")
	uploadID, _ := s.uploadFile("it-parse-kb", "payload.txt", data)

	// upload_complete 挂点建任务 → UploadedFile 收尾
	rid := s.env.Send("kb.file.upload_complete", map[string]any{"upload_id": uploadID}, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().True(resp.OK, "upload_complete 应成功: %v", resp.Payload)

	// 轮询 kb.task.progress 至 completed（重连兜底端点全量对齐）
	task := s.awaitKBStatusByKB("it-parse-kb", "completed", 30*time.Second)
	docID := task["doc_id"].(string)

	// 文档状态对齐
	rid = s.env.Send("kb.doc.get", map[string]any{"id": docID}, "", false)
	resp = s.env.AwaitUnary(rid)
	s.Require().True(resp.OK)
	doc := resp.Payload["doc"].(map[string]any)
	s.Require().Equal("completed", doc["parse_status"])
	s.Require().Contains(doc["stages_json"], "parsing")
	s.Require().Contains(doc["stages_json"], "embedding")

	// chunk 台账对齐（真实 SQLite 直查 kbadmin.db）
	chunks := s.queryChunkRows(docID)
	s.Require().NotEmpty(chunks, "kb_chunk_meta 应有行")
}

// TestProgressSubscribe_推送到达 subscribe 后解析进度经推送帧到达。
func (s *KBParsePipelineSuite) TestProgressSubscribe_推送到达() {
	rid := s.env.Send("kb.task.subscribe", map[string]any{"channel_id": s.env.ChannelID}, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().True(resp.OK)

	data := []byte("subscribe push integration payload")
	uploadID, _ := s.uploadFile("it-parse-kb", "push.txt", data)
	rid = s.env.Send("kb.file.upload_complete", map[string]any{"upload_id": uploadID}, "", false)
	s.env.AwaitUnary(rid)

	// 轮询推送帧直至出现 status=completed（push 帧 payload.kb_task_event）
	deadline := time.Now().Add(30 * time.Second)
	found := false
	for time.Now().Before(deadline) && !found {
		for _, p := range s.env.Pushes() {
			payload := testhelpers.PushPayload(p)
			if ev, ok := payload["kb_task_event"].(map[string]any); ok {
				if ev["phase"] == "status" && ev["status"] == "completed" {
					found = true
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !found {
		for i, p := range s.env.Pushes() {
			s.T().Logf("push[%d]: %v", i, p)
		}
	}
	s.Require().True(found, "应收到 status=completed 推送帧")
}

// TestReparse_新任务行 重解析建新任务并再走全链。
func (s *KBParsePipelineSuite) TestReparse_新任务行() {
	data := []byte("reparse integration payload")
	uploadID, _ := s.uploadFile("it-parse-kb", "reparse.txt", data)
	rid := s.env.Send("kb.file.upload_complete", map[string]any{"upload_id": uploadID}, "", false)
	s.env.AwaitUnary(rid)
	// 取最新任务行（progress DESC 首行 = 本次上传的任务，避免命中早期完成任务）
	task := s.latestTaskByKB("it-parse-kb")
	s.awaitTaskStatus(task["id"].(string), "completed", 30*time.Second)

	rid = s.env.Send("kb.doc.reparse", map[string]any{"doc_id": task["doc_id"]}, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().True(resp.OK, "reparse 应成功: %v", resp.Payload)
	newTask := resp.Payload["task"].(map[string]any)
	s.Require().Equal("pending", newTask["status"])
	s.Require().NotEqual(task["id"], newTask["id"], "reparse 应建新任务行")
	s.T().Logf("reparse old=%v new=%v", task["id"], newTask["id"])
	s.awaitTaskStatus(newTask["id"].(string), "completed", 30*time.Second)
}

// TestCancelParse_在途取消 处理中任务经 cancel 后落 cancelled。
func (s *KBParsePipelineSuite) TestCancelParse_在途取消() {
	data := []byte("cancel integration payload")
	uploadID, _ := s.uploadFile("it-parse-kb", "cancel.txt", data)
	rid := s.env.Send("kb.file.upload_complete", map[string]any{"upload_id": uploadID}, "", false)
	s.env.AwaitUnary(rid)
	task := s.latestTaskByKB("it-parse-kb")
	s.awaitTaskStatus(task["id"].(string), "processing", 10*time.Second)

	rid = s.env.Send("kb.doc.cancel_parse", map[string]any{"task_id": task["id"]}, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().True(resp.OK, "cancel_parse 应成功: %v", resp.Payload)
	s.awaitTaskStatus(task["id"].(string), "cancelled", 30*time.Second)
}

// TestUploadLimitConfig_上限可调 KB config max_file_mb=1 → 2MB 文件 FILE_TOO_LARGE。
func (s *KBParsePipelineSuite) TestUploadLimitConfig_上限可调() {
	// 建小上限 KB
	rid := s.env.Send("kb.kb.create", map[string]any{
		"id": "it-limit-kb", "name": "小上限库", "type": "document",
		"config_json": `{"max_file_mb":1}`,
	}, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().True(resp.OK)

	big := make([]byte, 2*1024*1024)
	hash := kbSha256Hex(big)
	rid = s.env.Send("kb.doc.upload", map[string]any{
		"kb_id": "it-limit-kb", "file_name": "big.bin",
		"size": len(big), "file_hash": hash,
	}, "", false)
	resp = s.env.AwaitUnary(rid)
	s.Require().False(resp.OK)
	s.Require().Equal("FILE_TOO_LARGE", resp.Payload["error_code"])
}

// TestOrphanRecovery_孤儿恢复 预置 processing 任务行 → 域重建 → 标 cancelled(server_restart)。
func (s *KBParsePipelineSuite) TestOrphanRecovery_孤儿恢复() {
	// 直写台账预置孤儿 processing 行（真实 SQLite）
	store := s.openKbadminStore()
	now := time.Now().Unix()
	s.Require().NoError(store.DB().Exec(
		`INSERT INTO kb_task (id, doc_id, kb_id, status, progress, created_at, started_at, finished_at)
		 VALUES ('it-orphan-task', 'it-orphan-doc', 'it-parse-kb', 'processing', 50, ?, ?, 0)`,
		now, now).Error)
	s.Require().NoError(store.DB().Exec(
		`INSERT INTO kb_doc (id, kb_id, file_name, parse_status, last_activity_at, stall_state, created_at, updated_at)
		 VALUES ('it-orphan-doc', 'it-parse-kb', 'orphan.txt', 'processing', ?, 'normal', ?, ?)`,
		now, now, now).Error)

	// 域重建（进程重启模拟）：懒装配重置后首个请求重建域 → worker recoverOrphans
	kbadminserver.ResetKBDomainForTest()
	rid := s.env.Send("kb.task.progress", map[string]any{"task_ids": []string{"it-orphan-task"}}, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().True(resp.OK, "progress 应成功: %v", resp.Payload)
	tasks := resp.Payload["tasks"].([]any)
	s.Require().NotEmpty(tasks)
	t0 := tasks[0].(map[string]any)
	s.Require().Equal("cancelled", t0["status"])
	s.Require().Equal("server_restart", t0["cancel_reason"])
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// uploadFile 分帧上传一文件到指定 KB（复用 30.5 协议映射形态），返回 upload_id 与落盘路径。
func (s *KBParsePipelineSuite) uploadFile(kbID, fileName string, data []byte) (string, string) {
	s.T().Helper()
	hash := kbSha256Hex(data)
	rid := s.env.Send("kb.doc.upload", map[string]any{
		"kb_id": kbID, "file_name": fileName,
		"size": len(data), "file_hash": hash,
	}, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().True(resp.OK, "upload 发起失败: %v", resp.Payload)
	upload := resp.Payload["upload"].(map[string]any)
	uploadID := upload["upload_id"].(string)
	resolved := upload["resolved_path"].(string)

	const frameSize = 32
	for off := 0; off < len(data); off += frameSize {
		end := off + frameSize
		if end > len(data) {
			end = len(data)
		}
		final := end >= len(data)
		rid = s.env.Send("file.upload_chunk", map[string]any{
			"upload_id": uploadID, "resolved_path": resolved,
			"target_rel_path": "kb_parse_tmp/" + fileName,
			"data":            kbB64(data[off:end]), "final": final,
		}, "", false)
		resp = s.env.AwaitUnary(rid)
		s.Require().True(resp.OK, "分帧 %d 落盘失败: %v", off, resp.Payload)
	}
	return uploadID, resolved
}

// awaitKBStatusByKB 轮询 kb.task.progress（按 kb_id）直至出现目标状态，返回该任务视图。
func (s *KBParsePipelineSuite) awaitKBStatusByKB(kbID, status string, timeout time.Duration) map[string]any {
	s.T().Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		rid := s.env.Send("kb.task.progress", map[string]any{"kb_id": kbID}, "", false)
		resp := s.env.AwaitUnary(rid)
		if resp.OK {
			if tasks, ok := resp.Payload["tasks"].([]any); ok {
				for _, t := range tasks {
					tm := t.(map[string]any)
					if tm["status"] == status {
						return tm
					}
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.T().Fatalf("任务未在 %v 内出现状态 %s（kb_id=%s）", timeout, status, kbID)
	return nil
}

// latestTaskByKB 取 kb 下最新创建的任务行（progress DESC 首行）。
func (s *KBParsePipelineSuite) latestTaskByKB(kbID string) map[string]any {
	s.T().Helper()
	rid := s.env.Send("kb.task.progress", map[string]any{"kb_id": kbID}, "", false)
	resp := s.env.AwaitUnary(rid)
	s.Require().True(resp.OK, "%v", resp.Payload)
	tasks := resp.Payload["tasks"].([]any)
	s.Require().NotEmpty(tasks)
	for _, t := range tasks {
		tm := t.(map[string]any)
		// 跳过孤儿恢复预置行（it-orphan 前缀哨兵）
		if id, _ := tm["id"].(string); len(id) >= 9 && id[:9] == "it-orphan" {
			continue
		}
		return tm
	}
	s.T().Fatalf("kb %s 无非哨兵任务行", kbID)
	return nil
}

// awaitTaskStatus 按 task_id 轮询至目标状态。
func (s *KBParsePipelineSuite) awaitTaskStatus(taskID, status string, timeout time.Duration) {
	s.T().Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		rid := s.env.Send("kb.task.progress", map[string]any{"task_ids": []string{taskID}}, "", false)
		resp := s.env.AwaitUnary(rid)
		if resp.OK {
			if tasks, ok := resp.Payload["tasks"].([]any); ok && len(tasks) > 0 {
				if tasks[0].(map[string]any)["status"] == status {
					return
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.dumpAllTasks()
	s.T().Fatalf("任务 %s 未在 %v 内进入 %s（最后状态见上方 dump）", taskID, timeout, status)
}

// dumpAllTasks 失败诊断：dump 全部任务行
func (s *KBParsePipelineSuite) dumpAllTasks() {
	store, err := kbadmin.OpenStore(filepath.Join(pathutil.WorkspaceDir(), "kbadmin", "kbadmin.db"))
	if err != nil {
		s.T().Logf("open store: %v", err)
		return
	}
	defer func() { _ = store.Close() }()
	var rows []map[string]any
	store.DB().Table("kb_task").Find(&rows)
	s.T().Logf("dump db=%s tasks=%d", filepath.Join(pathutil.WorkspaceDir(), "kbadmin", "kbadmin.db"), len(rows))
	for _, r := range rows {
		s.T().Logf("task %v doc=%v status=%v", r["id"], r["doc_id"], r["status"])
	}
	var docs []map[string]any
	store.DB().Table("kb_doc").Find(&docs)
	for _, d := range docs {
		s.T().Logf("doc %v id=%v status=%v err=%v stages=%v", d["file_name"], d["id"], d["parse_status"], d["error_msg"], d["stages_json"])
	}
}

// openKbadminDB 打开真实 kbadmin.db（WorkspaceDir/kbadmin/kbadmin.db），
// 返回既有 OpenStore 句柄（GORM），供孤儿行预置与台账直查。
func (s *KBParsePipelineSuite) openKbadminStore() *kbadmin.Store {
	s.T().Helper()
	store, err := kbadmin.OpenStore(filepath.Join(pathutil.WorkspaceDir(), "kbadmin", "kbadmin.db"))
	s.Require().NoError(err)
	s.T().Cleanup(func() { _ = store.Close() })
	return store
}

// queryChunkRows 直查 chunk 台账行数（真实 SQLite）
func (s *KBParsePipelineSuite) queryChunkRows(docID string) int64 {
	s.T().Helper()
	store := s.openKbadminStore()
	var n int64
	s.Require().NoError(store.DB().Table("kb_chunk_meta").Where("doc_id = ?", docID).Count(&n).Error)
	return n
}
