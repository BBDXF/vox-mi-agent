import { useState, useEffect } from 'react';
import { Sidebar, type NavTab } from '@/components/layout/Sidebar';
import { ChatView } from '@/components/chat/ChatView';
import { SettingsView } from '@/components/settings/SettingsView';
import { MediaView } from '@/components/media/MediaView';
import { ToastProvider } from '@/components/ui/toast';
import { getHealth, isMockMode } from '@/lib/api';
import {
  MessageSquare,
  Settings as SettingsIcon,
  Music,
  Radio,
  FlaskConical,
} from 'lucide-react';

export function App() {
  const [currentTab, setCurrentTab] = useState<NavTab>('chat');
  const [isDark, setIsDark] = useState<boolean>(() => {
    if (typeof window === 'undefined') return true;
    const saved = localStorage.getItem('vox_theme');
    if (saved) return saved === 'dark';
    return (
      window.matchMedia &&
      window.matchMedia('(prefers-color-scheme: dark)').matches
    );
  });
  const [healthOk, setHealthOk] = useState<boolean>(true);

  // Apply dark mode class to html element
  useEffect(() => {
    const root = document.documentElement;
    if (isDark) {
      root.classList.add('dark');
      localStorage.setItem('vox_theme', 'dark');
    } else {
      root.classList.remove('dark');
      localStorage.setItem('vox_theme', 'light');
    }
  }, [isDark]);

  // Periodic health check
  useEffect(() => {
    const checkHealth = async () => {
      try {
        const res = await getHealth();
        setHealthOk(res.status === 'ok');
      } catch {
        // If not mock mode and backend is down, healthOk = false
        if (!isMockMode()) {
          setHealthOk(false);
        }
      }
    };

    checkHealth();
    const interval = setInterval(checkHealth, 30000);
    return () => clearInterval(interval);
  }, []);

  const toggleTheme = () => setIsDark((prev) => !prev);

  const mockActive = isMockMode();

  return (
    <ToastProvider>
      <div className="flex h-screen w-screen overflow-hidden bg-background text-foreground">
        {/* Left Sidebar (Desktop/Tablet) */}
        <div className="hidden sm:flex h-full shrink-0">
          <Sidebar
            currentTab={currentTab}
            onSelectTab={setCurrentTab}
            isDark={isDark}
            onToggleTheme={toggleTheme}
            healthOk={healthOk}
          />
        </div>

        {/* Main Content Area */}
        <div className="flex-1 flex flex-col h-full min-w-0 overflow-hidden relative">
          {/* Mock Mode Alert Banner if active */}
          {mockActive && (
            <div className="bg-amber-500/10 border-b border-amber-500/20 px-3 py-1.5 flex items-center justify-between text-xs text-amber-600 dark:text-amber-400 shrink-0 select-none">
              <div className="flex items-center gap-2">
                <FlaskConical className="w-3.5 h-3.5" />
                <span className="font-medium">当前运行于本地演示模式 (Mock Mode)</span>
                <span className="hidden md:inline text-[11px] opacity-80">
                  - 模拟 SSE 流式响应、本地曲库状态与工具执行，脱离后端亦可完整体验
                </span>
              </div>
              <span className="font-mono text-[10px] bg-amber-500/20 px-1.5 py-0.5 rounded">
                ?mock=1
              </span>
            </div>
          )}

          {/* Active Tab View */}
          <div className="flex-1 h-full min-w-0 overflow-hidden">
            {currentTab === 'chat' && <ChatView />}
            {currentTab === 'settings' && <SettingsView />}
            {currentTab === 'media' && <MediaView />}
          </div>

          {/* Bottom Navigation for Mobile Screens (< 640px) */}
          <div className="sm:hidden border-t bg-card/90 backdrop-blur flex items-center justify-around py-2 px-1 shrink-0 z-20">
            <button
              type="button"
              onClick={() => setCurrentTab('chat')}
              className={`flex flex-col items-center gap-0.5 text-[10px] p-1.5 rounded ${
                currentTab === 'chat' ? 'text-primary font-semibold' : 'text-muted-foreground'
              }`}
            >
              <MessageSquare className="w-4 h-4" />
              <span>对话</span>
            </button>
            <button
              type="button"
              onClick={() => setCurrentTab('settings')}
              className={`flex flex-col items-center gap-0.5 text-[10px] p-1.5 rounded ${
                currentTab === 'settings' ? 'text-primary font-semibold' : 'text-muted-foreground'
              }`}
            >
              <SettingsIcon className="w-4 h-4" />
              <span>设置</span>
            </button>
            <button
              type="button"
              onClick={() => setCurrentTab('media')}
              className={`flex flex-col items-center gap-0.5 text-[10px] p-1.5 rounded ${
                currentTab === 'media' ? 'text-primary font-semibold' : 'text-muted-foreground'
              }`}
            >
              <Music className="w-4 h-4" />
              <span>曲库</span>
            </button>
            <div className="flex flex-col items-center gap-0.5 text-[10px] p-1.5 text-muted-foreground/40 cursor-not-allowed">
              <Radio className="w-4 h-4" />
              <span>音箱(待)</span>
            </div>
          </div>
        </div>
      </div>
    </ToastProvider>
  );
}

export default App;
