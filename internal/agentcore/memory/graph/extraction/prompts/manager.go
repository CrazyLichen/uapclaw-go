package prompts

import (
	"embed"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TemplateManager 提示词模板管理器（线程安全单例）
//
// Python: ThreadSafePromptManager
//
// 启动时通过 embed.FS 读取 prompts/cn/ 和 prompts/en/ 目录下的 .pr.md 文件，
// 解析为 PromptTemplate 并缓存到内存中。
type TemplateManager struct {
	// mu 保护 templates map 的读写锁。
	// 注意：sync.RWMutex 不可重入，禁止在 Get/registerInBulk 中递归调用自身。
	mu        sync.RWMutex
	templates map[string]*prompt.PromptTemplate
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// templateManagerInstance 单例实例
	templateManagerInstance *TemplateManager
	// templateManagerOnce 确保 single 初始化
	templateManagerOnce sync.Once

	//go:embed cn/*.pr.md en/*.pr.md
	promptFiles embed.FS
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetTemplateManager 获取 TemplateManager 单例
//
// Python: ThreadSafePromptManager()
func GetTemplateManager() *TemplateManager {
	templateManagerOnce.Do(func() {
		templateManagerInstance = &TemplateManager{
			templates: make(map[string]*prompt.PromptTemplate),
		}
		templateManagerInstance.init()
	})
	return templateManagerInstance
}

// Get 根据名称获取提示词模板
//
// Python: ThreadSafePromptManager.get(name)
func (m *TemplateManager) Get(name string) *prompt.PromptTemplate {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.templates[name]
}

// Contains 检查模板是否已注册
//
// Python: ThreadSafePromptManager.__contains__(key)
func (m *TemplateManager) Contains(name string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.templates[name]
	return ok
}

// AllNames 返回所有已注册模板名称
func (m *TemplateManager) AllNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.templates))
	for name := range m.templates {
		names = append(names, name)
	}
	return names
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 初始化模板管理器，通过 embed.FS 读取并加载所有 .pr.md 文件
func (m *TemplateManager) init() {
	languageDirs := []string{"cn", "en"}
	totalLoaded := 0

	for _, lang := range languageDirs {
		count, err := m.registerInBulk(lang)
		if err != nil {
			logger.Error(logger.ComponentAgentCore).Err(err).Str("lang", lang).Msg("加载提示词模板失败")
			continue
		}
		totalLoaded += count
	}

	logger.Info(logger.ComponentAgentCore).
		Int("count", totalLoaded).
		Msg("已加载全部提示词模板")
}

// registerInBulk 批量注册某目录下的 .pr.md 文件
//
// Python: ThreadSafePromptManager.register_in_bulk(prompt_dir, name)
func (m *TemplateManager) registerInBulk(lang string) (int, error) {
	pattern := lang + "/*.pr.md"

	// 文件扫描在锁内执行（对齐 Python: 同步扫描+注册）
	m.mu.Lock()
	paths, err := fs.Glob(promptFiles, pattern)
	if err != nil {
		m.mu.Unlock()
		return 0, exception.BuildError(
			exception.StatusMemoryGraphPromptFilesMissing,
			exception.WithParam("prompt_dir", lang),
			exception.WithParam("error_msg", err.Error()),
		)
	}
	if len(paths) == 0 {
		m.mu.Unlock()
		// 对齐 Python: raise build_error(StatusCode.MEMORY_GRAPH_PROMPT_FILES_MISSING)
		return 0, exception.BuildError(
			exception.StatusMemoryGraphPromptFilesMissing,
			exception.WithParam("prompt_dir", lang),
		)
	}

	for _, tPath := range paths {
		tName := filepath.Base(tPath)
		// 去掉 .pr.md 后缀
		tName = strings.TrimSuffix(tName, ".pr.md")

		// 读取文件内容
		data, err := promptFiles.ReadFile(tPath)
		if err != nil {
			logger.Error(logger.ComponentAgentCore).Str("path", tPath).Err(err).Msg("读取提示词模板文件失败")
			continue
		}

		// 解析为消息列表
		messages := ParsePRContent(string(data))
		if len(messages) == 0 {
			logger.Warn(logger.ComponentAgentCore).Str("path", tPath).Msg("提示词模板内容为空或解析失败")
			continue
		}

		m.templates[tName] = prompt.NewPromptTemplate(tName, messages)
	}
	m.mu.Unlock()

	// 日志在锁外输出（对齐 Python: 日志不阻塞其他操作）
	logger.Info(logger.ComponentAgentCore).
		Int("count", len(paths)).
		Str("name", lang).
		Msg("已加载提示词模板")

	return len(paths), nil
}
