import { useState, useEffect } from 'react';
import {
  Wrench,
  Database,
  Volume2,
  RefreshCw,
  CheckCircle2,
  AlertCircle,
  Sliders,
  FolderSync,
  ShieldCheck,
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
  getSettings,
  type SettingsResponse,
} from '@/lib/api';
import { LLMProviderCard } from './LLMProviderCard';

export function SettingsView() {
  const { error: toastError } = useToast();

  const [loading, setLoading] = useState(false);
  const [settings, setSettings] = useState<SettingsResponse | null>(null);

  const loadSettings = async () => {
    setLoading(true);
    try {
      const data = await getSettings();
      setSettings(data);
    } catch (err: any) {
      toastError(err.message || '获取配置信息失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadSettings();
  }, []);

  if (loading && !settings) {
    return (
      <div className="h-full flex items-center justify-center text-xs text-muted-foreground">
        <RefreshCw className="w-4 h-4 animate-spin mr-2" />
        正在加载系统设置...
      </div>
    );
  }

  return (
    <div className="h-full overflow-y-auto p-4 md:p-8 space-y-6 max-w-5xl mx-auto">
      {/* Top Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pb-2 border-b">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-foreground flex items-center gap-2">
            <Sliders className="w-5 h-5 text-primary" />
            系统配置与运行状态
          </h1>
          <p className="text-xs text-muted-foreground mt-1">
            查看工具链、本地数据源与音箱驱动，并配置核心 LLM 推理接口
          </p>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={loadSettings}
          disabled={loading}
          className="gap-2 shrink-0 self-start sm:self-auto"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
          刷新配置
        </Button>
      </div>

      {/* 1. Redesigned LLM Provider Card */}
      {settings?.llm && (
        <LLMProviderCard
          currentSettings={settings.llm}
          onSaved={loadSettings}
        />
      )}

      {/* 2. External Tools Status Table */}
      <Card className="border shadow-xs">
        <CardHeader className="pb-3">
          <div className="flex items-center gap-2">
            <Wrench className="w-4 h-4 text-primary" />
            <CardTitle>工具链执行环境 (External Tools)</CardTitle>
          </div>
          <CardDescription>
            Agent 执行系统级拼音搜索、JSON 与 YAML 管道解析所依赖的宿主二进制程序
          </CardDescription>
        </CardHeader>
        <CardContent>
          <div className="overflow-x-auto border rounded-lg">
            <table className="w-full text-left text-xs">
              <thead className="bg-muted/50 border-b text-muted-foreground">
                <tr>
                  <th className="px-4 py-2.5 font-medium">工具</th>
                  <th className="px-4 py-2.5 font-medium">用途说明</th>
                  <th className="px-4 py-2.5 font-medium">文件路径</th>
                  <th className="px-4 py-2.5 font-medium">版本</th>
                  <th className="px-4 py-2.5 font-medium text-right">状态</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border/60">
                {settings?.tools &&
                  Object.entries(settings.tools).map(([toolName, info]) => {
                    const descriptions: Record<string, string> = {
                      fzq: '本地曲库超高速拼音/模糊匹配引擎',
                      jq: '轻量结构化 JSON 数据过滤与转换',
                      yq: '系统 YAML 配置文件读取与处理',
                    };

                    return (
                      <tr
                        key={toolName}
                        className="hover:bg-muted/30 transition-colors"
                      >
                        <td className="px-4 py-3 font-mono font-semibold text-foreground">
                          {toolName}
                        </td>
                        <td className="px-4 py-3 text-muted-foreground">
                          {descriptions[toolName] || '系统辅助工具'}
                        </td>
                        <td className="px-4 py-3 font-mono text-muted-foreground truncate max-w-xs">
                          {info.path || '-'}
                        </td>
                        <td className="px-4 py-3 font-mono text-muted-foreground">
                          {info.version || '-'}
                        </td>
                        <td className="px-4 py-3 text-right">
                          {info.ok ? (
                            <Badge
                              variant="success"
                              className="gap-1 inline-flex"
                            >
                              <CheckCircle2 className="w-3 h-3" />
                              可用
                            </Badge>
                          ) : (
                            <Badge
                              variant="destructive"
                              className="gap-1 inline-flex"
                            >
                              <AlertCircle className="w-3 h-3" />
                              异常
                            </Badge>
                          )}
                        </td>
                      </tr>
                    );
                  })}
                {(!settings?.tools ||
                  Object.keys(settings.tools).length === 0) && (
                  <tr>
                    <td
                      colSpan={5}
                      className="px-4 py-6 text-center text-muted-foreground"
                    >
                      暂无工具信息
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>

      {/* 3. Data Sources (Read-only) & Speaker Driver (Read-only) */}
      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {/* Sources List */}
        <Card className="border shadow-xs">
          <CardHeader className="pb-3">
            <div className="flex items-center gap-2">
              <Database className="w-4 h-4 text-primary" />
              <CardTitle>已挂载数据源 (Data Sources)</CardTitle>
            </div>
            <CardDescription>
              LLM 仅通过注册名称进行只读检索，无权直接访问宿主底层文件路径
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {settings?.sources && settings.sources.length > 0 ? (
              settings.sources.map((src) => (
                <div
                  key={src.name}
                  className="p-3 rounded-lg border bg-muted/20 space-y-1.5 text-xs"
                >
                  <div className="flex items-center justify-between">
                    <span className="font-semibold font-mono text-foreground">
                      {src.name}
                    </span>
                    <Badge variant="secondary" className="uppercase text-[10px]">
                      {src.format}
                    </Badge>
                  </div>
                  <div className="font-mono text-muted-foreground text-[11px] truncate">
                    文件: {src.file}
                  </div>
                  {src.keys && src.keys.length > 0 && (
                    <div className="flex items-center gap-1 text-[11px] text-muted-foreground">
                      <span>索引键:</span>
                      <span className="font-mono">{src.keys.join(', ')}</span>
                    </div>
                  )}
                </div>
              ))
            ) : (
              <div className="text-xs text-muted-foreground py-4 text-center">
                未注册外部数据源
              </div>
            )}
          </CardContent>
        </Card>

        {/* Speaker Driver & Environment */}
        <Card className="border shadow-xs">
          <CardHeader className="pb-3">
            <div className="flex items-center gap-2">
              <Volume2 className="w-4 h-4 text-primary" />
              <CardTitle>音箱驱动 (Speaker Driver)</CardTitle>
            </div>
            <CardDescription>
              驱动小爱音箱进行播放控制、TTS 语音合成与麦克风唤醒拦截
            </CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="p-3 rounded-lg border bg-muted/20 space-y-2 text-xs">
              <div className="flex items-center justify-between">
                <span className="text-muted-foreground">当前驱动类型</span>
                <Badge variant="outline" className="font-mono font-semibold">
                  {settings?.speaker.driver || '未知'}
                </Badge>
              </div>
              <div className="flex items-center justify-between">
                <span className="text-muted-foreground">驱动状态</span>
                <span className="text-emerald-500 font-medium flex items-center gap-1">
                  <ShieldCheck className="w-3.5 h-3.5" /> 已连接就绪
                </span>
              </div>
              <div className="text-[11px] text-muted-foreground pt-1 border-t">
                {settings?.speaker.driver === 'xiaomi_miot'
                  ? '小米 MIoT 云端协议通道，支持小爱音箱 Pro / Play 等主流设备。'
                  : '模拟驱动环境 (Mock Driver)，用于本地研发及离线测试验证。'}
              </div>
            </div>

            {/* Media folders overview */}
            <div className="p-3 rounded-lg border bg-muted/20 space-y-1.5 text-xs">
              <div className="flex items-center justify-between">
                <span className="text-muted-foreground flex items-center gap-1">
                  <FolderSync className="w-3.5 h-3.5 text-primary" /> 媒体目录白名单
                </span>
                <span className="font-mono text-[11px] text-muted-foreground">
                  轮询间隔: {String(settings?.media.sync_interval || '6h')}
                </span>
              </div>
              <div className="space-y-1 pt-1">
                {settings?.media.folders?.map((folder, i) => (
                  <div
                    key={i}
                    className="font-mono text-[11px] bg-background/80 px-2 py-1 rounded border truncate"
                  >
                    {folder}
                  </div>
                ))}
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
