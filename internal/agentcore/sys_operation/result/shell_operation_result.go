package result

// ──────────────────────────── 结构体 ────────────────────────────

// ExecuteCmdData 执行命令结果数据。
// 对齐 Python ExecuteCmdData：command, cwd, exit_code, stdout, stderr。
type ExecuteCmdData struct {
	// Command 执行的命令
	Command string `json:"command"`
	// Cwd 工作目录
	Cwd string `json:"cwd"`
	// ExitCode 退出码
	ExitCode *int `json:"exit_code"`
	// Stdout 标准输出
	Stdout string `json:"stdout"`
	// Stderr 标准错误
	Stderr string `json:"stderr"`
}

// ExecuteCmdResult 执行命令结果
type ExecuteCmdResult struct {
	BaseResult
	// Data 结果数据
	Data *ExecuteCmdData `json:"data"`
}

// ExecuteCmdChunkData 执行命令流式块数据。
// 对齐 Python ExecuteCmdChunkData：text, type, chunk_index, exit_code, metadata。
type ExecuteCmdChunkData struct {
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

// ExecuteCmdStreamResult 执行命令流式结果
type ExecuteCmdStreamResult struct {
	BaseResult
	// Data 流式块数据
	Data *ExecuteCmdChunkData `json:"data"`
}

// ExecuteCmdBackgroundData 后台执行命令结果数据。
// 对齐 Python ExecuteCmdBackgroundData：command, cwd, pid。
type ExecuteCmdBackgroundData struct {
	// Command 执行的命令
	Command string `json:"command"`
	// Cwd 工作目录
	Cwd string `json:"cwd"`
	// Pid 进程 ID
	Pid *int `json:"pid"`
}

// ExecuteCmdBackgroundResult 后台执行命令结果
type ExecuteCmdBackgroundResult struct {
	BaseResult
	// Data 后台执行数据
	Data *ExecuteCmdBackgroundData `json:"data"`
}
