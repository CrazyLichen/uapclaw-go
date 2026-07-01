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
// ⤵️ 预留：9.32 SysOperation 接口实现后回填。
// 当前仅定义结构体和方法签名，核心逻辑标记 ⤵️。
type SysOperationMgr struct {
	// sysOperations 系统操作实例注册表
	// ⤵️ 预留：9.32 实现后，any 替换为 *SysOperation
	sysOperations *ThreadSafeDict[string, any]
	// sandboxKeyOwnerMap 沙箱键模板到所有者 SysOperationID 的映射
	// Python: _sandbox_key_owner_map: dict[str, str]
	sandboxKeyOwnerMap map[string]string
	// mu 读写锁
	mu sync.RWMutex
}

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
// ⤵️ 预留：9.32 SysOperation 接口实现后回填。回填内容：
//  1. 校验 sysOperationID 非空且不重复
//  2. 校验 instance 非空
//  3. 校验 isolation_key_template 唯一性（sandboxKeyOwnerMap 查重）
//  4. 写入 sysOperations 和 sandboxKeyOwnerMap
//
// 对应 Python: SysOperationMgr.add_sys_operation(sys_operation_id, sys_operation_instance)
func (m *SysOperationMgr) AddSysOperation(sysOperationID string, instance any) error {
	// ⤵️ 预留：9.32 实现后回填上述逻辑
	return fmt.Errorf("sys operation manager not implemented, sys_operation_id=%s", sysOperationID)
}

// RemoveSysOperation 注销系统操作。
//
// ⤵️ 预留：9.32 SysOperation 接口实现后回填。回填内容：
//  1. 校验 sysOperationID 非空
//  2. 从 sysOperations 弹出实例
//  3. 清理 sandboxKeyOwnerMap 中该实例的 isolation_key_template
//
// 对应 Python: SysOperationMgr.remove_sys_operation(sys_operation_id)
func (m *SysOperationMgr) RemoveSysOperation(sysOperationID string) (any, error) {
	// ⤵️ 预留：9.32 实现后回填上述逻辑
	return nil, fmt.Errorf("sys operation manager not implemented, sys_operation_id=%s", sysOperationID)
}

// GetSysOperation 获取系统操作。
//
// ⤵️ 预留：9.32 SysOperation 接口实现后回填。回填内容：
//  1. 校验 sysOperationID 非空
//  2. 从 sysOperations 查询并返回
//
// 对应 Python: SysOperationMgr.get_sys_operation(sys_operation_id)
func (m *SysOperationMgr) GetSysOperation(sysOperationID string) (any, error) {
	// ⤵️ 预留：9.32 实现后回填上述逻辑
	return nil, fmt.Errorf("sys operation manager not implemented, sys_operation_id=%s", sysOperationID)
}
