// acp_chat 工具本体（10.6.24d）：把用户消息转发给外部 ACP 兼容子代理。
// 对齐 Python jiuwenswarm/agents/harness/common/tools/acp_chat/tool.py L94-124：
// @tool 装饰的 acp_chat 函数——校验入参、按 profile 取客户端、Chat 聚合文本；
// 异常兜底转 [ERROR] 文本返回（Python 语义：不向调用方抛错）。

package acpchat

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AcpChatTool 外部 ACP 子代理聊天工具。
// cfg 为 config base 映射（Python 经 get_config() 现读；Go 侧构造注入），
// profile 规格位于 cfg["acp_agents"]。
type AcpChatTool struct {
	// cfg 配置基映射
	cfg map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAcpChatTool 构造工具。
func NewAcpChatTool(cfg map[string]any) *AcpChatTool {
	return &AcpChatTool{cfg: cfg}
}

// GetTools 返回注册用工具列表（单工具 acp_chat）。
// 卡名/描述/参数对齐 Python @tool 声明（description 逐字复制）。
func (tk *AcpChatTool) GetTools() []tool.Tool {
	card := tool.NewToolCard(
		"acp_chat",
		"Send a user message to an external ACP-compatible agent subprocess defined in "+
			"config.yaml under acp_agents.<name> (command + args). Use for Codex CLI, Gemini CLI, "+
			"or any other stdio ACP agent. Parameter agent is the profile key, message is the prompt. "+
			"Set new_session=true to restart the subprocess and ACP session for that profile.",
		[]*schema.Param{
			schema.NewStringParam("agent", "The acp_agents profile key identifying the agent.", true),
			schema.NewStringParam("message", "The user message / prompt to send to the agent.", true),
			schema.NewBooleanParam("new_session", "Restart the subprocess and ACP session for this profile.", false),
		},
		nil,
	)

	invokeFn := func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
		out := tk.runChat(ctx, inputs)
		return map[string]any{"result": out}, nil
	}

	fn, err := tool.NewMapFunction(card, invokeFn, nil)
	if err != nil {
		// 卡片为字面量常量，校验结构性不可能失败，防御性返回空
		logger.Warn(logComponent).Err(err).
			Msg("[acp_chat] build map function failed")
		return nil
	}
	return []tool.Tool{fn}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// invoke 执行工具（测试直入口，等价 GetTools 闭包主体）。
func (tk *AcpChatTool) invoke(ctx context.Context, inputs map[string]any) (map[string]any, error) {
	out := tk.runChat(ctx, inputs)
	return map[string]any{"result": out}, nil
}

// runChat 从入参提取三元组并执行 chat。
func (tk *AcpChatTool) runChat(ctx context.Context, inputs map[string]any) string {
	agent, _ := inputs["agent"].(string)
	message, _ := inputs["message"].(string)
	var newSession bool
	if v, ok := inputs["new_session"].(bool); ok {
		newSession = v
	}
	return tk.chat(ctx, agent, message, newSession)
}

// chat 核心执行：校验 → 持锁 → 取客户端 → Chat → 兜底文案。
// 恒返回文本不抛错（对齐 Python：校验失败与异常均回 [ERROR] 文本）。
// Python: acp_chat (tool.py L103-124)
func (tk *AcpChatTool) chat(ctx context.Context, agent, message string, newSession bool) string {
	profile := strings.TrimSpace(agent)
	text := strings.TrimSpace(message)
	if profile == "" {
		return "[ERROR] agent (profile name) is required."
	}
	if text == "" {
		return "[ERROR] message is empty."
	}

	lock := acquireProfileLock(profile)
	lock.mu.Lock()
	defer releaseProfileLock(profile, lock)

	// Python L117-124：try 块 = 取客户端 + Chat；except → 关闭缓存 + [ERROR] 文本
	client, err := getOrCreateClient(ctx, tk.cfg, profile, newSession)
	if err == nil {
		var out string
		out, err = client.Chat(ctx, text)
		if err == nil {
			if out == "" {
				return "(empty response from external ACP agent)"
			}
			return out
		}
	}
	logger.Error(logComponent).Str("profile", profile).Err(err).
		Msg("[acp_chat] profile failed")
	closeProfileUnlocked(profile)
	return fmt.Sprintf("[ERROR] acp_chat failed: %v", err)
}
