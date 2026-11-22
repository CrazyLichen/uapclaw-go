package tools

import (
	"strings"
)

// IsSendFileEnabled 判断渠道是否允许文件投递。
//
// Python: is_send_file_enabled (file_delivery_policy.py)。
// 小艺没有可用的入站文件投递路径——这是渠道能力限制，不是用户可选策略。
func IsSendFileEnabled(config map[string]any, channelID string) bool {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(channelID)), "xiaoyi") {
		return false
	}
	channels, _ := config["channels"].(map[string]any)
	allowed := channelSendFileAllowed(channels, channelID)
	// 全双工经私有流中转，但向 Web 会话投递文件。
	policyChannel := channelID
	if channelID == "video_tool" {
		policyChannel = "web"
	}
	if allowed == nil {
		allowed = channelSendFileAllowed(channels, policyChannel)
	}
	if allowed == nil {
		return policyChannel == "web"
	}
	b, _ := allowed.(bool)
	return b
}

// channelSendFileAllowed 读取 channels.<channel>.send_file_allowed；缺失返回 nil。
func channelSendFileAllowed(channels map[string]any, channel string) any {
	if channels == nil {
		return nil
	}
	entry, ok := channels[channel].(map[string]any)
	if !ok {
		return nil
	}
	if v, ok := entry["send_file_allowed"]; ok {
		return v
	}
	return nil
}
