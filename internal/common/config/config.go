package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Config 管理 YAML 配置文件的读写与环境变量解析。
//
// 支持以下能力：
//   - 读写 config.yaml
//   - 环境变量解析（${VAR:-default} 语法）
//   - 敏感字段自动解密（通过 DecryptFunc 钩子）
//   - 配置后处理（通过 NormalizeFunc 钩子）
//   - 热重载（通过 Reloader）
//   - 并发安全（sync.RWMutex）
type Config struct {
	path      string         // 配置文件路径
	raw       map[string]any // 原始数据（不含环境变量解析）
	data      map[string]any // 解析后数据（环境变量已替换）
	decryptFn DecryptFunc    // api_key/token 解密钩子
	normFn    NormalizeFunc  // 后处理钩子
	mu        sync.RWMutex   // 读写锁
}

// ──────────────────────────── 枚举 ────────────────────────────

// NormalizeFunc 配置后处理函数签名，用于 custom_headers 等字段的结构化。
type NormalizeFunc func(map[string]any)

// Option 配置选项函数。
type Option func(*Config)

// ──────────────────────────── 常量 ────────────────────────────
const (
	// DefaultConfigDir 相对于用户主目录的默认配置目录。
	DefaultConfigDir = ".uapclaw/config"
	// DefaultConfigFile 默认配置文件名。
	DefaultConfigFile = "config.yaml"
	// EnvConfigDir 配置目录环境变量名。
	EnvConfigDir = "UAPCLAW_CONFIG_DIR"
	// EnvDataDir 数据目录环境变量名。
	EnvDataDir = "UAPCLAW_DATA_DIR"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// New 创建配置管理器。
//
// path: 配置文件路径，为空时按以下优先级解析：
//  1. UAPCLAW_CONFIG_DIR 环境变量指向的 config.yaml
//  2. ~/.uapclaw/config/config.yaml
//
// opts: 可选配置，如 WithDecrypt、WithNormalize。
func New(path string, opts ...Option) (*Config, error) {
	cfg := &Config{}

	// 应用选项
	for _, opt := range opts {
		opt(cfg)
	}

	// 解析配置文件路径
	if path == "" {
		p, err := resolveConfigPath()
		if err != nil {
			return nil, fmt.Errorf("解析配置路径失败: %w", err)
		}
		path = p
	}
	cfg.path = path

	// 初始化空数据
	cfg.raw = make(map[string]any)
	cfg.data = make(map[string]any)

	return cfg, nil
}

// WithDecrypt 设置敏感字段解密函数。
func WithDecrypt(fn DecryptFunc) Option {
	return func(c *Config) {
		c.decryptFn = fn
	}
}

// WithNormalize 设置后处理函数。
func WithNormalize(fn NormalizeFunc) Option {
	return func(c *Config) {
		c.normFn = fn
	}
}

// Load 读取并解析配置文件（含环境变量替换 + 后处理 + 解密）。
// 如果配置文件不存在，返回空配置而不报错。
func (c *Config) Load() (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 读取文件内容
	content, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			// 配置文件不存在，返回空配置
			c.raw = make(map[string]any)
			c.data = make(map[string]any)
			return c.data, nil
		}
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	// YAML 反序列化
	var raw map[string]any
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}
	if raw == nil {
		raw = make(map[string]any)
	}
	c.raw = raw

	// 环境变量解析 + 解密
	c.data = ResolveEnvVars(c.raw, c.decryptFn).(map[string]any)

	// 后处理
	if c.normFn != nil {
		c.normFn(c.data)
	}

	return c.data, nil
}

// Raw 读取原始配置（不解析环境变量，不解密）。
// 如果配置文件不存在，返回空配置而不报错。
func (c *Config) Raw() (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	content, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]any), nil
		}
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}
	if raw == nil {
		raw = make(map[string]any)
	}
	c.raw = raw

	// 深拷贝一份返回，避免外部修改影响内部状态
	result := deepCopyMap(raw)
	return result, nil
}

// Save 整体写入配置。
// 写入前会自动创建不存在的目录。
func (c *Config) Save(data map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.saveLocked(data)
}

// Get 从已解析配置中获取值，支持点分隔路径。
//
// 示例：
//
//	cfg.Get("server.agentserver.host")  // 对应 YAML: server: agentserver: host:
//	cfg.Get("logging.level")            // 对应 YAML: logging: level:
func (c *Config) Get(key string) any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.data == nil {
		return nil
	}

	parts := strings.Split(key, ".")
	var current any = c.data

	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = m[part]
		if !ok {
			return nil
		}
	}

	return current
}

// Set 更新指定路径的值并写回文件。
// 支持点分隔路径，如 "server.agentserver.host"。
// 如果路径中的中间节点不存在，会自动创建。
func (c *Config) Set(key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 确保数据已加载
	if c.data == nil {
		c.data = make(map[string]any)
	}
	if c.raw == nil {
		c.raw = make(map[string]any)
	}

	parts := strings.Split(key, ".")

	// 设置 data 中的值
	setNestedValue(c.data, parts, value)

	// 设置 raw 中的值（同步更新）
	setNestedValue(c.raw, parts, value)

	// 写回文件
	return c.saveLocked(c.raw)
}

// Path 返回配置文件路径。
func (c *Config) Path() string {
	return c.path
}

// Reload 重新加载配置（供热重载回调使用）。
func (c *Config) Reload() error {
	_, err := c.Load()
	return err
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveConfigPath 按优先级解析配置文件路径。
//
// 优先级：
//  1. UAPCLAW_CONFIG_DIR 环境变量指向的 config.yaml
//  2. ~/.uapclaw/config/config.yaml
func resolveConfigPath() (string, error) {
	// 优先使用环境变量
	if envDir := os.Getenv(EnvConfigDir); envDir != "" {
		return filepath.Join(envDir, DefaultConfigFile), nil
	}

	// 默认使用用户主目录下的配置目录
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户主目录失败: %w", err)
	}

	return filepath.Join(home, DefaultConfigDir, DefaultConfigFile), nil
}

// saveLocked 将配置写入文件（调用方必须持有锁）。
func (c *Config) saveLocked(data map[string]any) error {
	// 确保目录存在
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	// YAML 序列化
	content, err := yaml.Marshal(data)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	// 写入文件
	if err := os.WriteFile(c.path, content, 0o644); err != nil {
		return fmt.Errorf("写入配置文件失败: %w", err)
	}

	return nil
}

// setNestedValue 在嵌套 map 中设置指定路径的值。
// 路径中间节点不存在时自动创建 map[string]any。
func setNestedValue(m map[string]any, parts []string, value any) {
	current := m
	for i, part := range parts {
		if i == len(parts)-1 {
			// 最后一个节点，设置值
			current[part] = value
			return
		}
		// 中间节点，确保是 map
		next, ok := current[part]
		if !ok {
			// 不存在，创建新的 map
			newMap := make(map[string]any)
			current[part] = newMap
			current = newMap
		} else if nextMap, ok := next.(map[string]any); ok {
			current = nextMap
		} else {
			// 已存在但不是 map，覆盖为新的 map
			newMap := make(map[string]any)
			current[part] = newMap
			current = newMap
		}
	}
}

// deepCopyMap 深拷贝 map[string]any。
func deepCopyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case map[string]any:
			result[k] = deepCopyMap(val)
		case []any:
			result[k] = deepCopySlice(val)
		default:
			result[k] = v
		}
	}
	return result
}

// deepCopySlice 深拷贝 []any。
func deepCopySlice(s []any) []any {
	if s == nil {
		return nil
	}
	result := make([]any, len(s))
	for i, v := range s {
		switch val := v.(type) {
		case map[string]any:
			result[i] = deepCopyMap(val)
		case []any:
			result[i] = deepCopySlice(val)
		default:
			result[i] = v
		}
	}
	return result
}
