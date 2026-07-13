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

// SkillDevService SkillDev 模式的服务入口（无状态）。
//
// 设计要点：
//   - 无状态：不持有 Pipeline 对象，不做 Pipeline 生命周期管理
//   - 每次请求：StateStore 加载状态 → 创建 Pipeline → 执行 → checkpoint → 释放
//   - 路由层（Gateway）保证同一 task_id 的请求路由到同一实例，Service 无需关心
//
// 对外只暴露一个入口：Handle(request) → (<-chan *AgentResponseChunk, error)
//
// 对应 Python: jiuwenswarm/server/runtime/skill/skilldev/service.py (SkillDevService)
type SkillDevService struct {
	// deps 外部依赖（懒初始化）
	deps *SkillDevDeps
	// mu 保护 skilldevDeps 的懒初始化
	mu sync.Mutex
	// skilldevDeps 懒初始化的依赖（可为 nil，首次使用时通过 GetSkillDevDeps 初始化）
	skilldevDeps *SkillDevDeps
	// methodDispatch method → handler 映射，避免 if/elif 链。
	//
	// 对齐 Python: _METHOD_DISPATCH
	methodDispatch map[schema.ReqMethod]methodHandler
}

// ──────────────────────────── 枚举 ────────────────────────────

// methodHandler 方法处理函数签名。
//
// 返回 chunk channel，调用方逐个读取，channel 关闭表示结束。
type methodHandler func(ctx context.Context, params map[string]any, requestID string, channelID string) (<-chan *schema.AgentResponseChunk, error)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillDevService 创建新的 SkillDevService 实例。
func NewSkillDevService(deps *SkillDevDeps) *SkillDevService {
	svc := &SkillDevService{
		deps: deps,
	}
	// 初始化方法分发表（需引用 svc 的方法）
	svc.methodDispatch = map[schema.ReqMethod]methodHandler{
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
// 返回 chunk channel，调用方逐个读取，channel 关闭表示结束。
//
// 对齐 Python: SkillDevService.handle(request) → AsyncIterator[AgentResponseChunk]
func (s *SkillDevService) Handle(ctx context.Context, request *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
	handler, ok := s.methodDispatch[request.ReqMethod]
	if !ok {
		// 未找到 handler：返回单 chunk 错误 channel
		ch := make(chan *schema.AgentResponseChunk, 1)
		ch <- errorChunk(request.RequestID, request.ChannelID, fmt.Sprintf("未知 method: %s", request.ReqMethod))
		close(ch)
		return ch, nil
	}

	// 解析 Params（json.RawMessage → map[string]any）
	var params map[string]any
	if len(request.Params) > 0 {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			ch := make(chan *schema.AgentResponseChunk, 1)
			ch <- errorChunk(request.RequestID, request.ChannelID, fmt.Sprintf("解析 params 失败: %s", err))
			close(ch)
			return ch, nil
		}
	}
	if params == nil {
		params = make(map[string]any)
	}

	// 调用 handler，直接透传其返回的 channel
	return handler(ctx, params, request.RequestID, request.ChannelID)
}

// handleStart 发起新任务。
//
// 对齐 Python: SkillDevService._handle_start()
func (s *SkillDevService) handleStart(ctx context.Context, params map[string]any, requestID string, channelID string) (<-chan *schema.AgentResponseChunk, error) {
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

	// 创建输出 chunk channel
	chunkCh := make(chan *schema.AgentResponseChunk, 64)

	// 启动 goroutine：推送 started → 读取 pipeline 事件 → 转为 chunk → 推送 suspended
	go func() {
		defer close(chunkCh)

		// 发送 started chunk
		chunkCh <- schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"event_type": "skilldev.started",
			"task_id":    taskID,
		})

		// 执行 Pipeline，读取事件 channel
		eventCh, err := pipeline.Run(ctx)
		if err != nil {
			chunkCh <- errorChunk(requestID, channelID, err.Error())
			return
		}
		for evt := range eventCh {
			chunkCh <- eventToChunk(evt, requestID, channelID)
		}

		// 发送 suspended chunk
		chunkCh <- schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"event_type": "skilldev.suspended",
			"task_id":    taskID,
			"stage":      string(state.Stage),
		}, schema.WithChunkIsComplete(true))
	}()

	return chunkCh, nil
}

// handleRespond 统一确认入口。
//
// 前端只管发 {task_id, action, ...}，后端根据当前阶段自动路由。
//
// 对齐 Python: SkillDevService._handle_respond()
func (s *SkillDevService) handleRespond(ctx context.Context, params map[string]any, requestID string, channelID string) (<-chan *schema.AgentResponseChunk, error) {
	deps := s.GetSkillDevDeps()
	taskID, _ := params["task_id"].(string)
	if taskID == "" {
		return singleChunkChannel(errorChunk(requestID, channelID, "缺少 task_id 参数")), nil
	}

	state, err := deps.StateStore.LoadState(taskID)
	if err != nil {
		return singleChunkChannel(errorChunk(requestID, channelID, fmt.Sprintf("加载状态失败: %s", err))), nil
	}
	if state == nil {
		return singleChunkChannel(errorChunk(requestID, channelID, fmt.Sprintf("任务 %s 不存在", taskID))), nil
	}

	if _, ok := SuspensionPoints[state.Stage]; !ok {
		return singleChunkChannel(errorChunk(requestID, channelID,
			fmt.Sprintf("任务 %s 当前阶段 %s 不是挂起点，无法 respond", taskID, state.Stage))), nil
	}

	pipeline := NewSkillDevPipeline(taskID, state, deps)

	// 创建输出 chunk channel
	chunkCh := make(chan *schema.AgentResponseChunk, 64)

	// 启动 goroutine：读取 pipeline 事件 → 转为 chunk → 推送终态
	go func() {
		defer close(chunkCh)

		// 执行 Pipeline.Resume，读取事件 channel
		eventCh, resumeErr := pipeline.Resume(ctx, params)
		if resumeErr != nil {
			chunkCh <- errorChunk(requestID, channelID, resumeErr.Error())
			return
		}
		for evt := range eventCh {
			chunkCh <- eventToChunk(evt, requestID, channelID)
		}

		// 推送 completed 或 suspended 事件
		isDone := state.Stage == SkillDevStageCompleted
		finalEventType := "skilldev.suspended"
		if isDone {
			finalEventType = "skilldev.completed"
		}
		chunkCh <- schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"event_type": finalEventType,
			"task_id":    taskID,
			"stage":      string(state.Stage),
		}, schema.WithChunkIsComplete(true))
	}()

	return chunkCh, nil
}

// handleStatus 查状态 / 列任务。
//
// 传 task_id → 返回单个任务状态；不传 → 返回任务列表。
//
// 对齐 Python: SkillDevService._handle_status()
func (s *SkillDevService) handleStatus(_ context.Context, params map[string]any, requestID string, channelID string) (<-chan *schema.AgentResponseChunk, error) {
	deps := s.GetSkillDevDeps()
	taskID, _ := params["task_id"].(string)

	if taskID == "" {
		taskIDs, err := deps.StateStore.ListTasks()
		if err != nil {
			return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
				"ok":    false,
				"error": fmt.Sprintf("列出任务失败: %s", err),
			})), nil
		}
		return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"ok":   true,
			"tasks": taskIDs,
		})), nil
	}

	state, err := deps.StateStore.LoadState(taskID)
	if err != nil {
		return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"ok":    false,
			"error": fmt.Sprintf("加载状态失败: %s", err),
		})), nil
	}

	if state == nil {
		return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"ok":    false,
			"error": fmt.Sprintf("任务 %s 不存在", taskID),
		})), nil
	}

	statusDict := state.ToStatusDict()
	payload := map[string]any{"ok": true}
	for k, v := range statusDict {
		payload[k] = v
	}
	return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, payload)), nil
}

// handleDownload 下载产物。
//
// 对齐 Python: SkillDevService._handle_download()
func (s *SkillDevService) handleDownload(_ context.Context, params map[string]any, requestID string, channelID string) (<-chan *schema.AgentResponseChunk, error) {
	deps := s.GetSkillDevDeps()
	taskID, _ := params["task_id"].(string)
	if taskID == "" {
		return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"ok":    false,
			"error": "缺少 task_id 参数",
		})), nil
	}

	state, err := deps.StateStore.LoadState(taskID)
	if err != nil {
		return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"ok":    false,
			"error": fmt.Sprintf("加载状态失败: %s", err),
		})), nil
	}
	if state == nil || state.ZipPath == nil {
		return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"ok":    false,
			"error": fmt.Sprintf("任务 %s 尚未完成打包", taskID),
		})), nil
	}

	zipPath := *state.ZipPath
	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"ok":    false,
			"error": "产物文件不存在",
		})), nil
	}

	raw, err := os.ReadFile(zipPath)
	if err != nil {
		return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"ok":    false,
			"error": fmt.Sprintf("读取产物失败: %s", err),
		})), nil
	}
	contentB64 := base64.StdEncoding.EncodeToString(raw)

	return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
		"ok":             true,
		"filename":       filepath.Base(zipPath),
		"content_base64": contentB64,
		"size_bytes":     state.ZipSize,
	})), nil
}

// handleCancel 取消任务。
//
// 对齐 Python: SkillDevService._handle_cancel()
func (s *SkillDevService) handleCancel(_ context.Context, params map[string]any, requestID string, channelID string) (<-chan *schema.AgentResponseChunk, error) {
	taskID, _ := params["task_id"].(string)
	// 待实现: 实现取消逻辑（中断正在运行的 Pipeline）
	logger.Warn(logComponent).
		Str("task_id", taskID).
		Msg("[SkillDevService] cancel 尚未实现")
	return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
		"ok":      true,
		"message": "取消请求已接收（实现待完善）",
	})), nil
}

// handleFileList 获取工作区文件树（供产物弹窗浏览）。
//
// 对齐 Python: SkillDevService._handle_file_list()
func (s *SkillDevService) handleFileList(_ context.Context, params map[string]any, requestID string, channelID string) (<-chan *schema.AgentResponseChunk, error) {
	deps := s.GetSkillDevDeps()
	taskID, _ := params["task_id"].(string)
	if taskID == "" {
		return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"ok":    false,
			"error": "缺少 task_id 参数",
		})), nil
	}

	workspace := deps.WorkspaceProvider.GetLocalPath(taskID)
	skillDir := filepath.Join(workspace, "skill")

	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"ok":   true,
			"tree": []any{},
		})), nil
	}

	tree := buildFileTree(skillDir, skillDir)
	return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
		"ok":   true,
		"tree": tree,
	})), nil
}

// handleFileRead 读取工作区文件内容。
//
// 对齐 Python: SkillDevService._handle_file_read()
func (s *SkillDevService) handleFileRead(_ context.Context, params map[string]any, requestID string, channelID string) (<-chan *schema.AgentResponseChunk, error) {
	deps := s.GetSkillDevDeps()
	taskID, _ := params["task_id"].(string)
	filePath, _ := params["path"].(string)
	if taskID == "" || filePath == "" {
		return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"ok":    false,
			"error": "缺少 task_id 或 path 参数",
		})), nil
	}

	workspace := deps.WorkspaceProvider.GetLocalPath(taskID)
	skillDir := filepath.Join(workspace, "skill")

	// 路径安全校验（防止 .. 越界）
	fullPath := filepath.Join(skillDir, filePath)
	resolvedPath, err := filepath.Abs(fullPath)
	if err != nil {
		return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"ok":    false,
			"error": "路径解析失败",
		})), nil
	}
	resolvedSkillDir, err := filepath.Abs(skillDir)
	if err != nil {
		return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"ok":    false,
			"error": "工作区路径解析失败",
		})), nil
	}
	if !strings.HasPrefix(resolvedPath, resolvedSkillDir) {
		return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"ok":    false,
			"error": "路径非法：不能访问工作区外的文件",
		})), nil
	}

	info, err := os.Stat(resolvedPath)
	if err != nil || info.IsDir() {
		return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"ok":    false,
			"error": fmt.Sprintf("文件不存在: %s", filePath),
		})), nil
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
			"ok":    false,
			"error": fmt.Sprintf("读取文件失败: %s", err),
		})), nil
	}

	// 尝试 UTF-8 解码，失败则返回二进制提示
	content := string(data)
	if !utf8.Valid(data) {
		content = fmt.Sprintf("[二进制文件，大小 %d bytes]", len(data))
	}

	return singleChunkChannel(schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
		"ok":      true,
		"path":    filePath,
		"content": content,
	})), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// eventToChunk 将 SkillDevEvent 转换为 AgentResponseChunk。
//
// 对齐 Python: SkillDevService._event_to_chunk()
func eventToChunk(evt SkillDevEvent, requestID, channelID string) *schema.AgentResponseChunk {
	payload := map[string]any{"event_type": string(evt.EventType)}
	for k, v := range evt.Payload {
		payload[k] = v
	}
	return schema.NewAgentResponseChunk(requestID, channelID, payload)
}

// errorChunk 构造错误 AgentResponseChunk。
//
// 对齐 Python: SkillDevService._error_chunk()
func errorChunk(requestID, channelID, message string) *schema.AgentResponseChunk {
	return schema.NewAgentResponseChunk(requestID, channelID, map[string]any{
		"event_type": "skilldev.error",
		"error":      message,
	}, schema.WithChunkIsComplete(true))
}

// singleChunkChannel 创建包含单个 chunk 的 channel 并关闭。
//
// 用于简单同步 handler（handleStatus/handleDownload/handleCancel/handleFileList/handleFileRead）。
func singleChunkChannel(chunk *schema.AgentResponseChunk) <-chan *schema.AgentResponseChunk {
	ch := make(chan *schema.AgentResponseChunk, 1)
	ch <- chunk
	close(ch)
	return ch
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
