package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hookscfg "github.com/uapclaw/uapclaw-go/internal/common/hooks"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HookResult hook 执行结果，对齐 Python HookResult dataclass
type HookResult struct {
	// Outcome 执行结果类型（success/blocking/non_blocking_error）
	Outcome string
	// Error 错误/拦截原因
	Error string
	// ShowToModel 是否展示给模型（blocking 时为 true）
	ShowToModel bool
	// ModifiedInput 修改后的输入（由 hook 修改）
	ModifiedInput map[string]any
	// AdditionalContext 附加上下文
	AdditionalContext string
}

// LLMConfig prompt hook 使用的 LLM 配置
// 对齐 Python _query_llm 中从 config 提取的 APIKey/APIBase/ClientProvider/DefaultModel
type LLMConfig struct {
	// APIKey LLM API 密钥
	APIKey string
	// APIBase LLM API 地址
	APIBase string
	// ClientProvider LLM 客户端提供者
	ClientProvider string
	// DefaultModel 默认模型名
	DefaultModel string
}

// HookExecutor hook 执行器，对齐 Python HookExecutor
// 统一调度 command/prompt 两类 hook，返回 HookResult 列表
type HookExecutor struct {
	// llmConfig prompt hook 使用的 LLM 配置，内部创建 ModelClient，对齐 Python _query_llm
	llmConfig LLMConfig
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// HookOutcome hook 执行结果类型，对齐 Python HookOutcome
const (
	// HookOutcomeSuccess 执行成功，对齐 Python HookOutcome.SUCCESS
	HookOutcomeSuccess = "success"
	// HookOutcomeBlocking 阻塞执行，对齐 Python HookOutcome.BLOCKING
	HookOutcomeBlocking = "blocking"
	// HookOutcomeNonBlockingError 非阻塞错误，对齐 Python HookOutcome.NON_BLOCKING_ERROR
	HookOutcomeNonBlockingError = "non_blocking_error"
)

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentServer

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewHookExecutor 创建 HookExecutor，对齐 Python HookExecutor()
func NewHookExecutor(llmConfig LLMConfig) *HookExecutor {
	return &HookExecutor{llmConfig: llmConfig}
}

// RunAll 并行执行同一 matcher 下的所有 hooks，对齐 Python HookExecutor.run_all()
// Go 中使用 goroutine + WaitGroup 实现并发（等价 asyncio.gather）
func (e *HookExecutor) RunAll(ctx context.Context, hookConfigs []map[string]any, hookInput map[string]any, sessionID string) []HookResult {
	if len(hookConfigs) == 0 {
		return nil
	}

	results := make([]HookResult, len(hookConfigs))
	var wg sync.WaitGroup

	for i, cfg := range hookConfigs {
		wg.Add(1)
		go func(idx int, c map[string]any) {
			defer wg.Done()
			hookType, _ := c["type"].(string)
			if hookType == string(hookscfg.HookTypeCommand) || hookType == "" {
				// 默认类型为 command，对齐 Python: hook_type = cfg.get("type", "command")
				results[idx] = e.runCommandHook(ctx, c, hookInput)
			} else if hookType == string(hookscfg.HookTypePrompt) {
				results[idx] = e.runPromptHook(ctx, c, hookInput)
			}
			// 未知类型：对齐 Python 静默跳过，不设置 results[idx]，保持零值 HookResult{Outcome:""}
		}(i, cfg)
	}
	wg.Wait()

	// 将异常结果（outcome 为空）替换为 SUCCESS
	// 对齐 Python: 未知 hook 类型静默跳过，视为 SUCCESS
	for i, r := range results {
		if r.Outcome == "" {
			results[i] = HookResult{Outcome: HookOutcomeSuccess}
		}
	}
	return results
}

// ParseCommandOutput 解析 command hook 的 stdout JSON 协议
// 对齐 Python HookExecutor.parse_command_output（静态方法）
func ParseCommandOutput(stdout string) HookResult {
	if strings.TrimSpace(stdout) == "" {
		return HookResult{Outcome: HookOutcomeSuccess}
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &data); err != nil {
		// 非 JSON → SUCCESS，对齐 Python: json.JSONDecodeError → return HookResult(outcome=SUCCESS)
		return HookResult{Outcome: HookOutcomeSuccess}
	}

	if _, ok := data["decision"]; !ok {
		// 非 dict 或无 decision 字段 → SUCCESS，对齐 Python: not isinstance(data, dict) → SUCCESS
		return HookResult{Outcome: HookOutcomeSuccess}
	}

	decision, _ := data["decision"].(string)
	if decision == "block" {
		// 对齐 Python: decision == "block" → BLOCKING
		reason := "blocked by hook"
		if v, ok := data["reason"].(string); ok && v != "" {
			reason = v
		}
		return HookResult{
			Outcome:     HookOutcomeBlocking,
			Error:       reason,
			ShowToModel: true,
		}
	}

	// 对齐 Python: decision != "block" → SUCCESS + 可能有 modifiedInput/additionalContext/reason
	result := HookResult{Outcome: HookOutcomeSuccess}
	if v, ok := data["modifiedInput"]; ok {
		if m, ok := v.(map[string]any); ok {
			result.ModifiedInput = m
		}
	}
	if v, ok := data["additionalContext"]; ok {
		if s, ok := v.(string); ok {
			result.AdditionalContext = s
		}
	}
	// 对齐 Python: "reason" in data and decision != "block" → additional_context = data["reason"]（无条件覆盖）
	if v, ok := data["reason"].(string); ok && decision != "block" {
		result.AdditionalContext = v
	}
	return result
}

// ExtractJSONFromResponse 从 LLM 响应中提取 JSON 对象
// 对齐 Python HookExecutor.extract_json_from_response（静态方法）
func ExtractJSONFromResponse(text string) map[string]any {
	if text == "" {
		return map[string]any{}
	}
	text = strings.TrimSpace(text)

	// 1. 直接 JSON 解析，对齐 Python: json.loads(text)
	var data map[string]any
	if err := json.Unmarshal([]byte(text), &data); err == nil {
		return data
	}

	// 2. markdown fence ```json``` 提取，对齐 Python: re.search(r'```(?:json)?\s*([\s\S]*?)```', text)
	re := regexp.MustCompile("```(?:json)?\\s*([\\s\\S]*?)```")
	if match := re.FindStringSubmatch(text); len(match) > 1 {
		var fenceData map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(match[1])), &fenceData); err == nil {
			return fenceData
		}
	}

	// 3. 嵌入式 { ... } 提取，对齐 Python: text.find("{") + text.rfind("}")
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		var embedData map[string]any
		if err := json.Unmarshal([]byte(text[start:end+1]), &embedData); err == nil {
			return embedData
		}
	}

	// 4. 失败 → 空 map，对齐 Python: return {}
	return map[string]any{}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// runCommandHook 执行 command 类型 hook（子进程），对齐 Python _run_command_hook
func (e *HookExecutor) runCommandHook(ctx context.Context, config map[string]any, hookInput map[string]any) HookResult {
	command, _ := config["command"].(string)
	if command == "" {
		// 对齐 Python: not command → NON_BLOCKING_ERROR("empty command")
		return HookResult{Outcome: HookOutcomeNonBlockingError, Error: "empty command"}
	}

	// 对齐 Python: timeout = config.get("timeout", 30)
	timeout := 30
	if v, ok := config["timeout"]; ok {
		switch n := v.(type) {
		case int:
			timeout = n
		case float64:
			timeout = int(n)
		}
	}
	// 对齐 Python: shell = config.get("shell", "bash")
	shell := "bash"
	if v, ok := config["shell"].(string); ok && v != "" {
		shell = v
	}

	hookInputJSON, err := json.Marshal(hookInput)
	if err != nil {
		return HookResult{Outcome: HookOutcomeNonBlockingError, Error: fmt.Sprintf("serialize hook input: %v", err)}
	}
	// 对齐 Python: tool_name = hook_input.get("tool_name", "")
	toolName, _ := hookInput["tool_name"].(string)

	// 对齐 Python: env = os.environ.copy(); env["ARGUMENTS"] = hook_input_json; env["TOOL_NAME"] = tool_name
	env := os.Environ()
	env = append(env, fmt.Sprintf("ARGUMENTS=%s", string(hookInputJSON)))
	env = append(env, fmt.Sprintf("TOOL_NAME=%s", toolName))

	// 使用带超时的 context 控制子进程生命周期，避免手动 goroutine + select 的竞态
	// 对齐 Python: asyncio.wait_for(proc.communicate(...), timeout=timeout)
	timeoutCtx, timeoutCancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer timeoutCancel()

	// 对齐 Python: proc = await asyncio.create_subprocess_exec(shell, "-c", command, stdin=PIPE, stdout=PIPE, stderr=PIPE, env=env)
	cmd := exec.CommandContext(timeoutCtx, shell, "-c", command)
	cmd.Env = env
	// 对齐 Python: proc.communicate(input=hook_input_json.encode()) — stdin 传入 JSON
	cmd.Stdin = strings.NewReader(string(hookInputJSON))

	var stdoutBuf, stderrBuf strings.Builder
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()

	// 获取退出码，对齐 Python: returncode = proc.returncode
	returnCode := -1
	if cmd.ProcessState != nil {
		returnCode = cmd.ProcessState.ExitCode()
	}

	// 判断是否超时：context 超时且进程被 kill
	if timeoutCtx.Err() == context.DeadlineExceeded {
		logger.Debug(logComponent).Int("timeout", timeout).Str("command", command).Msg("hook 子进程超时，已 kill")
		return HookResult{Outcome: HookOutcomeNonBlockingError, Error: fmt.Sprintf("hook timeout after %ds", timeout)}
	}

	if runErr != nil {
		// 对齐 Python: except Exception as e → NON_BLOCKING_ERROR(str(e))
		if cmd.ProcessState == nil {
			return HookResult{Outcome: HookOutcomeNonBlockingError, Error: runErr.Error()}
		}
	}

	stdout := stdoutBuf.String()
	stderr := stderrBuf.String()

	// 对齐 Python: if returncode is None → NON_BLOCKING_ERROR("hook process killed")
	if cmd.ProcessState == nil {
		return HookResult{Outcome: HookOutcomeNonBlockingError, Error: "hook process killed"}
	}

	// 对齐 Python 退出码语义：
	if returnCode == 0 {
		// 退出码 0 → 解析命令输出
		return ParseCommandOutput(stdout)
	}

	if returnCode == 2 {
		// exit 2 = blocking，对齐 Python: elif returncode == 2
		parsed := ParseCommandOutput(stdout)
		reason := ""
		if parsed.Outcome == HookOutcomeBlocking {
			reason = parsed.Error
		} else if parsed.AdditionalContext != "" {
			reason = parsed.AdditionalContext
		}
		if reason == "" {
			// fallback 到 stderr，对齐 Python: reason = stderr.strip() or "hook blocked execution"
			reason = strings.TrimSpace(stderr)
			if reason == "" {
				reason = "hook blocked execution"
			}
		}
		return HookResult{
			Outcome:     HookOutcomeBlocking,
			Error:       reason,
			ShowToModel: true,
		}
	}

	// 其他退出码 → NON_BLOCKING_ERROR(stderr or f"exit code {returncode}")
	// 对齐 Python: else → NON_BLOCKING_ERROR(stderr or f"exit code {returncode}")
	errMsg := strings.TrimSpace(stderr)
	if errMsg == "" {
		errMsg = fmt.Sprintf("exit code %d", returnCode)
	}
	return HookResult{Outcome: HookOutcomeNonBlockingError, Error: errMsg}
}

// runPromptHook 执行 prompt 类型 hook（LLM 审核），对齐 Python _run_prompt_hook
func (e *HookExecutor) runPromptHook(ctx context.Context, config map[string]any, hookInput map[string]any) HookResult {
	promptTemplate, _ := config["prompt"].(string)
	if promptTemplate == "" {
		// 对齐 Python: not prompt → NON_BLOCKING_ERROR("empty prompt")
		return HookResult{Outcome: HookOutcomeNonBlockingError, Error: "empty prompt"}
	}

	// 对齐 Python: timeout = config.get("timeout", 15)
	timeout := 15
	if v, ok := config["timeout"]; ok {
		switch n := v.(type) {
		case int:
			timeout = n
		case float64:
			timeout = int(n)
		}
	}
	// 对齐 Python: model_name = config.get("model", "")
	modelName, _ := config["model"].(string)

	// 对齐 Python: Python: hook_input_json = json.dumps(hook_input, ensure_ascii=False)
	// Python: final_prompt = prompt_template.replace("$ARGUMENTS", hook_input_json)
	// Python: final_prompt = final_prompt.replace("$TOOL_NAME", tool_name)
	hookInputJSON, _ := json.Marshal(hookInput)
	toolName, _ := hookInput["tool_name"].(string)
	finalPrompt := strings.ReplaceAll(promptTemplate, "$ARGUMENTS", string(hookInputJSON))
	finalPrompt = strings.ReplaceAll(finalPrompt, "$TOOL_NAME", toolName)

	// 带超时调用 LLM，对齐 Python: asyncio.wait_for(self._query_llm(prompt, model), timeout=timeout)
	type llmResult struct {
		text string
		err  error
	}
	resultCh := make(chan llmResult, 1)
	go func() {
		text, err := e.queryLLM(ctx, finalPrompt, modelName)
		resultCh <- llmResult{text, err}
	}()

	var result llmResult
	select {
	case <-time.After(time.Duration(timeout) * time.Second):
		// 对齐 Python: asyncio.TimeoutError → NON_BLOCKING_ERROR(f"prompt hook timeout after {timeout}s")
		return HookResult{Outcome: HookOutcomeNonBlockingError, Error: fmt.Sprintf("prompt hook timeout after %ds", timeout)}
	case result = <-resultCh:
		if result.err != nil {
			// 对齐 Python: except Exception as e → NON_BLOCKING_ERROR(str(e))
			return HookResult{Outcome: HookOutcomeNonBlockingError, Error: result.err.Error()}
		}
	}

	// 对齐 Python: data = self.extract_json_from_response(response_text)
	data := ExtractJSONFromResponse(result.text)
	decision, _ := data["decision"].(string)
	if decision == "" {
		decision = "allow" // 默认允许，对齐 Python: decision = data.get("decision", "allow")
	}

	if decision == "block" {
		// 对齐 Python: decision == "block" → BLOCKING
		reason, _ := data["reason"].(string)
		if reason == "" {
			reason = "blocked by prompt hook"
		}
		return HookResult{
			Outcome:     HookOutcomeBlocking,
			Error:       reason,
			ShowToModel: true,
		}
	}

	// 对齐 Python: result = HookResult(outcome=SUCCESS) + modifiedInput/additionalContext
	r := HookResult{Outcome: HookOutcomeSuccess}
	if v, ok := data["modifiedInput"]; ok {
		if m, ok := v.(map[string]any); ok {
			r.ModifiedInput = m
		}
	}
	if v, ok := data["additionalContext"]; ok {
		if s, ok := v.(string); ok {
			r.AdditionalContext = s
		}
	}
	return r
}

// queryLLM 调用 LLM 执行 hook 审查，对齐 Python _query_llm
// 内部用 LLMConfig 创建 Model 实例（对齐 Python: 动态 import config + 创建 Model）
func (e *HookExecutor) queryLLM(ctx context.Context, prompt, modelName string) (string, error) {
	clientConfig, cfgErr := llmschema.NewModelClientConfig(e.llmConfig.ClientProvider, e.llmConfig.APIKey, e.llmConfig.APIBase)
	if cfgErr != nil {
		return "", fmt.Errorf("创建 ModelClientConfig 失败: %w", cfgErr)
	}
	model, err := llm.NewModel(clientConfig, nil)
	if err != nil {
		return "", fmt.Errorf("创建 Model 失败: %w", err)
	}

	// 对齐 Python: model = model_name or default_model
	effectiveModel := modelName
	if effectiveModel == "" {
		effectiveModel = e.llmConfig.DefaultModel
	}

	// 对齐 Python: response = await model.invoke(messages=[{"role": "user", "content": prompt}], temperature=0.0, max_tokens=1024, model=model_name)
	messages := model_clients.NewMessagesParam(llmschema.NewUserMessage(prompt))
	opts := []model_clients.InvokeOption{
		model_clients.WithInvokeTemperature(0.0),
		model_clients.WithInvokeMaxTokens(1024),
		model_clients.WithInvokeModel(effectiveModel),
	}

	response, err := model.Invoke(ctx, messages, opts...)
	if err != nil {
		return "", fmt.Errorf("LLM Invoke 失败: %w", err)
	}

	// 对齐 Python: content = response.content
	// isinstance(content, str) → 返回文本内容
	// isinstance(content, list) → 拼接文本部分
	content := response.Content
	if content.IsText() {
		return content.Text(), nil
	}

	// 多模态 → 拼接文本部分，对齐 Python: parts = [block["text"] for block in content if isinstance(block, dict) and "text" in block]
	var textParts []string
	for _, part := range content.Parts() {
		if part.Type == "text" && part.Text != "" {
			textParts = append(textParts, part.Text)
		}
	}
	return strings.Join(textParts, "\n"), nil
}
