import { useEffect, useState } from 'react';
import { Wrench, Terminal, Save, Loader2 } from 'lucide-react';
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { IconChip } from '@/components/ui/icon-chip';
import { useToast } from '@/components/ui/toast';
import { getAgentSettings, updateSectionSettings, type AgentSettings as AS } from '@/lib/api';
import { Switch } from './AgentSettings';
import { McpSettings } from './McpSettings';

function BuiltinToolsCard() {
  const { success, error: toastError } = useToast();
  const [data, setData] = useState<AS | null>(null);
  const [disabled, setDisabled] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);
  const load = () => getAgentSettings().then((d) => { setData(d); setDisabled(d.tools_disabled || []); }).catch((e) => toastError(e.message || '加载失败'));
  useEffect(() => { load(); }, []);
  const toggle = (name: string, on: boolean) =>
    setDisabled((p) => on ? p.filter((n) => n !== name) : p.includes(name) ? p : [...p, name]);
  const save = async () => {
    setSaving(true);
    try {
      await updateSectionSettings('agent', { tools_disabled: disabled });
      success('内置工具设置已保存并生效');
      await load();
    } catch (e: any) { toastError(e.message || '保存失败'); } finally { setSaving(false); }
  };
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2"><IconChip icon={Wrench} color="tools" />内置工具</CardTitle>
        <CardDescription>关闭后智能体将不再调用该工具，保存后立即生效。</CardDescription>
      </CardHeader>
      <CardContent>
        {!data ? <div className="text-sm text-muted-foreground py-6 text-center">正在加载…</div> : (
          <div className="divide-y rounded-md border">
            {data.available_tools.map((t) => (
              <div key={t.name} className="flex items-start justify-between gap-3 p-3">
                <div className="min-w-0">
                  <div className="font-mono text-xs font-semibold">{t.name}</div>
                  <div className="text-xs text-muted-foreground mt-0.5">{t.description}</div>
                </div>
                <Switch label={t.name} checked={!disabled.includes(t.name)} onChange={(v) => toggle(t.name, v)} />
              </div>
            ))}
          </div>
        )}
      </CardContent>
      <CardFooter className="justify-end gap-2">
        <Button variant="outline" onClick={load} disabled={saving}>重置</Button>
        <Button onClick={save} disabled={saving || !data} className="gap-2">
          {saving ? <Loader2 className="w-4 h-4 animate-spin" /> : <Save className="w-4 h-4" />}保存
        </Button>
      </CardFooter>
    </Card>
  );
}

type Runtime = { name: string; builtin: boolean; installed: boolean; version: string; installing: boolean; error: string };
const RT_HINT: Record<string, string> = {
  bun: '提供 npx/bunx，用于 Node 类 MCP',
  node: '提供 npx/bunx，用于 Node 类 MCP',
  uv: '提供 uvx，用于 Python 类 MCP',
};

async function rtCall(path: string, method: string) {
  const res = await fetch(path, { method });
  if (!res.ok) {
    let msg = `请求失败 (${res.status})`;
    try { const j = await res.json(); msg = j.error || j.message || msg; } catch { /* ignore */ }
    throw new Error(msg);
  }
  return res;
}

function RuntimesCard() {
  const { success, error: toastError } = useToast();
  const [list, setList] = useState<Runtime[] | null>(null);
  const load = () => rtCall('api/runtimes', 'GET').then((r) => r.json()).then(setList).catch((e) => toastError(e.message || '加载失败'));
  useEffect(() => { load(); }, []);
  const anyInstalling = !!list?.some((r) => r.installing);
  useEffect(() => {
    if (!anyInstalling) return;
    const t = setInterval(() => {
      rtCall('api/runtimes', 'GET').then((r) => r.json()).then(setList).catch(() => {});
    }, 2000);
    return () => clearInterval(t);
  }, [anyInstalling]);

  const install = async (n: string) => {
    try { await rtCall(`api/runtimes/${n}/install`, 'POST'); await load(); }
    catch (e: any) { toastError(e.message || '安装失败'); }
  };
  const remove = async (n: string) => {
    if (!window.confirm(`确定卸载 ${n} 吗？`)) return;
    try { await rtCall(`api/runtimes/${n}`, 'DELETE'); success(`${n} 已卸载`); await load(); }
    catch (e: any) { toastError(e.message || '卸载失败'); }
  };
  const builtins = (list || []).filter((r) => r.builtin);
  const extras = (list || []).filter((r) => !r.builtin);
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2"><IconChip icon={Terminal} color="tools" />运行环境</CardTitle>
        <CardDescription>安装到 /data/runtime，需联网；MCP 本地命令与 run_shell 自动可用。</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {!list ? <div className="text-sm text-muted-foreground py-6 text-center">正在加载…</div> : (
          <>
            {builtins.length > 0 && (
              <div className="text-sm text-muted-foreground">已内置：<span className="font-mono text-foreground">{builtins.map((b) => b.name).join(' · ')}</span></div>
            )}
            <div className="divide-y rounded-md border">
              {extras.map((r) => (
                <div key={r.name} className="flex items-center justify-between gap-3 p-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-xs font-semibold">{r.name}</span>
                      <span className={`text-xs ${r.installing ? 'text-amber-600' : r.installed ? 'text-emerald-600' : 'text-muted-foreground'}`}>
                        {r.installing ? '安装中…' : r.installed ? `已安装 ${r.version ? (r.version.startsWith('v') ? r.version : 'v' + r.version) : ''}` : '未安装'}
                      </span>
                    </div>
                    <div className="text-xs text-muted-foreground mt-0.5">{RT_HINT[r.name]}</div>
                    {r.error && <div className="text-xs text-destructive mt-0.5 break-all">{r.error}</div>}
                  </div>
                  {r.installing ? (
                    <Button size="sm" variant="outline" disabled className="gap-2"><Loader2 className="w-4 h-4 animate-spin" />安装中</Button>
                  ) : r.installed ? (
                    <Button size="sm" variant="outline" onClick={() => remove(r.name)}>卸载</Button>
                  ) : (
                    <Button size="sm" onClick={() => install(r.name)}>安装</Button>
                  )}
                </div>
              ))}
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}

export function ToolsSettings() {
  return (
    <div className="space-y-6">
      <BuiltinToolsCard />
      <RuntimesCard />
      <McpSettings />
    </div>
  );
}
