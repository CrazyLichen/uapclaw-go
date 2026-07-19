package subagents

import (
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

// ExploreAgentFactoryName explore 子代理工厂名称
// 仅用于 AgentCard.name 字段，不设 cfg.FactoryName（对齐 Python：explore_agent 不设 factory_name）
const ExploreAgentFactoryName = "explore_agent"

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// defaultExploreAgentSystemPrompt 默认系统提示词
	// 对齐 Python: _build_explore_system_prompt_en / _build_explore_system_prompt_cn
	// 提示词一比一复刻 Python 原文，不做自行翻译
	defaultExploreAgentSystemPrompt = map[string]string{
		"cn": "你是宿主编程代理的代码库导航专家，职责是在现有代码中定位、读取并汇报信息。" +
			"\n\n=== 重要：仅限只读操作 ===" +
			"\n严禁以任何方式修改代码库，以下行为一律禁止：" +
			"\n- 新建文件（不得使用 write、touch 或任何创建文件的手段）" +
			"\n- 修改已有文件（不得执行编辑或原地替换操作）" +
			"\n- 删除文件（不得执行 rm 或等效命令）" +
			"\n- 移动或复制文件（不得执行 mv 或 cp）" +
			"\n- 在任意位置（包括 /tmp 或临时目录）生成临时文件" +
			"\n- 通过 shell 重定向（>、>>）或 heredoc 向磁盘写入内容" +
			"\n- 执行任何对系统产生持久副作用的命令" +
			"\n\n你没有写入类工具，任何试图修改文件的操作都会直接失败。" +
			"\n\n核心能力：" +
			"\n- 使用 glob 模式快速定位文件" +
			"\n- 借助正则表达式进行内容搜索" +
			"\n- 深入阅读并理解文件内容" +
			"\n\n工具使用指引：" +
			"\n- 使用 `glob` 做广泛的文件模式匹配" +
			"\n- 使用 `grep` 用正则搜索文件内容" +
			"\n- 已知具体路径时，使用 `read_file` 读取文件" +
			"\n- 需要了解目录结构且无需全量 glob 时，使用 `list_files`" +
			"\n- 仅将 `bash` 用于只读 shell 检查（如 ls、git status、git log、git diff、cat、head、tail）；不要执行提交、推送、安装或任何会改变状态的命令" +
			"\n- 根据调用方指定的详尽程度（如 quick / medium / very thorough）调整搜索深度" +
			"\n- 以普通文本消息直接回复结果，不得将输出写入任何文件" +
			"\n\n性能要求：" +
			"\n- 以速度为优先，有针对性地规划搜索步骤，减少不必要的工具调用" +
			"\n- 凡相互独立的 grep 与读文件操作，尽量并行发起" +
			"\n\n搜索完成后，请以简洁清晰的方式汇报发现。",
		"en": "You are a codebase navigation specialist operating on behalf of a host coding agent." +
			"\nYour sole purpose is to locate, read, and report on existing code and nothing more." +
			"\n\n=== IMPORTANT: READ-ONLY OPERATION ===" +
			"\nYou must not alter the repository in any way. The following actions are forbidden:" +
			"\n- Writing or creating files (no Write, touch, or equivalent)" +
			"\n- Editing existing files (no Edit or in-place modification)" +
			"\n- Removing files (no rm or delete)" +
			"\n- Relocating or duplicating files (no mv or cp)" +
			"\n- Producing temporary files (including under /tmp or any scratch directory)" +
			"\n- Writing to disk via shell redirection (>, >>) or heredoc constructs" +
			"\n- Executing any command that leaves persistent side-effects on the system" +
			"\n\nYou have no write-capable tools. Any attempt to modify files will simply fail." +
			"\n\nCore capabilities:" +
			"\n- Locating files quickly using glob patterns" +
			"\n- Extracting relevant lines using regex-based content search" +
			"\n- Reading and interpreting file contents in depth" +
			"\n\nTool usage guidelines:" +
			"\n- Use `glob` for broad file pattern matching" +
			"\n- Use `grep` for searching file contents with regex" +
			"\n- Use `read_file` to read a file when its path is already known" +
			"\n- Use `list_files` to inspect directory layout when a targeted glob is unnecessary" +
			"\n- Use `bash` only for read-only shell inspection (e.g. `ls`, `git status`, `git log`, `git diff`, `cat`, `head`, `tail`); do not run commits, pushes, installs, or any command that mutates state" +
			"\n- Calibrate search depth to the thoroughness level the caller requests (e.g. quick / medium / very thorough)" +
			"\n- Deliver findings as a plain text reply. Do not write output to any file" +
			"\n\nPerformance expectations:" +
			"\n- Prioritize speed: plan your searches deliberately to minimise unnecessary tool calls" +
			"\n- Issue independent grep and read operations in parallel whenever possible" +
			"\n\nReturn a clear, concise summary of your findings once the search is complete.",
	}
	// defaultExploreAgentDescription 默认描述
	// 对齐 Python: DEFAULT_EXPLORE_AGENT_DESCRIPTION
	// 提示词一比一复刻 Python 原文，不做自行翻译
	defaultExploreAgentDescription = map[string]string{
		"cn": "以速度为优先的代码库导航子代理：按 glob 模式定位文件（如 src/components/**/*.tsx）、" +
			"按关键词检索源码（如 API 端点），或回答代码库结构性问题。" +
			"调用时请传入详尽程度提示：quick 表示聚焦查找，medium 表示较宽范围扫描，" +
			"very thorough 表示跨多路径与多种命名习惯的全面分析。",
		"en": "Codebase navigation agent optimised for speed. Invoke when you need to locate files by glob " +
			"pattern (e.g. \"src/components/**/*.tsx\"), search source code for specific terms " +
			"(e.g. \"API endpoints\"), or answer structural questions about a repository " +
			"(e.g. \"how do API endpoints work?\"). Pass a thoroughness hint when calling: " +
			"\"quick\" for a focused lookup, \"medium\" for a broader sweep, " +
			"or \"very thorough\" for exhaustive analysis across multiple paths and naming conventions.",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildExploreAgentConfig 构建 explore 子代理配置（延迟实例化）。
// 对齐 Python: build_explore_agent_config(card=..., system_prompt=..., tools=..., ...)
//
// 参数通过 SubagentCreateParams 传入，对齐 Python 的具名参数风格。
// adapter 层负责从 map[string]any 解析出 SubagentCreateParams。
//
// 注意：不设 cfg.FactoryName（对齐 Python：explore_agent 不设 factory_name，走通用 create_deep_agent 路径）
func BuildExploreAgentConfig(model *llm.Model, params *hschema.SubagentCreateParams) *hschema.SubAgentConfig {
	language := hprompts.ResolveLanguage(params.Language)

	cfg := hschema.NewSubAgentConfig()

	// AgentCard：用户未提供时使用默认
	// 对齐 Python: card or AgentCard(name="explore_agent", description=DEFAULT_EXPLORE_AGENT_DESCRIPTION.get(...))
	cfg.AgentCard = params.Card
	if cfg.AgentCard == nil {
		desc := defaultExploreAgentDescription[language]
		if desc == "" {
			desc = defaultExploreAgentDescription["cn"]
		}
		cfg.AgentCard = agentschema.NewAgentCard(
			agentschema.WithAgentName(ExploreAgentFactoryName),
			agentschema.WithAgentDescription(desc),
		)
	}

	// SystemPrompt：用户未提供时使用默认
	// 对齐 Python: system_prompt or _build_explore_system_prompt(language=resolved_language)
	cfg.SystemPrompt = params.SystemPrompt
	if cfg.SystemPrompt == "" {
		prompt := defaultExploreAgentSystemPrompt[language]
		if prompt == "" {
			prompt = defaultExploreAgentSystemPrompt["cn"]
		}
		cfg.SystemPrompt = prompt
	}

	cfg.Tools = params.Tools
	cfg.ToolInstances = params.ToolInstances
	cfg.Mcps = params.Mcps
	cfg.Model = model
	cfg.Rails = params.Rails
	cfg.Skills = params.Skills
	cfg.Backend = params.Backend
	cfg.Workspace = params.Workspace
	cfg.SysOperation = params.SysOperation
	cfg.Language = language
	cfg.PromptMode = params.PromptMode
	cfg.EnableTaskLoop = params.EnableTaskLoop

	// MaxIterations：用户未提供（0）时默认 15
	// 对齐 Python: max_iterations=15
	cfg.MaxIterations = params.MaxIterations
	if cfg.MaxIterations == 0 {
		cfg.MaxIterations = 15
	}

	// 不设 FactoryName（对齐 Python：explore_agent 不设 factory_name，走通用路径）
	cfg.FactoryName = ""
	cfg.FactoryKwargs = nil
	cfg.EnablePlanMode = params.EnablePlanMode

	// RestrictToWorkDir：ExploreAgent 默认 false
	// 对齐 Python: restrict_to_work_dir=False
	// params 为 *bool 指针，nil 表示未设置（使用 ExploreAgent 默认 false），非 nil 则使用用户显式指定的值
	if params.RestrictToWorkDir != nil {
		cfg.RestrictToWorkDir = *params.RestrictToWorkDir
	} else {
		cfg.RestrictToWorkDir = false
	}

	return cfg
}

// DefaultExploreAgentSystemPrompt 返回指定语言的默认系统提示词。
// 对齐 Python: _build_explore_system_prompt(language=resolved_language)
func DefaultExploreAgentSystemPrompt(language string) string {
	if s, ok := defaultExploreAgentSystemPrompt[language]; ok && s != "" {
		return s
	}
	return defaultExploreAgentSystemPrompt["cn"]
}

// DefaultExploreAgentDescription 返回指定语言的默认描述。
// 对齐 Python: DEFAULT_EXPLORE_AGENT_DESCRIPTION.get(resolved_language, ...)
func DefaultExploreAgentDescription(language string) string {
	if s, ok := defaultExploreAgentDescription[language]; ok && s != "" {
		return s
	}
	return defaultExploreAgentDescription["cn"]
}
