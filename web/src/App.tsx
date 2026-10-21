import { useState, useEffect } from 'react';
import { Sidebar, type NavTab } from '@/components/layout/Sidebar';
import { ChatView } from '@/components/chat/ChatView';
import { SettingsView } from '@/components/settings/SettingsView';
import { MediaView } from '@/components/media/MediaView';
import { LogsView } from '@/components/logs/LogsView';
import { SpeakerSimulatorView } from '@/components/speaker/SpeakerSimulatorView';
import { ToastProvider } from '@/components/ui/toast';
import { getHealth } from '@/lib/api';
import {
  MessageSquare,
  Settings as SettingsIcon,
  Music,
  ScrollText,
} from 'lucide-react';

export function App() {
  const [currentTab, setCurrentTab] = useState<NavTab>('chat');
  const [targetConversationId, setTargetConversationId] = useState<string | undefined>();
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
              <ChatView targetConversationId={targetConversationId} />
            )}
            {currentTab === 'settings' && <SettingsView />}
            {currentTab === 'media' && <MediaView />}
            {currentTab === 'speaker_simulator' && <SpeakerSimulatorView onGoSettings={() => setCurrentTab('settings')} />}
            {currentTab === 'logs' && (
              <LogsView
                onNavigateToConversation={(convId) => {
                  setTargetConversationId(convId);
                  setCurrentTab('chat');
                }}
              />
            )}
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
            <button
              type="button"
              onClick={() => setCurrentTab('logs')}
              className={`flex flex-col items-center gap-0.5 text-[10px] p-1.5 rounded ${
                currentTab === 'logs' ? 'text-primary font-semibold' : 'text-muted-foreground'
              }`}
            >
              <ScrollText className="w-4 h-4" />
              <span>日志</span>
            </button>
          </div>
        </div>
      </div>
    </ToastProvider>
  );
}

export default App;
