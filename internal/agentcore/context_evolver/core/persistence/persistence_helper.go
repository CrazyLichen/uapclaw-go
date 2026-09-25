package persistence

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/file_connector"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 接口 ────────────────────────────

// MilvusConnector Milvus 后端接口（P7 实现）。
//
// P1 只定义接口，实现在 P7 和 ContextEvolutionRail 一起完成。
//
// Python: openjiuwen/extensions/context_evolver/core/db_connector/milvus_connector.py
type MilvusConnector interface {
	// SaveToDB 保存数据到 Milvus 命名空间
	SaveToDB(namespace string, data map[string]any) error
	// LoadFromDB 从 Milvus 命名空间加载数据
	LoadFromDB(namespace string) (map[string]any, error)
	// Exists 检查命名空间是否有数据
	Exists(namespace string) bool
	// Delete 删除命名空间数据
	Delete(namespace string) bool
}

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryPersistenceHelper 记忆持久化助手。
//
// 支持 JSON 文件和 Milvus 双后端。"auto" 模式探测 Milvus 可达性后回退 JSON。
// P1 阶段只实现 JSON 后端，Milvus 在 P7 启用。
//
// Python: openjiuwen/extensions/context_evolver/core/persistence.py
type MemoryPersistenceHelper struct {
	persistType      string
	persistPath      string
	milvusHost       string
	milvusPort       int
	milvusCollection string

	jsonConnector   *file_connector.JSONFileConnector
	milvusConnector MilvusConnector // P7 注入
	resolvedType    string          // auto 探测后缓存
	resolveOnce     sync.Once       // 保证只探测一次
}

// PersistenceOption 持久化助手配置选项。
type PersistenceOption func(*MemoryPersistenceHelper)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultPersistPath 默认持久化路径模板
	// 对齐 Python: "./memories/{algo_name}/{user_id}.json"
	defaultPersistPath = "./memories/{algo_name}/{user_id}.json"

	// defaultMilvusHost 默认 Milvus 主机
	// 对齐 Python: milvus_host="localhost"
	defaultMilvusHost = "localhost"

	// defaultMilvusPort 默认 Milvus 端口
	// 对齐 Python: milvus_port=19530
	defaultMilvusPort = 19530

	// defaultMilvusCollection 默认 Milvus 集合名
	// 对齐 Python: milvus_collection="vector_nodes"
	defaultMilvusCollection = "vector_nodes"

	// logComponent 日志组件标识
	logComponent = logger.ComponentCommon
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemoryPersistenceHelper 创建记忆持久化助手。
// 对齐 Python 默认 persistType="auto"，惰性探测 Milvus 可达性。
func NewMemoryPersistenceHelper(opts ...PersistenceOption) *MemoryPersistenceHelper {
	h := &MemoryPersistenceHelper{
		persistType:      "auto",
		persistPath:      defaultPersistPath,
		milvusHost:       defaultMilvusHost,
		milvusPort:       defaultMilvusPort,
		milvusCollection: defaultMilvusCollection,
		jsonConnector:    file_connector.NewJSONFileConnector(),
		resolvedType:     "", // auto 模式下待探测
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Namespace 生成 Milvus 命名空间键。
// 对齐 Python _namespace(user_id, algo_name) → "memory_{algo_name}_{user_id}"。
func Namespace(userID, algoName string) string {
	return fmt.Sprintf("memory_%s_%s", algoName, userID)
}

// WithPersistType 设置持久化类型。
func WithPersistType(pt string) PersistenceOption {
	return func(h *MemoryPersistenceHelper) { h.persistType = pt }
}

// WithPersistPath 设置路径模板。
func WithPersistPath(p string) PersistenceOption {
	return func(h *MemoryPersistenceHelper) { h.persistPath = p }
}

// WithMilvusHost 设置 Milvus 主机。
func WithMilvusHost(host string) PersistenceOption {
	return func(h *MemoryPersistenceHelper) { h.milvusHost = host }
}

// WithMilvusPort 设置 Milvus 端口。
func WithMilvusPort(port int) PersistenceOption {
	return func(h *MemoryPersistenceHelper) { h.milvusPort = port }
}

// WithMilvusCollection 设置 Milvus 集合名。
func WithMilvusCollection(name string) PersistenceOption {
	return func(h *MemoryPersistenceHelper) { h.milvusCollection = name }
}

// Save 持久化节点到后端。
// 对齐 Python MemoryPersistenceHelper.save(user_id, algo_name, nodes_dict)。
// auto 模式惰性探测后路由到 milvus 或 json 后端。
// 空数据直接返回（对齐 Python）。
func (h *MemoryPersistenceHelper) Save(userID, algoName string, nodesDict map[string]any) error {
	if len(nodesDict) == 0 {
		logger.Debug(logComponent).Str("user_id", userID).Str("algo", algoName).Msg("空数据跳过持久化")
		return nil
	}
	h.resolveBackend()
	switch h.resolvedType {
	case "milvus":
		ns := Namespace(userID, algoName)
		return h.milvusConnector.SaveToDB(ns, nodesDict)
	default:
		return h.saveJSON(userID, algoName, nodesDict)
	}
}

// Load 从后端加载节点。
// 对齐 Python MemoryPersistenceHelper.load(user_id, algo_name)。
// auto 模式惰性探测后路由到 milvus 或 json 后端。
func (h *MemoryPersistenceHelper) Load(userID, algoName string) (map[string]any, error) {
	h.resolveBackend()
	switch h.resolvedType {
	case "milvus":
		ns := Namespace(userID, algoName)
		return h.milvusConnector.LoadFromDB(ns)
	default:
		return h.loadJSON(userID, algoName)
	}
}

// SetMilvusConnector 注入 Milvus 连接器（P7 使用）。
// 对齐 Python MemoryPersistenceHelper.set_milvus_connector(connector)。
func (h *MemoryPersistenceHelper) SetMilvusConnector(conn MilvusConnector) {
	h.milvusConnector = conn
}

// ResolvedType 获取解析后的后端类型。
// 对齐 Python MemoryPersistenceHelper.resolved_type 属性。
func (h *MemoryPersistenceHelper) ResolvedType() string {
	return h.resolvedType
}

// PersistType 获取持久化类型配置。
func (h *MemoryPersistenceHelper) PersistType() string {
	return h.persistType
}

// PersistPath 获取路径模板配置。
func (h *MemoryPersistenceHelper) PersistPath() string {
	return h.persistPath
}

// MilvusHost 获取 Milvus 主机配置。
func (h *MemoryPersistenceHelper) MilvusHost() string {
	return h.milvusHost
}

// MilvusPort 获取 Milvus 端口配置。
func (h *MemoryPersistenceHelper) MilvusPort() int {
	return h.milvusPort
}

// MilvusCollection 获取 Milvus 集合名配置。
func (h *MemoryPersistenceHelper) MilvusCollection() string {
	return h.milvusCollection
}

// String 实现 Stringer 接口。
// 对齐 Python MemoryPersistenceHelper.__repr__()。
func (h *MemoryPersistenceHelper) String() string {
	return fmt.Sprintf("MemoryPersistenceHelper(persist_type=%s, persist_path=%s)", h.persistType, h.persistPath)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveBackend 惰性探测后端类型。
// 对齐 Python MemoryPersistenceHelper 构造时同步探测，Go 改为惰性避免阻塞。
func (h *MemoryPersistenceHelper) resolveBackend() {
	h.resolveOnce.Do(func() {
		switch h.persistType {
		case "auto":
			if h.milvusConnector != nil {
				if h.probeMilvus() {
					h.resolvedType = "milvus"
					logger.Info(logComponent).Str("resolved_type", "milvus").Msg("auto 模式探测到 Milvus 可达")
					return
				}
			} else if h.milvusHost != "" {
				// 自动创建 MilvusConnectorImpl 并注入
				conn := NewMilvusConnectorImpl(
					WithConnectorHost(h.milvusHost),
					WithConnectorPort(h.milvusPort),
					WithConnectorCollection(h.milvusCollection),
				)
				h.milvusConnector = conn
				if h.probeMilvus() {
					h.resolvedType = "milvus"
					logger.Info(logComponent).Str("resolved_type", "milvus").Msg("auto 模式探测到 Milvus 可达（自动创建连接器）")
					return
				}
			}
			h.resolvedType = "json"
			logger.Warn(logComponent).Str("persist_type", h.persistType).Msg("Milvus 不可达，回退 JSON 后端")
		case "milvus":
			h.resolvedType = "milvus"
		default:
			h.resolvedType = "json"
		}
	})
}

// probeMilvus 探测 Milvus 可达性。
func (h *MemoryPersistenceHelper) probeMilvus() bool {
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).Any("panic", r).Msg("探测 Milvus 时发生 panic")
		}
	}()

	if impl, ok := h.milvusConnector.(*MilvusConnectorImpl); ok {
		ctx, cancel := context.WithTimeout(context.Background(), milvusProbeTimeout)
		defer cancel()
		return impl.ProbeReachable(ctx)
	}

	// 非 MilvusConnectorImpl 实现时，尝试 Exists 操作
	// 如果不 panic 且不报错，认为可达
	defer func() { recover() }()
	return h.milvusConnector.Exists("__probe__") || true
}

// jsonPath 根据模板生成 JSON 文件路径。
// 对齐 Python _json_path(user_id, algo_name)，替换 {user_id} 和 {algo_name} 占位符。
func (h *MemoryPersistenceHelper) jsonPath(userID, algoName string) string {
	p := h.persistPath
	p = strings.ReplaceAll(p, "{user_id}", userID)
	p = strings.ReplaceAll(p, "{algo_name}", algoName)
	return p
}

// saveJSON 保存到 JSON 文件（合并已有数据）。
// 对齐 Python _save_json(user_id, algo_name, nodes_dict)。
// 先加载已有文件，合并 via dict.update（upsert 语义），再写回。
func (h *MemoryPersistenceHelper) saveJSON(userID, algoName string, nodesDict map[string]any) error {
	path := h.jsonPath(userID, algoName)

	existing := make(map[string]any)
	if h.jsonConnector.Exists(path) {
		loaded, err := h.jsonConnector.LoadFromFile(path)
		if err != nil {
			logger.Error(logComponent).Str("path", path).Err(err).Msg("Failed to load existing JSON")
			// 不阻断，继续写入（对齐 Python 只记日志不中断的行为）
		} else {
			existing = loaded
		}
	}

	// 合并（upsert 语义），对齐 Python dict.update(nodes_dict)
	for k, v := range nodesDict {
		existing[k] = v
	}

	if err := h.jsonConnector.SaveToFile(path, existing); err != nil {
		return fmt.Errorf("保存 JSON 失败: %w", err)
	}

	logger.Info(logComponent).
		Str("path", path).
		Int("count", len(nodesDict)).
		Str("algo", algoName).
		Msg("Persisted memories to JSON")
	return nil
}

// loadJSON 从 JSON 文件加载。
// 对齐 Python _load_json(user_id, algo_name)。
// 文件不存在时返回空 dict。
func (h *MemoryPersistenceHelper) loadJSON(userID, algoName string) (map[string]any, error) {
	path := h.jsonPath(userID, algoName)
	if !h.jsonConnector.Exists(path) {
		return map[string]any{}, nil
	}
	data, err := h.jsonConnector.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("加载 JSON 失败: %w", err)
	}
	logger.Info(logComponent).
		Str("path", path).
		Int("count", len(data)).
		Str("algo", algoName).
		Msg("Loaded memories from JSON")
	return data, nil
}
