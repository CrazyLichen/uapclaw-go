/**
 * ThinkingBlock 思考过程块（reasoning_content）。
 *
 * 对齐 Python TUI 的 kind:"thinking" entry（event-handlers.ts handleReasoning →
 * appendThinkingChunk）：与正文、工具卡片并列的独立条目，而不是塞进助手气泡。
 *
 * 交互：
 *   - 流式期间自动展开，并跟随最新思考内容
 *   - 思考结束（正文或工具开始）自动折叠，只留一行摘要，点击标题可再次展开
 */

import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';

interface ThinkingBlockProps {
  // content 思考文本（reasoning_content 累积结果）
  content: string;
  // isStreaming 该段思考是否仍在输出
  isStreaming: boolean;
}

// 折叠态摘要的最大字符数
const THINKING_SUMMARY_MAX = 60;

export function ThinkingBlock({ content, isStreaming }: ThinkingBlockProps) {
  const { t } = useTranslation();
  const [expanded, setExpanded] = useState(isStreaming);

  useEffect(() => {
    setExpanded(isStreaming);
  }, [isStreaming]);

  if (!content) {
    return null;
  }

  const summary = content.replace(/\s+/g, ' ').slice(0, THINKING_SUMMARY_MAX);

  return (
    <div className="my-1 animate-fade-in" data-testid="chat-thinking-block">
      <button
        type="button"
        onClick={() => setExpanded((prev) => !prev)}
        aria-expanded={expanded}
        className="flex items-center gap-1.5 text-xs text-text-muted hover:text-text transition-colors"
        data-testid="chat-thinking-toggle"
      >
        <span aria-hidden="true">{expanded ? '▼' : '▶'}</span>
        <span>{isStreaming ? t('chatUi.thinking.streaming') : t('chatUi.thinking.done')}</span>
        {!expanded && <span className="truncate max-w-[280px] opacity-70">{summary}</span>}
      </button>
      {expanded && (
        <div
          className="mt-1 pl-3 border-l-2 border-border text-sm text-text-muted whitespace-pre-wrap break-words"
          data-testid="chat-thinking-body"
        >
          {content}
        </div>
      )}
    </div>
  );
}
