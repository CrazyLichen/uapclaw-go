package resources_manager

import (
	"fmt"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SysOperationMgr 系统操作资源管理器。
//
// 对应 Python: SysOperationMgr (openjiuwen/core/runner/resources_manager/sys_operation_manager.py)
//
// ⤵️ 预留：等 SysOperation 类型实现后回填。
// 当前仅定义结构体和方法签名，核心逻辑标记 ⤵️。
type SysOperationMgr struct {
	// ⤵️ 预留：等 SysOperation 类型实现后回填
	sysOperations *ThreadSafeDict[string, any]
	// sandboxKeyOwnerMap 沙箱键到所有者的映射
	sandboxKeyOwnerMap map[string]string
	// mu 读写锁
	mu sync.RWMutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSysOperationMgr 创建系统操作资源管理器。
func NewSysOperationMgr() *SysOperationMgr {
	return &SysOperationMgr{
		sysOperations:      NewThreadSafeDict[string, any](),
		sandboxKeyOwnerMap: make(map[string]string),
	}
}

// AddSysOperation 注册系统操作。
//
// ⤵️ 预留：等 SysOperation 类型实现后回填
func (m *SysOperationMgr) AddSysOperation(sysOperationID string, instance any) error {
	// ⤵️ 预留：核心逻辑待 SysOperation 类型实现后回填
	return fmt.Errorf("sys operation manager not implemented, sys_operation_id=%s", sysOperationID)
}

// RemoveSysOperation 注销系统操作。
//
// ⤵️ 预留：等 SysOperation 类型实现后回填
func (m *SysOperationMgr) RemoveSysOperation(sysOperationID string) (any, error) {
	// ⤵️ 预留：核心逻辑待 SysOperation 类型实现后回填
	return nil, fmt.Errorf("sys operation manager not implemented, sys_operation_id=%s", sysOperationID)
}

// GetSysOperation 获取系统操作。
//
// ⤵️ 预留：等 SysOperation 类型实现后回填
func (m *SysOperationMgr) GetSysOperation(sysOperationID string) (any, error) {
	// ⤵️ 预留：核心逻辑待 SysOperation 类型实现后回填
	return nil, fmt.Errorf("sys operation manager not implemented, sys_operation_id=%s", sysOperationID)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
