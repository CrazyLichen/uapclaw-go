package skilldev

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// methodHandler 方法处理函数签名。
type methodHandler func(ctx context.Context, params map[string]any, requestID string, channelID string) ([]map[string]any, error)

// SkillDevService SkillDev 模式的服务入口（无状态）。
//
// 设计要点：
//   - 无状态：不持有 Pipeline 对象，不做 Pipeline 生命周期管理
//   - 每次请求：StateStore 加载状态 → 创建 Pipeline → 执行 → checkpoint → 释放
//   - 路由层（Gateway）保证同一 task_id 的请求路由到同一实例，Service 无需关心
//
// 对外只暴露一个入口：Handle(request) → ([]map[string]any, error)
//
// 对应 Python: jiuwenswarm/server/runtime/skill/skilldev/service.py (SkillDevService)
type SkillDevService struct {
	// deps 外部依赖（懒初始化）
	deps *SkillDevDeps
	// mu 保护 skilldevDeps 的懒初始化
	mu sync.Mutex
	// skilldevDeps 懒初始化的依赖（可为 nil，首次使用时通过 GetSkillDevDeps 初始化）
	skilldevDeps *SkillDevDeps
}

// ──────────────────────────── 全局变量 ────────────────────────────

// methodDispatch method → handler 映射，避免 if/elif 链。
//
// 对齐 Python: _METHOD_DISPATCH
var methodDispatch = map[schema.ReqMethod]methodHandler{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillDevService 创建新的 SkillDevService 实例。
func NewSkillDevService(deps *SkillDevDeps) *SkillDevService {
	svc := &SkillDevService{
		deps: deps,
	}
	// 初始化方法分发表（需引用 svc 的方法）
	methodDispatch = map[schema.ReqMethod]methodHandler{
		schema.ReqMethodSkilldevStart:    svc.handleStart,
		schema.ReqMethodSkilldevRespond:  svc.handleRespond,
		schema.ReqMethodSkilldevStatus:   svc.handleStatus,
		schema.ReqMethodSkilldevDownload: svc.handleDownload,
		schema.ReqMethodSkilldevCancel:   svc.handleCancel,
		schema.ReqMethodSkilldevFileList: svc.handleFileList,
		schema.ReqMethodSkilldevFileRead: svc.handleFileRead,
	}
	return svc
}

// GetSkillDevDeps 懒初始化 SkillDevDeps。
//
// 如果 SkillDevDeps 尚未初始化，使用 deps 字段作为默认值。
func (s *SkillDevService) GetSkillDevDeps() *SkillDevDeps {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.skilldevDeps == nil {
		s.skilldevDeps = s.deps
	}
	return s.skilldevDeps
}

// SetSkillDevDeps 设置 SkillDevDeps（用于测试注入）。
func (s *SkillDevService) SetSkillDevDeps(deps *SkillDevDeps) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skilldevDeps = deps
}

// Handle 统一入口，根据 ReqMethod 分发到具体处理函数。
//
// 对齐 Python: SkillDevService.handle(request) → AsyncIterator[AgentResponseChunk]
// Go 中改为返回 ([]map[string]any, error)（事件列表）。
func (s *SkillDevService) Handle(ctx context.Context, request *schema.AgentRequest) ([]map[string]any, error) {
	handler, ok := methodDispatch[request.ReqMethod]
	if !ok {
		return []map[string]any{
			errorChunk(request.RequestID, request.ChannelID, fmt.Sprintf("未知 method: %s", request.ReqMethod)),
		}, nil
	}

	// 解析 Params（json.RawMessage → map[string]any）
	var params map[string]any
	if len(request.Params) > 0 {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return []map[string]any{
				errorChunk(request.RequestID, request.ChannelID, fmt.Sprintf("解析 params 失败: %s", err)),
			}, nil
		}
	}
	if params == nil {
		params = make(map[string]any)
	}

	results, err := handler(ctx, params, request.RequestID, request.ChannelID)
	if err != nil {
		return []map[string]any{
			errorChunk(request.RequestID, request.ChannelID, err.Error()),
		}, nil
	}
	return results, nil
}

// handleStart 发起新任务。
//
// 对齐 Python: SkillDevService._handle_start()
func (s *SkillDevService) handleStart(ctx context.Context, params map[string]any, requestID string, channelID string) ([]map[string]any, error) {
	deps := s.GetSkillDevDeps()
	taskID := GenerateTaskID()
	state := NewSkillDevState(taskID)
	state.Input = map[string]any{
		"query":          params["query"],
		"tools":          params["tools"],
		"resources":      params["resources"],
		"existing_skill": params["existing_skill"],
	}
	state.Mode = DetermineTaskMode(params)

	pipeline := NewSkillDevPipeline(taskID, state, deps)

	// 推送 started 事件
	var results []map[string]any
	results = append(results, map[string]any{
		"event_type": "skilldev.started",
		"task_id":    taskID,
	})

	// 执行 Pipeline
	events, err := pipeline.Run(ctx)
	if err != nil {
		return append(results, errorChunk(requestID, channelID, err.Error())), nil
	}

	// 转换事件为响应
	for _, evt := range events {
		results = append(results, eventToPayload(evt))
	}

	// 推送 suspended 事件
	results = append(results, map[string]any{
		"event_type":  "skilldev.suspended",
		"task_id":     taskID,
		"stage":       string(state.Stage),
		"is_complete": true,
	})

	return results, nil
}

// handleRespond 统一确认入口。
//
// 前端只管发 {task_id, action, ...}，后端根据当前阶段自动路由。
//
// 对齐 Python: SkillDevService._handle_respond()
func (s *SkillDevService) handleRespond(ctx context.Context, params map[string]any, requestID string, channelID string) ([]map[string]any, error) {
	deps := s.GetSkillDevDeps()
	taskID, _ := params["task_id"].(string)
	if taskID == "" {
		return []map[string]any{errorChunk(requestID, channelID, "缺少 task_id 参数")}, nil
	}

	state, err := deps.StateStore.LoadState(taskID)
	if err != nil {
		return []map[string]any{errorChunk(requestID, channelID, fmt.Sprintf("加载状态失败: %s", err))}, nil
	}
	if state == nil {
		return []map[string]any{errorChunk(requestID, channelID, fmt.Sprintf("任务 %s 不存在", taskID))}, nil
	}

	if _, ok := SuspensionPoints[state.Stage]; !ok {
		return []map[string]any{
			errorChunk(requestID, channelID, fmt.Sprintf("任务 %s 当前阶段 %s 不是挂起点，无法 respond", taskID, state.Stage)),
		}, nil
	}

	pipeline := NewSkillDevPipeline(taskID, state, deps)

	events, err := pipeline.Resume(ctx, params)
	if err != nil {
		return []map[string]any{errorChunk(requestID, channelID, err.Error())}, nil
	}

	var results []map[string]any
	for _, evt := range events {
		results = append(results, eventToPayload(evt))
	}

	// 推送 completed 或 suspended 事件
	isDone := state.Stage == SkillDevStageCompleted
	finalEventType := "skilldev.suspended"
	if isDone {
		finalEventType = "skilldev.completed"
	}
	results = append(results, map[string]any{
		"event_type":  finalEventType,
		"task_id":     taskID,
		"stage":       string(state.Stage),
		"is_complete": true,
	})

	return results, nil
}

// handleStatus 查状态 / 列任务。
//
// 传 task_id → 返回单个任务状态；不传 → 返回任务列表。
//
// 对齐 Python: SkillDevService._handle_status()
func (s *SkillDevService) handleStatus(_ context.Context, params map[string]any, _ string, _ string) ([]map[string]any, error) {
	deps := s.GetSkillDevDeps()
	taskID, _ := params["task_id"].(string)

	if taskID == "" {
		taskIDs, err := deps.StateStore.ListTasks()
		if err != nil {
			return []map[string]any{{
				"ok":        false,
				"error":     fmt.Sprintf("列出任务失败: %s", err),
				"is_complete": true,
			}}, nil
		}
		return []map[string]any{{
			"ok":        true,
			"tasks":     taskIDs,
			"is_complete": true,
		}}, nil
	}

	state, err := deps.StateStore.LoadState(taskID)
	if err != nil {
		return []map[string]any{{
			"ok":        false,
			"error":     fmt.Sprintf("加载状态失败: %s", err),
			"is_complete": true,
		}}, nil
	}

	if state == nil {
		return []map[string]any{{
			"ok":        false,
			"error":     fmt.Sprintf("任务 %s 不存在", taskID),
			"is_complete": true,
		}}, nil
	}

	statusDict := state.ToStatusDict()
	result := map[string]any{"ok": true, "is_complete": true}
	for k, v := range statusDict {
		result[k] = v
	}
	return []map[string]any{result}, nil
}

// handleDownload 下载产物。
//
// 对齐 Python: SkillDevService._handle_download()
func (s *SkillDevService) handleDownload(_ context.Context, params map[string]any, _ string, _ string) ([]map[string]any, error) {
	deps := s.GetSkillDevDeps()
	taskID, _ := params["task_id"].(string)
	if taskID == "" {
		return []map[string]any{{
			"ok":        false,
			"error":     "缺少 task_id 参数",
			"is_complete": true,
		}}, nil
	}

	state, err := deps.StateStore.LoadState(taskID)
	if err != nil {
		return []map[string]any{{
			"ok":        false,
			"error":     fmt.Sprintf("加载状态失败: %s", err),
			"is_complete": true,
		}}, nil
	}
	if state == nil || state.ZipPath == nil {
		return []map[string]any{{
			"ok":        false,
			"error":     fmt.Sprintf("任务 %s 尚未完成打包", taskID),
			"is_complete": true,
		}}, nil
	}

	zipPath := *state.ZipPath
	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		return []map[string]any{{
			"ok":        false,
			"error":     "产物文件不存在",
			"is_complete": true,
		}}, nil
	}

	raw, err := os.ReadFile(zipPath)
	if err != nil {
		return []map[string]any{{
			"ok":        false,
			"error":     fmt.Sprintf("读取产物失败: %s", err),
			"is_complete": true,
		}}, nil
	}
	contentB64 := base64.StdEncoding.EncodeToString(raw)

	return []map[string]any{{
		"ok":             true,
		"filename":       filepath.Base(zipPath),
		"content_base64": contentB64,
		"size_bytes":     state.ZipSize,
		"is_complete":    true,
	}}, nil
}

// handleCancel 取消任务。
//
// 对齐 Python: SkillDevService._handle_cancel()
func (s *SkillDevService) handleCancel(_ context.Context, params map[string]any, _ string, _ string) ([]map[string]any, error) {
	taskID, _ := params["task_id"].(string)
	// 待实现: 实现取消逻辑（中断正在运行的 Pipeline）
	logger.Warn(logComponent).
		Str("task_id", taskID).
		Msg("[SkillDevService] cancel 尚未实现")
	return []map[string]any{{
		"ok":        true,
		"message":   "取消请求已接收（实现待完善）",
		"is_complete": true,
	}}, nil
}

// handleFileList 获取工作区文件树（供产物弹窗浏览）。
//
// 对齐 Python: SkillDevService._handle_file_list()
func (s *SkillDevService) handleFileList(_ context.Context, params map[string]any, _ string, _ string) ([]map[string]any, error) {
	deps := s.GetSkillDevDeps()
	taskID, _ := params["task_id"].(string)
	if taskID == "" {
		return []map[string]any{{
			"ok":        false,
			"error":     "缺少 task_id 参数",
			"is_complete": true,
		}}, nil
	}

	workspace := deps.WorkspaceProvider.GetLocalPath(taskID)
	skillDir := filepath.Join(workspace, "skill")

	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		return []map[string]any{{
			"ok":        true,
			"tree":      []any{},
			"is_complete": true,
		}}, nil
	}

	tree := buildFileTree(skillDir, skillDir)
	return []map[string]any{{
		"ok":        true,
		"tree":      tree,
		"is_complete": true,
	}}, nil
}

// handleFileRead 读取工作区文件内容。
//
// 对齐 Python: SkillDevService._handle_file_read()
func (s *SkillDevService) handleFileRead(_ context.Context, params map[string]any, _ string, _ string) ([]map[string]any, error) {
	deps := s.GetSkillDevDeps()
	taskID, _ := params["task_id"].(string)
	filePath, _ := params["path"].(string)
	if taskID == "" || filePath == "" {
		return []map[string]any{{
			"ok":        false,
			"error":     "缺少 task_id 或 path 参数",
			"is_complete": true,
		}}, nil
	}

	workspace := deps.WorkspaceProvider.GetLocalPath(taskID)
	skillDir := filepath.Join(workspace, "skill")

	// 路径安全校验（防止 .. 越界）
	fullPath := filepath.Join(skillDir, filePath)
	resolvedPath, err := filepath.Abs(fullPath)
	if err != nil {
		return []map[string]any{{
			"ok":        false,
			"error":     "路径解析失败",
			"is_complete": true,
		}}, nil
	}
	resolvedSkillDir, err := filepath.Abs(skillDir)
	if err != nil {
		return []map[string]any{{
			"ok":        false,
			"error":     "工作区路径解析失败",
			"is_complete": true,
		}}, nil
	}
	if !strings.HasPrefix(resolvedPath, resolvedSkillDir) {
		return []map[string]any{{
			"ok":        false,
			"error":     "路径非法：不能访问工作区外的文件",
			"is_complete": true,
		}}, nil
	}

	info, err := os.Stat(resolvedPath)
	if err != nil || info.IsDir() {
		return []map[string]any{{
			"ok":        false,
			"error":     fmt.Sprintf("文件不存在: %s", filePath),
			"is_complete": true,
		}}, nil
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return []map[string]any{{
			"ok":        false,
			"error":     fmt.Sprintf("读取文件失败: %s", err),
			"is_complete": true,
		}}, nil
	}

	// 尝试 UTF-8 解码，失败则返回二进制提示
	content := string(data)
	if !utf8.Valid(data) {
		content = fmt.Sprintf("[二进制文件，大小 %d bytes]", len(data))
	}

	return []map[string]any{{
		"ok":        true,
		"path":      filePath,
		"content":   content,
		"is_complete": true,
	}}, nil
}

// eventToPayload 将 SkillDevEvent 转换为响应负载 map。
//
// 对齐 Python: SkillDevService._event_to_chunk()
func eventToPayload(evt SkillDevEvent) map[string]any {
	result := map[string]any{
		"event_type": string(evt.EventType),
	}
	for k, v := range evt.Payload {
		result[k] = v
	}
	return result
}

// errorChunk 构造错误响应负载。
//
// 对齐 Python: SkillDevService._error_chunk()
func errorChunk(requestID string, channelID string, message string) map[string]any {
	return map[string]any{
		"event_type":  "skilldev.error",
		"error":       message,
		"request_id":  requestID,
		"channel_id":  channelID,
		"is_complete": true,
	}
}

// buildFileTree 递归构建文件树。
//
// 对齐 Python: SkillDevService._build_file_tree()
func buildFileTree(directory string, root string) []map[string]any {
	result := make([]map[string]any, 0)

	entries, err := os.ReadDir(directory)
	if err != nil {
		return result
	}

	// 排序：目录在前，文件在后；同类型按名称排序
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		// 跳过隐藏文件
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		fullPath := filepath.Join(directory, entry.Name())
		rel, err := filepath.Rel(root, fullPath)
		if err != nil {
			continue
		}
		// 统一使用 / 作为路径分隔符
		rel = filepath.ToSlash(rel)

		if entry.IsDir() {
			children := buildFileTree(fullPath, root)
			result = append(result, map[string]any{
				"path":     rel + "/",
				"type":     "dir",
				"children": children,
			})
		} else {
			info, err := entry.Info()
			size := int64(0)
			if err == nil {
				size = info.Size()
			}
			result = append(result, map[string]any{
				"path": rel,
				"type": "file",
				"size": size,
			})
		}
	}

	return result
}
