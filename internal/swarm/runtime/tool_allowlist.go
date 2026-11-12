package runtime

import (
	"errors"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 接口 ────────────────────────────

// ToolCard 一次工具发现返回的工具卡（只取过滤需要的名称）。
// Python: tool_allowlist.py:42-43 card.name in allowed
type ToolCard interface {
	// ToolName 工具名
	ToolName() string
}

// ToolInfo 一次工具信息查询返回的条目（只取过滤需要的名称）。
// Python: tool_allowlist.py:46-47 info.name in allowed
type ToolInfo interface {
	// ToolName 工具名
	ToolName() string
}

// ToolCall 一次工具调用请求（只取拒绝判断需要的名称）。
// Python: tool_allowlist.py:53 call.name not in allowed
type ToolCall interface {
	// ToolName 工具名
	ToolName() string
}

// ToolManager 允许白名单在最终能力边界上收窄的管理器。
// Python: tool_allowlist.py:29 manager（鸭子类型：list / list_tool_info / execute）
//
// Go 侧用接口表达上游的鸭子类型：三个能力加白名单标记的读写。
type ToolManager interface {
	// List 发现全部可用工具卡
	List() []ToolCard
	// ListToolInfo 查询全部工具信息
	ListToolInfo() ([]ToolInfo, error)
	// Execute 执行工具调用
	Execute(ctx any, toolCall any, session any) (any, error)
}

// ToolAllowlistHolder 记录已安装白名单的管理器。
// Python: tool_allowlist.py:35/61 manager.jiuwenswarm_tool_allowlist
type ToolAllowlistHolder interface {
	// ToolAllowlist 已安装的白名单（未安装为 nil）
	ToolAllowlist() map[string]struct{}
	// SetToolAllowlist 记录已安装的白名单
	SetToolAllowlist(allowed map[string]struct{})
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrToolAllowlistWildcard 显式白名单不得包含 "*"
	// Python: tool_allowlist.py:33-34 raise ValueError("an explicit tool allowlist must not contain '*'")
	ErrToolAllowlistWildcard = errors.New("an explicit tool allowlist must not contain '*'")
	// ErrToolAllowlistInstalled 白名单已安装
	// Python: tool_allowlist.py:35-36 raise ValueError("tool allowlist is already installed")
	ErrToolAllowlistInstalled = errors.New("tool allowlist is already installed")
	// ErrToolAllowlistDenied 调用越出白名单
	// Python: tool_allowlist.py:54-55 raise ValueError(f"Agent tool allowlist denied: ...")
	ErrToolAllowlistDenied = errors.New("Agent tool allowlist denied")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ApplyToolPolicyPrompt 工具策略提示（定义在 schema 包，见 schema/interrupt_payload.go）。
// Python: tool_allowlist.py:16-26 apply_tool_policy_prompt
var ApplyToolPolicyPrompt = schema.ApplyToolPolicyPrompt

// InstallToolAllowlist 收窄一个 Agent 面向模型的发现与执行能力。
// Python: tool_allowlist.py:29-61 install_tool_allowlist
//
// 上游是原地替换 manager 的 list / list_tool_info / execute 三个方法；
// Go 无猴子补丁，改为返回包装后的 manager 与原管理器的新视图。
func InstallToolAllowlist(manager ToolManager, holder ToolAllowlistHolder, toolNames []string) (ToolManager, error) {
	// Python:32-34 去重集合 + 拒绝 "*"
	allowed := make(map[string]struct{}, len(toolNames))
	for _, name := range toolNames {
		allowed[name] = struct{}{}
	}
	if _, wildcard := allowed["*"]; wildcard {
		return nil, ErrToolAllowlistWildcard
	}

	// Python:35-36 已安装则拒绝重复安装
	if holder != nil && holder.ToolAllowlist() != nil {
		return nil, ErrToolAllowlistInstalled
	}

	// Python:38-40 保留原始三方法（由包装器持有原 manager 即可）
	wrapped := &toolAllowlistManager{inner: manager, allowed: allowed}

	// Python:58-61 替换三方法并打标记
	if holder != nil {
		holder.SetToolAllowlist(allowed)
	}
	return wrapped, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toolAllowlistManager 白名单包装后的工具管理器。
// Python: tool_allowlist.py:42-57 limited_list / limited_list_tool_info / limited_execute
type toolAllowlistManager struct {
	// inner 原始管理器
	inner ToolManager
	// allowed 允许的工具名集合
	allowed map[string]struct{}
}

// List 只返回白名单内工具卡。
// Python: tool_allowlist.py:42-43 limited_list
func (m *toolAllowlistManager) List() []ToolCard {
	cards := m.inner.List()
	filtered := make([]ToolCard, 0, len(cards))
	for _, card := range cards {
		if _, ok := m.allowed[card.ToolName()]; ok {
			filtered = append(filtered, card)
		}
	}
	return filtered
}

// ListToolInfo 只返回白名单内工具信息。
// Python: tool_allowlist.py:45-47 limited_list_tool_info
func (m *toolAllowlistManager) ListToolInfo() ([]ToolInfo, error) {
	infos, err := m.inner.ListToolInfo()
	if err != nil {
		return nil, err
	}
	filtered := make([]ToolInfo, 0, len(infos))
	for _, info := range infos {
		if _, ok := m.allowed[info.ToolName()]; ok {
			filtered = append(filtered, info)
		}
	}
	return filtered, nil
}

// Execute 先校验全部调用都在白名单内，再交给原始执行。
// Python: tool_allowlist.py:49-56 limited_execute
//
// 上游接受单个调用或调用列表；Go 侧 toolCall 由调用方传入具体类型，
// 这里用类型断言同时支持单个与切片两种形状。
func (m *toolAllowlistManager) Execute(ctx any, toolCall any, session any) (any, error) {
	calls := flattenToolCalls(toolCall)
	if denied := m.deniedNames(calls); len(denied) > 0 {
		return nil, deniedError(denied)
	}
	return m.inner.Execute(ctx, toolCall, session)
}

// flattenToolCalls 把单个调用或调用切片统一为切片。
// Python: tool_allowlist.py:52 calls = tool_call if isinstance(tool_call, list) else [tool_call]
func flattenToolCalls(toolCall any) []ToolCall {
	if list, ok := toolCall.([]ToolCall); ok {
		return list
	}
	if single, ok := toolCall.(ToolCall); ok {
		return []ToolCall{single}
	}
	return nil
}

// deniedNames 返回越出白名单的工具名（保序）。
// Python: tool_allowlist.py:53 denied = [call.name for call in calls if call.name not in allowed]
func (m *toolAllowlistManager) deniedNames(calls []ToolCall) []string {
	denied := make([]string, 0, len(calls))
	for _, call := range calls {
		if _, ok := m.allowed[call.ToolName()]; !ok {
			denied = append(denied, call.ToolName())
		}
	}
	return denied
}

// deniedError 构造带工具名清单的拒绝错误。
// Python: tool_allowlist.py:55 f"Agent tool allowlist denied: {', '.join(denied)}"
func deniedError(denied []string) error {
	return errors.Join(ErrToolAllowlistDenied, errors.New(strings.Join(denied, ", ")))
}
