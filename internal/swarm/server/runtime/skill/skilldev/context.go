package skilldev

import (
	"context"
	"fmt"
	"strings"

	"errors"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/code"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/filesystem"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/shell"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StageHandler SkillDev Pipeline 阶段处理器接口。
// 每个阶段独立实现，通过 Execute() 与 Pipeline 交互。
// 处理器不应持有跨请求的状态——所有状态均通过 SkillDevContext 传入。
type StageHandler interface {
	// Execute 执行阶段逻辑。
	// 参数：
	//   - ctx:  上下文（控制生命周期）
	//   - sctx: SkillDevContext，包含 state、workspace、emit、create_stage_agent 等
	//
	// 返回值：
	//   - StageResult：Pipeline 据此跳转到下一阶段
	//   - error：执行错误
	Execute(ctx context.Context, sctx *SkillDevContext) (*StageResult, error)
}

// SkillDevContext 阶段执行上下文。
// Context 不是 Agent，它是每阶段 StageHandler 的运行环境：
//   - 持有 deps（外部依赖）和 state（运行时状态）的引用
//   - 提供 Emit() 向前端推送事件
//   - 提供 CreateStageAgent() 为当前阶段创建隔离的 ReActAgent
//
// 每阶段独立 Agent 的核心价值：
//   - 工具隔离：PLAN 只有搜索，GENERATE 才有文件写入
//   - Prompt 隔离：每阶段有焦点明确的专属 system prompt
//   - 内存隔离：阶段结束 Agent 即释放，无残留上下文
type SkillDevContext struct {
	// TaskID 任务标识
	TaskID string
	// Deps 外部依赖
	Deps *SkillDevDeps
	// State 运行时状态
	State *SkillDevState
	// Workspace 工作区路径
	Workspace string
	// eventQueue 事件队列（向 Pipeline 推送事件）
	eventQueue chan<- SkillDevEvent
	// stageSysOp 当前阶段 Agent 的系统操作（文件/命令工具依赖）
	stageSysOp sysop.SysOperation
}

// StageResult 阶段执行结果，由 Pipeline 读取以驱动状态跳转。
type StageResult struct {
	// NextStage 下一个跳转阶段
	NextStage SkillDevStage
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultStageMaxIterations 阶段 Agent 默认最大 ReAct 迭代次数
	// Python: create_stage_agent(max_iterations: int = 20)
	defaultStageMaxIterations = 20
	// stageToolLanguage 阶段工具卡片语言
	stageToolLanguage = "cn"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// stageToolNameAliases 阶段工具白名单别名 → 生产工具名。
// 阶段处理器沿用 Python 侧的工具命名（file_read/file_write/shell），
// Go 工具注册名为 read_file/write_file/bash，此处做一次显式映射。
// errStageToolOptional 表示可选工具（MCP 搜索等）当前不可用，注册时可跳过。
var errStageToolOptional = errors.New("可选阶段工具不可用")

var stageToolNameAliases = map[string]string{
	"file_read":  "read_file",
	"file_write": "write_file",
	"file_edit":  "edit_file",
	"shell":      "bash",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillDevContext 创建新的阶段执行上下文。
func NewSkillDevContext(
	taskID string,
	deps *SkillDevDeps,
	state *SkillDevState,
	workspace string,
	eventQueue chan<- SkillDevEvent,
) *SkillDevContext {
	return &SkillDevContext{
		TaskID:     taskID,
		Deps:       deps,
		State:      state,
		Workspace:  workspace,
		eventQueue: eventQueue,
	}
}

// Emit 向前端推送一个事件（放入 Pipeline 的事件队列）。
func (c *SkillDevContext) Emit(eventType SkillDevEventType, payload map[string]any) {
	merged := make(map[string]any, len(payload)+1)
	merged["task_id"] = c.TaskID
	for k, v := range payload {
		merged[k] = v
	}
	event := SkillDevEvent{
		EventType: eventType,
		Payload:   merged,
		TaskID:    c.TaskID,
	}
	select {
	case c.eventQueue <- event:
	default:
		logger.Warn(logComponent).
			Str("task_id", c.TaskID).
			Str("event_type", string(eventType)).
			Msg("[SkillDevContext] event queue full, dropping event")
	}
}

// CreateStageAgent 为当前阶段创建隔离的 ReActAgent。
// 参数：
//   - stageName:     阶段标识，用于 agent 命名（调试/日志用）
//   - systemPrompt:  该阶段专属的 system prompt
//   - tools:         工具名白名单，如 ["file_read", "file_write", "web_search"]
//   - maxIterations: ReAct 最大循环次数
//
// 返回：配置完毕的 ReActAgent 实例（尚未执行）
// Python: SkillDevContext.create_stage_agent()（jiuwenswarm 侧仍为 NotImplementedError 桩）
// Go 先行实现说明：上游 create_stage_agent 只留了注释级伪代码（AgentCard +
// ReActAgentConfig(model_name/model_client_config/max_iterations/prompt_template) +
// _register_tools），本实现按该伪代码落地，「工具隔离」语义保持不变。
// 上游落地后需按实际实现回归对齐。
func (c *SkillDevContext) CreateStageAgent(
	stageName string,
	systemPrompt string,
	tools []string,
	maxIterations int,
) (*agents.ReActAgent, error) {
	if c.Deps == nil {
		return nil, fmt.Errorf("阶段依赖 deps 未注入，无法创建阶段 Agent")
	}
	if strings.TrimSpace(c.Deps.ModelName) == "" {
		return nil, fmt.Errorf("阶段模型名称为空，无法创建阶段 Agent")
	}
	if maxIterations <= 0 {
		maxIterations = defaultStageMaxIterations
	}

	clientConfig, err := buildStageModelClientConfig(c.Deps.ModelClientConfig)
	if err != nil {
		logger.Error(logComponent).
			Str("stage", stageName).
			Err(err).
			Msg("[SkillDevContext] create_stage_agent: invalid model client config")
		return nil, fmt.Errorf("阶段 Agent 模型配置无效: %w", err)
	}

	agentID := fmt.Sprintf("skilldev_%s_%s", c.TaskID, stageName)
	card := agentschema.NewAgentCard(
		agentschema.WithAgentID(agentID),
		agentschema.WithAgentName(agentID),
		agentschema.WithAgentDescription(fmt.Sprintf("SkillDev %s stage agent", stageName)),
	)

	cfg := saconfig.NewReActAgentConfig(
		saconfig.WithModelName(c.Deps.ModelName),
		saconfig.WithModelClientConfig(clientConfig),
		saconfig.WithModelRequestConfig(&llmschema.ModelRequestConfig{ModelName: c.Deps.ModelName}),
		saconfig.WithMaxIterations(maxIterations),
		saconfig.WithPromptTemplate([]map[string]any{{"role": "system", "content": systemPrompt}}),
	)

	agent := agents.NewReActAgent(card, cfg)

	// 系统提示词与上下文引擎：NewReActAgent 不会自动创建 ContextEngine，
	// 缺省时 initContext 返回 nil → 用户消息不会进入模型上下文（prompt 为空）。
	agent.AddPromptBuilderSection("identity", systemPrompt, 10)
	ceConfig := ceschema.NewContextEngineConfig()
	ceConfig.MaxContextMessageNum = 200
	if cfg.ContextEngineConfig.MaxContextMessageNum > 0 {
		ceConfig = cfg.ContextEngineConfig
	}
	var ceOpts []ceinterface.ContextEngineOption
	if sysOpCard := c.Deps.SysOpConfig; sysOpCard != nil {
		if op, opErr := sysop.NewSysOperation(sysOpCard); opErr == nil {
			ceOpts = append(ceOpts, ceinterface.WithEngineSysOperation(op))
		}
	}
	agent.SetContextEngine(context_engine.NewContextEngine(ceConfig, ceOpts...))

	// 预构造模型注入：ReActAgent.getLLM 只读扁平字段（ModelProvider/APIKey/APIBase）且不传
	// client_id，无法表达 timeout/重试/SSL/自定义头；阶段 Agent 以 model_client_config 为准，
	// 这里直接构造 Model 后注入，避免再走扁平字段回退。
	modelCfg := llmschema.NewModelRequestConfig()
	modelCfg.ModelName = c.Deps.ModelName
	model, modelErr := llm.NewModel(clientConfig, modelCfg)
	if modelErr != nil {
		logger.Error(logComponent).
			Str("stage", stageName).
			Err(modelErr).
			Msg("[SkillDevContext] create_stage_agent: build model failed")
		return nil, fmt.Errorf("阶段 Agent 模型创建失败: %w", modelErr)
	}
	agent.SetLLM(model)

	// 阶段 Agent 独立持有 SysOperation：文件/命令工具按需构造，避免跨阶段串用。
	_, sysOpID, err := c.ensureStageSysOperation(stageName)
	if err != nil {
		return nil, err
	}
	cfg.SysOperationID = sysOpID

	if err := c.RegisterTools(agent, tools); err != nil {
		return nil, err
	}

	logger.Info(logComponent).
		Str("task_id", c.TaskID).
		Str("stage", stageName).
		Strs("tools", tools).
		Int("max_iterations", maxIterations).
		Str("sys_operation_id", sysOpID).
		Msg("[SkillDevContext] stage agent created")
	return agent, nil
}

// RegisterTools 根据工具名白名单将工具注册到 Agent。
// 工具来源：
//   - 文件类（file_read/file_write/file_edit）→ 由阶段 SysOperation 构造
//   - shell（bash）与 code → 由阶段 SysOperation 构造
//   - 其余名称 → 从 deps.MCPToolsFactory() 的 MCP 工具中按名称筛选
//
// Python: SkillDevContext._register_tools(agent, tool_names)
func (c *SkillDevContext) RegisterTools(agent *agents.ReActAgent, toolNames []string) error {
	if len(toolNames) == 0 {
		return nil
	}
	if agent == nil {
		return fmt.Errorf("阶段 Agent 为 nil")
	}

	rm := runner.GetResourceMgr()
	for _, raw := range toolNames {
		toolName := resolveStageToolName(raw)
		if toolName == "" {
			continue
		}

		stageTool, err := c.buildStageTool(toolName, agent.Card().GetID())
		if errors.Is(err, errStageToolOptional) {
			logger.Warn(logComponent).
				Str("task_id", c.TaskID).
				Str("tool", toolName).
				Msg("[SkillDevContext] optional stage tool unavailable, skipping")
			continue
		}
		if err != nil {
			logger.Error(logComponent).
				Str("task_id", c.TaskID).
				Str("tool", raw).
				Err(err).
				Msg("[SkillDevContext] register tool failed")
			return err
		}

		if rm != nil {
			if addErr := rm.AddTool(stageTool); addErr != nil {
				// 工具 ID 已存在时复用已有实例（同进程多阶段共用同名工具）
				logger.Debug(logComponent).
					Str("tool", toolName).
					Err(addErr).
					Msg("[SkillDevContext] tool already registered, reusing existing instance")
			}
		}
		agent.AbilityManager().Add(stageTool.Card())

		logger.Debug(logComponent).
			Str("task_id", c.TaskID).
			Str("tool", toolName).
			Msg("[SkillDevContext] stage tool registered")
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildStageModelClientConfig 把 deps.ModelClientConfig（配置字典）转换为模型客户端配置。
// 键名对齐 config.yaml 的 models.default.model_client_config。
func buildStageModelClientConfig(raw map[string]any) (*llmschema.ModelClientConfig, error) {
	if raw == nil {
		return nil, fmt.Errorf("model_client_config 为空")
	}
	provider := stageCfgStr(raw, "client_provider")
	if provider == "" {
		provider = stageCfgStr(raw, "model_provider")
	}
	if provider == "" {
		return nil, fmt.Errorf("model_client_config 缺少 client_provider")
	}

	cfg := &llmschema.ModelClientConfig{
		ClientID:       stageCfgStr(raw, "client_id"),
		ClientProvider: provider,
		APIKey:         stageCfgStr(raw, "api_key"),
		APIBase:        stageCfgStr(raw, "api_base"),
		Timeout:        60,
		MaxRetries:     3,
		VerifySSL:      true,
	}
	if v, ok := raw["timeout"]; ok {
		if f, ok := stageCfgFloat(v); ok && f > 0 {
			cfg.Timeout = f
		}
	}
	if v, ok := raw["max_retries"]; ok {
		if i, ok := stageCfgInt(v); ok && i >= 0 {
			cfg.MaxRetries = i
		}
	}
	if v, ok := raw["verify_ssl"]; ok {
		if b, ok := v.(bool); ok {
			cfg.VerifySSL = b
		}
	}
	if cert := stageCfgStr(raw, "ssl_cert"); cert != "" {
		cfg.SSLCert = cert
	}
	if v, ok := raw["custom_headers"].(map[string]any); ok {
		headers := make(map[string]string, len(v))
		for key, val := range v {
			if s, ok := val.(string); ok {
				headers[key] = s
			}
		}
		cfg.CustomHeaders = headers
	}
	if cfg.ClientID == "" {
		cfg.ClientID = fmt.Sprintf("%s-stagedev", provider)
	}
	return cfg, nil
}

// resolveStageToolName 将阶段白名单名解析为生产工具名，空串表示忽略。
func resolveStageToolName(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return ""
	}
	if alias, ok := stageToolNameAliases[name]; ok {
		return alias
	}
	return name
}

// ensureStageSysOperation 为阶段创建独立的 SysOperation 并注册到资源管理器。
// deps.SysOpConfig 为空时返回 (nil, "", nil)：命令行/文件工具不可用时，
// 只允许纯对话阶段（如 PLAN 仅用 MCP 搜索工具）。
func (c *SkillDevContext) ensureStageSysOperation(stageName string) (sysop.SysOperation, string, error) {
	if c.Deps.SysOpConfig == nil {
		return nil, "", nil
	}

	op, err := sysop.NewSysOperation(c.Deps.SysOpConfig)
	if err != nil {
		logger.Error(logComponent).
			Str("task_id", c.TaskID).
			Str("stage", stageName).
			Err(err).
			Msg("[SkillDevContext] create sys_operation failed")
		return nil, "", fmt.Errorf("创建阶段 SysOperation 失败: %w", err)
	}
	c.stageSysOp = op

	// 阶段隔离：每个阶段独立的 SysOperation 标识，避免多阶段共用同一实例的沙箱/工作目录状态。
	sysOpID := fmt.Sprintf("skilldev_%s_%s", c.TaskID, stageName)
	c.Deps.SysOpConfig.ID = sysOpID
	if card := op.Card(); card != nil {
		card.ID = sysOpID
	}
	if rm := runner.GetResourceMgr(); rm != nil {
		if addErr := rm.AddSysOperation(sysOpID, op); addErr != nil {
			logger.Debug(logComponent).
				Str("sys_operation_id", sysOpID).
				Err(addErr).
				Msg("[SkillDevContext] sys_operation already registered, reusing existing instance")
		}
	}
	return op, sysOpID, nil
}

// buildStageTool 按工具名构造阶段工具实例。
// 文件/命令类工具依赖阶段 SysOperation；其余名称从 MCP 工具工厂筛选。
func (c *SkillDevContext) buildStageTool(toolName, agentID string) (tool.Tool, error) {
	switch toolName {
	case "read_file", "write_file", "edit_file", "bash", "code":
		if c.stageSysOp == nil {
			return nil, fmt.Errorf("阶段工具 %s 需要 SysOperation 配置，但 deps.SysOpConfig 为空", toolName)
		}
		switch toolName {
		case "read_file":
			return filesystem.NewReadFileTool(c.stageSysOp, stageToolLanguage, agentID, false), nil
		case "write_file":
			return filesystem.NewWriteFileTool(c.stageSysOp, stageToolLanguage, agentID), nil
		case "edit_file":
			return filesystem.NewEditFileTool(c.stageSysOp, stageToolLanguage, agentID), nil
		case "bash":
			return shell.NewBashTool(c.stageSysOp, stageToolLanguage, agentID,
				shell.NewPermissionConfig(shell.PermissionModeAuto, nil, nil)), nil
		default:
			return code.NewCodeTool(c.stageSysOp, stageToolLanguage, agentID), nil
		}
	}

	if c.Deps.MCPToolsFactory == nil {
		return nil, fmt.Errorf("%w: %s（MCP 工具工厂为空）", errStageToolOptional, toolName)
	}
	for _, mcpTool := range c.Deps.MCPToolsFactory() {
		if mcpTool == nil || mcpTool.Card() == nil {
			continue
		}
		if mcpTool.Card().GetName() == toolName {
			return mcpTool, nil
		}
	}
	return nil, fmt.Errorf("%w: %s（不在 MCP 工具列表中）", errStageToolOptional, toolName)
}

// stageCfgStr 读取字符串字段（缺失或类型不符返回空串）。
func stageCfgStr(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

// stageCfgFloat 把数值字段转换为 float64。
func stageCfgFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

// stageCfgInt 把数值字段转换为 int。
func stageCfgInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}
