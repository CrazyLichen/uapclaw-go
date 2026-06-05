package workspace

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InitOption 工作区初始化选项。
//
// 对应 Python: init_user_workspace(overwrite, workspace_dir) + prepare_workspace(overwrite, preferred_language, workspace_dir)
type InitOption struct {
	Overwrite    bool   // 是否强制清理重建（-f 标志）
	Language     string // "zh" 或 "en"，空则交互询问
	WorkspaceDir string // 自定义工作区路径，空则用 WorkspaceDir()
	InstanceName string // 命名实例名称，空则为默认实例
}

// InitResult 工作区初始化结果。
//
// 对应 Python: init_user_workspace 返回值
type InitResult struct {
	WorkspaceDir string        // 实际使用的工作区路径
	Diff         CopyDiffResult // 文件变更差异
	Cancelled    bool          // 用户取消
}

// ──────────────────────────── 常量 ────────────────────────────

// 多语言文件映射：模板源名 → 目标名
// 对应 Python: prepare_workspace 中的 multilang_files
var multilangFiles = []struct {
	srcSuffix string // 如 "AGENT_ZH.md"
	dstName   string // 如 "AGENT.md"
}{
	{"AGENT", "AGENT.md"},
	{"HEARTBEAT", "HEARTBEAT.md"},
	{"IDENTITY", "IDENTITY.md"},
	{"SOUL", "SOUL.md"},
}

// memory 多语言文件映射
var memoryMultilangFiles = []struct {
	srcSuffix string
	dstName   string
}{
	{"MEMORY", "MEMORY.md"},
}

// ──────────────────────────── 全局变量 ────────────────────────────

// log 全局日志实例。
// 对应 Python: logger = logging.getLogger(__name__)
var log = logger.GetLogger(logger.ComponentCommon)

// ──────────────────────────── 导出函数 ────────────────────────────

// Init 初始化工作区。
//
// 对应 Python: init_user_workspace(overwrite, workspace_dir)
//
// 完整流程：
//  1. 验证实例名称（如有）
//  2. 确定目标路径
//  3. overwrite 确认
//  4. 语言选择
//  5. 调用 Prepare
//  6. 命名实例：更新 instances.yaml + 创建 bootstrap .env
func Init(opt InitOption) (*InitResult, error) {
	// 1. 验证实例名称
	if opt.InstanceName != "" {
		if err := ValidateInstanceName(opt.InstanceName); err != nil {
			return nil, fmt.Errorf("实例名称无效: %w", err)
		}
	}

	// 2. 确定目标路径
	targetDir := opt.WorkspaceDir
	if targetDir == "" {
		if opt.InstanceName != "" {
			targetDir = InstanceWorkspacePath(opt.InstanceName)
		} else {
			targetDir = WorkspaceDir()
		}
	}

	// 3. overwrite 确认
	if dirExists(targetDir) {
		if opt.Overwrite {
			if isInteractive() {
				fmt.Printf("[uapclaw-init] 使用 -f/--force 标志，%s 将被删除以进行全新初始化。\n", targetDir)
				fmt.Println("[uapclaw-init] 警告：这将删除所有历史配置和记忆信息。")
				fmt.Println("[uapclaw-init] 此操作无法撤销。")
				confirmed := promptYesNo("[uapclaw-init] 确认重新初始化？(yes/no): ")
				if !confirmed {
					fmt.Println("[uapclaw-init] 初始化已取消。")
					return &InitResult{Cancelled: true}, nil
				}
			} else {
				fmt.Println("[uapclaw-init] 非交互模式：继续重新初始化。")
			}
			if err := os.RemoveAll(targetDir); err != nil {
				log.Error().Str("dir", targetDir).Err(err).Msg("删除工作区失败")
				return nil, fmt.Errorf("删除工作区失败: %w", err)
			}
			log.Info().Str("dir", targetDir).Msg("已删除工作区目录")
			fmt.Printf("[uapclaw-init] 已删除工作区目录: %s\n", targetDir)
		} else {
			fmt.Println("[uapclaw-init] 增量初始化：只添加缺失文件，不覆盖已有文件")
			fmt.Println("[uapclaw-init] 此操作不可撤销。")
			if isInteractive() {
				confirmed := promptYesNo("[uapclaw-init] 继续吗？(yes/no): ")
				if !confirmed {
					fmt.Println("[uapclaw-init] 初始化已取消。")
					return &InitResult{Cancelled: true}, nil
				}
			} else {
				fmt.Println("[uapclaw-init] 非交互模式：继续增量初始化。")
			}
		}
	}

	// 4. 语言选择
	lang := opt.Language
	if lang == "" {
		lang = promptPreferredLanguage()
		if lang == "" {
			fmt.Println("[uapclaw-init] 未选择语言，初始化已取消。")
			return &InitResult{Cancelled: true}, nil
		}
	}
	log.Info().Str("language", lang).Msg("使用语言")
	fmt.Printf("[uapclaw-init] 使用语言 / Language: %s\n", lang)

	// 5. 调用 Prepare
	diff, err := Prepare(InitOption{
		Overwrite:    opt.Overwrite,
		Language:     lang,
		WorkspaceDir: targetDir,
	})
	if err != nil {
		return nil, err
	}

	// 6. 命名实例：更新 instances.yaml + 创建 bootstrap .env
	if opt.InstanceName != "" {
		// 计算端口
		index, err := GetInstanceIndex(opt.InstanceName)
		if err != nil {
			return nil, fmt.Errorf("获取实例序号失败: %w", err)
		}
		ports := CalculateInstancePorts(index)

		// 更新 instances.yaml
		if err := UpdateInstancesYAML(opt.InstanceName, targetDir, ports); err != nil {
			return nil, fmt.Errorf("更新 instances.yaml 失败: %w", err)
		}

		// 创建 bootstrap .env
		config := &InstanceConfig{
			Name:      opt.InstanceName,
			Workspace: targetDir,
			Ports:     ports,
		}
		if _, err := CreateBootstrapEnv(config); err != nil {
			return nil, fmt.Errorf("创建 bootstrap .env 失败: %w", err)
		}

		log.Info().Str("instance", opt.InstanceName).Str("dir", targetDir).Msg("实例初始化成功")
		fmt.Printf("[uapclaw-init] 实例 '%s' 初始化成功。\n", opt.InstanceName)
	}

	// 打印差异摘要
	diff.PrintSummary(opt.Overwrite)

	log.Info().Str("dir", targetDir).Bool("overwrite", opt.Overwrite).Int("added_files", len(diff.AddedFiles)).Int("overwritten_files", len(diff.OverwrittenFiles)).Msg("工作区初始化完成")

	return &InitResult{
		WorkspaceDir: targetDir,
		Diff:         *diff,
	}, nil
}

// Prepare 复制模板文件到工作区。
//
// 对应 Python: prepare_workspace(overwrite, preferred_language, workspace_dir)
//
// 流程：
//  1. 确保根目录存在
//  2. 复制 config.yaml → <workspace>/config/config.yaml
//  3. 复制 .env.template → <workspace>/config/.env
//  4. 创建 agent/.checkpoint + agent/.logs 目录
//  5. 复制 agent/workspace/ 模板
//  6. 复制多语言文件
//  7. 创建 agent/sessions 目录
//  8. 设置 preferred_language 到 config
func Prepare(opt InitOption) (*CopyDiffResult, error) {
	workspaceDir := opt.WorkspaceDir
	if workspaceDir == "" {
		workspaceDir = WorkspaceDir()
	}

	// 获取 resources 目录
	resDir, err := ResourcesDir()
	if err != nil {
		return nil, fmt.Errorf("找不到资源目录: %w", err)
	}

	log.Info().Str("workspace", workspaceDir).Str("resources", resDir).Bool("overwrite", opt.Overwrite).Msg("开始准备工作区")

	// 确保工作区根目录存在
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建工作区目录失败: %w", err)
	}

	cumulativeDiff := CopyDiffResult{}

	// 解析语言
	lang := resolvePreferredLanguage(opt.Language, workspaceDir)

	// ----- 复制 config.yaml -----
	configSrc := filepath.Join(resDir, "config.yaml")
	configDestDir := filepath.Join(workspaceDir, "config")
	if err := os.MkdirAll(configDestDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建 config 目录失败: %w", err)
	}
	configDest := filepath.Join(configDestDir, "config.yaml")
	if opt.Overwrite || !fileExists(configDest) {
		if err := copyFileWithDiff(configSrc, configDest, &cumulativeDiff); err != nil {
			return nil, fmt.Errorf("复制 config.yaml 失败: %w", err)
		}
	}

	// ----- 复制 .env.template → config/.env -----
	envSrc := filepath.Join(resDir, ".env.template")
	envDest := filepath.Join(configDestDir, ".env")
	if opt.Overwrite || !fileExists(envDest) {
		if err := copyFileWithDiff(envSrc, envDest, &cumulativeDiff); err != nil {
			return nil, fmt.Errorf("复制 .env.template 失败: %w", err)
		}
	}

	// ----- 创建 agent 目录结构 -----
	agentRoot := filepath.Join(workspaceDir, "agent")
	os.MkdirAll(filepath.Join(agentRoot, ".checkpoint"), 0o755)
	os.MkdirAll(filepath.Join(agentRoot, ".logs"), 0o755)
	os.MkdirAll(filepath.Join(agentRoot, "sessions"), 0o755)

	// ----- 复制 DeepAgent workspace 模板 -----
	deepAgentWorkspace := filepath.Join(agentRoot, "workspace")
	templateWorkspace := filepath.Join(resDir, "agent", "workspace")

	if dirExists(templateWorkspace) {
		// 忽略 _ZH.md/_EN.md（由多语言逻辑处理）和 skills（暂不复制内置技能）
		ignorePatterns := []string{"*_ZH.md", "*_EN.md", "skills"}
		if opt.Overwrite {
			if err := copyDirWithDiff(templateWorkspace, deepAgentWorkspace, &cumulativeDiff, ignorePatterns); err != nil {
				return nil, fmt.Errorf("复制 workspace 模板失败: %w", err)
			}
		} else {
			if err := copyDirWithDiffIncremental(templateWorkspace, deepAgentWorkspace, &cumulativeDiff, ignorePatterns); err != nil {
				return nil, fmt.Errorf("复制 workspace 模板失败: %w", err)
			}
		}
	} else {
		os.MkdirAll(deepAgentWorkspace, 0o755)
	}

	// ----- 复制 memory 模板 -----
	templateMemory := filepath.Join(templateWorkspace, "memory")
	agentMemory := filepath.Join(deepAgentWorkspace, "memory")
	if dirExists(templateMemory) {
		ignorePatterns := []string{"*_ZH.md", "*_EN.md"}
		if opt.Overwrite {
			if err := copyDirWithDiff(templateMemory, agentMemory, &cumulativeDiff, ignorePatterns); err != nil {
				return nil, fmt.Errorf("复制 memory 模板失败: %w", err)
			}
		} else {
			if err := copyDirWithDiffIncremental(templateMemory, agentMemory, &cumulativeDiff, ignorePatterns); err != nil {
				return nil, fmt.Errorf("复制 memory 模板失败: %w", err)
			}
		}
	}

	// ----- 复制多语言文件 -----
	suffix := "_ZH"
	if lang == "en" {
		suffix = "_EN"
	}

	for _, mf := range multilangFiles {
		srcName := mf.srcSuffix + suffix + ".md"
		srcPath := filepath.Join(templateWorkspace, srcName)
		dstPath := filepath.Join(deepAgentWorkspace, mf.dstName)

		if fileExists(srcPath) && !fileExists(dstPath) {
			if err := copyFileWithDiff(srcPath, dstPath, &cumulativeDiff); err != nil {
				return nil, fmt.Errorf("复制 %s 失败: %w", srcName, err)
			}
		}
	}

	for _, mf := range memoryMultilangFiles {
		srcName := mf.srcSuffix + suffix + ".md"
		srcPath := filepath.Join(templateWorkspace, "memory", srcName)
		dstPath := filepath.Join(agentMemory, mf.dstName)

		if fileExists(srcPath) && !fileExists(dstPath) {
			if err := copyFileWithDiff(srcPath, dstPath, &cumulativeDiff); err != nil {
				return nil, fmt.Errorf("复制 %s 失败: %w", srcName, err)
			}
		}
	}

	// ----- 设置 preferred_language 到 config -----
	setPreferredLanguage(configDest, lang)

	return &cumulativeDiff, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isInteractive 检查 stdin 是否连接到终端。
// 对应 Python: _is_interactive()
func isInteractive() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// promptYesNo 交互式 yes/no 确认。
func promptYesNo(prompt string) bool {
	fmt.Print(prompt)
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "yes" || input == "y"
}

// promptPreferredLanguage 交互询问语言偏好。
//
// 对应 Python: prompt_preferred_language()
// 非交互环境默认返回 "zh"。
func promptPreferredLanguage() string {
	if !isInteractive() {
		fmt.Println("[uapclaw-init] 非交互模式：使用默认语言 'zh'")
		return "zh"
	}

	fmt.Println()
	fmt.Println("[uapclaw-init] ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("[uapclaw-init]  请选择默认语言 / Choose your default language")
	fmt.Println("[uapclaw-init] ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("[uapclaw-init]   [1] 中文（简体）")
	fmt.Println("[uapclaw-init]       → config: preferred_language: zh")
	fmt.Println("[uapclaw-init]   ────────────────────────────────────────────")
	fmt.Println("[uapclaw-init]   [2] English")
	fmt.Println("[uapclaw-init]       → config: preferred_language: en")
	fmt.Println("[uapclaw-init] ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("[uapclaw-init]  须明确选择：1 / 2 / zh / en")
	fmt.Println("[uapclaw-init]  取消：no / n / q / cancel")
	fmt.Println("[uapclaw-init] ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	fmt.Print("[uapclaw-init] 请输入选项 (1, 2, zh, en) 或 no 取消: ")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	switch input {
	case "1", "zh", "中文", "chinese":
		return "zh"
	case "2", "en", "english", "e", "英文":
		return "en"
	case "no", "n", "q", "quit", "cancel", "取消":
		return ""
	default:
		fmt.Println("[uapclaw-init] 无效选项；未选择有效语言，初始化已取消。")
		return ""
	}
}

// resolvePreferredLanguage 确定初始化使用的语言。
//
// 优先级：显式参数 > 已有 config.yaml 中的 preferred_language > 默认 "zh"
// 对应 Python: _resolve_preferred_language()
func resolvePreferredLanguage(explicit string, workspaceDir string) string {
	if explicit != "" {
		lang := strings.TrimSpace(strings.ToLower(explicit))
		if lang == "zh" || lang == "en" {
			return lang
		}
		return "zh"
	}

	// 尝试从已有 config.yaml 读取
	configPath := filepath.Join(workspaceDir, "config", "config.yaml")
	if data, err := os.ReadFile(configPath); err == nil {
		// 简单解析 YAML 中的 preferred_language
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "preferred_language:") {
				lang := strings.TrimSpace(strings.TrimPrefix(line, "preferred_language:"))
				lang = strings.Trim(lang, "\"'")
				lang = strings.TrimSpace(strings.ToLower(lang))
				if lang == "zh" || lang == "en" {
					return lang
				}
			}
		}
	} else {
		log.Debug().Str("path", configPath).Err(err).Msg("读取 config.yaml 获取语言偏好失败，使用默认值")
	}

	return "zh"
}

// setPreferredLanguage 将语言偏好写入 config.yaml。
//
// 对应 Python: set_preferred_language_in_config_file()
func setPreferredLanguage(configPath string, lang string) {
	// 读取现有内容
	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}

	content := string(data)
	lines := strings.Split(content, "\n")

	// 查找并替换 preferred_language 行
	found := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "preferred_language:") {
			lines[i] = fmt.Sprintf("preferred_language: %s", lang)
			found = true
			break
		}
	}

	// 如果没有找到，追加
	if !found {
		lines = append(lines, fmt.Sprintf("preferred_language: %s", lang))
	}

	os.WriteFile(configPath, []byte(strings.Join(lines, "\n")), 0o644)
}

// fileExists 检查文件是否存在。
func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
