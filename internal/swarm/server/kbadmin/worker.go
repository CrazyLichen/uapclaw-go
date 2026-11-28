package kbadmin

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	fembedding "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/embedding"
	rembedding "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/embedding"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/indexing/processor/extractor"
)

// ParseWorker 固定池解析执行器（spec §2 方案A：SQLite kb_task 台账为唯一真相源 +
// channel 唤醒空闲 worker + 启动期轮询恢复）
type ParseWorker struct {
	// store kbadmin SQLite 台账
	store *Store
	// kbFactory collection/indexer 构造接线
	factory *RetrievalKBFactory
	// sink 进度观察者
	sink *ProgressSink
	// tripleExtractor graph 类型抽取器（nil 跳过 triple 索引）
	tripleExtractor extractor.Extractor
	// size worker 池并发度
	size int
	// wake 任务 ID 唤醒通道（缓冲=池大小，防提交方阻塞）
	wake chan string
	// cancels taskID → context.CancelFunc（在途任务取消注册表）
	cancels sync.Map
	// stopOnce 优雅停机单次保护
	stopOnce sync.Once
	// stopCh 停机信号
	stopCh chan struct{}
	// wg worker goroutine 组
	wg sync.WaitGroup
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewParseWorker 构造（size 并发度）
func NewParseWorker(store *Store, factory *RetrievalKBFactory, size int) *ParseWorker {
	if size <= 0 {
		size = 1
	}
	return &ParseWorker{store: store, factory: factory, size: size,
		wake: make(chan string, size), stopCh: make(chan struct{})}
}

// SetSink 注入观察者（Domain 组装期；含 publisher）
func (w *ParseWorker) SetSink(sink *ProgressSink) { w.sink = sink }

// SetTripleExtractor 注入 graph 类型抽取器（triple 步骤消费；nil 跳过 triple 索引）
func (w *ParseWorker) SetTripleExtractor(e extractor.Extractor) { w.tripleExtractor = e }

// Enqueue 提交唤醒（非阻塞：缓冲满则丢弃——启动恢复期 recoverOrphans 会重捞兜底）
func (w *ParseWorker) Enqueue(taskID string) {
	select {
	case w.wake <- taskID:
	default:
	}
}

// Start 启动：孤儿恢复 + N 个常驻 worker
func (w *ParseWorker) Start() {
	w.recoverOrphans()
	for i := 0; i < w.size; i++ {
		w.wg.Add(1)
		go w.loop()
	}
}

// Stop 优雅停机（测试与进程退出用）
func (w *ParseWorker) Stop() {
	w.stopOnce.Do(func() { close(w.stopCh) })
	w.wg.Wait()
}

// CancelTask 取消在途任务（cancelParse 编排调用）；任务不在途返回 false
func (w *ParseWorker) CancelTask(taskID string) bool {
	v, ok := w.cancels.Load(taskID)
	if !ok {
		return false
	}
	v.(context.CancelFunc)()
	return true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// recoverOrphans 启动轮询：processing → cancelled(server_restart)（只认确切信号，
// 不推断中途进度——videotask 结算纪律）；pending → 重新入队
func (w *ParseWorker) recoverOrphans() {
	orphans, err := w.store.Task().ListByStatus("processing")
	if err == nil {
		for i := range orphans {
			t := &orphans[i]
			t.Status = "cancelled"
			t.CancelReason = "server_restart"
			t.FinishedAt = time.Now().Unix()
			if uerr := w.store.Task().Update(t); uerr != nil {
				logger.Error(logComponent).Str("task_id", t.ID).Err(uerr).
					Msg("orphan task recovery failed")
				continue
			}
			w.syncDocStatus(t.DocID, "cancelled", "")
		}
	}
	pending, err := w.store.Task().ListByStatus("pending")
	if err == nil {
		for i := range pending {
			w.Enqueue(pending[i].ID)
		}
	}
}

// loop worker 主循环
func (w *ParseWorker) loop() {
	defer w.wg.Done()
	for {
		select {
		case <-w.stopCh:
			return
		case taskID := <-w.wake:
			w.runOne(taskID)
		}
	}
}

// claimTask CAS 单认领：UPDATE kb_task SET status='processing', started_at=?
// WHERE id=? AND status='pending'——影响行数=1 才算认领成功（双 worker 竞争安全）
func (w *ParseWorker) claimTask(taskID string) bool {
	res := w.store.DB().Model(&KBTask{}).
		Where("id = ? AND status = ?", taskID, "pending").
		Updates(map[string]any{"status": "processing", "started_at": time.Now().Unix()})
	return res.Error == nil && res.RowsAffected == 1
}

// runOne 单任务执行：认领 → 装配 env → runPipeline → 终态落库
func (w *ParseWorker) runOne(taskID string) {
	if !w.claimTask(taskID) {
		return // 他 worker 已认领或任务已取消
	}
	task, err := w.store.Task().Get(taskID)
	if err != nil {
		return
	}
	doc, err := w.store.Doc().Get(task.DocID)
	if err != nil {
		w.finishTask(task, "failed", err.Error())
		return
	}
	kb, err := w.store.KB().Get(task.KBID)
	if err != nil {
		w.finishTask(task, "failed", err.Error())
		return
	}
	job := &ParseJob{TaskID: task.ID, DocID: doc.ID, KBID: task.KBID, KBType: kb.Type,
		Config: ParseKBConfig(kb.ConfigJSON)}
	var meta struct {
		FinalPath string `json:"final_path"`
	}
	_ = json.Unmarshal([]byte(doc.MetaJSON), &meta)
	job.FilePath = meta.FinalPath

	ctx, cancel := context.WithCancel(context.Background())
	w.cancels.Store(taskID, cancel)
	defer func() {
		cancel()
		w.cancels.Delete(taskID)
	}()

	steps, ok := pipelinePlans[job.KBType]
	if !ok {
		w.finishTask(task, "failed", "未知库类型: "+job.KBType)
		w.syncDocStatus(doc.ID, "failed", "未知库类型")
		return
	}
	env := &pipelineEnv{store: w.store, kbFactory: w.factory,
		embedModel: w.buildEmbedModel(job.Config), sink: w.sink,
		tripleExtractor: w.tripleExtractor}
	if err := runPipeline(ctx, job, steps, env); err != nil {
		// 取消判定以 ctx 确切信号为准（组件可能把 cancel 包装成业务错误如
		// 155108，errors.Is 链不保证透传——只认 ctx.Err）
		if ctx.Err() != nil || errors.Is(err, context.Canceled) {
			// 取消回滚（spec §3）：本任务已写向量库 chunks 先删、台账再删（向量库先行固定顺序）
			w.rollbackDoc(job, ctx)
			w.finishTask(task, "cancelled", "")
			w.syncDocStatus(doc.ID, "cancelled", "")
			return
		}
		w.finishTask(task, "failed", err.Error())
		w.syncDocStatus(doc.ID, "failed", err.Error())
		return
	}
	w.finishTask(task, "completed", "")
	w.syncDocStatus(doc.ID, "completed", "")
}

// finishTask 终态落库 + 状态事件
func (w *ParseWorker) finishTask(task *KBTask, status, errMsg string) {
	task.Status = status
	task.FinishedAt = time.Now().Unix()
	if err := w.store.Task().Update(task); err != nil {
		logger.Error(logComponent).Str("task_id", task.ID).Err(err).
			Msg("task finish update failed")
	}
	if w.sink != nil {
		w.sink.EmitStatus(&ParseJob{TaskID: task.ID, DocID: task.DocID, KBID: task.KBID}, status, errMsg)
	}
}

// syncDocStatus 文档状态跟随任务终态
func (w *ParseWorker) syncDocStatus(docID, status, errMsg string) {
	doc, err := w.store.Doc().Get(docID)
	if err != nil {
		return
	}
	doc.ParseStatus = status
	doc.UpdatedAt = time.Now().Unix()
	if errMsg != "" {
		doc.ErrorMsg = errMsg
	}
	_ = w.store.Doc().Update(doc)
}

// rollbackDoc 取消回滚：向量库按 docID 删本任务 chunks → 台账 chunk 行删除
// （固定顺序：向量库先行，spec §3；向量库删除失败仅记日志不阻断——终态仍落 cancelled）
func (w *ParseWorker) rollbackDoc(job *ParseJob, ctx context.Context) {
	idx, err := w.factory.CreateIndexer(ctx, job.KBID)
	if err == nil {
		if _, derr := idx.DeleteIndex(ctx, job.DocID, "kb_"+job.KBID+"_chunks"); derr != nil {
			logger.Error(logComponent).Str("doc_id", job.DocID).Err(derr).
				Msg("rollback vector index failed")
		}
		_ = idx.Close()
	} else {
		logger.Error(logComponent).Str("kb_id", job.KBID).Err(err).
			Msg("rollback indexer construction failed")
	}
	metas, err := w.store.Chunk().ListByDoc(job.DocID)
	if err == nil {
		for _, m := range metas {
			_ = w.store.Chunk().DeleteMeta(m.ID)
		}
	}
}

// buildEmbedModel 按 KB config 构造 embedding 客户端（D-30-1-4 消费端；
// provider 缺省 openai 兼容协议；retrieval/embedding 实现满足 foundation BaseEmbedding）
func (w *ParseWorker) buildEmbedModel(cfg KBConfig) fembedding.BaseEmbedding {
	ecfg := rembedding.EmbeddingConfig{
		ModelName: cfg.Embedding.ModelName,
		BaseURL:   cfg.Embedding.BaseURL,
		APIKey:    cfg.Embedding.APIKey,
	}
	return rembedding.NewOpenAIEmbedding(ecfg)
}
