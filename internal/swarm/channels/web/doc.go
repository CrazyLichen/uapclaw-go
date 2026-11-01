// Package web 提供 Web 前端所需的 HTTP 静态服务与本地文件接口。
//
// 本包对应 Python 的 jiuwenswarm/channels/web/app_web.py：一个面向浏览器的
// 文件服务层，负责静态资源（SPA fallback）与 /file-api/* 本地文件接口。
// Go 侧把静态服务与网关合并为单进程单端口，因此本包只提供路由与处理器，
// 由 gateway 挂载（不再像 Python 那样另起 5173 端口做反代）。
//
// 路径约定：所有 file-api 的路径参数都以「数据根」WorkspaceDir()（默认 ~/.uapclaw）
// 为基准，并限制在四个白名单根内：agent、.agent_teams、agent/.logs、auto-harness。
//
// 文件目录：
//
//	web/
//	├── doc.go          # 包文档
//	├── app_web.go      # file-api 路由装配 + SPA 静态处理器
//	├── file_api.go     # 本地文件接口实现（读/写/列目录/下载/索引重建）
//	├── embed.go        # 前端构建产物嵌入
//	└── frontend/       # React 前端源码与 dist 构建产物
//
// 对应 Python 代码：jiuwenswarm/channels/web/
package web
