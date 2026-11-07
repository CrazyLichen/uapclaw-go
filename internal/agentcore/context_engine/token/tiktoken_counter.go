package token

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/tiktoken-go/tokenizer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/support"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	common_schema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TiktokenCounter 基于 tiktoken-go/tokenizer 的 Token 计数器。
// 提供文本、消息列表和工具定义的 Token 计数能力。
// 初始化时按模型名称选定 encoding，初始化失败时降级为 len(text)//3。
// Python: openjiuwen/core/context_engine/token/tiktoken_counter.py (TiktokenCounter)
type TiktokenCounter struct {
	// enc tiktoken 编码器实例，初始化失败时为 nil
	enc tokenizer.Codec
	// model 构造时指定的模型名称
	model string
	// fallbackWarned 是否已输出降级警告（只警告一次）
	fallbackWarned bool
	// mu 保护 fallbackWarned 的互斥锁
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// defaultModel 默认模型名称（对齐 Python: TiktokenCounter(model="gpt-4")）
const defaultModel = "gpt-4"

// logComponent 日志组件常量
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// model2enc 模型名称到 tiktoken 编码名称的映射表。
// Python: TiktokenCounter._MODEL2ENC。
// tokenizer.ForModel() 内置了更完整的映射，此表用于明确对齐 Python 端的映射关系，
// 以及覆盖 ForModel 不支持的模型名（如 "text-embedding-3-small"/"text-embedding-3-large"）。
var model2enc = map[string]tokenizer.Encoding{
	"gpt-3.5-turbo":          tokenizer.Cl100kBase,
	"gpt-4":                  tokenizer.Cl100kBase,
	"gpt-4-turbo":            tokenizer.Cl100kBase,
	"gpt-4o":                 tokenizer.O200kBase,
	"gpt-4o-mini":            tokenizer.O200kBase,
	"text-embedding-ada-002": tokenizer.Cl100kBase,
	"text-embedding-3-small": tokenizer.Cl100kBase,
	"text-embedding-3-large": tokenizer.Cl100kBase,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTiktokenCounter 创建 TiktokenCounter 实例。
// model 为模型名称，用于选择对应的 encoding；空字符串默认使用 "gpt-4"。
// 初始化失败时 enc 为 nil，后续计数降级为 len(text)//3。
// Python: TiktokenCounter(model="gpt-4")
func NewTiktokenCounter(model string) *TiktokenCounter {
	if model == "" {
		model = defaultModel
	}

	tc := &TiktokenCounter{
		model: model,
	}

	// 优先使用 model2enc 映射表（对齐 Python）
	if encName, ok := model2enc[model]; ok {
		enc, err := tokenizer.Get(encName)
		if err == nil {
			tc.enc = enc
			return tc
		}
		logger.Warn(logComponent).Str("model", model).
			Str("encoding", string(encName)).
			Err(err).
			Msg("Tiktoken encoder init failed (by mapping table), falling back to len(text)//3 counting")
		return tc
	}

	// 其次尝试 tokenizer.ForModel（利用其内置映射 + 前缀匹配）
	enc, err := tokenizer.ForModel(tokenizer.Model(model))
	if err == nil {
		tc.enc = enc
		return tc
	}

	// 最后降级到 Cl100kBase
	enc, fallbackErr := tokenizer.Get(tokenizer.Cl100kBase)
	if fallbackErr == nil {
		tc.enc = enc
		logger.Warn(logComponent).Str("model", model).
			Err(err).
			Msg("model not in mapping table and ForModel failed, falling back to cl100k_base encoding")
		return tc
	}

	// 全部失败，enc 为 nil，后续使用 len(text)//3
	logger.Warn(logComponent).Str("model", model).
		Err(fallbackErr).
		Msg("Tiktoken init completely failed, falling back to len(text)//3 counting")
	return tc
}

// Count 计算文本的 Token 数量。
// 优先使用 tiktoken 编码器精确计算，失败时降级为 len(text)//3。
// 返回 (count, error)：计数成功时 error 为 nil，count 为 token 数；
// 编码器不可用（enc 为 nil）时返回 (0, error)，由调用方决定是否降级。
// Python: TiktokenCounter.count(text, model="")
func (tc *TiktokenCounter) Count(text string, model string) (int, error) {
	if tc.enc == nil {
		return 0, fmt.Errorf("tiktoken 编码器未初始化（模型: %s），无法计算 token", tc.model)
	}
	count, err := tc.enc.Count(text)
	if err != nil {
		// 运行时编码失败，返回降级值和 error，调用方可选择使用降级值
		fallback := tc.fallbackCount(text)
		logger.Warn(logComponent).Str("model", tc.model).
			Int("text_len", len(text)).
			Int("fallback_count", fallback).
			Err(err).
			Msg("Tiktoken encoding failed, falling back to len//3 counting")
		return fallback, nil
	}
	return count, nil
}

// CountMessages 计算消息列表的 Token 数量。
// 按 OpenAI 惯例格式化消息：<|start|>{role}\n{content}<|end|>，
// AssistantMessage 额外序列化 ToolCalls 计入 token，末尾 +3 tokens。
// 内部调用 Count，若编码器不可用则返回 (0, error)。
// Python: TiktokenCounter.count_messages(messages, model="")
func (tc *TiktokenCounter) CountMessages(messages []llm_schema.BaseMessage, model string) (int, error) {
	if len(messages) == 0 {
		return 0, nil
	}
	total := 0
	for _, msg := range messages {
		// 格式: <|start|>{role}\n{content}<|end|>
		// 多模态消息：文本块走 tokenizer，图像块按尺寸计费（不把 base64 当文本）
		content, imageTokens := contentForCounting(msg.GetContent())
		piece := fmt.Sprintf("<|start|>%s\n%s<|end|>", msg.GetRole(), content)
		count, err := tc.Count(piece, model)
		if err != nil {
			return 0, fmt.Errorf("计算消息 token 失败: %w", err)
		}
		total += count + imageTokens

		// AssistantMessage 特殊处理：额外计算 ToolCalls token
		if asst, ok := msg.(*llm_schema.AssistantMessage); ok && len(asst.ToolCalls) > 0 {
			toolCallsJSON, err := json.Marshal(asst.ToolCalls)
			if err == nil {
				tcCount, tcErr := tc.Count(string(toolCallsJSON), model)
				if tcErr != nil {
					return 0, fmt.Errorf("计算 ToolCalls token 失败: %w", tcErr)
				}
				total += tcCount
			}
		}
	}
	return total + 3, nil
}

// CountTools 计算工具定义的 Token 数量。
// 按格式 <|start|>functions.{name}:{idx}\n{json}<|end|> 计数，末尾 +3 tokens。
// 内部调用 Count，若编码器不可用则返回 (0, error)。
// Python: TiktokenCounter.count_tools(tools, model="")
func (tc *TiktokenCounter) CountTools(tools []common_schema.ToolInfoInterface, model string) (int, error) {
	if len(tools) == 0 {
		return 0, nil
	}
	total := 0
	for idx, tool := range tools {
		functionObj := map[string]any{
			"name":        tool.GetName(),
			"description": tool.GetDescription(),
			"parameters":  tool.GetParameters(),
		}
		jsonStr, err := json.Marshal(functionObj)
		if err != nil {
			// JSON 序列化失败，使用字段拼接作为降级
			jsonStr = []byte(tool.GetName() + tool.GetDescription())
		}
		// 格式: <|start|>functions.{name}:{idx}\n{json}<|end|>
		piece := fmt.Sprintf("<|start|>functions.%s:%d\n%s<|end|>", tool.GetName(), idx, string(jsonStr))
		count, countErr := tc.Count(piece, model)
		if countErr != nil {
			return 0, fmt.Errorf("计算工具 token 失败: %w", countErr)
		}
		total += count
	}
	return total + 3, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// contentForCounting 把消息内容拆成「用于 tokenizer 的文本」与「图像 token 数」。
// 图像块不进入文本：base64 载荷被当文本计数会让单张图算出几万 token，
// 触发过早压缩（B-05 的根因）。
func contentForCounting(content llm_schema.MessageContent) (string, int) {
	parts := content.Parts()
	if len(parts) == 0 {
		return content.Text(), 0
	}
	texts := make([]string, 0, len(parts))
	imageTokens := 0
	for _, part := range parts {
		switch {
		case IsImagePart(part):
			imageTokens += ImageBlockTokens(part)
		case part.Type == "text" || part.Type == "":
			texts = append(texts, part.Text)
		default:
			// 未知块（音频/视频/自定义）：净化后按文本计，避免原始载荷进入计数
			texts = append(texts, support.SanitizeContentForText(part))
		}
	}
	return strings.Join(texts, "\n"), imageTokens
}

// fallbackCount 降级 token 计算：len(text)//3。
// 只在首次调用时输出警告日志（通过 fallbackWarned 标志控制），
// Python: TiktokenCounter.count() 中的 fallback 逻辑。
func (tc *TiktokenCounter) fallbackCount(text string) int {
	tc.mu.Lock()
	if !tc.fallbackWarned {
		tc.fallbackWarned = true
		logger.Warn(logComponent).Str("model", tc.model).
			Msg("Tiktoken init failed, falling back to len(text)//3 counting")
	}
	tc.mu.Unlock()
	return len(text) / 3
}

// contentToString 将 MessageContent 转换为字符串用于 token 计数。
// 纯文本模式直接返回文本内容，多模态模式提取 Type=="text" 的分片文本拼接，
// 忽略 image_url 等非文本分片（它们在 LLM 端有独立的 token 计算规则）。
// 不使用 MessageContent.String()，因为多模态模式下它会 json.Marshal(parts)，
// 将 image_url 的 JSON 结构也计入 token，与 Python 行为不一致。
func contentToString(content llm_schema.MessageContent) string {
	if content.IsText() {
		return content.Text()
	}
	// 多模态模式：只提取 text 分片
	var sb strings.Builder
	for _, part := range content.Parts() {
		if part.Type == "text" {
			sb.WriteString(part.Text)
		}
	}
	return sb.String()
}
