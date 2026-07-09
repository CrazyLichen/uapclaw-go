package sys_operation

import (
	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SysSubOperation 系统子操作公共接口，FsOperation/ShellOperation/CodeOperation 均满足此接口。
type SysSubOperation interface {
	// ListTools 返回操作的工具卡片列表
	ListTools() []*tool.ToolCard
}

// SysOperation 系统操作主接口，编排文件系统、Shell、代码执行等子操作。
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

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 BaseSysOperation 满足 SysOperation 接口
var _ SysOperation = (*BaseSysOperation)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

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
