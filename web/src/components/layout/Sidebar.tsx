import {
  MessageSquare,
  Settings,
  Music,
  Radio,
  Sun,
  Moon,
  FlaskConical,
  Activity,
  ScrollText,
} from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import { isMockMode, setMockMode } from '@/lib/api';

export type NavTab = 'chat' | 'settings' | 'media' | 'logs';

interface SidebarProps {
  currentTab: NavTab;
  onSelectTab: (tab: NavTab) => void;
  isDark: boolean;
  onToggleTheme: () => void;
  healthOk: boolean;
}

export function Sidebar({
  currentTab,
  onSelectTab,
  isDark,
  onToggleTheme,
  healthOk,
}: SidebarProps) {
  const mockActive = isMockMode();

  const handleToggleMock = () => {
    setMockMode(!mockActive);
    // Reload to apply mock or real mode across application
    window.location.reload();
  };

  const navItems: Array<{
    id: NavTab | 'speaker_simulator';
    label: string;
    icon: typeof MessageSquare;
    badge?: string;
    disabled?: boolean;
  }> = [
    {
      id: 'chat',
      label: '对话调试',
      icon: MessageSquare,
      badge: undefined,
      disabled: false,
    },
    {
      id: 'settings',
      label: '系统设置',
      icon: Settings,
      badge: undefined,
      disabled: false,
    },
    {
      id: 'media',
      label: '本地曲库',
      icon: Music,
      badge: undefined,
      disabled: false,
    },
    {
      id: 'logs',
      label: '日志',
      icon: ScrollText,
      badge: undefined,
      disabled: false,
    },
    {
      id: 'speaker_simulator',
      label: '模拟音箱',
      icon: Radio,
      badge: '即将推出',
      disabled: true,
    },
  ];

  return (
    <div className="w-56 h-full border-r bg-card/70 backdrop-blur flex flex-col justify-between shrink-0 select-none z-20">
      {/* Brand Header */}
      <div>
        <div className="h-14 border-b px-4 flex items-center justify-between">
          <div className="flex items-center gap-2.5 min-w-0">
            <div className="w-8 h-8 rounded-lg bg-primary/10 border border-primary/20 flex items-center justify-center shrink-0">
              <span className="text-base leading-none">🎙️</span>
            </div>
            <div className="min-w-0">
              <div className="font-bold text-sm tracking-tight text-foreground truncate">
                vox-mi-agent
              </div>
              <div className="text-[10px] text-muted-foreground flex items-center gap-1 font-mono">
                <span>小爱伴侣控制台</span>
              </div>
            </div>
          </div>
        </div>

        {/* Navigation list */}
        <nav className="p-3 space-y-1">
          {navItems.map((item) => {
            const Icon = item.icon;
            const isActive = currentTab === item.id;

            if (item.disabled) {
              return (
                <div
                  key={item.id}
                  className="flex items-center justify-between px-3 py-2 rounded-lg text-xs font-medium text-muted-foreground/50 cursor-not-allowed"
                >
                  <div className="flex items-center gap-2.5">
                    <Icon className="w-4 h-4 shrink-0" />
                    <span>{item.label}</span>
                  </div>
                  {item.badge && (
                    <Badge
                      variant="secondary"
                      className="text-[9px] px-1.5 py-0 h-4 text-muted-foreground/60 border-0 bg-muted/40"
                    >
                      {item.badge}
                    </Badge>
                  )}
                </div>
              );
            }

            return (
              <button
                key={item.id}
                type="button"
                onClick={() => onSelectTab(item.id as NavTab)}
                className={cn(
                  'w-full flex items-center justify-between px-3 py-2 rounded-lg text-xs font-medium transition-colors',
                  isActive
                    ? 'bg-primary text-primary-foreground shadow-xs'
                    : 'text-muted-foreground hover:bg-muted hover:text-foreground'
                )}
              >
                <div className="flex items-center gap-2.5">
                  <Icon className="w-4 h-4 shrink-0" />
                  <span>{item.label}</span>
                </div>
              </button>
            );
          })}
        </nav>
      </div>

      {/* Bottom Footer & System Status */}
      <div className="p-3 border-t space-y-2 bg-muted/20">
        {/* Health status pill */}
        <div className="flex items-center justify-between px-2 py-1.5 rounded-md bg-background/60 border text-[11px]">
          <div className="flex items-center gap-1.5 min-w-0">
            <span
              className={cn(
                'w-2 h-2 rounded-full shrink-0',
                healthOk ? 'bg-emerald-500 animate-pulse' : 'bg-destructive'
              )}
            />
            <span className="text-muted-foreground truncate">
              {healthOk ? '服务正常' : '健康检查告警'}
            </span>
          </div>
          <Activity className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
        </div>

        {/* Mock Mode Toggle */}
        <button
          type="button"
          onClick={handleToggleMock}
          className={cn(
            'w-full flex items-center justify-between px-2 py-1.5 rounded-md text-[11px] border transition-colors',
            mockActive
              ? 'bg-amber-500/10 text-amber-600 dark:text-amber-400 border-amber-500/30'
              : 'bg-background/60 text-muted-foreground border-border hover:text-foreground'
          )}
          title="点击切换真实后台 / 本地 Mock 演示模式"
        >
          <div className="flex items-center gap-1.5">
            <FlaskConical className="w-3.5 h-3.5 shrink-0" />
            <span>演示模式 (Mock)</span>
          </div>
          <span className="font-mono text-[10px] uppercase font-semibold">
            {mockActive ? 'ON' : 'OFF'}
          </span>
        </button>

        {/* Theme Toggle */}
        <div className="flex items-center justify-between pt-1">
          <span className="text-[11px] text-muted-foreground px-2">外观主题</span>
          <Button
            variant="ghost"
            size="icon"
            onClick={onToggleTheme}
            className="h-7 w-7 text-muted-foreground hover:text-foreground"
            title={isDark ? '切换至亮色模式' : '切换至暗色模式'}
          >
            {isDark ? (
              <Sun className="w-3.5 h-3.5 text-amber-400" />
            ) : (
              <Moon className="w-3.5 h-3.5 text-slate-700" />
            )}
          </Button>
        </div>
      </div>
    </div>
  );
}
