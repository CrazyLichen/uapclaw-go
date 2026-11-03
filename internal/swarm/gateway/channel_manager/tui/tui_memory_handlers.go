package tui

import (
	"context"
	"fmt"

	"github.com/gorilla/websocket"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	memoryrpc "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memoryrpc"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// memoryHandlerFunc 统一记忆 RPC handler 签名（mode 对 edit/open 无意义但保持同形）
type memoryHandlerFunc func(ctx context.Context, workspace, mode string, params map[string]any) map[string]any

// ──────────────────────────── 常量 ────────────────────────────

const (
	// memoryRPCErrorCode 记忆 RPC 内部错误码，对齐 Python code="INTERNAL_ERROR"
	memoryRPCErrorCode = "INTERNAL_ERROR"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// memoryHandlers 方法 → handler 映射。
//
// 生产填充 memoryrpc 实现；测试可替换单项以覆盖 error/panic 分支。
var memoryHandlers = map[string]memoryHandlerFunc{
	"memory.list": func(ctx context.Context, ws, mode string, params map[string]any) map[string]any {
		return memoryrpc.HandleMemoryList(ctx, ws, mode, params)
	},
	"memory.edit": func(ctx context.Context, ws, _ string, params map[string]any) map[string]any {
		return memoryrpc.HandleMemoryEdit(ctx, ws, params)
	},
	"memory.status": func(ctx context.Context, ws, mode string, params map[string]any) map[string]any {
		return memoryrpc.HandleMemoryStatus(ctx, ws, mode, params)
	},
	"memory.toggle": func(ctx context.Context, ws, mode string, params map[string]any) map[string]any {
		return memoryrpc.HandleMemoryToggle(ctx, ws, mode, params)
	},
	"memory.open": func(ctx context.Context, ws, _ string, params map[string]any) map[string]any {
		return memoryrpc.HandleMemoryOpen(ctx, ws, params)
	},
}

// memoryProjectDirMethods 需要解析并注入 project_dir 的方法。
//
// Python: _memory_toggle 不解析 project_dir，其余四个解析。
var memoryProjectDirMethods = map[string]bool{
	"memory.list":   true,
	"memory.edit":   true,
	"memory.status": true,
	"memory.open":   true,
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// memoryLocalHandler 构造记忆 RPC 的本地 handler。
//
// Python: tui_connect.py 的 _memory_list/_memory_edit/_memory_status/_memory_toggle/_memory_open
//
// 与 Python 一致：handler 自己发 res 帧并恒返回 nil——gateway 对返回 error 的本地
// handler 会二次包装成 "handler error: ..."（gateway_server.go），与 Python 文案不符。
func memoryLocalHandler(send ResponseSender, method string) routing.LocalHandlerFunc {
	return func(ws *websocket.Conn, reqID string, params map[string]any, _ string) error {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error(logComponent).
					Str("method", method).
					Any("recover", recovered).
					Msg("memory rpc panic recovered")
				send.SendResFrame(ws, reqID, false, nil, fmt.Sprintf("%v", recovered), memoryRPCErrorCode)
			}
		}()

		methodParams := params
		if memoryProjectDirMethods[method] {
			methodParams = withProjectDir(params, resolveProjectDir(params))
		}

		result := memoryHandlers[method](context.Background(), workspace.AgentWorkspaceDir(), memoryMode(params), methodParams)
		if errMsg, failed := result["error"].(string); failed && errMsg != "" {
			logger.Warn(logComponent).
				Str("method", method).
				Str("error", errMsg).
				Msg("memory rpc failed")
			send.SendResFrame(ws, reqID, false, nil, errMsg, memoryRPCErrorCode)
			return nil
		}

		send.SendResFrame(ws, reqID, true, result, "", "")
		return nil
	}
}

// memoryMode 取请求 mode，缺省 "plan"。
//
// Python: mode = params.get("mode", "plan")
func memoryMode(params map[string]any) string {
	if mode, ok := params["mode"].(string); ok && mode != "" {
		return mode
	}
	return "plan"
}

// resolveProjectDir 解析项目目录。
//
// Python: _resolve_project_dir —— params.project_dir → params.trusted_dirs[0] → params.cwd
func resolveProjectDir(params map[string]any) string {
	if projectDir, ok := params["project_dir"].(string); ok && projectDir != "" {
		return projectDir
	}
	if dirs, ok := params["trusted_dirs"].([]any); ok && len(dirs) > 0 {
		if projectDir, ok := dirs[0].(string); ok && projectDir != "" {
			return projectDir
		}
	}
	if cwd, ok := params["cwd"].(string); ok && cwd != "" {
		return cwd
	}
	return ""
}

// withProjectDir 返回注入 project_dir 后的 params 副本；未命中时原样返回。
//
// Python: if project_dir: params = {**params, "project_dir": project_dir}
func withProjectDir(params map[string]any, projectDir string) map[string]any {
	if projectDir == "" {
		return params
	}
	merged := make(map[string]any, len(params)+1)
	for key, value := range params {
		merged[key] = value
	}
	merged["project_dir"] = projectDir
	return merged
}
