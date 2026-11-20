package dsh

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/google/uuid"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
)

// ──────────────────────────── 常量 ────────────────────────────

// mcpNamePattern DSH MCP 名字合法集（composition.py:21 fullmatch 语义 =
// 锚定全串）。
var mcpNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// ──────────────────────────── 结构体 ────────────────────────────

// OverlayResult write_overlay 的三元组（目录、patch 路径、env 变量）。
// Python: composition.py:48 返回 tuple[TemporaryDirectory, str, dict[str, str]]
type OverlayResult struct {
	// dir 临时目录根（偏差 D-dsh-3：TemporaryDirectory → MkdirTemp + cleanup）
	dir string
	// path host.patch.yml 绝对路径
	path string
	// envName 环境变量名 OPENJIUWEN_DSH_HOST_<HEX>
	envName string
	// envValue JSON 序列化的 values
	envValue string
}

// EnvName 环境变量名。
func (o *OverlayResult) EnvName() string { return o.envName }

// EnvValue 环境变量值（JSON 序列化的 values）。
func (o *OverlayResult) EnvValue() string { return o.envValue }

// Path host.patch.yml 绝对路径。
func (o *OverlayResult) Path() string { return o.path }

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
		config := map[string]any{"serverName": server.Name, "failOnStartupError": true}
		switch server.Transport {
		case harnessprotocol.McpTransportStdio:
			if len(server.Command) == 0 {
				return nil, fmt.Errorf("DSH stdio MCP server %q requires a command", server.Name)
			}
			config["transport"] = "stdio"
			config["command"] = server.Command[0]
			config["args"] = append([]string{}, server.Command[1:]...)
			config["env"] = cloneStringMap(server.Env)
			if context.Cwd != nil && *context.Cwd != "" {
				config["cwd"] = *context.Cwd
			}
		case harnessprotocol.McpTransportHTTP:
			config["transport"] = "streamable-http"
			config["url"] = derefString(server.URL)
			config["headers"] = cloneStringMap(server.Headers)
		default:
			// Python: composition.py:37 in-process 拒绝
			return nil, &harnessprotocol.UnsupportedHarnessCapabilityError{
				Msg: "DSH cannot mount in-process MCP servers",
			}
		}
		configs = append(configs, config)
	}
	return configs, nil
}

// WriteOverlay 用户数据留在子进程环境并注册原生插件。
//
// Append 模式贡献一个独立 section 与单一插值变量：用户文本不会被递归解析为
// DSH 模板语法。
// Python: composition.py:42-113 write_overlay
//
// 偏差 D-dsh-3：tempfile.TemporaryDirectory → os.MkdirTemp，由 Cleanup 显式
// 回收（Python 靠 context 管理器自动清理）。
func WriteOverlay(
	context *harnessprotocol.HarnessContext,
	includePrompt bool,
	promptMode string,
	enableSkillPlugins bool,
) (*OverlayResult, error) {
	configs, err := MCPConfigs(context)
	if err != nil {
		return nil, err
	}
	// Python: composition.py:55-56（三条件全空 → nil）
	if len(configs) == 0 && !(includePrompt && context.SystemPrompt != "") && !enableSkillPlugins {
		return nil, nil
	}
	envName := "OPENJIUWEN_DSH_HOST_" + strings.ToUpper(
		strings.ReplaceAll(uuid.New().String(), "-", ""))
	dir, err := os.MkdirTemp("", "openjiuwen-dsh-")
	if err != nil {
		return nil, err
	}
	result := &OverlayResult{dir: dir, envName: envName}
	values := map[string]any{"mcps": configs}
	var rows []string
	var inserts []string

	build := func() error {
		if includePrompt && context.SystemPrompt != "" {
			plugin := filepath.Join(dir, "host-prompt.mjs")
			var body string
			if promptMode == "replace" {
				values["prompt"] = map[string]any{"personaPrefix": context.SystemPrompt}
				body = "  const value = JSON.parse(process.env." + envName + ").prompt.personaPrefix;\n" +
					"  ctx.on('system-prompt/assemble', async (_assembly, _context, next) => {\n" +
					"    const assembly = await next();\n" +
					"    return {...assembly, sections: assembly.sections.map(section =>\n" +
					"      section.name === 'deployment:persona-prefix' ? {...section, text: value} : section)};\n" +
					"  });\n"
			} else {
				values["append"] = context.SystemPrompt
				body = "  const value = JSON.parse(process.env." + envName + ").append;\n" +
					"  ctx.systemPrompt.variable('openjiuwen_host_instructions', () => value);\n" +
					"  ctx.systemPrompt.section({name: 'openjiuwen:host-instructions',\n" +
					"    order: Number.MAX_SAFE_INTEGER, text: '{{openjiuwen_host_instructions}}'});\n"
			}
			pluginContent := "export const name = 'openjiuwen-host-prompt';\n" +
				"export const inject = ['systemPrompt'];\n" +
				"export function apply(ctx) {\n" + body + "}\n"
			// Python: composition.py:84-89（写文本 + 0600）
			if err := os.WriteFile(plugin, []byte(pluginContent), 0o600); err != nil {
				return err
			}
			pluginJSON, err := json.Marshal(plugin)
			if err != nil {
				return err
			}
			inserts = append(inserts,
				"    - id: openjiuwen-host-prompt\n      name: "+string(pluginJSON)+"\n")
		}
		if enableSkillPlugins {
			// Python: composition.py:91-98（skill 三插件）
			skillPlugins := [][2]string{
				{"skill", "skill"},
				{"skill-filesystem", "skill-filesystem"},
				{"tool-skill", "tool-skill"},
			}
			for _, pair := range skillPlugins {
				inserts = append(inserts,
					fmt.Sprintf("    - id: %s\n      name: \"@deepseek-ai/dsh-%s\"\n", pair[0], pair[1]))
			}
		}
		for index := range configs {
			inserts = append(inserts,
				fmt.Sprintf("    - id: openjiuwen-mcp-%d\n", index)+
					"      name: \"@deepseek-ai/dsh-mcp-client\"\n"+
					fmt.Sprintf("      config: !!js \"JSON.parse(process.env.%s).mcps[%d]\"\n", envName, index))
		}
		if len(inserts) > 0 {
			rows = append(rows, "- insert:\n")
			rows = append(rows, inserts...)
		}
		path := filepath.Join(dir, "host.patch.yml")
		// Python: composition.py:107-109（写 patch + 0600）
		if err := os.WriteFile(path, []byte(strings.Join(rows, "")), 0o600); err != nil {
			return err
		}
		result.path = path
		return nil
	}

	if buildErr := build(); buildErr != nil {
		// Python: composition.py:110-112（失败清理后重抛）
		_ = os.RemoveAll(dir)
		return nil, buildErr
	}
	// Python: composition.py:113（env 变量 = JSON 序列化的 values）
	encoded, err := json.Marshal(values)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	result.envValue = string(encoded)
	return result, nil
}

// Cleanup 释放 overlay 临时目录（Python TemporaryDirectory.cleanup 等价；
// 幂等）。
func (o *OverlayResult) Cleanup() {
	if o == nil || o.dir == "" {
		return
	}
	_ = os.RemoveAll(o.dir)
	o.dir = ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// cloneStringMap 字符串映射拷贝（nil → nil）。
func cloneStringMap(value map[string]string) map[string]string {
	if value == nil {
		return nil
	}
	cloned := make(map[string]string, len(value))
	for key, item := range value {
		cloned[key] = item
	}
	return cloned
}

// derefString 指针字符串解引用（nil → 空串）。
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
