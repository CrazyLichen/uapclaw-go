//go:build integration

// Package github_test 提供 GitHub 远程技能能力的集成测试。
//
// 被测边界是外部 GitHub API（Trees API + Contents API），属真跨网络边界的集成测试：
// fixture 位于本仓库 tests/fixtures/remote-skills/，通过 GitHubTree.Directory 定位子树，
// tree ref 由 GITHUB_SKILL_TEST_REF 指定（CI 传 github.sha，保证读到本次提交的 fixture）。
//
// 守卫：RUN_GITHUB_TEST=1 才执行，缺省全 Skip；GITHUB_TOKEN 可选（避免匿名 60 次/小时限流）。
//
// 对应 Python 代码：tests/system_tests/agent/skill/test_remote_skill.py
//
// 文件目录：
//
//	github/
//	├── doc.go               # 包文档
//	└── remote_skill_test.go # 远程技能搜索与下载用例
package github_test
