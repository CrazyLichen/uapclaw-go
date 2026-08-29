package rails

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常数 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStructuredAskUserTool 创建支持结构化选项的 AskUserTool 空壳实例。
// 自建 EXTENDED_INPUT_PARAMS schema（不再依赖 BuildToolCard/AskUserMetadataProvider），
// 对齐 Python: StructuredAskUserTool.__init__ 直接构建 ToolCard。
//
// 对齐 Python: StructuredAskUserTool(Tool) — jiuwenswarm/agents/harness/common/rails/ask_user_rail.py
func NewStructuredAskUserTool(language, agentID string) (tool.Tool, error) {
	// 自建 schema（对齐 Python: EXTENDED_INPUT_PARAMS_EN/CN）
	inputParams := buildExtendedInputParams(language)
	description := getStructuredDescription(language)

	// 对齐 Python: tool_id = f"ask_user_{agent_id}" if agent_id else f"ask_user_{uuid.uuid4().hex}"
	toolID := fmt.Sprintf("ask_user_%s", agentID)
	if agentID == "" {
		toolID = fmt.Sprintf("ask_user_%s", generateToolID())
	}

	card := &tool.ToolCard{
		BaseCard: schema.BaseCard{
			ID:          toolID,
			Name:        "ask_user",
			Description: description,
		},
		InputParams: inputParams,
	}

	// 空壳 invoke：返回空 map（对齐 Python StructuredAskUserTool.invoke → return {}）
	askUserTool, err := tool.NewMapFunction(
		card,
		func(_ context.Context, _ map[string]any) (map[string]any, error) {
			return map[string]any{}, nil
		},
		nil,
	)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "structured_ask_user_tool_create").
			Err(err).
			Msg("创建 StructuredAskUserTool 失败")
		return nil, fmt.Errorf("创建 StructuredAskUserTool 失败: %w", err)
	}

	logger.Info(logComponent).
		Str("event_type", "structured_ask_user_tool_create").
		Str("tool_id", card.ID).
		Msg("StructuredAskUserTool 创建成功")

	return askUserTool, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildExtendedInputParams 自建 EXTENDED_INPUT_PARAMS schema。
// 对齐 Python: EXTENDED_INPUT_PARAMS_EN / EXTENDED_INPUT_PARAMS_CN
//
// 顶层参数：
//   - query (string, required) — 问题文本
//   - questions (array, optional) — 结构化选项列表
//
// questions 条目（对象，必填=["question"]）:
//   - question (string, required) — 问题文本
//   - header (string, optional) — 短标签（最多 12 字符）
//   - options (array, optional) — 选项列表（2-4 项）
//   - multi_select (boolean, optional, default=false) — 是否多选
//
// options 条目（对象，必填=["label"]）:
//   - label (string, required) — 选项文本（1-5 个词）
//   - description (string, optional) — 选项说明
func buildExtendedInputParams(language string) []*schema.Param {
	// options item schema（对齐 Python: options.items）
	optionsItem := schema.NewObjectParam("", "", false, []*schema.Param{
		schema.NewStringParam("label", getLabelDesc(language), true),
		schema.NewStringParam("description", getOptionDescDesc(language), false),
	})

	// questions item schema（对齐 Python: _QUESTIONS_ITEM_SCHEMA）
	questionsItem := schema.NewObjectParam("", "", false, []*schema.Param{
		schema.NewStringParam("question", getQuestionDesc(language), true),
		schema.NewStringParam("header", getHeaderDesc(language), false),
		schema.NewArrayParam("options", getOptionsDesc(language), false, optionsItem),
		schema.NewBooleanParam("multi_select", getMultiSelectDesc(language), false, false),
	})

	// 顶层参数
	queryDesc := getQueryDesc(language)
	questionsArrayDesc := getQuestionsArrayDesc(language)

	return []*schema.Param{
		schema.NewStringParam("query", queryDesc, true),
		schema.NewArrayParam("questions", questionsArrayDesc, false, questionsItem),
	}
}

// getStructuredDescription 返回结构化 ask_user 工具描述。
// 对齐 Python: EXTENDED_DESCRIPTION_EN / EXTENDED_DESCRIPTION_CN
func getStructuredDescription(language string) string {
	if language == "en" {
		return "Interrupts execution and requests input from the user. " +
			"Supports two modes:\n" +
			"1. Plain query (free-text): pass only `query` — the user types their answer.\n" +
			"2. Structured questions (multi-choice): pass `query` + `questions` — " +
			"the user selects from predefined options. " +
			"Use `questions` when you want the user to choose between specific options " +
			"(e.g., 'Apply update' vs 'Skip'). Each question can have 2-4 options."
	}
	return "中断执行并向用户请求输入。支持两种模式：\n" +
		"1. 纯文本查询：只传 `query` —— 用户自由输入回答。\n" +
		"2. 结构化选项：传 `query` + `questions` —— 用户从预定义选项中选择。" +
		"当你希望用户在特定选项间做选择时（如「应用更新」vs「跳过」）使用 `questions`。" +
		"每个问题可提供 2-4 个选项。"
}

// 以下描述函数一比一复刻 Python 的 EXTENDED_INPUT_PARAMS_EN/CN 中的 description 字段。

func getQueryDesc(language string) string {
	if language == "en" {
		return "The question to present to the user (required)."
	}
	return "向用户展示的问题（必填）。"
}

func getQuestionsArrayDesc(language string) string {
	if language == "en" {
		return "Structured questions with selectable options. " +
			"Use this when you want the user to choose from predefined options " +
			"instead of typing free text. Each question must have 2-4 options. " +
			"The user can always select 'Other' for custom input."
	}
	return "带选项的结构化问题。当希望用户从预定义选项中选择而非自由输入时使用。" +
		"每个问题必须提供 2-4 个选项。用户始终可以选择「其他」进行自定义输入。"
}

func getQuestionDesc(language string) string {
	if language == "en" {
		return "The question to present to the user."
	}
	return "向用户展示的问题。"
}

func getHeaderDesc(language string) string {
	if language == "en" {
		return "A short label displayed as a chip/tag (max 12 chars)."
	}
	return "短标签（最多 12 字符）。"
}

func getOptionsDesc(language string) string {
	if language == "en" {
		return "Available choices for this question (2-4 items)."
	}
	return "问题的可选项（2-4 项）。"
}

func getLabelDesc(language string) string {
	if language == "en" {
		return "Display text for this option (1-5 words)."
	}
	return "选项显示文本（1-5 个词）。"
}

func getOptionDescDesc(language string) string {
	if language == "en" {
		return "Explanation of what this option means."
	}
	return "选项含义说明。"
}

func getMultiSelectDesc(language string) string {
	if language == "en" {
		return "Allow multiple selections instead of just one."
	}
	return "允许多选而非单选。"
}

// generateToolID 生成唯一工具 ID。
// 对齐 Python: uuid.uuid4().hex
func generateToolID() string {
	n, _ := rand.Int(rand.Reader, big.NewInt(0xFFFFFFFF))
	return fmt.Sprintf("%08x", n)
}
