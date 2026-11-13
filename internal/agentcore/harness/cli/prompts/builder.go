package prompts

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	harnessprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// CLI agent 的系统提示词构建。
//
// 基于 harness 的 SystemPromptBuilder（自带 identity / tools / safety /
// runtime 等内置节），在其上叠加 CLI 专属的动态节：环境信息与
// OPENJIUWEN.md 项目记忆。
//
// Python: openjiuwen/harness/cli/prompts/builder.py（192 行）

// ──────────────────────────── 常量 ────────────────────────────

const (
	// MaxMemoryChars 记忆文本的总长度上限。
	// Python: builder.py:20 MAX_MEMORY_CHARS = 40_000
	MaxMemoryChars = 40000
)

// rootMarkers 向上寻找项目根时识别的标记文件。
// Python: builder.py:22-29 ROOT_MARKERS
var rootMarkers = []string{
	".git",
	"pyproject.toml",
	"package.json",
	"Cargo.toml",
	"go.mod",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// FindProjectRoot 从 cwd 向上找第一个带根标记的目录；找不到返回空串。
// Python: builder.py:31-39 _find_project_root
func FindProjectRoot(cwd string) string {
	absolute, err := filepath.Abs(cwd)
	if err != nil {
		return ""
	}
	current := absolute
	for {
		for _, marker := range rootMarkers {
			// 标记是目录（.git）或文件都算命中（Python 的 exists()）
			if _, statErr := os.Stat(filepath.Join(current, marker)); statErr == nil {
				return current
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

// LoadOpenjiuwenMd 加载并合并 OPENJIUWEN.md 记忆文件。
// Python: builder.py:41-72 _load_openjiuwen_md
//
// 读两层：用户级 ~/.openjiuwen/OPENJIUWEN.md 与项目级
// {project_root}/OPENJIUWEN.md。都缺时返回空串；总长超
// MaxMemoryChars 时截断并追加 "[...truncated]"。
func LoadOpenjiuwenMd(cwd string) string {
	var parts []string

	// 用户级（builder.py:52-58）
	home, err := os.UserHomeDir()
	if err == nil {
		userFile := filepath.Join(home, ".openjiuwen", "OPENJIUWEN.md")
		if content, readErr := os.ReadFile(userFile); readErr == nil {
			parts = append(parts, "### User-level memory\n"+string(content))
		}
	}

	// 项目级（builder.py:60-67）
	if projectRoot := FindProjectRoot(cwd); projectRoot != "" {
		projFile := filepath.Join(projectRoot, "OPENJIUWEN.md")
		if content, readErr := os.ReadFile(projFile); readErr == nil {
			parts = append(parts, "### Project-level memory\n"+string(content))
		}
	}

	if len(parts) == 0 {
		return ""
	}
	combined := strings.Join(parts, "\n\n")
	// Python: len(combined) 按 Unicode 码点计；Go 按 byte。中英混合的
	// 截断点会有差异，上限本身是防膨胀的软约束，按 byte 从宽处理。
	if len(combined) > MaxMemoryChars {
		combined = combined[:MaxMemoryChars] + "\n[...truncated]"
	}
	return combined
}

// BuildSystemPrompt 用 harness 构建器拼出完整系统提示词。
// Python: builder.py:107-155 build_system_prompt
//
// harness 构建器按提示词模式提供内置节（identity / tools / safety /
// runtime）。CLI 在其上叠加两节：
//   - environment（priority 20）：CWD、平台、模型、git 分支
//   - project_memory（priority 120）：OPENJIUWEN.md 内容
func BuildSystemPrompt(cwd, model, provider, language string) string {
	lang := harnessprompts.ResolveLanguage(language)
	builder := harnessprompts.NewSystemPromptBuilder(lang, harnessprompts.PromptModeFull)

	// 注入环境信息（builder.py:126-131）
	envText := buildEnvironmentSection(cwd, model, provider)
	builder.AddSection(saprompt.PromptSection{
		Name:     "environment",
		Content:  map[string]string{lang: envText},
		Priority: 20,
	})

	// 注入 OPENJIUWEN.md 项目记忆（builder.py:133-142）
	if memory := LoadOpenjiuwenMd(cwd); memory != "" {
		builder.AddSection(saprompt.PromptSection{
			Name:     "project_memory",
			Content:  map[string]string{lang: "## Project Memory\n" + memory},
			Priority: 120,
		})
	}
	return builder.Build()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// gitBranch 返回当前 git 分支名；失败返回空串。
// Python: builder.py:74-86 _get_git_branch（subprocess 5s 超时）
func gitBranch(cwd string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = cwd
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// buildEnvironmentSection 构造环境信息（每个会话重新生成）。
// Python: builder.py:88-103 _build_environment_section
func buildEnvironmentSection(cwd, model, provider string) string {
	branch := gitBranch(cwd)
	branchLabel := branch
	if branch == "" {
		branchLabel = "N/A"
	}
	return "## Environment\n" +
		"- CWD: " + cwd + "\n" +
		"- Platform: " + runtime.GOOS + " " + runtime.GOARCH + "\n" +
		"- Go: " + runtime.Version() + "\n" +
		"- Model: " + model + " (" + provider + ")\n" +
		"- Git branch: " + branchLabel + "\n" +
		"- Date: " + time.Now().UTC().Format("2006-01-02") + "\n"
}
