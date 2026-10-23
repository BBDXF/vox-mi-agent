import { useState } from 'react';
import { PALETTE, STEP } from '@/lib/accent';
import { Check, Copy, ExternalLink, Mic, Music, Pause, Square, Play, Volume2, Wrench, Brain, Ear, Radio, Activity, ChevronRight, ChevronDown, User, Flag } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { MessageBubble } from './MessageBubble';
import { TurnTraceTimeline } from './TurnTraceTimeline';
import type { Message } from '@/lib/api';
import type { MockEvent } from '@/lib/speakerApi';

const ROUTES: Record<string, string> = { direct: '直接播放', artist_random: '随机播放歌手', agent: '交给Agent', ignored: '未接管' };

export const toMs = (t?: number | string) => {
  if (t == null) return 0;
  const ms = typeof t === 'number' ? (t < 1e12 ? t * 1000 : t) : Date.parse(t);
  return isNaN(ms) ? 0 : ms;
};
const fmtTime = (t?: number | string) => { const ms = toMs(t); return ms ? new Date(ms).toLocaleTimeString() : ''; };
const fmtMs = (ms?: number) => (ms == null ? '' : ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${Math.round(ms)}ms`);
function titleOf(url?: string) {
  if (!url) return '音频';
  try {
    const u = new URL(url, window.location.href);
    return u.searchParams.get('title') || u.searchParams.get('name') || decodeURIComponent(u.pathname.split('/').filter(Boolean).pop() || '') || url;
  } catch { return url; }
}
function Lat({ ms }: { ms?: number }) {
  return ms != null ? <Badge variant="outline" className="font-mono text-[10px] px-1 py-0">{fmtMs(ms)}</Badge> : null;
}
const asStr = (v: unknown) => (typeof v === 'string' ? v : JSON.stringify(v, null, 2));

function PlayUrlStep({ e }: { e: MockEvent }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => { try { await navigator.clipboard.writeText(e.url || ''); setCopied(true); setTimeout(() => setCopied(false), 1500); } catch { /* */ } };
  return (
    <div className="space-y-1.5 rounded-lg border bg-card px-3 py-2 max-w-xl">
      <div className="font-medium text-sm">{e.title || titleOf(e.url)}{e.artist && <span className="text-muted-foreground font-normal"> · {e.artist}</span>}</div>
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

function ToolStep({ call, res }: { call?: MockEvent; res?: MockEvent }) {
  const [open, setOpen] = useState(false);
  const name = call?.name || res?.name || '工具';
  const hasBody = call?.args != null || res?.result != null || res?.error;
  return (
    <div className="text-xs">
      <button type="button" onClick={() => setOpen(!open)} className="flex items-center gap-1.5 hover:text-foreground">
        {hasBody && (open ? <ChevronDown className="w-3 h-3" /> : <ChevronRight className="w-3 h-3" />)}
        <span className="font-mono font-medium">{name}</span>
        {res ? <Badge variant={res.ok === false || res.error ? 'destructive' : 'success'} className="text-[10px] px-1 py-0">{res.ok === false || res.error ? '失败' : '成功'}</Badge> : <span className="text-muted-foreground animate-pulse">执行中…</span>}
        <Lat ms={res?.duration_ms} />
      </button>
      {open && (
        <div className="mt-1 space-y-1">
          {call?.args != null && <pre className="rounded bg-muted p-2 overflow-x-auto whitespace-pre-wrap break-all text-[11px]">参数：{asStr(call.args)}</pre>}
          {res?.error && <pre className="rounded bg-destructive/10 text-destructive p-2 whitespace-pre-wrap break-all text-[11px]">{res.error}</pre>}
          {res?.result != null && <pre className="rounded bg-muted p-2 overflow-x-auto max-h-64 whitespace-pre-wrap break-all text-[11px]">{asStr(res.result)}</pre>}
        </div>
      )}
    </div>
  );
}

const iconStep = (I: any): keyof typeof STEP =>
  I === Radio ? 'detect' : I === Pause || I === Square ? 'interrupt' : I === Brain ? 'llm' : I === Wrench ? 'tool'
  : I === Mic ? 'tts' : I === Play || I === Music || I === Volume2 ? 'play' : 'native';

interface Step { key: string; icon: any; label: React.ReactNode; body?: React.ReactNode; tone?: string; }

function buildSteps(evs: MockEvent[]): Step[] {
  const steps: Step[] = [];
  const toolIdx: Record<string, number> = {};
  const toolCalls: Record<string, MockEvent | undefined> = {};
  const toolRes: Record<string, MockEvent | undefined> = {};
  for (const e of evs) { const id = e.call_id || String(e.seq); if (e.kind === 'tool_call') toolCalls[id] = e; if (e.kind === 'tool_result') toolRes[id] = e; }
  const llmEnd = evs.filter((e) => e.kind === 'llm_end');
  let llmN = 0;
  let ttsStep: { sentences: { text: string; lat?: number }[] } | null = null;
  const hasTts = evs.some((e) => e.kind === 'tts');
  const flushTts = () => {
    if (!ttsStep) return; const t = ttsStep; ttsStep = null;
    steps.push({ key: 'tts' + steps.length, icon: Mic, label: <>播报 {t.sentences.length} 句</>,
      body: <ol className="list-decimal ml-4 space-y-0.5 text-sm">{t.sentences.map((x, i) => <li key={i}>{x.text}{' '}<Lat ms={x.lat} /></li>)}</ol> });
  };
  for (const e of evs) {
    if (e.kind === 'tts') { if (!ttsStep) ttsStep = { sentences: [] }; ttsStep.sentences.push({ text: e.text || '', lat: e.latency_ms }); continue; }
    flushTts();
    switch (e.kind) {
      case 'ask': case 'turn_start': case 'turn_end': case 'ask_detected': break;
      case 'pause': steps.push({ key: 'k' + e.seq, icon: Pause, label: <>暂停小爱原生回复<Lat ms={e.latency_ms} /></> }); break;
      case 'stop': steps.push({ key: 'k' + e.seq, icon: Square, label: <>打断小爱 / 停止播放<Lat ms={e.latency_ms} /></> }); break;
      case 'play': steps.push({ key: 'k' + e.seq, icon: Play, label: '继续播放' }); break;
      case 'volume': steps.push({ key: 'k' + e.seq, icon: Volume2, label: `音量 ${e.volume}` }); break;
      case 'llm_start': {
        const end = llmEnd[llmN++];
        steps.push({ key: 'k' + e.seq, icon: Brain, tone: end?.error ? 'err' : undefined,
          label: <>大模型{e.model && <span className="font-mono text-muted-foreground"> {e.model}</span>}{end ? <Lat ms={end.duration_ms} /> : <span className="text-muted-foreground animate-pulse text-xs"> 思考中…</span>}</>,
          body: end?.error ? <div className="text-xs text-destructive">{end.error}</div> : undefined });
        break;
      }
      case 'llm_end': if (!evs.some((x) => x.kind === 'llm_start')) steps.push({ key: 'k' + e.seq, icon: Brain, label: <>大模型<Lat ms={e.duration_ms} /></>, body: e.error ? <div className="text-xs text-destructive">{e.error}</div> : undefined }); break;
      case 'tool_call': { const id = e.call_id || String(e.seq); toolIdx[id] = steps.length; steps.push({ key: 'k' + e.seq, icon: Wrench, label: <ToolStep call={e} res={toolRes[id]} /> }); break; }
      case 'tool_result': { const id = e.call_id || String(e.seq); if (!toolCalls[id]) steps.push({ key: 'k' + e.seq, icon: Wrench, label: <ToolStep res={e} /> }); break; }
      case 'reply': if (!hasTts && e.text) steps.push({ key: 'k' + e.seq, icon: Mic, label: <>播报 <Badge variant="warning" className="text-[10px] px-1 py-0">未播报</Badge></>, body: <div className="text-sm whitespace-pre-wrap rounded-lg bg-muted/60 px-3 py-2 max-w-xl">{e.text}</div> }); break;
      case 'native_reply': steps.push({ key: 'k' + e.seq, icon: Ear, label: '小爱原生回复', body: e.text ? <div className="text-sm text-muted-foreground">{e.text}</div> : undefined }); break;
      case 'play_url': steps.push({ key: 'k' + e.seq, icon: Music, label: <>播放链接<Lat ms={e.latency_ms} /></>, body: <PlayUrlStep e={e} /> }); break;
      default: steps.push({ key: 'k' + e.seq, icon: Activity, label: <span className="text-muted-foreground text-xs">{e.kind}{e.text ? `：${e.text}` : ''}</span> });
    }
  }
  flushTts();
  // ask_detected first with latency
  const det = evs.find((e) => e.kind === 'ask_detected');
  if (det) steps.unshift({ key: 'det', icon: Radio, label: <>检测到提问<Lat ms={det.latency_ms} /></> });
  return steps;
}

function TurnCard({ id, evs, conversationId }: { id: string; evs: MockEvent[]; conversationId?: string }) {
  const [trace, setTrace] = useState(false);
  const ask = evs.find((e) => e.kind === 'ask');
  const det = evs.find((e) => e.kind === 'ask_detected');
  const start = evs.find((e) => e.kind === 'turn_start');
  const end = evs.find((e) => e.kind === 'turn_end');
  const route = start?.route || det?.route || ask?.route;
  const routeLabel = route ? ROUTES[route] || route : det?.handled === false ? '未接管' : undefined;
  const first = toMs(evs[0].ts), last = toMs(evs[evs.length - 1].ts);
  const dur = end?.duration_ms ?? (end && last > first ? last - first : undefined);
  const hasNative = evs.some((e) => e.kind === 'native_reply');
  const steps = buildSteps(evs);
  const real = id.startsWith('loose') ? undefined : id;
  return (
    <div className="mx-4 my-3 max-w-3xl rounded-xl border bg-card shadow-xs">
      <div className="flex flex-wrap items-center gap-2 px-4 py-2.5 border-b">
        <User className="w-3.5 h-3.5 text-blue-600 dark:text-blue-400 shrink-0" />
        <span className="font-medium text-sm flex-1 min-w-0 break-words">{ask?.text || det?.text || '（无提问文本）'}</span>
        {routeLabel && <Badge variant={route === 'ignored' ? 'secondary' : 'default'} className="text-[10px]">{routeLabel}</Badge>}
        {end?.canceled && <Badge variant="warning" className="text-[10px]">已取消</Badge>}
        {end ? (dur != null && <Lat ms={dur} />) : !hasNative && route !== 'ignored' && <span className="text-[10px] text-muted-foreground animate-pulse">进行中…</span>}
        <span className="text-[10px] text-muted-foreground">{fmtTime(evs[0].ts)}</span>
        <button type="button" onClick={() => setTrace(!trace)} className="text-[11px] text-primary hover:underline flex items-center gap-0.5"><Activity className="w-3 h-3" />追踪</button>
      </div>
      <div className="px-4 py-3">
        {steps.length === 0 && <div className="text-xs text-muted-foreground">等待后续步骤…</div>}
        <ol className="relative space-y-3 border-l border-border ml-2">
          {steps.map((st) => {
            const Icon = st.icon;
            const ic = st.tone === 'err' ? 'error' : iconStep(Icon);
            return (
              <li key={st.key} className="pl-5 relative">
                <span className={`absolute -left-[9px] top-0 w-[18px] h-[18px] rounded-full border bg-background flex items-center justify-center ${PALETTE[STEP[ic]].ring} ${PALETTE[STEP[ic]].chip}`}><Icon className="w-2.5 h-2.5" /></span>
                <div className="text-xs flex items-center gap-1.5 min-h-[18px]">{st.label}</div>
                {st.body && <div className="mt-1">{st.body}</div>}
              </li>
            );
          })}
          {end && <li className="pl-5 relative"><span className="absolute -left-[9px] top-0 w-[18px] h-[18px] rounded-full border bg-background flex items-center justify-center text-muted-foreground"><Flag className="w-2.5 h-2.5" /></span><div className="text-xs text-muted-foreground">本轮结束</div></li>}
        </ol>
      </div>
      {trace && <div className="border-t px-2 py-2"><TurnTraceTimeline conversationId={conversationId} turnId={real} /></div>}
    </div>
  );
}

export function SpeakerTimeline({
  messages, events, toolResults, calledIds, conversationId,
}: {
  messages: Message[]; events: MockEvent[];
  toolResults: Record<string, { result?: unknown; error?: string; durationMs?: number }>;
  calledIds: Set<string>; conversationId?: string;
}) {
  // group events per turn
  const groups: { id: string; evs: MockEvent[] }[] = [];
  const byId: Record<string, { id: string; evs: MockEvent[] }> = {};
  let loose = 0, curLoose = '';
  for (const e of [...events].sort((a, b) => a.seq - b.seq)) {
    let id = e.turn_id || '';
    if (!id) {
      if (e.kind === 'ask' || !curLoose) { curLoose = 'loose' + (++loose); }
      id = curLoose;
    } else if (!byId[id] && curLoose && byId[curLoose] && !byId[curLoose].evs.some((x) => x.turn_id)) {
      // the ask event carries no turn_id: fold the pending loose group into this turn
      byId[id] = byId[curLoose]; byId[id].id = id; delete byId[curLoose]; curLoose = id;
    }
    if (!byId[id]) { byId[id] = { id, evs: [] }; groups.push(byId[id]); }
    byId[id].evs.push(e);
  }
  // persisted history from before the in-memory event window
  const firstTs = events.length ? Math.min(...events.map((e) => toMs(e.ts)).filter(Boolean)) : Infinity;
  const history = events.length ? messages.filter((m) => toMs(m.created_at) && toMs(m.created_at) < firstTs - 1000) : messages;
  return (
    <>
      {history.map((m) => (
        <MessageBubble key={m.id} orphanTool={m.role === 'tool' && !calledIds.has(m.tool_call_id || '')} message={m} toolResultsMap={toolResults} isStreaming={false} conversationId={conversationId} />
      ))}
      {groups.map((g) => <TurnCard key={g.id} id={g.id} evs={g.evs} conversationId={conversationId} />)}
    </>
  );
}
