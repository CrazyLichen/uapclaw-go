package main

// acp_chat_cmd.go 提供 uapclaw acp-chat 子命令（10.4.6）。
// Python: jiuwenswarm/acp/cli.py（90 行，一次性冒烟：connect → chat 一次 → close）。
// 定位：外部 ACP 子代理出口链的旁路冒烟入口——不进 Gateway/AgentServer/LLM/工具调度，
// 不产生 E2A 消息与会话历史（设计见 docs/superpowers/specs/2026-10-31-acp-cli-smoke-10.4.6-design.md）。

import (
	"errors"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// acpChatExitError 带进程退出码的语义错误（Execute 消费，映射为 os.Exit 码）。
type acpChatExitError struct {
	// code 进程退出码
	code int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// exitCodeOK 成功
	exitCodeOK = 0
	// exitCodeSession 会话异常（connect/chat 失败）
	exitCodeSession = 1
	// exitCodeConfig 配置错误（profile 缺失/规格非法/参数个数错）
	exitCodeConfig = 2
	// exitCodeInterrupted 被信号中断（对齐 Python KeyboardInterrupt → 130）
	exitCodeInterrupted = 130
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *acpChatExitError) Error() string {
	return fmt.Sprintf("acp-chat exit code %d", e.code)
}

// exitCodeFor 从 error 解析进程退出码（nil → 0，未知 → 1）。
func exitCodeFor(err error) int {
	if err == nil {
		return exitCodeOK
	}
	var exitErr *acpChatExitError
	if errors.As(err, &exitErr) && exitErr != nil {
		return exitErr.code
	}
	return exitCodeSession
}
