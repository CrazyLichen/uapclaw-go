package acp

import (
	"fmt"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件（基础设施层，本包 client/server 侧按 swarm 分层归 AgentServer）。
const logComponent = logger.ComponentAgentServer

// ──────────────────────────── 全局变量 ────────────────────────────

// profileScopedKeys 不从父进程继承的敏感键（仅 acp_agents.<profile>.env 提供）。
// Python: _PROFILE_SCOPED_KEYS (subprocess_env.py L20-30)
var profileScopedKeys = map[string]struct{}{
	"OPENAI_API_KEY":      {},
	"OPENAI_ORGANIZATION": {},
	"OPENAI_PROJECT":      {},
	"HTTPS_PROXY":         {},
	"HTTP_PROXY":          {},
	"ALL_PROXY":           {},
	"NO_PROXY":            {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildAcpSubprocessEnv 构造子进程环境变量。
// 分层：1) os.Environ 剥离 profileScopedKeys；2) profile_env（${VAR} 解析后）覆盖。
// Python: build_acp_subprocess_env (subprocess_env.py L33-56)
func BuildAcpSubprocessEnv(profileEnv map[string]any) map[string]string {
	env := make(map[string]string)
	for _, kv := range os.Environ() {
		idx := strings.IndexByte(kv, '=')
		if idx <= 0 {
			continue
		}
		key, val := kv[:idx], kv[idx+1:]
		if _, scoped := profileScopedKeys[key]; scoped {
			continue
		}
		env[key] = val
	}
	if len(profileEnv) > 0 {
		resolvedAny := config.ResolveEnvVars(profileEnv, nil)
		if resolved, ok := resolvedAny.(map[string]any); ok {
			for key, val := range resolved {
				if val == nil {
					continue
				}
				sk := key
				sv := strings.TrimSpace(strOf(val))
				if sv != "" {
					env[sk] = sv
				}
			}
		}
	}
	return env
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// strOf 任意值转字符串（Python str() 等价）。
func strOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
