package skilldev

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RunStageAgent 创建阶段 Agent 并执行一次对话，返回模型输出文本。
//
// 参数：
//   - stageName:     阶段标识（决定 Agent 名称与日志）
//   - systemPrompt:  阶段专属 system prompt
//   - tools:         工具白名单（可为空）
//   - maxIterations: ReAct 最大迭代次数
//   - userPrompt:    本阶段任务描述
//
// 阶段处理器统一通过本方法调用模型，避免各自重复装配 Agent。
func (c *SkillDevContext) RunStageAgent(
	ctx context.Context,
	stageName string,
	systemPrompt string,
	tools []string,
	maxIterations int,
	userPrompt string,
) (string, error) {
	agent, err := c.CreateStageAgent(stageName, systemPrompt, tools, maxIterations)
	if err != nil {
		return "", err
	}

	return c.InvokeStageAgent(ctx, stageName, agent, userPrompt)
}

// InvokeStageAgent 复用一个阶段 Agent 执行一次对话（多文件生成等重复调用场景）。
func (c *SkillDevContext) InvokeStageAgent(
	ctx context.Context,
	stageName string,
	agent *agents.ReActAgent,
	userPrompt string,
) (string, error) {
	result, err := agent.Invoke(ctx, map[string]any{"query": userPrompt})
	if err != nil {
		logger.Error(logComponent).
			Str("task_id", c.TaskID).
			Str("stage", stageName).
			Err(err).
			Msg("[SkillDevContext] stage agent invoke failed")
		return "", fmt.Errorf("阶段 %s 模型调用失败: %w", stageName, err)
	}
	return extractStageOutput(result), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractStageOutput 从 Agent 返回结构中取文本输出。
//
// ReActAgent.Invoke 的返回是 map（键随实现而定），这里按 output/response/content
// 优先级取字符串，取不到时退化为整体格式化。
func extractStageOutput(result map[string]any) string {
	found := false
	for _, key := range []string{"output", "response", "content", "result"} {
		v, ok := result[key]
		if !ok {
			continue
		}
		found = true
		if s, ok := v.(string); ok {
			return s
		}
	}
	if found {
		// 命中已知键但非字符串/为空：视为无有效输出，避免把 map 打印成文件内容
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", result))
}

// StripCodeFences 去掉模型输出里包裹的 Markdown 代码围栏。
//
// 模型经常把文件内容包在 ```lang ... ``` 里，写入磁盘前需剥离。
func StripCodeFences(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "```") {
		return text
	}

	lines := strings.Split(trimmed, "\n")
	if len(lines) < 2 {
		return text
	}
	// 去掉首行围栏
	lines = lines[1:]
	// 去掉末尾围栏
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "```" {
			lines = lines[:i]
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
