// Package version 提供版本号统一管理。
//
// 所有子命令和构建脚本通过此包获取当前版本号，
// 确保整个项目版本信息单一来源。
//
// 编译时可通过 -ldflags 注入版本信息：
//
//	go build -ldflags "\
//	  -X github.com/uapclaw/uapclaw-go/internal/common/version.Version=0.2.0 \
//	  -X github.com/uapclaw/uapclaw-go/internal/common/version.GitCommit=abc1234 \
//	  -X github.com/uapclaw/uapclaw-go/internal/common/version.BuildDate=2025-07-12T00:00:00Z" \
//	  -o bin/uapclaw ./cmd/uapclaw/
//
// 文件目录：
//
//	version/
//	├── doc.go              # 包文档
//	├── version.go          # 版本号定义、构建元信息、BuildInfo()
//	├── prerelease.go       # 预发布版本识别与比较
//	├── release_info.go     # 发布信息模型（ReleaseInfo/ReleaseAsset）
//	└── version_source.go   # 版本源接口 + GitHub Releases 实现
//
// 对应 Python 代码：
//   - jiuwenswarm/common/version.py（版本号定义）
//   - jiuwenswarm/common/version_source.py（版本源 + 预发布处理）
package version
