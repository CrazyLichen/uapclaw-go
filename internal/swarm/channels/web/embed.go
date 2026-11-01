package web

import "embed"

// ──────────────────────────── 全局变量 ────────────────────────────

// frontendDist 嵌入前端构建产物目录。
//
// 使用 go:embed 将 frontend/dist 目录下的所有文件嵌入二进制，
// 实现单二进制部署。开发模式下前端走 Vite dev server（5173），
// 生产模式下从嵌入的 FS 读取。
//
//go:embed frontend/dist
var frontendDist embed.FS
