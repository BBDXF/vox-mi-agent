import { X } from 'lucide-react';
import { AboutCard } from './AboutCard';

export function AboutDialog({ onClose }: { onClose: () => void }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div className="relative w-full max-w-lg max-h-[90vh] overflow-y-auto" onClick={(e) => e.stopPropagation()}>
        <button type="button" onClick={onClose} className="absolute right-3 top-3 z-10 text-muted-foreground hover:text-foreground" aria-label="关闭">
          <X className="w-4 h-4" />
        </button>
        <AboutCard />
      </div>
    </div>
  );
}
