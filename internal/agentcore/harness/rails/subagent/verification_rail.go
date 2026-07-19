package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VerificationRail 验证代理约束 Rail。
// 强制执行工具白名单和每轮约束提醒，包括工作空间范围守卫。
//
// 设计为叠加在 SysOperationRail 之上使用。
// SysOperationRail 注册所有文件系统和 Shell 工具；
// 本 Rail 限制验证代理实际可调用的工具子集，
// 并在每轮模型调用前重新注入约束提醒，
// 防止代理在长时间运行中忘记自身角色。
//
// 对齐 Python: VerificationRail (openjiuwen/harness/rails/subagent/verification_rail.py)
type VerificationRail struct {
	rails.DeepAgentRail
	// allowedTools 工具白名单
	allowedTools map[string]bool
	// pathToolArg 路径工具参数映射
	pathToolArg map[string]string
	// promptBuilder 系统提示词构建器引用
	promptBuilder saprompt.SystemPromptBuilderInterface
}

// VerificationRailOption 配置选项函数
type VerificationRailOption func(*VerificationRail)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// verificationRailPriority VerificationRail 优先级
	// 对齐 Python: VerificationRail.priority = 90
	// 在 SysOperationRail (100) 之后运行，此时工具已注册
	verificationRailPriority = 90

	// reminderSectionName 约束提醒节名称
	// 对齐 Python: _REMINDER_SECTION_NAME = "verification_reminder"
	reminderSectionName = "verification_reminder"

	// reminderSectionPriority 约束提醒节优先级
	// 对齐 Python: _REMINDER_PRIORITY = 95
	// 注入在 prompt 末尾附近
	reminderSectionPriority = 95
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// defaultVerificationAllowedTools 默认允许的工具集
	// 对齐 Python: VERIFICATION_ALLOWED_TOOLS
	// SysOperationRail 注册但不在白名单中的工具将被拦截
	defaultVerificationAllowedTools = map[string]bool{
		"read_file":   true,
		"bash":        true,
		"grep":        true,
		"glob":        true,
		"list_files":  true,
		"web_search":  true,
		"web_fetch":   true,
		"todo_create": true,
		"todo_list":   true,
		"todo_modify": true,
		"skill_tool":  true,
		"tool_search": true,
	}

	// defaultPathToolArg 路径工具参数映射
	// 对齐 Python: _PATH_TOOL_ARG
	// 将路径读取工具名映射到持有目标路径的 tool_args 键名
	// 用于工作空间范围守卫，在 SysOperation 层之前拦截超范围路径请求
	defaultPathToolArg = map[string]string{
		"list_files": "path",
		"read_file":  "file_path",
		"glob":       "path",
		"grep":       "path",
	}

	// 提示词一比一复刻 Python 原文，不做自行翻译

	// reminderEN 英文约束提醒
	// 对齐 Python: _REMINDER_EN
	reminderEN = "=== VERIFICATION AGENT — ACTIVE CONSTRAINTS ===\n" +
		"1. You CANNOT create, modify, or delete project files. Use /tmp only for ephemeral test scripts.\n" +
		"2. Every check MUST include a 'Command run' block with verbatim terminal output. " +
		"A check without a command block is a SKIP, not a PASS.\n" +
		"3. You MUST end your final response with exactly one of:\n" +
		"   VERDICT: PASS\n" +
		"   VERDICT: FAIL\n" +
		"   VERDICT: PARTIAL\n" +
		"   No markdown, no punctuation after the verdict word, no variation.\n" +
		"4. Reading code is NOT verification. Run commands and show actual output."

	// reminderCN 中文约束提醒
	// 对齐 Python: _REMINDER_CN
	reminderCN = "=== 验证代理 -- 当前约束 ===\n" +
		"1. 你不能创建、修改或删除项目文件。/tmp 仅可用于临时测试脚本。\n" +
		"2. 每项检查必须包含'执行命令'块，并逐字粘贴终端输出。没有命令块的检查视为跳过，而非 PASS。\n" +
		"3. 你必须以以下之一结束最终回复：\n" +
		"   VERDICT: PASS\n" +
		"   VERDICT: FAIL\n" +
		"   VERDICT: PARTIAL\n" +
		"   不加 Markdown，判决词后不加标点，不得有任何格式变体。\n" +
		"4. 阅读代码不等于验证。运行命令并展示实际输出。"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVerificationRail 创建 VerificationRail 实例。
//
// 对齐 Python: VerificationRail(allowed_tools=None)
func NewVerificationRail(opts ...VerificationRailOption) *VerificationRail {
	r := &VerificationRail{
		DeepAgentRail: *rails.NewDeepAgentRail(),
		allowedTools:  copyBoolMap(defaultVerificationAllowedTools),
		pathToolArg:   defaultPathToolArg,
	}
	r.WithPriority(verificationRailPriority)
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// WithAllowedTools 设置允许的工具集。
func WithAllowedTools(tools map[string]bool) VerificationRailOption {
	return func(r *VerificationRail) { r.allowedTools = tools }
}

// Init 初始化钩子：捕获 system_prompt_builder。
//
// 对齐 Python: VerificationRail.init(agent)
// Python L115-123: self._agent = agent; self.system_prompt_builder = agent.system_prompt_builder
func (r *VerificationRail) Init(agent agentinterfaces.BaseAgent) error {
	r.promptBuilder = agent.SystemPromptBuilder()
	logger.Info(logger.ComponentAgentCore).
		Int("allowed_tools_count", len(r.allowedTools)).
		Msg("[VerificationRail] 已初始化")
	return nil
}

// BeforeModelCall 模型调用前注入约束提醒 section。
//
// 注入逻辑：
// 1. promptBuilder 为 nil 时跳过
// 2. 构建约束提醒 PromptSection（双语内容）
// 3. 先移除旧 section 再添加新 section，避免重复累积
//
// 对齐 Python: VerificationRail.before_model_call(ctx)
// Python L125-163
func (r *VerificationRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.promptBuilder == nil {
		return nil
	}

	// 构建约束提醒 section
	// 对齐 Python L156-161:
	//   reminder = PromptSection(name=_REMINDER_SECTION_NAME, content={"en": _REMINDER_EN, "cn": _REMINDER_CN}, priority=_REMINDER_PRIORITY)
	section := saprompt.PromptSection{
		Name:     reminderSectionName,
		Content:  map[string]string{"en": reminderEN, "cn": reminderCN},
		Priority: reminderSectionPriority,
	}

	// 对齐 Python L161-162:
	//   self.system_prompt_builder.remove_section(_REMINDER_SECTION_NAME)
	//   self.system_prompt_builder.add_section(reminder)
	r.promptBuilder.RemoveSection(reminderSectionName)
	r.promptBuilder.AddSection(section)

	logger.Debug(logger.ComponentAgentCore).
		Str("language", r.promptBuilder.Language()).
		Msg("[VerificationRail] 已注入约束提醒 section")

	return nil
}

// BeforeToolCall 工具调用前执行白名单检查和工作空间范围守卫。
//
// 两个守卫按顺序运行：
// 1. 白名单 — 拒绝不在 VERIFICATION_ALLOWED_TOOLS 中的工具
// 2. 工作空间范围 — 对于接受文件系统路径的工具，
//    拒绝解析后路径超出配置工作空间根目录的调用，
//    提供清晰的说明而非让 SysOperation 层弹出晦涩的"Access denied"错误
//
// 对齐 Python: VerificationRail.before_tool_call(ctx)
// Python L165-233
func (r *VerificationRail) BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 对齐 Python L179-180: if ctx.extra.get("_skip_tool"): return
	if _, skip := cbc.Extra()["_skip_tool"]; skip {
		return nil
	}

	// 获取工具调用输入
	inputs := cbc.Inputs()
	toolCallInputs, ok := inputs.(*agentinterfaces.ToolCallInputs)
	if !ok || toolCallInputs == nil {
		return nil
	}

	toolName := toolCallInputs.ToolName

	// 对齐 Python L186-187: MCP 工具（mcp__*）无条件放行
	// 同一约定在 harness 其他地方用于 MCP 工具访问
	if strings.HasPrefix(toolName, "mcp__") {
		return nil
	}

	// ── 守卫 1：白名单检查 ──
	// 对齐 Python L189-196
	if !r.allowedTools[toolName] {
		sortedTools := sortedBoolKeys(r.allowedTools)
		errorMsg := fmt.Sprintf(
			"[VerificationAgent] Tool '%s' is not available to the verification agent. "+
				"Permitted tools: %s.",
			toolName, strings.Join(sortedTools, ", "),
		)
		logger.Info(logger.ComponentAgentCore).
			Str("tool_name", toolName).
			Msg("[VerificationRail] 已拦截工具")

		r.rejectTool(cbc, toolCallInputs, errorMsg)
		return nil
	}

	// ── 守卫 2：工作空间范围守卫 ──
	// 对齐 Python L198-233
	pathArgKey, hasPathArg := r.pathToolArg[toolName]
	if hasPathArg && r.Workspace() != nil {
		toolArgs := toolCallInputs.ToolArgs

		// 对齐 Python L203-208: tool_args 可能是 JSON 字符串，需要解析
		var args map[string]any
		if err := json.Unmarshal([]byte(toolArgs), &args); err != nil {
			// 无法解析，不做路径检查
			return nil
		}

		// 对齐 Python L209: raw_path = tool_args.get(path_arg_key) if isinstance(tool_args, dict) else None
		rawPath, _ := args[pathArgKey].(string)
		if rawPath == "" {
			return nil
		}

		// 对齐 Python L210-215: workspace_root 解析
		workspaceRoot := r.Workspace().RootPath

		// 对齐 Python L217-219: resolved = Path(raw_path).expanduser().resolve()
		resolved, err := filepath.Abs(filepath.Clean(rawPath))
		if err != nil {
			return nil
		}
		root, err := filepath.Abs(filepath.Clean(workspaceRoot))
		if err != nil {
			return nil
		}

		// 对齐 Python L219: if not (resolved == root or resolved.is_relative_to(root)):
		if !strings.HasPrefix(resolved, root+string(filepath.Separator)) && resolved != root {
			// 对齐 Python L220-225
			errorMsg := fmt.Sprintf(
				"[VerificationAgent] Path '%s' is outside the workspace scope "+
					"(workspace root: '%s'). Only paths within the workspace are accessible. "+
					"Use paths relative to '%s' or absolute paths within it.",
				rawPath, root, root,
			)
			logger.Info(logger.ComponentAgentCore).
				Str("raw_path", rawPath).
				Str("tool_name", toolName).
				Str("workspace_root", root).
				Msg("[VerificationRail] 已拦截超出工作空间范围的路径")

			r.rejectTool(cbc, toolCallInputs, errorMsg)
		}
	}

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// rejectTool 标记工具调用为跳过并注入错误结果。
//
// 对齐 Python: VerificationRail._reject_tool(ctx, error_msg)
// Python L235-248
func (r *VerificationRail) rejectTool(cbc *agentinterfaces.AgentCallbackContext, inputs *agentinterfaces.ToolCallInputs, errorMsg string) {
	// 对齐 Python L242-243: tool_call_id = tool_call.id if tool_call else ""
	toolCallID := ""
	if inputs.ToolCall != nil {
		toolCallID = inputs.ToolCall.ID
	}

	// 对齐 Python L244: msg = ToolMessage(content=error_msg, tool_call_id=tool_call_id)
	msg := llmschema.NewToolMessage(toolCallID, errorMsg)

	// 对齐 Python L245-247:
	//   ctx.extra["_skip_tool"] = True
	//   ctx.inputs.tool_result = {"error": error_msg}
	//   ctx.inputs.tool_msg = msg
	cbc.Extra()["_skip_tool"] = true
	inputs.ToolResult = map[string]any{"error": errorMsg}
	inputs.ToolMsg = msg
}

// copyBoolMap 复制 map[string]bool
func copyBoolMap(m map[string]bool) map[string]bool {
	result := make(map[string]bool, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// sortedBoolKeys 返回排序后的键列表
func sortedBoolKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// compile-time check
var _ agentinterfaces.AgentRail = (*VerificationRail)(nil)
