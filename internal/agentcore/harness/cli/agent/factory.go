package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// CLI agent 的后端装配。
//
// Python: openjiuwen/harness/cli/agent/factory.py（774 行）
//
// ⏵ 实施范围（2026-11-27）：本文件先落低依赖部分——MCP 配置加载、默认
// skill 目录、CLI workspace 内容、AgentBackend 接口、LocalBackend 骨架、
// CreateBackend 工厂。create_agent 的全量 rails 装配（AskUserRail /
// ConfirmInterruptRail / SkillUseRail / ContextProcessorRail / MemoryRail /
// JiuwenMemoryRail / subagents / vision / audio / runtime_extensions 热加载）
// ⤵️ 15.6-15.9 内置 provider 落地后回填（它们依赖 provider 化的模型构造）。

// logComponent 本包的日志组件。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 常量 ────────────────────────────

// DefaultSkillDirs CLI agent 启动时扫描的 skill 根目录。
// 降序优先：两个目录有同名 skill 时取靠前的（更高优先级）。
// Python: factory.py:49-55 _DEFAULT_SKILL_DIRS
var DefaultSkillDirs = []string{
	"~/.openjiuwen/workspace/skills",
	"~/.claude/skills",
	"~/.codex/skills",
	"~/.jiuwenclaw/workspace/skills",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultSkillDirsSnapshot 返回默认 skill 目录的副本。
// Python: factory.py:112-127 _default_skill_dirs
func DefaultSkillDirsSnapshot() []string {
	return append([]string(nil), DefaultSkillDirs...)
}

// LoadMcpConfigs 从 ~/.openjiuwen/mcp.json 加载 MCP 服务器配置。
// Python: factory.py:334-396 _load_mcp_configs
//
// 文件格式遵循 Claude Code 约定：顶层 mcpServers → 名字 → 规格。
// transport 取 spec.transport（回退 client_type，再回退 stdio）；
// server_path 取 spec.url（回退 server_path）；params 收 command/args/
// env/cwd 并过滤 None；坏文件静默返回空。
func LoadMcpConfigs() []map[string]any {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	mcpPath := filepath.Join(home, ".openjiuwen", "mcp.json")
	content, err := os.ReadFile(mcpPath)
	if err != nil {
		return nil
	}
	var data struct {
		McpServers map[string]map[string]any `json:"mcpServers"`
	}
	if json.Unmarshal(content, &data) != nil {
		logger.Debug(logComponent).Str("path", mcpPath).
			Msg("failed to load MCP config")
		return nil
	}
	configs := make([]map[string]any, 0, len(data.McpServers))
	for name, spec := range data.McpServers {
		transport := "stdio"
		if t, ok := spec["transport"].(string); ok && t != "" {
			transport = t
		} else if t, ok := spec["client_type"].(string); ok && t != "" {
			transport = t
		}
		serverPath := ""
		if u, ok := spec["url"].(string); ok && u != "" {
			serverPath = u
		} else if p, ok := spec["server_path"].(string); ok {
			serverPath = p
		}
		configs = append(configs, map[string]any{
			"server_name":  name,
			"server_path":  serverPath,
			"client_type":  transport,
			"params":       filterNoneValues(mcpParams(spec)),
			"auth_headers": spec["auth_headers"],
		})
	}
	return configs
}

// GetCliContentBaseDir CLI 专属 workspace 内容文件的基目录。
// Python: factory.py:57-59 _get_cli_content_base_dir
// （__file__/prompts/workspace_content；Go 以源码目录定位）
func GetCliContentBaseDir() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "prompts", "workspace_content")
}

// LoadCliContent 加载 CLI 专属 workspace 内容。
// Python: factory.py:62-79 _load_cli_content
//
// 文件不存在时返回空串。
func LoadCliContent(language, filePath string) string {
	fullPath := filepath.Join(GetCliContentBaseDir(), language, filePath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return ""
	}
	return string(content)
}

// ──────────────────────────── AgentBackend ────────────────────────────

// AgentBackend 本地 / 远程 agent 执行的抽象。
// Python: factory.py:463-484 AgentBackend（Protocol，4 方法）
type AgentBackend interface {
	// Start 初始化后端（启动 Runner / 连接）
	Start(ctx context.Context) error
	// Stop 释放资源（停止 Runner / 断开）
	Stop(ctx context.Context) error
	// RunStreaming 执行 query 并流式吐 OutputSchema 块。
	// 通道元素具体化为 stream.OutputSchema（曾用 any，2026-11-27 审查
	// 裁决具体化：实际承载的就是块类型，any 会逼消费者做类型断言并
	// 静默丢弃非块项）。
	RunStreaming(ctx context.Context, query any, sessionID *string) (<-chan stream.OutputSchema, error)
	// Abort 中止当前运行中的查询
	Abort(ctx context.Context) error
}

// LocalBackend 直接调 SDK Runner 的后端（MVP）。
// Python: factory.py:664-748 LocalBackend
type LocalBackend struct {
	// cfg CLI 配置
	cfg CLIConfig
	// sessionID 会话标识（cli- + 8 位随机）
	sessionID string
	// loadedExtensions 已热加载的运行时扩展（Python: _loaded_extensions）
	loadedExtensions map[string]bool
	// agent / tracker 由 Start 装配（⤵️ create_agent 回填后接通）
	agent any
}

// NewLocalBackend 构造本地后端。
// Python: factory.py:667-672 __init__
func NewLocalBackend(cfg CLIConfig) *LocalBackend {
	return &LocalBackend{
		cfg:              cfg,
		sessionID:        "cli-" + randomID8(),
		loadedExtensions: map[string]bool{},
	}
}

// Start 创建 agent 并启动 Runner。
// Python: factory.py:674-677 start
//
// ⏵ create_agent 回填后把 agent/tracker 装配上。
func (b *LocalBackend) Start(_ context.Context) error {
	return nil
}

// Stop 停止 Runner。
// Python: factory.py:679-681 stop
func (b *LocalBackend) Stop(_ context.Context) error {
	return nil
}

// RunStreaming 流式执行查询。
// Python: factory.py:683-701 run_streaming
//
// query 可以是普通字符串（普通轮次）或 InteractiveInput（中断恢复）。
// ⏵ Runner.run_agent_streaming 回填后接通；当前返回空流。
func (b *LocalBackend) RunStreaming(_ context.Context, _ any, sessionID *string) (<-chan stream.OutputSchema, error) {
	ch := make(chan stream.OutputSchema)
	close(ch)
	return ch, nil
}

// Abort 请求 agent 中止当前任务循环。
// Python: factory.py:703-709 abort（失败吞掉）
func (b *LocalBackend) Abort(_ context.Context) error {
	return nil
}

// SessionID 返回后端会话标识。
func (b *LocalBackend) SessionID() string { return b.sessionID }

// CreateBackend 选择并实例化后端。
// Python: factory.py:756-774 create_backend
//
// server_url 非空时报错（远程模式 MVP 尚未支持）。
func CreateBackend(cfg CLIConfig) (*LocalBackend, error) {
	if cfg.ServerURL != "" {
		return nil, errNotImplemented(
			"RemoteBackend is not supported in the MVP. " +
				"Remove OPENJIUWEN_SERVER_URL to use local mode.")
	}
	return NewLocalBackend(cfg), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mcpParams 收集 command/args/env/cwd 四键（缺失跳过）。
// Python: factory.py:373-378 的 params 字典
func mcpParams(spec map[string]any) map[string]any {
	params := map[string]any{}
	for _, key := range []string{"command", "args", "env", "cwd"} {
		if value, exists := spec[key]; exists {
			params[key] = value
		}
	}
	return params
}

// filterNoneValues 剔除 nil 值。
// Python: factory.py:327-332 _filter_none_values
func filterNoneValues(data map[string]any) map[string]any {
	filtered := make(map[string]any, len(data))
	for key, value := range data {
		if value == nil {
			continue
		}
		filtered[key] = value
	}
	return filtered
}

// randomID8 生成 8 位十六进制随机串。
// Python: factory.py:671 uuid4().hex[:8]
func randomID8() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
}

// errNotImplemented 远程模式未实现的错误。
func errNotImplemented(message string) error {
	return &notImplementedError{message}
}

// notImplementedError 功能未实现。
type notImplementedError struct {
	message string
}

// Error 实现 error。
func (e *notImplementedError) Error() string { return e.message }

// 编译期断言
var _ AgentBackend = (*LocalBackend)(nil)
