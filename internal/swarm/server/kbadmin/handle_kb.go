package kbadmin

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

// ErrCodeKBNotImplemented 方法待后续批次实现
const ErrCodeKBNotImplemented = "KB_NOT_IMPLEMENTED"

// ErrCodeKBNotFound 知识库不存在
const ErrCodeKBNotFound = "KB_NOT_FOUND"

// ErrCodeKBInternal 内部错误
const ErrCodeKBInternal = "KB_INTERNAL"

// ErrCodeKBInvalidParams 参数解析失败
const ErrCodeKBInvalidParams = "KB_CONFIG_INVALID"

// stallScanThreshold 停滞扫描阈值（spec §3：仅标记不自动取消）
const stallScanThreshold = 10 * time.Minute

// ──────────────────────────── 结构体 ────────────────────────────

// Domain kb_admin 域聚合（server 侧单点注入）
type Domain struct {
	// KB 知识库 CRUD 编排
	KB *KBService
	// Doc 文档管理编排（30.2 真实现：解析任务/重解析/取消/列表）
	Doc *DocService
	// Chunk 分块管理编排（30.6 回填）
	Chunk *ChunkService
	// Search 检索测试编排（30.6 回填）
	Search *SearchService
	// Worker 解析执行器（30.2）
	Worker *ParseWorker
	// Sink 进度观察者（kb.task 推送源）
	Sink *ProgressSink
	// subscribers kb.task.subscribe 注册表（channelID → 订阅中）
	subMu       sync.Mutex
	subscribers map[string]struct{}
	// push 推送函数（server 侧注入：事件 → WS 帧写出）
	push func(channelID string, event TaskEvent)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDomain 组装 kb_admin 域
func NewDomain(store *Store, kbFactory *RetrievalKBFactory) (*Domain, error) {
	kbSvc, err := NewKBService(store, kbFactory)
	if err != nil {
		return nil, err
	}
	d := &Domain{
		KB:          kbSvc,
		Doc:         NewDocService(store),
		Chunk:       NewChunkService(store),
		Search:      NewSearchService(store),
		subscribers: make(map[string]struct{}),
	}
	// 30.5 上传槽位表：落盘根目录置于 WorkspaceDir()/kb_uploads（既有
	// isPathWithinUserDirs 边界内，file.upload_chunk 复用协议可直接写）；
	// TTL 清理挂后台周期任务（泄漏防护）
	uploads := NewUploadStore(filepath.Join(path.WorkspaceDir(), "kb_uploads"))
	d.Doc.SetUploadStore(uploads)
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			uploads.CleanupExpired()
		}
	}()
	// 30.2 解析任务链装配：worker 池 + 进度观察者 + 任务提交/取消接线 +
	// 停滞扫描周期任务（spec §2）
	d.Sink = NewProgressSink(store, d.fanoutTaskEvent)
	d.Worker = NewParseWorker(store, kbFactory, 2)
	d.Worker.SetSink(d.Sink)
	d.Doc.SetEnqueue(d.Worker.Enqueue)
	d.Doc.SetCancelFn(d.Worker.CancelTask)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			d.Sink.ScanStalled(stallScanThreshold)
		}
	}()
	d.Worker.Start()
	return d, nil
}

// Stop 停机（测试与优雅退出用）：worker 停止 + 释放工厂缓存的 chroma client。
// 域重建前必须先停旧域——chroma PersistentClient 同路径双实例并发会锁竞争/
// 读可见性抖动（30.2 集成 flake 根因），单停 worker 不够
func (d *Domain) Stop() {
	d.Worker.Stop()
	d.KB.CloseIndexers()
}

// SetPushFunc 注入推送函数（server 侧接线：事件 → WS 帧写出）
func (d *Domain) SetPushFunc(fn func(channelID string, event TaskEvent)) { d.push = fn }

// fanoutTaskEvent 事件扇出：逐订阅 channel 经 push 发出（无订阅即丢弃）
func (d *Domain) fanoutTaskEvent(event TaskEvent) {
	d.subMu.Lock()
	targets := make([]string, 0, len(d.subscribers))
	for ch := range d.subscribers {
		targets = append(targets, ch)
	}
	d.subMu.Unlock()
	for _, ch := range targets {
		if d.push != nil {
			d.push(ch, event)
		}
	}
}

// IsKBMethod 判定是否 kb.* 方法族
func IsKBMethod(method string) bool { return strings.HasPrefix(method, "kb.") }

// BootstrapErrorResponse 域装配失败的统一错误响应（server 薄适配层使用）
func BootstrapErrorResponse(request *schema.AgentRequest) *schema.AgentResponse {
	return errorResponse(request, "kb_admin 域装配失败", ErrCodeKBInternal)
}

// HandleKBRequest kb.* 方法族统一分发（30.1 注册全族；未实现返 KB_NOT_IMPLEMENTED）。
// 对齐 repositories.HandleProjectRequest 写法：返回 *schema.AgentResponse，
// 失败经 OK=false + payload.error_code 表达；由 server 侧薄适配写回。
func HandleKBRequest(domain *Domain, request *schema.AgentRequest) *schema.AgentResponse {
	notImplemented := func() *schema.AgentResponse {
		return errorResponse(request, "方法将在后续批次实现", ErrCodeKBNotImplemented)
	}
	switch request.ReqMethod {
	case schema.ReqMethodKBKBList:
		list, err := domain.KB.List()
		if err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInternal)
		}
		return okResponse(request, map[string]any{"kbs": mustJSONAny(list)})
	case schema.ReqMethodKBKBGet:
		var p struct {
			ID string `json:"id"`
		}
		if err := parseParams(request, &p); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInvalidParams)
		}
		kb, err := domain.KB.Get(p.ID)
		if err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBNotFound)
		}
		return okResponse(request, map[string]any{"kb": mustJSONValue(kb)})
	case schema.ReqMethodKBKBCreate:
		var p struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Type       string `json:"type"`
			ConfigJSON string `json:"config_json"`
			FolderPath string `json:"folder_path"`
		}
		if err := parseParams(request, &p); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInvalidParams)
		}
		if p.Type == "" {
			p.Type = "document"
		}
		kb, err := domain.KB.Create(context.Background(), p.ID, p.Name, p.Type, p.ConfigJSON, p.FolderPath)
		if err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInternal)
		}
		return okResponse(request, map[string]any{"kb": mustJSONValue(kb)})
	case schema.ReqMethodKBKBUpdate:
		var kb KBInfo
		if err := parseParams(request, &kb); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInvalidParams)
		}
		updated, err := domain.KB.Update(&kb)
		if err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInternal)
		}
		return okResponse(request, map[string]any{"kb": mustJSONValue(updated)})
	case schema.ReqMethodKBKBDelete:
		var p struct {
			ID string `json:"id"`
		}
		if err := parseParams(request, &p); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInvalidParams)
		}
		if err := domain.KB.Delete(context.Background(), p.ID); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBNotFound)
		}
		return okResponse(request, map[string]any{"deleted": true})
	// ── 上传分帧通道（30.5；X 边界：落盘成功不触发解析，⤵️30.2 加挂点）──
	case schema.ReqMethodKBDocUpload:
		var p struct {
			KBID     string `json:"kb_id"`
			FileName string `json:"file_name"`
			Size     int64  `json:"size"`
			FileHash string `json:"file_hash"`
		}
		if err := parseParams(request, &p); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInvalidParams)
		}
		slot, err := domain.Doc.InitUpload(p.KBID, p.FileName, p.Size, p.FileHash)
		if err != nil {
			return errorResponse(request, err.Error(), uploadErrCode(err))
		}
		return okResponse(request, map[string]any{"upload": mustJSONValue(slot)})
	case schema.ReqMethodKBFileUploadComplete:
		var p struct {
			UploadID string `json:"upload_id"`
		}
		if err := parseParams(request, &p); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInvalidParams)
		}
		done, err := domain.Doc.CompleteUpload(p.UploadID)
		if err != nil {
			return errorResponse(request, err.Error(), uploadErrCode(err))
		}
		return okResponse(request, map[string]any{"file": mustJSONValue(done)})
	case schema.ReqMethodKBDocCancelUpload:
		var p struct {
			UploadID string `json:"upload_id"`
		}
		if err := parseParams(request, &p); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInvalidParams)
		}
		if err := domain.Doc.CancelUpload(p.UploadID); err != nil {
			return errorResponse(request, err.Error(), uploadErrCode(err))
		}
		return okResponse(request, map[string]any{"cancelled": true})
	case schema.ReqMethodKBDocDownload:
		var p struct {
			Path   string `json:"path"`
			Offset int    `json:"offset"`
			Limit  int    `json:"limit"`
		}
		if err := parseParams(request, &p); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInvalidParams)
		}
		data, eof, err := domain.Doc.DownloadChunk(p.Path, p.Offset, p.Limit)
		if err != nil {
			return errorResponse(request, err.Error(), uploadErrCode(err))
		}
		return okResponse(request, map[string]any{
			"data_base64": base64.StdEncoding.EncodeToString(data),
			"offset":      p.Offset,
			"eof":         eof,
		})
	// ── 文档管理面（30.2 摘除）──
	case schema.ReqMethodKBDocList:
		var p struct {
			KBID   string `json:"kb_id"`
			Status string `json:"status"`
		}
		if err := parseParams(request, &p); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInvalidParams)
		}
		docs, err := domain.Doc.List(p.KBID, p.Status)
		if err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInternal)
		}
		return okResponse(request, map[string]any{"docs": mustJSONAny(docs)})
	case schema.ReqMethodKBDocGet:
		var p struct {
			ID string `json:"id"`
		}
		if err := parseParams(request, &p); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInvalidParams)
		}
		doc, err := domain.Doc.Get(p.ID)
		if err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBNotFound)
		}
		return okResponse(request, map[string]any{"doc": mustJSONValue(doc)})
	case schema.ReqMethodKBDocReparse:
		var p struct {
			DocID string `json:"doc_id"`
		}
		if err := parseParams(request, &p); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInvalidParams)
		}
		task, err := domain.Doc.Reparse(p.DocID)
		if err != nil {
			return errorResponse(request, err.Error(), docErrCode(err))
		}
		return okResponse(request, map[string]any{"task": mustJSONValue(task)})
	case schema.ReqMethodKBDocCancelParse:
		var p struct {
			TaskID string `json:"task_id"`
		}
		if err := parseParams(request, &p); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInvalidParams)
		}
		if err := domain.Doc.CancelParse(p.TaskID); err != nil {
			return errorResponse(request, err.Error(), docErrCode(err))
		}
		return okResponse(request, map[string]any{"cancelled": true})
	// ── 解析任务通信面（30.2 全量：spec §5）──
	case schema.ReqMethodKBTaskProgress:
		var p struct {
			TaskIDs []string `json:"task_ids"`
			KBID    string   `json:"kb_id"`
		}
		if err := parseParams(request, &p); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInvalidParams)
		}
		tasks, err := domain.TaskProgress(p.TaskIDs, p.KBID)
		if err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInternal)
		}
		return okResponse(request, map[string]any{"tasks": mustJSONAny(tasks)})
	case schema.ReqMethodKBTaskSubscribe:
		var p struct {
			ChannelID string `json:"channel_id"`
		}
		if err := parseParams(request, &p); err != nil {
			return errorResponse(request, err.Error(), ErrCodeKBInvalidParams)
		}
		domain.subscribeChannel(p.ChannelID)
		return okResponse(request, map[string]any{"subscribed": true})
	// 其余方法族：30.1 只注册不实现（spec D-30-1-2，30.6 各批摘除；
	// 30.2 已摘除 kb.doc.list/get/reparse/cancel_parse 与 kb.task.progress/subscribe）
	case schema.ReqMethodKBKBPin, schema.ReqMethodKBKBMove,
		schema.ReqMethodKBKBSetConfig, schema.ReqMethodKBKBStatistics,
		schema.ReqMethodKBKBGenerateProfile,
		schema.ReqMethodKBDocImportURL, schema.ReqMethodKBDocImportManual,
		schema.ReqMethodKBDocUpdate,
		schema.ReqMethodKBDocDelete, schema.ReqMethodKBDocBatchDelete,
		schema.ReqMethodKBDocBatchReparse,
		schema.ReqMethodKBDocRegenerateSummary,
		schema.ReqMethodKBDocPreview,
		schema.ReqMethodKBChunkList, schema.ReqMethodKBChunkUpdate,
		schema.ReqMethodKBChunkRevisions, schema.ReqMethodKBChunkRevert,
		schema.ReqMethodKBChunkDelete, schema.ReqMethodKBChunkGenerateQuestions,
		schema.ReqMethodKBChunkListQuestions, schema.ReqMethodKBChunkerPreview,
		schema.ReqMethodKBSearchTest:
		return notImplemented()
	default:
		return errorResponse(request, "未知的 kb 方法", ErrCodeKBInternal)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// uploadErrCode 上传链错误哨兵 → 错误码（spec §5 错误码族）
func uploadErrCode(err error) string {
	switch {
	case errors.Is(err, ErrKBNotFound):
		return ErrCodeKBNotFound
	case errors.Is(err, ErrFileTooLarge):
		return "FILE_TOO_LARGE"
	case errors.Is(err, ErrKBUploadNotFound):
		return "KB_UPLOAD_NOT_FOUND"
	case errors.Is(err, ErrHashMismatch):
		return "UPLOAD_HASH_MISMATCH"
	default:
		return ErrCodeKBInternal
	}
}

// docErrCode 文档/解析链错误哨兵 → 错误码（spec §5 错误码族）
func docErrCode(err error) string {
	switch {
	case errors.Is(err, ErrDocNotFound), errors.Is(err, ErrTaskNotFound):
		return ErrCodeKBNotFound
	case errors.Is(err, ErrDocDuplicate):
		return "DOC_DUPLICATE"
	case errors.Is(err, ErrDocParseActive):
		return "TASK_ACTIVE"
	default:
		return ErrCodeKBInternal
	}
}

// okResponse 成功响应（对齐 repositories.buildErrorResponse 的工厂用法）
func okResponse(request *schema.AgentRequest, payload map[string]any) *schema.AgentResponse {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(true),
		schema.WithPayload(payload),
	)
}

// errorResponse 失败响应（OK=false + payload.error_code）
func errorResponse(request *schema.AgentRequest, message, code string) *schema.AgentResponse {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(false),
		schema.WithPayload(map[string]any{"error": message, "error_code": code}),
	)
}

// parseParams 解析请求参数（AgentRequest.Params JSON）
func parseParams(request *schema.AgentRequest, target any) error {
	if len(request.Params) == 0 {
		return nil
	}
	return json.Unmarshal(request.Params, target)
}

// mustJSONAny 把模型转 map（单个对象）；json tag 已定义
func mustJSONValue(v any) map[string]any {
	out, _ := mustJSONAny(v).(map[string]any)
	if out == nil {
		return map[string]any{}
	}
	return out
}

// mustJSONAny 把任意模型经 JSON 往返转 payload 值（对象或数组）
func mustJSONAny(v any) any {
	data, err := json.Marshal(v)
	if err != nil {
		return map[string]any{}
	}
	var out any
	_ = json.Unmarshal(data, &out)
	return out
}
