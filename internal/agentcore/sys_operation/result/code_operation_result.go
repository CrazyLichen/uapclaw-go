package result

// ──────────────────────────── 结构体 ────────────────────────────

// ExecuteCodeData 代码执行结果数据。
// 对齐 Python ExecuteCodeData：code_content, language, exit_code, stdout, stderr。
type ExecuteCodeData struct {
	// CodeContent 执行的代码内容
	CodeContent string `json:"code_content"`
	// Language 编程语言
	Language string `json:"language"`
	// ExitCode 退出码
	ExitCode *int `json:"exit_code"`
	// Stdout 标准输出
	Stdout string `json:"stdout"`
	// Stderr 标准错误
	Stderr string `json:"stderr"`
}

// ExecuteCodeResult 代码执行结果
type ExecuteCodeResult struct {
	BaseResult
	// Data 结果数据
	Data *ExecuteCodeData `json:"data"`
}

// ExecuteCodeChunkData 代码执行流式块数据。
// 对齐 Python ExecuteCodeChunkData：text, type, chunk_index, exit_code, metadata。
type ExecuteCodeChunkData struct {
	// Text 输出块内容
	Text string `json:"text"`
	// Type 输出类型：stdout / stderr
	Type *string `json:"type"`
	// ChunkIndex 块索引
	ChunkIndex int `json:"chunk_index"`
	// ExitCode 退出码
	ExitCode *int `json:"exit_code"`
	// Metadata 附加元数据
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ExecuteCodeStreamResult 代码执行流式结果
type ExecuteCodeStreamResult struct {
	BaseResult
	// Data 流式块数据
	Data *ExecuteCodeChunkData `json:"data"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
