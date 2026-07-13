package resources_manager

import (
	"fmt"
	"sync"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SysOperationMgr 系统操作资源管理器。
//
// 对应 Python: SysOperationMgr (openjiuwen/core/runner/resources_manager/sys_operation_manager.py)
type SysOperationMgr struct {
	// sysOperations 系统操作实例注册表
	sysOperations *ThreadSafeDict[string, sysop.SysOperation]
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
		sysOperations:      NewThreadSafeDict[string, sysop.SysOperation](),
		sandboxKeyOwnerMap: make(map[string]string),
	}
}

// AddSysOperation 注册系统操作。
//
// 校验流程：
//  1. 校验 sysOperationID 非空且不重复
//  2. 校验 instance 非空
//  3. 校验 isolation_key_template 唯一性（sandboxKeyOwnerMap 查重）
//  4. 写入 sysOperations 和 sandboxKeyOwnerMap
//
// 对应 Python: SysOperationMgr.add_sys_operation(sys_operation_id, sys_operation_instance)
func (m *SysOperationMgr) AddSysOperation(sysOperationID string, instance sysop.SysOperation) error {
	if sysOperationID == "" {
		return fmt.Errorf("sys_operation_id 不能为空")
	}
	if instance == nil {
		return fmt.Errorf("instance 不能为空，sys_operation_id=%s", sysOperationID)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查是否已存在
	if m.sysOperations.Contains(sysOperationID) {
		return fmt.Errorf("sys_operation_id=%s 已存在", sysOperationID)
	}

	// 校验 isolation_key_template 唯一性
	if tpl := instance.IsolationKeyTemplate(); tpl != "" {
		if ownerID, exists := m.sandboxKeyOwnerMap[tpl]; exists {
			return fmt.Errorf("isolation_key_template=%s 已被 sys_operation_id=%s 占用", tpl, ownerID)
		}
		m.sandboxKeyOwnerMap[tpl] = sysOperationID
	}

	m.sysOperations.Set(sysOperationID, instance)
	return nil
}

// RemoveSysOperation 注销系统操作。
//
// 校验流程：
//  1. 校验 sysOperationID 非空
//  2. 从 sysOperations 弹出实例
//  3. 清理 sandboxKeyOwnerMap 中该实例的 isolation_key_template
//
// 对应 Python: SysOperationMgr.remove_sys_operation(sys_operation_id)
func (m *SysOperationMgr) RemoveSysOperation(sysOperationID string) (sysop.SysOperation, error) {
	if sysOperationID == "" {
		return nil, fmt.Errorf("sys_operation_id 不能为空")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.sysOperations.Contains(sysOperationID) {
		return nil, fmt.Errorf("sys_operation_id=%s 不存在", sysOperationID)
	}
	instance := m.sysOperations.Pop(sysOperationID)

	// 清理 sandboxKeyOwnerMap
	if tpl := instance.IsolationKeyTemplate(); tpl != "" {
		delete(m.sandboxKeyOwnerMap, tpl)
	}

	return instance, nil
}

// GetSysOperation 获取系统操作。
//
// 校验流程：
//  1. 校验 sysOperationID 非空
//  2. 从 sysOperations 查询并返回
//
// 对应 Python: SysOperationMgr.get_sys_operation(sys_operation_id)
func (m *SysOperationMgr) GetSysOperation(sysOperationID string) (sysop.SysOperation, error) {
	if sysOperationID == "" {
		return nil, fmt.Errorf("sys_operation_id 不能为空")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.sysOperations.Contains(sysOperationID) {
		return nil, fmt.Errorf("sys_operation_id=%s 不存在", sysOperationID)
	}
	instance := m.sysOperations.Get(sysOperationID)
	if instance == nil {
		return nil, fmt.Errorf("sys_operation_id=%s 不存在", sysOperationID)
	}

	return instance, nil
}

// GetSysOperationByIsolationKey 按隔离键模板查找已注册的 SysOperation。
// 对齐 Python: SysOperationMgr._sandbox_key_owner_map[key] → get_sys_operation(op_id)
func (m *SysOperationMgr) GetSysOperationByIsolationKey(key string) (sysop.SysOperation, error) {
	if key == "" {
		return nil, fmt.Errorf("隔离键模板为空")
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	opID, ok := m.sandboxKeyOwnerMap[key]
	if !ok {
		return nil, fmt.Errorf("未找到隔离键 %q 对应的 SysOperation", key)
	}
	instance := m.sysOperations.Get(opID)
	if instance == nil {
		return nil, fmt.Errorf("隔离键 %q 对应的 SysOperationID=%s 不存在", key, opID)
	}
	return instance, nil
}
