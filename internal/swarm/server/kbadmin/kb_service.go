package kbadmin

import (
	"context"
	"fmt"
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

// RetrievalKBFactory collection 生命周期接线：retrieval 层 Chroma 本地持久化
// 向量库（构造期 GetOrCreateCollection 真建 collection，与 13.27 真链同款）
type RetrievalKBFactory struct {
	// dataDir 各 KB collection 持久化根目录（<dataDir>/<kbID>/chroma）
	dataDir string
	// mu stores/configs 并发保护
	mu sync.Mutex
	// stores kbID → 已建向量库（delete 时取用）
	stores map[string]*vs.ChromaVectorStore
	// configs kbID → KB config 解析结果（D-30-1-4 收口：configJSON 消费登记，
	// worker 管线装配读取）
	configs map[string]KBConfig
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewKBService 构造 KBService
func NewKBService(store *Store, kbFactory *RetrievalKBFactory) (*KBService, error) {
	return &KBService{store: store, kbFactory: kbFactory}, nil
}

// NewRetrievalKBFactory 构造 collection 生命周期接线（dataDir 为空时 CreateCollection 必然失败，
// 供回滚用例与调用方校验）
func NewRetrievalKBFactory(dataDir string) *RetrievalKBFactory {
	return &RetrievalKBFactory{dataDir: dataDir, stores: make(map[string]*vs.ChromaVectorStore),
		configs: make(map[string]KBConfig)}
}

// CreateCollection 为 KB 创建向量库 collection（Chroma 本地持久化，构造期即建）。
// configJSON 消费收口（D-30-1-4）：解析登记到 configs，供 worker 管线装配读取。
func (f *RetrievalKBFactory) CreateCollection(_ context.Context, kbID, configJSON string) error {
	if f.dataDir == "" {
		err := fmt.Errorf("kb 数据目录未配置")
		logger.Error(logComponent).Str("kb_id", kbID).Err(err).
			Msg("kb collection creation failed")
		return err
	}
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "kb_" + kbID,
		DistanceMetric: common.DistanceMetricCosine,
	}
	store, err := vs.NewChromaVectorStore(config, filepath.Join(f.dataDir, kbID, "chroma"),
		"content", vector_fields.NewDefaultChromaVectorField(),
		"sparse_vector", "metadata", "document_id",
	)
	if err != nil {
		logger.Error(logComponent).Str("kb_id", kbID).Err(err).
			Msg("kb collection creation failed")
		return fmt.Errorf("创建 kb collection 失败: %w", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stores[kbID] = store
	f.configs[kbID] = ParseKBConfig(configJSON)
	return nil
}

// Config 读取已登记的 KB config 解析结果（未登记返回 false）
func (f *RetrievalKBFactory) Config(kbID string) (KBConfig, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cfg, ok := f.configs[kbID]
	return cfg, ok
}

// CreateIndexer 为已建 collection 构造 ChromaIndexer（chunk/triple 索引共用；
// chromaPath 与 CreateCollection 同源：dataDir/<kbID>/chroma）
func (f *RetrievalKBFactory) CreateIndexer(_ context.Context, kbID string) (indexer.Indexer, error) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "kb_" + kbID,
		DistanceMetric: common.DistanceMetricCosine,
	}
	return indexer.NewChromaIndexer(config, filepath.Join(f.dataDir, kbID, "chroma"))
}

// DeleteCollection 删除 KB 的向量库 collection（DeleteTable 模板方法路径）
func (f *RetrievalKBFactory) DeleteCollection(ctx context.Context, kbID string) error {
	f.mu.Lock()
	store, ok := f.stores[kbID]
	delete(f.stores, kbID)
	f.mu.Unlock()
	if !ok {
		return nil // 幂等：未登记视为已删
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

// Create 创建知识库：DAO 落行 + collection 创建（向量库失败则 DAO 回滚）
func (s *KBService) Create(ctx context.Context, id, name, typ, configJSON, folderPath string) (*KBInfo, error) {
	now := time.Now().Unix()
	if id == "" {
		id = uuid.NewString()
	}
	kb := &KBInfo{ID: id, Name: name, Type: typ, ConfigJSON: configJSON,
		FolderPath: folderPath, CreatedAt: now, UpdatedAt: now}
	if err := s.store.KB().Create(kb); err != nil {
		return nil, err
	}
	if err := s.kbFactory.CreateCollection(ctx, id, configJSON); err != nil {
		_ = s.store.KB().Delete(id) // 回滚
		return nil, err
	}
	return kb, nil
}

// Get 查询知识库（纯 DAO）
func (s *KBService) Get(id string) (*KBInfo, error) { return s.store.KB().Get(id) }

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

// Delete 删除知识库：先删 collection 再删 DAO 行（固定顺序，spec §7 一致性）
func (s *KBService) Delete(ctx context.Context, id string) error {
	if err := s.kbFactory.DeleteCollection(ctx, id); err != nil {
		return err
	}
	return s.store.KB().Delete(id)
}
