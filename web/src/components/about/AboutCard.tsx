import { useEffect, useState } from 'react';
import { Info, Copy, ExternalLink } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { IconChip } from '@/components/ui/icon-chip';
import { useToast } from '@/components/ui/toast';
import { getHealth } from '@/lib/api';
import { getVersion, humanizeUptime, RELEASES_URL, type VersionInfo } from '@/lib/version';

export function AboutCard() {
  const [v, setV] = useState<VersionInfo | null>(null);
  const { success, error } = useToast();

  useEffect(() => { getVersion().then(setV); }, []);

  const copy = async () => {
    const health = await getHealth().catch((e) => ({ error: String(e?.message || e) }));
    const text = JSON.stringify({ version: v, healthz: health, ua: navigator.userAgent }, null, 2);
    try {
      await navigator.clipboard.writeText(text);
      success('诊断信息已复制');
    } catch {
      error('复制失败，请手动选择');
    }
  };

  const rows: Array<[string, string | undefined]> = v ? [
    ['版本', v.version], ['提交', v.commit], ['构建时间', v.build_date],
    ['Go 版本', v.go_version], ['系统 / 架构', v.os ? `${v.os} / ${v.arch ?? ''}` : undefined],
    ['启动时间', v.started_at], ['已运行', v.uptime_s != null ? humanizeUptime(v.uptime_s) : undefined],
    ['数据目录', v.data_dir],
    ...Object.entries(v.tools || {}).map(([k, val]) => [`工具 ${k}`, val] as [string, string]),
  ] : [];

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-base"><IconChip icon={Info} color="about" />关于</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {!v ? <p className="text-sm text-muted-foreground">加载中…</p> : v.version === 'dev' && !v.commit ? (
          <p className="text-sm text-muted-foreground">开发版（后端未提供版本信息）</p>
        ) : (
          <dl className="grid grid-cols-[7rem_1fr] gap-y-2 text-sm">
            {rows.filter(([, val]) => val).map(([k, val]) => (
              <div key={k} className="contents">
                <dt className="text-muted-foreground">{k}</dt>
                <dd className="font-mono text-xs break-all self-center">{val}</dd>
              </div>
            ))}
          </dl>
        )}
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" size="sm" onClick={() => window.open(RELEASES_URL, '_blank', 'noopener')}>
            <ExternalLink className="w-3.5 h-3.5 mr-1" />GitHub 发布页
          </Button>
          <Button variant="outline" size="sm" onClick={copy}>
            <Copy className="w-3.5 h-3.5 mr-1" />复制诊断信息
          </Button>
        </div>
      </CardContent>
    </Card>
  );
}
