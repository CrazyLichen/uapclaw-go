// Package dotenv 提供轻量级 .env 文件解析与加载能力。
//
// 本包自实现 .env 文件解析，不依赖第三方库（如 godotenv），
// 因为 .env 格式极其简单（KEY=VALUE + 注释 + 引号），
// 无需引入额外依赖。
//
// 支持的 .env 格式特性：
//   - KEY=VALUE 基本赋值
//   - KEY="VALUE" / KEY='VALUE' 带引号的值
//   - # 开头的注释行
//   - 空行跳过
//   - export 前缀忽略（export KEY=VALUE）
//   - 行尾注释（KEY=VALUE # comment，仅无引号时生效）
//
// 本包还提供 ParseEarly 函数，用于在 CLI 子命令执行前
// 预解析 --dotenv/--name 参数，实现多实例隔离。
// 对应 Python: jiuwenswarm/dotenv_early.py
package dotenv
