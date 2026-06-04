package dotenv

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Parse 解析 .env 文件内容为 key-value 映射。
//
// 支持的格式：
//   - KEY=VALUE
//   - KEY="VALUE" / KEY='VALUE'（引号内的 # 不会被当作注释）
//   - # 注释行
//   - 空行跳过
//   - export KEY=VALUE（export 前缀被忽略）
//   - KEY=VALUE # comment（无引号时行尾注释）
//
// 对应 Python: dotenv.load_dotenv() 的解析逻辑
func Parse(content string) map[string]string {
	result := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// 跳过空行和注释行
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// 去除 export 前缀
		line = strings.TrimPrefix(line, "export ")
		line = strings.TrimSpace(line)

		// 解析 KEY=VALUE
		key, value, ok := parseKeyValue(line)
		if !ok {
			continue
		}

		result[key] = value
	}

	return result
}

// Load 加载 .env 文件并将变量写入环境变量（override 模式）。
//
// 读取指定路径的 .env 文件，解析后以 override 方式写入 os.Setenv，
// 即 .env 中的值会覆盖已有环境变量。
// 对应 Python: dotenv.load_dotenv(path, override=True)
func Load(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取 .env 文件失败: %w", err)
	}

	kvs := Parse(string(data))
	for k, v := range kvs {
		if err := os.Setenv(k, v); err != nil {
			return fmt.Errorf("设置环境变量 %s 失败: %w", k, err)
		}
	}

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseKeyValue 解析单行 KEY=VALUE，返回 key、value 和是否成功。
func parseKeyValue(line string) (string, string, bool) {
	// 找到第一个等号的位置
	eqIdx := strings.Index(line, "=")
	if eqIdx < 1 { // 等号不存在或 key 为空
		return "", "", false
	}

	key := strings.TrimSpace(line[:eqIdx])
	value := strings.TrimSpace(line[eqIdx+1:])

	if key == "" {
		return "", "", false
	}

	// 处理带引号的值
	value = parseValue(value)

	return key, value, true
}

// parseValue 解析值部分，处理引号和行尾注释。
func parseValue(value string) string {
	if len(value) == 0 {
		return ""
	}

	// 双引号包裹
	if value[0] == '"' {
		return parseQuotedValue(value, '"')
	}

	// 单引号包裹
	if value[0] == '\'' {
		return parseQuotedValue(value, '\'')
	}

	// 无引号：处理行尾注释（# 后面的是注释，但需避免误切 URL 中的 #）
	// 简单策略：仅在 # 前有空格时才视为注释分隔
	if idx := findInlineComment(value); idx >= 0 {
		value = strings.TrimSpace(value[:idx])
	}

	return value
}

// parseQuotedValue 解析带引号的值，引号内的 # 不视为注释。
func parseQuotedValue(value string, quote byte) string {
	// 找到闭合引号
	for i := 1; i < len(value); i++ {
		if value[i] == quote {
			// 检查是否被转义（前一个字符是 \）
			if i > 0 && value[i-1] == '\\' {
				continue
			}
			return value[1:i]
		}
	}
	// 没有闭合引号，返回引号后的全部内容
	return value[1:]
}

// findInlineComment 在无引号的值中查找行尾注释的位置。
//
// 仅当 # 前有空格时才视为注释分隔符，
// 避免 http://example.com#anchor 这样的 URL 被误切。
func findInlineComment(value string) int {
	for i := 0; i < len(value); i++ {
		if value[i] == '#' {
			// # 在行首或前面有空格，视为注释
			if i == 0 || value[i-1] == ' ' {
				return i
			}
		}
	}
	return -1
}
