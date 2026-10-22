import { useEffect, useState, useCallback } from 'react';
import { BookOpen, CheckCircle2, AlertTriangle, RefreshCw } from 'lucide-react';
import { IconChip } from '@/components/ui/icon-chip';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { getMediaStatus } from '@/lib/api';
import { getSpeakerStatus } from '@/lib/speakerApi';
import type { NavTab } from '@/components/layout/Sidebar';

interface Step {
  title: string;
  done: boolean | null; // null = always available
  desc: string;
  tip?: string;
  action: string;
  tab: NavTab;
  section?: string;
}

const getJson = (u: string) => fetch(u).then((r) => (r.ok ? r.json() : null)).catch(() => null);

export function GuideView({ onGo }: { onGo: (tab: NavTab, section?: string) => void }) {
  const [steps, setSteps] = useState<Step[] | null>(null);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    const [llm, media, spk, st] = await Promise.all([
      getJson('api/settings/llm'),
      getMediaStatus().catch(() => null),
      getJson('api/settings/speaker'),
      getSpeakerStatus().catch(() => null),
    ]);
    const tracks = media?.tracks ?? 0;
    const url: string = (spk?.public_base_url || '').trim();
    const urlOk = !!url && !/(127\.0\.0\.1|localhost)/i.test(url);
    const loggedIn = st?.account?.state === 'logged_in';
    const devs = spk?.xiaomi?.devices;
    const saved = Array.isArray(devs) && devs.length > 0 && spk?.driver === 'xiaomi';
    const talked = (st?.devices || []).some((d) => d.device_id !== 'debug' && !!d.conversation_id);
    setSteps([
      { title: '配置大模型', done: !!(llm?.has_api_key && llm?.model), action: '去设置大模型', tab: 'settings', section: 'llm',
        desc: '大模型负责理解你的话。填入 API Key 并选择模型即可。', tip: '推荐硅基流动 Qwen/Qwen3-8B（免费额度、速度快）。' },
      { title: '添加音乐目录', done: tracks > 0, action: '去曲库', tab: 'media',
        desc: tracks > 0 ? `曲库已有 ${tracks} 首歌。` : '告诉系统你的歌放在哪里，并同步曲库。',
        tip: 'Docker 部署时音乐目录要先挂载进容器（例如 /music），这里填容器内路径。' },
      { title: '试一下调试音箱', done: null, action: '去对话页', tab: 'chat',
        desc: '不需要真实音箱，在对话页对「调试音箱」说一句就能看到效果。', tip: '试试输入：播放晴天' },
      { title: '设置对外地址', done: urlOk, action: '去设置音箱', tab: 'settings', section: 'speaker',
        desc: '小爱音箱通过这个地址拉取歌曲，需填 NAS 的局域网 IP 和端口，例如 http://192.168.1.10:8081。' },
      { title: '登录小米账号', done: loggedIn, action: '去登录', tab: 'settings', section: 'speaker',
        desc: '在音箱设置中选择「小米音箱」驱动并登录。密码不会被保存；如需验证，按页面提示收验证码。' },
      { title: '添加并保存音箱', done: saved, action: '去添加音箱', tab: 'settings', section: 'speaker',
        desc: '登录后发现你的小爱音箱，勾选添加并保存配置。' },
      { title: '对音箱说话', done: talked, action: '看对话记录', tab: 'chat',
        desc: '对真实音箱说：「小爱同学，播放晴天」。有对话记录即视为完成。' },
    ]);
    setLoading(false);
  }, []);

  useEffect(() => { load(); }, [load]);

  const render = (s: Step, n: number) => (
    <div key={n} className="flex items-start gap-3 p-3 rounded-lg border bg-card/50">
      {s.done === false
        ? <AlertTriangle className="w-5 h-5 text-amber-500 shrink-0 mt-0.5" />
        : <CheckCircle2 className={`w-5 h-5 shrink-0 mt-0.5 ${s.done ? 'text-green-500' : 'text-amber-500/60'}`} />}
      <div className="flex-1 min-w-0 space-y-1">
        <div className="flex items-center gap-2 flex-wrap">
          <span className="inline-flex items-center justify-center w-5 h-5 rounded-full bg-amber-500/10 text-amber-600 dark:text-amber-400 text-[11px] font-mono">{n}</span>
          <span className="text-sm font-semibold">{s.title}</span>
          {s.done === true && <Badge variant="success">✅ 已完成</Badge>}
          {s.done === false && <Badge variant="warning">⚠️ 待配置</Badge>}
        </div>
        <p className="text-xs text-muted-foreground leading-relaxed">{s.desc}</p>
        {s.tip && <p className="text-xs text-amber-700 dark:text-amber-400">💡 {s.tip}</p>}
      </div>
      <Button size="sm" variant={s.done === false ? 'default' : 'outline'} className="shrink-0" onClick={() => onGo(s.tab, s.section)}>{s.action}</Button>
    </div>
  );

  return (
    <div className="h-full overflow-y-auto">
      <div className="max-w-3xl mx-auto p-4 sm:p-6 space-y-6">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h1 className="text-xl font-bold tracking-tight flex items-center gap-2"><IconChip icon={BookOpen} color="guide" />快速开始</h1>
            <p className="text-sm text-muted-foreground mt-1">按下面的步骤配置，几分钟就能让音箱听你的话点歌。状态会自动检测。</p>
          </div>
          <Button variant="outline" size="sm" onClick={load} disabled={loading}>
            <RefreshCw className={`w-3.5 h-3.5 mr-1 ${loading ? 'animate-spin' : ''}`} />重新检测
          </Button>
        </div>

        {!steps ? <p className="text-sm text-muted-foreground">检测中…</p> : (
          <>
            <div className="space-y-2">{steps.slice(0, 3).map((s, i) => render(s, i + 1))}</div>
            <div className="space-y-2">
              <h2 className="text-sm font-semibold">接入小米音箱（可选）</h2>
              {steps.slice(3).map((s, i) => render(s, i + 4))}
            </div>
          </>
        )}

        <Card>
          <CardHeader><CardTitle className="text-base">怎么用</CardTitle></CardHeader>
          <CardContent className="text-sm space-y-2 text-muted-foreground">
            <p>点歌说法示例：「播放晴天」「来一首晴天」「我想听晴天」「来点周杰伦的歌」。</p>
            <p>其他问题仍由小爱自己回答，不会被拦截。</p>
            <p>想把问题交给 Agent（如「问问…」），需要开启关键词模式。</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader><CardTitle className="text-base">常见问题</CardTitle></CardHeader>
          <CardContent className="text-sm space-y-3 text-muted-foreground">
            <p><b className="text-foreground">音箱没声音？</b> 用手机浏览器打开对外地址，确认能访问。</p>
            <p><b className="text-foreground">没打断小爱？</b> 在音箱设置里调大打断窗口。</p>
            <p><b className="text-foreground">登录要验证？</b> 收验证码填入，或在浏览器完成验证后重试。</p>
            <p><b className="text-foreground">搜不到歌？</b> 看曲库是否已同步，必要时给歌曲或歌手加别名。</p>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
