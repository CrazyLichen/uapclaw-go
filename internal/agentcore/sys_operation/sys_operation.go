package sys_operation

import (
	"sync"

	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SysSubOperation 系统子操作公共接口，FsOperation/ShellOperation/CodeOperation 均满足此接口。
type SysSubOperation interface {
	// ListTools 返回操作的工具卡片列表
	ListTools() []*tool.ToolCard
}

// SysOperation 系统操作主接口，编排文件系统、Shell、代码执行等子操作。
// 对齐 Python SysOperation：card, fs(), shell(), code(), isolation_key_template。
type SysOperation interface {
	// Card 返回系统操作配置卡片
	Card() *SysOperationCard
	// Fs 返回文件系统操作实例
	Fs() FsOperation
	// Shell 返回 Shell 操作实例
	Shell() ShellOperation
	// Code 返回代码执行实例
	Code() CodeOperation
	// IsolationKeyTemplate 返回隔离键模板
	IsolationKeyTemplate() string
}

// BaseSysOperation SysOperation 的空操作桩实现，所有方法返回 nil 或未实现错误。
type BaseSysOperation struct{}

// LocalSysOperation 本地系统操作实现。
// 对齐 Python SysOperation 的 __getattr__ 动态调度逻辑，
// 使用 OperationRegistry + instances map 实现 lazy 实例化。
type LocalSysOperation struct {
	// card 配置卡片
	card *SysOperationCard
	// instances 懒实例化的子操作缓存 name → SysSubOperation
	instances map[string]SysSubOperation
	// mu 保护 instances
	mu sync.RWMutex
}

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 BaseSysOperation 满足 SysOperation 接口
var _ SysOperation = (*BaseSysOperation)(nil)

// 编译时验证 LocalSysOperation 满足 SysOperation 接口
var _ SysOperation = (*LocalSysOperation)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSysOperation 系统操作工厂函数，根据 card.Mode 决定构造类型。
// 对齐 Python SysOperation(card) 构造函数的 mode 分支：
//   - OperationModeLocal → NewLocalSysOperation
//   - OperationModeSandbox → NewLocalSysOperation（sandbox 预留，当前 fallback）
func NewSysOperation(card *SysOperationCard) SysOperation {
	if card == nil {
		card = NewSysOperationCard()
	}
	if card.Mode == OperationModeSandbox {
		// sandbox 模式预留，当前 fallback 到 local
		return NewLocalSysOperation(card)
	}
	return NewLocalSysOperation(card)
}

// NewLocalSysOperation 创建本地系统操作实例。
// 对齐 Python SysOperation.__init__：根据 mode 初始化 runConfig。
func NewLocalSysOperation(card *SysOperationCard) *LocalSysOperation {
	if card == nil {
		card = NewSysOperationCard()
	}
	return &LocalSysOperation{
		card:      card,
		instances: make(map[string]SysSubOperation),
	}
}

// Card 返回系统操作配置卡片
func (s *LocalSysOperation) Card() *SysOperationCard { return s.card }

// Fs 返回文件系统操作实例（lazy 实例化）。
// 对齐 Python SysOperation.fs()：通过 _get_operation("fs") 获取实例。
func (s *LocalSysOperation) Fs() FsOperation {
	op := s.getOperation("fs")
	if op == nil {
		return &BaseFsOperation{}
	}
	if fsOp, ok := op.(FsOperation); ok {
		return fsOp
	}
	return &BaseFsOperation{}
}

// Shell 返回 Shell 操作实例（lazy 实例化）。
// 对齐 Python SysOperation.shell()：通过 _get_operation("shell") 获取实例。
func (s *LocalSysOperation) Shell() ShellOperation {
	op := s.getOperation("shell")
	if op == nil {
		return &BaseShellOperation{}
	}
	if shellOp, ok := op.(ShellOperation); ok {
		return shellOp
	}
	return &BaseShellOperation{}
}

// Code 返回代码执行实例（lazy 实例化）。
// 对齐 Python SysOperation.code()：通过 _get_operation("code") 获取实例。
func (s *LocalSysOperation) Code() CodeOperation {
	op := s.getOperation("code")
	if op == nil {
		return &BaseCodeOperation{}
	}
	if codeOp, ok := op.(CodeOperation); ok {
		return codeOp
	}
	return &BaseCodeOperation{}
}

// IsolationKeyTemplate 返回隔离键模板。
// 对齐 Python SysOperation.isolation_key_template：sandbox 模式返回模板，local 模式返回空。
func (s *LocalSysOperation) IsolationKeyTemplate() string {
	return s.card.IsolationKeyTemplate()
}

// Card 返回系统操作配置卡片（BaseSysOperation 空实现）
func (b *BaseSysOperation) Card() *SysOperationCard { return nil }

// Fs 返回文件系统操作实例（BaseSysOperation 空实现）
func (b *BaseSysOperation) Fs() FsOperation { return nil }

// Shell 返回 Shell 操作实例（BaseSysOperation 空实现）
func (b *BaseSysOperation) Shell() ShellOperation { return nil }

// Code 返回代码执行实例（BaseSysOperation 空实现）
func (b *BaseSysOperation) Code() CodeOperation { return nil }

// IsolationKeyTemplate 返回隔离键模板（BaseSysOperation 空实现）
func (b *BaseSysOperation) IsolationKeyTemplate() string { return "" }

// ──────────────────────────── 非导出函数 ────────────────────────────

// getOperation 通用 lazy 实例化，从 OperationRegistry 查 OperationDef，调用 NewFunc 创建实例。
// 对齐 Python SysOperation._get_operation：
//  1. 查缓存 instances[name]
//  2. 从 GlobalRegistry 获取 OperationDef
//  3. 根据 card.Mode 构造 runConfig
//  4. 调用 NewFunc 工厂创建实例
//  5. 双重检查写入 instances 缓存
func (s *LocalSysOperation) getOperation(name string) SysSubOperation {
	// 快路径：读锁查缓存
	s.mu.RLock()
	if inst, ok := s.instances[name]; ok {
		s.mu.RUnlock()
		return inst
	}
	s.mu.RUnlock()

	// 慢路径：写锁创建实例
	s.mu.Lock()
	defer s.mu.Unlock()

	// 双重检查
	if inst, ok := s.instances[name]; ok {
		return inst
	}

	def, ok := GlobalRegistry.GetOperationInfo(name, s.card.Mode)
	if !ok {
		return nil
	}

	// 构造 runConfig，对齐 Python SysOperation.__init__ 的 mode 分支
	var runConfig any
	if s.card.Mode == OperationModeLocal {
		if s.card.WorkConfig != nil {
			runConfig = s.card.WorkConfig
		} else {
			runConfig = NewLocalWorkConfig()
		}
	} else {
		if s.card.GatewayConfig != nil {
			runConfig = s.card.GatewayConfig
		} else {
			runConfig = NewSandboxGatewayConfig()
		}
	}

	inst := def.NewFunc(runConfig)
	s.instances[name] = inst
	return inst
}
