import { useState, useEffect, useRef, useCallback } from 'react';
import {
  Plus,
  Send,
  Square,
  Radio,
  Monitor,
  RefreshCw,
  Sparkles,
  MessageSquare,
  Menu,
  X,
  Trash2,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';
import { Badge } from '@/components/ui/badge';
import { MessageBubble } from './MessageBubble';
import {
  type Conversation,
  type Message,
  type ToolCall,
  getConversations,
  createConversation,
  deleteConversation,
  deleteAllConversations,
  getMessages,
} from '@/lib/api';
import { streamChat } from '@/lib/sse';
import { useToast } from '@/components/ui/toast';
import { formatRelativeTime } from '@/lib/utils';

export interface ChatViewProps {
  targetConversationId?: string;
}

export function ChatView({ targetConversationId }: ChatViewProps = {}) {
  const { toast, error: toastError } = useToast();

  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [activeConvId, setActiveConvId] = useState<string | null>(targetConversationId || null);
  const [nextBefore, setNextBefore] = useState<string | undefined>(undefined);
  const [loadingList, setLoadingList] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);

  useEffect(() => {
    if (targetConversationId) {
      setActiveConvId(targetConversationId);
    }
  }, [targetConversationId]);

  const [messages, setMessages] = useState<Message[]>([]);
  const [loadingMessages, setLoadingMessages] = useState(false);
  const [toolResults, setToolResults] = useState<
    Record<string, { result?: unknown; error?: string; durationMs?: number }>
  >({});

  const [input, setInput] = useState('');
  const [isStreaming, setIsStreaming] = useState(false);
  const abortControllerRef = useRef<AbortController | null>(null);
  const toolStartRef = useRef<Record<string, number>>({});

  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  // Auto scroll to bottom
  const scrollToBottom = useCallback((smooth = true) => {
    messagesEndRef.current?.scrollIntoView({
      behavior: smooth ? 'smooth' : 'auto',
    });
  }, []);

  // Fetch initial conversations
  const fetchConversationsList = useCallback(async () => {
    setLoadingList(true);
    try {
      const res = await getConversations({ limit: 50 });
      setConversations(res.items);
      setNextBefore(res.next_before);
      if (res.items.length > 0 && !activeConvId) {
        setActiveConvId(res.items[0].id);
      }
    } catch (err: any) {
      toastError(err.message || '获取对话列表失败');
    } finally {
      setLoadingList(false);
    }
  }, [activeConvId, toastError]);

  useEffect(() => {
    fetchConversationsList();
  }, [fetchConversationsList]);

  // Load more conversations
  const handleLoadMore = async () => {
    if (!nextBefore || loadingMore) return;
    setLoadingMore(true);
    try {
      const res = await getConversations({ limit: 50, before: nextBefore });
      setConversations((prev) => [...prev, ...res.items]);
      setNextBefore(res.next_before);
    } catch (err: any) {
      toastError(err.message || '加载更多对话失败');
    } finally {
      setLoadingMore(false);
    }
  };

  // Fetch messages when active conversation changes
  useEffect(() => {
    if (!activeConvId) {
      setMessages([]);
      return;
    }

    let isSubscribed = true;
    setLoadingMessages(true);
    setToolResults({});

    getMessages(activeConvId)
      .then((res) => {
        if (!isSubscribed) return;
        setMessages(res.items || []);

        // Prepopulate tool results from messages with role === 'tool'
        const resultsMap: Record<string, { result?: unknown; error?: string; durationMs?: number }> = {};
        for (const msg of res.items || []) {
          if (msg.role === 'tool' && msg.tool_call_id) {
            resultsMap[msg.tool_call_id] = { result: msg.content };
          }
        }
        setToolResults(resultsMap);
      })
      .catch((err: any) => {
        if (!isSubscribed) return;
        toastError(err.message || '获取对话消息失败');
      })
      .finally(() => {
        if (isSubscribed) {
          setLoadingMessages(false);
          setTimeout(() => scrollToBottom(false), 50);
        }
      });

    return () => {
      isSubscribed = false;
    };
  }, [activeConvId, scrollToBottom, toastError]);

  // Scroll on message updates
  useEffect(() => {
    scrollToBottom(false);
  }, [messages, scrollToBottom]);

  // Create new conversation
  const handleNewChat = async () => {
    if (isStreaming) {
      abortControllerRef.current?.abort();
      setIsStreaming(false);
    }
    try {
      const conv = await createConversation({ title: '新对话' });
      const newConvObj: Conversation = {
        id: conv.id,
        title: conv.title || '新对话',
        channel: 'web',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      };
      setConversations((prev) => [newConvObj, ...prev]);
      setActiveConvId(conv.id);
      setMessages([]);
      setToolResults({});
      setMobileSidebarOpen(false);
    } catch (err: any) {
      toastError(err.message || '创建对话失败');
    }
  };

  const startEmptyConversation = async () => {
    setMessages([]);
    setToolResults({});
    try {
      await handleNewChat();
    } catch {
      /* handled inside */
    }
  };

  const handleDeleteOne = async (c: Conversation) => {
    if (!window.confirm(`确定删除会话「${c.title || '无标题会话'}」吗？\n会话、消息及其追踪记录将一并删除。`)) return;
    try {
      if (c.id === activeConvId) abortControllerRef.current?.abort();
      await deleteConversation(c.id);
      setConversations((prev) => prev.filter((x) => x.id !== c.id));
      if (c.id === activeConvId) {
        setIsStreaming(false);
        await startEmptyConversation();
      }
      toast({ type: 'success', title: '已删除会话', message: '会话及其追踪记录已移除' });
    } catch (err: any) {
      toastError(err.message || '删除会话失败');
    }
  };

  const handleClearAll = async () => {
    if (!window.confirm('确定清空全部会话吗？\n所有会话、消息及其追踪记录将一并删除，且无法恢复。')) return;
    try {
      abortControllerRef.current?.abort();
      const res = await deleteAllConversations();
      setConversations([]);
      setNextBefore(undefined);
      setIsStreaming(false);
      await startEmptyConversation();
      toast({ type: 'success', title: `已清空 ${res.deleted} 个会话`, message: '所有会话及追踪记录已移除' });
    } catch (err: any) {
      toastError(err.message || '清空会话失败');
    }
  };

  // Stop generation
  const handleStop = () => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
      abortControllerRef.current = null;
    }
    setIsStreaming(false);
  };

  // Send message
  const handleSend = async (overrideText?: string) => {
    const textToSend = (overrideText ?? input).trim();
    if (!textToSend || isStreaming) return;

    setInput('');
    const currentConvId = activeConvId;

    const userMsgId = 'u_' + Date.now();
    const userMsg: Message = {
      id: userMsgId,
      role: 'user',
      content: textToSend,
      created_at: new Date().toISOString(),
    };

    const assistantMsgId = 'a_' + (Date.now() + 1);
    const initialAssistantMsg: Message = {
      id: assistantMsgId,
      role: 'assistant',
      content: '',
      tool_calls: [],
      created_at: new Date().toISOString(),
      streaming: true,
    };

    setMessages((prev) => [...prev, userMsg, initialAssistantMsg]);
    setIsStreaming(true);

    const controller = new AbortController();
    abortControllerRef.current = controller;

    try {
      await streamChat({
        conversationId: currentConvId || undefined,
        message: textToSend,
        signal: controller.signal,
        callbacks: {
          onConversation: (data) => {
            if (!currentConvId || currentConvId !== data.id) {
              setActiveConvId(data.id);
              fetchConversationsList();
            }
          },
          onToken: (data) => {
            setMessages((prev) =>
              prev.map((m) =>
                m.id === assistantMsgId
                  ? { ...m, content: m.content + data.text }
                  : m
              )
            );
          },
          onSentence: (data) => {
            // If tokens are not provided, append sentence
            setMessages((prev) =>
              prev.map((m) =>
                m.id === assistantMsgId && !m.content.includes(data.text)
                  ? { ...m, content: m.content + (m.content ? '\n' : '') + data.text }
                  : m
              )
            );
          },
          onToolCall: (data) => {
            toolStartRef.current[data.id] = Date.now();
            const newTc: ToolCall = {
              id: data.id,
              name: data.name,
              arguments: data.arguments,
            };
            setMessages((prev) =>
              prev.map((m) => {
                if (m.id !== assistantMsgId) return m;
                const existing = m.tool_calls || [];
                if (existing.some((t) => t.id === newTc.id)) return m;
                return { ...m, tool_calls: [...existing, newTc] };
              })
            );
          },
          onToolResult: (data) => {
            setToolResults((prev) => ({
              ...prev,
              [data.id]: {
                result: data.result,
                error: data.error,
                durationMs:
                  data.duration_ms ??
                  (toolStartRef.current[data.id]
                    ? Date.now() - toolStartRef.current[data.id]
                    : undefined),
              },
            }));
          },
          onDone: (data) => {
            setMessages((prev) => {
              const updated = prev.map((m) =>
                m.id === assistantMsgId
                  ? {
                      ...m,
                      streaming: false,
                      timings: data.timings,
                    }
                  : m
              );
              return updated;
            });
            setIsStreaming(false);
            abortControllerRef.current = null;
          },
          onError: (data) => {
            toast({
              type: 'error',
              title: '生成出错',
              message: data.message,
            });
            setMessages((prev) =>
              prev.map((m) =>
                m.id === assistantMsgId
                  ? {
                      ...m,
                      streaming: false,
                      content:
                        m.content +
                        `\n\n> ⚠️ **流式传输异常中断**: ${data.message}`,
                    }
                  : m
              )
            );
            setIsStreaming(false);
            abortControllerRef.current = null;
          },
        },
      });
    } catch (err: any) {
      if (err.name !== 'AbortError') {
        toastError(err.message || '对话请求异常');
      }
    } finally {
      setIsStreaming(false);
      abortControllerRef.current = null;
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  const calledIds = new Set<string>(
    messages.flatMap((m) => (m.tool_calls || []).map((t) => t.id))
  );

  const quickPrompts = [
    '播放周杰伦的晴天',
    '检索本地曲库中的无损音频',
    '查一下明天出门的天气',
    '检查本地 fzq 和 jq 工具链状态',
  ];

  return (
    <div className="flex h-full w-full overflow-hidden bg-background">
      {/* Mobile Sidebar Overlay */}
      {mobileSidebarOpen && (
        <div
          className="fixed inset-0 bg-background/80 backdrop-blur-sm z-30 lg:hidden"
          onClick={() => setMobileSidebarOpen(false)}
        />
      )}

      {/* Conversation List Sidebar */}
      <aside
        className={`fixed inset-y-0 left-0 z-40 w-72 lg:static lg:w-72 border-r bg-card/60 backdrop-blur flex flex-col shrink-0 transition-transform duration-200 ease-in-out ${
          mobileSidebarOpen
            ? 'translate-x-0'
            : '-translate-x-full lg:translate-x-0'
        }`}
      >
        {/* Header */}
        <div className="p-3 border-b flex items-center justify-between gap-2">
          <Button
            onClick={handleNewChat}
            className="flex-1 gap-2 shadow-sm font-medium"
            size="sm"
          >
            <Plus className="w-4 h-4" />
            新建对话
          </Button>
          <Button
            variant="ghost"
            size="icon"
            onClick={fetchConversationsList}
            disabled={loadingList}
            title="刷新对话列表"
            className="shrink-0"
          >
            <RefreshCw
              className={`w-4 h-4 text-muted-foreground ${
                loadingList ? 'animate-spin' : ''
              }`}
            />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            onClick={() => setMobileSidebarOpen(false)}
            className="lg:hidden shrink-0"
          >
            <X className="w-4 h-4" />
          </Button>
        </div>

        {conversations.length > 0 && (
          <div className="px-3 py-1.5 border-b flex items-center justify-between text-[11px] text-muted-foreground">
            <span>共 {conversations.length}{nextBefore ? '+' : ''} 个会话</span>
            <button
              type="button"
              onClick={handleClearAll}
              className="flex items-center gap-1 px-1.5 py-0.5 rounded hover:text-destructive hover:bg-destructive/10 transition-colors"
            >
              <Trash2 className="w-3 h-3" />
              清空全部
            </button>
          </div>
        )}

        {/* List items */}
        <div className="flex-1 overflow-y-auto p-2 space-y-1">
          {conversations.length === 0 && !loadingList && (
            <div className="text-center py-10 px-4 text-muted-foreground text-xs">
              暂无历史对话记录，点击上方按钮发起对话
            </div>
          )}

          {conversations.map((c) => {
            const isActive = c.id === activeConvId;
            return (
              <div
                key={c.id}
                role="button"
                tabIndex={0}
                onClick={() => {
                  setActiveConvId(c.id);
                  setMobileSidebarOpen(false);
                }}
                className={`group/item relative cursor-pointer w-full text-left p-2.5 rounded-lg text-xs transition-all flex flex-col gap-1.5 ${
                  isActive
                    ? 'bg-primary/10 text-primary font-medium border border-primary/20 shadow-xs'
                    : 'text-muted-foreground hover:bg-muted/70 hover:text-foreground'
                }`}
              >
                <div className="flex items-center justify-between gap-1 w-full">
                  <span className="truncate font-sans text-[13px] text-foreground">
                    {c.title || '无标题会话'}
                  </span>
                  {c.channel === 'speaker' ? (
                    <Badge
                      variant="outline"
                      className="text-[9px] px-1 py-0 h-4 gap-0.5 text-indigo-500 border-indigo-200 dark:border-indigo-800 shrink-0"
                    >
                      <Radio className="w-2.5 h-2.5" />
                      音箱
                    </Badge>
                  ) : (
                    <Badge
                      variant="secondary"
                      className="text-[9px] px-1 py-0 h-4 gap-0.5 shrink-0"
                    >
                      <Monitor className="w-2.5 h-2.5" />
                      控制台
                    </Badge>
                  )}
                </div>
                <div className="flex items-center justify-between text-[10px] text-muted-foreground/80">
                  <span className="font-mono">{c.id.slice(0, 10)}</span>
                  <span>{formatRelativeTime(c.updated_at || c.created_at)}</span>
                </div>
                <button
                  type="button"
                  title="删除会话"
                  onClick={(e) => {
                    e.stopPropagation();
                    handleDeleteOne(c);
                  }}
                  className="absolute right-1.5 top-1.5 p-1 rounded bg-card/90 border border-border text-muted-foreground opacity-0 group-hover/item:opacity-100 focus:opacity-100 hover:text-destructive hover:border-destructive/40 transition-opacity"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
              </div>
            );
          })}

          {/* Load More Button */}
          {nextBefore && (
            <div className="pt-2">
              <Button
                variant="outline"
                size="sm"
                onClick={handleLoadMore}
                disabled={loadingMore}
                className="w-full text-xs"
              >
                {loadingMore ? '加载中...' : '加载更多历史对话'}
              </Button>
            </div>
          )}
        </div>
      </aside>

      {/* Main Chat Area */}
      <main className="flex-1 flex flex-col min-w-0 h-full relative">
        {/* Chat Header */}
        <header className="h-13 border-b px-4 flex items-center justify-between bg-card/40 backdrop-blur shrink-0">
          <div className="flex items-center gap-2 min-w-0">
            <Button
              variant="ghost"
              size="icon"
              className="lg:hidden shrink-0 -ml-2"
              onClick={() => setMobileSidebarOpen(true)}
            >
              <Menu className="w-4 h-4" />
            </Button>
            <MessageSquare className="w-4 h-4 text-primary shrink-0" />
            <div className="min-w-0">
              <h2 className="text-sm font-semibold truncate text-foreground flex items-center gap-2">
                <span>
                  {conversations.find((c) => c.id === activeConvId)?.title ||
                    '在线对话调试'}
                </span>
                {activeConvId && (
                  <span className="text-[10px] text-muted-foreground font-mono hidden sm:inline-block">
                    ID: {activeConvId}
                  </span>
                )}
              </h2>
            </div>
          </div>

          <div className="flex items-center gap-2 shrink-0">
            {isStreaming && (
              <Badge variant="warning" className="gap-1 animate-pulse">
                <span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-ping" />
                正在响应中
              </Badge>
            )}
            <span className="text-xs text-muted-foreground hidden md:inline-block">
              {messages.length} 条消息
            </span>
          </div>
        </header>

        {/* Message Stream Scroll Area */}
        <div className="flex-1 overflow-y-auto divide-y divide-border/20 py-3">
          {loadingMessages ? (
            <div className="h-full flex items-center justify-center text-xs text-muted-foreground">
              <RefreshCw className="w-4 h-4 animate-spin mr-2" />
              正在加载历史消息...
            </div>
          ) : messages.length === 0 ? (
            <div className="h-full flex flex-col items-center justify-center text-center p-6 text-muted-foreground">
              <div className="w-12 h-12 rounded-2xl bg-primary/10 flex items-center justify-center mb-3">
                <Sparkles className="w-6 h-6 text-primary" />
              </div>
              <h3 className="font-medium text-foreground text-sm mb-1">
                小爱音箱 AI 伴侣调试通道
              </h3>
              <p className="text-xs max-w-sm text-muted-foreground mb-4">
                在此发送指令可直接测试 LLM 推理、本地工具调用 (fzq 拼音检索、音箱播放流驱动) 与事件流。
              </p>
              <div className="flex flex-wrap gap-2 justify-center max-w-md">
                {quickPrompts.map((p, idx) => (
                  <button
                    key={idx}
                    onClick={() => handleSend(p)}
                    className="text-xs px-2.5 py-1.5 rounded-md border border-border bg-card hover:bg-muted text-foreground transition-colors"
                  >
                    {p}
                  </button>
                ))}
              </div>
            </div>
          ) : (
            <>
              {messages.map((msg) => (
                <MessageBubble
                  key={msg.id}
                  orphanTool={msg.role === 'tool' && !calledIds.has(msg.tool_call_id || '')}
                  message={msg}
                  toolResultsMap={toolResults}
                  isStreaming={msg.streaming && isStreaming}
                  conversationId={activeConvId || undefined}
                />
              ))}
              <div ref={messagesEndRef} className="h-4" />
            </>
          )}
        </div>

        {/* Message Input Bottom Area */}
        <div className="border-t bg-card/60 backdrop-blur p-3 shrink-0">
          <div className="max-w-4xl mx-auto flex flex-col gap-2">
            {/* Quick Chips on bottom */}
            {messages.length > 0 && !isStreaming && (
              <div className="flex items-center gap-1.5 overflow-x-auto pb-1 scrollbar-none text-[11px] text-muted-foreground">
                <span className="shrink-0 text-muted-foreground/70 flex items-center gap-1">
                  <Sparkles className="w-3 h-3 text-primary" /> 快速调试:
                </span>
                {quickPrompts.slice(0, 3).map((prompt, i) => (
                  <button
                    key={i}
                    onClick={() => handleSend(prompt)}
                    className="shrink-0 px-2 py-0.5 rounded-full border border-border/80 bg-background hover:bg-muted text-muted-foreground hover:text-foreground transition-colors"
                  >
                    {prompt}
                  </button>
                ))}
              </div>
            )}

            {/* Input Box */}
            <div className="relative flex items-end gap-2 bg-background rounded-xl border border-input focus-within:ring-1 focus-within:ring-ring p-1.5 shadow-xs">
              <Textarea
                value={input}
                onChange={(e) => setInput(e.target.value)}
                onKeyDown={handleKeyDown}
                placeholder="发送调试指令给小爱音箱 AI 伴侣... (Enter 发送，Shift+Enter 换行)"
                className="border-0 focus-visible:ring-0 shadow-none min-h-[44px] max-h-36 py-2 px-3 text-sm bg-transparent"
                rows={1}
                disabled={isStreaming}
              />

              <div className="flex items-center gap-1.5 shrink-0 pr-1 pb-1">
                {isStreaming ? (
                  <Button
                    type="button"
                    variant="destructive"
                    size="sm"
                    onClick={handleStop}
                    className="gap-1.5 h-8 px-3 text-xs shadow-sm"
                  >
                    <Square className="w-3.5 h-3.5 fill-current" />
                    停止生成
                  </Button>
                ) : (
                  <Button
                    type="button"
                    size="sm"
                    onClick={() => handleSend()}
                    disabled={!input.trim()}
                    className="gap-1.5 h-8 px-3 text-xs shadow-sm"
                  >
                    <span>发送</span>
                    <Send className="w-3.5 h-3.5" />
                  </Button>
                )}
              </div>
            </div>

            <div className="flex items-center justify-between text-[11px] text-muted-foreground/70 px-1">
              <span>Enter 发送，Shift + Enter 换行</span>
              <span className="hidden sm:inline">POST sse/chat 流式解析驱动</span>
            </div>
          </div>
        </div>
      </main>
    </div>
  );
}
