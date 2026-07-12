package sys_operation

import (
	"context"
	"encoding/json"
	"fmt"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolAdapterEntry 工具适配条目。
// 对齐 Python SysOperationToolAdapter.extract_tools 返回的 (tool_id, LocalFunction) 元组。
type ToolAdapterEntry struct {
	// ToolID 工具标识（格式：{cardID}.{opType}.{methodName}）
	ToolID string
	// Tool 工具实例
	Tool tool.Tool
}

// SysOperationToolAdapter SysOperation → tool.Tool 适配器。
// 对齐 Python SysOperationToolAdapter：extract_tools。
type SysOperationToolAdapter struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractTools 从 SysOperation 提取所有方法包装为 tool.Tool。
// 对齐 Python SysOperationToolAdapter.extract_tools 逻辑：
//  1. 遍历 OperationRegistry.GetSupportedOperations(card.Mode) 获取 op_type 列表
//  2. 对每个 op_type，获取子操作实例
//  3. 调用 sub_op.ListTools() 获取 ToolCard 列表
//  4. 对每个 ToolCard，构建 fn 闭包 → NewTool → 收集 ToolAdapterEntry
func (SysOperationToolAdapter) ExtractTools(
	card *SysOperationCard,
	instance SysOperation,
	_ string, // language 参数，预留
	_ string, // agentID 参数，预留
) ([]ToolAdapterEntry, error) {
	if card == nil || instance == nil {
		return nil, fmt.Errorf("card 和 instance 不能为 nil")
	}

	var entries []ToolAdapterEntry

	// 遍历注册表中当前模式下所有已注册的操作类型
	for _, opType := range GlobalRegistry.GetSupportedOperations(card.Mode) {
		// 获取子操作实例（通过 SysOperation 接口方法）
		var subOp interface {
			ListTools() []*tool.ToolCard
		}

		switch opType {
		case "fs":
			subOp = instance.Fs()
		case "shell":
			subOp = instance.Shell()
		case "code":
			subOp = instance.Code()
		default:
			continue
		}

		if subOp == nil {
			continue
		}

		// 获取工具卡片列表
		toolCards := subOp.ListTools()
		if len(toolCards) == 0 {
			continue
		}

		for _, tc := range toolCards {
			// 生成唯一工具 ID
			toolID := card.GenerateToolID(opType, tc.Name)

			// 构建闭包绑定到具体子操作方法
			boundOpType := opType
			boundMethodName := tc.Name
			invokeFn := func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
				return dispatchOperationMethod(instance, boundOpType, boundMethodName, ctx, inputs)
			}

			// 创建 ToolCard 副本并设置 ID
			toolCardCopy := *tc
			toolCardCopy.ID = toolID

			// 使用 NewMapFunction 包装为 tool.Tool（弱类型 map 函数工具）
			// 对齐 Python SysOperationToolAdapter 中 LocalFunction(func=None) 的降级场景
			t, err := tool.NewMapFunction(&toolCardCopy, invokeFn, nil)
			if err != nil {
				return nil, fmt.Errorf("创建工具 %s 失败: %w", toolID, err)
			}

			entries = append(entries, ToolAdapterEntry{
				ToolID: toolID,
				Tool:   t,
			})
		}
	}

	return entries, nil
}

// GetToolIDPrefix 获取工具标识前缀。
// 对齐 Python SysOperationToolAdapter.get_tool_id_prefix（Deprecated 但保留）。
// 输入 string → 返回 "{id}."；输入 []string → 返回每项加 "."；其他 → 返回 ""。
func (SysOperationToolAdapter) GetToolIDPrefix(sysOperationID any) any {
	switch v := sysOperationID.(type) {
	case string:
		return v + "."
	case []string:
		result := make([]string, len(v))
		for i, id := range v {
			result[i] = id + "."
		}
		return result
	default:
		return ""
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// dispatchOperationMethod 分发操作方法调用
func dispatchOperationMethod(instance SysOperation, opType string, methodName string, ctx context.Context, params map[string]any) (map[string]any, error) {
	switch opType {
	case "fs":
		fsOp := instance.Fs()
		if fsOp == nil {
			return nil, fmt.Errorf("fs 操作不可用")
		}
		return dispatchFsMethod(fsOp, ctx, methodName, params)
	case "shell":
		shellOp := instance.Shell()
		if shellOp == nil {
			return nil, fmt.Errorf("shell 操作不可用")
		}
		return dispatchShellMethod(shellOp, ctx, methodName, params)
	case "code":
		codeOp := instance.Code()
		if codeOp == nil {
			return nil, fmt.Errorf("code 操作不可用")
		}
		return dispatchCodeMethod(codeOp, ctx, methodName, params)
	default:
		return nil, fmt.Errorf("未知的操作类型: %s", opType)
	}
}

// dispatchShellMethod 分发 Shell 操作方法调用
func dispatchShellMethod(shellOp ShellOperation, ctx context.Context, methodName string, params map[string]any) (map[string]any, error) {
	command, _ := params["command"].(string)
	switch methodName {
	case "execute_cmd":
		var opts []ShellOption
		if cwd, ok := params["cwd"].(string); ok && cwd != "" {
			opts = append(opts, WithShellCwd(cwd))
		}
		if timeout, ok := params["timeout"].(float64); ok {
			opts = append(opts, WithShellTimeout(int(timeout)))
		}
		if shellType, ok := params["shell_type"].(string); ok {
			opts = append(opts, WithShellType(ParseShellType(shellType)))
		}
		r, err := shellOp.ExecuteCmd(ctx, command, opts...)
		if err != nil {
			return nil, err
		}
		return structToMap(r), nil
	case "execute_cmd_stream":
		var opts []ShellOption
		if cwd, ok := params["cwd"].(string); ok && cwd != "" {
			opts = append(opts, WithShellCwd(cwd))
		}
		ch, err := shellOp.ExecuteCmdStream(ctx, command, opts...)
		if err != nil {
			return nil, err
		}
		var results []result.ExecuteCmdStreamResult
		for r := range ch {
			results = append(results, r)
		}
		return map[string]any{"chunks": results}, nil
	case "execute_cmd_background":
		var opts []ShellOption
		if cwd, ok := params["cwd"].(string); ok && cwd != "" {
			opts = append(opts, WithShellCwd(cwd))
		}
		r, err := shellOp.ExecuteCmdBackground(ctx, command, opts...)
		if err != nil {
			return nil, err
		}
		return structToMap(r), nil
	default:
		return nil, fmt.Errorf("未知的 shell 方法: %s", methodName)
	}
}

// dispatchFsMethod 分发 FS 操作方法调用
func dispatchFsMethod(fsOp FsOperation, ctx context.Context, methodName string, params map[string]any) (map[string]any, error) {
	switch methodName {
	case "read_file":
		path, _ := params["path"].(string)
		var opts []FsOption
		if mode, ok := params["mode"].(string); ok {
			opts = append(opts, WithFsMode(mode))
		}
		r, err := fsOp.ReadFile(ctx, path, opts...)
		if err != nil {
			return nil, err
		}
		return structToMap(r), nil
	case "write_file":
		path, _ := params["path"].(string)
		content, _ := params["content"].(string)
		var opts []FsOption
		r, err := fsOp.WriteFile(ctx, path, content, opts...)
		if err != nil {
			return nil, err
		}
		return structToMap(r), nil
	case "list_files":
		path, _ := params["path"].(string)
		r, err := fsOp.ListFiles(ctx, path)
		if err != nil {
			return nil, err
		}
		return structToMap(r), nil
	case "list_directories":
		path, _ := params["path"].(string)
		r, err := fsOp.ListDirectories(ctx, path)
		if err != nil {
			return nil, err
		}
		return structToMap(r), nil
	case "search_files":
		path, _ := params["path"].(string)
		pattern, _ := params["pattern"].(string)
		r, err := fsOp.SearchFiles(ctx, path, pattern)
		if err != nil {
			return nil, err
		}
		return structToMap(r), nil
	case "upload_file":
		localPath, _ := params["local_path"].(string)
		targetPath, _ := params["target_path"].(string)
		r, err := fsOp.UploadFile(ctx, localPath, targetPath)
		if err != nil {
			return nil, err
		}
		return structToMap(r), nil
	case "download_file":
		sourcePath, _ := params["source_path"].(string)
		localPath, _ := params["local_path"].(string)
		r, err := fsOp.DownloadFile(ctx, sourcePath, localPath)
		if err != nil {
			return nil, err
		}
		return structToMap(r), nil
	case "read_file_stream":
		path, _ := params["path"].(string)
		var opts []FsOption
		if mode, ok := params["mode"].(string); ok {
			opts = append(opts, WithFsMode(mode))
		}
		ch, err := fsOp.ReadFileStream(ctx, path, opts...)
		if err != nil {
			return nil, err
		}
		var results []result.ReadFileStreamResult
		for r := range ch {
			results = append(results, r)
		}
		return map[string]any{"chunks": results}, nil
	case "upload_file_stream":
		localPath, _ := params["local_path"].(string)
		targetPath, _ := params["target_path"].(string)
		ch, err := fsOp.UploadFileStream(ctx, localPath, targetPath)
		if err != nil {
			return nil, err
		}
		var results []result.UploadFileStreamResult
		for r := range ch {
			results = append(results, r)
		}
		return map[string]any{"chunks": results}, nil
	case "download_file_stream":
		sourcePath, _ := params["source_path"].(string)
		localPath, _ := params["local_path"].(string)
		ch, err := fsOp.DownloadFileStream(ctx, sourcePath, localPath)
		if err != nil {
			return nil, err
		}
		var results []result.DownloadFileStreamResult
		for r := range ch {
			results = append(results, r)
		}
		return map[string]any{"chunks": results}, nil
	default:
		return nil, fmt.Errorf("未知的 fs 方法: %s", methodName)
	}
}

// dispatchCodeMethod 分发 Code 操作方法调用
func dispatchCodeMethod(codeOp CodeOperation, ctx context.Context, methodName string, params map[string]any) (map[string]any, error) {
	code, _ := params["code"].(string)
	switch methodName {
	case "execute_code":
		var opts []CodeOption
		if lang, ok := params["language"].(string); ok {
			opts = append(opts, WithCodeLanguage(lang))
		}
		r, err := codeOp.ExecuteCode(ctx, code, opts...)
		if err != nil {
			return nil, err
		}
		return structToMap(r), nil
	case "execute_code_stream":
		var opts []CodeOption
		if lang, ok := params["language"].(string); ok {
			opts = append(opts, WithCodeLanguage(lang))
		}
		ch, err := codeOp.ExecuteCodeStream(ctx, code, opts...)
		if err != nil {
			return nil, err
		}
		var results []result.ExecuteCodeStreamResult
		for r := range ch {
			results = append(results, r)
		}
		return map[string]any{"chunks": results}, nil
	default:
		return nil, fmt.Errorf("未知的 code 方法: %s", methodName)
	}
}

// structToMap 将结构体转换为 map[string]any（简单 JSON 序列化中间步骤）
func structToMap(v any) map[string]any {
	// 通过 JSON 序列化/反序列化实现 struct → map
	// 这不是最高效的方式，但保证正确性和一致性
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{"raw": string(b)}
	}
	return m
}
