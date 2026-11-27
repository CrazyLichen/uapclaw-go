/**
 * 前端功能开关（集中管理，便于按构建裁剪 UI）
 */
// 2026-11-27 用户裁决：桌面/自托管分发无应用内更新链路，移除更新页签
export const FEATURE_APP_UPDATER_UI = false;

/**
 * 个人上下文（主动上下文）UI 开关。
 * 上下文图谱 + 上下文内容 + 上下文设置三个面板的统一可插拔门控；
 * false 时从侧边栏与主区移除全部个人上下文入口。
 */
export const FEATURE_PERSONAL_CONTEXT_UI = true;
