import {
  MessageSquare,
  Settings,
  Music,
  Sun,
  Moon,
  Activity,
  ScrollText,
  BookOpen,
} from 'lucide-react';
import { useEffect, useState } from 'react';
import { accent, type Area } from '@/lib/accent';
import { getVersion, type VersionInfo } from '@/lib/version';
import { AboutDialog } from '@/components/about/AboutDialog';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';

export type NavTab = 'guide' | 'chat' | 'settings' | 'media' | 'logs';

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
  const [ver, setVer] = useState<VersionInfo | null>(null);
  const [aboutOpen, setAboutOpen] = useState(false);
  useEffect(() => { getVersion().then(setVer); }, []);

  const navItems: Array<{
    id: NavTab;
    label: string;
    icon: typeof MessageSquare;
    badge?: string;
    disabled?: boolean;
  }> = [
    { id: 'guide', label: '快速开始', icon: BookOpen },
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
                  <Icon className={cn('w-4 h-4 shrink-0', !isActive && accent(item.id as Area).text)} />
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

        <button
          type="button"
          onClick={() => setAboutOpen(true)}
          className="w-full text-left px-2 text-[10px] font-mono text-muted-foreground/70 hover:text-foreground truncate"
          title="关于"
        >
          {ver ? `v${ver.version}${ver.commit ? ` · ${ver.commit.slice(0, 7)}` : ''}` : ' '}
        </button>
        {aboutOpen && <AboutDialog onClose={() => setAboutOpen(false)} />}

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
