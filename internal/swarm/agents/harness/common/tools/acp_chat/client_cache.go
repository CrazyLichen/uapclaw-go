// profile 缓存与互斥（10.6.24d）：按 profile 复用 AcpStdioClient。
// 对齐 Python jiuwenswarm/agents/harness/common/tools/acp_chat/tool.py 的
// 模块级 _clients/_locks 字典与 _load_profile/_get_or_create_client/
// _close_profile_unlocked/_profile_lock 函数。

package acpchat

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/acp"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ClientSpec 子进程启动规格（Python: _get_or_create_client 内局部解析结果）。
//
// 导出给 cmd 层 acp-chat 冒烟入口（10.4.6）复用严格解析逻辑。
type ClientSpec struct {
	// Command 启动命令（trim 后非空）
	Command string
	// Args 命令参数（元素经 str() 转换）
	Args []string
	// Env 子进程环境（nil = 继承）
	Env map[string]any
	// Cwd 子进程工作目录（空 = 继承）
	Cwd string
}

// profileLock profile 级互斥（对齐 Python asyncio.Lock 的获取/释放语义）。
// Python: _locks: dict[str, asyncio.Lock]
type profileLock struct {
	// mu 同 profile 并发 spawn/chat 互斥
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件（工具层，对齐 tools 包先例；Python logging 模块名归属）。
const logComponent = logger.ComponentAgentServer

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// clients profile → 客户端缓存。
	// Python: _clients: dict[str, AcpStdioClient]
	clients = map[string]*acp.AcpStdioClient{}

	// clientsMu 保护 clients 与 profileLocks 两张表。
	clientsMu sync.Mutex

	// profileLocks profile → 互斥锁（懒创建）。
	// Python: _locks: dict[str, asyncio.Lock]
	profileLocks = map[string]*profileLock{}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadProfile 从 configBase 读取 acp_agents 映射并提取 profile 规格。
// Python: _load_profile (tool.py L28-38)——get_config() 经注入 cfg 等价。
// 导出给 10.4.6 acp-chat 冒烟入口复用。
func LoadProfile(configBase map[string]any, profile string) (map[string]any, error) {
	agents, _ := configBase["acp_agents"].(map[string]any)
	if agents == nil {
		return nil, fmt.Errorf("config has no acp_agents mapping")
	}
	spec, ok := agents[strings.TrimSpace(profile)].(map[string]any)
	if !ok {
		return nil, fmt.Errorf(
			"unknown acp_agents profile %q; define it under acp_agents in config.yaml", profile)
	}
	return spec, nil
}

// BuildClientSpec 解析并校验子进程启动规格。
// Python: _get_or_create_client L62-87 的 command/args/cwd/env 解析段。
// 导出给 10.4.6 acp-chat 冒烟入口复用（与工具路径共用同一套严格校验）。
func BuildClientSpec(spec map[string]any) (*ClientSpec, error) {
	command, _ := spec["command"].(string)
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, fmt.Errorf("acp_agents profile needs non-empty command")
	}

	// args：nil → []；list → 逐元素 str()；其他 → 报错
	var args []string
	if rawArgs, ok := spec["args"]; ok && rawArgs != nil {
		list, isList := rawArgs.([]any)
		if !isList {
			return nil, fmt.Errorf("acp_agents.args must be a list of strings")
		}
		args = make([]string, 0, len(list))
		for _, item := range list {
			args = append(args, strOfAcpChat(item))
		}
	}

	// cwd：非字符串或空白 → 空（None）；字符串 → trim
	cwd := ""
	if rawCwd, ok := spec["cwd"].(string); ok {
		cwd = strings.TrimSpace(rawCwd)
	}

	// env：非映射 → 报错（构造在 NewAcpStdioClient 内完成）
	if rawEnv, ok := spec["env"]; ok && rawEnv != nil {
		if _, isMap := rawEnv.(map[string]any); !isMap {
			return nil, fmt.Errorf("acp_agents.env must be a map of strings")
		}
		envMap, _ := rawEnv.(map[string]any)
		return &ClientSpec{Command: command, Args: args, Env: envMap, Cwd: cwd}, nil
	}
	return &ClientSpec{Command: command, Args: args, Env: nil, Cwd: cwd}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// acquireProfileLock 获取 profile 互斥锁（懒创建，对齐 _profile_lock）。
// Python: _profile_lock (tool.py L22-25)
func acquireProfileLock(profile string) *profileLock {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	if l, ok := profileLocks[profile]; ok {
		return l
	}
	l := &profileLock{}
	profileLocks[profile] = l
	return l
}

// releaseProfileLock 释放 profile 互斥锁（配对 acquireProfileLock 使用）。
func releaseProfileLock(profile string, l *profileLock) {
	_ = profile
	if l != nil {
		l.mu.Unlock()
	}
}

// getOrCreateClient 获取或创建 profile 客户端。
// newSession=true 时先关闭旧实例再重建（重启子进程与会话）。
// Python: _get_or_create_client (tool.py L50-91)
func getOrCreateClient(ctx context.Context, cfg map[string]any, profile string, newSession bool) (*acp.AcpStdioClient, error) {
	spec, err := LoadProfile(cfg, profile)
	if err != nil {
		return nil, err
	}

	if newSession {
		closeProfileUnlocked(profile)
	}

	if existing := cachedClient(profile); existing != nil {
		if existing.IsConnected() {
			return existing, nil
		}
		closeProfileUnlocked(profile)
	}

	cs, err := BuildClientSpec(spec)
	if err != nil {
		return nil, err
	}

	client := acp.NewAcpStdioClient(cs.Command, cs.Args, cs.Cwd, cs.Env)
	if err := client.Connect(ctx); err != nil {
		return nil, err
	}
	storeClient(profile, client)
	return client, nil
}

// resetClientsForTest 测试钩子：清空全局缓存与锁表（不关闭实例）。
func resetClientsForTest() {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	clients = map[string]*acp.AcpStdioClient{}
	profileLocks = map[string]*profileLock{}
}

// cachedClient 读缓存实例。
func cachedClient(profile string) *acp.AcpStdioClient {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	return clients[profile]
}

// storeClient 写缓存实例。
func storeClient(profile string, c *acp.AcpStdioClient) {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	clients[profile] = c
}

// closeProfileUnlocked 关闭并移除 profile 缓存实例（忽略关闭异常）。
// Python: _close_profile_unlocked (tool.py L41-47)
func closeProfileUnlocked(profile string) {
	clientsMu.Lock()
	client := clients[profile]
	delete(clients, profile)
	clientsMu.Unlock()
	if client != nil {
		if err := client.Close(); err != nil {
			logger.Warn(logComponent).Str("profile", profile).Err(err).
				Msg("[acp_chat] close profile failed")
		}
	}
}

// strOfAcpChat 任意值转字符串（Python str() 等价）。
func strOfAcpChat(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// NormalizeProfileSpecForCLI 按 Python acp/cli.py 的宽松语义归一化 profile 规格。
//
// Python 两个入口对畸形 profile 的处理不同：
//   - cli.py（命令行冒烟）：args 非 list → []；env 非 dict → None（静默放宽）
//   - tool.py（acp_chat 工具）：args/env 类型不符 → 报错
//
// Go 两处共用 BuildClientSpec 的严格校验，故由 CLI 入口在调用前先做宽松归一，
// 保证「命令行能跑的配置不会被更严格的 Go 校验拒掉」，工具侧严格语义不受影响。
func NormalizeProfileSpecForCLI(spec map[string]any) map[string]any {
	if spec == nil {
		return nil
	}
	out := make(map[string]any, len(spec))
	for k, v := range spec {
		out[k] = v
	}
	if rawArgs, ok := out["args"]; ok && rawArgs != nil {
		if _, isList := rawArgs.([]any); !isList {
			delete(out, "args")
		}
	}
	if rawEnv, ok := out["env"]; ok && rawEnv != nil {
		if _, isMap := rawEnv.(map[string]any); !isMap {
			delete(out, "env")
		}
	}
	return out
}
