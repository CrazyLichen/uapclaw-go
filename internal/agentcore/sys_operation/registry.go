package sys_operation

import (
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OperationDef 操作定义，包含类型信息和工厂方法。
// Python: OperationDef：cls, description, name, mode, create_instance。
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
// Python: OperationRegistry：_repository mode → name → OperationDef。
// Go 不做包扫描（Python 的 _discover_package），改用 init() 显式注册。
type OperationRegistry struct {
	// mu 保护 repository
	mu sync.RWMutex
	// 仓库模式映射：mode → name → OperationDef
	repository map[OperationMode]map[string]OperationDef
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

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
// M-52: 对齐 Python OperationRegistry.register 完整语义——同定义跳过（幂等），
// 不同定义覆盖（OVERRIDE，docstring 明文记载用户可覆盖内置操作是设计特性）。
// Python dataclass __eq__ 全字段比较；Go 用 NewFunc（函数值 == 指针比较，
// 对齐 Python cls 身份比较）+ Description 全等判断同定义。
func (r *OperationRegistry) Register(def OperationDef) error {
	if def.Name == "" {
		return fmt.Errorf("操作名称不能为空")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.repository[def.Mode]; !ok {
		r.repository[def.Mode] = make(map[string]OperationDef)
	}

	// 幂等性检查：完全相同的定义（工厂+描述）跳过。
	// Go 函数值不可 == 比较（只能与 nil 比），用 reflect.ValueOf(...).Pointer()
	// 比较函数代码指针——对齐 Python dataclass __eq__ 的 cls 身份比较语义
	// （同一函数定义的两个闭包实例指针不同，视为不同定义，符合 Python 语义）。
	if existing, ok := r.repository[def.Mode][def.Name]; ok {
		if reflect.ValueOf(existing.NewFunc).Pointer() == reflect.ValueOf(def.NewFunc).Pointer() &&
			existing.Description == def.Description {
			return nil
		}
		// 不同定义 → 覆盖 + Warn 日志（对齐 Python 无条件 store 的 override 语义）
		logger.Warn(logComponent).
			Str("name", def.Name).
			Str("mode", def.Mode.String()).
			Msg("operation definition overridden by new registration")
	}

	r.repository[def.Mode][def.Name] = def
	return nil
}

// GetOperationInfo 获取操作定义。
// Python: OperationRegistry.get_operation_info。
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
// Python: OperationRegistry.get_supported_operations：返回排序后的名称列表。
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
	// M-51: 对齐 Python sorted(...)，sort.Strings 语义完全一致（升序字符串）
	sort.Strings(names)
	return names
}
