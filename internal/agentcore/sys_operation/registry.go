package sys_operation

import (
	"fmt"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// OperationDef 操作定义，包含类型信息和工厂方法。
// 对齐 Python OperationDef：cls, description, name, mode, create_instance。
type OperationDef struct {
	// NewFunc 工厂函数：从 runConfig 创建子操作实例
	NewFunc func(runConfig any) SysSubOperation
	// Name 操作名称（如 "fs", "shell", "code"）
	Name string
	// Mode 操作模式
	Mode OperationMode
	// Description 操作描述
	Description string
}

// OperationRegistry 操作注册表。
// 对齐 Python OperationRegistry：_repository mode → name → OperationDef。
// Go 不做包扫描（Python 的 _discover_package），改用 init() 显式注册。
type OperationRegistry struct {
	// mu 保护 repository
	mu sync.RWMutex
	// 仓库模式映射：mode → name → OperationDef
	repository map[OperationMode]map[string]OperationDef
}

// ──────────────────────────── 全局变量 ────────────────────────────

// GlobalRegistry 全局操作注册表实例
var GlobalRegistry = NewOperationRegistry()

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOperationRegistry 创建空的操作注册表
func NewOperationRegistry() *OperationRegistry {
	return &OperationRegistry{
		repository: make(map[OperationMode]map[string]OperationDef),
	}
}

// Register 注册操作定义。
// 对齐 Python OperationRegistry.register：幂等性检查（相同定义跳过）。
func (r *OperationRegistry) Register(def OperationDef) error {
	if def.Name == "" {
		return fmt.Errorf("操作名称不能为空")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.repository[def.Mode]; !ok {
		r.repository[def.Mode] = make(map[string]OperationDef)
	}

	// 幂等性检查：相同名称+模式跳过
	if _, ok := r.repository[def.Mode][def.Name]; ok {
		return nil
	}

	r.repository[def.Mode][def.Name] = def
	return nil
}

// GetOperationInfo 获取操作定义。
// 对齐 Python OperationRegistry.get_operation_info。
func (r *OperationRegistry) GetOperationInfo(name string, mode OperationMode) (OperationDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	modeMap, ok := r.repository[mode]
	if !ok {
		return OperationDef{}, false
	}
	def, ok := modeMap[name]
	return def, ok
}

// GetSupportedOperations 获取指定模式下所有已注册操作名称。
// 对齐 Python OperationRegistry.get_supported_operations：返回排序后的名称列表。
func (r *OperationRegistry) GetSupportedOperations(mode OperationMode) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	modeMap, ok := r.repository[mode]
	if !ok {
		return nil
	}
	names := make([]string, 0, len(modeMap))
	for name := range modeMap {
		names = append(names, name)
	}
	// 排序保证确定性
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[i] > names[j] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}
