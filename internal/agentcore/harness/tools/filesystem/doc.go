// Package filesystem 提供文件系统工具集，包括 GlobTool、ListDirTool 等具体工具，
// 以及文件读取状态注册表、路径解析、XML 去消毒/引号容错、各类辅助函数等基础设施层。
//
// 本包被 ReadFileTool/WriteFileTool/EditFileTool/GrepTool 等工具依赖，
// 为它们提供通用的路径处理、状态管理和文本预处理能力。
//
// 文件目录：
//
//	filesystem/
//	├── doc.go            # 包文档
//	├── read_file.go      # ReadFileTool 读取文件工具（文本/PDF/Notebook/图片）
//	├── write_file.go     # WriteFileTool 写入文件工具（先读后写保护）
//	├── edit_file.go      # EditFileTool 编辑文件工具（替换/创建，含引号容错）
//	├── grep.go           # GrepTool grep 搜索工具（rg/grep/Select-String）
//	├── glob.go           # GlobTool glob 模式搜索工具
//	├── list_dir.go       # ListDirTool 列出目录工具
//	├── read_registry.go  # 文件读取状态注册表（sync.Map 并发安全）
//	├── path_resolve.go   # 路径解析（UNC/绝对/相对路径）
//	├── desanitize.go     # XML 去消毒 + 引号容错
//	└── helpers.go        # 辅助函数（花括号展开、行号格式化、设备检测等）
//
// 对应 Python 代码：openjiuwen/harness/tools/filesystem.py
package filesystem
