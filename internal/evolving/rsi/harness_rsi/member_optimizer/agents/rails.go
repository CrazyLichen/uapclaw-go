// rails.go 提供 MemberOptimizer agent 的护栏装配与作用域文件工具。
// Python: factory.py 的 _build_rails / _build_skill_rail / _ScopedPackageTool 段。

package agents

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BuildRailsParams BuildRails 参数。
type BuildRailsParams struct {
	// Profile agent 档案
	Profile MemberOptimizerAgentProfile
	// AgentSkillsDirs agent 技能目录
	AgentSkillsDirs []string
	// ExtraRails 额外护栏
	ExtraRails []agentinterfaces.AgentRail
	// EnabledSkillsOverride 启用技能覆盖
	EnabledSkillsOverride []string
}

// ScopedPackageTool 保持原生文件系统语义的同时强制候选包边界。
// Python: factory.py:423-467 _ScopedPackageTool(Tool)。
type ScopedPackageTool struct {
	// inner 被包装的原生工具
	inner tool.Tool
	// workspacePath 工作空间绝对路径
	workspacePath string
	// readRoots 允许读取的根目录
	readRoots []string
	// declaredPaths 声明写路径
	declaredPaths []string
	// repair 是否 repair 模式
	repair bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewScopedPackageTool 构造作用域包工具。
func NewScopedPackageTool(inner tool.Tool, workspacePath string, readRoots []string, declaredPaths []string, repair bool) *ScopedPackageTool {
	return &ScopedPackageTool{
		inner:         inner,
		workspacePath: workspacePath,
		readRoots:     readRoots,
		declaredPaths: declaredPaths,
		repair:        repair,
	}
}

// Card 返回工具卡片（转调内层）。
func (s *ScopedPackageTool) Card() *tool.ToolCard { return s.inner.Card() }

// Invoke 执行工具：写路径强制包边界，读路径强制根目录白名单。
// Python: factory.py:434-462 invoke。
func (s *ScopedPackageTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	if inputs == nil {
		return nil, fmt.Errorf("tool inputs must be an object")
	}
	// Python: writing = isinstance(self._tool, (WriteFileTool, EditFileTool))
	writing := isWriteTool(s.inner)
	// Python: key = "file_path" if isinstance(...(ReadFileTool, WriteFileTool, EditFileTool)) else "path"
	key := "path"
	if isFilePathTool(s.inner) {
		key = "file_path"
	}
	value, exists := inputs[key]
	if !exists {
		value = "."
	}
	strValue, isStr := value.(string)
	if !isStr || strings.TrimSpace(strValue) == "" {
		return nil, fmt.Errorf("%s must be a path string", key)
	}
	rawPath := expandUserPath(strValue)
	absPath := rawPath
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(s.workspacePath, rawPath)
	}
	absPath, _ = filepath.Abs(absPath)
	if writing {
		// Python: relative = path.relative_to(self._workspace); 越界报错
		rel, relErr := filepath.Rel(s.workspacePath, absPath)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("write is outside the permitted package paths")
		}
		// Python: if not package_write_allowed(relative.as_posix(), ...): raise ValueError(...)
		if !PackageWriteAllowed(filepath.ToSlash(rel), s.declaredPaths, s.repair) {
			return nil, fmt.Errorf("write is outside the permitted package paths")
		}
		// Python: any(parent.is_symlink() for parent in [lexical, *lexical.parents] if parent != self._workspace)
		check := absPath
		for {
			parent := filepath.Dir(check)
			if parent == s.workspacePath || check == parent {
				break
			}
			if isSymlink(check) {
				return nil, fmt.Errorf("writes through symlinks are forbidden")
			}
			check = parent
		}
	} else if !isUnderAnyRoot(absPath, s.readRoots) {
		return nil, fmt.Errorf("read is outside the package and supported host contracts")
	}
	// Python: if isinstance(self._tool, GlobTool): pattern 校验
	if isGlobTool(s.inner) {
		pattern, _ := inputs["pattern"].(string)
		if filepath.IsAbs(pattern) || containsDotDot(splitPathParts(pattern)) {
			return nil, fmt.Errorf("glob pattern must stay within the search path")
		}
	}
	scopedInputs := map[string]any{}
	for k, v := range inputs {
		scopedInputs[k] = v
	}
	scopedInputs[key] = absPath
	return s.inner.Invoke(ctx, scopedInputs, opts...)
}

// Stream 流式执行（转调内层）。Python: factory.py:465-467 stream。
func (s *ScopedPackageTool) Stream(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return s.inner.Stream(ctx, inputs, opts...)
}

// GetCallTimeout 外层预算（转调内层）。
func (s *ScopedPackageTool) GetCallTimeout(inputs map[string]any, defaultTimeout float64) float64 {
	return s.inner.GetCallTimeout(inputs, defaultTimeout)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// BuildRails 装配护栏清单。Python: factory.py:319-335 _build_rails。
func BuildRails(params BuildRailsParams) []agentinterfaces.AgentRail {
	railsList := []agentinterfaces.AgentRail{}
	enabledSkills := params.Profile.EnabledSkills
	if params.EnabledSkillsOverride != nil {
		enabledSkills = params.EnabledSkillsOverride
	}
	if len(enabledSkills) > 0 {
		if skillRail := buildSkillRail(enabledSkills, params.AgentSkillsDirs); skillRail != nil {
			railsList = append(railsList, skillRail)
		}
	}
	if len(params.ExtraRails) > 0 {
		railsList = append(railsList, params.ExtraRails...)
	}
	return railsList
}

// buildSkillRail 装配技能护栏；无可用技能返回 nil。Python: factory.py:469-498。
func buildSkillRail(requestedSkills []string, agentSkillsDirs []string) agentinterfaces.AgentRail {
	// Python: roots = [_SKILLS_DIR, *agent_skills_dirs]
	packageSkillsDir := filepath.Join(findRepoRoot(), "internal", "evolving", "rsi",
		"harness_rsi", "member_optimizer", "agents", "skills")
	roots := append([]string{packageSkillsDir}, agentSkillsDirs...)
	skillsDir := []string{}
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		for _, skillName := range requestedSkills {
			if info2, err2 := os.Stat(filepath.Join(root, skillName)); err2 == nil && info2.IsDir() {
				skillsDir = append(skillsDir, root)
				break
			}
		}
	}
	enabledSkills := []string{}
	for _, skillName := range requestedSkills {
		for _, root := range skillsDir {
			if info, err := os.Stat(filepath.Join(root, skillName)); err == nil && info.IsDir() {
				enabledSkills = append(enabledSkills, skillName)
				break
			}
		}
	}
	if len(enabledSkills) == 0 {
		return nil
	}
	// Python: SkillUseRail(skills_dir=..., skill_mode="all", enabled_skills=..., include_tools=False)
	return skills.NewSkillUseRail(skillsDir,
		skills.WithSkillMode("all"),
		skills.WithEnabledSkills(enabledSkills),
		skills.WithIncludeTools(false))
}

// isWriteTool 是否写类工具（Python isinstance (WriteFileTool, EditFileTool)；
// Go 侧具体类型未导出，以工具名判别——BuildToolCard 名即 Python 类名小写）。
func isWriteTool(inner tool.Tool) bool {
	name := inner.Card().Name
	return name == "write_file" || name == "edit_file"
}

// isFilePathTool 是否以 file_path 为路径键的工具。
func isFilePathTool(inner tool.Tool) bool {
	name := inner.Card().Name
	return name == "read_file" || name == "write_file" || name == "edit_file"
}

// isGlobTool 是否 glob 工具。
func isGlobTool(inner tool.Tool) bool {
	return inner.Card().Name == "glob"
}

// isSymlink 路径是否符号链接。
func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSymlink != 0
}

// isUnderAnyRoot 路径是否在任一根之下。
func isUnderAnyRoot(path string, roots []string) bool {
	for _, root := range roots {
		if path == root || isUnderPath(path, root) {
			return true
		}
	}
	return false
}

// execLookPath 封装 exec.LookPath（便于测试注入）。
var execLookPath = exec.LookPath
