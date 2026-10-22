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

const blank = (): McpServer => ({
  name: '', enabled: true, transport: 'http', url: '', headers: {}, command: '', args: [], env: {}, timeout: '15s', tools_allow: [],
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

function ServerEditor({ s, idx, errors, status, onChange, onRemove }: {
  s: McpServer; idx: number; errors: Record<string, string>; status?: McpStatusItem;
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
    else { dot = 'bg-red-500'; label = `错误：${status.error || '未连接'}`; }
  }
  const tools = status?.tools || [];

  return (
    <div className="rounded-lg border p-4 space-y-3">
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
          <Input value={s.timeout} placeholder="15s" onChange={(e) => set({ timeout: e.target.value })} /><E k="timeout" />
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
          <Input className="font-mono text-xs" value={s.url} placeholder="https://example.com/mcp" onChange={(e) => set({ url: e.target.value })} /><E k="url" /></div>
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
          <ServerEditor key={`${ver}-${i}`} s={s} idx={i} errors={errors}
            status={status.find((x) => x.name === s.name)}
            onChange={(n) => setServers(servers.map((x, j) => j === i ? n : x))}
            onRemove={() => { setServers(servers.filter((_, j) => j !== i)); setVer((v) => v + 1); }} />
        ))}
        {errors.servers && <p className="text-xs text-destructive">{errors.servers}</p>}
        <Button type="button" variant="outline" className="gap-1.5" onClick={() => setServers([...servers, blank()])}><Plus className="w-4 h-4" />添加服务器</Button>
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
