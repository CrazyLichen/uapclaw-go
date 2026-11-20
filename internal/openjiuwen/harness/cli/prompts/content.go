package prompts

import (
	"embed"
	"io/fs"
	"strings"
)

// CLI 专属 workspace 内容资源。
//
// Python: harness/cli/prompts/workspace_content/{cn,en}/IDENTITY.md（按
// 相对路径从文件系统读取）；Go 以 //go:embed 打包进二进制，LoadWorkspaceContent
// 按同样的相对路径（如 "IDENTITY.md"）检索。这是对 Python「从包目录
// 读文件」的等价物，避免依赖运行时的源码路径（已知偏差 D-embed）。

//go:embed workspace_content
var workspaceContentFS embed.FS

// workspaceContentBaseDir 嵌入资源的虚拟基目录（对应 Python
// 的 _get_cli_content_base_dir() 路径语义）。
const workspaceContentBaseDir = "workspace_content"

// LoadWorkspaceContent 加载 CLI 专属 workspace 内容。
// 文件不存在时返回空串。
// Python: factory.py:62-79 _load_cli_content
func LoadWorkspaceContent(language, filePath string) string {
	fullPath := workspaceContentBaseDir + "/" + language + "/" + filePath
	content, err := fs.ReadFile(workspaceContentFS, fullPath)
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(string(content), "\r\n", "\n")
}
