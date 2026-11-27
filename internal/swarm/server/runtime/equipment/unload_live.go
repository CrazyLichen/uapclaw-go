package equipment

import (
	"sync"
)

// ──────────────────────────── 类型 ────────────────────────────

// EquipmentUnloadAdapter 运行会话设备卸载的宿主缝。
// Python: interface_deep.py unload_equipment_if_loaded（root fan-out 到会话子适配器）。
type EquipmentUnloadAdapter interface {
	// UnloadEquipmentIfLoaded 从运行会话卸载已加载的设备包。
	// 未加载为 no-op；失败上抛使 uninstall 不删除包目录。
	UnloadEquipmentIfLoaded(kind, packageID string) error
}

// ──────────────────────────── 全局变量 ────────────────────────────

// unloadAdapterMu 注入锁。
var unloadAdapterMu sync.RWMutex

// unloadAdapter 宿主注入的卸载适配器（nil = 无运行会话面，no-op）。
var unloadAdapter EquipmentUnloadAdapter

// ──────────────────────────── 导出函数 ────────────────────────────

// SetUnloadLiveAdapter 注入运行会话卸载适配器（server 侧启动时调用；
// 传 nil 清除）。对齐 Python adapter 属性探测（getattr(_adapter, ...)）。
func SetUnloadLiveAdapter(adapter EquipmentUnloadAdapter) {
	unloadAdapterMu.Lock()
	defer unloadAdapterMu.Unlock()
	unloadAdapter = adapter
}

// NewUnloadLiveFunc 构造 UnloadLiveFunc 真实现。
// Python: interface.py:6016-6026 _unload_live_equipment——adapter 缺失 no-op；
// 错误上抛（调用方中止删除）。
func NewUnloadLiveFunc() UnloadLiveFunc {
	return func(kind, packageID string) error {
		unloadAdapterMu.RLock()
		adapter := unloadAdapter
		unloadAdapterMu.RUnlock()
		if adapter == nil {
			return nil
		}
		return adapter.UnloadEquipmentIfLoaded(kind, packageID)
	}
}
