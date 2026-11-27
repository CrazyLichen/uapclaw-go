package adapter

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/equipment"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// UnloadEquipmentIfLoaded 从本适配器及其全部会话子适配器卸载已加载的设备包。
// Python: interface_deep.py:3051-3104 unload_equipment_if_loaded——
// 根适配器扇出到会话子适配器；会话级子适配器只卸自己。
// 未加载为 no-op；卸载失败上抛（使 uninstall 不删包目录）。
// 本方法即 15.18 交付记录"unload 半区 ⤴️"的回收实现。
func (d *DeepAdapter) UnloadEquipmentIfLoaded(kind, packageID string) error {
	pid := strings.TrimSpace(packageID)
	if pid == "" {
		return nil
	}
	// 1. 根自身：external harness 绑定为 child 专属（Python _external_harness_binding
	// 在 session-scoped child 上），根级无绑定面 → 直接扇出。
	failed := ""
	d.sessionAdapters.mu.RLock()
	entries := make([]struct {
		sid     string
		adapter AgentAdapter
	}, 0, len(d.sessionAdapters.entries))
	for sid, adapter := range d.sessionAdapters.entries {
		entries = append(entries, struct {
			sid     string
			adapter AgentAdapter
		}{sid, adapter})
	}
	d.sessionAdapters.mu.RUnlock()
	for _, entry := range entries {
		typedChild, ok := entry.adapter.(*DeepAdapter)
		if !ok {
			continue
		}
		if typedChild.externalHarnessBinding == nil {
			continue
		}
		if kind != "agent_templates" ||
			typedChild.externalHarnessBinding.AgentTemplateName != pid {
			continue
		}
		// Python :3063-3070：stop_external_harness + 清绑定；失败上抛聚合
		var io *harnessproviders.HarnessIOAdapter
		if typedChild.externalHarnessState != nil {
			io = typedChild.externalHarnessState.IO
		}
		if err := StopExternalHarness(context.Background(),
			typedChild.externalHarnessBinding, io, entry.sid); err != nil {
			failed = fmt.Sprintf("kind=%s id=%s session=%s: %v", kind, pid, entry.sid, err)
			break
		}
		typedChild.externalHarnessBinding = nil
		typedChild.externalHarnessState = nil
	}
	if failed != "" {
		return fmt.Errorf("unload equipment failed: %s", failed)
	}
	logger.Info(logger.ComponentAgentServer).
		Str("kind", kind).Str("package_id", pid).
		Msg("[equipment] unload_equipment_if_loaded done")
	return nil
}

// 编译期断言：DeepAdapter 实现 equipment 卸载缝。
var _ equipment.EquipmentUnloadAdapter = (*DeepAdapter)(nil)
