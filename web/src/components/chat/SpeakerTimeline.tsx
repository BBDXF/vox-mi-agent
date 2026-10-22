import { useState } from 'react';
import { Check, Copy, ExternalLink, Mic, Music, Pause, Square, Play, Volume2 } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { MessageBubble } from './MessageBubble';
import type { Message } from '@/lib/api';
import type { MockEvent } from '@/lib/speakerApi';

const ROUTES: Record<string, string> = { direct: '直接播放', artist_random: '随机播放歌手', agent: '交给Agent', ignored: '未接管' };

export const toMs = (t?: number | string) => {
  if (t == null) return 0;
  const ms = typeof t === 'number' ? (t < 1e12 ? t * 1000 : t) : Date.parse(t);
  return isNaN(ms) ? 0 : ms;
};
const fmtTime = (t?: number | string) => {
  const ms = toMs(t);
  return ms ? new Date(ms).toLocaleTimeString() : '';
};
function titleOf(url?: string) {
  if (!url) return '音频';
  try {
    const u = new URL(url, window.location.href);
    return u.searchParams.get('title') || u.searchParams.get('name') || decodeURIComponent(u.pathname.split('/').filter(Boolean).pop() || '') || url;
  } catch { return url; }
}

function Lat({ ms }: { ms?: number }) {
  return ms != null ? <Badge variant="outline" className="font-mono text-[10px] px-1 py-0">{ms}ms</Badge> : null;
}

function PlayUrlCard({ e }: { e: MockEvent }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    try { await navigator.clipboard.writeText(e.url || ''); setCopied(true); setTimeout(() => setCopied(false), 1500); } catch { /* */ }
  };
  return (
    <div className="mx-4 my-2 max-w-xl rounded-xl border bg-card px-4 py-3 space-y-1.5 text-sm">
      <div className="text-[10px] text-muted-foreground flex items-center gap-1.5"><Music className="w-3 h-3" />播放链接<Lat ms={e.latency_ms} /><span>{fmtTime(e.ts)}</span></div>
      <div className="font-medium">{e.title || titleOf(e.url)}{e.artist && <span className="text-muted-foreground font-normal"> · {e.artist}</span>}</div>
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

function SysLine({ children }: { children: React.ReactNode }) {
  return <div className="flex justify-center py-1"><div className="text-[11px] text-muted-foreground flex items-center gap-1.5 rounded-full bg-muted px-3 py-0.5">{children}</div></div>;
}

function TtsGroup({ items }: { items: MockEvent[] }) {
  return (
    <details className="mx-4 my-1 max-w-xl text-[11px] text-muted-foreground">
      <summary className="cursor-pointer flex items-center gap-1.5 select-none"><Mic className="w-3 h-3" />播报 {items.length} 句{items.length > 0 && <Lat ms={items[0].latency_ms} />}</summary>
      <ol className="mt-1 ml-4 list-decimal space-y-0.5">
        {items.map((t) => <li key={t.seq}>{t.text}</li>)}
      </ol>
    </details>
  );
}

type Item = { t: number; order: number; msg?: Message; ev?: MockEvent };

export function SpeakerTimeline({
  messages, events, toolResults, calledIds, conversationId,
}: {
  messages: Message[]; events: MockEvent[];
  toolResults: Record<string, { result?: unknown; error?: string; durationMs?: number }>;
  calledIds: Set<string>; conversationId?: string;
}) {
  const items: Item[] = [
    ...messages.map((m, i) => ({ t: toMs(m.created_at), order: i, msg: m })),
    ...events.filter((e) => e.kind !== 'reply' && e.kind !== 'ask').map((e) => ({ t: toMs(e.ts), order: 1e6 + e.seq, ev: e })),
  ];
  // messages without a timestamp keep their relative position; sort only when both have times
  items.sort((a, b) => (a.t && b.t && a.t !== b.t ? a.t - b.t : a.order - b.order));

  const out: React.ReactNode[] = [];
  let tts: MockEvent[] = [];
  const flush = () => { if (tts.length) { out.push(<TtsGroup key={'tts' + tts[0].seq} items={tts} />); tts = []; } };
  for (const it of items) {
    if (it.msg) {
      flush();
      const m = it.msg;
      out.push(<MessageBubble key={m.id} orphanTool={m.role === 'tool' && !calledIds.has(m.tool_call_id || '')} message={m} toolResultsMap={toolResults} isStreaming={false} conversationId={conversationId} />);
      continue;
    }
    const e = it.ev!;
    if (e.kind === 'tts') { tts.push(e); continue; }
    flush();
    if (e.kind === 'ask_detected') {
      const r = e.route ? ROUTES[e.route] || e.route : e.handled ? '已接管' : '未接管';
      out.push(<SysLine key={e.seq}>检测到提问 · 路线：{r}<Lat ms={e.latency_ms} /></SysLine>);
    } else if (e.kind === 'play_url') out.push(<PlayUrlCard key={e.seq} e={e} />);
    else if (e.kind === 'pause') out.push(<SysLine key={e.seq}><Pause className="w-3 h-3" />暂停原生回复<Lat ms={e.latency_ms} /></SysLine>);
    else if (e.kind === 'stop') out.push(<SysLine key={e.seq}><Square className="w-3 h-3" />停止播放<Lat ms={e.latency_ms} /></SysLine>);
    else if (e.kind === 'play') out.push(<SysLine key={e.seq}><Play className="w-3 h-3" />继续播放</SysLine>);
    else if (e.kind === 'volume') out.push(<SysLine key={e.seq}><Volume2 className="w-3 h-3" />音量 {e.volume}</SysLine>);
  }
  flush();
  return <>{out}</>;
}
