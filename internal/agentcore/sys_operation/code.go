package sys_operation

import (
	"context"
	"fmt"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/result"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeOperation 代码操作接口
type CodeOperation interface {
	// ExecuteCode 执行代码
	ExecuteCode(ctx context.Context, code string, opts ...CodeOption) (*result.ExecuteCodeResult, error)
	// ExecuteCodeStream 流式执行代码
	ExecuteCodeStream(ctx context.Context, code string, opts ...CodeOption) (<-chan result.ExecuteCodeStreamResult, error)
	// ListTools 返回代码执行的工具卡片列表
	ListTools() []*tool.ToolCard
}

// CodeOptions 代码执行选项
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

// BaseCodeOperation 代码操作基类
type BaseCodeOperation struct {
	BaseOperation
}

// ──────────────────────────── 枚举 ────────────────────────────

// CodeOption 代码执行选项函数
type CodeOption func(*CodeOptions)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

func NewCodeOptions(opts ...CodeOption) *CodeOptions {
	o := &CodeOptions{Language: "python", Timeout: 300}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func WithCodeLanguage(lang string) CodeOption {
	return func(o *CodeOptions) { o.Language = lang }
}

func WithCodeTimeout(timeout int) CodeOption {
	return func(o *CodeOptions) { o.Timeout = timeout }
}

func WithCodeEnvironment(env map[string]string) CodeOption {
	return func(o *CodeOptions) { o.Environment = env }
}

func WithCodeCwd(cwd string) CodeOption {
	return func(o *CodeOptions) { o.Cwd = cwd }
}

func WithCodeOptions(options map[string]any) CodeOption {
	return func(o *CodeOptions) { o.Options = options }
}

func (b *BaseCodeOperation) ExecuteCode(_ context.Context, _ string, _ ...CodeOption) (*result.ExecuteCodeResult, error) {
	return nil, fmt.Errorf("未实现: ExecuteCode")
}

func (b *BaseCodeOperation) ExecuteCodeStream(_ context.Context, _ string, _ ...CodeOption) (<-chan result.ExecuteCodeStreamResult, error) {
	return nil, fmt.Errorf("未实现: ExecuteCodeStream")
}

func (b *BaseCodeOperation) ListTools() []*tool.ToolCard { return nil }
