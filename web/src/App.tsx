import { useState, useEffect } from 'react';
import { accent } from '@/lib/accent';
import { Sidebar, type NavTab } from '@/components/layout/Sidebar';
import { ChatView } from '@/components/chat/ChatView';
import { SettingsView } from '@/components/settings/SettingsView';
import { MediaView } from '@/components/media/MediaView';
import { GuideView } from '@/components/guide/GuideView';
import { LogsView } from '@/components/logs/LogsView';
import { ToastProvider } from '@/components/ui/toast';
import { getHealth } from '@/lib/api';
import { useHashRoute, type Tab } from '@/lib/useHashRoute';
import {
  MessageSquare,
  Settings as SettingsIcon,
  Music,
  ScrollText,
  BookOpen,
} from 'lucide-react';

export function App() {
  const { route, navigate } = useHashRoute();
  const currentTab: NavTab = route.tab;
  const setCurrentTab = (t: Tab) => navigate(t);
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
        setHealthOk(false);
      }
    };

    checkHealth();
    const interval = setInterval(checkHealth, 30000);
    return () => clearInterval(interval);
  }, []);

  const toggleTheme = () => setIsDark((prev) => !prev);

  return (
    <ToastProvider>
      <div className="flex h-screen w-screen overflow-hidden bg-background text-foreground">
        {/* Left Sidebar (Desktop/Tablet) */}
        <div className="hidden sm:flex h-full shrink-0">
          <Sidebar
            currentTab={currentTab}
            onSelectTab={setCurrentTab}
            onOpenAbout={() => navigate('settings', 'about')}
            isDark={isDark}
            onToggleTheme={toggleTheme}
            healthOk={healthOk}
          />
        </div>

        {/* Main Content Area */}
        <div className="flex-1 flex flex-col h-full min-w-0 overflow-hidden relative">
          {/* Active Tab View */}
          <div className="flex-1 h-full min-w-0 overflow-hidden">
            {currentTab === 'chat' && (
              <ChatView routeKey={route.param} onSelectKey={(k) => navigate('chat', k)} onOpenGuide={() => setCurrentTab('guide')} />
            )}
            {currentTab === 'guide' && (
              <GuideView
                onGo={(tab, section) => {
                  navigate(tab, tab === 'settings' ? section : undefined);
                }}
              />
            )}
            {currentTab === 'settings' && <SettingsView section={route.param || 'llm'} onSectionChange={(sec) => navigate('settings', sec)} />}
            {currentTab === 'media' && <MediaView />}
            {currentTab === 'logs' && (
              <LogsView
                onNavigateToConversation={(convId) => {
                  navigate('chat', 'conv:' + convId);
                }}
              />
            )}
          </div>

          {/* Bottom Navigation for Mobile Screens (< 640px) */}
          <div className="sm:hidden border-t bg-card/90 backdrop-blur flex items-center justify-around py-2 px-1 shrink-0 z-20">
            <button
              type="button"
              onClick={() => setCurrentTab('guide')}
              className={`flex flex-col items-center gap-0.5 text-[10px] p-1.5 rounded ${
                currentTab === 'guide' ? 'text-foreground font-semibold' : 'text-muted-foreground'
              }`}
            >
              <BookOpen className={`w-4 h-4 ${accent('guide').text}`} />
              <span>开始</span>
            </button>
            <button
              type="button"
              onClick={() => setCurrentTab('chat')}
              className={`flex flex-col items-center gap-0.5 text-[10px] p-1.5 rounded ${
                currentTab === 'chat' ? 'text-foreground font-semibold' : 'text-muted-foreground'
              }`}
            >
              <MessageSquare className={`w-4 h-4 ${accent('chat').text}`} />
              <span>对话</span>
            </button>
            <button
              type="button"
              onClick={() => setCurrentTab('settings')}
              className={`flex flex-col items-center gap-0.5 text-[10px] p-1.5 rounded ${
                currentTab === 'settings' ? 'text-foreground font-semibold' : 'text-muted-foreground'
              }`}
            >
              <SettingsIcon className={`w-4 h-4 ${accent('settings').text}`} />
              <span>设置</span>
            </button>
            <button
              type="button"
              onClick={() => setCurrentTab('media')}
              className={`flex flex-col items-center gap-0.5 text-[10px] p-1.5 rounded ${
                currentTab === 'media' ? 'text-foreground font-semibold' : 'text-muted-foreground'
              }`}
            >
              <Music className={`w-4 h-4 ${accent('media').text}`} />
              <span>曲库</span>
            </button>
            <button
              type="button"
              onClick={() => setCurrentTab('logs')}
              className={`flex flex-col items-center gap-0.5 text-[10px] p-1.5 rounded ${
                currentTab === 'logs' ? 'text-foreground font-semibold' : 'text-muted-foreground'
              }`}
            >
              <ScrollText className={`w-4 h-4 ${accent('logs').text}`} />
              <span>日志</span>
            </button>
          </div>
        </div>
      </div>
    </ToastProvider>
  );
}

export default App;
