import { useEffect, useState } from 'react';
import { Plug, Save, Loader2, Plus, Trash2, ChevronDown, ChevronRight, Zap } from 'lucide-react';
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { IconChip } from '@/components/ui/icon-chip';
import { useToast } from '@/components/ui/toast';
import {
  getMcpSettings, getMcpStatus, testMcpServer, updateSectionSettings, ApiError,
  type McpServer, type McpStatusItem, type McpTestResult,
} from '@/lib/api';
import { Switch } from './AgentSettings';
import { MCP_PRESETS, type McpPreset } from '@/lib/mcpPresets';

const blank = (): McpServer => ({
  name: '', enabled: false, transport: 'http', url: '', headers: {}, command: '', args: [], env: {}, timeout: '60s', tools_allow: [],
});

function KV({ value, onChange, kp, vp }: { value: Record<string, string>; onChange: (v: Record<string, string>) => void; kp: string; vp: string }) {
  const [rows, setRows] = useState<[string, string][]>(Object.entries(value));
  const commit = (r: [string, string][]) => {
    setRows(r);
    const o: Record<string, string> = {};
    r.forEach(([k, v]) => { if (k.trim()) o[k.trim()] = v; });
    onChange(o);
  };
  return (
    <div className="space-y-1.5">
      {rows.map(([k, v], i) => (
        <div key={i} className="flex gap-2">
          <Input className="font-mono text-xs" placeholder={kp} value={k} onChange={(e) => commit(rows.map((r, j) => j === i ? [e.target.value, r[1]] : r))} />
          <Input className="font-mono text-xs" placeholder={vp} value={v} onChange={(e) => commit(rows.map((r, j) => j === i ? [r[0], e.target.value] : r))} />
          <Button type="button" variant="ghost" size="icon" onClick={() => commit(rows.filter((_, j) => j !== i))}><Trash2 className="w-4 h-4" /></Button>
        </div>
      ))}
      <Button type="button" variant="outline" size="sm" className="gap-1" onClick={() => commit([...rows, ['', '']])}><Plus className="w-3.5 h-3.5" />添加</Button>
    </div>
  );
}

function ServerEditor({ s, idx, errors, status, highlight, onChange, onRemove }: {
  s: McpServer; idx: number; errors: Record<string, string>; status?: McpStatusItem; highlight?: boolean;
  onChange: (s: McpServer) => void; onRemove: () => void;
}) {
  const [open, setOpen] = useState(false);
  const [testing, setTesting] = useState(false);
  const [res, setRes] = useState<McpTestResult | null>(null);
  const [argsText, setArgsText] = useState((s.args || []).join('\n'));
  const [allowText, setAllowText] = useState((s.tools_allow || []).join(', '));
  const set = (p: Partial<McpServer>) => onChange({ ...s, ...p });
  const err = (k: string) => errors[`servers[${idx}].${k}`];
  const E = ({ k }: { k: string }) => err(k) ? <p className="text-xs text-destructive mt-1">{err(k)}</p> : null;

  const test = async () => {
    setTesting(true); setRes(null);
    try { setRes(await testMcpServer(s)); }
    catch (e: any) { setRes({ ok: false, error: e.message || '测试失败' }); }
    finally { setTesting(false); }
  };

  let dot = 'bg-muted-foreground/40', label = '未保存';
  if (status) {
    if (!status.enabled) { dot = 'bg-muted-foreground/40'; label = '已停用'; }
    else if (status.connected) { dot = 'bg-green-500'; label = '已连接'; }
    else if (status.retrying) { dot = 'bg-yellow-500'; label = `重试中（下次 ${status.next_retry ? new Date(status.next_retry).toLocaleTimeString('zh-CN', { hour12: false }) : '—'}）：${status.error || ''}`; }
    else { dot = 'bg-red-500'; label = `错误：${status.error || '未连接'}`; }
  }
  const tools = status?.tools || [];

  return (
    <div ref={(el) => { if (el && highlight && !el.dataset.seen) { el.dataset.seen = '1'; el.scrollIntoView({ block: 'center', behavior: 'smooth' }); el.querySelector<HTMLInputElement>('input[data-key]')?.focus({ preventScroll: true }); } }}
      className={`rounded-lg border p-4 space-y-3 ${highlight ? 'ring-2 ring-amber-500/60' : ''}`}>
      <div className="flex items-center gap-3">
        <Switch checked={s.enabled} onChange={(v) => set({ enabled: v })} label="启用" />
        <Input className="max-w-[200px] font-mono text-sm" placeholder="名称，如 web" value={s.name} onChange={(e) => set({ name: e.target.value })} />
        <div className="flex-1 min-w-0 flex items-center gap-1.5 text-xs text-muted-foreground">
          <span className={`w-2 h-2 rounded-full shrink-0 ${dot}`} /><span className="truncate" title={label}>{label}</span>
        </div>
        <Button type="button" variant="ghost" size="icon" onClick={onRemove} aria-label="删除"><Trash2 className="w-4 h-4" /></Button>
      </div>
      <E k="name" />

      {tools.length > 0 && (
        <div>
          <button type="button" onClick={() => setOpen(!open)} className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground">
            {open ? <ChevronDown className="w-3.5 h-3.5" /> : <ChevronRight className="w-3.5 h-3.5" />}工具 ({tools.length})
          </button>
          {open && <ul className="mt-1.5 space-y-1 pl-4">{tools.map((t) => (
            <li key={t.name} className="text-xs"><span className="font-mono font-semibold">{t.name}</span>
              {t.description && <span className="text-muted-foreground"> — {t.description}</span>}</li>))}</ul>}
        </div>
      )}

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div>
          <label className="text-xs font-medium">传输方式</label>
          <select value={s.transport} onChange={(e) => set({ transport: e.target.value as McpServer['transport'] })}
            className="w-full h-9 rounded-md border bg-background px-2 text-sm">
            <option value="http">HTTP (Streamable)</option><option value="sse">SSE</option><option value="stdio">stdio</option>
          </select><E k="transport" />
        </div>
        <div>
          <label className="text-xs font-medium">超时</label>
          <Input value={s.timeout} placeholder="60s" onChange={(e) => set({ timeout: e.target.value })} /><E k="timeout" />
        </div>
      </div>

      {s.transport === 'stdio' ? (<>
        <p className="text-xs text-muted-foreground">命令在容器内运行（镜像为 Alpine，仅有基础工具）。</p>
        <div><label className="text-xs font-medium">命令</label>
          <Input className="font-mono text-xs" value={s.command} placeholder="npx" onChange={(e) => set({ command: e.target.value })} /><E k="command" /></div>
        <div><label className="text-xs font-medium">参数（每行一个）</label>
          <textarea rows={3} value={argsText} className="w-full rounded-md border bg-background p-2 font-mono text-xs"
            onChange={(e) => { setArgsText(e.target.value); set({ args: e.target.value.split('\n').map((x) => x.trim()).filter(Boolean) }); }} /><E k="args" /></div>
        <div><label className="text-xs font-medium">环境变量</label>
          <KV value={s.env || {}} onChange={(env) => set({ env })} kp="KEY" vp="值" /><E k="env" /></div>
      </>) : (<>
        <div><label className="text-xs font-medium">URL</label>
          <Input data-key className={`font-mono text-xs ${highlight ? 'border-amber-500' : ''}`} value={s.url} placeholder="https://example.com/mcp" onChange={(e) => set({ url: e.target.value })} /><E k="url" /></div>
        <div><label className="text-xs font-medium">请求头</label>
          <KV value={s.headers || {}} onChange={(headers) => set({ headers })} kp="Header" vp="值" /><E k="headers" /></div>
      </>)}

      <div><label className="text-xs font-medium">允许的工具</label>
        <Input className="font-mono text-xs" value={allowText} placeholder="留空 = 全部；逗号或换行分隔"
          onChange={(e) => { setAllowText(e.target.value); set({ tools_allow: e.target.value.split(/[,\n]/).map((x) => x.trim()).filter(Boolean) }); }} /><E k="tools_allow" /></div>

      <div className="flex items-center gap-3 pt-1">
        <Button type="button" variant="outline" size="sm" onClick={test} disabled={testing} className="gap-1.5">
          {testing ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <Zap className="w-3.5 h-3.5" />}测试连接
        </Button>
        {res && (
          <span className={`text-xs ${res.ok ? 'text-green-600 dark:text-green-400' : 'text-destructive'}`}>
            {res.ok ? `连接成功 · ${res.latency_ms ?? 0}ms · ${res.tools?.length ?? 0} 个工具` : `失败：${res.error}`}
          </span>
        )}
      </div>
    </div>
  );
}

export function McpSettings() {
  const { success, error: toastError } = useToast();
  const [servers, setServers] = useState<McpServer[] | null>(null);
  const [status, setStatus] = useState<McpStatusItem[]>([]);
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);
  const [ver, setVer] = useState(0); // remount editors on reload
  const [rts, setRts] = useState<{ name: string; installed: boolean; installing: boolean }[] | null>(null);
  const [hl, setHl] = useState<number | null>(null);

  const loadStatus = () => getMcpStatus().then((r) => setStatus(r.servers || [])).catch(() => {});
  const load = async () => {
    try { const r = await getMcpSettings(); setServers(r.servers || []); setVer((v) => v + 1); }
    catch (e: any) { toastError(e.message || '加载失败'); }
    loadStatus();
  };
  useEffect(() => { load(); }, []);

  const save = async () => {
    setSaving(true); setErrors({});
    try {
      await updateSectionSettings('mcp', { servers });
      success('MCP 设置已保存');
      await load();
    } catch (e: any) {
      if (e instanceof ApiError && e.fields) setErrors(e.fields);
      toastError(e.message || '保存失败');
    } finally { setSaving(false); }
  };

  const loadRts = () => fetch('api/runtimes').then((r) => r.json()).then((l) => setRts(Array.isArray(l) ? l : [])).catch(() => {});
  useEffect(() => { loadRts(); }, []);
  const installing = !!rts?.some((r) => r.installing);
  useEffect(() => {
    if (!installing) return;
    const t = setInterval(loadRts, 2000);
    return () => clearInterval(t);
  }, [installing]);
  const installRt = async (n: string) => {
    try {
      const r = await fetch(`api/runtimes/${n}/install`, { method: 'POST' });
      if (!r.ok) throw new Error(`安装失败 (${r.status})`);
    } catch (e: any) { toastError(e.message || '安装失败'); }
    loadRts();
  };
  const addPreset = (p: McpPreset) => {
    if (!servers) return;
    let name = p.id, n = 1;
    while (servers.some((s) => s.name === name)) name = `${p.id}${++n}`;
    setServers([...servers, { ...blank(), ...structuredClone(p.server), name, enabled: false } as McpServer]);
    setVer((v) => v + 1);
    setHl(p.key ? servers.length : null);
    if (p.key) success('请把尖括号占位值替换为你的密钥');
  };

  if (!servers) return <div className="text-sm text-muted-foreground py-12 text-center">正在加载…</div>;
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2"><IconChip icon={Plug} color="mcp" />MCP 服务</CardTitle>
        <CardDescription>接入外部 MCP 服务器，为智能体扩展工具。请求头与环境变量的值已脱敏，保持不变即可保留原值。</CardDescription>
      </CardHeader>
      <CardContent className="space-y-4">
        {servers.length === 0 && <p className="text-sm text-muted-foreground text-center py-6">尚未配置 MCP 服务器</p>}
        {servers.map((s, i) => (
          <ServerEditor key={`${ver}-${i}`} s={s} idx={i} errors={errors} highlight={hl === i}
            status={status.find((x) => x.name === s.name)}
            onChange={(n) => setServers(servers.map((x, j) => j === i ? n : x))}
            onRemove={() => { setServers(servers.filter((_, j) => j !== i)); setVer((v) => v + 1); }} />
        ))}
        {errors.servers && <p className="text-xs text-destructive">{errors.servers}</p>}
        <Button type="button" variant="outline" className="gap-1.5" onClick={() => setServers([...servers, blank()])}><Plus className="w-4 h-4" />添加服务器</Button>

        <div className="rounded-lg border bg-muted/30 p-3 space-y-2.5">
          <div className="text-sm font-medium">常用 MCP</div>
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
            {MCP_PRESETS.map((p) => {
              const missing = p.needs && rts && rts.some((r) => r.name === p.needs && !r.installed);
              const installing = p.needs && rts?.some((r) => r.name === p.needs && r.installing);
              return (
                <div key={p.id} className="rounded-md border bg-background p-2.5 flex flex-col gap-1.5">
                  <div className="flex items-center gap-2">
                    <span className="font-mono text-xs font-semibold">{p.id}</span>
                    {p.needs && <span className="text-[10px] rounded bg-secondary px-1.5 py-0.5">需要 {p.needs}</span>}
                    {p.key && <span className="text-[10px] rounded bg-amber-500/15 text-amber-700 dark:text-amber-400 px-1.5 py-0.5">需要密钥</span>}
                    <Button type="button" size="sm" variant="outline" className="ml-auto h-6 px-2 text-xs gap-1" onClick={() => addPreset(p)}>
                      <Plus className="w-3 h-3" />添加
                    </Button>
                  </div>
                  <p className="text-xs text-muted-foreground">{p.desc}</p>
                  {missing && (
                    <div className="flex items-center gap-2 text-xs text-amber-700 dark:text-amber-400">
                      <span>尚未安装 {p.needs}</span>
                      <Button type="button" size="sm" className="h-6 px-2 text-xs gap-1" disabled={!!installing} onClick={() => installRt(p.needs!)}>
                        {installing && <Loader2 className="w-3 h-3 animate-spin" />}安装 {p.needs}
                      </Button>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
          <p className="text-xs text-muted-foreground">首次启动需下载依赖，测试连接可能要 10–60 秒。添加后默认停用，填好密钥、测试通过后再启用并保存。</p>
        </div>
      </CardContent>
      <CardFooter className="justify-end gap-2">
        <Button variant="outline" onClick={load} disabled={saving}>重置</Button>
        <Button onClick={save} disabled={saving} className="gap-2">
          {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}保存
        </Button>
      </CardFooter>
    </Card>
  );
}
