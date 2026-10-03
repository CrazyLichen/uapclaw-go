//go:build integration

package server_test

// TODO: 补充 SkillManager 集成测试
//   原测试（TestGitMethods/TestSyncMarketplaceRepos/TestHandleSkillsInstall_*）
//   依赖 SkillManager 内部非导出方法（gitClone/gitPull/gitGetCommit/saveState/syncMarketplaceRepos）
//   和非导出字段（mu/state/marketplaceDir），迁移到外部包后无法访问。
//   需要在 SkillManager 包添加 export_test.go 桥接文件，或重构测试使用导出 API。
//
// 运行方式: go test -tags=integration ./tests/integration/swarm/server/...
