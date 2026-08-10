package messager

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateMessager 根据 config 构建 Messager 实例。
// 对齐 Python: create_messager(config) (openjiuwen/agent_teams/messager/base.py)
func CreateMessager(config schema.MessagerTransportConfig) (Messager, error) {
	switch config.Backend {
	case "inprocess":
		return NewInProcessMessager(config), nil
	// ⤵️ 9.65-2: pyzmq 后端
	default:
		return nil, fmt.Errorf("unsupported messager backend: %s", config.Backend)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
