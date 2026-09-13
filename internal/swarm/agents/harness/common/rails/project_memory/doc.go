// Package project_memory 提供项目记忆文件发现、加载、缓存和合并功能。
//
// 本包对齐 Python jiuwenswarm/agents/harness/common/rails/project_memory/，
// 在 ProjectMemoryRail 的 before_model_call 钩子中使用。
//
// 功能包括：
//   - 多层级发现：managed → user → project (root → cwd) → local
//   - 固定文件名 + glob 扫描（+ 可选额外目录）
//   - git worktree 处理
//   - symlink 安全去重
//   - @include 展开
//   - frontmatter 剥离 + paths: 作用域限定
//   - 内存缓存 + 文件系统快照失效
//   - 软字符上限截断
//
// 文件目录：
//
//	project_memory/
//	├── doc.go           # 包文档
//	├── files.go         # 文件发现/加载/缓存/合并
//	└── section.go       # PromptSection 工厂
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/project_memory/
package project_memory
