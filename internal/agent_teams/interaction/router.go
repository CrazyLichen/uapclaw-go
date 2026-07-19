package interaction

import (
	"fmt"
	"regexp"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// mentionRe @target body 正则
	// 对齐 Python: _MENTION_RE = re.compile(r"^@(\S+)\s+([\s\S]+)$")
	mentionRe = regexp.MustCompile(`^@(\S+)\s+([\s\S]+)$`)
	// humanAgentPrefixRe $name 前缀正则（第一步：匹配 $name 后跟空格）
	// 对齐 Python: _HUMAN_AGENT_PREFIX_RE = re.compile(r"^\$([^\s@]+)(?:\s+|(?=@))([\s\S]*)$")
	// Go regexp 不支持 lookahead，拆成两步：
	//   1. humanAgentPrefixSpaceRe 匹配 "$name " 格式
	//   2. humanAgentPrefixAtRe 匹配 "$name@" 格式
	humanAgentPrefixSpaceRe = regexp.MustCompile(`^\$([^\s@]+)\s+([\s\S]*)$`)
	humanAgentPrefixAtRe    = regexp.MustCompile(`^\$([^\s@]+)@([\s\S]*)$`)
	// recipientRe @name 后续匹配正则
	// 对齐 Python: _RECIPIENT_RE = re.compile(r"^@(\S+)\s+")
	recipientRe = regexp.MustCompile(`^@(\S+)\s+`)
	// BroadcastTargets 广播目标集合
	// 对齐 Python: BROADCAST_TARGETS = frozenset({"all", "*"})
	BroadcastTargets = map[string]bool{"all": true, "*": true}
	// routerLogComponent 日志组件
	routerLogComponent = logger.ComponentChannel
)

// MemberExistsCheck 成员存在性检查函数类型。
// 对齐 Python: MemberExistsCheck = Callable[[str], Awaitable[bool]]
//
// 每个调用方决定查询来源——通常是闭包包裹
// TeamBackend.get_member（或任何将名称映射到花名册行的等价物）。
type MemberExistsCheck func(name string) (bool, error)

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseInteractStr 将自由文本解析为 InteractPayload 列表。
// 对齐 Python: parse_interact_str(body) (openjiuwen/agent_teams/interaction/router.py)
//
// 语法规则：
//
//	input := channel? recipients? body
//	channel := "# " | "$" name (" " | "@")    // 默认 "# "
//	recipients := ("@" name " ")*
//	body := <remaining text>
//
// 产出的载荷列表：
//   - 空/纯空格输入 → 空列表
//   - GodViewMessage — `# body`（无 @recipient），或裸文本
//   - HumanAgentMessage — `$name body`（无 recipient），驱动 avatar
//   - OperatorMessage(target=nil) — `# @all/@* body`，广播
//   - HumanAgentMessage(target="*") — `$name @all/@* body`，广播
//   - []OperatorMessage — `# @m1 @m2 body`，每个 recipient 一个
//   - []HumanAgentMessage — `$name @m1 @m2 body`，每个 recipient 一个
func ParseInteractStr(body string) []InteractPayload {
	// 对齐 Python: if not body or not body.strip(): return []
	if body == "" {
		return nil
	}
	allSpace := true
	for _, r := range body {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			allSpace = false
			break
		}
	}
	if allSpace {
		return nil
	}

	rest := body
	sender := agentteams.UserPseudoMemberName
	isHumanAgent := false

	// ---- 通道前缀 ----
	// 对齐 Python: if rest.startswith(_GOD_VIEW_PREFIX):
	if len(rest) >= 2 && rest[:2] == "# " {
		rest = rest[2:]
		rest = trimLeadingSpaces(rest)
	} else {
		// 对齐 Python: match = _HUMAN_AGENT_PREFIX_RE.match(rest)
		// Go 不支持 lookahead，拆成两步尝试
		match := humanAgentPrefixSpaceRe.FindStringSubmatch(rest)
		if match == nil {
			match = humanAgentPrefixAtRe.FindStringSubmatch(rest)
		}
		if match != nil {
			sender = match[1]
			rest = match[2]
			rest = trimLeadingSpaces(rest)
			isHumanAgent = true
		}
		// else: 无识别前缀 → 视为 "# " 默认；rest 保持完整
	}

	// ---- 接收者 ----
	// 对齐 Python: while True: match = _RECIPIENT_RE.match(rest); if match is None: break
	var recipients []string
	for {
		match := recipientRe.FindStringSubmatch(rest)
		if match == nil {
			break
		}
		recipients = append(recipients, match[1])
		rest = rest[len(match[0]):]
	}

	finalBody := rest

	// ---- 载荷合成 ----
	// 对齐 Python: if not recipients:
	if len(recipients) == 0 {
		if isHumanAgent {
			// 对齐 Python: return [HumanAgentMessage(body=final_body, sender=sender)]
			return []InteractPayload{NewHumanAgentMessage(finalBody, sender, nil)}
		}
		// 对齐 Python: return [GodViewMessage(body=final_body)]
		return []InteractPayload{NewGodViewMessage(finalBody)}
	}

	// 对齐 Python: has_broadcast = any(r in BROADCAST_TARGETS for r in recipients)
	hasBroadcast := false
	for _, r := range recipients {
		if BroadcastTargets[r] {
			hasBroadcast = true
			break
		}
	}

	if hasBroadcast {
		// 对齐 Python: 广播覆盖所有其他命名接收者
		if isHumanAgent {
			// 对齐 Python: return [HumanAgentMessage(body=final_body, sender=sender, target="*")]
			broadcastTarget := "*"
			return []InteractPayload{NewHumanAgentMessage(finalBody, sender, &broadcastTarget)}
		}
		// 对齐 Python: return [OperatorMessage(body=final_body)]
		return []InteractPayload{NewOperatorMessage(finalBody, nil)}
	}

	if isHumanAgent {
		// 对齐 Python: return [HumanAgentMessage(body=final_body, sender=sender, target=name) for name in recipients]
		result := make([]InteractPayload, len(recipients))
		for i, name := range recipients {
			target := name
			result[i] = NewHumanAgentMessage(finalBody, sender, &target)
		}
		return result
	}
	// 对齐 Python: return [OperatorMessage(body=final_body, target=name) for name in recipients]
	result := make([]InteractPayload, len(recipients))
	for i, name := range recipients {
		target := name
		result[i] = NewOperatorMessage(finalBody, &target)
	}
	return result
}

// ParseMention 解析单个 @target body。
// 对齐 Python: parse_mention(content) (openjiuwen/agent_teams/interaction/router.py)
// 返回 (target, body, true) 匹配成功；("","",false) 无匹配。
func ParseMention(content string) (target string, body string, ok bool) {
	if content == "" {
		return "", "", false
	}
	match := mentionRe.FindStringSubmatch(content)
	if match == nil {
		return "", "", false
	}
	return match[1], match[2], true
}

// IsReservedName 检查是否为运行时保留成员名。
// 对齐 Python: is_reserved_name(name) (openjiuwen/agent_teams/interaction/router.py)
func IsReservedName(name string) bool {
	return agentteams.ReservedMemberNames[name]
}

// ResolveTargets 校验 @<member> 接收者是否在花名册中。
// 对齐 Python: resolve_targets(payloads, *, member_exists) (openjiuwen/agent_teams/interaction/router.py)
//
// 已知接收者保留原载荷；未知接收者的 @提及折回到一条无提及消息；
// God View / Avatar 驱动 / 广播载荷不携带命名目标，直接透传。
func ResolveTargets(payloads []InteractPayload, memberExists MemberExistsCheck) ([]InteractPayload, error) {
	var unknown []InteractPayload
	var kept []InteractPayload

	for _, p := range payloads {
		name := namedTarget(p)
		if name == nil {
			kept = append(kept, p)
			continue
		}
		exists, err := memberExists(*name)
		if err != nil {
			return nil, err
		}
		if exists {
			kept = append(kept, p)
		} else {
			unknown = append(unknown, p)
		}
	}

	// 对齐 Python: if not unknown: return payloads
	if len(unknown) == 0 {
		return payloads, nil
	}
	// 对齐 Python: return kept + [_fold_unknown_mentions(unknown)]
	return append(kept, foldUnknownMentions(unknown)), nil
}

// DeliverDirect 验证 target 并发送点对点消息。
// 对齐 Python: deliver_direct(body, *, sender, target, message_manager, member_exists)
//
// messageManager 为 any 占位：
//
//	⤵️ 待 9.55 回填: TeamMessageManager — 调用 send_message(content, to_member_name, from_member_name)
func DeliverDirect(body string, sender string, target string, messageManager any, memberExists MemberExistsCheck) (*DeliverResult, error) {
	// 对齐 Python: if not await member_exists(target): return DeliverResult.failure(f"unknown_member:{target}")
	exists, err := memberExists(target)
	if err != nil {
		return nil, err
	}
	if !exists {
		reason := "unknown_member:" + target
		return NewDeliverResultFailure(reason), nil
	}
	// ⤵️ 待 9.55 回填: messageManager.SendMessage(content=body, to_member_name=target, from_member_name=sender)
	// 对齐 Python: msg_id = await message_manager.send_message(content=body, to_member_name=target, from_member_name=sender)
	// 当前 stub: 模拟成功
	logger.Debug(routerLogComponent).Str("sender", sender).Str("target", target).
		Str("body_len", fmt.Sprintf("%d", len(body))).
		Msg("DeliverDirect (stub)")
	msgID := "stub-msg-id"
	return NewDeliverResultSuccess(&msgID), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// namedTarget 提取载荷中的点对点接收者。
// 对齐 Python: _named_target(payload)
// 返回 nil 表示无命名目标（God View / Avatar 驱动 / 广播）。
func namedTarget(payload InteractPayload) *string {
	switch p := payload.(type) {
	case *OperatorMessage:
		t := p.Target()
		if t == nil || BroadcastTargets[*t] {
			return nil
		}
		return t
	case *HumanAgentMessage:
		t := p.Target()
		if t == nil || BroadcastTargets[*t] {
			return nil
		}
		return t
	default:
		return nil
	}
}

// foldUnknownMentions 折叠未知 @提及到一条无提及消息。
// 对齐 Python: _fold_unknown_mentions(unknown)
//
// 未知 @<member> 不是路由指令——是用户输入的纯文本。
// 将这些提及重新附加到共享 body，路由到通道默认受众。
func foldUnknownMentions(unknown []InteractPayload) InteractPayload {
	// 对齐 Python: sample = unknown[0]
	sample := unknown[0]
	// 对齐 Python: mentions = " ".join(f"@{p.target}" for p in unknown)
	mentions := ""
	for i, p := range unknown {
		if i > 0 {
			mentions += " "
		}
		nt := namedTarget(p)
		if nt != nil {
			mentions += "@" + *nt
		}
	}
	// 对齐 Python: general_body = f"{mentions} {sample.body}" if sample.body else mentions
	generalBody := mentions + " " + sample.Body()
	if sample.Body() == "" {
		generalBody = mentions
	}

	// 对齐 Python: isinstance(sample, HumanAgentMessage) → HumanAgentMessage; else → GodViewMessage
	switch s := sample.(type) {
	case *HumanAgentMessage:
		return NewHumanAgentMessage(generalBody, s.Sender(), nil)
	default:
		return NewGodViewMessage(generalBody)
	}
}

// trimLeadingSpaces 去除前导空白。
func trimLeadingSpaces(s string) string {
	for i, r := range s {
		if r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return s[i:]
		}
	}
	return ""
}
