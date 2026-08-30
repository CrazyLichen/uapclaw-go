package prompts

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PromptApplier 提示词模板加载器，运行时读取 .md 文件并缓存。
//
// 单例模式（对齐 Python: PromptApplier(metaclass=Singleton)），
// 缓存已加载的 PromptTemplate 实例，避免重复 I/O。
//
// 对应 Python: openjiuwen/core/memory/prompts/prompt_applier.py (PromptApplier)
type PromptApplier struct {
	// cache 已加载的模板缓存：file_prefix → *prompt.PromptTemplate
	cache sync.Map
	// promptDir 模板文件目录路径
	promptDir string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// defaultApplierInstance 全局单例实例
	defaultApplierInstance *PromptApplier
	// defaultApplierOnce 单例初始化控制
	defaultApplierOnce sync.Once
)

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultApplier 返回全局 PromptApplier 单例。
// 模板目录通过 runtime.Caller(0) 获取当前文件所在目录（对齐 Python: Path(__file__).parent）。
func DefaultApplier() *PromptApplier {
	defaultApplierOnce.Do(func() {
		_, thisFile, _, ok := runtime.Caller(0)
		if !ok {
			panic("无法获取 PromptApplier 源文件路径")
		}
		dir := filepath.Dir(thisFile)
		defaultApplierInstance = NewPromptApplier(dir)
		logger.Info(logComponent).Msg("PromptApplier 单例初始化")
	})
	return defaultApplierInstance
}

// NewPromptApplier 创建 PromptApplier 实例。
// dir 为模板 .md 文件所在目录。
func NewPromptApplier(dir string) *PromptApplier {
	return &PromptApplier{
		promptDir: dir,
	}
}

// Apply 加载模板并替换变量，返回填充后的字符串。
//
// 对齐 Python: PromptApplier.apply(file_prefix, variables)
//
// 流程：
//  1. 缓存命中 → template.Format(variables) → 返回 Content 字符串
//  2. 缓存未命中 → 读取 {promptDir}/{filePrefix}.md → 创建 PromptTemplate → 缓存 → Format → 返回
//  3. 文件不存在 → 返回 error
func (a *PromptApplier) Apply(filePrefix string, variables map[string]any) (string, error) {
	tmpl, err := a.GetTemplate(filePrefix)
	if err != nil {
		return "", err
	}
	formatted, err := tmpl.Format(variables)
	if err != nil {
		return "", fmt.Errorf("应用提示词模板 %q 变量替换失败: %w", filePrefix, err)
	}
	content, ok := formatted.Content.(string)
	if !ok {
		return "", fmt.Errorf("提示词模板 %q 格式化后内容类型不是 string", filePrefix)
	}
	logger.Debug(logComponent).Str("file_prefix", filePrefix).Msg("已应用提示词模板")
	return content, nil
}

// GetTemplate 获取已缓存的 PromptTemplate，未缓存则加载。
//
// 对齐 Python: PromptApplier.get_template(file_prefix)
func (a *PromptApplier) GetTemplate(filePrefix string) (*prompt.PromptTemplate, error) {
	if cached, ok := a.cache.Load(filePrefix); ok {
		logger.Debug(logComponent).Str("file_prefix", filePrefix).Msg("使用缓存的提示词模板")
		return cached.(*prompt.PromptTemplate), nil
	}

	filePath := filepath.Join(a.promptDir, filePrefix+".md")
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("提示词模板文件不存在: %s: %w", filePath, err)
	}

	tmpl := prompt.NewPromptTemplate(filePrefix, string(content))
	a.cache.Store(filePrefix, tmpl)
	logger.Info(logComponent).Str("file_prefix", filePrefix).Msg("加载并缓存提示词模板")
	return tmpl, nil
}

// ClearCache 清除缓存。
//
// 对齐 Python: PromptApplier.clear_cache(file_prefix=None)
// 无参数时清除所有缓存；指定 filePrefix 时只清除该条目。
func (a *PromptApplier) ClearCache(filePrefix ...string) {
	if len(filePrefix) == 0 {
		a.cache = sync.Map{}
		logger.Info(logComponent).Msg("清除所有提示词模板缓存")
	} else {
		for _, prefix := range filePrefix {
			a.cache.Delete(prefix)
			logger.Info(logComponent).Str("file_prefix", prefix).Msg("清除提示词模板缓存")
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
