import { useState } from 'react';
import { ChevronDown, ChevronRight, Wrench, CheckCircle2, AlertCircle, Copy, Check } from 'lucide-react';
import { Badge } from '@/components/ui/badge';

export interface ToolCallCardProps {
  id?: string;
  name: string;
  args?: string | Record<string, unknown>;
  result?: unknown;
  error?: string;
  isRunning?: boolean;
  durationMs?: number;
}

export function ToolCallCard({
  id,
  name,
  args,
  result,
  error,
  isRunning = false,
  durationMs,
}: ToolCallCardProps) {
  const [expanded, setExpanded] = useState(false);
  const [copied, setCopied] = useState(false);

  // Format arguments
  let argsFormatted = '';
  if (typeof args === 'string') {
    try {
      const parsed = JSON.parse(args);
      argsFormatted = JSON.stringify(parsed, null, 2);
    } catch {
      argsFormatted = args;
    }
  } else if (args !== undefined) {
    argsFormatted = JSON.stringify(args, null, 2);
  }

  // Format result
  let resultFormatted = '';
  if (typeof result === 'string') {
    try {
      const parsed = JSON.parse(result);
      resultFormatted = JSON.stringify(parsed, null, 2);
    } catch {
      resultFormatted = result;
    }
  } else if (result !== undefined) {
    resultFormatted = JSON.stringify(result, null, 2);
  }

  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation();
    const payload = JSON.stringify(
      {
        tool: name,
        tool_call_id: id,
        arguments: args,
        result: result,
        error: error,
      },
      null,
      2
    );
    navigator.clipboard.writeText(payload);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="my-2.5 rounded-lg border border-border/80 bg-muted/30 overflow-hidden text-xs shadow-sm transition-all duration-150">
      {/* Header bar */}
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center justify-between px-3 py-2 bg-muted/50 hover:bg-muted/70 transition-colors text-left"
      >
        <div className="flex items-center gap-2 min-w-0">
          <span className="text-muted-foreground">
            {expanded ? (
              <ChevronDown className="w-3.5 h-3.5" />
            ) : (
              <ChevronRight className="w-3.5 h-3.5" />
            )}
          </span>
          <Wrench className="w-3.5 h-3.5 text-primary shrink-0" />
          <span className="font-mono font-medium text-foreground tracking-tight">
            {name}
          </span>
          {id && (
            <span className="text-[10px] text-muted-foreground font-mono truncate max-w-[120px]">
              #{id.slice(-8)}
            </span>
          )}
        </div>

        <div className="flex items-center gap-2 shrink-0">
          {durationMs !== undefined && !isRunning && (
            <span className="text-[10px] font-mono text-muted-foreground">
              {durationMs >= 1000 ? (durationMs / 1000).toFixed(2) + 's' : Math.round(durationMs) + 'ms'}
            </span>
          )}
          {isRunning ? (
            <Badge variant="warning" className="h-5 text-[10px] animate-pulse">
              运行中
            </Badge>
          ) : error ? (
            <Badge variant="destructive" className="h-5 text-[10px] gap-1">
              <AlertCircle className="w-3 h-3" />
              失败
            </Badge>
          ) : result !== undefined ? (
            <Badge variant="success" className="h-5 text-[10px] gap-1">
              <CheckCircle2 className="w-3 h-3" />
              成功
            </Badge>
          ) : (
            <Badge variant="secondary" className="h-5 text-[10px]">
              调用
            </Badge>
          )}

          <button
            type="button"
            onClick={handleCopy}
            title="复制工具调用数据"
            className="p-1 rounded text-muted-foreground hover:text-foreground hover:bg-background/80 transition-colors"
          >
            {copied ? (
              <Check className="w-3.5 h-3.5 text-emerald-500" />
            ) : (
              <Copy className="w-3.5 h-3.5" />
            )}
          </button>
        </div>
      </button>

      {/* Collapsible Content */}
      {expanded && (
        <div className="p-3 space-y-2.5 font-mono bg-background/50 border-t border-border/50">
          {/* Input Args */}
          <div>
            <div className="text-[10px] uppercase font-semibold text-muted-foreground tracking-wider mb-1 flex items-center gap-1.5">
              <span>输入参数 (Arguments)</span>
            </div>
            <pre className="bg-muted/60 text-muted-foreground p-2 rounded text-[11px] leading-relaxed overflow-x-auto whitespace-pre-wrap max-h-48 scrollbar-thin">
              {argsFormatted || '(无输入参数)'}
            </pre>
          </div>

          {/* Result or Error */}
          {error ? (
            <div>
              <div className="text-[10px] uppercase font-semibold text-destructive tracking-wider mb-1 flex items-center gap-1.5">
                <AlertCircle className="w-3 h-3" />
                <span>错误输出 (Error)</span>
              </div>
              <pre className="bg-destructive/10 text-destructive border border-destructive/20 p-2 rounded text-[11px] leading-relaxed overflow-x-auto whitespace-pre-wrap max-h-48 scrollbar-thin">
                {error}
              </pre>
            </div>
          ) : result !== undefined ? (
            <div>
              <div className="text-[10px] uppercase font-semibold text-emerald-600 dark:text-emerald-400 tracking-wider mb-1 flex items-center gap-1.5">
                <CheckCircle2 className="w-3 h-3" />
                <span>返回结果 (Result)</span>
              </div>
              <pre className="bg-muted/60 text-emerald-700 dark:text-emerald-300 p-2 rounded text-[11px] leading-relaxed overflow-x-auto whitespace-pre-wrap max-h-56 scrollbar-thin">
                {resultFormatted}
              </pre>
            </div>
          ) : isRunning ? (
            <div className="text-[11px] text-muted-foreground py-1 flex items-center gap-2 italic">
              <span className="w-1.5 h-1.5 rounded-full bg-primary animate-ping" />
              正在等待工具执行返回...
            </div>
          ) : null}
        </div>
      )}
    </div>
  );
}
