package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────
const (
	// offloadMessageHandle 卸载消息占位符格式，统一使用 handle+type 双字段
	// filesystem 的 path 是内部实现细节，不暴露给 LLM，LLM 只需 handle 和 type 来调用 reload 工具
	offloadMessageHandle = "[[OFFLOAD: handle=%s, type=%s]]"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// OffloadMessages 将消息卸载到文件系统或内存。
//
// 根据选项中的 OffloadType 决定卸载目标：
//   - "in_memory"：卸载到内存（通过 ModelContext.OffloadMessages 存入）
//   - "filesystem"（默认）：卸载到文件系统，失败时 fallback 到内存
//
// 选项中可传递 ToolCallID/Name/Metadata，在创建 OffloadMessage 时携带原始消息的关联信息。
//
// 对应 Python: ContextProcessor.offload_messages()
func (p *BaseProcessor) OffloadMessages(ctx context.Context, mc iface.ModelContext, role string, content string, messages []llm_schema.BaseMessage, opts ...iface.Option) (llm_schema.BaseMessage, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	po := iface.NewProcessorOption(opts...)

	offloadHandle := po.OffloadHandle
	if offloadHandle == "" {
		offloadHandle = uuid.New().String()
	}

	offloadType := po.OffloadType
	if offloadType == "" {
		offloadType = "filesystem"
	}

	if mc == nil {
		return nil, nil
	}

	// 构建消息选项：从 ProcessorOption 中提取 Name/Metadata 传递给 OffloadMessage
	var msgOpts []llm_schema.MessageOption
	if po.Name != "" {
		msgOpts = append(msgOpts, llm_schema.WithMessageName(po.Name))
	}
	if po.Metadata != nil {
		msgOpts = append(msgOpts, llm_schema.WithMetadata(po.Metadata))
	}

	if offloadType == "in_memory" {
		return p.offloadMessagesToMemory(mc, role, content, offloadHandle, po.ToolCallID, messages, msgOpts)
	}

	// filesystem 模式
	sessionID := mc.SessionID()
	offloadPath := po.OffloadPath
	if offloadPath == "" {
		offloadPath = p.GenerateOffloadPath("", sessionID, offloadHandle)
	}

	writeSuccess := p.writeOffloadToFile(sessionID, offloadHandle, offloadPath, messages, po.SysOperation)
	if !writeSuccess {
		// fallback 到内存模式
		return p.offloadMessagesToMemory(mc, role, content, offloadHandle, po.ToolCallID, messages, msgOpts)
	}

	return p.offloadMessagesToFilesystem(role, content, offloadHandle, offloadPath, po.ToolCallID, msgOpts)
}

// GenerateOffloadPath 生成 offload 文件路径。
//
// 目录结构: {workspaceDir}/context/{sessionID}_context/offload/{handle}.json
// 若 workspaceDir 为空，使用 memory/offloads/{sessionID}/{handle}.json。
//
// 对应 Python: ContextProcessor._generate_offload_path()
func (p *BaseProcessor) GenerateOffloadPath(workspaceDir, sessionID, offloadHandle string) string {
	fileName := offloadHandle + ".json"
	if workspaceDir != "" {
		return filepath.Join(workspaceDir, "context", sessionID+"_context", "offload", fileName)
	}
	return filepath.Join("memory", "offloads", sessionID, fileName)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// offloadMessagesToMemory 将消息卸载到内存。
func (p *BaseProcessor) offloadMessagesToMemory(mc iface.ModelContext, role string, content string, offloadHandle string, toolCallID string, messages []llm_schema.BaseMessage, msgOpts []llm_schema.MessageOption) (llm_schema.BaseMessage, error) {
	content = content + fmt.Sprintf(offloadMessageHandle, offloadHandle, "in_memory")

	// 调用 mc.OffloadMessages 将消息存入内存缓冲区
	mc.OffloadMessages(offloadHandle, messages)

	return schema.NewOffloadMessage(
		roleTypeFromRole(role),
		content,
		offloadHandle,
		"in_memory",
		toolCallID,
		msgOpts...,
	), nil
}

// offloadMessagesToFilesystem 将消息卸载到文件系统。
func (p *BaseProcessor) offloadMessagesToFilesystem(role string, content string, offloadHandle string, offloadPath string, toolCallID string, msgOpts []llm_schema.MessageOption) (llm_schema.BaseMessage, error) {
	// 统一使用 handle+type 格式，path 是内部实现细节不暴露给 LLM
	content = content + fmt.Sprintf(offloadMessageHandle, offloadHandle, "filesystem")

	return schema.NewOffloadMessage(
		roleTypeFromRole(role),
		content,
		offloadHandle,
		"filesystem",
		toolCallID,
		msgOpts...,
	), nil
}

// writeOffloadToFile 写入卸载内容到文件系统。
//
// 当 sysOperation 不为 nil 且其 Fs() 不为 nil 时，优先使用 SysOperation 的 Fs().WriteFile 写入；
// 失败时 fallback 到 os 直接写文件。
func (p *BaseProcessor) writeOffloadToFile(sessionID string, offloadHandle string, offloadPath string, messages []llm_schema.BaseMessage, sysOperation sysop.SysOperation) bool {
	messageData := map[string]any{
		"offload_handle": offloadHandle,
		"messages":       serializeMessages(messages),
	}
	contentJSON, err := json.Marshal(messageData)
	if err != nil {
		return false
	}

	// 优先使用 SysOperation 写文件
	if sysOperation != nil && sysOperation.Fs() != nil {
		result, err := sysOperation.Fs().WriteFile(context.Background(), offloadPath, string(contentJSON))
		if err == nil && result != nil && result.Code == 0 {
			return true
		}
		// SysOperation 写入失败，fallback 到 os
	}

	return writeOffloadFallback(offloadPath, contentJSON)
}

// writeOffloadFallback 使用 os 直接写文件的兜底方法。
func writeOffloadFallback(offloadPath string, contentJSON []byte) bool {
	if !filepath.IsAbs(offloadPath) {
		return false
	}
	dir := filepath.Dir(offloadPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	if err := os.WriteFile(offloadPath, contentJSON, 0o644); err != nil {
		return false
	}
	return true
}

// serializeMessages 将消息列表序列化为可 JSON 化的切片
func serializeMessages(messages []llm_schema.BaseMessage) []any {
	result := make([]any, 0, len(messages))
	for _, msg := range messages {
		result = append(result, map[string]any{
			"role":    msg.GetRole().String(),
			"content": msg.GetContent(),
		})
	}
	return result
}

// roleTypeFromRole 根据 role 字符串返回 RoleType 枚举值
func roleTypeFromRole(role string) llm_schema.RoleType {
	switch role {
	case "assistant":
		return llm_schema.RoleTypeAssistant
	case "user":
		return llm_schema.RoleTypeUser
	case "system":
		return llm_schema.RoleTypeSystem
	case "tool":
		return llm_schema.RoleTypeTool
	default:
		return llm_schema.RoleTypeUser
	}
}
