import { useState, useEffect, useRef, useCallback } from 'react';
import {
  Send, Radio, RefreshCw, Menu, X, Trash2, ChevronRight, ChevronDown, Wifi, WifiOff, Mic, History,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';
import { Badge } from '@/components/ui/badge';
import { FirstRunBanner } from '@/components/guide/FirstRunBanner';
import { SpeakerTimeline } from './SpeakerTimeline';
import {
  getSpeakerStatus, getSpeakerEvents, mockAsk,
  type SpeakerStatus, type SpeakerDeviceStatus, type MockEvent,
} from '@/lib/speakerApi';
import { type Conversation, type Message, getConversations, deleteConversation, getMessages } from '@/lib/api';
import { useToast } from '@/components/ui/toast';
import { formatRelativeTime } from '@/lib/utils';

export interface ChatViewProps {
  targetConversationId?: string;
  onOpenGuide?: () => void;
}

const DEBUG_ID = 'debug';
const EXAMPLES = ['小爱同学，播放一首周杰伦的晴天', '播放海阔天空', '来点陈奕迅的歌', '我想听七里香', '今天天气怎么样', '问问 我的曲库有多少首歌'];

export function ChatView({ targetConversationId, onOpenGuide }: ChatViewProps = {}) {
  const { toast, error: toastError } = useToast();

  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [status, setStatus] = useState<SpeakerStatus | null>(null);
  const [selected, setSelected] = useState<string>('device:' + DEBUG_ID); // 'device:<id>' | 'conv:<id>'
  const [debugConvOverride, setDebugConvOverride] = useState<string | null>(null);
  const [legacyOpen, setLegacyOpen] = useState(false);
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false);

  const [messages, setMessages] = useState<Message[]>([]);
  const [toolResults, setToolResults] = useState<Record<string, { result?: unknown; error?: string; durationMs?: number }>>({});
  const [events, setEvents] = useState<MockEvent[]>([]);
  const afterRef = useRef(0);

  const [input, setInput] = useState('');
  const [sending, setSending] = useState(false);
  const endRef = useRef<HTMLDivElement>(null);

  // ---------- data ----------
  const loadConversations = useCallback(async () => {
    try { setConversations((await getConversations({ limit: 100 })).items || []); } catch (e: any) { toastError(e.message || '获取对话列表失败'); }
  }, [toastError]);
  const loadStatus = useCallback(() => getSpeakerStatus().then(setStatus).catch(() => {}), []);

  useEffect(() => {
    loadConversations(); loadStatus();
    const t = setInterval(loadStatus, 4000);
    return () => clearInterval(t);
  }, [loadConversations, loadStatus]);

  // devices: debug always first, then the rest
  const rawDevices: SpeakerDeviceStatus[] = status?.devices || [];
  const debugDev: SpeakerDeviceStatus = rawDevices.find((d) => d.device_id === DEBUG_ID) || { device_id: DEBUG_ID, name: '调试音箱' };
  const devices = [debugDev, ...rawDevices.filter((d) => d.device_id !== DEBUG_ID)];
  const convOfDevice = (d: SpeakerDeviceStatus) =>
    (d.device_id === DEBUG_ID && debugConvOverride) || d.conversation_id ||
    conversations.find((c) => c.channel === 'speaker' && c.device_id === d.device_id)?.id;
  const deviceConvIds = new Set(devices.map(convOfDevice).filter(Boolean) as string[]);
  const legacy = conversations.filter((c) => !deviceConvIds.has(c.id));

  let activeConvId: string | undefined;
  let activeDevice: SpeakerDeviceStatus | undefined;
  if (selected.startsWith('device:')) {
    activeDevice = devices.find((d) => 'device:' + d.device_id === selected) || debugDev;
    activeConvId = convOfDevice(activeDevice);
  } else activeConvId = selected.slice(5);
  const editable = activeDevice?.device_id === DEBUG_ID;
  const activeTitle = activeDevice ? (activeDevice.device_id === DEBUG_ID ? '调试音箱' : activeDevice.name) : conversations.find((c) => c.id === activeConvId)?.title || '历史会话';

  // external navigation (from logs page)
  useEffect(() => {
    if (!targetConversationId) return;
    setSelected('conv:' + targetConversationId);
    setLegacyOpen(true);
  }, [targetConversationId]);

  const reloadMessages = useCallback(async (id: string) => {
    try {
      const res = await getMessages(id);
      setMessages(res.items || []);
      const map: Record<string, { result?: unknown }> = {};
      for (const m of res.items || []) if (m.role === 'tool' && m.tool_call_id) map[m.tool_call_id] = { result: m.content };
      setToolResults(map);
    } catch { /* ignore */ }
  }, []);

  // messages + events when the conversation changes
  useEffect(() => {
    setMessages([]); setToolResults({}); setEvents([]); afterRef.current = 0;
    if (!activeConvId) return;
    const convId = activeConvId;
    reloadMessages(convId);
    let stop = false;
    const tick = async () => {
      try {
        const r = await getSpeakerEvents(afterRef.current, convId);
        if (stop) return;
        if (r.last_seq != null) afterRef.current = r.last_seq;
        const evs = (r.events || []).filter((e) => !e.conversation_id || e.conversation_id === convId);
        if (evs.length) {
          setEvents((p) => [...p, ...evs]);
          if (evs.some((e) => e.kind === 'reply' || e.kind === 'turn_end')) { reloadMessages(convId); loadConversations(); }
        }
      } catch { /* backend may not support yet */ }
    };
    tick();
    const t = setInterval(tick, 700);
    return () => { stop = true; clearInterval(t); };
  }, [activeConvId, reloadMessages, loadConversations]);

  useEffect(() => { endRef.current?.scrollIntoView({ behavior: 'smooth' }); }, [events.length, messages.length]);

  // ---------- actions ----------
  const send = async (t?: string) => {
    const text = (t ?? input).trim();
    if (!text || sending || !editable) return;
    setSending(true);
    try {
      const r = await mockAsk(text, DEBUG_ID);
      setInput('');
      if (r.conversation_id) setDebugConvOverride(r.conversation_id);
      loadStatus(); loadConversations();
    } catch (e: any) { toastError(e.message || '发送失败'); }
    finally { setSending(false); }
  };

  const del = async (id: string, label: string, isDevice: boolean) => {
    if (!window.confirm(`确定删除「${label}」${isDevice ? '的历史记录' : ''}吗？\n消息及其追踪记录将一并删除。`)) return;
    try {
      await deleteConversation(id);
      if (debugConvOverride === id) setDebugConvOverride(null);
      if (id === activeConvId) { setMessages([]); setEvents([]); if (!isDevice) setSelected('device:' + DEBUG_ID); }
      await Promise.all([loadConversations(), loadStatus()]);
      toast({ type: 'success', title: '已删除', message: label });
    } catch (e: any) { toastError(e.message || '删除失败'); }
  };

  const calledIds = new Set<string>(messages.flatMap((m) => (m.tool_calls || []).map((t) => t.id)));
  const showEmpty = messages.length === 0 && events.length === 0;
  const pick = (key: string) => { setSelected(key); setMobileSidebarOpen(false); };

  return (
    <div className="flex h-full w-full overflow-hidden bg-background">
      {mobileSidebarOpen && <div className="fixed inset-0 bg-background/80 backdrop-blur-sm z-30 lg:hidden" onClick={() => setMobileSidebarOpen(false)} />}

      <aside className={`fixed inset-y-0 left-0 z-40 w-72 lg:static border-r bg-card/60 backdrop-blur flex flex-col shrink-0 transition-transform duration-200 ${mobileSidebarOpen ? 'translate-x-0' : '-translate-x-full lg:translate-x-0'}`}>
        <div className="p-3 border-b flex items-center justify-between">
          <span className="text-sm font-semibold flex items-center gap-2"><Radio className="w-4 h-4 text-primary" />音箱</span>
          <div className="flex gap-1">
            <Button variant="ghost" size="icon" title="刷新" onClick={() => { loadConversations(); loadStatus(); }}><RefreshCw className="w-4 h-4 text-muted-foreground" /></Button>
            <Button variant="ghost" size="icon" className="lg:hidden" onClick={() => setMobileSidebarOpen(false)}><X className="w-4 h-4" /></Button>
          </div>
        </div>

        <div className="flex-1 overflow-y-auto p-2 space-y-1">
          {devices.map((d) => {
            const key = 'device:' + d.device_id;
            const cid = convOfDevice(d);
            const conv = conversations.find((c) => c.id === cid);
            const isDebug = d.device_id === DEBUG_ID;
            const active = selected === key;
            return (
              <div key={key} role="button" tabIndex={0} onClick={() => pick(key)}
                className={`group/item relative cursor-pointer p-2.5 rounded-lg text-xs border transition-all ${active ? 'bg-primary/10 border-primary/30' : 'border-transparent hover:bg-muted/70'}`}>
                <div className="flex items-center gap-2">
                  <Radio className={`w-4 h-4 shrink-0 ${isDebug ? 'text-primary' : 'text-indigo-500'}`} />
                  <span className="font-medium text-[13px] truncate flex-1">{isDebug ? '调试音箱' : d.name}</span>
                  {isDebug ? <Badge variant="secondary" className="text-[9px] px-1 py-0 h-4">可对话</Badge>
                    : d.online === false ? <WifiOff className="w-3 h-3 text-muted-foreground" /> : <Wifi className="w-3 h-3 text-emerald-500" />}
                </div>
                <div className="mt-1 text-[10px] text-muted-foreground flex justify-between pl-6">
                  <span>{isDebug ? '对它说话来测试' : `${d.hardware || ''} · 只读`}</span>
                  <span>{conv ? formatRelativeTime(conv.updated_at || conv.created_at) : '暂无记录'}</span>
                </div>
                {cid && (
                  <button type="button" title="清空该设备历史" onClick={(e) => { e.stopPropagation(); del(cid, isDebug ? '调试音箱' : d.name, true); }}
                    className="absolute right-1.5 top-1.5 p-1 rounded bg-card/90 border text-muted-foreground opacity-0 group-hover/item:opacity-100 hover:text-destructive transition-opacity">
                    <Trash2 className="w-3 h-3" />
                  </button>
                )}
              </div>
            );
          })}

          {legacy.length > 0 && (
            <div className="pt-2">
              <button type="button" onClick={() => setLegacyOpen(!legacyOpen)} className="w-full flex items-center gap-1.5 px-2 py-1.5 text-[11px] text-muted-foreground hover:text-foreground">
                {legacyOpen ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />}
                <History className="w-3 h-3" />历史网页会话 ({legacy.length})
              </button>
              {legacyOpen && legacy.map((c) => (
                <div key={c.id} role="button" tabIndex={0} onClick={() => pick('conv:' + c.id)}
                  className={`group/item relative cursor-pointer p-2 rounded-lg text-xs ${selected === 'conv:' + c.id ? 'bg-primary/10 border border-primary/20' : 'hover:bg-muted/70 border border-transparent'}`}>
                  <div className="truncate text-[13px] pr-6">{c.title || '无标题会话'}</div>
                  <div className="text-[10px] text-muted-foreground flex justify-between"><span className="font-mono">{c.id.slice(0, 10)}</span><span>{formatRelativeTime(c.updated_at || c.created_at)}</span></div>
                  <button type="button" title="删除会话" onClick={(e) => { e.stopPropagation(); del(c.id, c.title || '无标题会话', false); }}
                    className="absolute right-1.5 top-1.5 p-1 rounded bg-card/90 border text-muted-foreground opacity-0 group-hover/item:opacity-100 hover:text-destructive transition-opacity">
                    <Trash2 className="w-3 h-3" />
                  </button>
                </div>
              ))}
            </div>
          )}
        </div>
      </aside>

      <main className="flex-1 flex flex-col min-w-0 h-full">
        {onOpenGuide && <FirstRunBanner onOpenGuide={onOpenGuide} />}
        <header className="h-13 border-b px-4 flex items-center gap-2 bg-card/40 backdrop-blur shrink-0">
          <Button variant="ghost" size="icon" className="lg:hidden shrink-0 -ml-2" onClick={() => setMobileSidebarOpen(true)}><Menu className="w-4 h-4" /></Button>
          <Radio className="w-4 h-4 text-primary shrink-0" />
          <h2 className="text-sm font-semibold truncate">{activeTitle}</h2>
          {!editable && <Badge variant="outline" className="text-[10px]">只读</Badge>}
          {status?.driver && status.driver !== 'mock' && editable && <span className="text-[11px] text-amber-600">当前驱动为 {status.driver}，调试音箱可能不响应</span>}
          {activeConvId && <span className="text-[10px] text-muted-foreground font-mono hidden sm:inline ml-auto">ID: {activeConvId}</span>}
        </header>

        <div className="flex-1 overflow-y-auto py-3">
          {showEmpty ? (
            <div className="h-full flex flex-col items-center justify-center text-center p-6 text-muted-foreground">
              <div className="w-12 h-12 rounded-2xl bg-primary/10 flex items-center justify-center mb-3"><Mic className="w-6 h-6 text-primary" /></div>
              <h3 className="font-medium text-foreground text-sm mb-1">{editable ? '对调试音箱说点什么' : '暂无记录'}</h3>
              <p className="text-xs max-w-sm mb-4">{editable ? '每次提问都会展示完整流程：检测 → 打断 → 大模型 → 工具 → 播报 → 播放。' : '该音箱收到提问后，处理流程会实时显示在这里。'}</p>
              {editable && (
                <div className="flex flex-wrap gap-2 justify-center max-w-md">
                  {EXAMPLES.map((p) => <button key={p} onClick={() => send(p)} className="text-xs px-2.5 py-1.5 rounded-md border bg-card hover:bg-muted text-foreground">{p}</button>)}
                </div>
              )}
            </div>
          ) : (
            <>
              <SpeakerTimeline messages={messages} events={events} toolResults={toolResults} calledIds={calledIds} conversationId={activeConvId} />
              <div ref={endRef} className="h-4" />
            </>
          )}
        </div>

        <div className="border-t bg-card/60 backdrop-blur p-3 shrink-0">
          {editable ? (
            <div className="max-w-4xl mx-auto flex flex-col gap-2">
              {!showEmpty && (
                <div className="flex items-center gap-1.5 overflow-x-auto pb-1 text-[11px]">
                  {EXAMPLES.map((p) => <button key={p} onClick={() => send(p)} disabled={sending} className="shrink-0 px-2 py-0.5 rounded-full border bg-background hover:bg-muted text-muted-foreground hover:text-foreground">{p}</button>)}
                </div>
              )}
              <div className="flex items-end gap-2 bg-background rounded-xl border border-input focus-within:ring-1 focus-within:ring-ring p-1.5">
                <Textarea value={input} onChange={(e) => setInput(e.target.value)}
                  onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); } }}
                  placeholder="对调试音箱说话… (Enter 发送，Shift+Enter 换行)"
                  className="border-0 focus-visible:ring-0 shadow-none min-h-[44px] max-h-36 py-2 px-3 text-sm bg-transparent" rows={1} />
                <Button type="button" size="sm" onClick={() => send()} disabled={!input.trim() || sending} className="gap-1.5 h-8 px-3 text-xs mb-1 mr-1">发送<Send className="w-3.5 h-3.5" /></Button>
              </div>
            </div>
          ) : (
            <div className="text-center text-xs text-muted-foreground py-2">
              {activeDevice ? '真实音箱请直接对音箱说话，这里只显示记录' : '历史会话仅供查看'}
            </div>
          )}
        </div>
      </main>
    </div>
  );
}
