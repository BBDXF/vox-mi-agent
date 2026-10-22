import { useEffect, useState } from 'react';
import { BookOpen, X } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { getMediaStatus } from '@/lib/api';

const KEY = 'vox_guide_dismissed';

export function FirstRunBanner({ onOpenGuide }: { onOpenGuide: () => void }) {
  const [show, setShow] = useState(false);
  useEffect(() => {
    if (localStorage.getItem(KEY)) return;
    Promise.all([
      fetch('api/settings/llm').then((r) => r.json()).catch(() => null),
      getMediaStatus().catch(() => null),
    ]).then(([llm, media]) => {
      const llmOk = !!(llm?.has_api_key && llm?.model);
      const mediaOk = (media?.tracks ?? 0) > 0;
      if (!llmOk || !mediaOk) setShow(true);
    });
  }, []);
  if (!show) return null;
  return (
    <div className="shrink-0 flex items-center gap-3 px-4 py-2 bg-primary/10 border-b border-primary/20 text-xs">
      <BookOpen className="w-4 h-4 text-primary shrink-0" />
      <span className="flex-1 min-w-0">第一次使用？还没配置大模型或音乐目录，按「快速开始」几步就能点歌。</span>
      <Button size="sm" onClick={onOpenGuide}>查看快速开始</Button>
      <button type="button" title="不再提示" className="p-1 text-muted-foreground hover:text-foreground"
        onClick={() => { localStorage.setItem(KEY, '1'); setShow(false); }}>
        <X className="w-4 h-4" />
      </button>
    </div>
  );
}
