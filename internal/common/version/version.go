// Package version 提供版本号统一管理。
//
// 所有子命令和构建脚本通过此包获取当前版本号，
// 确保整个项目版本信息单一来源。
package version

// ──────────────────────────── 常量 ────────────────────────────

const (
	// Version 当前版本号，开发阶段使用 -dev 后缀
	Version = "0.1.0-dev"
	// ProjectName 项目名称
	ProjectName = "uapclaw"
)
