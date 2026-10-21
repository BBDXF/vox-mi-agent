import { useState, useEffect, useRef } from 'react';
import {
  Activity,
  Cpu,
  Wrench,
  CheckCircle2,
  AlertCircle,
  Clock,
  Zap,
  Code2,
  ChevronDown,
  ChevronRight,
  AlertTriangle,
  RefreshCw,
} from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  getConversationTrace,
  getTracePayload,
  type TraceTurn,
  type TraceStep,
  type TracePayloadResponse,
} from '@/lib/api';

interface TurnTraceTimelineProps {
  conversationId?: string;
  turnId?: string;
}

export function TurnTraceTimeline({ conversationId, turnId }: TurnTraceTimelineProps) {
  const [loading, setLoading] = useState(false);
  const [turn, setTurn] = useState<TraceTurn | null>(null);

  // Payload viewer state
  const [showPayload, setShowPayload] = useState(false);
  const [loadingPayload, setLoadingPayload] = useState(false);
  const [payloadData, setPayloadData] = useState<TracePayloadResponse | null>(null);
  const [expandedPayloadStep, setExpandedPayloadStep] = useState<number | null>(null);

  const isMountedRef = useRef(true);

  useEffect(() => {
    isMountedRef.current = true;
    if (!conversationId) return;

    setLoading(true);
    getConversationTrace(conversationId)
      .then((res) => {
        if (!isMountedRef.current) return;
        if (res.turns && res.turns.length > 0) {
          if (turnId) {
            const found = res.turns.find((t) => t.turn_id === turnId);
            setTurn(found || res.turns[0]);
          } else {
            // Default to first/latest turn
            setTurn(res.turns[0]);
          }
        }
      })
      .catch(() => {})
      .finally(() => {
        if (isMountedRef.current) setLoading(false);
      });

    return () => {
      isMountedRef.current = false;
    };
  }, [conversationId, turnId]);

  const handleTogglePayload = async () => {
    if (showPayload) {
      setShowPayload(false);
      return;
    }

    setShowPayload(true);
    if (!payloadData && turn) {
      setLoadingPayload(true);
      try {
        const data = await getTracePayload(turn.turn_id);
        if (isMountedRef.current) {
          setPayloadData(data);
          if (data.items && data.items.length > 0) {
            setExpandedPayloadStep(0);
          }
        }
      } catch {
        if (isMountedRef.current) {
          setPayloadData({ available: false, items: [] });
        }
      } finally {
        if (isMountedRef.current) setLoadingPayload(false);
      }
    }
  };

  if (loading) {
    return (
      <div className="mt-2 p-3 rounded-lg border bg-muted/30 text-xs text-muted-foreground flex items-center gap-2">
        <RefreshCw className="w-3.5 h-3.5 animate-spin text-primary" />
        <span>正在加载调用链追踪数据...</span>
      </div>
    );
  }

  if (!turn || !turn.steps || turn.steps.length === 0) {
    return (
      <div className="mt-2 p-3 rounded-lg border bg-muted/20 text-xs text-muted-foreground">
        暂无当前回复的执行追踪快照记录
      </div>
    );
  }

  // Calculate waterfall metrics
  const totalMs = Math.max(
    1,
    turn.duration_ms || turn.steps.reduce((acc, s) => acc + (s.duration_ms || 0), 0)
  );

  return (
    <div className="mt-2.5 p-3.5 sm:p-4 rounded-xl border border-indigo-500/20 bg-card/90 shadow-sm space-y-4 text-xs w-full max-w-2xl animate-in fade-in duration-200">
      {/* Header bar */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2 pb-2 border-b">
        <div className="flex items-center gap-2">
          <Activity className="w-4 h-4 text-indigo-500" />
          <span className="font-semibold text-foreground">
            单轮意图调用链追踪 (Trace)
          </span>
          <Badge
            variant={turn.status === 'ok' ? 'success' : 'destructive'}
            className="text-[10px] py-0 px-1.5 font-mono"
          >
            {turn.status.toUpperCase()}
          </Badge>
        </div>

        <div className="flex items-center gap-3 text-muted-foreground font-mono text-[11px]">
          <span className="flex items-center gap-1">
            <Zap className="w-3 h-3 text-amber-500" />
            首字: {turn.first_token_ms}ms
          </span>
          <span className="flex items-center gap-1">
            <Clock className="w-3 h-3 text-primary" />
            总计: {(totalMs / 1000).toFixed(2)}s
          </span>
        </div>
      </div>

      {/* 1. Waterfall Bar (Relative Durations) */}
      <div className="space-y-1.5">
        <div className="text-[11px] font-medium text-muted-foreground flex items-center justify-between">
          <span>调用时序与相对耗时占比 (Waterfall)</span>
          <span className="font-mono text-[10px]">{totalMs}ms</span>
        </div>
        <div className="h-3 w-full bg-muted rounded-full overflow-hidden flex shadow-inner">
          {turn.steps.map((st: TraceStep, i: number) => {
            const pct = Math.max(4, Math.min(100, ((st.duration_ms || 1) / totalMs) * 100));
            const isLLM = st.kind === 'llm';
            const isErr = st.status === 'error';

            return (
              <div
                key={st.id || i}
                style={{ width: `${pct}%` }}
                className={`h-full border-r border-background/60 last:border-r-0 transition-opacity hover:opacity-80 relative group ${
                  isErr
                    ? 'bg-destructive'
                    : isLLM
                    ? 'bg-indigo-500'
                    : 'bg-amber-500'
                }`}
                title={`${st.name} (${st.duration_ms}ms, ${pct.toFixed(1)}%)`}
              />
            );
          })}
        </div>
        <div className="flex items-center gap-4 text-[10px] text-muted-foreground font-mono pt-0.5">
          <span className="flex items-center gap-1">
            <span className="w-2 h-2 rounded-full bg-indigo-500" />
            LLM 推理
          </span>
          <span className="flex items-center gap-1">
            <span className="w-2 h-2 rounded-full bg-amber-500" />
            工具调用
          </span>
          <span className="flex items-center gap-1">
            <span className="w-2 h-2 rounded-full bg-destructive" />
            异常节点
          </span>
        </div>
      </div>

      {/* 2. Vertical Timeline */}
      <div className="space-y-3 pt-2">
        <div className="text-[11px] font-medium text-muted-foreground">
          执行阶段时序明细 (Timeline)
        </div>
        <div className="relative pl-6 space-y-4 before:absolute before:left-2.5 before:top-2 before:bottom-2 before:w-0.5 before:bg-border">
          {turn.steps.map((step: TraceStep, idx: number) => {
            const isLLM = step.kind === 'llm';
            const isErr = step.status === 'error';

            return (
              <div key={step.id || idx} className="relative group">
                {/* Node Bullet Icon */}
                <div
                  className={`absolute -left-6 top-0.5 w-5 h-5 rounded-full border flex items-center justify-center text-[10px] shrink-0 bg-background ${
                    isErr
                      ? 'border-destructive text-destructive bg-destructive/10'
                      : isLLM
                      ? 'border-indigo-500 text-indigo-500 bg-indigo-500/10'
                      : 'border-amber-500 text-amber-500 bg-amber-500/10'
                  }`}
                >
                  {isLLM ? <Cpu className="w-3 h-3" /> : <Wrench className="w-3 h-3" />}
                </div>

                {/* Step Details Box */}
                <div
                  className={`p-2.5 rounded-lg border text-xs space-y-1.5 transition-colors ${
                    isErr
                      ? 'border-destructive/40 bg-destructive/5'
                      : 'bg-muted/20 border-border/70 hover:border-border'
                  }`}
                >
                  {/* Title row */}
                  <div className="flex items-center justify-between gap-2">
                    <div className="flex items-center gap-2">
                      <span className="font-mono font-bold text-foreground">
                        {step.name}
                      </span>
                      <Badge variant="outline" className="text-[10px] py-0 px-1 font-mono uppercase">
                        {step.kind}
                      </Badge>
                    </div>

                    <div className="flex items-center gap-1.5 font-mono text-[11px]">
                      {isErr ? (
                        <span className="text-destructive font-semibold flex items-center gap-1">
                          <AlertCircle className="w-3 h-3" />
                          失败 ({step.duration_ms}ms)
                        </span>
                      ) : (
                        <span className="text-muted-foreground flex items-center gap-1">
                          <CheckCircle2 className="w-3 h-3 text-emerald-500" />
                          {step.duration_ms}ms
                        </span>
                      )}
                    </div>
                  </div>

                  {/* Step metadata */}
                  {isLLM && (
                    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-muted-foreground font-mono">
                      {step.first_token_ms !== undefined && (
                        <span>首字延迟: {step.first_token_ms}ms</span>
                      )}
                      {step.prompt_tokens !== undefined && (
                        <span>输入: {step.prompt_tokens} tokens</span>
                      )}
                      {step.completion_tokens !== undefined && (
                        <span>输出: {step.completion_tokens} tokens</span>
                      )}
                      {step.finish_reason && (
                        <span className="text-foreground">
                          结束原因: {step.finish_reason}
                        </span>
                      )}
                    </div>
                  )}

                  {/* Tool metadata / summary */}
                  {!isLLM && (
                    <div className="space-y-1 text-[11px]">
                      {step.args && (
                        <div className="font-mono text-muted-foreground truncate bg-muted/40 px-2 py-0.5 rounded">
                          参数: {typeof step.args === 'string' ? step.args : JSON.stringify(step.args)}
                        </div>
                      )}
                      {step.result_summary && (
                        <div className="text-foreground/90 font-mono">
                          {step.result_summary}
                        </div>
                      )}
                    </div>
                  )}

                  {/* Error display */}
                  {step.error && (
                    <div className="text-[11px] text-destructive bg-destructive/10 border border-destructive/20 p-2 rounded font-mono">
                      {step.error}
                    </div>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      </div>

      {/* 3. Button "查看原文" & Payload Viewer */}
      <div className="pt-2 border-t space-y-3">
        <div className="flex items-center justify-between">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={handleTogglePayload}
            className="gap-1.5 text-xs h-7.5"
          >
            <Code2 className="w-3.5 h-3.5 text-primary" />
            {showPayload ? '收起原文报文' : '查看原文 (Payload)'}
          </Button>

          {showPayload && (
            <span className="text-[11px] text-muted-foreground font-mono">
              原始请求与工具响应快照
            </span>
          )}
        </div>

        {/* Payload Drawer / Content */}
        {showPayload && (
          <div className="rounded-lg border bg-muted/30 p-3 space-y-2.5 animate-in fade-in">
            {loadingPayload ? (
              <div className="py-4 flex items-center justify-center gap-2 text-muted-foreground text-xs">
                <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                <span>正在提取原始报文快照...</span>
              </div>
            ) : payloadData?.available === false ? (
              <div className="p-3 bg-amber-500/10 border border-amber-500/25 rounded-md text-amber-700 dark:text-amber-300 text-xs flex items-center gap-2">
                <AlertTriangle className="w-4 h-4 text-amber-500 shrink-0" />
                <span>未开启调试记录（设置 → 日志 → 调试模式）</span>
              </div>
            ) : payloadData?.items && payloadData.items.length > 0 ? (
              <div className="space-y-2">
                {payloadData.items.map((item, idx) => {
                  const isExpanded = expandedPayloadStep === idx;
                  return (
                    <div
                      key={idx}
                      className="border rounded-md bg-background/80 overflow-hidden"
                    >
                      <button
                        type="button"
                        onClick={() =>
                          setExpandedPayloadStep(isExpanded ? null : idx)
                        }
                        className="w-full px-3 py-2 text-left flex items-center justify-between hover:bg-muted/40 transition-colors"
                      >
                        <div className="flex items-center gap-2 font-mono text-xs">
                          {isExpanded ? (
                            <ChevronDown className="w-3.5 h-3.5 text-muted-foreground" />
                          ) : (
                            <ChevronRight className="w-3.5 h-3.5 text-muted-foreground" />
                          )}
                          <Badge variant="outline" className="text-[10px] py-0 px-1 uppercase">
                            Step {item.step} ({item.kind})
                          </Badge>
                          <span className="text-foreground font-semibold">
                            {item.tool || (item.kind === 'llm' ? 'LLM Request/Response' : 'Tool Result')}
                          </span>
                        </div>
                        <span className="text-[10px] text-muted-foreground font-mono">
                          {item.ts ? new Date(item.ts).toLocaleTimeString() : ''}
                        </span>
                      </button>

                      {isExpanded && (
                        <div className="p-3 border-t bg-muted/20 space-y-2 overflow-x-auto text-[11px] font-mono">
                          {item.request && (
                            <div>
                              <div className="text-[10px] font-bold text-muted-foreground uppercase mb-1">
                                [Request Payload]
                              </div>
                              <pre className="p-2 rounded bg-muted/60 text-foreground overflow-x-auto max-h-48 whitespace-pre-wrap">
                                {JSON.stringify(item.request, null, 2)}
                              </pre>
                            </div>
                          )}
                          {item.response && (
                            <div>
                              <div className="text-[10px] font-bold text-muted-foreground uppercase mb-1">
                                [Response Payload]
                              </div>
                              <pre className="p-2 rounded bg-muted/60 text-foreground overflow-x-auto max-h-48 whitespace-pre-wrap">
                                {JSON.stringify(item.response, null, 2)}
                              </pre>
                            </div>
                          )}
                          {item.args && (
                            <div>
                              <div className="text-[10px] font-bold text-muted-foreground uppercase mb-1">
                                [Tool Arguments]
                              </div>
                              <pre className="p-2 rounded bg-muted/60 text-foreground overflow-x-auto max-h-36 whitespace-pre-wrap">
                                {JSON.stringify(item.args, null, 2)}
                              </pre>
                            </div>
                          )}
                          {item.result && (
                            <div>
                              <div className="text-[10px] font-bold text-muted-foreground uppercase mb-1">
                                [Tool Execution Result]
                              </div>
                              <pre className="p-2 rounded bg-muted/60 text-foreground overflow-x-auto max-h-48 whitespace-pre-wrap">
                                {JSON.stringify(item.result, null, 2)}
                              </pre>
                            </div>
                          )}
                        </div>
                      )}
                    </div>
                  );
                })}
              </div>
            ) : (
              <div className="text-center py-3 text-muted-foreground text-xs">
                暂无报文细节
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}