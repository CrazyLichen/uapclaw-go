package extensions

import (
	"context"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TriggerMemoryBeforeChat 触发记忆对话前钩子并写回，拼接 memory_block 注入 inputs。
//
// 对齐 Python interface.py:834-845 / 951-963：
//   - 构造 MemoryHookContext → Trigger → FromMap 写回
//   - join 时过滤空块（对齐 Python `join(b for b in mem_ctx.memory_blocks if b)`）
//   - inputs["memory_block"] 无条件写入（对齐 Python 无条件赋值，含空串）
//
// workspaceDir 传入 workspace.AgentRootDir()/home（调用方组装，避免本包依赖 workspace）。
// extra 对齐 Python `extra=request.params`（调用方传 parseRequestParams(request)）。
func TriggerMemoryBeforeChat(ctx context.Context, request *schema.AgentRequest, inputs map[string]any, workspaceDir string, extra map[string]any) {
	extReg, extErr := GetInstanceErr()
	if extErr != nil || extReg == nil {
		return
	}
	memCtx := buildMemoryHookContext(request, workspaceDir, nil, extra)
	data := memCtx.ToMap()
	extReg.Trigger(ctx, AgentServerMemoryBeforeChat, data)
	memCtx.FromMap(data)

	memoryBlock := strings.Join(filterNonEmpty(memCtx.MemoryBlocks), "\n\n")
	inputs["memory_block"] = memoryBlock
}

// TriggerMemoryAfterChat 触发记忆对话后钩子并写回（当前宿主无消费，保持对称闭环）。
//
// 对齐 Python interface.py:866-877 / 1134-1146。
func TriggerMemoryAfterChat(ctx context.Context, request *schema.AgentRequest, assistantMessage string, workspaceDir string, extra map[string]any) {
	extReg, extErr := GetInstanceErr()
	if extErr != nil || extReg == nil {
		return
	}
	afterCtx := buildMemoryHookContext(request, workspaceDir, &assistantMessage, extra)
	data := afterCtx.ToMap()
	extReg.Trigger(ctx, AgentServerMemoryAfterChat, data)
	afterCtx.FromMap(data)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildMemoryHookContext 构造 MemoryHookContext（对齐 Python 构造字段）。
// session_id 回退 "default" 对齐 Python `request.session_id or "default"`
// （nil 或空串均回退，interface.py L836/869/954/1138 共 4 处）。
func buildMemoryHookContext(request *schema.AgentRequest, workspaceDir string, assistantMessage *string, extra map[string]any) *MemoryHookContext {
	channelID := request.ChannelID
	sid := "default"
	if request.SessionID != nil && *request.SessionID != "" {
		sid = *request.SessionID
	}
	return &MemoryHookContext{
		SessionID:        sid,
		RequestID:        request.RequestID,
		ChannelID:        &channelID,
		AgentName:        "main_agent",
		WorkspaceDir:     workspaceDir,
		AssistantMessage: assistantMessage,
		Extra:            extra,
	}
}

// filterNonEmpty 过滤空字符串块（对齐 Python if b 语义：空串为假）
func filterNonEmpty(blocks []string) []string {
	result := make([]string, 0, len(blocks))
	for _, b := range blocks {
		if b != "" {
			result = append(result, b)
		}
	}
	return result
}
