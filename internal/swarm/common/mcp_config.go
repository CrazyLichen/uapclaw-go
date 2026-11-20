// Python 源：jiuwenswarm/common/mcp_config.py（build 链 9 符号 1:1）
//
// config.yaml MCP 条目 → 运行时 McpServerConfig 的转换助手。团队装配链
// （EnrichTeamSpecForSwarmSeed）与单 agent 路径共用本文件解析 `${VAR}` 占位。
//
// 已登记不移植段（偏差 D-15.16-e6，Python 源行号）：运行时探测半区
// preflight_mcp_server_reachable / fetch_mcp_tools_via_temp_connection /
// fill_mcp_tools_fallback / probe_mcp_live_connection / prewarm_connected_mcps
// （mcp_config.py:258-724）——消费方是 gateway mcp.show / 冷启动注册 /
// prewarm（web/adapter 章节，未迁），且深度耦合 httpx/mcp SDK/Runner.resource_mgr；
// 常量 _HTTP_MCP_TRANSPORTS / _PREWARM_* 仅被探测半区消费，随之不落。
//
// get_mcp_servers 合并段偏差（D-15.16-e7）：Python extract_enabled_mcp_server_entries
// 的权威源是 get_mcp_servers()（config.yaml ∪ mcp/state.json，state.json 为
// marketplace 动态连接记录）；该合并逻辑 Go 侧未迁（缺口已登记 10.3.31g，见
// recommendation/calendar_source.go 注释）。本批走 Python 的 fallback 分支
// 语义：直接读调用方传入的 config 快照的 mcp.servers。

package common

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"strings"

	mcptypes "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// stdioTransport / 其余合法 transport 值。
	// Python: build_mcp_server_config 的白名单集合 (mcp_config.py:155)
	mcpTransportStdio           = "stdio"
	mcpTransportSSE             = "sse"
	mcpTransportHTTP            = "http"
	mcpTransportStreamableDash  = "streamable-http"
	mcpTransportStreamableUnder = "streamable_http"
)

// nodePackageRunners stdio 启动器中 Hub MCP 常见声明的 Node 族（如 amap 用 npx）。
// Python: _NODE_PACKAGE_RUNNERS (mcp_config.py:111)
var nodePackageRunners = map[string]bool{"npx": true, "npm": true}

// placeholderRe `${VAR}` 占位符。Python: _PLACEHOLDER_RE (mcp_config.py:33)
var placeholderRe = regexp.MustCompile(`\$\{(\w+)\}`)

// ──────────────────────────── 全局变量 ────────────────────────────

// lookPathPath 注入点：Python 测试 monkeypatch shutil.which 的 Go 等价物
// （包级函数变量，测试可替换）。生产恒 exec.LookPath。
// Python: shutil.which (mcp_config.py:122/:124)
var lookPathFn = exec.LookPath

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildMcpCredentialResolver 为一个 MCP 构建 `${VAR}` 解析器：
// CredentialStore(name) ∪ os.environ。单 agent 与团队装配路径共用，
// 保证两侧占位解析一致。无存储凭据时返回 nil（占位保持字面量，对应
// build_mcp_server_config 的 credential_resolver=None 缺省）。
// Python: build_mcp_credential_resolver (mcp_config.py:50-74)
func BuildMcpCredentialResolver(name string) func(string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	// Python: _credential_store() 经 provider("mcp_credential") 注入点获取
	// （分层规则 R1：不 import server）；store 未注册/非 MCP 时异常吞掉返回 None。
	// Python: _credential_store (mcp_config.py:36-38) + try/except (mcp_config.py:61-65)
	storeAny, err := Provider("mcp_credential")
	if err != nil {
		return nil
	}
	store, ok := storeAny.(interface {
		// GetAll 返回该 MCP 名下存储的全部凭据（Python store.get_all(name)）。
		GetAll(name string) map[string]string
	})
	if !ok {
		return nil
	}
	stored := store.GetAll(name)
	if len(stored) == 0 {
		// Python: if not stored → return None
		return nil
	}
	return func(key string) string {
		if v, ok := stored[key]; ok {
			return v
		}
		// Python: os.environ.get(key)（缺省 None → _resolve_string 视为 falsy 保留字面量）
		return os.Getenv(key)
	}
}

// ExtractEnabledMCPServerEntries 返回合并源中已启用的 MCP server 条目。
// get_mcp_servers 合并段未迁（D-15.16-e7），恒走 Python fallback 分支：读
// config_base 的 mcp.servers。enabled 缺省 true；非 dict 条目剔除。
// Python: extract_enabled_mcp_server_entries (mcp_config.py:77-107)
func ExtractEnabledMCPServerEntries(configBase map[string]any) []map[string]any {
	var servers []any
	if configBase != nil {
		if mcpCfg, ok := configBase["mcp"].(map[string]any); ok {
			if raw, ok := mcpCfg["servers"].([]any); ok {
				servers = raw
			}
		}
	}
	result := make([]map[string]any, 0, len(servers))
	for _, item := range servers {
		entry, ok := item.(map[string]any)
		if !ok {
			// Python: not isinstance(item, dict) → continue
			continue
		}
		if enabled, ok := entry["enabled"].(bool); ok && !enabled {
			// Python: not bool(item.get("enabled", True)) → continue（缺省 True）
			continue
		}
		result = append(result, entry)
	}
	return result
}

// ResolveNodePackageRunner 启动器不在 PATH 时经 corepack 运行 npx/npm：
// 仅含 Node 的镜像有 corepack 无 npm/npx，`corepack npx ...` 仍能解析捆绑的
// npm 启动器。
// Python: resolve_node_package_runner (mcp_config.py:114-126)
func ResolveNodePackageRunner(command string, args []string) (string, []string) {
	if !nodePackageRunners[command] {
		return command, args
	}
	if _, err := lookPathFn(command); err == nil {
		// Python: shutil.which(command) 命中 → 原样
		return command, args
	}
	if _, err := lookPathFn("corepack"); err != nil {
		// Python: shutil.which("corepack") 未命中 → 原样
		return command, args
	}
	return "corepack", append([]string{command}, args...)
}

// MCPServerConfigOptions BuildMCPServerConfig 的具名形参（对齐 Python keyword-only 形参）。
type MCPServerConfigOptions struct {
	// ServerIDScope 非空时用于派生稳定 server_id；为空保留 openjiuwen 默认随机 id 行为。
	// Python: server_id_scope (mcp_config.py:132)
	ServerIDScope string
	// CredentialResolver `${VAR}` 占位解析器；nil 时占位保持字面量（向后兼容）。
	// Python: credential_resolver (mcp_config.py:133)
	CredentialResolver func(string) string
	// ResolveNodeLauncher 缺失的 npx/npm 经 corepack 路由；仅对真正会被 spawn 的配置开启。
	// Python: resolve_node_launcher (mcp_config.py:134)
	ResolveNodeLauncher bool
}

// BuildMCPServerConfig 从单个 mcp.servers 条目构建 McpServerConfig；非法返回 nil。
// env/headers/url 中的占位符在 resolver 提供时于运行期替换——config.yaml 可存
// 占位符（不落明文 token），spawn 出的 stdio 进程 / HTTP 请求拿到真实凭据。
// Python: build_mcp_server_config (mcp_config.py:129-226)
func BuildMCPServerConfig(entry map[string]any, opts MCPServerConfigOptions) *mcptypes.McpServerConfig {
	name := strings.TrimSpace(strings.TrimSpace(config.String(entry["name"])))
	if name == "" {
		// Python: not name → return None
		return nil
	}
	transport := strings.ToLower(strings.TrimSpace(config.String(entry["transport"])))
	switch transport {
	case mcpTransportStdio, mcpTransportSSE, mcpTransportHTTP, mcpTransportStreamableDash, mcpTransportStreamableUnder:
	default:
		// Python: transport not in 白名单 → return None
		return nil
	}

	// Python payload: server_name + client_type；Go 对应 McpServerConfig 字段。
	cfg := &mcptypes.McpServerConfig{
		ServerName: name,
		ClientType: transport,
	}
	explicitServerID := strings.TrimSpace(config.String(entry["server_id"]))
	if explicitServerID != "" {
		// Python: explicit_server_id → payload["server_id"]
		cfg.ServerID = explicitServerID
	}

	if transport == mcpTransportStdio {
		command := strings.TrimSpace(config.String(entry["command"]))
		if command == "" {
			// Python: not command → return None
			return nil
		}
		// Python: resolve_preserved_cli_path(command) or command
		if resolved := resolvePreservedCLIPath(command, false); resolved != "" {
			command = resolved
		}
		args := []string{}
		if rawArgs, ok := entry["args"].([]any); ok {
			// 占位符经 credential_resolver 解析（同 env）：ssh-mcp-server 等
			// stdio MCP 以 --host ${SSH_HOST} 传参，spawn 进程拿到的是字面
			// argv（无 shell 展开），必须在这里替换。
			// Python: mcp_config.py:174-182
			args = make([]string, 0, len(rawArgs))
			for _, item := range rawArgs {
				args = append(args, resolvePreservedCLIPath(resolveString(config.String(item), opts.CredentialResolver), false))
			}
		}
		// args 缺省 []，裸命令（无 args）也能干净 spawn。
		if opts.ResolveNodeLauncher {
			// Python: resolve_node_package_runner(command, args)
			command, args = ResolveNodePackageRunner(command, args)
		}
		params := map[string]any{"command": command, "args": args}
		if cwd, ok := entry["cwd"].(string); ok && strings.TrimSpace(cwd) != "" {
			// Python: resolve_preserved_cli_path(cwd.strip(), directory=True)
			params["cwd"] = ResolvePreservedCLIDir(strings.TrimSpace(cwd))
		}
		if env, ok := entry["env"].(map[string]any); ok {
			// 占位符经 credential_resolver 解析。Python: mcp_config.py:190-193
			resolved := map[string]any{}
			for k, v := range env {
				resolved[k] = resolveString(config.String(v), opts.CredentialResolver)
			}
			params["env"] = resolved
		}
		if timeout := numericTimeout(entry["timeout_s"]); timeout > 0 {
			// Python: int(timeout_s) > 0 → params["timeout_s"]
			params["timeout_s"] = int(timeout)
		}
		// Python: payload["server_path"] = f"stdio://{name}"
		cfg.ServerPath = "stdio://" + name
		cfg.Params = params
	} else {
		url := strings.TrimSpace(config.String(entry["url"]))
		if url == "" {
			// Python: not url → return None
			return nil
		}
		// url 中的占位符解析（如 gildata ?token=${GILDATA_TOKEN}）。
		// Python: payload["server_path"] = _resolve_string(url, credential_resolver)
		cfg.ServerPath = resolveString(url, opts.CredentialResolver)
		params := map[string]any{}
		if headers, ok := entry["headers"].(map[string]any); ok {
			// openjiuwen 的 SseClient / StreamableHttpClient 读 auth_headers
			// （不是 params.headers）——解析后的头必须进 auth_headers，否则
			// 远端 MCP 拿不到 Authorization、静默返回 0 个工具。
			// auth_query_params 保持为空；url 里的 query token 已在上文解析。
			// Python: mcp_config.py:206-216
			resolved := map[string]string{}
			for k, v := range headers {
				resolved[k] = resolveString(config.String(v), opts.CredentialResolver)
			}
			cfg.AuthHeaders = resolved
		}
		if timeout := numericTimeout(entry["timeout_s"]); timeout > 0 {
			params["timeout_s"] = int(timeout)
		}
		if len(params) > 0 {
			cfg.Params = params
		}
	}

	if opts.ServerIDScope != "" && cfg.ServerID == "" {
		// Python: server_id_scope and "server_id" not in payload → 稳定 id
		cfg.ServerID = stableMCPServerID(opts.ServerIDScope, name, cfg)
	}
	return cfg
}

// BuildEnabledMCPServerConfigs 构建全部已启用的 MCP server 配置，跳过非法条目。
// resolve_credentials=True 时经 BuildMcpCredentialResolver 解析占位符——团队
// 装配用，使 HTTP MCP 存的占位 token 拿到真实凭据，与单 agent 路径一致。
// Python: build_enabled_mcp_server_configs (mcp_config.py:229-255)
func BuildEnabledMCPServerConfigs(configBase map[string]any, serverIDScope string, resolveCredentials bool) []*mcptypes.McpServerConfig {
	configs := make([]*mcptypes.McpServerConfig, 0)
	for _, entry := range ExtractEnabledMCPServerEntries(configBase) {
		var resolver func(string) string
		if resolveCredentials {
			// Python: build_mcp_credential_resolver(str(entry.get("name", "") or "").strip())
			resolver = BuildMcpCredentialResolver(strings.TrimSpace(config.String(entry["name"])))
		}
		cfg := BuildMCPServerConfig(entry, MCPServerConfigOptions{
			ServerIDScope:       serverIDScope,
			CredentialResolver:  resolver,
			ResolveNodeLauncher: true,
		})
		if cfg != nil {
			configs = append(configs, cfg)
		}
	}
	return configs
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// numericTimeout 读 timeout_s：Python isinstance((int, float)) 且 >0 才有效，
// 其余得 0（构建函数视作未设置）。
func numericTimeout(v any) float64 {
	switch t := v.(type) {
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case float64:
		return t
	}
	return 0
}

// resolveString 经 resolver 替换字符串中的 `${VAR}`；缺失保持字面量。
// Python: _resolve_string (mcp_config.py:41-47)
func resolveString(value string, resolver func(string) string) string {
	if resolver == nil {
		return value
	}
	return placeholderRe.ReplaceAllStringFunc(value, func(match string) string {
		key := placeholderRe.FindStringSubmatch(match)[1]
		// Python: resolver(m.group(1)) or m.group(0)——None/空串均回退原匹配。
		if resolved := resolver(key); resolved != "" {
			return resolved
		}
		return match
	})
}

// stableMCPServerID scope:name 稳定 server_id：sha256(规范化 payload) 前 16 位。
// 排除 server_id 自身避免自引用；scope/name 非法字符经 safeIDPart 兜底。
// Python: _stable_mcp_server_id (mcp_config.py:727-742)
func stableMCPServerID(scope string, name string, cfg *mcptypes.McpServerConfig) string {
	// Python stable_payload 排除 "server_id" 键后按 sort_keys 序列化——Go 的
	// json.Marshal 对 map[string]any 本身按键排序，等价。
	stable := map[string]any{
		"server_name": cfg.ServerName,
		"client_type": cfg.ClientType,
		"server_path": cfg.ServerPath,
		"params":      cfg.Params,
	}
	if cfg.AuthHeaders != nil {
		stable["auth_headers"] = cfg.AuthHeaders
	}
	raw, err := json.Marshal(map[string]any{"scope": scope, "payload": stable})
	if err != nil {
		// Python json.dumps(..., default=str) 恒成功；Go 对不可序列化值无
		// 对应物（YAML 解析出的都是基础类型），失败按空 payload 兜底。
		raw, _ = json.Marshal(map[string]any{"scope": scope})
	}
	digest := sha256.Sum256(raw)
	return "mcp_" + safeIDPart(scope, "scope") + "_" + safeIDPart(name, "server") + "_" + hex.EncodeToString(digest[:])[:16]
}

// safeIDPart 非 [A-Za-z0-9_] 序列折叠为 "_"、去首尾 "_"、小写、截 48 位。
// Python: _safe_id_part (mcp_config.py:745-747)
func safeIDPart(value string, defaultPart string) string {
	normalized := strings.Trim(regexp.MustCompile(`[^A-Za-z0-9_]+`).ReplaceAllString(value, "_"), "_")
	normalized = strings.ToLower(normalized)
	if len(normalized) > 48 {
		normalized = normalized[:48]
	}
	if normalized == "" {
		return defaultPart
	}
	return normalized
}
