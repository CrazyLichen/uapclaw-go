package shell

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dlclark/regexp2"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/filesystem"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// recurseRe 匹配 -Recurse 标志
	recurseRe = regexp.MustCompile(`(?i)-Recurse\b`)
	// valuelessFlagsRe 匹配无值标志 (Force/WhatIf/Confirm/Verbose)
	valuelessFlagsRe = regexp.MustCompile(`(?i)-(?:Force|WhatIf|Confirm|Verbose)\b`)
	// errorActionRe 匹配 -ErrorAction <value>
	errorActionRe = regexp.MustCompile(`(?i)-ErrorAction\s+\S+`)
	// pathFlagRe 匹配 -Path 或 -LiteralPath 参数
	// 使用 regexp2 支持 Perl backreference 语法 \1
	pathFlagRe = regexp2.MustCompile(`(?i)-(?:Path|LiteralPath)\s+(["\']?)(.+?)\1(?:\s|$)`, 0)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseRmTargets 解析 Unix rm 命令中的删除目标。
// 对齐 Python: _parse_rm_targets (filesystem.py L106-129)
// 从简单的 rm 命令中提取显式的、非通配符的文件路径。
// 对于复合命令、通配符模式或递归标志，返回空切片。
func ParseRmTargets(command string) []string {
	stripped := strings.TrimSpace(command)

	// 复合命令返回空
	for _, op := range []string{"|", ";", "&&", "||", "\n", "`", "$("} {
		if strings.Contains(stripped, op) {
			return nil
		}
	}

	// 用 shlex 风格的拆分
	parts, err := shlexSplit(stripped)
	if err != nil || len(parts) == 0 {
		return nil
	}

	// 检查第一个 token 是否为 rm
	base := parts[0]
	if idx := strings.LastIndex(base, "/"); idx >= 0 {
		base = base[idx+1:]
	}
	if base != "rm" {
		return nil
	}

	var targets []string
	for _, part := range parts[1:] {
		if strings.HasPrefix(part, "-") {
			// 递归删除标志 → 返回空
			if strings.ContainsAny(part, "rR") {
				return nil
			}
			continue
		}
		// 通配符模式 → 跳过
		if strings.ContainsAny(part, "*?[{") {
			continue
		}
		targets = append(targets, part)
	}
	return targets
}

// ParsePSRemoveTargets 解析 PowerShell Remove-Item 命令中的删除目标。
// 对齐 Python: _parse_ps_remove_targets (filesystem.py L132-177)
// 处理 Remove-Item 及其别名 (rm, del, ri, erase)。
// 对于递归删除、通配符或复合命令，返回空切片。
func ParsePSRemoveTargets(command string) []string {
	stripped := strings.TrimSpace(command)

	// 复合命令返回空
	for _, op := range []string{"|", ";", "\n", "`"} {
		if strings.Contains(stripped, op) {
			return nil
		}
	}

	removeAliases := []string{"remove-item", "ri", "rm", "del", "erase"}
	cmdLower := strings.ToLower(stripped)

	matchedAlias := ""
	for _, alias := range removeAliases {
		if strings.HasPrefix(cmdLower, alias+" ") || cmdLower == alias {
			matchedAlias = alias
			break
		}
	}
	if matchedAlias == "" {
		return nil
	}

	rest := strings.TrimSpace(stripped[len(matchedAlias):])

	// 拒绝递归删除
	if recurseRe.MatchString(rest) {
		return nil
	}

	// 去除无值标志
	rest = stripValuelessFlags(rest)
	rest = stripErrorAction(rest)

	// 处理 -Path 或 -LiteralPath 命名参数
	pathMatch, err := pathFlagRe.FindStringMatch(rest)
	if err == nil && pathMatch != nil {
		group2 := pathMatch.GroupByNumber(2)
		if group2 != nil {
			path := strings.TrimSpace(group2.String())
			if strings.ContainsAny(path, "*?[") {
				return nil
			}
			return []string{path}
		}
	}

	// 位置参数路径
	var targets []string
	for _, token := range strings.Fields(rest) {
		if strings.HasPrefix(token, "-") {
			continue
		}
		token = strings.Trim(token, "\"'")
		if strings.ContainsAny(token, "*?[") {
			continue
		}
		targets = append(targets, token)
	}
	return targets
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// shlexSplit 类似 Python shlex.split 的简单 shell 词法拆分
func shlexSplit(s string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	quote := byte(0)
	escaped := false

	for i := 0; i < len(s); i++ {
		char := s[i]
		if escaped {
			current.WriteByte(char)
			escaped = false
		} else if char == '\\' {
			escaped = true
		} else if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				current.WriteByte(char)
			}
		} else if char == '"' || char == '\'' {
			quote = char
		} else if char == ' ' || char == '\t' {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		} else {
			current.WriteByte(char)
		}
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens, nil
}

// stripValuelessFlags 去除无值标志
func stripValuelessFlags(rest string) string {
	return strings.TrimSpace(valuelessFlagsRe.ReplaceAllString(rest, ""))
}

// stripErrorAction 去除 -ErrorAction <value>
func stripErrorAction(rest string) string {
	return strings.TrimSpace(errorActionRe.ReplaceAllString(rest, ""))
}

// buildHistoryPathFromOpts 从 ToolOption 列表中提取 session 信息，构建 history path。
// 对齐 Python: _build_history_path(session)
func buildHistoryPathFromOpts(opts []tool.ToolOption, agentID string) string {
	callOpts := &tool.ToolCallOptions{}
	for _, opt := range opts {
		opt(callOpts)
	}

	if callOpts.Session == nil {
		return ""
	}

	sessionFacade, ok := callOpts.Session.(sessioninterfaces.SessionFacade)
	if !ok || sessionFacade == nil {
		return ""
	}

	sessionID := sessionFacade.GetSessionID()
	if sessionID == "" {
		return ""
	}

	baseDir := workspace.WorkspaceDir()
	return filesystem.BuildHistoryPath(baseDir, agentID, sessionID)
}

// recordRmTargetsBeforeDeletion 在执行 rm 前记录被删除文件的内容到历史。
// 对齐 Python: _record_rm_targets_before_deletion(history_path, file_paths, operation)
// (filesystem.py line 180-202)
func recordRmTargetsBeforeDeletion(historyPath string, targets []string, cwdPath string) {
	if len(targets) == 0 {
		return
	}

	for _, rawPath := range targets {
		absPath := rawPath
		if !filepath.IsAbs(rawPath) {
			absPath = filepath.Join(cwdPath, rawPath)
		}

		// 检查文件是否存在且是普通文件
		if st, err := os.Stat(absPath); err != nil || st.IsDir() {
			continue
		}

		// 读取文件内容
		data, err := os.ReadFile(absPath)
		if err != nil {
			logger.Warn(logger.ComponentAgentCore).
				Str("abs_path", absPath).
				Err(err).
				Msg("读取 rm 目标文件失败")
			continue
		}
		content := string(data)

		filesystem.AppendOpHistory(historyPath, absPath, "delete", &content, nil)
	}
}
