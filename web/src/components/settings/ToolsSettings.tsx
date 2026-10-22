import { useEffect, useState } from 'react';
import { Wrench, Save, Loader2 } from 'lucide-react';
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

export function ToolsSettings() {
  return (
    <div className="space-y-6">
      <BuiltinToolsCard />
      <McpSettings />
    </div>
  );
}
