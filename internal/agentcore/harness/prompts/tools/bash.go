package tools

// ──────────────────────────── 结构体 ────────────────────────────

// BashMetadataProvider bash 工具元数据提供者
type BashMetadataProvider struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

// bashDescription bash 工具双语描述
var bashDescription = map[string]string{
	"cn": `执行 Shell 命令并返回输出。

工作目录在命令之间保持不变，但 Shell 状态（变量、函数、alias）不保留。Shell 环境从用户的 profile（bash 或 zsh）初始化。

Windows 注意：` + "`cmd`" + ` 自带 ` + "`mkdir`" + ` **不支持 ` + "`-p`" + `**，不要在 cmd/PowerShell 中使用 ` + "`mkdir -p`" + `。本工具按 Bash 语义执行时（默认/` + "`shell_type=bash`" + `，Windows 上常为 Git Bash），POSIX ` + "`mkdir`" + ` **支持 ` + "`-p`" + `**。只有 ` + "`shell_type=cmd`" + ` 或 ` + "`shell_type=powershell`" + ` 时，才改用 ` + "`New-Item ... -Force`" + ` 或逐级 ` + "`mkdir`" + `。

重要：避免使用本工具执行 ` + "`find`" + `、` + "`grep`" + `、` + "`cat`" + `、` + "`head`" + `、` + "`tail`" + `、` + "`sed`" + `、` + "`awk`" + ` 或 ` + "`echo`" + ` 命令，除非明确指示或确认专用工具无法完成任务。请使用对应的专用工具，以获得更好的体验：

 - 文件搜索：使用 glob 工具（不要用 find 或 ls）
 - 内容搜索：使用 grep 工具（不要用 grep 或 rg 命令）
 - 读取文件：使用 read_file 工具（不要用 cat/head/tail）
 - 编辑文件：使用 edit_file 工具（不要用 sed/awk）
 - 写入文件：使用 write_file 工具（不要用 echo > 或 cat <<EOF）
 - 输出文本：直接输出（不要用 echo/printf）
虽然 bash 工具也能做到，但专用工具提供更好的用户体验，并且更方便审查和授权。

# 使用说明
 - 创建文件或目录前，先用本工具执行 ` + "`ls`" + ` 确认父目录存在且位置正确
 - 路径包含空格时必须用双引号括起（例如 cd "path with spaces/file.txt"）
 - 尽量使用绝对路径维持当前工作目录，避免使用 ` + "`cd`" + `；除非用户明确要求
 - 可通过 timeout 参数指定超时（秒），默认 300 秒，上限 3600 秒
 - 可将 run_in_background 设为 true 来后台运行命令。仅在不需要立即获取结果时使用，命令完成后会收到通知。使用该参数时无需在命令末尾加 ` + "`&`" + `
 - 发出多条命令时：
   - 独立命令：在同一消息中多次并行调用本工具。例如需要同时运行 "git status" 和 "git diff"，发送一条消息包含两次并行调用
   - 依赖命令：在单次调用中用 ` + "`&&`" + ` 串联
   - 仅在不关心前序命令是否失败时使用 ` + "`;`" + `
   - 不要用换行分隔命令（引号字符串内换行可以）
 - Git 命令规范：
   - 优先创建新提交而非修改已有提交
   - 执行破坏性操作（如 git reset --hard、git push --force、git checkout --）前，先考虑是否有更安全的替代方案，只在确实必要时使用
   - 除非用户明确要求，不要跳过 hooks（--no-verify）或绕过签名（--no-gpg-sign）。hook 失败时应排查并修复根本原因
   - 用户分享代码仓 URL 让你「看看这个仓库」或「分析一下」时，首选动作是 ` + "`git clone <url> <本地路径>`" + `；克隆完再用 read_file/grep/glob 等专用工具读源码，比抓取仓库主页能拿到的信息完整得多
 - 避免不必要的 ` + "`sleep`" + ` 命令：
   - 能立即执行的命令之间不要 sleep
   - 长时间运行的命令使用 ` + "`run_in_background: true`" + `，无需 sleep 等待
   - 不要在 sleep 循环中重试失败命令——排查根本原因
   - 等待后台任务完成时会自动通知——不要轮询
   - 如必须轮询外部进程，使用检查命令（如 ` + "`gh run view`" + `）而非先 sleep
   - 如必须 sleep，保持短时间（1-5 秒）`,
	"en": `Executes a given bash command and returns its output.

The working directory persists between commands, but shell state (variables, functions, aliases) does not. The shell environment is initialized from the user's profile (bash or zsh).

Windows note: ` + "`cmd`" + `'s ` + "`mkdir`" + ` **does not support ` + "`-p`" + `**; do not use ` + "`mkdir -p`" + ` in cmd/PowerShell. When this tool runs with Bash semantics (default/` + "`shell_type=bash`" + `, often Git Bash on Windows), POSIX ` + "`mkdir`" + ` **supports ` + "`-p`" + `**. Only with ` + "`shell_type=cmd`" + ` or ` + "`shell_type=powershell`" + `, use ` + "`New-Item ... -Force`" + ` or create each level with ` + "`mkdir`" + `.

IMPORTANT: Avoid using this tool to run ` + "`find`" + `, ` + "`grep`" + `, ` + "`cat`" + `, ` + "`head`" + `, ` + "`tail`" + `, ` + "`sed`" + `, ` + "`awk`" + `, or ` + "`echo`" + ` commands, unless explicitly instructed or after you have verified that a dedicated tool cannot accomplish your task. Instead, use the appropriate dedicated tool as this will provide a much better experience for the user:

 - File search: Use glob tool (NOT find or ls)
 - Content search: Use grep tool (NOT grep or rg)
 - Read files: Use read_file tool (NOT cat/head/tail)
 - Edit files: Use edit_file tool (NOT sed/awk)
 - Write files: Use write_file tool (NOT echo >/cat <<EOF)
 - Communication: Output text directly (NOT echo/printf)
While the bash tool can do similar things, it is better to use the built-in tools as they provide a better user experience and make it easier to review tool calls and give permission.

# Instructions
 - If your command will create new directories or files, first use this tool to run ` + "`ls`" + ` to verify the parent directory exists and is the correct location.
 - Always quote file paths that contain spaces with double quotes in your command (e.g., cd "path with spaces/file.txt").
 - Try to maintain your current working directory throughout the session by using absolute paths and avoiding usage of ` + "`cd`" + `. You may use ` + "`cd`" + ` if the user explicitly requests it.
 - You may specify an optional timeout in seconds (up to 3600s / 60 minutes). By default, your command will timeout after 300s.
 - You can use the ` + "`run_in_background`" + ` parameter to run the command in the background. Only use this if you don't need the result immediately and are OK being notified when the command completes later. You do not need to use '&' at the end of the command when using this parameter.
 - When issuing multiple commands:
   - If the commands are independent and can run in parallel, make multiple bash tool calls in a single message. Example: if you need to run "git status" and "git diff", send a single message with two bash tool calls in parallel.
   - If the commands depend on each other and must run sequentially, use a single bash call with '&&' to chain them together.
   - Use ';' only when you need to run commands sequentially but don't care if earlier commands fail.
   - DO NOT use newlines to separate commands (newlines are ok in quoted strings).
 - For git commands:
   - Prefer to create a new commit rather than amending an existing commit.
   - Before running destructive operations (e.g., git reset --hard, git push --force, git checkout --), consider whether there is a safer alternative that achieves the same goal. Only use destructive operations when they are truly the best approach.
   - Never skip hooks (--no-verify) or bypass signing (--no-gpg-sign) unless the user has explicitly asked for it. If a hook fails, investigate and fix the underlying issue.
   - When a user shares a repo URL and asks you to 'look at' or 'analyze' it, the natural first step is ` + "`git clone <url> <local_path>`" + `; after cloning, use read_file/grep/glob on the working tree - it gives you far more than the rendered repository page would.
 - Avoid unnecessary ` + "`sleep`" + ` commands:
   - Do not sleep between commands that can run immediately -- just run them.
   - If your command is long running and you would like to be notified when it finishes -- use ` + "`run_in_background: true`" + `. No sleep needed.
   - Do not retry failing commands in a sleep loop -- diagnose the root cause.
   - If waiting for a background task you started with ` + "`run_in_background: true`" + `, you will be notified when it completes -- do not poll.
   - If you must poll an external process, use a check command (e.g. ` + "`gh run view`" + `) rather than sleeping first.
   - If you must sleep, keep the duration short (1-5 seconds) to avoid blocking the user.`,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetBashInputParams 构建 bash 工具的参数 Schema
func GetBashInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"command":           {"cn": "要执行的命令", "en": "The command to execute"},
		"timeout":           {"cn": "可选超时时间（秒），默认 300，上限 3600。对于长时间运行的任务，建议适当增大该值以避免任务被提前中断", "en": "Optional timeout in seconds, default 300, max 3600. For long-running tasks, it is recommended to increase this value to avoid premature termination"},
		"description":       {"cn": "用简洁的主动语态描述该命令的作用。不要在描述中使用 \"复杂\" 或 \"风险\" 等词——直接描述它做什么。\n\n对于简单命令（git、npm、常用 CLI 工具），保持简短（5-10 个字）：\n- ls → \"列出当前目录文件\"\n- git status → \"显示工作区状态\"\n- npm install → \"安装项目依赖\"\n\n对于不易一眼看懂的命令（管道命令、冷门参数等），补充足够上下文说明其用途：\n- find . -name \"*.tmp\" -exec rm {} \\; → \"递归查找并删除所有 .tmp 文件\"\n- git reset --hard origin/main → \"丢弃所有本地更改，与远程 main 对齐\"\n- curl -s url | jq '.data[]' → \"从 URL 获取 JSON 并提取 data 数组元素\"", "en": "Clear, concise description of what this command does in active voice. Never use words like \"complex\" or \"risk\" in the description - just describe what it does.\n\nFor simple commands (git, npm, standard CLI tools), keep it brief (5-10 words):\n- ls -> \"List files in current directory\"\n- git status -> \"Show working tree status\"\n- npm install -> \"Install package dependencies\"\n\nFor commands that are harder to parse at a glance (piped commands, obscure flags, etc.), add enough context to clarify what it does:\n- find . -name \"*.tmp\" -exec rm {} \\; -> \"Find and delete all .tmp files recursively\"\n- git reset --hard origin/main -> \"Discard all local changes and match remote main\"\n- curl -s url | jq '.data[]' -> \"Fetch JSON from URL and extract data array elements\""},
		"run_in_background": {"cn": "设为 true 以后台运行命令。仅在不需要立即获取结果时使用，命令完成后会收到通知", "en": "Set to true to run this command in the background. Only use this if you don't need the result immediately and are OK being notified when the command completes later"},
		"workdir":           {"cn": "执行目录（相对或绝对路径），默认为工作区根目录；不能越出工作区沙箱", "en": "Working directory (relative or absolute path), defaults to workspace root; cannot escape workspace sandbox"},
		"max_output_chars":  {"cn": "最大输出字符数，0 表示不限制（默认）；非零时上限 20000，防止超大输出撑爆上下文", "en": "Max output characters; 0 (default) means no limit; non-zero values are capped at 20000 to prevent oversized output from flooding context"},
		"shell_type":        {"cn": "指定 Shell 类型，可选值：auto/cmd/powershell/bash/sh，默认 auto。cmd/PowerShell 不支持 `mkdir -p`；需要该语法时保持 auto/bash/sh。", "en": "Shell to use: auto/cmd/powershell/bash/sh, default auto. cmd/PowerShell do not support `mkdir -p`; keep auto/bash/sh when you need that syntax."},
	}

	desc := func(key string) string {
		if d, ok := p[key][lang]; ok {
			return d
		}
		return p[key]["cn"]
	}

	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command":           map[string]any{"type": "string", "description": desc("command")},
			"timeout":           map[string]any{"type": "integer", "description": desc("timeout")},
			"description":       map[string]any{"type": "string", "description": desc("description")},
			"run_in_background": map[string]any{"type": "boolean", "description": desc("run_in_background")},
			"workdir":           map[string]any{"type": "string", "description": desc("workdir")},
			"max_output_chars":  map[string]any{"type": "integer", "description": desc("max_output_chars")},
			"shell_type":        map[string]any{"type": "string", "enum": []any{"auto", "cmd", "powershell", "bash", "sh"}, "description": desc("shell_type")},
		},
		"required": []any{"command"},
	}
}

func (p *BashMetadataProvider) GetName() string { return "bash" }
func (p *BashMetadataProvider) GetDescription(language string) string {
	if desc, ok := bashDescription[language]; ok {
		return desc
	}
	return bashDescription["cn"]
}
func (p *BashMetadataProvider) GetInputParams(language string) map[string]any {
	return GetBashInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() { RegisterToolProvider(&BashMetadataProvider{}) }
