package fileguard

import "sync"

// ──────────────────────────── 结构体 ────────────────────────────

// FileToolSpec 一个面向文件的工具的参数与动作约定。
//
// Python: @dataclass(frozen=True) FileToolSpec
type FileToolSpec struct {
	// ToolName 工具名
	ToolName string
	// ArgName 承载路径的参数名
	ArgName string
	// Action 该参数涉及的访问动作
	Action FileAction
}

// ──────────────────────────── 枚举 ────────────────────────────

// FileAction 文件访问动作。
//
// Python: FileAction = Literal["read", "write", "exec"]
type FileAction string

const (
	// FileActionRead 读取
	FileActionRead FileAction = "read"
	// FileActionWrite 写入
	FileActionWrite FileAction = "write"
	// FileActionExec 执行
	FileActionExec FileAction = "exec"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// fileToolRegistryMu 注册表读写锁。
	//
	// Python 侧是可变全局（9.90e 的 send_file_path_guard 会注册原生
	// schema），Go 需保护并发。
	fileToolRegistryMu sync.RWMutex
	// fileToolRegistry 工具名 → spec 列表
	fileToolRegistry = map[string][]FileToolSpec{}
	// bootstrapOnce 内置 spec 只注册一次
	bootstrapOnce sync.Once
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterFileTool 注册或扩展一个工具的参数约定。
//
// 同一 arg_name 重复注册时替换，不同 arg_name 追加。
//
// Python: register_file_tool(spec)
func RegisterFileTool(spec FileToolSpec) {
	bootstrapOnce.Do(bootstrapBuiltinSpecs)

	fileToolRegistryMu.Lock()
	defer fileToolRegistryMu.Unlock()

	existing := fileToolRegistry[spec.ToolName]
	filtered := make([]FileToolSpec, 0, len(existing)+1)
	for _, s := range existing {
		if s.ArgName != spec.ArgName {
			filtered = append(filtered, s)
		}
	}
	filtered = append(filtered, spec)
	fileToolRegistry[spec.ToolName] = filtered
}

// LookupFileToolSpecs 查询一个工具的参数约定，未注册返回 nil。
//
// 返回副本，调用方修改不影响注册表。
//
// Python: lookup_file_tool_specs(tool_name)
func LookupFileToolSpecs(toolName string) []FileToolSpec {
	bootstrapOnce.Do(bootstrapBuiltinSpecs)

	fileToolRegistryMu.RLock()
	defer fileToolRegistryMu.RUnlock()

	specs, ok := fileToolRegistry[toolName]
	if !ok || len(specs) == 0 {
		return nil
	}
	out := make([]FileToolSpec, len(specs))
	copy(out, specs)
	return out
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// bootstrapBuiltinSpecs 注册内置的 18 条工具参数约定。
//
// Python: _bootstrap_builtin_specs()
func bootstrapBuiltinSpecs() {
	fileToolRegistryMu.Lock()
	defer fileToolRegistryMu.Unlock()

	if len(fileToolRegistry) > 0 {
		return
	}
	builtins := []FileToolSpec{
		{ToolName: "read_file", ArgName: "file_path", Action: FileActionRead},
		{ToolName: "read_file", ArgName: "path", Action: FileActionRead},
		{ToolName: "write_file", ArgName: "file_path", Action: FileActionWrite},
		{ToolName: "write_file", ArgName: "path", Action: FileActionWrite},
		{ToolName: "edit_file", ArgName: "file_path", Action: FileActionWrite},
		{ToolName: "read_text_file", ArgName: "file_path", Action: FileActionRead},
		{ToolName: "write_text_file", ArgName: "file_path", Action: FileActionWrite},
		{ToolName: "write", ArgName: "file_path", Action: FileActionWrite},
		{ToolName: "read", ArgName: "file_path", Action: FileActionRead},
		{ToolName: "search_replace", ArgName: "file_path", Action: FileActionWrite},
		{ToolName: "search_replace", ArgName: "path", Action: FileActionWrite},
		{ToolName: "search_replace", ArgName: "target_file", Action: FileActionWrite},
		{ToolName: "grep", ArgName: "path", Action: FileActionRead},
		{ToolName: "grep", ArgName: "file_path", Action: FileActionRead},
		{ToolName: "glob_file_search", ArgName: "glob_pattern", Action: FileActionRead},
		{ToolName: "glob", ArgName: "pattern", Action: FileActionRead},
		{ToolName: "list_dir", ArgName: "path", Action: FileActionRead},
		{ToolName: "list_files", ArgName: "path", Action: FileActionRead},
	}
	for _, s := range builtins {
		list := fileToolRegistry[s.ToolName]
		filtered := make([]FileToolSpec, 0, len(list)+1)
		for _, old := range list {
			if old.ArgName != s.ArgName {
				filtered = append(filtered, old)
			}
		}
		fileToolRegistry[s.ToolName] = append(filtered, s)
	}
}
