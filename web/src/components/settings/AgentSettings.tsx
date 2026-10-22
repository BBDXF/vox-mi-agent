import { useEffect, useState } from 'react';
import { Bot, Save, Loader2 } from 'lucide-react';
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { IconChip } from '@/components/ui/icon-chip';
import { useToast } from '@/components/ui/toast';
import { getAgentSettings, updateSectionSettings, ApiError, type AgentSettings as AS } from '@/lib/api';

export function Switch({ checked, onChange, label }: { checked: boolean; onChange: (v: boolean) => void; label?: string }) {
  return (
    <button type="button" role="switch" aria-checked={checked} aria-label={label} onClick={() => onChange(!checked)}
      className={`relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors ${checked ? 'bg-primary' : 'bg-muted-foreground/30'}`}>
      <span className={`inline-block h-4 w-4 rounded-full bg-background shadow transition-transform ${checked ? 'translate-x-4' : 'translate-x-0.5'}`} />
    </button>
  );
}

export function AgentSettings() {
  const { success, error: toastError } = useToast();
  const [data, setData] = useState<AS | null>(null);
  const [prompt, setPrompt] = useState('');
  const [maxSteps, setMaxSteps] = useState('6');
  const [temp, setTemp] = useState('');
  const [timeout, setTimeoutV] = useState('2m');
  const [hist, setHist] = useState('40');
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState(false);

  const apply = (d: AS) => {
    setData(d); setPrompt(d.system_prompt || ''); setMaxSteps(String(d.max_steps));
    setTemp(d.temperature == null ? '' : String(d.temperature)); setTimeoutV(d.turn_timeout);
    setHist(String(d.history_messages));
  };
  const load = () => getAgentSettings().then(apply).catch((e) => toastError(e.message || '加载失败'));
  useEffect(() => { load(); }, []);

  const save = async () => {
    setSaving(true); setErrors({});
    try {
      await updateSectionSettings('agent', {
        system_prompt: prompt,
        max_steps: Number(maxSteps),
        temperature: temp.trim() === '' ? null : Number(temp),
        turn_timeout: timeout.trim(),
        history_messages: Number(hist),
      });
      success('智能体设置已保存并生效');
      await load();
    } catch (e: any) {
      if (e instanceof ApiError && e.fields) setErrors(e.fields);
      toastError(e.message || '保存失败');
    } finally { setSaving(false); }
  };

  if (!data) return <div className="text-sm text-muted-foreground py-12 text-center">正在加载…</div>;
  const custom = prompt.trim() !== '';
  const Err = ({ k }: { k: string }) => errors[k] ? <p className="text-xs text-destructive mt-1">{errors[k]}</p> : null;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2"><IconChip icon={Bot} color="agent" />智能体</CardTitle>
        <CardDescription>提示词与运行参数，保存后立即生效，无需重启。</CardDescription>
      </CardHeader>
      <CardContent className="space-y-6">
        <section className="space-y-2">
          <div className="flex flex-wrap items-center justify-between gap-2">
            <label className="text-sm font-medium">系统提示词
              <span className="ml-2 text-xs font-normal text-muted-foreground">当前使用：{custom ? '自定义' : '默认'}</span>
            </label>
            <div className="flex gap-2">
              <Button type="button" variant="outline" size="sm" onClick={() => setPrompt('')} disabled={!custom}>使用默认</Button>
              <Button type="button" variant="outline" size="sm" onClick={() => setPrompt(data.default_system_prompt)}>以默认为基础编辑</Button>
            </div>
          </div>
          <textarea value={prompt} onChange={(e) => setPrompt(e.target.value)} placeholder={data.default_system_prompt} rows={14}
            className="w-full rounded-md border bg-background p-3 font-mono text-xs leading-relaxed outline-none focus:ring-2 focus:ring-ring resize-y" />
          <p className="text-xs text-muted-foreground">留空表示使用默认提示词。</p>
          <Err k="system_prompt" />
        </section>

        <section className="space-y-3"><h3 className="text-sm font-medium">运行参数</h3><div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <div>
            <label className="text-sm font-medium">最大步数</label>
            <Input type="number" min={1} max={50} value={maxSteps} onChange={(e) => setMaxSteps(e.target.value)} />
            <p className="text-xs text-muted-foreground mt-1">单轮对话最多调用工具的步数（1–50）</p><Err k="max_steps" />
          </div>
          <div>
            <label className="text-sm font-medium">温度</label>
            <Input type="number" step="0.1" min={0} max={2} value={temp} placeholder="跟随大模型设置" onChange={(e) => setTemp(e.target.value)} />
            <p className="text-xs text-muted-foreground mt-1">0–2，留空跟随大模型设置</p><Err k="temperature" />
          </div>
          <div>
            <label className="text-sm font-medium">单轮超时</label>
            <Input value={timeout} placeholder="2m" onChange={(e) => setTimeoutV(e.target.value)} />
            <p className="text-xs text-muted-foreground mt-1">如 120s、2m；0 = 不限</p><Err k="turn_timeout" />
          </div>
          <div>
            <label className="text-sm font-medium">历史消息数</label>
            <Input type="number" min={0} max={1000} value={hist} onChange={(e) => setHist(e.target.value)} />
            <p className="text-xs text-muted-foreground mt-1">0 = 全部，最大 1000</p><Err k="history_messages" />
          </div>
        </div></section>
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
