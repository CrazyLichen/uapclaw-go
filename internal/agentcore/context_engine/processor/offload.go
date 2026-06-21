package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// offloadMessageHandle 内存卸载消息占位符格式
	offloadMessageHandle = "[[OFFLOAD: handle=%s, type=%s]]"
	// offloadMessageHandleWithPath 文件系统卸载消息占位符格式（含路径）
	offloadMessageHandleWithPath = "[[OFFLOAD: type=%s, path=%s]]"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// OffloadMessages 将消息卸载到文件系统或内存。
//
// 根据选项中的 OffloadType 决定卸载目标：
//   - "in_memory"：卸载到内存（通过 ModelContext.OffloadMessages 存入）
//   - "filesystem"（默认）：卸载到文件系统，失败时 fallback 到内存
//
// 对应 Python: ContextProcessor.offload_messages()
func (p *BaseProcessor) OffloadMessages(ctx context.Context, mc context_engine.ModelContext, role string, content string, messages []llm_schema.BaseMessage, opts ...Option) (llm_schema.BaseMessage, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	po := newProcessorOption(opts...)

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

	if offloadType == "in_memory" {
		return p.offloadMessagesToMemory(mc, role, content, messages, offloadHandle)
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
		return p.offloadMessagesToMemory(mc, role, content, messages, offloadHandle)
	}

	return p.offloadMessagesToFilesystem(role, content, offloadHandle, offloadPath)
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
//
// ⤵️ 5.31 回填：需 ModelContext.OffloadMessages(handle, messages) 方法
// 当前实现预留调用点，待 5.31 ModelContext 补充 OffloadMessages 方法后回填。
func (p *BaseProcessor) offloadMessagesToMemory(mc context_engine.ModelContext, role string, content string, messages []llm_schema.BaseMessage, offloadHandle string) (llm_schema.BaseMessage, error) {
	content = content + fmt.Sprintf(offloadMessageHandle, offloadHandle, "in_memory")

	// ⤵️ 5.31 回填：调用 mc.OffloadMessages(offloadHandle, messages) 存入内存
	// if om, ok := mc.(interface{ OffloadMessages(string, []llm_schema.BaseMessage) }); ok {
	//     om.OffloadMessages(offloadHandle, messages)
	// } else {
	//     return nil, nil
	// }

	return schema.NewOffloadMessage(
		roleTypeFromRole(role),
		content,
		offloadHandle,
		"in_memory",
	), nil
}

// offloadMessagesToFilesystem 将消息卸载到文件系统。
func (p *BaseProcessor) offloadMessagesToFilesystem(role string, content string, offloadHandle string, offloadPath string) (llm_schema.BaseMessage, error) {
	if offloadPath != "" {
		content = content + fmt.Sprintf(offloadMessageHandleWithPath, "filesystem", offloadPath)
	} else {
		content = content + fmt.Sprintf(offloadMessageHandle, offloadHandle, "filesystem")
	}

	return schema.NewOffloadMessage(
		roleTypeFromRole(role),
		content,
		offloadHandle,
		"filesystem",
	), nil
}

// writeOffloadToFile 写入卸载内容到文件系统。
//
// ⤵️ 9.32 回填：优先使用 SysOperation 异步写，移除 os 兜底路径
func (p *BaseProcessor) writeOffloadToFile(sessionID string, offloadHandle string, offloadPath string, messages []llm_schema.BaseMessage, sysOperation any) bool {
	messageData := map[string]any{
		"offload_handle": offloadHandle,
		"messages":       serializeMessages(messages),
	}
	contentJSON, err := json.Marshal(messageData)
	if err != nil {
		return false
	}

	// ⤵️ 9.32 回填：当 sysOperation 不为 nil 时，优先使用 SysOperation 写文件
	_ = sysOperation // 暂时忽略

	// 兜底：使用 os 直接写文件
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
