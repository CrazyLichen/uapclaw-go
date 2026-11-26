// SessionMemoryAgent：经 ReActAgent 更新会话记忆笔记的 forked 压缩执行体。
// 对应 Python 代码：openjiuwen/core/context_engine/processor/forked/compressor/session_memory_agent.py（260 行）
package compressor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	cecontext "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/context"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor/forked/compressor/support"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/ability"
	saagents "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/agents"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/sys_operation"
	filesystemtools "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/filesystem"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sessionMemoryLogComponent 日志组件
	sessionMemoryLogComponent = logger.ComponentAgentCore

	// SessionMemoryDebugDumpDirEnv 调试落盘目录环境变量（OPENUAPCLAW_* 前缀，D-24.15-b2 同族）
	// Python: session_memory_agent.py:213 (_dump_debug_dir env OPENJIUWEN_SESSION_MEMORY_DEBUG_DUMP_DIR)
	SessionMemoryDebugDumpDirEnv = "OPENUAPCLAW_SESSION_MEMORY_DEBUG_DUMP_DIR"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionMemoryAgentConfig agent_edit 执行体配置。
// Python: session_memory_agent.py:19 (SessionMemoryAgentConfig)
type SessionMemoryAgentConfig struct {
	// MaxIterations ReAct 最大迭代（1-3）
	// Python: Field(default=3, ge=1, le=3)
	MaxIterations int `json:"max_iterations"`
	// EnableDebugDump 调试落盘开关
	EnableDebugDump bool `json:"enable_debug_dump"`
	// DebugDumpDir 调试目录（空走环境变量/工作目录回退）
	DebugDumpDir string `json:"debug_dump_dir"`
}

// NewSessionMemoryAgentConfig 创建配置，字段使用 Python 默认值。
func NewSessionMemoryAgentConfig() *SessionMemoryAgentConfig {
	return &SessionMemoryAgentConfig{MaxIterations: 3}
}

// Validate 校验配置。
func (c *SessionMemoryAgentConfig) Validate() error {
	if c.MaxIterations < 1 || c.MaxIterations > 3 {
		return fmt.Errorf("max_iterations 必须在 [1,3] 区间，当前值: %d", c.MaxIterations)
	}
	return nil
}

// SessionMemoryAbilityManager 受控能力管理器：稳定暴露模型工具 + 宿主侧执行策略。
// Python: session_memory_agent.py:25 (SessionMemoryAbilityManager(AbilityManager))
type SessionMemoryAbilityManager struct {
	ability.AbilityManager
	// modelTools 恒定暴露给模型的工具信息
	modelTools []commonschema.ToolInfoInterface
	// allowedNotesPath 允许编辑的笔记文件绝对路径
	allowedNotesPath string
	// rejectedToolCalls 被拒工具调用记账
	rejectedToolCalls []map[string]any
}

// NewSessionMemoryAbilityManager 创建受控管理器。
// Python: session_memory_agent.py:28 (__init__)
func NewSessionMemoryAbilityManager(ownerID string) *SessionMemoryAbilityManager {
	return &SessionMemoryAbilityManager{
		AbilityManager: *ability.NewAbilityManagerWithOwner(runner.GetResourceMgr(), ownerID),
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetModelTools 设置恒定暴露的工具信息。
// Python: session_memory_agent.py:34 (set_model_tools)
func (m *SessionMemoryAbilityManager) SetModelTools(tools []commonschema.ToolInfoInterface) {
	m.modelTools = append([]commonschema.ToolInfoInterface(nil), tools...)
}

// SetAllowedNotesPath 设置允许编辑的笔记路径（绝对化）。
// Python: session_memory_agent.py:37 (set_allowed_notes_path)
func (m *SessionMemoryAbilityManager) SetAllowedNotesPath(path string) {
	if path == "" {
		m.allowedNotesPath = ""
		return
	}
	if abs, err := filepath.Abs(path); err == nil {
		m.allowedNotesPath = abs
	} else {
		m.allowedNotesPath = path
	}
}

// ListToolInfo 恒定返回配置的工具信息（忽略名称过滤）。
// Python: session_memory_agent.py:40 (list_tool_info)
func (m *SessionMemoryAbilityManager) ListToolInfo(_ context.Context, _ []string, _ ...string) ([]commonschema.ToolInfoInterface, error) {
	return append([]commonschema.ToolInfoInterface(nil), m.modelTools...), nil
}

// DebugSnapshot 调试快照。
// Python: session_memory_agent.py:48 (debug_snapshot)
func (m *SessionMemoryAbilityManager) DebugSnapshot() map[string]any {
	modelTools := make([]any, 0, len(m.modelTools))
	for _, item := range m.modelTools {
		modelTools = append(modelTools, item)
	}
	allowed := m.allowedNotesPath
	return map[string]any{
		"rejected_tool_calls": m.rejectedToolCalls,
		"model_tools":         modelTools,
		"allowed_notes_path":  allowed,
	}
}

// Execute 宿主侧执行策略：仅放行 edit_file 且目标为待写笔记文件。
// Python: session_memory_agent.py:63 (execute)
func (m *SessionMemoryAbilityManager) Execute(
	ctx context.Context,
	cbc *sainterfaces.AgentCallbackContext,
	toolCalls []*llm_schema.ToolCall,
	sess sessioninterfaces.SessionFacade,
	tag string,
	parallelToolCalls bool,
) []agentschema.ExecuteResult {
	var allowed []*llm_schema.ToolCall
	type rejectedEntry struct {
		index  int
		reason string
	}
	rejected := map[int]string{}
	for i, call := range toolCalls {
		if call.Name != "edit_file" {
			rejected[i] = "only edit_file is executable"
			m.rejectedToolCalls = append(m.rejectedToolCalls, map[string]any{
				"name": call.Name, "tool_call_id": call.ID,
				"reason": "only edit_file is executable",
			})
			continue
		}
		if !m.isAllowedEditPath(call.Arguments) {
			rejected[i] = "target path is not the pending notes file"
			m.rejectedToolCalls = append(m.rejectedToolCalls, map[string]any{
				"name": call.Name, "tool_call_id": call.ID,
				"reason": "target path is not the pending notes file",
			})
			continue
		}
		allowed = append(allowed, call)
	}

	allowedResults := []agentschema.ExecuteResult{}
	if len(allowed) > 0 {
		allowedResults = m.AbilityManager.Execute(ctx, cbc, allowed, sess, tag, parallelToolCalls)
	}
	allowedByID := map[string]agentschema.ExecuteResult{}
	for i, call := range allowed {
		if i < len(allowedResults) {
			allowedByID[call.ID] = allowedResults[i]
		}
	}
	// 按原顺序输出：拒绝项填策略拒绝 ToolMessage
	results := make([]agentschema.ExecuteResult, 0, len(toolCalls))
	allowedCursor := 0
	for i, call := range toolCalls {
		if reason, isRejected := rejected[i]; isRejected {
			results = append(results, agentschema.ExecuteResult{
				ToolMsg: llm_schema.NewToolMessage(call.ID,
					"Tool call rejected by SessionMemory policy. Only edit_file on the pending notes file is allowed. Reason: "+reason),
			})
			continue
		}
		if result, ok := allowedByID[call.ID]; ok {
			results = append(results, result)
			allowedCursor++
			continue
		}
		allowedCursor++
		results = append(results, agentschema.ExecuteResult{})
	}
	return results
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isAllowedEditPath 目标路径必须是待写笔记文件（绝对路径等值）。
// Python: session_memory_agent.py:122 (_is_allowed_edit_path)
func (m *SessionMemoryAbilityManager) isAllowedEditPath(arguments any) bool {
	if m.allowedNotesPath == "" {
		return false
	}
	var parsed map[string]any
	switch v := arguments.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &parsed); err != nil {
			return false
		}
	case map[string]any:
		parsed = v
	default:
		return false
	}
	requested, ok := parsed["file_path"].(string)
	if !ok || requested == "" {
		return false
	}
	abs, err := filepath.Abs(requested)
	if err != nil {
		return false
	}
	return abs == m.allowedNotesPath
}

// ──────────────────────────── 结构体 ────────────────────────────

// SessionMemoryAgent 经 ReActAgent 更新会话记忆笔记的 forked 压缩执行器。
// 实现 cecontext.SessionMemoryAgent 接口（Invoke + SetInheritedSystemPrompt）。
// Python: session_memory_agent.py:135 (SessionMemoryAgent(ForkedAgent))
type SessionMemoryAgent struct {
	support.ForkedAgent
	config          *SessionMemoryAgentConfig
	abilityMgr      *SessionMemoryAbilityManager
	workspaceRoot   string
	editTool        tool.Tool
	inheritedPrompt string
}

// NewSessionMemoryAgent 创建执行体。
// Python: session_memory_agent.py:138 (__init__)
func NewSessionMemoryAgent(
	model *llm.Model,
	modelConfig *llm_schema.ModelRequestConfig,
	modelClientConfig *llm_schema.ModelClientConfig,
	promptTemplate []map[string]any,
	config *SessionMemoryAgentConfig,
	workspaceRoot string,
) *SessionMemoryAgent {
	if config == nil {
		config = NewSessionMemoryAgentConfig()
	}
	if workspaceRoot != "" {
		if abs, err := filepath.Abs(workspaceRoot); err == nil {
			workspaceRoot = abs
		}
	}
	sma := &SessionMemoryAgent{
		ForkedAgent: *support.NewForkedAgent(
			model, modelConfig, modelClientConfig,
			"session_memory_update_agent",
			"Updates the session memory Markdown file.",
			promptTemplate,
			config.MaxIterations,
		),
		config:        config,
		workspaceRoot: workspaceRoot,
	}
	// Python: _configure_agent 覆盖（构造期注入，Go embedding 静态派发规避）
	sma.SetConfigureAgent(sma.configureAgent)
	return sma
}

// ──────────────────────────── 导出函数 ────────────────────────────

// AbilityManager 返回受控管理器（未装配为 nil）。
// Python: session_memory_agent.py:163 (ability_manager property)
func (sma *SessionMemoryAgent) AbilityManager() *SessionMemoryAbilityManager { return sma.abilityMgr }

// SetInheritedSystemPrompt 设置继承的系统提示词。
// Python: 继承提示经 prompt 模板注入；Go 存字段供 Invoke inputs 携带。
func (sma *SessionMemoryAgent) SetInheritedSystemPrompt(prompt string) {
	sma.inheritedPrompt = prompt
}

// ConfigureRequest 每请求注入工具信息与允许编辑路径。
// Python: session_memory_agent.py:188 (configure_request)
func (sma *SessionMemoryAgent) ConfigureRequest(tools []commonschema.ToolInfoInterface, allowedNotesPath string) error {
	if sma.abilityMgr == nil {
		return fmt.Errorf("SessionMemory ability manager is not initialized")
	}
	sma.abilityMgr.SetModelTools(tools)
	sma.abilityMgr.SetAllowedNotesPath(allowedNotesPath)
	return nil
}

// Invoke 经 ReActAgent 执行（cecontext.SessionMemoryAgent 实现）。
// Python: session_memory_agent.py:201 (invoke)
func (sma *SessionMemoryAgent) Invoke(ctx context.Context, inputs map[string]any, _ ...cecontext.SessionMemoryAgentOption) (map[string]any, error) {
	var result *support.CompressionResult
	var invokeErr error
	func() {
		defer func() {
			// Python: finally _dump_debug_request
			var payload map[string]any
			if result != nil && result.Response != nil {
				payload = map[string]any{"output": result.Response}
			}
			sma.dumpDebugRequest(payload, invokeErr)
		}()
		result, invokeErr = sma.InvokeAgent(ctx, sma.ForkedAgent.Model(), nil)
	}()
	if invokeErr != nil {
		return nil, invokeErr
	}
	out := map[string]any{}
	if result != nil && result.Response != nil {
		out["output"] = result.Response
	}
	if result != nil && result.Usage != nil {
		out["usage"] = result.Usage
	}
	return out, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// configureAgent agent 装配：受控管理器 + LOCAL 沙箱 sysop + EditFileTool。
// Python: session_memory_agent.py:166 (_configure_agent)
func (sma *SessionMemoryAgent) configureAgent(agent *saagents.ReActAgent) {
	cardID := sma.ForkedAgent.AgentCardID()
	manager := NewSessionMemoryAbilityManager(cardID)
	if sma.workspaceRoot != "" {
		workConfig := sysop.NewLocalWorkConfig()
		workConfig.SandboxRoot = []string{sma.workspaceRoot}
		workConfig.RestrictToSandbox = true
		card := sysop.NewSysOperationCard(
			sysop.WithSysOpMode(sysop.OperationModeLocal),
			sysop.WithSysOpWorkConfig(workConfig),
			sysop.WithSysOpCustomID(cardID+"_sysop"),
		)
		operation, err := sysop.NewSysOperation(card)
		if err == nil {
			editTool := filesystemtools.NewEditFileTool(operation, "cn", cardID)
			manager.AddAbility(editTool.Card(), editTool)
			sma.editTool = editTool
		}
	}
	agent.SetAbilityManager(manager)
	sma.abilityMgr = manager
}

// dumpDebugRequest 调试落盘（追踪不得打断压缩路径）。
// Python: session_memory_agent.py:213 (_dump_debug_request)
func (sma *SessionMemoryAgent) dumpDebugRequest(result map[string]any, err error) {
	if !sma.config.EnableDebugDump {
		return
	}
	directory := sma.config.DebugDumpDir
	if directory == "" {
		if envDir := os.Getenv(SessionMemoryDebugDumpDirEnv); envDir != "" {
			directory = envDir
		} else {
			directory = filepath.Join("context", "session_memory_debug_logs")
		}
	}
	if mkErr := os.MkdirAll(directory, 0o755); mkErr != nil {
		return
	}
	payload := map[string]any{
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"result":    result,
		"error":     errString(err),
		"agent": map[string]any{
			"max_iterations": sma.config.MaxIterations,
		},
	}
	if sma.abilityMgr != nil {
		payload["ability"] = sma.abilityMgr.DebugSnapshot()
	}
	data, marshalErr := json.MarshalIndent(payload, "", "  ")
	if marshalErr != nil {
		return
	}
	fileName := time.Now().UTC().Format("20060102T150405") + "_" +
		strings.ReplaceAll(uuid.New().String(), "-", "") + ".json"
	// 失败静默：追踪不得打断压缩路径（PY except Exception: return）
	_ = os.WriteFile(filepath.Join(directory, fileName), data, 0o644)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
