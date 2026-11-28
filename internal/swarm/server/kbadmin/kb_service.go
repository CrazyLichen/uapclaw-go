package kbadmin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/indexing/indexer"
	vs "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/vector_store"
)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件
const logComponent = logger.ComponentAgentServer

// ──────────────────────────── 结构体 ────────────────────────────

// KBService 知识库 CRUD 编排（30.1 方案 C：DAO 全真 + create/delete
// 最小编排 SimpleKB 先行；graph/MultiKB 差异 ⤵️30.2 回填，spec D-30-1-1）
type KBService struct {
	// store kbadmin SQLite 存储
	store *Store
	// kbFactory collection 生命周期接线（具体类型直调，用户拍板：禁止为
	// 未发生的未来需求预留窄接口）
	kbFactory *RetrievalKBFactory
}

// RetrievalKBFactory collection/indexer 生命周期接线（spec §9 provider 分派）：
// 按 KB config store.provider 分派 chroma（默认）/ sqlite / milvus
type RetrievalKBFactory struct {
	// dataDir 各 KB collection 持久化根目录（<dataDir>/<kbID>/chroma 或 kbvector.db）
	dataDir string
	// mu stores/configs 并发保护
	mu sync.Mutex
	// stores kbID → 已建向量库（delete 时取用；chroma/sqlite 两 provider 登记）
	stores map[string]vs.VectorStore
	// configs kbID → KB config 解析结果（D-30-1-4 收口：configJSON 消费登记，
	// worker 管线装配读取）
	configs map[string]KBConfig
	// indexers kbID → 缓存的 indexer（单实例复用：chroma PersistentClient
	// 反复新建存在跨实例读可见性抖动，实测 155108 dedup 误报；单实例消除）
	indexers map[string]indexer.Indexer
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewKBService 构造 KBService
func NewKBService(store *Store, kbFactory *RetrievalKBFactory) (*KBService, error) {
	return &KBService{store: store, kbFactory: kbFactory}, nil
}

// NewRetrievalKBFactory 构造 collection 生命周期接线（dataDir 为空时 CreateCollection 必然失败，
// 供回滚用例与调用方校验）
func NewRetrievalKBFactory(dataDir string) *RetrievalKBFactory {
	return &RetrievalKBFactory{dataDir: dataDir, stores: make(map[string]vs.VectorStore),
		configs: make(map[string]KBConfig), indexers: make(map[string]indexer.Indexer)}
}

// CreateCollection 为 KB 创建向量库 collection（按 KB config store.provider 分派，
// spec §9；缺省 chroma）。configJSON 消费收口（D-30-1-4）：解析登记到 configs，
// 供 worker 管线装配读取。
func (f *RetrievalKBFactory) CreateCollection(_ context.Context, kbID, configJSON string) error {
	if f.dataDir == "" {
		err := fmt.Errorf("kb 数据目录未配置")
		logger.Error(logComponent).Str("kb_id", kbID).Err(err).
			Msg("kb collection creation failed")
		return err
	}
	cfg := ParseKBConfig(configJSON)
	// sqlite provider 不自建父目录（chroma 客户端自建），统一先建
	if err := os.MkdirAll(filepath.Join(f.dataDir, kbID), 0o755); err != nil {
		logger.Error(logComponent).Str("kb_id", kbID).Err(err).
			Msg("kb collection dir creation failed")
		return fmt.Errorf("创建 kb collection 目录失败: %w", err)
	}
	vsConfig := common.VectorStoreConfig{
		CollectionName: "kb_" + kbID,
		DistanceMetric: common.DistanceMetricCosine,
	}
	var store vs.VectorStore
	var err error
	switch cfg.Store.Provider {
	case "", "chroma":
		// chroma 域扩展实现（spec §10）：ChromaVectorStore + Bleve 倒排组合——
		// 向量半边原样 chroma，稀疏半边由 Bleve BM25 接管（替代伪 EF 路径）
		vsConfig.StoreProvider = common.StoreTypeChroma
		vsConfig.DatabaseName = "kb_" + kbID
		store, err = vs.NewChromaBleveStore(vsConfig,
			filepath.Join(f.dataDir, kbID, "chroma"),
			filepath.Join(f.dataDir, kbID, "bleve"),
			"content", vector_fields.NewDefaultChromaVectorField(),
			"sparse_vector", "metadata", "document_id",
		)
	case "sqlite":
		// D-25-7 私有 provider：FTS5 bm25 + sqlite-vec，本地零依赖真混合检索
		vsConfig.StoreProvider = common.StoreTypeSqlite
		vsConfig.DatabaseName = "kb_" + kbID
		store, err = vs.NewSQLiteVectorStore(vsConfig, filepath.Join(f.dataDir, kbID, "kbvector.db"),
			"content", "vector", "sparse_vector", "metadata", "document_id")
	case "milvus":
		// milvus collection 由 MilvusIndexer 建表时创建（13.3 路径），无独立构造步骤
		vsConfig.StoreProvider = common.StoreTypeMilvus
		vsConfig.DatabaseName = "kb_" + kbID
		store = nil
	default:
		err = fmt.Errorf("未知存储 provider: %s（支持 chroma/sqlite/milvus）", cfg.Store.Provider)
	}
	if err != nil {
		logger.Error(logComponent).Str("kb_id", kbID).Str("provider", cfg.Store.Provider).Err(err).
			Msg("kb collection creation failed")
		return fmt.Errorf("创建 kb collection 失败: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stores[kbID] = store
	f.configs[kbID] = cfg
	return nil
}

// Config 读取已登记的 KB config 解析结果（未登记返回 false）
func (f *RetrievalKBFactory) Config(kbID string) (KBConfig, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cfg, ok := f.configs[kbID]
	return cfg, ok
}

// CreateIndexer 为已建 collection 构造 indexer（chunk/triple 索引共用；按 KB
// config store.provider 分派，spec §9；缺省 chroma）。实例按 kbID 缓存复用
// （并发安全）：反复新建 chroma PersistentClient 存在读可见性抖动（155108
// dedup 误报实测），单实例消除；调用方不再各自 Close。
func (f *RetrievalKBFactory) CreateIndexer(_ context.Context, kbID string) (indexer.Indexer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if idx, ok := f.indexers[kbID]; ok {
		return idx, nil
	}
	cfg := f.configs[kbID]
	// 未登记 config（如域重建后直接 reparse）回退 chroma 默认
	provider := cfg.Store.Provider
	// sqlite provider 不自建父目录，统一先建
	if err := os.MkdirAll(filepath.Join(f.dataDir, kbID), 0o755); err != nil {
		logger.Error(logComponent).Str("kb_id", kbID).Err(err).
			Msg("kb indexer dir creation failed")
		return nil, fmt.Errorf("创建 kb collection 目录失败: %w", err)
	}
	vsConfig := common.VectorStoreConfig{
		CollectionName: "kb_" + kbID,
		DistanceMetric: common.DistanceMetricCosine,
		// 与 BuildIndex 的 database_name extra 同源（kb_<kbID>）：client 建在
		// 正确 database 上，否则 DeleteIndex 在默认库中查不到 collection 恒
		// 返回 false（M-44 路径缺陷，30.2 update/delete 路径首次踩中）
		DatabaseName: "kb_" + kbID,
	}
	var idx indexer.Indexer
	var err error
	switch provider {
	case "", "chroma":
		// spec §10：chroma 路径走组合 store 单实例（chroma 向量 + Bleve 倒排），
		// 入库/检索共享同一 chroma client（消除双 client 读可见性抖动根因）
		vsConfig.StoreProvider = common.StoreTypeChroma
		inner, ok := f.stores[kbID].(*vs.ChromaBleveStore)
		if !ok || inner == nil {
			// 未登记（域重建后直接 reparse）：构造完整组合 store（与 CreateCollection 同构）
			inner, err = vs.NewChromaBleveStore(vsConfig,
				filepath.Join(f.dataDir, kbID, "chroma"),
				filepath.Join(f.dataDir, kbID, "bleve"),
				"content", vector_fields.NewDefaultChromaVectorField(),
				"sparse_vector", "metadata", "document_id",
			)
			if err != nil {
				break
			}
			f.stores[kbID] = inner
		}
		idx = indexer.NewChromaBleveIndexer(inner)
	case "sqlite":
		vsConfig.StoreProvider = common.StoreTypeSqlite
		idx, err = indexer.NewSQLiteIndexer(vsConfig, filepath.Join(f.dataDir, kbID, "kbvector.db"))
	case "milvus":
		vsConfig.StoreProvider = common.StoreTypeMilvus
		idx, err = indexer.NewMilvusIndexer(vsConfig, cfg.Store.MilvusURI, cfg.Store.MilvusToken)
	default:
		err = fmt.Errorf("未知存储 provider: %s（支持 chroma/sqlite/milvus）", provider)
	}
	if err != nil {
		logger.Error(logComponent).Str("kb_id", kbID).Str("provider", provider).Err(err).
			Msg("kb indexer construction failed")
		return nil, err
	}
	f.indexers[kbID] = idx
	return idx, nil
}

// DeleteCollection 删除 KB 的向量库 collection（DeleteTable 模板方法路径）；
// 同步关闭并逐出缓存的 indexer 实例
func (f *RetrievalKBFactory) DeleteCollection(ctx context.Context, kbID string) error {
	f.mu.Lock()
	store, ok := f.stores[kbID]
	delete(f.stores, kbID)
	delete(f.configs, kbID)
	idx, hasIdx := f.indexers[kbID]
	delete(f.indexers, kbID)
	f.mu.Unlock()
	if hasIdx {
		_ = idx.Close()
	}
	if !ok {
		return nil // 幂等：未登记视为已删
	}
	// milvus provider 无本地 store 登记（collection 由 milvus 服务端管理），
	// 本地 DeleteTable 不适用——远端 collection 清理随 milvus 侧生命周期
	if store == nil {
		return nil
	}
	if err := store.DeleteTable(ctx, "kb_"+kbID); err != nil {
		logger.Error(logComponent).Str("kb_id", kbID).Err(err).
			Msg("kb collection deletion failed")
		return fmt.Errorf("删除 kb collection 失败: %w", err)
	}
	return nil
}

// Has 登记/存在判定
func (f *RetrievalKBFactory) Has(kbID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.stores[kbID]
	return ok
}

// CloseAll 关闭并逐出全部缓存的 indexer（域停机/重建清理）。
// chroma PersistentClient 对同一路径多实例并发存在锁竞争与读可见性抖动
// （30.2 集成 flake 根因：域重建后旧域 client 未释放，新旧双实例打架），
// 域重建前必须先经本方法释放旧域持有的全部 client
func (f *RetrievalKBFactory) CloseAll() {
	f.mu.Lock()
	idxs := make(map[string]indexer.Indexer, len(f.indexers))
	stores := make(map[string]vs.VectorStore, len(f.stores))
	for kbID, idx := range f.indexers {
		idxs[kbID] = idx
		delete(f.indexers, kbID)
	}
	for kbID, st := range f.stores {
		stores[kbID] = st
		delete(f.stores, kbID)
		delete(f.configs, kbID)
	}
	f.mu.Unlock()
	for kbID, idx := range idxs {
		if err := idx.Close(); err != nil {
			logger.Error(logComponent).Str("kb_id", kbID).Err(err).
				Msg("kb indexer close failed")
		}
	}
	// 组合 store 的 bleve 索引句柄随 store 关闭（chroma inner Close 为空操作惯例）
	for kbID, st := range stores {
		if err := st.Close(); err != nil {
			logger.Error(logComponent).Str("kb_id", kbID).Err(err).
				Msg("kb store close failed")
		}
	}
}

// Create 创建知识库：类型校验 → DAO 落行 → collection 创建（向量库失败则 DAO 回滚）。
// 类型编排（D-30-1-1 收口）：document/graph 建向量 collection；multi 为聚合配置
// 只落库不建 collection（config 须可解析 JSON）；其余类型拒单
func (s *KBService) Create(ctx context.Context, id, name, typ, configJSON, folderPath string) (*KBInfo, error) {
	now := time.Now().Unix()
	if id == "" {
		id = uuid.NewString()
	}
	switch typ {
	case "document", "graph":
	case "multi":
		var probe map[string]any
		if err := json.Unmarshal([]byte(configJSON), &probe); err != nil {
			return nil, fmt.Errorf("multi 聚合配置不可解析: %w", err)
		}
	default:
		return nil, fmt.Errorf("不支持的库类型: %s", typ)
	}
	kb := &KBInfo{ID: id, Name: name, Type: typ, ConfigJSON: configJSON,
		FolderPath: folderPath, CreatedAt: now, UpdatedAt: now}
	if err := s.store.KB().Create(kb); err != nil {
		return nil, err
	}
	if typ != "multi" {
		if err := s.kbFactory.CreateCollection(ctx, id, configJSON); err != nil {
			_ = s.store.KB().Delete(id) // 回滚
			return nil, err
		}
	}
	return kb, nil
}

// Get 查询知识库（纯 DAO）
func (s *KBService) Get(id string) (*KBInfo, error) { return s.store.KB().Get(id) }

// CloseIndexers 释放工厂缓存的全部 chroma client（Domain.Stop 消费）
func (s *KBService) CloseIndexers() { s.kbFactory.CloseAll() }

// List 列表（纯 DAO）
func (s *KBService) List() ([]KBInfo, error) { return s.store.KB().List() }

// Update 更新（纯 DAO）
func (s *KBService) Update(kb *KBInfo) (*KBInfo, error) {
	kb.UpdatedAt = time.Now().Unix()
	if err := s.store.KB().Update(kb); err != nil {
		return nil, err
	}
	return kb, nil
}

// Delete 删除知识库：MultiKB 引用检查 → 先删 collection 再删 DAO 行（固定顺序，spec §7 一致性）
func (s *KBService) Delete(ctx context.Context, id string) error {
	if err := s.checkMultiReferences(id); err != nil {
		return err
	}
	if err := s.kbFactory.DeleteCollection(ctx, id); err != nil {
		return err
	}
	return s.store.KB().Delete(id)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// checkMultiReferences MultiKB 级联引用检查：任一聚合库 config 的 sub_kb_ids
// 含目标 ID 即拒绝删除（引用解除后可删——先删聚合库）
func (s *KBService) checkMultiReferences(id string) error {
	kbs, err := s.store.KB().List()
	if err != nil {
		return err
	}
	for _, kb := range kbs {
		if kb.Type != "multi" {
			continue
		}
		var cfg struct {
			SubKBIDs []string `json:"sub_kb_ids"`
		}
		if json.Unmarshal([]byte(kb.ConfigJSON), &cfg) != nil {
			continue
		}
		for _, sub := range cfg.SubKBIDs {
			if sub == id {
				return fmt.Errorf("知识库被聚合库 %s 引用，先解除引用再删除", kb.ID)
			}
		}
	}
	return nil
}
