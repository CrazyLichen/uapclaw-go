package filesystem

import "sync"

// ──────────────────────────── 结构体 ────────────────────────────

// FileReadState 文件读取状态，用于先读后写/改保护。
// 对齐 Python: _FileReadState (filesystem.py L49-55)
type FileReadState struct {
	// MtimeNS 文件修改时间（纳秒）
	MtimeNS int64
	// SizeBytes 文件大小（字节）
	SizeBytes int64
	// IsPartial 是否仅读取了部分内容（offset > 0 或显式指定 limit）
	IsPartial bool
	// Content 完整读取时缓存的原始内容
	Content string
}

// ──────────────────────────── 全局变量 ────────────────────────────

// fileReadRegistry 全局文件读取状态注册表。
// 对齐 Python: _FILE_READ_REGISTRY (filesystem.py L67)
// ReadFileTool 写入，EditFileTool 读取以执行"必须先读后改"和外部修改检测。
var fileReadRegistry sync.Map

// ──────────────────────────── 导出函数 ────────────────────────────

// GetFileReadState 获取文件读取状态。
// 对齐 Python: _FILE_READ_REGISTRY[path]
func GetFileReadState(path string) (*FileReadState, bool) {
	val, ok := fileReadRegistry.Load(path)
	if !ok {
		return nil, false
	}
	state, ok := val.(*FileReadState)
	return state, ok
}

// SetFileReadState 设置文件读取状态。
// 对齐 Python: _FILE_READ_REGISTRY[path] = state
func SetFileReadState(path string, state *FileReadState) {
	fileReadRegistry.Store(path, state)
}

// DeleteFileReadState 删除文件读取状态。
// 对齐 Python: del _FILE_READ_REGISTRY[path]
func DeleteFileReadState(path string) {
	fileReadRegistry.Delete(path)
}
