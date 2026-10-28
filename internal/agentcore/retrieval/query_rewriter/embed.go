// 嵌入提示词模板文件。
package query_rewriter

import "embed"

// ──────────────────────────── 全局变量 ────────────────────────────

// promptsFS 编译时嵌入的提示词模板文件系统。
//
// Python: Path(__file__).resolve().parent / "prompts"（源码部署），
// Go 使用 go:embed 确保二进制自带模板（memory/prompts 先例）。
//
//go:embed prompts/*.md
var promptsFS embed.FS
