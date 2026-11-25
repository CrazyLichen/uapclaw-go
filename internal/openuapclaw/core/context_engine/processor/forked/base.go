package forked

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/google/uuid"

	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/processor"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/sys_operation"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// offloadMessageHandle 卸载消息占位符格式（无 path 形态）
	// Python: forked/base.py:26 (_OFFLOAD_MESSAGE_HANDLE)
	offloadMessageHandle = "\n[[OFFLOAD: handle=%s, type=%s]]"
	// offloadMessageHandleWithPath 卸载消息占位符格式（带 path 形态，forked 独有）
	// Python: forked/base.py:27 (_OFFLOAD_MESSAGE_HANDLE_WITH_PATH)
	offloadMessageHandleWithPath = "\n[[OFFLOAD: handle=%s, type=%s, path=%s]]"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContextEvent 处理器对上下文的变更事件。
// Python: openjiuwen/core/context_engine/processor/forked/base.py:29 (ContextEvent)
type ContextEvent struct {
	// EventType 事件类型（= 触发它的处理器 processor_type）
	EventType string `json:"event_type"`
	// MessagesToModify 被修改消息在窗口中的索引列表
	MessagesToModify []int `json:"messages_to_modify"`
	// CompactSummary 压缩摘要内容
	CompactSummary string `json:"compact_summary"`
	// CompressionUsage 本次压缩的模型用量（可 nil）
	CompressionUsage map[string]any `json:"compression_usage,omitempty"`
}

// ContextProcessor forked 处理器家族的抽象基类。
// 配置在构造期传入，可重入并发上下文。
// Python: openjiuwen/core/context_engine/processor/forked/base.py:36
// (ContextProcessor, metaclass=MetaContextProcessor)
//
// 偏差 D-24.2-1：Python MetaContextProcessor 元类把类名注入 __processor_type，
// ProcessorType() 读该属性；Go 无元类，由各实现自行提供 ProcessorType() 方法。
type ContextProcessor struct {
	// config 处理器配置（各实现的 *XxxConfig）
	config any
	// compressionUsage 累计的压缩用量（nil=尚未记录）
	compressionUsage map[string]any
	// model 缓存的模型实例（RebindModelReference 维护）
	model any
}

// NewContextProcessor 创建基类实例。
// Python: ContextProcessor.__init__ (base.py:54)
func NewContextProcessor(config any) *ContextProcessor {
	return &ContextProcessor{
		config: config,
	}
}

// Config 返回构造期传入的配置对象。
// Python: ContextProcessor.config property (base.py:169)
func (p *ContextProcessor) Config() any {
	return p.config
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ResetCompressionUsage 清空累计压缩用量。
// Python: ContextProcessor._reset_compression_usage (base.py:176)
func (p *ContextProcessor) ResetCompressionUsage() {
	p.compressionUsage = nil
}

// RecordCompressionUsage 从模型响应提取用量并累计。
// Python: ContextProcessor._record_compression_usage (base.py:179)
func (p *ContextProcessor) RecordCompressionUsage(response any) {
	usage := extractUsageMetadata(response)
	if usage == nil {
		return
	}
	p.compressionUsage = mergeCompressionUsage(p.compressionUsage, usage)
}

// CurrentCompressionUsage 返回累计用量的拷贝（nil=无）。
// Python: ContextProcessor._current_compression_usage (base.py:185)
func (p *ContextProcessor) CurrentCompressionUsage() map[string]any {
	if p.compressionUsage == nil {
		return nil
	}
	out := make(map[string]any, len(p.compressionUsage))
	for k, v := range p.compressionUsage {
		out[k] = v
	}
	return out
}

// RebindModel 运行时模型切换后刷新模型态（forked 基类默认不处理）。
// Python: ContextProcessor.rebind_model (base.py:189, @staticmethod)
func RebindModel(model any, modelConfig any, modelClientConfig any) bool {
	_, _, _ = model, modelConfig, modelClientConfig
	return false
}

// RebindModelReference 替换缓存的模型引用并同步配置。
// 返回是否发生了实际替换。
// Python: ContextProcessor._rebind_model_reference (base.py:199)
func (p *ContextProcessor) RebindModelReference(
	model any,
	modelConfig any,
	modelClientConfig any,
) bool {
	current := p.model
	if model != nil {
		// Python: model_config = model_config or model.model_config（实例属性直取，
		// Go 侧由调用方保证传入；缺省时回退 config 字段由下方分支处理）
		if modelConfig == nil || modelClientConfig == nil {
			mc, mcc := extractModelConfigs(model)
			if modelConfig == nil {
				modelConfig = mc
			}
			if modelClientConfig == nil {
				modelClientConfig = mcc
			}
		}
	} else {
		if modelConfig == nil {
			modelConfig = configField(p.config, "Model")
		}
		if modelClientConfig == nil {
			modelClientConfig = configField(p.config, "ModelClient")
		}
		if current != nil && sameModelIdentity(current, modelConfig, modelClientConfig) {
			model = current
		} else if modelConfig != nil && modelClientConfig != nil {
			model = llmNewModel(modelClientConfig, modelConfig)
		}
	}

	if model == nil {
		return false
	}

	if configField(p.config, "Model") != nil {
		setConfigField(p.config, "Model", modelConfig)
	}
	if configField(p.config, "ModelClient") != nil {
		setConfigField(p.config, "ModelClient", modelClientConfig)
	}

	if model == current {
		return false
	}
	p.model = model
	return true
}

// OffloadMessages 将消息卸载到内存缓冲区或文件系统，返回占位消息。
// offloadType 为 "in_memory" 走内存缓冲；"filesystem" 写盘失败返回 nil（Python 语义）。
// Python: ContextProcessor.offload_messages (base.py:291)
func (p *ContextProcessor) OffloadMessages(
	ctx context.Context,
	role string,
	content string,
	messages []llm_schema.BaseMessage,
	mc iface.ModelContext,
	offloadHandle string,
	offloadType string,
	offloadPath string,
	sysOperation sysop.SysOperation,
) (llm_schema.BaseMessage, error) {
	if len(messages) == 0 {
		return nil, nil
	}

	if offloadHandle == "" {
		offloadHandle = uuid.New().String()
	}

	if mc != nil {
		if offloadType == "in_memory" {
			return p.offloadMessagesToMemory(role, content, messages, mc, offloadHandle)
		} else if offloadType == "filesystem" {
			sessionID := mc.SessionID()
			workspaceDir := mc.WorkspaceDir()
			// 生成 offload_path
			if offloadPath == "" {
				offloadPath = generateOffloadPath(workspaceDir, sessionID, offloadHandle)
			}
			// 写入原始内容到文件，失败时返回 nil（Python 不做 fallback，与旧链不同）
			writeSuccess := p.writeOffloadToFile(ctx, sessionID, offloadHandle, offloadPath, messages, sysOperation)
			if !writeSuccess {
				return nil, nil
			}
			return p.offloadMessagesToFilesystem(role, content, offloadHandle, offloadPath)
		}
	}
	return nil, nil
}

// GenerateOffloadPath 生成 offload 文件路径。
// 目录结构: {workspace_dir}/context/{session_id}_context/offload/{handle}.json；
// workspace_dir 为空时用相对 memory/offloads/{session_id}/{handle}.json 的绝对路径。
// Python: ContextProcessor._generate_offload_path (base.py:337, @staticmethod)
func GenerateOffloadPath(workspaceDir string, sessionID string, offloadHandle string) string {
	return generateOffloadPath(workspaceDir, sessionID, offloadHandle)
}

// APIRound 判断消息列表是否恰好以一个已完成 API 轮收尾。
// Python: ContextProcessor._api_round (base.py:435, @staticmethod)
func APIRound(messages []llm_schema.BaseMessage) bool {
	return apiRound(messages)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// generateOffloadPath 的实现体（导出包装见 GenerateOffloadPath）。
// Python: ContextProcessor._generate_offload_path (base.py:337)
func generateOffloadPath(workspaceDir string, sessionID string, offloadHandle string) string {
	if workspaceDir != "" {
		return filepath.Join(workspaceDir, "context", sessionID+"_context", "offload", offloadHandle+".json")
	}
	abs, _ := filepath.Abs(filepath.Join("memory", "offloads", sessionID, offloadHandle+".json"))
	return abs
}

// offloadMessagesToMemory 将消息卸载到内存缓冲区。
// Python: ContextProcessor._offload_messages_to_memory (base.py:346, @staticmethod)
func (p *ContextProcessor) offloadMessagesToMemory(
	role string,
	content string,
	messages []llm_schema.BaseMessage,
	mc iface.ModelContext,
	offloadHandle string,
) (llm_schema.BaseMessage, error) {
	content = content + fmt.Sprintf(offloadMessageHandle, offloadHandle, "in_memory")
	mc.OffloadMessages(offloadHandle, messages)
	if offloadHandle == "" {
		offloadHandle = uuid.New().String()
	}
	return createOffloadMessage(role, content, offloadHandle, "in_memory", nil), nil
}

// offloadMessagesToFilesystem 构造文件系统卸载占位消息（有 path 时用 WITH_PATH 格式）。
// Python: ContextProcessor._offload_messages_to_filesystem (base.py:368, @staticmethod)
func (p *ContextProcessor) offloadMessagesToFilesystem(
	role string,
	content string,
	offloadHandle string,
	offloadPath string,
) (llm_schema.BaseMessage, error) {
	if offloadPath != "" {
		content = content + fmt.Sprintf(offloadMessageHandleWithPath, offloadHandle, "filesystem", offloadPath)
	} else {
		content = content + fmt.Sprintf(offloadMessageHandle, offloadHandle, "filesystem")
	}
	return createOffloadMessage(role, content, offloadHandle, "filesystem", nil), nil
}

// writeOffloadToFile 使用 SysOperation（或 os 兜底）写入 offload 内容到文件系统。
// Python: ContextProcessor._write_offload_to_file (base.py:394)
func (p *ContextProcessor) writeOffloadToFile(
	ctx context.Context,
	sessionID string,
	offloadHandle string,
	offloadPath string,
	messages []llm_schema.BaseMessage,
	sysOperation sysop.SysOperation,
) bool {
	filePath := offloadPath
	if filePath == "" {
		filePath = filepath.Join("memory", "offloads", sessionID, offloadHandle+".json")
	}

	messageData := map[string]any{
		"offload_handle": offloadHandle,
		"messages":       forkedSerializeMessages(messages),
	}
	contentJSON, err := json.MarshalIndent(messageData, "", "  ")
	if err != nil {
		return false
	}
	if sysOperation != nil && sysOperation.Fs() != nil {
		result, err := sysOperation.Fs().WriteFile(ctx, filePath, string(contentJSON))
		if err == nil && result != nil && result.Code == 0 {
			return true
		}
		return true
	}
	// Python: sys_operation 为 None 时要求绝对路径 + os 直写
	if !filepath.IsAbs(filePath) {
		return false
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return false
	}
	if err := os.WriteFile(filePath, contentJSON, 0o644); err != nil {
		return false
	}
	return true
}

// apiRound 判断消息列表是否恰好以一个已完成 API 轮收尾。
// Python: ContextProcessor._api_round (base.py:435)
func apiRound(messages []llm_schema.BaseMessage) bool {
	if len(messages) == 0 {
		return false
	}
	// Python: group_completed_api_rounds（forked/base.py 顶部自 session_memory_manager 导入；
	// Go 侧复用 processor 包的同源实现）
	completedRounds := processor.GroupCompletedAPIRounds(messages)
	if len(completedRounds) == 0 {
		return false
	}
	lastEnd := completedRounds[len(completedRounds)-1][1]
	return lastEnd == len(messages)
}

// createOffloadMessage 按 role 构造卸载占位消息。
// Python: schema/messages.py:58 (create_offload_message)——forked/base.py 经
// `from ...schema.messages import create_offload_message` 依赖；
// Go 侧本包落等价转发（schema 包已有构造器）。
func createOffloadMessage(
	role string,
	content string,
	offloadHandle string,
	offloadType string,
	opts []llm_schema.MessageOption,
) llm_schema.BaseMessage {
	switch role {
	case "assistant":
		return schema.NewOffloadAssistantMessage(content, offloadHandle, offloadType)
	case "tool":
		return schema.NewOffloadToolMessage("", content, offloadHandle, offloadType, opts...)
	case "system":
		return schema.NewOffloadSystemMessage(content, offloadHandle, offloadType, opts...)
	default:
		return schema.NewOffloadUserMessage(content, offloadHandle, offloadType, opts...)
	}
}

// forkedSerializeMessages 序列化消息列表（model_dump 等价：JSON-safe map）。
// Python: base.py:413 [msg.model_dump() ...]
func forkedSerializeMessages(messages []llm_schema.BaseMessage) []any {
	out := make([]any, 0, len(messages))
	for _, msg := range messages {
		data, err := json.Marshal(msg)
		if err != nil {
			out = append(out, fmt.Sprintf("%v", msg))
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			out = append(out, fmt.Sprintf("%v", msg))
			continue
		}
		out = append(out, m)
	}
	return out
}

// extractUsageMetadata 从模型响应提取用量元数据（forked 记账形态）。
// Python: ContextProcessor._extract_usage_metadata (base.py:238, @staticmethod)
func extractUsageMetadata(response any) map[string]any {
	msg, ok := response.(*llm_schema.AssistantMessage)
	if !ok || msg == nil || msg.UsageMetadata == nil {
		return nil
	}
	m := msg.UsageMetadata
	return map[string]any{
		"calls":         1,
		"input_tokens":  m.InputTokens,
		"output_tokens": m.OutputTokens,
		"total_tokens":  m.TotalTokens,
		"cache_tokens":  m.CacheTokens,
		"input_cost":    m.InputCost,
		"output_cost":   m.OutputCost,
		"total_cost":    m.TotalCost,
		"model_name":    m.ModelName,
		"details":       []map[string]any{{"input_tokens": m.InputTokens, "output_tokens": m.OutputTokens, "total_tokens": m.TotalTokens, "cache_tokens": m.CacheTokens, "input_cost": m.InputCost, "output_cost": m.OutputCost, "total_cost": m.TotalCost, "model_name": m.ModelName}},
	}
}

// mergeCompressionUsage 合并两份累计用量。
// Python: ContextProcessor._merge_compression_usage (base.py:264, @staticmethod)
func mergeCompressionUsage(left map[string]any, right map[string]any) map[string]any {
	if left == nil {
		if right == nil {
			return nil
		}
		out := make(map[string]any, len(right))
		for k, v := range right {
			out[k] = v
		}
		return out
	}
	if right == nil {
		out := make(map[string]any, len(left))
		for k, v := range left {
			out[k] = v
		}
		return out
	}
	merged := make(map[string]any, len(left))
	for k, v := range left {
		merged[k] = v
	}
	for _, key := range []string{"calls", "input_tokens", "output_tokens", "total_tokens", "cache_tokens"} {
		merged[key] = mapInt(merged, key) + mapInt(right, key)
	}
	for _, key := range []string{"input_cost", "output_cost", "total_cost"} {
		merged[key] = mapFloat(merged, key) + mapFloat(right, key)
	}
	if mapStr(merged, "model_name") == "" {
		merged["model_name"] = mapStr(right, "model_name")
	}
	leftDetails, _ := merged["details"].([]map[string]any)
	rightDetails, _ := right["details"].([]map[string]any)
	merged["details"] = append(append([]map[string]any{}, leftDetails...), rightDetails...)
	return merged
}

// mapInt / mapFloat / mapStr 小工具：map[string]any 的宽容取值
func mapInt(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

func mapFloat(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0
}

func mapStr(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// llmNewModel 按客户端/请求配置创建 Model 实例。
// Python: base.py:230 `Model(model_client_config, model_config)`
func llmNewModel(modelClientConfig any, modelConfig any) any {
	clientCfg, _ := modelClientConfig.(*llm_schema.ModelClientConfig)
	reqCfg, _ := modelConfig.(*llm_schema.ModelRequestConfig)
	if clientCfg == nil || reqCfg == nil {
		return nil
	}
	m, err := llm.NewModel(clientCfg, reqCfg)
	if err != nil {
		return nil
	}
	return m
}

// extractModelConfigs 从缓存的模型实例提取两份配置。
// Python: base.py:209 getattr(model, "model_config") / getattr(model, "model_client_config")
func extractModelConfigs(model any) (any, any) {
	m, ok := model.(*llm.Model)
	if !ok || m == nil {
		return nil, nil
	}
	return m.ModelConfig, m.ClientConfig
}

// sameModelIdentity 判断缓存模型的两份配置与目标配置是否一致。
// Python: base.py:212 getattr(current, "model_config") == model_config and ... （指针等价）
func sameModelIdentity(current any, modelConfig any, modelClientConfig any) bool {
	mc, mcc := extractModelConfigs(current)
	curReq, _ := mc.(*llm_schema.ModelRequestConfig)
	curClient, _ := mcc.(*llm_schema.ModelClientConfig)
	tgtReq, _ := modelConfig.(*llm_schema.ModelRequestConfig)
	tgtClient, _ := modelClientConfig.(*llm_schema.ModelClientConfig)
	return curReq == tgtReq && curClient == tgtClient
}

// configField 反射读取配置结构体的字段（Model / ModelClient）。
// Python: base.py:226 hasattr(self._config, "model") / getattr(self._config, "model", None)
func configField(config any, field string) any {
	v := reflect.ValueOf(config)
	if v.Kind() != reflect.Ptr || v.IsNil() {
		v = reflect.ValueOf(config)
	}
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	f := v.FieldByName(field)
	if !f.IsValid() {
		return nil
	}
	return f.Interface()
}

// setConfigField 反射写配置结构体的字段。
// Python: base.py:226-230 self._config.model = model_config
func setConfigField(config any, field string, value any) {
	v := reflect.ValueOf(config)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}
	f := v.FieldByName(field)
	if !f.IsValid() || !f.CanSet() {
		return
	}
	rv := reflect.ValueOf(value)
	if rv.IsValid() && rv.Type().AssignableTo(f.Type()) {
		f.Set(rv)
	}
}
