//go:build integration

// Package security 提供安全模块集成测试。
//
// 覆盖以下模块：
//   - PermissionMerge：MergePermissionAllowRuleIntoPermissions 合并逻辑
//   - Patterns：CommandMatcher/PathMatcher/URLMatcher/MatchWildcard/ContainsPath/YAML 读写
//
// 对齐 Python: tests/unit_tests/harness/security/test_permission_merge_after_auto_confirm.py,
// tests/unit_tests/harness/security/test_patterns.py
//
// 文件目录：
//
//	security/
//	├── doc.go                        # 包文档
//	├── permission_merge_test.go      # 权限合并逻辑测试
//	└── patterns_integration_test.go  # 安全模式匹配器集成测试
package security
