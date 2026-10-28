// ACP 运行时工具刷新（10.6.24d Task 6）：按客户端能力装卸 acp_output 六工具。
// 对齐 Python interface_deep.py L3023-3089 _refresh_acp_runtime_tools：
// 1) channel=acp 时移除默认文件/命令工具（避免与 IDE 桥接工具冲突）
// 2) 无条件移除已注册的 ACP 六工具（每次请求按能力重建）
// 3) 按 request_metadata.acp_client_capabilities 的 fs/terminal 能力注册子集
// 4) channel=acp 时输出工具快照日志

package adapter

import (
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// acpBlockedDefaultToolNames channel=acp 时移除的默认文件/命令工具名。
// Python: _ACP_BLOCKED_DEFAULT_TOOL_NAMES (interface_deep.py L256-264)
var acpBlockedDefaultToolNames = map[string]struct{}{
	"read_file":  {},
	"write_file": {},
	"edit_file":  {},
	"bash":       {},
	"code":       {},
}

// acpRuntimeToolNames ACP 六工具名（fs 2 + terminal 4）。
// Python: acp_tool_names (L3031-3038)
var acpRuntimeToolNames = []string{
	"read_text_file",
	"write_text_file",
	"create_terminal",
	"read_terminal_output",
	"wait_for_terminal_exit",
	"release_terminal",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// refreshAcpRuntimeTools 按客户端能力刷新当前请求的 ACP 工具。
// Python: _refresh_acp_runtime_tools (interface_deep.py L3023-3089)
func (d *DeepAdapter) refreshAcpRuntimeTools(config *runtimeConfig) {
	if config == nil {
		return
	}
	sessionID := config.SessionID
	requestID := config.RequestID
	channelID := config.ChannelID
	requestMetadata := config.RequestMetadata

	// instance 未初始化时 ability_manager 访问抛 AttributeError（Python）→
	// Go 显式守卫直接返回（等价降级：无注册表可操作）
	if d.instance == nil {
		return
	}
	abilityMgr := d.instance.AbilityManager()

	// Python L3039-3042: channel=acp → 移除默认文件/命令工具
	if channelID == "acp" {
		for _, existing := range abilityMgr.List() {
			if _, blocked := acpBlockedDefaultToolNames[existing.AbilityName()]; blocked {
				abilityMgr.Remove(existing.AbilityName())
			}
		}
	}
	// Python L3043-3045: 无条件移除已注册的 ACP 六工具（按能力重建）
	for _, existing := range abilityMgr.List() {
		if isAcpRuntimeToolName(existing.AbilityName()) {
			abilityMgr.Remove(existing.AbilityName())
		}
	}

	fsEnabled, terminalEnabled := acpRuntimeToolsEnabled(requestMetadata)
	hasRuntimeCapability := fsEnabled || terminalEnabled
	if shouldRegisterAcpRuntimeTools(channelID, requestID, sessionID, hasRuntimeCapability) {
		resourceMgr := runner.GetResourceMgr()
		for _, acpTool := range swarmtools.GetAcpOutputTools(sessionID, requestID) {
			name := acpTool.Card().Name
			switch name {
			case "read_text_file", "write_text_file":
				// fs 类工具需 fs 能力
				if !fsEnabled {
					continue
				}
			default:
				// terminal 类工具需 terminal 能力
				if !terminalEnabled {
					continue
				}
			}
			// Python L3062: Runner.resource_mgr.add_tool(tool)（重复静默失败）
			_ = resourceMgr.AddTool(acpTool)
			abilityMgr.Add(acpTool.Card())
		}
	}

	// Python L3065-3089: channel=acp 时输出工具快照日志
	if channelID == "acp" {
		abilityNames := collectRegisteredAbilityNames(d)
		acpRuntimeNames := selectRegisteredRuntimeToolNames(acpRuntimeToolNames, abilityNames)
		sorted := abilityNames
		sort.Strings(sorted)
		logger.Info(logComponent).
			Str("session_id", sessionID).
			Str("request_id", requestID).
			Bool("fs_enabled", fsEnabled).
			Bool("terminal_enabled", terminalEnabled).
			Strs("acp_runtime_tools", acpRuntimeNames).
			Int("ability_count", len(sorted)).
			Strs("abilities", sorted).
			Msg("[ACP] runtime tool snapshot")
	}
}

// acpRuntimeToolsEnabled 从请求 metadata 解析 ACP 客户端能力开关。
// 返回 (fs_enabled, terminal_enabled)。
// Python: _acp_runtime_tools_enabled (interface_deep.py L2897-2933, staticmethod)
func acpRuntimeToolsEnabled(requestMetadata map[string]any) (fsEnabled, terminalEnabled bool) {
	var caps map[string]any
	if requestMetadata != nil {
		if raw, ok := requestMetadata["acp_client_capabilities"].(map[string]any); ok {
			caps = raw
		}
	}
	if caps == nil {
		caps = map[string]any{}
	}

	// fs：True → 开；dict → readTextFile/writeTextFile 任一真值；其他 → 关
	switch fsRaw := caps["fs"].(type) {
	case bool:
		fsEnabled = fsRaw
	case map[string]any:
		fsEnabled = truthyAcpCap(fsRaw["readTextFile"]) || truthyAcpCap(fsRaw["writeTextFile"])
	}

	// terminal：True → 开；dict → create/output/waitForExit/release 任一真值；其他 → 关
	switch terminalRaw := caps["terminal"].(type) {
	case bool:
		terminalEnabled = terminalRaw
	case map[string]any:
		terminalEnabled = truthyAcpCap(terminalRaw["create"]) ||
			truthyAcpCap(terminalRaw["output"]) ||
			truthyAcpCap(terminalRaw["waitForExit"]) ||
			truthyAcpCap(terminalRaw["release"])
	}
	return fsEnabled, terminalEnabled
}

// shouldRegisterAcpRuntimeTools 判定是否注册 ACP 运行时工具。
// Python: _should_register_acp_runtime_tools (interface_deep.py L3232-3243, staticmethod)
func shouldRegisterAcpRuntimeTools(channelID, requestID, sessionID string, hasRuntimeCapability bool) bool {
	if channelID != "acp" {
		return false
	}
	if requestID == "" || sessionID == "" {
		return false
	}
	return hasRuntimeCapability
}

// collectRegisteredAbilityNames 收集已注册能力名集合（trim 后非空）。
// Python: _collect_registered_ability_names (interface_deep.py L3249-3255)
func collectRegisteredAbilityNames(d *DeepAdapter) []string {
	var abilityNames []string
	if d.instance == nil {
		return abilityNames
	}
	for _, card := range d.instance.AbilityManager().List() {
		abilityName := strings.TrimSpace(card.AbilityName())
		if abilityName != "" {
			abilityNames = append(abilityNames, abilityName)
		}
	}
	return abilityNames
}

// selectRegisteredRuntimeToolNames 按候选序筛选已注册的工具名。
// Python: _select_registered_runtime_tool_names (interface_deep.py L3257-3266)
func selectRegisteredRuntimeToolNames(runtimeToolCandidates []string, abilityNames []string) []string {
	registered := make(map[string]struct{}, len(abilityNames))
	for _, name := range abilityNames {
		registered[name] = struct{}{}
	}
	var selectedNames []string
	for _, name := range runtimeToolCandidates {
		if _, ok := registered[name]; ok {
			selectedNames = append(selectedNames, name)
		}
	}
	return selectedNames
}

// isAcpRuntimeToolName 判定是否 ACP 六工具名。
func isAcpRuntimeToolName(name string) bool {
	for _, n := range acpRuntimeToolNames {
		if n == name {
			return true
		}
	}
	return false
}

// truthyAcpCap ACP 能力子开关真值判定（Python bool() 语义：非 nil 非空即真，
// 但实际配置只会出现 bool；数字/字符串按 Python truthiness 等价处理）。
func truthyAcpCap(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0
	case int:
		return t != 0
	default:
		return true
	}
}
