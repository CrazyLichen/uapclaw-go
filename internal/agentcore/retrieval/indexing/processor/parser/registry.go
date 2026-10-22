package parser

import (
	"fmt"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────
// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识（对齐 chunker/indexer 层先例：
	// chunker/char_chunker.go 与 indexer/base.go 同款常量）
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// parserRegistry 扩展名 → 解析器工厂。并发访问由 parserRegistryMu 保护
	//（Python 模块级 dict 无并发问题，Go 包级 map 需要互斥——chunker registry 同款先例）。
	//
	// Python: auto_file_parser.py _PARSER_REGISTRY
	parserRegistry = map[string]func() Parser{}
	// parserRegistryMu registry 互斥锁
	parserRegistryMu sync.Mutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterParser 按扩展名列表注册解析器工厂。
//
// 扩展名小写归一（".JSON"→".json"）；已存在的扩展名静默跳过（first-wins，
// 字面对齐 Python if normalized_ext not in _PARSER_REGISTRY）。
// 与 chunker registry 的"重名报错"风格不同——Python 两处 registry 语义
// 本就不同，各自忠实对齐。13.11 的 registerNewParser 届时按 last-wins
// 覆盖语义单独实现。
//
// Python: auto_file_parser.py register_parser 装饰器（Go 无装饰器，
// 各具体解析器文件 init() 调用本函数，import 即触发，语义对齐
// 装饰器 import 时执行）
func RegisterParser(fileExtensions []string, factory func() Parser) {
	parserRegistryMu.Lock()
	defer parserRegistryMu.Unlock()
	for _, ext := range fileExtensions {
		normalizedExt := strings.ToLower(ext)
		if _, exists := parserRegistry[normalizedExt]; exists {
			continue
		}
		parserRegistry[normalizedExt] = factory
		// 对齐 Python: logger.info(f"Registered parser {parser_class.__name__} for {normalized_ext}")
		// Python 取类名（类对象非实例）；Go 工厂闭包无类型名可取，
		// 实例化一次取名是最低成本近似（注册表内容零偏差，归档为已知偏差）
		logger.Info(logComponent).
			Str("parser_name", fmt.Sprintf("%T", factory())).
			Str("ext", normalizedExt).
			Msg("registered parser")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getParserFactory 按扩展名取工厂。调用方（13.11 AutoFileParser）传入
// 小写归一后的扩展名；未注册返回 false。
//
// Python: auto_file_parser.py parse 中 _PARSER_REGISTRY[file_ext]() 查取
func getParserFactory(fileExt string) (func() Parser, bool) {
	parserRegistryMu.Lock()
	defer parserRegistryMu.Unlock()
	factory, exists := parserRegistry[fileExt]
	return factory, exists
}

// snapshotRegistry 返回当前注册扩展名列表（测试用）。
func snapshotRegistry() []string {
	parserRegistryMu.Lock()
	defer parserRegistryMu.Unlock()
	exts := make([]string, 0, len(parserRegistry))
	for k := range parserRegistry {
		exts = append(exts, k)
	}
	return exts
}

// parserTypeName 取解析器实例的类型名（nil 防御返回 "unknown"）。
func parserTypeName(p Parser) string {
	if p == nil {
		return "unknown"
	}
	return fmt.Sprintf("%T", p)
}
