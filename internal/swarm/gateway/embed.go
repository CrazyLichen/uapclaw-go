package gateway

import "embed"

// frontendDist 嵌入前端构建产物目录。
//
// 使用 go:embed 将 channel_manager/web/frontend/dist 目录下的所有文件
// 嵌入到二进制中，实现单二进制部署。
//
// 开发模式下前端走 Vite dev server（5173），
// 生产模式下从嵌入的 FS 中读取。
//
//go:embed channel_manager/web/frontend/dist
// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var frontendDist embed.FS
