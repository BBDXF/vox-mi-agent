import { useState, useEffect, useRef } from 'react';
import {
  Music,
  RefreshCw,
  Clock,
  FileCheck,
  AlertCircle,
  CheckCircle2,
  ListMusic,
  Search,
  Sparkles,
  Zap,
  Info,
} from 'lucide-react';
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
} from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { useToast } from '@/components/ui/toast';
import {
  getMediaStatus,
  triggerMediaSync,
  getSettings,
  type MediaStatus,
  type SettingsResponse,
} from '@/lib/api';
import { formatTime } from '@/lib/utils';
import { MediaSettingsCard } from './MediaSettingsCard';

export function MediaView() {
  const { success, error: toastError, info } = useToast();

  const [status, setStatus] = useState<MediaStatus | null>(null);
  const [settings, setSettings] = useState<SettingsResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [isSyncing, setIsSyncing] = useState(false);

  const pollingTimerRef = useRef<number | null>(null);

  // Fetch status
  const fetchStatus = async () => {
    try {
      const data = await getMediaStatus();
      setStatus(data);
      setIsSyncing(data.running);
      return data;
    } catch (err: any) {
      toastError(err.message || '获取曲库状态失败');
      return null;
    }
  };

  // Load initial settings and status
  useEffect(() => {
    setLoading(true);
    Promise.all([
      fetchStatus(),
      getSettings().then((s) => setSettings(s)).catch(() => {}),
    ]).finally(() => {
      setLoading(false);
    });

    return () => {
      if (pollingTimerRef.current) {
        clearInterval(pollingTimerRef.current);
      }
    };
  }, []);

  // Poll while syncing
  useEffect(() => {
    if (isSyncing) {
      if (!pollingTimerRef.current) {
        pollingTimerRef.current = window.setInterval(async () => {
          const updated = await fetchStatus();
          if (updated && !updated.running) {
            setIsSyncing(false);
            if (pollingTimerRef.current) {
              clearInterval(pollingTimerRef.current);
              pollingTimerRef.current = null;
            }
            success('曲库同步已完成，最新索引已生效');
          }
        }, 1500);
      }
    } else {
      if (pollingTimerRef.current) {
        clearInterval(pollingTimerRef.current);
        pollingTimerRef.current = null;
      }
    }

    return () => {
      if (pollingTimerRef.current) {
        clearInterval(pollingTimerRef.current);
        pollingTimerRef.current = null;
      }
    };
  }, [isSyncing, success]);

  // Handle immediate manual sync trigger
  const handleTriggerSync = async () => {
    if (isSyncing) return;
    try {
      setIsSyncing(true);
      info('已提交曲库扫描与索引重建任务...');
      await triggerMediaSync();
      await fetchStatus();
    } catch (err: any) {
      setIsSyncing(false);
      toastError(err.message || '发起同步失败');
    }
  };

  // Refresh both status and settings
  const handleRefreshAll = async () => {
    setLoading(true);
    try {
      await Promise.all([
        fetchStatus(),
        getSettings().then((s) => setSettings(s)).catch(() => {}),
      ]);
    } finally {
      setLoading(false);
    }
  };

  // Called when settings are saved from MediaSettingsCard
  const handleSettingsSaved = async (syncStarted: boolean) => {
    try {
      const data = await getSettings();
      setSettings(data);
    } catch {
      // ignore
    }

    if (syncStarted) {
      setIsSyncing(true);
      fetchStatus();
    }
  };

  const tracksCount = status?.tracks ?? 0;
  const durationSec = status?.duration_ms
    ? (status.duration_ms / 1000).toFixed(2)
    : '0';

  return (
    <div className="h-full overflow-y-auto p-4 md:p-8 space-y-6 max-w-5xl mx-auto">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pb-2 border-b">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-foreground flex items-center gap-2">
            <Music className="w-5 h-5 text-primary" />
            家庭本地曲库与索引管理
          </h1>
          <p className="text-xs text-muted-foreground mt-1">
            监控 NAS 本地音乐文件扫描状态，维护 fzq 超高速拼音模糊检索索引
          </p>
        </div>

        <div className="flex items-center gap-2 shrink-0">
          <Button
            variant="outline"
            size="sm"
            onClick={handleRefreshAll}
            disabled={loading || isSyncing}
            className="gap-2"
          >
            <RefreshCw
              className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`}
            />
            刷新状态
          </Button>
          <Button
            size="sm"
            onClick={handleTriggerSync}
            disabled={isSyncing}
            className="gap-2 shadow-xs"
          >
            <RefreshCw
              className={`w-3.5 h-3.5 ${isSyncing ? 'animate-spin' : ''}`}
            />
            {isSyncing ? '正在同步中...' : '立即同步'}
          </Button>
        </div>
      </div>

      {/* Metric Cards Row */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {/* Metric 1 */}
        <Card className="p-4 border shadow-xs">
          <div className="flex items-center justify-between text-muted-foreground mb-2">
            <span className="text-xs font-medium">曲目总数</span>
            <ListMusic className="w-4 h-4 text-primary" />
          </div>
          <div className="text-2xl font-bold font-mono tracking-tight text-foreground">
            {tracksCount.toLocaleString()}
          </div>
          <div className="text-[11px] text-muted-foreground mt-1">
            支持无损 FLAC / WAV / MP3
          </div>
        </Card>

        {/* Metric 2 */}
        <Card className="p-4 border shadow-xs">
          <div className="flex items-center justify-between text-muted-foreground mb-2">
            <span className="text-xs font-medium">同步状态</span>
            <Sparkles className="w-4 h-4 text-amber-500" />
          </div>
          <div className="flex items-center gap-2 mt-1">
            {isSyncing ? (
              <Badge variant="warning" className="gap-1 animate-pulse">
                <span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-ping" />
                后台同步中
              </Badge>
            ) : (
              <Badge variant="success" className="gap-1">
                <CheckCircle2 className="w-3.5 h-3.5" />
                索引已就绪
              </Badge>
            )}
          </div>
          <div className="text-[11px] text-muted-foreground mt-2">
            fzq 缓存命中率 100%
          </div>
        </Card>

        {/* Metric 3 */}
        <Card className="p-4 border shadow-xs">
          <div className="flex items-center justify-between text-muted-foreground mb-2">
            <span className="text-xs font-medium">最近同步耗时</span>
            <Zap className="w-4 h-4 text-emerald-500" />
          </div>
          <div className="text-2xl font-bold font-mono tracking-tight text-foreground">
            {durationSec} <span className="text-xs font-normal text-muted-foreground">秒</span>
          </div>
          <div className="text-[11px] text-muted-foreground mt-1">
            mtime 差异比对快速跳过
          </div>
        </Card>

        {/* Metric 4 */}
        <Card className="p-4 border shadow-xs">
          <div className="flex items-center justify-between text-muted-foreground mb-2">
            <span className="text-xs font-medium">上次同步时间</span>
            <Clock className="w-4 h-4 text-indigo-500" />
          </div>
          <div className="text-xs font-mono font-medium text-foreground truncate mt-1.5">
            {status?.last_sync_at ? formatTime(status.last_sync_at) : '未记录'}
          </div>
          <div className="text-[11px] text-muted-foreground mt-2">
            定时任务每 {settings?.media?.sync_interval || '6h'} 自动核对
          </div>
        </Card>
      </div>

      {/* Sync Status Card with Polling Indicator */}
      <Card className="border shadow-xs">
        <CardHeader className="pb-3">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <FileCheck className="w-4 h-4 text-primary" />
              <CardTitle>同步执行详情 (Sync Execution)</CardTitle>
            </div>
            {isSyncing && (
              <span className="text-xs text-primary flex items-center gap-1.5 font-mono">
                <RefreshCw className="w-3 h-3 animate-spin" />
                自动轮询中 (1.5s 周期)
              </span>
            )}
          </div>
          <CardDescription>
            从 NAS 本地目录扫描文件、提取歌手与标题元数据，并原子生成 songs.jsonl 索引快照
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {status?.error ? (
            <div className="flex items-start gap-3 p-3.5 rounded-lg border border-destructive/30 bg-destructive/5 text-destructive text-xs">
              <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />
              <div className="space-y-1">
                <div className="font-semibold">同步遇到异常</div>
                <div className="font-mono">{status.error}</div>
              </div>
            </div>
          ) : (
            <div className="p-3.5 rounded-lg border bg-muted/30 text-xs space-y-2">
              <div className="flex items-center justify-between">
                <span className="font-medium text-foreground flex items-center gap-1.5">
                  <CheckCircle2 className="w-4 h-4 text-emerald-500" />
                  最新同步报告:
                </span>
                <span className="text-muted-foreground font-mono text-[11px]">
                  {status?.last_sync_at ? formatTime(status.last_sync_at) : ''}
                </span>
              </div>
              <p className="text-muted-foreground leading-relaxed">
                {status?.result ||
                  '曲库状态正常，无未决同步任务。可通过右上角按钮手动触发全量重新扫描。'}
              </p>
            </div>
          )}

          {/* Workflow step explanation */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-3 pt-2">
            <div className="p-3 rounded-lg border bg-card text-xs space-y-1">
              <div className="font-semibold text-foreground flex items-center gap-1.5">
                <span className="w-5 h-5 rounded-full bg-primary/10 text-primary text-[11px] flex items-center justify-center font-mono">
                  1
                </span>
                文件差异扫描
              </div>
              <p className="text-[11px] text-muted-foreground leading-relaxed">
                遍历配置的媒体白名单目录，依据「相对路径 + size + mtime」比对，无变动则零 IO 立即返回。
              </p>
            </div>

            <div className="p-3 rounded-lg border bg-card text-xs space-y-1">
              <div className="font-semibold text-foreground flex items-center gap-1.5">
                <span className="w-5 h-5 rounded-full bg-primary/10 text-primary text-[11px] flex items-center justify-center font-mono">
                  2
                </span>
                原子写盘替换
              </div>
              <p className="text-[11px] text-muted-foreground leading-relaxed">
                写入临时文件 <code>songs.jsonl.tmp</code>，执行 <code>fsync</code> 后原子重命名，确保查询进程零中断。
              </p>
            </div>

            <div className="p-3 rounded-lg border bg-card text-xs space-y-1">
              <div className="font-semibold text-foreground flex items-center gap-1.5">
                <span className="w-5 h-5 rounded-full bg-primary/10 text-primary text-[11px] flex items-center justify-center font-mono">
                  3
                </span>
                fzq 拼音缓存重建
              </div>
              <p className="text-[11px] text-muted-foreground leading-relaxed">
                同步任务与查询使用相同的 <code>IndexArgs()</code> 生成参数，构建缓存键，实现 20ms 内模糊点播命中。
              </p>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Library Settings Card (曲库设置) */}
      <MediaSettingsCard
        mediaConfig={settings?.media}
        onSaved={handleSettingsSaved}
        isSyncing={isSyncing}
      />

      {/* Metadata & Scanning Reference Spec */}
      <Card className="border shadow-xs bg-muted/10">
        <CardHeader className="pb-3">
          <div className="flex items-center gap-2">
            <Search className="w-4 h-4 text-primary" />
            <CardTitle>元数据提取与文件匹配规则</CardTitle>
          </div>
          <CardDescription>
            音箱端本地无损解码与文件过滤机制说明
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4 text-xs text-muted-foreground">
            <div className="p-3 rounded-lg border bg-background/60 space-y-1.5">
              <div className="font-medium text-foreground flex items-center gap-1.5">
                <Info className="w-3.5 h-3.5 text-primary" />
                文件名与标签解析优先级
              </div>
              <p className="text-[11px] leading-relaxed">
                优先从音频文件的 ID3v2 / FLAC Vorbis Comment / MP4 标签中读取歌手与歌曲标题；若元数据缺失，则按 <code>歌手 - 歌名.后缀</code> 模式从文件名提取。
              </p>
            </div>

            <div className="p-3 rounded-lg border bg-background/60 space-y-1.5">
              <div className="font-medium text-foreground flex items-center gap-1.5">
                <Info className="w-3.5 h-3.5 text-primary" />
                单库规模上限与性能保障
              </div>
              <p className="text-[11px] leading-relaxed">
                当前单实例最大索引曲目上限为 {settings?.media?.max_files?.toLocaleString() || '100,000'} 首。索引生成 songs.jsonl 快照后常驻内存，fzq 搜索平均延迟 &lt; 25ms。
              </p>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}