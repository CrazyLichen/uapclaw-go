package utils

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ConfigManager 统一配置管理器，支持 JSON/YAML 文件加载与保存。
//
// 对齐 Python retrieval/utils/config_manager.py。**键双轨**是 Python 真实
// 行为（spec 13.27 §8-1）：LoadFromFile 存 "knowledge_base" 字面键，
// UpdateConfig 按类型名存键——两条键不一致，因此 update 之后再调
// GetKnowledgeBaseConfig 必报 155304。此 bug 亦属行为，字面复刻不改。
//
// Python: class ConfigManager
type ConfigManager struct {
	// mu 保护 configs（并发 Load/Update/Save 会对同一 map 读写，未加锁会 panic）
	mu sync.RWMutex
	// configs 已加载配置（Python Dict[str, BaseModel]；Go 用 any 收纳）
	configs map[string]any
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// knowledgeBaseConfigKey LoadFromFile 的固定存储键（Python 字面 "knowledge_base"）
	knowledgeBaseConfigKey = "knowledge_base"
	// maxSafeIntFloat float64 能精确表示的最大 int 边界（2^63）
	maxSafeIntFloat = float64(1 << 63)
	// minSafeIntFloat float64 能精确表示的最小 int 边界（-2^63）
	minSafeIntFloat = -float64(1 << 63)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewConfigManager 创建配置管理器。configPath 非空时立即加载。
//
// Python: ConfigManager.__init__（config_path 真值时调 load_from_file）
func NewConfigManager(configPath string) (*ConfigManager, error) {
	m := &ConfigManager{configs: make(map[string]any)}
	if configPath != "" {
		if err := m.LoadFromFile(configPath); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// LoadFromFile 从文件加载配置（支持 JSON 与 YAML）。
//
// 文件不存在→155300；后缀不支持→155302；成功则按 "knowledge_base" 键存
// KnowledgeBaseConfig。Python 的 `try: import yaml except ImportError`
// 分支（155301）在 Go 为编译期死路径（yaml.v3 是直接依赖），错误码保留定义
// 但不可达（spec 13.27 §8-2）。
//
// Python: load_from_file
func (m *ConfigManager) LoadFromFile(path string) error {
	if _, err := os.Stat(path); err != nil {
		return exception.BuildError(
			exception.StatusRetrievalUtilsConfigFileNotFound,
			exception.WithMsg(fmt.Sprintf("Configuration file does not exist: %s", path)),
		)
	}

	suffix := strings.ToLower(filepath.Ext(path))
	var data map[string]any
	switch suffix {
	case ".json":
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return err
		}
	case ".yaml", ".yml":
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := yaml.Unmarshal(raw, &data); err != nil {
			return err
		}
	default:
		return exception.BuildError(
			exception.StatusRetrievalUtilsConfigFormatNotSupport,
			exception.WithMsg(fmt.Sprintf("Unsupported configuration file format: %s", suffix)),
		)
	}

	// 对齐 Python KnowledgeBaseConfig(**data)：忽略未知键，已知字段显式取；
	// pydantic 的必填 / 字面量约束在此转为显式校验（kb_id 必填、index_type 枚举）。
	cfg, err := knowledgeBaseConfigFromMap(data)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.configs[knowledgeBaseConfigKey] = cfg
	m.mu.Unlock()
	return nil
}

// SaveToFile 保存配置到文件（支持 JSON 与 YAML）。
//
// 无 "knowledge_base" 键→155303。JSON 用 indent 2 + 非 ASCII 不转义
// （对齐 json.dump(indent=2, ensure_ascii=False)）；YAML 用 yaml.Marshal
// 逐行输出（对齐 yaml.dump(allow_unicode=True, default_flow_style=False)）。
//
// 字节差异（有意保留，不影响语义）：Go 的 encoding/json 对 map 按 key 字典序
// 输出且 Encoder 追加尾换行；Python model_dump + json.dump 按字段声明序且无尾
// 换行。JSON 对象无序，两侧解析结果等价；仅在与 Python 做字节级对拍时不同。
//
// Python: save_to_file
func (m *ConfigManager) SaveToFile(path string) error {
	m.mu.RLock()
	cfgAny, ok := m.configs[knowledgeBaseConfigKey]
	m.mu.RUnlock()
	if !ok {
		return exception.BuildError(
			exception.StatusRetrievalUtilsConfigNotFound,
			exception.WithMsg("No configuration to save"),
		)
	}
	cfg, ok := cfgAny.(common.KnowledgeBaseConfig)
	if !ok {
		// Python 侧此处会对非 KnowledgeBaseConfig 调 model_dump 而 AttributeError；
		// Go 收敛为同码错误（spec 13.27 §8-8 pydantic 类型约束差异）。
		return exception.BuildError(
			exception.StatusRetrievalUtilsConfigNotFound,
			exception.WithMsg("No configuration to save"),
		)
	}

	data := knowledgeBaseConfigToMap(cfg)
	suffix := strings.ToLower(filepath.Ext(path))
	switch suffix {
	case ".json":
		var sb strings.Builder
		enc := json.NewEncoder(&sb)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(data); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(sb.String()), 0o644)
	case ".yaml", ".yml":
		raw, err := yaml.Marshal(data)
		if err != nil {
			return err
		}
		return os.WriteFile(path, raw, 0o644)
	default:
		return exception.BuildError(
			exception.StatusRetrievalUtilsConfigFormatNotSupport,
			exception.WithMsg(fmt.Sprintf("Unsupported configuration file format: %s", suffix)),
		)
	}
}

// GetConfig 按类型取回配置（Python get_config(Type[T]) 的 Go 泛型同构）。
//
// 遍历顺序即 map 迭代顺序（Python dict 插入序在 Go map 无序，但同类型
// 只会有一个键命中，行为等价）。未找到返回零值与 false（Python None）。
func GetConfig[T any](m *ConfigManager) (T, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, cfg := range m.configs {
		if v, ok := cfg.(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// GetKnowledgeBaseConfig 取回知识库配置。
//
// "knowledge_base" 键缺失→155304。注意键双轨偏差：UpdateConfig 存的是
// 类型名键，故 update 路径必然取不到（Python 真实行为，spec 13.27 §8-1）。
//
// Python: get_knowledge_base_config
func (m *ConfigManager) GetKnowledgeBaseConfig() (common.KnowledgeBaseConfig, error) {
	m.mu.RLock()
	cfgAny, ok := m.configs[knowledgeBaseConfigKey]
	m.mu.RUnlock()
	if !ok {
		return common.KnowledgeBaseConfig{}, exception.BuildError(
			exception.StatusRetrievalUtilsConfigProcessError,
			exception.WithMsg("Knowledge base configuration not loaded"),
		)
	}
	cfg, ok := cfgAny.(common.KnowledgeBaseConfig)
	if !ok {
		return common.KnowledgeBaseConfig{}, exception.BuildError(
			exception.StatusRetrievalUtilsConfigProcessError,
			exception.WithMsg("Knowledge base configuration not loaded"),
		)
	}
	return cfg, nil
}

// UpdateConfig 更新配置（按类型名存键——键双轨偏差，spec 13.27 §8-1）。
//
// Python 用 type(config).__name__（如 "KnowledgeBaseConfig"），Go 用
// reflect 类型名（含包前缀 "common.KnowledgeBaseConfig"）——各自语言的
// 类型名字面（spec 13.27 §8-10）。
//
// Python: update_config
func (m *ConfigManager) UpdateConfig(config any) {
	m.mu.Lock()
	m.configs[typeNameOf(config)] = config
	m.mu.Unlock()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// knowledgeBaseConfigFromMap 由解析出的 map 构造 KnowledgeBaseConfig。
//
// 对齐 pydantic 默认行为（spec 13.27 §8-8）：未知键忽略；缺省字段取字段
// 默认值（index_type="hybrid" / chunk_size=512 / chunk_overlap=50 / 两个
// bool 为 false）。JSON 数字解码为 float64、YAML 数字解码为 int，两条路径
// 都要吃下（Python json 同为 int，但我们经 any 中转后类型不同）。
func knowledgeBaseConfigFromMap(data map[string]any) (common.KnowledgeBaseConfig, error) {
	// 基线取 pydantic 字段默认值
	cfg := common.KnowledgeBaseConfig{
		IndexType:    common.IndexTypeHybrid,
		ChunkSize:    512,
		ChunkOverlap: 50,
	}
	if v, ok := data["kb_id"].(string); ok {
		cfg.KBID = v
	}
	if v, ok := data["index_type"].(string); ok {
		cfg.IndexType = common.IndexTypeKind(v)
	}
	if v, ok := data["use_graph"].(bool); ok {
		cfg.UseGraph = v
	}
	if n, ok := asInt(data["chunk_size"]); ok {
		cfg.ChunkSize = n
	}
	if n, ok := asInt(data["chunk_overlap"]); ok {
		cfg.ChunkOverlap = n
	}
	if v, ok := data["use_caption_for_images"].(bool); ok {
		cfg.UseCaptionForImages = v
	}
	// Python: pydantic KnowledgeBaseConfig 的必填/字面量约束
	if strings.TrimSpace(cfg.KBID) == "" {
		return cfg, exception.BuildError(
			exception.StatusRetrievalUtilsConfigInvalid,
			exception.WithMsg("knowledge_base.kb_id 不能为空"),
		)
	}
	switch cfg.IndexType {
	case common.IndexTypeHybrid, common.IndexTypeBM25, common.IndexTypeVector:
	default:
		return cfg, exception.BuildError(
			exception.StatusRetrievalUtilsConfigInvalid,
			exception.WithMsg(fmt.Sprintf("knowledge_base.index_type 非法: %s", cfg.IndexType)),
		)
	}
	return cfg, nil
}

// knowledgeBaseConfigToMap 将 KnowledgeBaseConfig 序列化为 map（键名对齐
// Python model_dump 的 snake_case 输出）。
func knowledgeBaseConfigToMap(cfg common.KnowledgeBaseConfig) map[string]any {
	return map[string]any{
		"kb_id":                  cfg.KBID,
		"index_type":             string(cfg.IndexType),
		"use_graph":              cfg.UseGraph,
		"chunk_size":             cfg.ChunkSize,
		"chunk_overlap":          cfg.ChunkOverlap,
		"use_caption_for_images": cfg.UseCaptionForImages,
	}
}

// asInt 把 JSON 的 float64 或 YAML 的 int 统一取为 int。
//
// 对齐 Python int() 的精度语义：Python 任意精度整数不存在溢出，Go 的
// int 会回绕。超出 int 可表示范围的值返回 false，调用方保留字段默认值
// （Python 侧则是得到一个超大的合法整数——极端输入下仍是分叉，但至少
// 不会静默得到一个回绕后的负数）。
func asInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		if int64(int(n)) != n {
			return 0, false
		}
		return int(n), true
	case float64:
		// NaN、±Inf 与超出 int64 可表示范围的数值都无法安全转换
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		f := math.Trunc(n)
		if f < minSafeIntFloat || f > maxSafeIntFloat {
			return 0, false
		}
		return int(f), true
	default:
		return 0, false
	}
}

// typeNameOf 取值类型名（Python type(x).__name__ 的对应物，Go 含包前缀，
// 如 "common.KnowledgeBaseConfig"，spec 13.27 §8-10）。
func typeNameOf(v any) string {
	if v == nil {
		return "<nil>"
	}
	return reflect.TypeOf(v).String()
}
