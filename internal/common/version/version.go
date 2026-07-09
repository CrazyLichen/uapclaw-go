// Package version 提供版本号统一管理。
//
// 所有子命令和构建脚本通过此包获取当前版本号，
// 确保整个项目版本信息单一来源。
//
// 编译时可通过 -ldflags 注入版本信息（构建命令示例）：
//
//	go build -ldflags "\
//	  -X github.com/uapclaw/uapclaw-go/internal/common/version.Version=0.2.0 \
//	  -X github.com/uapclaw/uapclaw-go/internal/common/version.GitCommit=abc1234 \
//	  -X github.com/uapclaw/uapclaw-go/internal/common/version.BuildDate=2025-07-12T00:00:00Z" \
//	  -o bin/uapclaw ./cmd/uapclaw/
package version

import (
	"fmt"
	"runtime"
	"strings"
)

// ──────────────────────────── 全局变量 ────────────────────────────
var (
	// Version 当前版本号，开发阶段使用 -dev 后缀
	// 正式构建时通过 -ldflags "-X ...Version=x.y.z" 注入
	Version = "0.1.0-dev"
	// GitCommit git commit hash，通过 -ldflags 注入
	GitCommit = ""
	// BuildDate 构建时间（UTC），通过 -ldflags 注入
	BuildDate = ""
	// ProjectName 项目名称
	ProjectName = "uapclaw"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildInfo 返回完整构建信息字符串。
//
// 格式示例：
//
//	uapclaw 0.1.0-dev (commit: abc1234, built: 2025-07-12, go1.23, linux/amd64)
//
// 如果 GitCommit 或 BuildDate 未通过 ldflags 注入，则显示 "unknown"。
func BuildInfo() string {
	commit := GitCommit
	if commit == "" {
		commit = "unknown"
	}

	built := BuildDate
	if built == "" {
		built = "unknown"
	} else if len(built) > 10 {
		// 仅保留日期部分，去掉时间（如 2025-07-12T00:00:00Z → 2025-07-12）
		built = built[:10]
	}

	goVer := strings.TrimPrefix(runtime.Version(), "go")
	platform := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)

	return fmt.Sprintf("%s %s (commit: %s, built: %s, go%s, %s)",
		ProjectName, Version, commit, built, goVer, platform)
}
