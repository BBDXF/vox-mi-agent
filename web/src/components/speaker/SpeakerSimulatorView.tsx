import { useEffect, useRef, useState } from 'react';
import { Check, Copy, ExternalLink, Radio, Send, User, Pause, Mic, Music, Volume2, Square, Play } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { useToast } from '@/components/ui/toast';
import { getSpeakerStatus, getMockEvents, mockAsk, type MockEvent, type SpeakerStatus } from '@/lib/speakerApi';

const EXAMPLES = ['小爱同学，播放一首周杰伦的晴天', '播放海阔天空', '来点陈奕迅的歌', '我想听七里香', '今天天气怎么样', '问问 我的曲库有多少首歌'];
const ROUTES: Record<string, string> = { direct: '直接播放', artist_random: '随机播放歌手', agent: '交给Agent', ignored: '未接管' };

function titleOf(url?: string) {
  if (!url) return '音频';
  try {
    const u = new URL(url, window.location.href);
    const t = u.searchParams.get('title') || u.searchParams.get('name');
    if (t) return t;
    const last = decodeURIComponent(u.pathname.split('/').filter(Boolean).pop() || '');
    return last || url;
  } catch { return url; }
}
const fmtTime = (ts: number | string) => {
  const ms = typeof ts === 'number' ? (ts < 1e12 ? ts * 1000 : ts) : Date.parse(ts);
  return isNaN(ms) ? '' : new Date(ms).toLocaleTimeString();
};

function PlayUrl({ e }: { e: MockEvent }) {
  const [copied, setCopied] = useState(false);
  const title = e.title || titleOf(e.url);
  const copy = async () => {
    try { await navigator.clipboard.writeText(e.url || ''); setCopied(true); setTimeout(() => setCopied(false), 1500); } catch { /* */ }
  };
  return (
    <div className="space-y-1.5">
      <div className="font-medium">{title}{e.artist && <span className="text-muted-foreground font-normal"> · {e.artist}</span>}</div>
      {e.url && (
        <div className="flex items-start gap-2">
          <code className="text-[11px] font-mono break-all text-muted-foreground flex-1">{e.url}</code>
          <button type="button" onClick={copy} title="复制" className="shrink-0 text-muted-foreground hover:text-foreground">{copied ? <Check className="w-3.5 h-3.5" /> : <Copy className="w-3.5 h-3.5" />}</button>
          <a href={e.url} target="_blank" rel="noreferrer" title="在新标签页打开" className="shrink-0 text-muted-foreground hover:text-foreground"><ExternalLink className="w-3.5 h-3.5" /></a>
        </div>
      )}
      <audio controls preload="none" src={e.url} className="h-8 max-w-full" />
    </div>
  );
}

function EventRow({ e }: { e: MockEvent }) {
  const lat = e.latency_ms != null && <Badge variant="outline" className="font-mono text-[10px]">{e.latency_ms}ms</Badge>;
  if (e.kind === 'ask') {
    return (
      <div className="flex justify-end">
        <div className="max-w-[80%] rounded-2xl rounded-br-sm bg-primary text-primary-foreground px-4 py-2 text-sm">
          <div className="text-[10px] opacity-70 flex items-center gap-1 mb-0.5"><User className="w-3 h-3" />用户提问 · {fmtTime(e.ts)}</div>
          {e.text}
        </div>
      </div>
    );
  }
  if (e.kind === 'reply') {
    return (
      <div className="flex">
        <div className="max-w-[85%] rounded-2xl rounded-bl-sm border border-primary/30 bg-primary/5 px-4 py-2 text-sm">
          <div className="text-[10px] text-muted-foreground flex items-center gap-1.5 mb-0.5"><Mic className="w-3 h-3" />Agent 完整回复{lat}<span>{fmtTime(e.ts)}</span></div>
          <div className="whitespace-pre-wrap">{e.text}</div>
        </div>
      </div>
    );
  }
  if (e.kind === 'ask_detected') {
    const r = e.route ? ROUTES[e.route] || e.route : e.handled ? '已接管' : '未接管';
    return (
      <div className="flex justify-center">
        <div className="text-[11px] text-muted-foreground flex items-center gap-1.5 rounded-full bg-muted px-3 py-1">
          检测到提问 · 路线：{r}{lat}
        </div>
      </div>
    );
  }
  const map: Record<string, { icon: any; label: string }> = {
    pause: { icon: Pause, label: '暂停原生回复' },
    stop: { icon: Square, label: '停止播放' },
    play: { icon: Play, label: '继续播放' },
    tts: { icon: Mic, label: 'TTS 播报' },
    play_url: { icon: Music, label: '播放链接' },
    volume: { icon: Volume2, label: '调整音量' },
  };
  const m = map[e.kind] || { icon: Square, label: e.kind };
  const Icon = m.icon;
  return (
    <div className="flex">
      <div className={`max-w-[85%] rounded-2xl rounded-bl-sm border px-4 py-2 text-sm space-y-1 ${'bg-card'}`}>
        <div className="text-[10px] text-muted-foreground flex items-center gap-1.5">
          <Icon className="w-3 h-3" />{m.label}{lat}<span>{fmtTime(e.ts)}</span>
        </div>
        {e.kind === 'tts' && <div>{e.text}</div>}
        {e.kind === 'volume' && <div>音量 {e.volume}</div>}
        {e.kind === 'play_url' && <PlayUrl e={e} />}
      </div>
    </div>
  );
}

export function SpeakerSimulatorView({ onGoSettings }: { onGoSettings: () => void }) {
  const { error: toastError } = useToast();
  const [status, setStatus] = useState<SpeakerStatus | null>(null);
  const [device, setDevice] = useState('');
  const [text, setText] = useState('');
  const [events, setEvents] = useState<MockEvent[]>([]);
  const [nativeReply, setNativeReply] = useState(true);
  const [sending, setSending] = useState(false);
  const afterRef = useRef(0);
  const endRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    getSpeakerStatus().then(setStatus).catch(() => {});
  }, []);

  useEffect(() => {
    let stop = false;
    const tick = async () => {
      try {
        const r = await getMockEvents(afterRef.current);
        if (stop) return;
        const items = r.events || [];
        if (items.length) setEvents((p) => [...p, ...items]);
        if (r.last_seq != null) afterRef.current = r.last_seq;
      } catch { /* ignore */ }
    };
    tick();
    const t = setInterval(tick, 700);
    return () => { stop = true; clearInterval(t); };
  }, []);

  useEffect(() => { endRef.current?.scrollIntoView({ behavior: 'smooth' }); }, [events.length]);

  const send = async (t?: string) => {
    const msg = (t ?? text).trim();
    if (!msg || sending) return;
    setSending(true);
    try { await mockAsk(msg, device || undefined, nativeReply); setText(''); }
    catch (e: any) { toastError(e.message || '发送失败'); }
    finally { setSending(false); }
  };

  const devices = status?.devices || [];
  const notMock = status?.driver && status.driver !== 'mock';

  return (
    <div className="h-full flex flex-col max-w-3xl mx-auto p-4 md:p-6 gap-4">
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-xl font-bold flex items-center gap-2"><Radio className="w-5 h-5 text-primary" />模拟音箱</h1>
        <select value={device} onChange={(e) => setDevice(e.target.value)} className="h-9 rounded-md border border-input bg-background px-2 text-sm">
          <option value="">默认设备</option>
          {devices.map((d) => <option key={d.device_id} value={d.device_id}>{d.name}</option>)}
        </select>
      </div>

      {notMock && (
        <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs">
          当前驱动不是「模拟音箱」，这里不会有事件。
          <button className="text-primary underline ml-1" onClick={onGoSettings}>去设置切换驱动</button>
        </div>
      )}

      <div className="flex-1 min-h-0 overflow-y-auto rounded-xl border bg-muted/20 p-4 space-y-3">
        {events.length === 0 && <div className="text-center text-sm text-muted-foreground py-16">对音箱说点什么吧，事件会实时出现在这里</div>}
        {events.map((e) => <EventRow key={e.seq} e={e} />)}
        <div ref={endRef} />
      </div>

      <div className="space-y-2">
        <div className="flex flex-wrap gap-2">
          {EXAMPLES.map((x) => (
            <button key={x} type="button" onClick={() => send(x)} className="text-xs rounded-full border px-3 py-1 hover:bg-muted">{x}</button>
          ))}
        </div>
        <label className="flex items-center gap-2 text-xs text-muted-foreground cursor-pointer">
          <input type="checkbox" role="switch" checked={nativeReply} onChange={(e) => setNativeReply(e.target.checked)} className="accent-primary w-4 h-4" />模拟小爱原生回复
        </label>
        <form className="flex gap-2" onSubmit={(e) => { e.preventDefault(); send(); }}>
          <Input value={text} onChange={(e) => setText(e.target.value)} placeholder="对音箱说话…" className="h-12 text-base" />
          <Button type="submit" disabled={sending || !text.trim()} className="h-12 px-5 gap-2"><Send className="w-4 h-4" />对音箱说话</Button>
        </form>
      </div>
    </div>
  );
}
