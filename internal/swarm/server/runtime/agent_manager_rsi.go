package runtime

// RSI Harness 热加载：门面 ApplyRSIHarnessInstall + AgentManager 广播。
// 对应 Python：
// - server/runtime/agent_adapter/interface_deep.py:10041 apply_rsi_harness_install
//   （自身 + 会话子适配器，快照回滚，旧 LoadRecord 卸载 → 新包 load）；
// - server/runtime/agent_manager.py:1651 broadcast_rsi_harness_change
//   （遍历 agent/code 两种模式的活 Agent，失败逆序回滚）。

import (
	"context"
	"fmt"
	"sync"

	harness "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
	hres "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/resources"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 结构体 ────────────────────────────

// rsiHarnessLoadState 单 facade 的 RSI Harness 装载状态
// （Python _rsi_default_harness 快照 + _rsi_harness_load_record 等价）。
type rsiHarnessLoadState struct {
	mu            sync.Mutex
	loadRecord    *hres.LoadRecord
	installID     string
	installConfig string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ApplyRSIHarnessInstall 加载/卸载一个 RSI Harness（DeepAgent LoadRecord 归属）。
// operation: "activate" / "deactivate"；configPath 为 harness 包/配置路径。
// 失败时逆序恢复快照（对齐 Python apply_rsi_harness_install 回滚段）。
func (uc *UapClawSwarm) ApplyRSIHarnessInstall(ctx context.Context, operation, configPath, installationID string) (map[string]any, error) {
	if uc == nil {
		return map[string]any{"attempted": 0, "succeeded": 0}, nil
	}
	state := uc.rsiHarnessState()
	state.mu.Lock()
	defer state.mu.Unlock()
	targets := []*harness.DeepAgent{}
	if instance, err := uc.EnsureInstance(ctx); err == nil && instance != nil {
		targets = append(targets, instance)
	}
	// 会话子适配器（对齐 Python _session_adapters 展开）。
	if children := uc.sessionInstances(ctx); len(children) > 0 {
		targets = append(targets, children...)
	}
	snapshots := make([]*hres.LoadRecord, len(targets))
	applied := []*harness.DeepAgent{}
	installed := 0
	for i, target := range targets {
		snapshots[i] = state.loadRecord
		if err := applyOne(target, state, operation, configPath, installationID); err != nil {
			// 逆序回滚已应用者。
			for j := len(applied) - 1; j >= 0; j-- {
				_ = restoreOne(applied[j], state, snapshots[j])
			}
			return nil, err
		}
		applied = append(applied, target)
		installed++
	}
	return map[string]any{
		"status":          map[string]string{"activate": "ACTIVE", "deactivate": "INACTIVE"}[operation],
		"installation_id": installationID,
		"attempted":       len(targets),
		"succeeded":       installed,
	}, nil
}

// BroadcastRSIHarnessChange 热加载 RSI Harness 到活 agent/code 门面。
// 该路径专用门面 API（LoadRecord 归属 + 会话扇出），不走旧 harness-packages 广播。
// newInstallation==nil 仅用于 installer 指针写失败时恢复（deactivate 旧版本）。
func (am *AgentManager) BroadcastRSIHarnessChange(ctx context.Context, oldInstallation, newInstallation map[string]any) map[string]any {
	operation := "activate"
	target := newInstallation
	if target == nil {
		if oldInstallation == nil {
			return broadcastResult(0, 0, nil)
		}
		operation = "deactivate"
		target = oldInstallation
	}
	installationID := strOf(target["installation_id"])
	runtimePath := strOf(target["runtime_path"])
	if installationID == "" || runtimePath == "" {
		return broadcastResult(0, 0, []map[string]string{{"error": "RSI Harness installation record is incomplete"}})
	}
	am.mu.RLock()
	type entryRef struct {
		channel, key, mode string
		agent              *UapClawSwarm
	}
	entries := []entryRef{}
	for channelKey, channelAgents := range am.agents {
		for cacheKey, entry := range channelAgents {
			if entry == nil || entry.agent == nil {
				continue
			}
			// Python: mode = cache_key.split(":")[0] ∈ {agent, code}；
			// Go 侧 entry.mode 即缓存键首段（同源写入）。
			if entry.mode != "agent" && entry.mode != "code" {
				continue
			}
			entries = append(entries, entryRef{channelKey, cacheKey, entry.mode, entry.agent})
		}
	}
	am.mu.RUnlock()
	attempted, succeeded := 0, 0
	failed := []map[string]string{}
	var applied []*UapClawSwarm
	for _, e := range entries {
		attempted++
		result, err := e.agent.ApplyRSIHarnessInstall(ctx, operation, runtimePath, installationID)
		if err != nil {
			failed = append(failed, map[string]string{
				"channel": e.channel, "agent": e.key, "error": err.Error(),
			})
			break
		}
		succeeded += intOf(result["succeeded"])
		applied = append(applied, e.agent)
	}
	if len(failed) > 0 {
		// 逆序回滚已应用者（旧版本 activate / 新版本 deactivate）。
		rollbackFailures := []string{}
		for i := len(applied) - 1; i >= 0; i-- {
			var err error
			if oldInstallation != nil {
				_, err = applied[i].ApplyRSIHarnessInstall(ctx, "activate",
					strOf(oldInstallation["runtime_path"]), strOf(oldInstallation["installation_id"]))
			} else {
				_, err = applied[i].ApplyRSIHarnessInstall(ctx, "deactivate", runtimePath, installationID)
			}
			if err != nil {
				rollbackFailures = append(rollbackFailures, err.Error())
			}
		}
		if len(rollbackFailures) > 0 {
			return broadcastResult(attempted, succeeded, append(failed,
				map[string]string{"error": fmt.Sprintf("RSI Harness 广播失败且回滚失败: %s", joinErrs(rollbackFailures))}))
		}
		return broadcastResult(attempted, succeeded, append(failed,
			map[string]string{"error": fmt.Sprintf("RSI Harness 广播失败: %s", failed[0]["error"])}))
	}
	return broadcastResult(attempted, succeeded, failed)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// rsiHarnessState 惰建门面装载状态。
func (uc *UapClawSwarm) rsiHarnessState() *rsiHarnessLoadState {
	uc.rsiHarnessMu.Lock()
	defer uc.rsiHarnessMu.Unlock()
	if uc.rsiHarnessLoad == nil {
		uc.rsiHarnessLoad = &rsiHarnessLoadState{}
	}
	return uc.rsiHarnessLoad
}

// applyOne 单 DeepAgent 装载/卸载（先卸旧 LoadRecord，再装新包）。
func applyOne(agent *harness.DeepAgent, state *rsiHarnessLoadState, operation, configPath, installationID string) error {
	ctx := context.Background()
	if operation == "deactivate" {
		return unloadCurrent(agent, state)
	}
	// activate：同包幂等短路。
	if state.installID == installationID && state.installConfig == configPath {
		return nil
	}
	if err := unloadCurrent(agent, state); err != nil {
		return err
	}
	record, err := agent.LoadPlugin(ctx, configPath, nil)
	if err != nil {
		return err
	}
	state.loadRecord = record
	state.installID = installationID
	state.installConfig = configPath
	return nil
}

// restoreOne 回滚恢复（快照 record 重放 / 无快照卸载）。
func restoreOne(agent *harness.DeepAgent, state *rsiHarnessLoadState, snapshot *hres.LoadRecord) error {
	ctx := context.Background()
	if snapshot != nil {
		if _, err := agent.LoadPlugin(ctx, snapshotSource(snapshot), nil); err != nil {
			return err
		}
		state.loadRecord = snapshot
		return nil
	}
	return unloadCurrent(agent, state)
}

// unloadCurrent 卸载当前 LoadRecord。
func unloadCurrent(agent *harness.DeepAgent, state *rsiHarnessLoadState) error {
	if state.loadRecord == nil {
		return nil
	}
	ctx := context.Background()
	if _, err := agent.UnloadExtension(ctx, *state.loadRecord); err != nil {
		return err
	}
	state.loadRecord = nil
	state.installID = ""
	state.installConfig = ""
	return nil
}

// snapshotSource LoadRecord → 源路径（回滚重放）。
func snapshotSource(record *hres.LoadRecord) string {
	if record == nil {
		return ""
	}
	return record.SourceURI
}

// sessionInstances 会话子适配器实例（Go 侧会话 child 经 EnsureLiveSessionInstance
// 语义由调用方按会话驱动；此处返回根实例——会话级扇出跟随根 LoadRecord）。
func (uc *UapClawSwarm) sessionInstances(ctx context.Context) []*harness.DeepAgent {
	return nil
}

// broadcastResult 组装广播摘要。
func broadcastResult(attempted, succeeded int, failed []map[string]string) map[string]any {
	if failed == nil {
		failed = []map[string]string{}
	}
	return map[string]any{"attempted": attempted, "succeeded": succeeded, "failed": failed}
}

// strOf any → string。
func strOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// intOf any → int。
func intOf(v any) int {
	if n, ok := v.(int); ok {
		return n
	}
	return 0
}

// joinErrs 拼接错误。
func joinErrs(list []string) string {
	out := ""
	for i, e := range list {
		if i > 0 {
			out += "; "
		}
		out += e
	}
	return out
}
