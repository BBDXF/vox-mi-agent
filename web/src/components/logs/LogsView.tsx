import { useState, useEffect, useRef, useCallback } from 'react';
import {
  ScrollText,
  RefreshCw,
  CheckCircle2,
  AlertCircle,
  Clock,
  Zap,
  Cpu,
  Wrench,
  Bot,
  MessageSquare,
  ArrowRight,
  Code2,
  X,
  AlertTriangle,
  ChevronRight,
  ChevronDown,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { useToast } from '@/components/ui/toast';
import {
  getTraces,
  getTracePayload,
  type TraceItem,
  type TracePayloadResponse,
} from '@/lib/api';
import { formatTime } from '@/lib/utils';

export interface LogsViewProps {
  onNavigateToConversation?: (conversationId: string) => void;
}

export function LogsView({ onNavigateToConversation }: LogsViewProps) {
  const { error: toastError } = useToast();

  const [traces, setTraces] = useState<TraceItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [nextBeforeId, setNextBeforeId] = useState<string | undefined>(undefined);

  // Filters
  const [statusFilter, setStatusFilter] = useState<string>('all');
  const [kindFilter, setKindFilter] = useState<string>('all');
  const [nameFilter, setNameFilter] = useState<string>('');
  const [timeRangeFilter, setTimeRangeFilter] = useState<string>('all');
  const [convFilter, setConvFilter] = useState<string>('');

  // 5s Auto refresh
  const [autoRefresh, setAutoRefresh] = useState(false);
  const autoRefreshTimerRef = useRef<number | null>(null);

  // Selected row for Drawer
  const [selectedTrace, setSelectedTrace] = useState<TraceItem | null>(null);

  // Drawer payload state
  const [showPayload, setShowPayload] = useState(false);
  const [loadingPayload, setLoadingPayload] = useState(false);
  const [payloadData, setPayloadData] = useState<TracePayloadResponse | null>(null);
  const [expandedPayloadIdx, setExpandedPayloadIdx] = useState<number | null>(null);

  const isMountedRef = useRef(true);

  // Fetch initial or filtered traces
  const fetchTracesList = useCallback(
    async (isBackground = false) => {
      if (!isBackground) setLoading(true);

      const params: any = { limit: 50 };
      if (statusFilter !== 'all') params.status = statusFilter;
      if (kindFilter !== 'all') params.kind = kindFilter;
      if (nameFilter.trim()) params.name = nameFilter.trim();
      if (convFilter.trim()) params.conversation_id = convFilter.trim();

      if (timeRangeFilter !== 'all') {
        const now = Date.now();
        if (timeRangeFilter === '1h') {
          params.since = new Date(now - 3600000).toISOString();
        } else if (timeRangeFilter === '24h') {
          params.since = new Date(now - 86400000).toISOString();
        } else if (timeRangeFilter === '7d') {
          params.since = new Date(now - 86400000 * 7).toISOString();
        }
      }

      try {
        const res = await getTraces(params);
        if (!isMountedRef.current) return;
        setTraces(res.items || []);
        setNextBeforeId(res.next_before_id);
      } catch (err: any) {
        if (!isMountedRef.current) return;
        if (!isBackground) toastError(err.message || '获取执行日志失败');
      } finally {
        if (isMountedRef.current && !isBackground) setLoading(false);
      }
    },
    [statusFilter, kindFilter, nameFilter, timeRangeFilter, convFilter, toastError]
  );

  // Effect on filter changes
  useEffect(() => {
    isMountedRef.current = true;
    fetchTracesList();
    return () => {
      isMountedRef.current = false;
    };
  }, [fetchTracesList]);

  // Handle 5s auto refresh
  useEffect(() => {
    if (autoRefresh) {
      autoRefreshTimerRef.current = window.setInterval(() => {
        fetchTracesList(true);
      }, 5000);
    } else {
      if (autoRefreshTimerRef.current) {
        clearInterval(autoRefreshTimerRef.current);
        autoRefreshTimerRef.current = null;
      }
    }

    return () => {
      if (autoRefreshTimerRef.current) {
        clearInterval(autoRefreshTimerRef.current);
        autoRefreshTimerRef.current = null;
      }
    };
  }, [autoRefresh, fetchTracesList]);

  // Load more via before_id
  const handleLoadMore = async () => {
    if (!nextBeforeId || loadingMore) return;
    setLoadingMore(true);

    const params: any = { limit: 50, before_id: nextBeforeId };
    if (statusFilter !== 'all') params.status = statusFilter;
    if (kindFilter !== 'all') params.kind = kindFilter;
    if (nameFilter.trim()) params.name = nameFilter.trim();
    if (convFilter.trim()) params.conversation_id = convFilter.trim();

    try {
      const res = await getTraces(params);
      if (!isMountedRef.current) return;
      setTraces((prev) => [...prev, ...(res.items || [])]);
      setNextBeforeId(res.next_before_id);
    } catch (err: any) {
      if (!isMountedRef.current) return;
      toastError(err.message || '加载更多记录失败');
    } finally {
      if (isMountedRef.current) setLoadingMore(false);
    }
  };

  // Reset filters
  const handleResetFilters = () => {
    setStatusFilter('all');
    setKindFilter('all');
    setNameFilter('');
    setTimeRangeFilter('all');
    setConvFilter('');
  };

  // Drawer payload fetch
  const handleFetchPayload = async (turnId: string) => {
    if (showPayload) {
      setShowPayload(false);
      return;
    }
    setShowPayload(true);
    setLoadingPayload(true);
    try {
      const data = await getTracePayload(turnId);
      if (isMountedRef.current) {
        setPayloadData(data);
        if (data.items && data.items.length > 0) {
          setExpandedPayloadIdx(0);
        }
      }
    } catch {
      if (isMountedRef.current) {
        setPayloadData({ available: false, items: [] });
      }
    } finally {
      if (isMountedRef.current) setLoadingPayload(false);
    }
  };

  const handleRowClick = (item: TraceItem) => {
    setSelectedTrace(item);
    setShowPayload(false);
    setPayloadData(null);
  };

  return (
    <div className="h-full flex flex-col overflow-hidden bg-background">
      {/* Header */}
      <header className="px-4 md:px-8 py-3.5 border-b bg-card/60 backdrop-blur shrink-0 flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div>
          <h1 className="text-lg font-bold tracking-tight text-foreground flex items-center gap-2">
            <ScrollText className="w-5 h-5 text-primary" />
            运行日志与调用追踪 (Trace Logs)
          </h1>
          <p className="text-xs text-muted-foreground mt-0.5">
            全链路审计 Agent 意图拆解、LLM 推理吞吐、外部工具响应延迟与系统异常
          </p>
        </div>

        <div className="flex items-center gap-2 shrink-0">
          <Button
            variant={autoRefresh ? 'default' : 'outline'}
            size="sm"
            onClick={() => setAutoRefresh(!autoRefresh)}
            className="gap-1.5 text-xs h-8"
          >
            <RefreshCw
              className={`w-3.5 h-3.5 ${autoRefresh ? 'animate-spin' : ''}`}
            />
            {autoRefresh ? '自动刷新 (5s 开启)' : '自动刷新 (5s)'}
          </Button>

          <Button
            variant="outline"
            size="sm"
            onClick={() => fetchTracesList(false)}
            disabled={loading}
            className="gap-1.5 text-xs h-8"
          >
            <RefreshCw
              className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`}
            />
            刷新
          </Button>
        </div>
      </header>

      {/* Filter Toolbar */}
      <div className="px-4 md:px-8 py-3 border-b bg-muted/20 shrink-0 space-y-2">
        <div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-5 gap-2 text-xs">
          {/* Status filter */}
          <div>
            <select
              value={statusFilter}
              onChange={(e) => setStatusFilter(e.target.value)}
              className="w-full h-8 rounded-md border border-input bg-background px-2 font-medium"
            >
              <option value="all">全部状态 (Status)</option>
              <option value="ok">正常 (ok)</option>
              <option value="error">异常 (error)</option>
              <option value="canceled">取消 (canceled)</option>
            </select>
          </div>

          {/* Kind filter */}
          <div>
            <select
              value={kindFilter}
              onChange={(e) => setKindFilter(e.target.value)}
              className="w-full h-8 rounded-md border border-input bg-background px-2 font-medium"
            >
              <option value="all">全部阶段 (Kind)</option>
              <option value="turn">意图轮次 (turn)</option>
              <option value="llm">大模型推理 (llm)</option>
              <option value="tool">工具调用 (tool)</option>
            </select>
          </div>

          {/* Time range filter */}
          <div>
            <select
              value={timeRangeFilter}
              onChange={(e) => setTimeRangeFilter(e.target.value)}
              className="w-full h-8 rounded-md border border-input bg-background px-2 font-medium"
            >
              <option value="all">全部时间 (Time Range)</option>
              <option value="1h">最近 1 小时</option>
              <option value="24h">最近 24 小时</option>
              <option value="7d">最近 7 天</option>
            </select>
          </div>

          {/* Name filter */}
          <div>
            <Input
              value={nameFilter}
              onChange={(e) => setNameFilter(e.target.value)}
              placeholder="搜索模型/工具名..."
              className="h-8 text-xs font-mono bg-background"
            />
          </div>

          {/* Conversation filter */}
          <div className="flex items-center gap-1">
            <Input
              value={convFilter}
              onChange={(e) => setConvFilter(e.target.value)}
              placeholder="对话 ID (conv_...)"
              className="h-8 text-xs font-mono bg-background flex-1"
            />
            {(statusFilter !== 'all' ||
              kindFilter !== 'all' ||
              nameFilter ||
              timeRangeFilter !== 'all' ||
              convFilter) && (
              <Button
                variant="ghost"
                size="sm"
                onClick={handleResetFilters}
                className="h-8 px-2 text-xs text-muted-foreground hover:text-foreground"
                title="清空筛选"
              >
                <X className="w-3.5 h-3.5" />
              </Button>
            )}
          </div>
        </div>
      </div>

      {/* Main Table Area */}
      <div className="flex-1 overflow-y-auto p-4 md:p-8">
        <div className="max-w-6xl mx-auto space-y-4">
          <div className="border rounded-xl bg-card overflow-hidden shadow-xs">
            <table className="w-full text-left text-xs">
              <thead className="bg-muted/60 border-b text-muted-foreground select-none">
                <tr>
                  <th className="px-4 py-3 font-medium w-36">发生时间</th>
                  <th className="px-4 py-3 font-medium w-24">阶段</th>
                  <th className="px-4 py-3 font-medium">名称 / 工具</th>
                  <th className="px-4 py-3 font-medium w-24">状态</th>
                  <th className="px-4 py-3 font-medium w-28">耗时</th>
                  <th className="px-4 py-3 font-medium">细节摘要</th>
                  <th className="px-4 py-3 font-medium w-28 text-right">所属对话</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border/60">
                {loading && traces.length === 0 ? (
                  <tr>
                    <td colSpan={7} className="px-4 py-12 text-center text-muted-foreground">
                      <div className="flex flex-col items-center justify-center gap-2">
                        <RefreshCw className="w-5 h-5 animate-spin text-primary" />
                        <span>正在检索调用日志快照...</span>
                      </div>
                    </td>
                  </tr>
                ) : traces.length === 0 ? (
                  <tr>
                    <td colSpan={7} className="px-4 py-12 text-center text-muted-foreground">
                      暂无匹配的调用追踪记录
                    </td>
                  </tr>
                ) : (
                  traces.map((row) => {
                    const isErr = row.status === 'error' || Boolean(row.error);
                    const isSelected = selectedTrace?.id === row.id;

                    return (
                      <tr
                        key={row.id}
                        onClick={() => handleRowClick(row)}
                        className={`cursor-pointer transition-colors select-none ${
                          isSelected
                            ? 'bg-primary/10'
                            : isErr
                            ? 'bg-destructive/10 hover:bg-destructive/15 border-l-4 border-l-destructive'
                            : 'hover:bg-muted/40'
                        }`}
                      >
                        {/* Time */}
                        <td className="px-4 py-3 font-mono text-muted-foreground whitespace-nowrap">
                          {formatTime(row.ts)}
                        </td>

                        {/* Kind */}
                        <td className="px-4 py-3">
                          <Badge
                            variant={
                              row.kind === 'llm'
                                ? 'default'
                                : row.kind === 'tool'
                                ? 'secondary'
                                : 'outline'
                            }
                            className="font-mono text-[10px] py-0 uppercase"
                          >
                            {row.kind}
                          </Badge>
                        </td>

                        {/* Name */}
                        <td className="px-4 py-3 font-mono font-semibold text-foreground">
                          <div className="flex items-center gap-1.5">
                            {row.kind === 'llm' ? (
                              <Bot className="w-3.5 h-3.5 text-indigo-500 shrink-0" />
                            ) : row.kind === 'tool' ? (
                              <Wrench className="w-3.5 h-3.5 text-amber-500 shrink-0" />
                            ) : (
                              <Cpu className="w-3.5 h-3.5 text-primary shrink-0" />
                            )}
                            <span className="truncate max-w-[160px]">{row.name}</span>
                          </div>
                        </td>

                        {/* Status */}
                        <td className="px-4 py-3">
                          {isErr ? (
                            <Badge variant="destructive" className="gap-1 text-[10px] py-0">
                              <AlertCircle className="w-3 h-3" />
                              异常
                            </Badge>
                          ) : (
                            <Badge variant="success" className="gap-1 text-[10px] py-0">
                              <CheckCircle2 className="w-3 h-3" />
                              正常
                            </Badge>
                          )}
                        </td>

                        {/* Duration */}
                        <td className="px-4 py-3 font-mono font-medium text-foreground">
                          {row.duration_ms}ms
                        </td>

                        {/* Detail / Summary */}
                        <td className="px-4 py-3 text-muted-foreground truncate max-w-xs">
                          {isErr ? (
                            <span className="text-destructive font-mono truncate block">
                              {row.error || '执行遇到错误'}
                            </span>
                          ) : row.kind === 'llm' ? (
                            <span className="font-mono text-[11px]">
                              {row.prompt_tokens ? `${row.prompt_tokens} in / ${row.completion_tokens} out` : '完成生成'}
                            </span>
                          ) : (
                            <span className="truncate block">
                              {row.result_summary || (row.args ? JSON.stringify(row.args) : '-')}
                            </span>
                          )}
                        </td>

                        {/* Conv ID */}
                        <td className="px-4 py-3 text-right font-mono text-[11px] text-muted-foreground">
                          {row.conversation_id ? row.conversation_id.slice(0, 8) : '-'}
                        </td>
                      </tr>
                    );
                  })
                )}
              </tbody>
            </table>
          </div>

          {/* Load More Button */}
          {nextBeforeId && (
            <div className="pt-2 text-center">
              <Button
                variant="outline"
                size="sm"
                onClick={handleLoadMore}
                disabled={loadingMore}
                className="gap-2 text-xs shadow-xs"
              >
                {loadingMore ? (
                  <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                ) : null}
                {loadingMore ? '正在加载更多...' : '加载更多历史日志'}
              </Button>
            </div>
          )}
        </div>
      </div>

      {/* Slide-over Detail Drawer */}
      {selectedTrace && (
        <div className="fixed inset-0 z-50 flex justify-end bg-background/50 backdrop-blur-xs animate-in fade-in duration-150">
          <div
            className="w-full max-w-xl h-full bg-card border-l shadow-2xl flex flex-col overflow-hidden animate-in slide-in-from-right duration-200"
            onClick={(e) => e.stopPropagation()}
          >
            {/* Drawer Header */}
            <div className="px-5 py-4 border-b bg-muted/20 flex items-center justify-between shrink-0">
              <div className="flex items-center gap-2 min-w-0">
                <ScrollText className="w-4 h-4 text-primary shrink-0" />
                <div className="min-w-0">
                  <h3 className="text-sm font-bold text-foreground truncate">
                    追踪节点详情: {selectedTrace.name}
                  </h3>
                  <div className="text-[10px] text-muted-foreground font-mono">
                    ID: {selectedTrace.id}
                  </div>
                </div>
              </div>

              <button
                type="button"
                onClick={() => setSelectedTrace(null)}
                className="text-muted-foreground hover:text-foreground rounded p-1.5 transition-colors"
              >
                <X className="w-4 h-4" />
              </button>
            </div>

            {/* Drawer Body */}
            <div className="flex-1 overflow-y-auto p-5 space-y-5 text-xs">
              {/* Badges row */}
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="outline" className="font-mono text-xs uppercase">
                  {selectedTrace.kind}
                </Badge>
                <Badge
                  variant={selectedTrace.status === 'error' ? 'destructive' : 'success'}
                  className="font-mono text-xs"
                >
                  {selectedTrace.status.toUpperCase()}
                </Badge>
                <Badge variant="secondary" className="font-mono text-xs gap-1">
                  <Clock className="w-3 h-3" />
                  {selectedTrace.duration_ms}ms
                </Badge>
                {selectedTrace.first_token_ms !== undefined && (
                  <Badge variant="secondary" className="font-mono text-xs gap-1">
                    <Zap className="w-3 h-3 text-amber-500" />
                    首字 {selectedTrace.first_token_ms}ms
                  </Badge>
                )}
              </div>

              {/* Node Basic Info */}
              <div className="space-y-2 p-3.5 rounded-lg border bg-muted/20 text-xs">
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">记录时间</span>
                  <span className="font-mono">{formatTime(selectedTrace.ts)}</span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">所属轮次 ID</span>
                  <span className="font-mono">{selectedTrace.turn_id}</span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">输入渠道</span>
                  <span className="font-mono">{selectedTrace.channel || 'web'}</span>
                </div>
              </div>

              {/* LLM Tokens */}
              {selectedTrace.kind === 'llm' && (
                <div className="p-3.5 rounded-lg border bg-muted/20 space-y-2">
                  <div className="font-semibold text-foreground">Token 计量与生成结果</div>
                  <div className="grid grid-cols-2 gap-2 font-mono text-[11px]">
                    <div>Prompt: {selectedTrace.prompt_tokens || 0} tokens</div>
                    <div>Completion: {selectedTrace.completion_tokens || 0} tokens</div>
                    <div>首字响应: {selectedTrace.first_token_ms || 0}ms</div>
                    <div>结束原因: {selectedTrace.finish_reason || 'stop'}</div>
                  </div>
                </div>
              )}

              {/* Tool Execution Details */}
              {selectedTrace.kind === 'tool' && (
                <div className="space-y-3">
                  {selectedTrace.args && (
                    <div className="space-y-1">
                      <div className="font-semibold text-foreground">调用参数 (Arguments):</div>
                      <pre className="p-2.5 rounded-lg border bg-muted/40 font-mono text-[11px] overflow-x-auto whitespace-pre-wrap">
                        {typeof selectedTrace.args === 'string'
                          ? selectedTrace.args
                          : JSON.stringify(selectedTrace.args, null, 2)}
                      </pre>
                    </div>
                  )}

                  {selectedTrace.result_summary && (
                    <div className="space-y-1">
                      <div className="font-semibold text-foreground">执行摘要 (Summary):</div>
                      <div className="p-2.5 rounded-lg border bg-muted/30 font-mono text-[11px]">
                        {selectedTrace.result_summary}
                      </div>
                    </div>
                  )}

                  {selectedTrace.error && (
                    <div className="space-y-1">
                      <div className="font-semibold text-destructive">错误堆栈 (Error):</div>
                      <div className="p-2.5 rounded-lg border border-destructive/30 bg-destructive/10 text-destructive font-mono text-[11px]">
                        {selectedTrace.error}
                      </div>
                    </div>
                  )}
                </div>
              )}

              {/* Link to Conversation */}
              <div className="p-3.5 rounded-lg border bg-primary/5 border-primary/20 flex flex-col sm:flex-row sm:items-center justify-between gap-3">
                <div className="space-y-0.5">
                  <div className="font-semibold text-foreground flex items-center gap-1.5">
                    <MessageSquare className="w-3.5 h-3.5 text-primary" />
                    关联历史对话
                  </div>
                  <div className="font-mono text-[10px] text-muted-foreground">
                    {selectedTrace.conversation_id}
                  </div>
                </div>

                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    if (onNavigateToConversation && selectedTrace.conversation_id) {
                      onNavigateToConversation(selectedTrace.conversation_id);
                    }
                  }}
                  className="gap-1.5 text-xs h-7.5 shrink-0"
                >
                  前往该对话
                  <ArrowRight className="w-3.5 h-3.5" />
                </Button>
              </div>

              {/* Payload viewer button & collapsible */}
              <div className="space-y-3 pt-2 border-t">
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => handleFetchPayload(selectedTrace.turn_id)}
                  className="w-full gap-2 text-xs"
                >
                  <Code2 className="w-3.5 h-3.5 text-primary" />
                  {showPayload ? '收起调试报文快照' : '查看原文 (Payload)'}
                </Button>

                {showPayload && (
                  <div className="p-3 rounded-lg border bg-muted/30 space-y-2 animate-in fade-in">
                    {loadingPayload ? (
                      <div className="py-4 text-center text-muted-foreground text-xs flex items-center justify-center gap-2">
                        <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                        <span>正在提取原始报文快照...</span>
                      </div>
                    ) : payloadData?.available === false ? (
                      <div className="p-3 bg-amber-500/10 border border-amber-500/25 rounded-md text-amber-700 dark:text-amber-300 text-xs flex items-center gap-2 font-sans">
                        <AlertTriangle className="w-4 h-4 text-amber-500 shrink-0" />
                        <span>未开启调试记录（设置 → 日志 → 调试模式）</span>
                      </div>
                    ) : payloadData?.items && payloadData.items.length > 0 ? (
                      <div className="space-y-2">
                        {payloadData.items.map((it, pIdx) => {
                          const isExp = expandedPayloadIdx === pIdx;
                          return (
                            <div key={pIdx} className="border rounded bg-background/80 overflow-hidden">
                              <button
                                type="button"
                                onClick={() =>
                                  setExpandedPayloadIdx(isExp ? null : pIdx)
                                }
                                className="w-full px-3 py-2 text-left flex items-center justify-between hover:bg-muted/40 font-mono text-xs"
                              >
                                <span className="font-semibold text-foreground flex items-center gap-1.5">
                                  {isExp ? (
                                    <ChevronDown className="w-3.5 h-3.5 text-muted-foreground" />
                                  ) : (
                                    <ChevronRight className="w-3.5 h-3.5 text-muted-foreground" />
                                  )}
                                  Step {it.step} ({it.kind})
                                </span>
                                <span className="text-[10px] text-muted-foreground">
                                  {it.tool || 'LLM Payload'}
                                </span>
                              </button>

                              {isExp && (
                                <div className="p-2.5 border-t bg-muted/20 space-y-2 text-[10px] font-mono">
                                  {it.request && (
                                    <div>
                                      <div className="text-muted-foreground font-bold uppercase mb-1">
                                        [Request]
                                      </div>
                                      <pre className="p-2 rounded bg-muted/60 overflow-x-auto whitespace-pre-wrap max-h-40">
                                        {JSON.stringify(it.request, null, 2)}
                                      </pre>
                                    </div>
                                  )}
                                  {it.response && (
                                    <div>
                                      <div className="text-muted-foreground font-bold uppercase mb-1">
                                        [Response]
                                      </div>
                                      <pre className="p-2 rounded bg-muted/60 overflow-x-auto whitespace-pre-wrap max-h-40">
                                        {JSON.stringify(it.response, null, 2)}
                                      </pre>
                                    </div>
                                  )}
                                  {it.args && (
                                    <div>
                                      <div className="text-muted-foreground font-bold uppercase mb-1">
                                        [Tool Args]
                                      </div>
                                      <pre className="p-2 rounded bg-muted/60 overflow-x-auto whitespace-pre-wrap max-h-32">
                                        {JSON.stringify(it.args, null, 2)}
                                      </pre>
                                    </div>
                                  )}
                                  {it.result && (
                                    <div>
                                      <div className="text-muted-foreground font-bold uppercase mb-1">
                                        [Tool Result]
                                      </div>
                                      <pre className="p-2 rounded bg-muted/60 overflow-x-auto whitespace-pre-wrap max-h-40">
                                        {JSON.stringify(it.result, null, 2)}
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
                      <div className="text-center py-2 text-muted-foreground text-xs">
                        暂无报文细节
                      </div>
                    )}
                  </div>
                )}
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}