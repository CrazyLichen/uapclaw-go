// Package mcp 内 state_store——MCP 连接状态存储读取侧。
// 对齐 Python state_store.py 读取侧全量（:1-250）+ _save 原语（:107-119）；
// 写侧 upsert_mcp_record/remove_mcp_record/set_mcp_state/mounts 消费方是
// MCP 连接章节 ⤵️（缺口 10.3.31g，本批未迁）。
package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// stateVersion state.json 版本（Python _STATE_VERSION）
	stateVersion = 1
	// stateFileName 状态文件名（Python _STATE_FILE）
	stateFileName = "state.json"
	// mcpRootDir 统一 MCP 数据根目录名（Python _MCP_ROOT）
	mcpRootDir = "mcp"
	// skillsSubDir MCP 捆绑技能根子目录（connected_mcp_skill_dirs 的 skills_root）
	skillsSubDir = "skills"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// stateMu state.json 变更串行锁；reload 读并发安全（读到旧快照最多得到
// "尚未连接"，下次 reload 重试即修正）。Python _lock 等价。
var stateMu sync.RWMutex

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// McpRoot <workspace>/mcp/ —— 统一 MCP 数据根。
// Python: _mcp_root（state_store.py:58-60）。
func McpRoot() string {
	return filepath.Join(workspace.WorkspaceDir(), mcpRootDir)
}

// StatePath state.json 全路径。
// Python: _state_path（:69-70）。
func StatePath() string {
	return filepath.Join(McpRoot(), stateFileName)
}

// ReadMcpState 供 get_mcp_servers 合并的公开读取（返回新 dict）。
// Python: read_mcp_state（:100-103）。
func ReadMcpState() map[string]any {
	stateMu.Lock()
	defer stateMu.Unlock()
	return loadState()
}

// GetMcpRecord 返回单个 MCP 的状态记录，缺失返回 nil。
// Python: get_mcp_record（:105-112）。
func GetMcpRecord(name string) map[string]any {
	n := strings.TrimSpace(name)
	if n == "" {
		return nil
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	cons, _ := loadState()["mcp"].(map[string]any)
	rec, _ := cons[n].(map[string]any)
	return rec
}

// ListConnectedMcps 视为存活（state ∈ {connected, connecting}）的 MCP 全集。
// connecting 也算：入口已持久化、重启重读（注册/凭据路径必须含 connecting）。
// Python: list_connected_mcps（:131-162）。
func ListConnectedMcps() []map[string]any {
	stateMu.Lock()
	defer stateMu.Unlock()
	cons, _ := loadState()["mcp"].(map[string]any)
	return filterByStates(cons, map[string]bool{"connected": true, "connecting": true})
}

// ListTrulyConnectedMcps 仅 state==connected（connecting 排除）——前端
// "connected" 徽标只反映完成注册的 MCP。Python: list_truly_connected_mcps（:164-183）。
func ListTrulyConnectedMcps() []map[string]any {
	stateMu.Lock()
	defer stateMu.Unlock()
	cons, _ := loadState()["mcp"].(map[string]any)
	return filterByStates(cons, map[string]bool{"connected": true})
}

// ListConnectingMcps 仅 state==connecting（连接进行中）——前端 "connecting"
// 徽标驱动。Python: list_connecting_mcps（:185-201）。
func ListConnectingMcps() []map[string]any {
	stateMu.Lock()
	defer stateMu.Unlock()
	cons, _ := loadState()["mcp"].(map[string]any)
	return filterByStates(cons, map[string]bool{"connecting": true})
}

// ListTuiEnabledMcps 存活且 TUI/全局默认 enabled=True 的 MCP 全集。
// TUI 渠道（根 adapter）加载 config.yaml enabled 与这些记录的并集；web 忽略
// enabled（按 chat.send 的 mcp 字段加载）。Python: list_tui_enabled_mcps（:203-223）。
func ListTuiEnabledMcps() []map[string]any {
	out := []map[string]any{}
	for _, rec := range ListConnectedMcps() {
		if enabled, ok := rec["enabled"].(bool); ok && enabled {
			out = append(out, rec)
		}
	}
	return out
}

// ListRegisteredMcps state ∈ {registered, connected} 的自定义 MCP 全集——
// mcp.list 需要让已注册（未连接）的也露出，用户才能点连接。
// Python: list_registered_mcps（:249-268）。
func ListRegisteredMcps() []map[string]any {
	stateMu.Lock()
	defer stateMu.Unlock()
	cons, _ := loadState()["mcp"].(map[string]any)
	return filterByStates(cons, map[string]bool{"registered": true, "connected": true})
}

// ConnectedMcpSkillDirs 连接中 MCP 的捆绑技能目录。
// Python: connected_mcp_skill_dirs（:225-246）：state.json 连接记录逐个取
// <workspace>/mcp/skills/<name>/，磁盘存在才返回——断连删除记录后技能随
// 下一次扫描消失；remote/stdio MCP（无捆绑技能）从不写目录，自然缺席。
// 返回 [{"name", "dir"}] 形态。
func ConnectedMcpSkillDirs() []map[string]string {
	skillsRoot := filepath.Join(McpRoot(), skillsSubDir)
	out := []map[string]string{}
	for _, rec := range ListConnectedMcps() {
		name := strings.TrimSpace(strOfAny(rec["name"]))
		if name == "" {
			continue
		}
		d := filepath.Join(skillsRoot, name)
		if info, err := os.Stat(d); err == nil && info.IsDir() {
			out = append(out, map[string]string{"name": name, "dir": d})
		}
	}
	// Python dict 顺序 = state.json 键序（json 保序）；Go map 无序 → 按 name
	// 排序保证确定性（扫描根顺序影响多根同名去重的先扫优先，须稳定）。
	sort.Slice(out, func(i, j int) bool { return out[i]["name"] < out[j]["name"] })
	return out
}

// SaveState 原子写 state.json（无 BOM、排序键保证 diff 稳定）。
// Python: _save（:107-119）。写侧 upsert/remove/set_state ⤵️10.3.31g；
// 本原语导出供写侧章节与测试复用。
func SaveState(state map[string]any) {
	root := McpRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		logger.Warn(logger.ComponentAgentServer).
			Str("path", root).
			Msg("[mcp.state] failed to ensure mcp dir")
		return
	}
	p := StatePath()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		logger.Warn(logger.ComponentAgentServer).Err(err).
			Msg("[mcp.state] failed to serialize state")
		return
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		logger.Warn(logger.ComponentAgentServer).Err(err).
			Str("path", p).
			Msg("[mcp.state] failed to write")
		return
	}
	if err := os.Rename(tmp, p); err != nil {
		logger.Warn(logger.ComponentAgentServer).Err(err).
			Str("path", p).
			Msg("[mcp.state] failed to write")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadState 读 state.json；容忍 BOM 与缺失/损坏文件。
// Python: _load（:82-99）：utf-8-sig 容忍旧工具（PowerShell Set-Content）写
// 的 BOM；解析失败/非 dict 回落空态并补默认键。
func loadState() map[string]any {
	p := StatePath()
	raw, err := os.ReadFile(p)
	if err != nil {
		return emptyState()
	}
	// utf-8-sig：剥前导 BOM
	text := strings.TrimPrefix(string(raw), "\ufeff")
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		logger.Warn(logger.ComponentAgentServer).Err(err).
			Str("path", p).
			Msg("[mcp.state] failed to read")
		return emptyState()
	}
	if data == nil {
		return emptyState()
	}
	if _, ok := data["version"]; !ok {
		data["version"] = stateVersion
	}
	if _, ok := data["mcp"]; !ok {
		data["mcp"] = map[string]any{}
	}
	if _, ok := data["mounts"]; !ok {
		data["mounts"] = map[string]any{}
	}
	return data
}

// emptyState 空态（Python _empty_state :75-77）。
func emptyState() map[string]any {
	return map[string]any{
		"version": stateVersion,
		"mcp":     map[string]any{},
		"mounts":  map[string]any{},
	}
}

// filterByStates 按 state 集合过滤并补 name 键（list_*_mcps 公共尾部）。
func filterByStates(cons map[string]any, states map[string]bool) []map[string]any {
	out := []map[string]any{}
	// 排序保证确定性（Python dict 保序；Go map 无序）
	names := make([]string, 0, len(cons))
	for name := range cons {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		c, ok := cons[name].(map[string]any)
		if !ok {
			continue
		}
		st := strOfAny(c["state"])
		if !states[st] {
			continue
		}
		rec := map[string]any{}
		for k, v := range c {
			rec[k] = v
		}
		rec["name"] = name
		out = append(out, rec)
	}
	return out
}

// strOfAny 宽松字符串化。
func strOfAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
