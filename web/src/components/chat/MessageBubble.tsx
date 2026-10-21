import { useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Bot, User, Clock, Zap, Activity, ChevronDown, ChevronUp } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { ToolCallCard } from './ToolCallCard';
import { TurnTraceTimeline } from './TurnTraceTimeline';
import { type Message, type ToolCall } from '@/lib/api';
import { formatTime, cn } from '@/lib/utils';

export interface MessageBubbleProps {
  message: Message;
  toolResultsMap?: Record<string, { result?: unknown; error?: string; durationMs?: number }>;
  isStreaming?: boolean;
  conversationId?: string;
  orphanTool?: boolean;
}

export function MessageBubble({
  message,
  toolResultsMap = {},
  isStreaming = false,
  conversationId,
  orphanTool = false,
}: MessageBubbleProps) {
  const [showTrace, setShowTrace] = useState(false);
  const isUser = message.role === 'user';
  const isTool = message.role === 'tool';
  const isAssistant = message.role === 'assistant';

  if (isTool) {
    // Tool results are shown inside the assistant's tool call card;
    // only orphan tool messages (no matching call) render standalone.
    if (message.tool_call_id && toolResultsMap[message.tool_call_id] && !orphanTool) return null;
    return (
      <div className="py-2 px-3 sm:px-6 flex justify-start">
        <div className="max-w-2xl w-full">
          <ToolCallCard
            id={message.tool_call_id}
            name={message.name || 'tool'}
            result={message.content}
          />
        </div>
      </div>
    );
  }

  return (
    <div
      className={cn(
        'group flex gap-3 px-3 sm:px-6 py-3 transition-colors',
        isUser ? 'flex-row-reverse' : 'flex-row'
      )}
    >
      {/* Avatar */}
      <div
        className={cn(
          'w-8 h-8 rounded-lg flex items-center justify-center shrink-0 text-xs font-semibold select-none shadow-sm',
          isUser
            ? 'bg-primary text-primary-foreground'
            : 'bg-gradient-to-br from-indigo-500 to-purple-600 text-white'
        )}
      >
        {isUser ? <User className="w-4 h-4" /> : <Bot className="w-4 h-4" />}
      </div>

      {/* Message Content Container */}
      <div
        className={cn(
          'flex flex-col min-w-0 max-w-[85%] sm:max-w-[78%]',
          isUser ? 'items-end' : 'items-start'
        )}
      >
        {/* Header meta */}
        <div className="flex items-center gap-2 mb-1 text-[11px] text-muted-foreground select-none">
          <span className="font-medium">
            {isUser ? '用户' : '小爱 AI 伴侣 (Agent)'}
          </span>
          {message.created_at && (
            <span>{formatTime(message.created_at)}</span>
          )}
        </div>

        {/* Bubble */}
        <div
          className={cn(
            'rounded-2xl px-4 py-3 text-sm leading-relaxed shadow-sm break-words',
            isUser
              ? 'bg-primary text-primary-foreground rounded-tr-sm'
              : 'bg-card border border-border/80 text-foreground rounded-tl-sm'
          )}
        >
          {/* Tool Calls inside assistant message */}
          {message.tool_calls && message.tool_calls.length > 0 && (
            <div className="mb-3 space-y-2">
              {message.tool_calls.map((tc: ToolCall) => {
                const res = toolResultsMap[tc.id];
                return (
                  <ToolCallCard
                    key={tc.id}
                    id={tc.id}
                    name={tc.name}
                    args={tc.arguments}
                    result={res?.result}
                    error={res?.error}
                    durationMs={res?.durationMs}
                    isRunning={!res && isStreaming}
                  />
                );
              })}
            </div>
          )}

          {/* Text Content */}
          {isUser ? (
            <div className="whitespace-pre-wrap">{message.content}</div>
          ) : (
            <div className="prose prose-sm dark:prose-invert max-w-none text-sm leading-relaxed font-sans prose-p:my-1.5 prose-pre:my-2 prose-pre:bg-muted/80 prose-pre:text-foreground prose-code:text-primary prose-code:font-mono prose-code:bg-muted prose-code:px-1 prose-code:py-0.5 prose-code:rounded prose-code:text-xs">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>
                {message.content}
              </ReactMarkdown>
              {isStreaming && <span className="streaming-cursor" />}
            </div>
          )}
        </div>

        {/* Timings & Stats Badge for Assistant */}
        {isAssistant && (
          <div className="mt-1.5 flex items-center gap-2 select-none flex-wrap">
            {message.timings && (
              <Badge
                variant="secondary"
                className="h-5 text-[10px] gap-1 px-2 text-muted-foreground bg-muted/60 hover:bg-muted"
              >
                <Zap className="w-3 h-3 text-amber-500" />
                <span>首字 {message.timings.first_token_ms ?? 0}ms</span>
                <span className="opacity-40">|</span>
                <Clock className="w-3 h-3 text-primary" />
                <span>
                  耗时{' '}
                  {message.timings.total_ms
                    ? (message.timings.total_ms / 1000).toFixed(2) + 's'
                    : '0s'}
                </span>
              </Badge>
            )}

            {/* 追踪 (Trace) Toggle Button */}
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setShowTrace((prev) => !prev)}
              className="h-5 text-[10px] gap-1 px-1.5 text-muted-foreground hover:text-foreground hover:bg-muted/60 rounded"
              title="查看该轮回复的执行追踪与耗时瀑布流"
            >
              <Activity className="w-3 h-3 text-indigo-500" />
              <span>追踪</span>
              {showTrace ? (
                <ChevronUp className="w-2.5 h-2.5" />
              ) : (
                <ChevronDown className="w-2.5 h-2.5" />
              )}
            </Button>
          </div>
        )}

        {/* Turn Trace Timeline & Waterfall */}
        {isAssistant && showTrace && (
          <TurnTraceTimeline conversationId={conversationId} />
        )}
      </div>
    </div>
  );
}
