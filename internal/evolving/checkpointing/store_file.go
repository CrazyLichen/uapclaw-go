package checkpointing

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FileCheckpointStore 基于 JSON 文件的检查点存储。
//
// 本地 JSON 检查点存储，用于保存和加载 EvolveCheckpoint。
// 不依赖核心检查点模块（避免污染核心生命周期语义），
// 可以在任何环境运行，方便调试和审计。
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/store_file.py FileCheckpointStore
type FileCheckpointStore struct {
	// baseDir 检查点文件存储目录
	baseDir string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewFileCheckpointStore 创建本地 JSON 检查点存储。
//
// 构造并确保目录存在。
// 对应 Python: FileCheckpointStore.__init__(base_dir)
func NewFileCheckpointStore(baseDir string) *FileCheckpointStore {
	ensureDir(baseDir)
	return &FileCheckpointStore{baseDir: baseDir}
}

// SaveCheckpoint 保存检查点到 JSON 文件。
//
// 对应 Python: FileCheckpointStore.save_checkpoint(ckpt, filename="latest.json")
func (s *FileCheckpointStore) SaveCheckpoint(ckpt *EvolveCheckpoint, filename string) (string, error) {
	if s.baseDir == "" {
		return "", nil
	}
	ensureDir(s.baseDir)
	path := filepath.Join(s.baseDir, filename)

	serialized := toJSONCompatible(ckpt)
	data, err := json.Marshal(serialized)
	if err != nil {
		return "", fmt.Errorf("序列化检查点失败: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", fmt.Errorf("写入检查点文件失败: %w", err)
	}
	return path, nil
}

// LoadCheckpoint 从 JSON 文件加载检查点。
//
// 对应 Python: FileCheckpointStore.load_checkpoint(path)
func (s *FileCheckpointStore) LoadCheckpoint(path string) (*EvolveCheckpoint, error) {
	if s.baseDir == "" {
		return nil, nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取检查点文件失败: %w", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("解析检查点 JSON 失败: %w", err)
	}

	ckpt := &EvolveCheckpoint{
		Version:        getStrFromAny(raw["version"], "v1"),
		RunID:          getStrFromAny(raw["run_id"], ""),
		Step:           getIntMapFromAny(raw["step"]),
		Best:           getAnyMapFromAny(raw["best"]),
		OperatorsState: getNestedMapFromAny(raw["operators_state"]),
		UpdaterState:   getAnyMapFromAny(raw["updater_state"]),
		SearcherState:  getAnyMapFromAny(raw["searcher_state"]),
		LastMetrics:    getAnyMapFromAny(raw["last_metrics"]),
	}
	if raw["seed"] != nil {
		seed := getIntFromAny(raw["seed"], 0)
		ckpt.Seed = &seed
	}
	return ckpt, nil
}

// LoadStateDict 从检查点文件读取 operators_state。
//
// 深度学习风格推理加载器，从检查点 JSON 读取 operators_state：
//
//		state = store.load_state_dict(path)
//	   加载操作状态（op.load_state）
//
// 对应 Python: FileCheckpointStore.load_state_dict(path)
func (s *FileCheckpointStore) LoadStateDict(path string) (map[string]map[string]any, error) {
	if s.baseDir == "" {
		return nil, nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取检查点文件失败: %w", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("解析检查点 JSON 失败: %w", err)
	}

	opsState, ok := raw["operators_state"]
	if !ok || opsState == nil {
		return map[string]map[string]any{}, nil
	}
	return getNestedMapFromAny(opsState), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureDir 确保目录存在。
func ensureDir(dir string) {
	if dir != "" {
		_ = os.MkdirAll(dir, 0755)
	}
}

// toJSONCompatible 递归将对象转换为 JSON 兼容类型。
//
// 对应 Python: _to_json_compatible(obj)
func toJSONCompatible(obj any) any {
	if obj == nil {
		return nil
	}

	// 处理 map
	if m, ok := obj.(map[string]any); ok {
		result := make(map[string]any, len(m))
		for k, v := range m {
			result[k] = toJSONCompatible(v)
		}
		return result
	}

	// 处理 map[string]int
	if m, ok := obj.(map[string]int); ok {
		result := make(map[string]any, len(m))
		for k, v := range m {
			result[k] = toJSONCompatible(v)
		}
		return result
	}

	// 处理 map[string]map[string]any
	if m, ok := obj.(map[string]map[string]any); ok {
		result := make(map[string]any, len(m))
		for k, v := range m {
			result[k] = toJSONCompatible(v)
		}
		return result
	}

	// 处理 slice/array
	if s, ok := obj.([]any); ok {
		result := make([]any, len(s))
		for i, v := range s {
			result[i] = toJSONCompatible(v)
		}
		return result
	}

	// 处理 struct（通过反射转为 map）
	// 对齐 Python: hasattr(obj, "__dataclass_fields__") → asdict(obj)

	// 原始类型直接返回
	return obj
}

// getIntMapFromAny 从 any 提取 map[string]int。
func getIntMapFromAny(v any) map[string]int {
	if v == nil {
		return map[string]int{}
	}
	m, ok := v.(map[string]any)
	if !ok {
		return map[string]int{}
	}
	result := make(map[string]int, len(m))
	for k, val := range m {
		result[k] = getIntFromAny(val, 0)
	}
	return result
}

// getAnyMapFromAny 从 any 提取 map[string]any。
func getAnyMapFromAny(v any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	m, ok := v.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return m
}

// getNestedMapFromAny 从 any 提取 map[string]map[string]any。
func getNestedMapFromAny(v any) map[string]map[string]any {
	if v == nil {
		return map[string]map[string]any{}
	}
	m, ok := v.(map[string]any)
	if !ok {
		return map[string]map[string]any{}
	}
	result := make(map[string]map[string]any, len(m))
	for k, val := range m {
		if nested, ok := val.(map[string]any); ok {
			result[k] = nested
		}
	}
	return result
}
