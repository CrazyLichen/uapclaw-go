// SendFileToolkit 发送文件到用户工具（10.6.24b）。
// 对齐 Python jiuwenswarm/agents/harness/common/tools/send_file_to_user.py（255 行）：
// 每请求绑定路由三元组（request_id/session_id/channel_id）+ 请求 metadata，
// 经 UpdateRuntimeContext 刷新（不重建工具实例）；
// push/history 经包级注入函数（Python 为 lazy import 单例，Go 编译期成环改启动接线）。

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SendFileToolkit 发送文件到用户的工具包。
// 绑定请求级路由三元组（request_id/session_id/channel_id）+ 请求 metadata；
// 每请求经 UpdateRuntimeContext 刷新（不重建工具实例）。
// Python: SendFileToolkit (send_file_to_user.py L26-77)
type SendFileToolkit struct {
	// requestID 请求标识（消息路由）
	requestID string
	// sessionID 会话标识
	sessionID string
	// channelID 渠道标识
	channelID string
	// requestMetadata 请求元数据（E2A channel_context 映射结果，send_push 透传）
	requestMetadata map[string]any
	// mu 保护并发 UpdateRuntimeContext 与 SendFile 读取
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// sendFilePushFunc E2A 推送函数（agent_server 启动注入）。
	// Python 通过函数体 lazy import + AgentWebSocketServer.get_instance() 取单例——
	// Go swarmtools→server 编译期成环，等价机制为启动接线（cronbridge 先例）。
	sendFilePushFunc func(ctx context.Context, msg map[string]any) error

	// sendFileHistoryFunc 历史记录函数（agent_server 启动注入，
	// 签名与 session.AppendHistoryRecord 一致）。
	sendFileHistoryFunc func(sessionID, requestID, channelID, role, content string,
		timestamp float64, eventType string, extra map[string]any,
		channelMetadata map[string]any, mode string)

	// downloadInfoBuilder 下载信息构造（测试注入点，默认 BuildFileDownloadInfo）
	downloadInfoBuilder = defaultDownloadInfoBuilder
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetSendFilePushFunc 注入 E2A 推送函数（agent_server 启动调用）。
func SetSendFilePushFunc(fn func(ctx context.Context, msg map[string]any) error) {
	sendFilePushFunc = fn
}

// SetSendFileHistoryFunc 注入历史记录函数（agent_server 启动调用）。
func SetSendFileHistoryFunc(fn func(sessionID, requestID, channelID, role, content string,
	timestamp float64, eventType string, extra map[string]any,
	channelMetadata map[string]any, mode string)) {
	sendFileHistoryFunc = fn
}

// NewSendFileToolkit 构造工具包。
// Python: SendFileToolkit.__init__ (send_file_to_user.py L29-55)
func NewSendFileToolkit(requestID, sessionID, channelID string, metadata map[string]any) *SendFileToolkit {
	md := copySendFileMetadata(metadata)
	logger.Debug(logComponent).
		Str("request_id", requestID).
		Str("session_id", sessionID).
		Str("channel_id", channelID).
		Bool("has_metadata", md != nil).
		Msg("[SendFileToolkit] init")
	return &SendFileToolkit{
		requestID:       requestID,
		sessionID:       sessionID,
		channelID:       channelID,
		requestMetadata: md,
	}
}

// UpdateRuntimeContext 刷新请求级上下文（不重建工具）。
// Python: update_runtime_context (send_file_to_user.py L57-77)
func (tk *SendFileToolkit) UpdateRuntimeContext(requestID, sessionID, channelID string, metadata map[string]any) {
	tk.mu.Lock()
	defer tk.mu.Unlock()
	tk.requestID = requestID
	tk.sessionID = sessionID
	tk.channelID = channelID
	tk.requestMetadata = copySendFileMetadata(metadata)
	logger.Debug(logComponent).
		Str("request_id", requestID).
		Str("session_id", sessionID).
		Str("channel_id", channelID).
		Bool("has_metadata", tk.requestMetadata != nil).
		Msg("[SendFileToolkit] update_runtime_context")
}

// SendFile 发送文件到用户（核心执行）。
// Python: send_file (send_file_to_user.py L79-206)
// 返回给 LLM 的中文结果串；error 恒为 nil（Python 语义：异常兜底返回描述串）。
func (tk *SendFileToolkit) SendFile(ctx context.Context, absFilePathList string) (string, error) {
	// 1. 参数宽容解析（Python L88-101）
	paths := parseSendFileInput(absFilePathList)

	// 2. 存在性分流（Python L103-113）
	var validFiles, missingFiles []string
	for _, fp := range paths {
		cleaned := strings.TrimSpace(fp)
		if cleaned == "" {
			continue
		}
		if isSendFileRegular(cleaned) {
			validFiles = append(validFiles, cleaned)
		} else {
			missingFiles = append(missingFiles, cleaned)
			logger.Warn(logComponent).Str("file", cleaned).
				Msg("[SendFileToolkit] file not found")
		}
	}

	// 3. 全缺失（Python L115-119）
	if len(validFiles) == 0 {
		msgParts := []string{"发送文件失败：所有文件均不存在"}
		for _, mf := range missingFiles {
			msgParts = append(msgParts, "  - "+mf)
		}
		return strings.Join(msgParts, "\n"), nil
	}

	logger.Info(logComponent).
		Str("session_id", tk.sessionID).
		Int("valid", len(validFiles)).
		Int("missing", len(missingFiles)).
		Msg("[SendFileToolkit] send_file start")

	// 4. 核心执行（Python L128-199）——异常兜底返回中文错误串
	result, err := tk.sendValidFiles(ctx, validFiles, missingFiles)
	if err != nil {
		// Python L200-206: except Exception → logger.exception + 错误串
		logger.Error(logComponent).
			Err(err).
			Str("session_id", tk.sessionID).
			Msg("[SendFileToolkit] send_file failed")
		return fmt.Sprintf("提交文件失败: %s", err.Error()), nil
	}
	return result, nil
}

// GetTools 返回注册用工具列表（单工具 send_file_to_user）。
// Python: get_tools (send_file_to_user.py L208-254)——description 逐字复制。
func (tk *SendFileToolkit) GetTools() []tool.Tool {
	card := tool.NewToolCard(
		"send_file_to_user",
		"【文件发送工具】当需要将生成的文件、导出的数据、创建的文档等发送给用户时使用此工具。"+
			"使用场景包括：用户请求导出/下载文件、任务完成后需要交付文件、生成报告/文档后发送给用户。"+
			"参数格式：接受单个路径字符串或路径数组，路径必须是绝对路径。"+
			"示例：'/tmp/report.pdf' 或 ['/tmp/file1.csv', '/tmp/file2.xlsx']",
		[]*schema.Param{
			{
				Name: "abs_file_path_list",
				Description: "要发送的文件绝对路径。" +
					"可以是单个路径字符串如 '/path/to/file.pdf'，" +
					"或 JSON 数组字符串如 '[\"/path/file1.csv\", \"/path/file2.xlsx\"]'。" +
					"支持任意文件类型（pdf、xlsx、docx、png、zip等）。",
				Type:     schema.ParamTypeString,
				Required: true,
			},
		},
		nil,
	)

	invokeFn := func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
		raw := ""
		if v, ok := inputs["abs_file_path_list"]; ok {
			raw = fmt.Sprint(v)
		}
		result, err := tk.SendFile(ctx, raw)
		if err != nil {
			return nil, err
		}
		return map[string]any{"result": result}, nil
	}

	fn, err := tool.NewMapFunction(card, invokeFn, nil)
	if err != nil {
		// 卡片为字面量常量，校验结构性不可能失败，防御性返回空
		logger.Warn(logComponent).Err(err).
			Msg("[SendFileToolkit] build map function failed")
		return nil
	}
	return []tool.Tool{fn}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sendValidFiles 发送有效文件（history + push + 结果串）。
// Python L128-199 主体（server.send_push 段）。
func (tk *SendFileToolkit) sendValidFiles(ctx context.Context, validFiles, missingFiles []string) (string, error) {
	tk.mu.Lock()
	requestID, sessionID, channelID := tk.requestID, tk.sessionID, tk.channelID
	requestMetadata := copySendFileMetadata(tk.requestMetadata)
	tk.mu.Unlock()

	// 4-5. 下载信息生成（整块失败回退基础模式，Python L133-164）
	filesPayload, downloadErr := buildSendFilePayload(validFiles, sessionID)
	if downloadErr != nil {
		logger.Warn(logComponent).Err(downloadErr).
			Msg("[SendFileToolkit] build download info failed, fallback to basic mode")
		filesPayload = basicSendFilePayload(validFiles)
	}

	// 6. history（Python L166-179；注入函数为 nil 时 Warn 跳过）
	if sendFileHistoryFunc != nil {
		sendFileHistoryFunc(
			sessionID, requestID, channelID,
			"assistant", "", currentSendFileTimestamp(),
			"chat.file", map[string]any{"files": filesPayload},
			nil, "",
		)
	} else {
		logger.Warn(logComponent).
			Msg("[SendFileToolkit] history func not injected, skip")
	}

	// 7. push（Python L181-193；手工构造对齐 Python msg 字面量）
	msg := map[string]any{
		"request_id": requestID,
		"channel_id": channelID,
		"session_id": sessionID,
		"payload": map[string]any{
			"event_type": "chat.file",
			"files":      filesPayload,
		},
		"is_complete": false,
	}
	if requestMetadata != nil {
		msg["metadata"] = copySendFileMetadata(requestMetadata)
	}
	if sendFilePushFunc != nil {
		if err := sendFilePushFunc(ctx, msg); err != nil {
			return "", err
		}
	} else {
		logger.Warn(logComponent).
			Msg("[SendFileToolkit] push func not injected, skip")
	}

	// 8-9. 结果串（Python L194-199）
	resultParts := []string{fmt.Sprintf("成功发送 %d 个文件", len(validFiles))}
	if len(missingFiles) > 0 {
		resultParts = append(resultParts, "以下文件不存在，未发送：")
		for _, mf := range missingFiles {
			resultParts = append(resultParts, "  - "+mf)
		}
	}
	return strings.Join(resultParts, "\n"), nil
}

// parseSendFileInput 宽容解析入参（Python L88-101）。
// JSON 数组→展开；JSON 单字符串→包裹；解析失败/其他类型→包裹原文。
func parseSendFileInput(raw string) []string {
	var parsed any
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
		switch v := parsed.(type) {
		case []any:
			out := make([]string, 0, len(v))
			for _, item := range v {
				out = append(out, fmt.Sprint(item))
			}
			return out
		case string:
			return []string{v}
		default:
			// Python: 非 list 非 str → [原文]
			return []string{raw}
		}
	}
	// 解析失败 → [原文]
	return []string{raw}
}

// buildSendFilePayload 构造下载信息载荷（Python L139-152 循环）。
func buildSendFilePayload(validFiles []string, sessionID string) ([]map[string]any, error) {
	payload := make([]map[string]any, 0, len(validFiles))
	for _, fp := range validFiles {
		baseName := filepath.Base(fp)
		info, err := downloadInfoBuilder(fp, baseName, sessionID)
		if err != nil {
			return nil, err
		}
		payload = append(payload, map[string]any{
			"path":           fp,
			"name":           baseName,
			"size":           info["size"],
			"mime_type":      info["mime_type"],
			"download_url":   info["download_url"],
			"download_token": info["download_token"],
			"expires_at":     info["expires_at"],
		})
	}
	return payload, nil
}

// basicSendFilePayload 回退基础模式载荷（仅 path+name，Python L158-164）。
func basicSendFilePayload(validFiles []string) []map[string]any {
	payload := make([]map[string]any, 0, len(validFiles))
	for _, fp := range validFiles {
		payload = append(payload, map[string]any{
			"path": fp,
			"name": filepath.Base(fp),
		})
	}
	return payload
}

// defaultDownloadInfoBuilder 默认下载信息构造（默认过期 600 秒）。
func defaultDownloadInfoBuilder(filePath, baseName, sessionID string) (map[string]any, error) {
	info := BuildFileDownloadInfo(filePath, baseName, sessionID, webFileDownloadDefaultExpiresSeconds)
	return info, nil
}

// isSendFileRegular 判定常规文件（对齐 os.path.isfile）。
func isSendFileRegular(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// currentSendFileTimestamp 当前秒级时间戳（毫秒精度，对齐 time.time()）。
func currentSendFileTimestamp() float64 {
	return float64(time.Now().UnixMilli()) / 1000.0
}

// copySendFileMetadata 浅拷贝 metadata（nil 保留 nil，对齐 dict(metadata) if metadata else None）。
func copySendFileMetadata(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
