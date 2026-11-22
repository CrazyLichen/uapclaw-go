// Package claudecode Claude Code harness provider 的 Go 侧前置半区。
//
// 当前仅承载 15.18 面板配置合入所需的默认常量（偏差 D-18-2：常量半区提前落地）；
// provider 主体（SDK 流/会话 resume/observation 等）⤴️15.7 实施，届时归位并补全
// config/harness/mapping 各文件（对齐 Python harness_providers/claudecode/）。
//
// 对应 Python 代码：openjiuwen/harness_providers/claudecode/
package claudecode

// ──────────────────────────── 常量 ────────────────────────────

// DefaultClaudeMaxBufferSize Claude Code CLI stdio 读缓冲默认上限（32 MiB）。
// Python: harness_providers/claudecode/config.py:22 DEFAULT_CLAUDE_MAX_BUFFER_SIZE；
// 偏差 D-18-2：15.18 mergeHostExternalCliConfig 需要，常量半区提前，config 主体 ⤴️15.7。
const DefaultClaudeMaxBufferSize = 32 * 1024 * 1024
