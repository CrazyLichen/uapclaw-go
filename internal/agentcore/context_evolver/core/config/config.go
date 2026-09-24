package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// _config 已加载的配置字典。对齐 Python _config = {}。
	_config map[string]any
	// _configLoaded 配置是否已加载。对齐 Python _config_loaded = False。
	_configLoaded bool
	// _configMu 保护并发读写。Go 新增，Python 无此需求（GIL）。
	_configMu sync.RWMutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Get 获取配置值。对齐 Python config.get(key, default)。
// 查找顺序：1. 已加载 _config  2. os.Getenv(key)  3. 返回 defaultVal。
// 首次调用自动触发 Load()。
func Get(key string, defaultVal any) any {
	ensureLoaded()

	_configMu.RLock()
	val, ok := _config[key]
	_configMu.RUnlock()
	if ok {
		return val
	}

	// 对齐 Python：环境变量兜底
	envVal := os.Getenv(key)
	if envVal != "" {
		return convertValue(envVal)
	}

	return defaultVal
}

// GetString 获取字符串配置值。
func GetString(key string, defaultVal string) string {
	val := Get(key, defaultVal)
	if s, ok := val.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", val)
}

// GetInt 获取整数配置值。
func GetInt(key string, defaultVal int) int {
	val := Get(key, defaultVal)
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return defaultVal
}

// GetBool 获取布尔配置值。对齐 Python 中 "true"/"false" 字符串转 bool 的逻辑。
func GetBool(key string, defaultVal bool) bool {
	val := Get(key, defaultVal)
	switch v := val.(type) {
	case bool:
		return v
	case string:
		lower := strings.ToLower(v)
		if lower == "true" || lower == "yes" || lower == "1" {
			return true
		}
		if lower == "false" || lower == "no" || lower == "0" {
			return false
		}
	case int:
		return v != 0
	}
	return defaultVal
}

// Load 加载配置文件。对齐 Python config.load(config_path, env_path)。
// 数据源优先级：.env（最高）→ config.yaml（不覆盖 .env）→ os.Getenv（Get 时兜底）。
// configPath/envPath 为空时使用 context_evolver 根目录下的默认路径。
func Load(configPath ...string) error {
	_configMu.Lock()
	defer _configMu.Unlock()

	// 重置
	_config = make(map[string]any)
	_configLoaded = true

	rootDir := rootDir()

	// 确定路径
	envPath := filepath.Join(rootDir, ".env")
	yamlPath := filepath.Join(rootDir, "config.yaml")
	if len(configPath) > 0 && configPath[0] != "" {
		yamlPath = configPath[0]
	}
	if len(configPath) > 1 && configPath[1] != "" {
		envPath = configPath[1]
	}

	// 对齐 Python：先加载 .env（最高优先级）
	if _, err := os.Stat(envPath); err == nil {
		if err := loadEnvFile(envPath); err != nil {
			logger.Warn(logComponent).Str("path", envPath).Err(err).Msg("Failed to load .env file")
		} else {
			logger.Debug(logComponent).Str("path", envPath).Msg("Loaded .env config")
		}
	}

	// 对齐 Python：再加载 config.yaml（不覆盖 .env 已有 key）
	if _, err := os.Stat(yamlPath); err == nil {
		if err := loadYAMLFile(yamlPath); err != nil {
			logger.Warn(logComponent).Str("path", yamlPath).Err(err).Msg("Failed to load config.yaml file")
		} else {
			logger.Debug(logComponent).Str("path", yamlPath).Msg("Loaded config.yaml config")
		}
	}

	logger.Info(logComponent).
		Int("key_count", len(_config)).
		Msg("Config loaded")
	return nil
}

// Set 运行时修改配置值。对齐 Python config.set_value(key, value)。
func Set(key string, value any) {
	ensureLoaded()
	_configMu.Lock()
	_config[key] = value
	_configMu.Unlock()
}

// Delete 删除配置值。对齐 Python config.delete(key)。
func Delete(key string) {
	ensureLoaded()
	_configMu.Lock()
	delete(_config, key)
	_configMu.Unlock()
}

// Snapshot 获取配置快照（用于测试保存/恢复）。对齐 Python config.snapshot()。
func Snapshot() map[string]any {
	ensureLoaded()
	_configMu.RLock()
	snap := make(map[string]any, len(_config))
	for k, v := range _config {
		snap[k] = v
	}
	_configMu.RUnlock()
	return snap
}

// Restore 从快照恢复配置。对齐 Python config.restore(snap)。
func Restore(snap map[string]any) {
	_configMu.Lock()
	_config = make(map[string]any, len(snap))
	for k, v := range snap {
		_config[k] = v
	}
	_configLoaded = true
	_configMu.Unlock()
}

// Reload 强制重新加载配置。对齐 Python config.reload()。
func Reload() {
	_configMu.Lock()
	_config = make(map[string]any)
	_configLoaded = false
	_configMu.Unlock()
	// Load 内部会重新设置 _configLoaded
	_ = Load()
}

// Reset 重置配置为空（仅用于测试）。
func Reset() {
	_configMu.Lock()
	_config = make(map[string]any)
	_configLoaded = false
	_configMu.Unlock()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureLoaded 确保配置已加载（懒加载）。对齐 Python get/set_value/delete 开头的 if not _config_loaded: load()。
func ensureLoaded() {
	_configMu.RLock()
	loaded := _configLoaded
	_configMu.RUnlock()
	if !loaded {
		_ = Load()
	}
}

// rootDir 计算 context_evolver 根目录。
// 对齐 Python: os.path.dirname(os.path.dirname(os.path.abspath(__file__)))。
// Go 中 __file__ 等价于当前源文件路径，core/config/config.go → core/ → context_evolver/。
func rootDir() string {
	_, filename, _, _ := runtime.Caller(0)
	// filename = .../context_evolver/core/config/config.go
	// 向上两级：config/ → core/ → context_evolver/
	dir := filepath.Dir(filepath.Dir(filepath.Dir(filename)))
	return dir
}

// loadEnvFile 加载 .env 文件。对齐 Python load() 中读取 .env 的逻辑。
// 逐行解析 KEY=VALUE，支持 # 注释，带类型推断。
func loadEnvFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("打开 .env 文件失败: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// 对齐 Python：跳过空行和注释
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// 对齐 Python：按第一个 = 分割
		if !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		// 对齐 Python：类型推断
		_config[key] = convertValue(value)
	}

	return scanner.Err()
}

// loadYAMLFile 加载 config.yaml 文件。对齐 Python load() 中读取 config.yaml 的逻辑。
// 合并到 _config 但不覆盖 .env 已有 key。
func loadYAMLFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取 config.yaml 失败: %w", err)
	}

	var yamlConfig map[string]any
	if err := yaml.Unmarshal(data, &yamlConfig); err != nil {
		return fmt.Errorf("解析 config.yaml 失败: %w", err)
	}
	if yamlConfig == nil {
		return nil
	}

	// 对齐 Python：YAML config values don't override .env values
	for key, value := range yamlConfig {
		if _, exists := _config[key]; !exists {
			_config[key] = value
		}
	}
	return nil
}

// convertValue 将字符串值转换为合适的类型。对齐 Python _convert_value(value)。
// 支持 bool（true/false/yes/no/1/0）、int、float、原始字符串。
func convertValue(value string) any {
	if value == "" {
		return value
	}

	// 对齐 Python：布尔值
	lower := strings.ToLower(value)
	if lower == "true" || lower == "yes" || lower == "1" {
		return true
	}
	if lower == "false" || lower == "no" || lower == "0" {
		return false
	}

	// 对齐 Python：数值
	if strings.Contains(value, ".") {
		if f, err := strconv.ParseFloat(value, 64); err == nil {
			return f
		}
	}
	if i, err := strconv.Atoi(value); err == nil {
		return i
	}

	return value
}
