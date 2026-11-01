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
// 前端产物来源（对齐 Python _default_dist_dir，app_web.py L53-66）：
//
//  1. 显式指定：--dist 命令行选项 → 环境变量 UAPCLAW_WEB_DIST（不存在即启动失败，
//     对齐 Python SystemExit）；
//  2. 用户数据根 <WorkspaceDir>/channels/web/frontend/dist（存在即用，装好后可
//     直接替换前端而无需重编译）；
//  3. 编译期内嵌的 frontend/dist（go:embed，Go 独有兜底；未构建时是占位页）。
//
// 文件目录：
//
//	web/
//	├── doc.go          # 包文档
//	├── app_web.go      # file-api 路由装配
//	├── dist.go         # 前端产物来源解析（工作区优先 → 内嵌兜底）+ SPA 处理器
//	├── file_api.go     # 本地文件接口实现（读/写/列目录/下载/索引重建）
//	├── embed.go        # 前端构建产物嵌入
//	└── frontend/       # React 前端源码与 dist 构建产物
//
// 对应 Python 代码：jiuwenswarm/channels/web/
package web
