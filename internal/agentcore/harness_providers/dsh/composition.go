package dsh

import (
	cryptoRand "crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
)

// ──────────────────────────── 常量 ────────────────────────────

// mcpNamePattern DSH MCP 名白名单（Python: composition.py:21 re.fullmatch）
var mcpNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// ──────────────────────────── 结构体 ────────────────────────────

// overlayResult writeOverlay 的三元组（目录、patch 路径、env 变量）。
// Python: composition.py:48 返回 tuple[TemporaryDirectory, str, dict]
type overlayResult struct {
	// dir MkdirTemp 根（偏差 D-dsh-3：调用方经 cleanup 删除）
	dir string
	// path host.patch.yml 绝对路径
	path string
	// envName OPENJIUWEN_DSH_HOST_<HEX>
	envName string
	// envValue JSON 序列化的 values
	envValue string
}

// cleanup overlay 目录清理（Python TemporaryDirectory.cleanup 等价）。
func (o *overlayResult) cleanup() { _ = os.RemoveAll(o.dir) }

// ──────────────────────────── 导出函数 ────────────────────────────

// MCPConfigs 把协议 MCP 定义翻译成 DSH 原生 MCP-client 配置。
// Python: composition.py:16-39 mcp_configs
func MCPConfigs(context *harnessprotocol.HarnessContext) ([]map[string]any, error) {
	var configs []map[string]any
	names := map[string]bool{}
	for _, server := range context.McpServers {
		if !mcpNamePattern.MatchString(server.Name) || names[server.Name] {
			return nil, fmt.Errorf("DSH MCP names must be unique and match [A-Za-z0-9_-]{1,32}")
		}
		names[server.Name] = true
		config := map[string]any{
			"serverName":         server.Name,
			"failOnStartupError": true,
		}
		switch server.Transport {
		case harnessprotocol.McpTransportStdio:
			// Python: composition.py:26-33 command[0]/args[1:]/env/cwd
			if len(server.Command) == 0 {
				return nil, fmt.Errorf("DSH stdio MCP server %q requires a command", server.Name)
			}
			config["transport"] = "stdio"
			config["command"] = server.Command[0]
			args := make([]any, 0, len(server.Command)-1)
			for _, arg := range server.Command[1:] {
				args = append(args, arg)
			}
			config["args"] = args
			env := map[string]any{}
			for key, value := range server.Env {
				env[key] = value
			}
			config["env"] = env
			if context.Cwd != nil && *context.Cwd != "" {
				config["cwd"] = *context.Cwd
			}
		case harnessprotocol.McpTransportHTTP:
			// Python: composition.py:34-35 streamable-http/url/headers
			config["transport"] = "streamable-http"
			if server.URL != nil {
				config["url"] = *server.URL
			}
			headers := map[string]any{}
			for key, value := range server.Headers {
				headers[key] = value
			}
			config["headers"] = headers
		default:
			// Python: composition.py:36-37 in-process 拒绝
			return nil, &harnessprotocol.UnsupportedHarnessCapabilityError{
				Msg: "DSH cannot mount in-process MCP servers",
			}
		}
		configs = append(configs, config)
	}
	return configs, nil
}

// WriteOverlay 用户数据留在子进程环境并注册原生插件。
// append 模式贡献独立 section 与单个插值变量：用户文本不会被递归解析为 DSH 模板语法。
// Python: composition.py:42-113 write_overlay
func WriteOverlay(
	context *harnessprotocol.HarnessContext,
	includePrompt bool,
	promptMode string,
	enableSkillPlugins bool,
) (*overlayResult, error) {
	configs, err := MCPConfigs(context)
	if err != nil {
		return nil, err
	}
	// Python: composition.py:55-56 无内容 → nil
	if len(configs) == 0 && !(includePrompt && context.SystemPrompt != "") && !enableSkillPlugins {
		return nil, nil
	}
	envName := fmt.Sprintf("OPENJIUWEN_DSH_HOST_%s", strings.ToUpper(randomHex()))
	dir, err := os.MkdirTemp("", "openjiuwen-dsh-")
	if err != nil {
		return nil, err
	}
	result := &overlayResult{dir: dir, envName: envName}
	// Python: composition.py:58-59 root = Path(directory.name)
	root := dir
	values := map[string]any{"mcps": configs}
	var rows []string
	var inserts []string
	fail := func(err error) (*overlayResult, error) {
		_ = os.RemoveAll(dir)
		return nil, err
	}

	// Python: composition.py:64-90 host-prompt 插件
	if includePrompt && context.SystemPrompt != "" {
		plugin := filepath.Join(root, "host-prompt.mjs")
		var body string
		if promptMode == "replace" {
			// Python: composition.py:67-75 persona-prefix section 原位替换
			values["prompt"] = map[string]any{"personaPrefix": context.SystemPrompt}
			body = "  const value = JSON.parse(process.env." + envName + ").prompt.personaPrefix;\n" +
				"  ctx.on('system-prompt/assemble', async (_assembly, _context, next) => {\n" +
				"    const assembly = await next();\n" +
				"    return {...assembly, sections: assembly.sections.map(section =>\n" +
				"      section.name === 'deployment:persona-prefix' ? {...section, text: value} : section)};\n" +
				"  });\n"
		} else {
			// Python: composition.py:76-83 独立 section + 单插值变量
			values["append"] = context.SystemPrompt
			body = "  const value = JSON.parse(process.env." + envName + ").append;\n" +
				"  ctx.systemPrompt.variable('openjiuwen_host_instructions', () => value);\n" +
				"  ctx.systemPrompt.section({name: 'openjiuwen:host-instructions',\n" +
				"    order: Number.MAX_SAFE_INTEGER, text: '{{openjiuwen_host_instructions}}'});\n"
		}
		pluginBody := "export const name = 'openjiuwen-host-prompt';\n" +
			"export const inject = ['systemPrompt'];\n" +
			"export function apply(ctx) {\n" + body + "}\n"
		if err := os.WriteFile(plugin, []byte(pluginBody), 0o600); err != nil {
			return fail(err)
		}
		inserts = append(inserts, fmt.Sprintf("    - id: openjiuwen-host-prompt\n      name: %s\n",
			jsonString(plugin)))
	}
	// Python: composition.py:91-98 skill 三插件
	if enableSkillPlugins {
		skillPlugins := [][2]string{
			{"skill", "skill"},
			{"skill-filesystem", "skill-filesystem"},
			{"tool-skill", "tool-skill"},
		}
		for _, item := range skillPlugins {
			inserts = append(inserts, fmt.Sprintf("    - id: %s\n      name: \"@deepseek-ai/dsh-%s\"\n",
				item[0], item[1]))
		}
	}
	// Python: composition.py:99-104 dsh-mcp-client 注入
	for index := range configs {
		inserts = append(inserts, fmt.Sprintf("    - id: openjiuwen-mcp-%d\n"+
			"      name: \"@deepseek-ai/dsh-mcp-client\"\n"+
			"      config: !!js \"JSON.parse(process.env.%s).mcps[%d]\"\n", index, envName, index))
	}
	// Python: composition.py:105-108 patch 文件组装
	if len(inserts) > 0 {
		rows = append(rows, "- insert:\n")
		rows = append(rows, inserts...)
	}
	path := filepath.Join(root, "host.patch.yml")
	if err := os.WriteFile(path, []byte(strings.Join(rows, "")), 0o600); err != nil {
		return fail(err)
	}
	result.path = path
	// Python: composition.py:113 env 变量 JSON 序列化
	valueJSON, err := json.Marshal(values)
	if err != nil {
		return fail(err)
	}
	result.envValue = string(valueJSON)
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// randomHex 生成 32 位大写十六进制串（uuid4().hex.upper() 等价）。
func randomHex() string {
	var buf [16]byte
	// crypto/rand 全 16 字节读失败概率可忽略（错误已被忽略，与 uuid4 语义一致）
	_, _ = cryptoRand.Read(buf[:])
	return fmt.Sprintf("%x", buf)
}

// jsonString JSON 字符串序列化（json.dumps 等价）。
func jsonString(value string) string {
	b, _ := json.Marshal(value)
	return string(b)
}
