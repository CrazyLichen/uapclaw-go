package harness_config

import (
	"fmt"
	"sort"
	"sync"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HarnessConfigInfo 注册的 harness_config 元数据
type HarnessConfigInfo struct {
	// ID 唯一标识
	ID string
	// Name 名称
	Name string
	// Version 版本
	Version string
	// PackageName 包名
	PackageName string
	// ConfigPath 配置文件路径
	ConfigPath string
	// Enabled 是否启用
	Enabled bool
}

// HarnessConfigRegistry 发现和管理已注册的 harness_config 包
type HarnessConfigRegistry struct {
	// registry 注册表
	registry map[string]HarnessConfigInfo
	// disabled 已禁用集合
	disabled map[string]bool
	// mu 读写锁
	mu sync.RWMutex
}

// ──────────────────────────── 全局变量 ────────────────────────────

// globalRegistry 全局注册表单例
var globalRegistry *HarnessConfigRegistry

// globalRegistryOnce 确保单例只初始化一次
var globalRegistryOnce sync.Once

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// getGlobalRegistry 获取全局注册表单例
func getGlobalRegistry() *HarnessConfigRegistry {
	globalRegistryOnce.Do(func() {
		globalRegistry = &HarnessConfigRegistry{
			registry: make(map[string]HarnessConfigInfo),
			disabled: make(map[string]bool),
		}
	})
	return globalRegistry
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Register 向全局注册表添加 harness_config 信息
func Register(info HarnessConfigInfo) {
	getGlobalRegistry().Register(info)
}

// Discover 返回所有已启用且已注册的 harness_config
func Discover() []HarnessConfigInfo {
	return getGlobalRegistry().Discover()
}

// Get 按 ID 查找 harness_config 信息
func Get(configID string) *HarnessConfigInfo {
	return getGlobalRegistry().Get(configID)
}

// Load 便捷方法：按 ID 加载并构建 harness_config
func Load(configID string, model *llm.Model, params map[string]any, workspaceRoot ...string) error {
	return getGlobalRegistry().Load(configID, model, params, workspaceRoot...)
}

// Disable 禁用指定 ID 的 harness_config
func Disable(configID string) {
	getGlobalRegistry().Disable(configID)
}

// Enable 重新启用指定 ID 的 harness_config
func Enable(configID string) {
	getGlobalRegistry().Enable(configID)
}

// InvalidateCache 清除缓存（init 注册模式下为空操作）
func InvalidateCache() {
	getGlobalRegistry().InvalidateCache()
}

// Register 向注册表添加 harness_config 信息
func (r *HarnessConfigRegistry) Register(info HarnessConfigInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	info.Enabled = true
	r.registry[info.ID] = info
}

// Discover 返回所有已启用的 harness_config 条目
func (r *HarnessConfigRegistry) Discover() []HarnessConfigInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]HarnessConfigInfo, 0, len(r.registry))
	for _, info := range r.registry {
		if !r.disabled[info.ID] {
			result = append(result, info)
		}
	}
	// 按 ID 排序以保持稳定输出
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

// Get 按 ID 查找 harness_config 信息，未找到返回 nil
func (r *HarnessConfigRegistry) Get(configID string) *HarnessConfigInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, ok := r.registry[configID]
	if !ok || r.disabled[configID] {
		return nil
	}
	return &info
}

// Load 便捷方法：按 ID 查找 → Loader.Load → Builder.Build
// 当前 Builder.Build 为桩实现，返回错误。
func (r *HarnessConfigRegistry) Load(configID string, model *llm.Model, params map[string]any, workspaceRoot ...string) error {
	info := r.Get(configID)
	if info == nil {
		installed := make([]string, 0)
		for _, r := range r.Discover() {
			installed = append(installed, r.ID)
		}
		return fmt.Errorf("HarnessConfig '%s' 未找到或已禁用，已安装: %v", configID, installed)
	}
	if info.ConfigPath == "" {
		return fmt.Errorf("HarnessConfig '%s' 没有配置路径，请确保 ConfigPath 已设置", configID)
	}

	loader := HarnessConfigLoader{}
	resolved, err := loader.Load(info.ConfigPath, params, workspaceRoot...)
	if err != nil {
		return fmt.Errorf("加载 HarnessConfig '%s' 失败: %w", configID, err)
	}

	builder := HarnessConfigBuilder{}
	wsRoot := ""
	if len(workspaceRoot) > 0 {
		wsRoot = workspaceRoot[0]
	}
	if err := builder.Build(resolved, model, wsRoot); err != nil {
		return fmt.Errorf("构建 HarnessConfig '%s' 失败: %w", configID, err)
	}

	return nil
}

// Disable 禁用指定 ID 的 harness_config
func (r *HarnessConfigRegistry) Disable(configID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.disabled[configID] = true
}

// Enable 重新启用指定 ID 的 harness_config
func (r *HarnessConfigRegistry) Enable(configID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.disabled, configID)
}

// InvalidateCache 清除缓存（init 注册模式下为空操作）
func (r *HarnessConfigRegistry) InvalidateCache() {
	// init 注册模式下无需缓存，为空操作
}
