package rb

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LabelDeterminator 轨迹标签判定器。
// 使用 LLM-as-judge 判断轨迹执行成功或失败。
// 对齐 Python LabelDeterminator (summary/task/reasoning_bank/update.py)。
type LabelDeterminator struct {
	// sc 服务上下文
	sc *cecontext.ServiceContext
	// prompts 提示词配置
	prompts *ReasoningBankSummaryPrompt
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// statusRegex 从 LLM 响应中提取 Status 字段
	statusRegex = regexp.MustCompile(`(?i)Status:\s*(success|failure)`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLabelDeterminator 创建轨迹标签判定器。
func NewLabelDeterminator(sc *cecontext.ServiceContext) *LabelDeterminator {
	return &LabelDeterminator{
		sc:      sc,
		prompts: defaultReasoningBankSummaryPrompt,
	}
}

// DetermineLabel 判断轨迹是否成功。
// 对齐 Python LabelDeterminator.determine_label()。
func (d *LabelDeterminator) DetermineLabel(ctx context.Context, query string, trajectory string) (bool, error) {
	llm := d.sc.LLM()
	if llm == nil {
		return false, fmt.Errorf("LabelDeterminator: LLM 服务未注册")
	}

	// 构建用户提示词
	var userBuf bytes.Buffer
	if err := d.prompts.LLMJudgeUserPrompt.Execute(&userBuf, map[string]string{
		"Query":      query,
		"Trajectory": trajectory,
	}); err != nil {
		return false, fmt.Errorf("LabelDeterminator: 构建用户提示词失败: %w", err)
	}
	userPrompt := userBuf.String()

	// 构建系统提示词
	var sysBuf bytes.Buffer
	if err := d.prompts.LLMJudgeSystemPrompt.Execute(&sysBuf, nil); err != nil {
		return false, fmt.Errorf("LabelDeterminator: 构建系统提示词失败: %w", err)
	}
	systemPrompt := sysBuf.String()

	// 调用 LLM
	resp, err := llm.Generate(ctx, userPrompt, cecontext.WithSystemPrompt(systemPrompt))
	if err != nil {
		logger.Error(logComponent).Str("method", "DetermineLabel").Err(err).Msg("LLM call failed")
		return false, fmt.Errorf("LabelDeterminator: LLM 调用失败: %w", err)
	}

	// 正则匹配 Status 字段
	matches := statusRegex.FindStringSubmatch(resp)
	if len(matches) >= 2 {
		return strings.EqualFold(matches[1], "success"), nil
	}

	// 回退：检查响应中是否包含 "success"（不区分大小写）
	logger.Warn(logComponent).Str("method", "DetermineLabel").Str("response", resp).Msg("Regex did not match Status, falling back to success keyword check")
	return strings.Contains(strings.ToLower(resp), "success"), nil
}
