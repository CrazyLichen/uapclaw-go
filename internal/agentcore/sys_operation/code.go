package sys_operation

import (
	"context"
	"fmt"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeOperation 代码执行接口，定义代码执行操作。
// 对齐 Python BaseCodeOperation：execute_code, execute_code_stream, list_tools。
type CodeOperation interface {
	// ExecuteCode 执行代码
	ExecuteCode(ctx context.Context, code string, opts ...CodeOption) (*result.ExecuteCodeResult, error)
	// ExecuteCodeStream 流式执行代码
	ExecuteCodeStream(ctx context.Context, code string, opts ...CodeOption) (<-chan result.ExecuteCodeStreamResult, error)
	// ListTools 返回代码执行的工具卡片列表
	ListTools() []*tool.ToolCard
}

// CodeOption 代码执行选项函数
type CodeOption func(*CodeOptions)

// CodeOptions 代码执行选项。
// 对齐 Python execute_code 签名：code, language, timeout, environment, cwd, options。
type CodeOptions struct {
	// Language 编程语言
	Language string
	// Timeout 超时时间（秒）
	Timeout int
	// Environment 环境变量
	Environment map[string]string
	// Cwd 工作目录
	Cwd string
	// Options 扩展配置选项
	Options map[string]any
}

// BaseCodeOperation CodeOperation 的空操作桩实现
type BaseCodeOperation struct {
	BaseOperation
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCodeOptions 从选项列表构造 CodeOptions
func NewCodeOptions(opts ...CodeOption) *CodeOptions {
	o := &CodeOptions{Language: "python", Timeout: 300}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// WithCodeLanguage 设置代码执行编程语言
func WithCodeLanguage(lang string) CodeOption {
	return func(o *CodeOptions) { o.Language = lang }
}

// WithCodeTimeout 设置代码执行超时时间
func WithCodeTimeout(timeout int) CodeOption {
	return func(o *CodeOptions) { o.Timeout = timeout }
}

// WithCodeEnvironment 设置代码执行环境变量
func WithCodeEnvironment(env map[string]string) CodeOption {
	return func(o *CodeOptions) { o.Environment = env }
}

// WithCodeCwd 设置代码执行工作目录
func WithCodeCwd(cwd string) CodeOption {
	return func(o *CodeOptions) { o.Cwd = cwd }
}

// WithCodeOptions 设置扩展配置选项
func WithCodeOptions(options map[string]any) CodeOption {
	return func(o *CodeOptions) { o.Options = options }
}

// ExecuteCode 执行代码（BaseCodeOperation 空实现）
func (b *BaseCodeOperation) ExecuteCode(_ context.Context, _ string, _ ...CodeOption) (*result.ExecuteCodeResult, error) {
	return nil, fmt.Errorf("未实现: ExecuteCode")
}

// ExecuteCodeStream 流式执行代码（BaseCodeOperation 空实现）
func (b *BaseCodeOperation) ExecuteCodeStream(_ context.Context, _ string, _ ...CodeOption) (<-chan result.ExecuteCodeStreamResult, error) {
	return nil, fmt.Errorf("未实现: ExecuteCodeStream")
}

// ListTools 返回工具卡片列表（BaseCodeOperation 空实现）
func (b *BaseCodeOperation) ListTools() []*tool.ToolCard { return nil }
