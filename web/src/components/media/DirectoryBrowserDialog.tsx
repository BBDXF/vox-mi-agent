import { useState, useEffect, useRef } from 'react';
import {
  Folder,
  FolderTree,
  FolderSymlink,
  ArrowUp,
  ChevronRight,
  Check,
  X,
  RefreshCw,
  AlertCircle,
  CheckCircle2,
  HardDrive,
  ShieldCheck,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import {
  getFsDirs,
  validateFsPath,
  type FsDirEntry,
  type ValidateFsPathResponse,
} from '@/lib/api';

interface DirectoryBrowserDialogProps {
  isOpen: boolean;
  onClose: () => void;
  onSelect: (selectedPath: string) => void;
  accessibleRoots?: string[];
  alreadySelectedFolders?: string[];
  initialPath?: string;
}

export function DirectoryBrowserDialog({
  isOpen,
  onClose,
  onSelect,
  accessibleRoots = [],
  alreadySelectedFolders = [],
  initialPath,
}: DirectoryBrowserDialogProps) {
  const [currentPath, setCurrentPath] = useState<string>('');
  const [parentPath, setParentPath] = useState<string | null>(null);
  const [entries, setEntries] = useState<FsDirEntry[]>([]);
  const [truncated, setTruncated] = useState<boolean>(false);
  const [loading, setLoading] = useState<boolean>(false);
  const [error, setError] = useState<string | null>(null);

  // Manual input state
  const [manualInput, setManualInput] = useState<string>('');

  // Live validation for currently viewed directory
  const [validating, setValidating] = useState<boolean>(false);
  const [validateResult, setValidateResult] = useState<ValidateFsPathResponse | null>(null);

  const isMountedRef = useRef(true);

  // Load directory entries and validate current directory
  const loadDirectory = async (targetPath?: string) => {
    setLoading(true);
    setError(null);
    try {
      const res = await getFsDirs(targetPath);
      if (!isMountedRef.current) return;
      setCurrentPath(res.path);
      setParentPath(res.parent);
      setEntries(res.entries || []);
      setTruncated(Boolean(res.truncated));
      setManualInput(res.path);

      // Trigger background validation for current viewed folder
      triggerValidation(res.path);
    } catch (err: any) {
      if (!isMountedRef.current) return;
      setError(err.message || '读取目录失败');
      setEntries([]);
      setValidateResult(null);
    } finally {
      if (isMountedRef.current) setLoading(false);
    }
  };

  const triggerValidation = async (path: string) => {
    setValidating(true);
    try {
      const res = await validateFsPath(path);
      if (isMountedRef.current) {
        setValidateResult(res);
      }
    } catch {
      if (isMountedRef.current) {
        setValidateResult({ ok: false, reason: '目录校验失败' });
      }
    } finally {
      if (isMountedRef.current) {
        setValidating(false);
      }
    }
  };

  // On open or target change, initialize directory
  useEffect(() => {
    isMountedRef.current = true;
    if (isOpen) {
      const startPath =
        initialPath ||
        (accessibleRoots.length > 0 ? accessibleRoots[0] : undefined);
      loadDirectory(startPath);
    }
    return () => {
      isMountedRef.current = false;
    };
  }, [isOpen, initialPath]);

  // Handle ESC key to close
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && isOpen) {
        onClose();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [isOpen, onClose]);

  if (!isOpen) return null;

  const handleManualNavigate = () => {
    const trimmed = manualInput.trim();
    if (!trimmed) return;
    loadDirectory(trimmed);
  };

  const handleSelectCurrent = () => {
    if (!currentPath) return;
    onSelect(currentPath);
    onClose();
  };

  const isAlreadyAdded = alreadySelectedFolders.includes(currentPath);

  // Compute breadcrumb segments from currentPath
  // e.g. "/vol1/1000/Music" -> [{ label: "/", path: "/" }, { label: "vol1", path: "/vol1" }, ...]
  const breadcrumbs: Array<{ label: string; path: string }> = [];
  if (currentPath === '/' || !currentPath) {
    breadcrumbs.push({ label: '/', path: '/' });
  } else {
    breadcrumbs.push({ label: '/', path: '/' });
    const segments = currentPath.split('/').filter(Boolean);
    let accum = '';
    for (const seg of segments) {
      accum += `/${seg}`;
      breadcrumbs.push({ label: seg, path: accum });
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-3 sm:p-4 bg-background/80 backdrop-blur-xs animate-in fade-in duration-150">
      <div
        className="relative flex flex-col w-full max-w-2xl max-h-[90vh] bg-card border border-border/80 rounded-xl shadow-2xl overflow-hidden animate-in zoom-in-95 duration-200"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-3.5 border-b bg-muted/20 shrink-0">
          <div className="flex items-center gap-2">
            <div className="w-8 h-8 rounded-lg bg-primary/10 text-primary flex items-center justify-center">
              <FolderTree className="w-4 h-4" />
            </div>
            <div>
              <h2 className="text-sm font-semibold text-foreground">
                浏览 NAS 存储目录
              </h2>
              <p className="text-[11px] text-muted-foreground">
                选择包含音频文件的本地文件夹作为曲库扫描源
              </p>
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="text-muted-foreground hover:text-foreground rounded-md p-1.5 transition-colors"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Accessible Roots Quick-Jump (fnOS 授权目录) */}
        {accessibleRoots && accessibleRoots.length > 0 && (
          <div className="px-5 py-2.5 bg-muted/40 border-b flex flex-wrap items-center gap-2 text-xs shrink-0">
            <div className="flex items-center gap-1.5 text-muted-foreground font-medium text-[11px]">
              <ShieldCheck className="w-3.5 h-3.5 text-primary" />
              <span>fnOS 授权目录:</span>
            </div>
            <div className="flex flex-wrap items-center gap-1.5">
              {accessibleRoots.map((root) => {
                const isActive =
                  currentPath === root || currentPath.startsWith(root + '/');
                return (
                  <button
                    key={root}
                    type="button"
                    onClick={() => loadDirectory(root)}
                    className={`inline-flex items-center gap-1 px-2.5 py-1 rounded-md font-mono text-[11px] border transition-colors ${
                      isActive
                        ? 'bg-primary/15 border-primary/40 text-primary font-medium'
                        : 'bg-background hover:bg-muted border-border/70 text-foreground'
                    }`}
                  >
                    <HardDrive className="w-3 h-3 text-muted-foreground" />
                    <span>{root}</span>
                  </button>
                );
              })}
            </div>
          </div>
        )}

        {/* Manual path bar + Navigation */}
        <div className="px-5 py-3 space-y-2.5 border-b shrink-0 bg-card">
          {/* Manual input */}
          <div className="flex items-center gap-2">
            <div className="relative flex-1">
              <Input
                value={manualInput}
                onChange={(e) => setManualInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    e.preventDefault();
                    handleManualNavigate();
                  }
                }}
                placeholder="/vol1/1000/Music"
                className="font-mono text-xs h-8.5 pr-8 bg-muted/20"
              />
            </div>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={handleManualNavigate}
              disabled={loading || !manualInput.trim()}
              className="h-8.5 text-xs px-3.5 shrink-0"
            >
              前往
            </Button>
          </div>

          {/* Breadcrumb + Up button */}
          <div className="flex items-center gap-2 pt-0.5">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => parentPath && loadDirectory(parentPath)}
              disabled={loading || !parentPath}
              className="h-7 px-2 text-xs gap-1 shrink-0"
              title="返回上一级目录"
            >
              <ArrowUp className="w-3.5 h-3.5" />
              <span className="hidden sm:inline">上一级</span>
            </Button>

            {/* Breadcrumb path */}
            <div className="flex items-center gap-1 overflow-x-auto text-xs font-mono py-1 px-2 rounded-md bg-muted/30 border border-border/50 flex-1 min-w-0 scrollbar-none">
              {breadcrumbs.map((bc, idx) => {
                const isLast = idx === breadcrumbs.length - 1;
                return (
                  <div key={bc.path} className="flex items-center shrink-0">
                    {idx > 0 && (
                      <ChevronRight className="w-3 h-3 text-muted-foreground/60 mx-0.5" />
                    )}
                    <button
                      type="button"
                      disabled={loading || isLast}
                      onClick={() => loadDirectory(bc.path)}
                      className={`px-1.5 py-0.5 rounded transition-colors ${
                        isLast
                          ? 'font-bold text-foreground bg-primary/10'
                          : 'text-muted-foreground hover:text-foreground hover:bg-muted'
                      }`}
                    >
                      {bc.label}
                    </button>
                  </div>
                );
              })}
            </div>
          </div>
        </div>

        {/* Subdirectories List Body */}
        <div className="flex-1 overflow-y-auto p-4 min-h-[220px] max-h-[340px]">
          {loading ? (
            <div className="h-44 flex flex-col items-center justify-center gap-2 text-muted-foreground text-xs">
              <RefreshCw className="w-5 h-5 animate-spin text-primary" />
              <span>正在读取目录内容...</span>
            </div>
          ) : error ? (
            <div className="h-44 flex flex-col items-center justify-center p-4 text-center text-xs text-destructive gap-2 border border-destructive/20 rounded-lg bg-destructive/5">
              <AlertCircle className="w-6 h-6 shrink-0" />
              <div className="font-semibold">无法访问目标路径</div>
              <div className="font-mono text-[11px] text-muted-foreground">{error}</div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => loadDirectory(accessibleRoots[0] || '/')}
                className="mt-2 text-xs h-7 text-foreground"
              >
                返回授权目录
              </Button>
            </div>
          ) : entries.length === 0 ? (
            <div className="h-44 flex flex-col items-center justify-center text-center p-4 text-muted-foreground text-xs border border-dashed rounded-lg bg-muted/10 gap-1.5">
              <Folder className="w-7 h-7 text-muted-foreground/50 mb-1" />
              <span className="font-medium text-foreground">此目录下无子目录</span>
              <span className="text-[11px]">
                若此目录包含音频文件，可直接点击下方「选择此文件夹」添加
              </span>
            </div>
          ) : (
            <div className="space-y-1">
              <div className="text-[11px] text-muted-foreground px-1 pb-1 flex items-center justify-between">
                <span>子目录列表 ({entries.length})</span>
                {truncated && (
                  <span className="text-amber-600 dark:text-amber-400">
                    条目过多，仅展示部分
                  </span>
                )}
              </div>
              <div className="border rounded-lg divide-y divide-border/50 bg-background/50 overflow-hidden">
                {entries.map((entry) => (
                  <div
                    key={entry.path}
                    onClick={() => loadDirectory(entry.path)}
                    className="flex items-center justify-between px-3.5 py-2.5 hover:bg-muted/50 cursor-pointer transition-colors text-xs group"
                  >
                    <div className="flex items-center gap-2.5 min-w-0">
                      {entry.symlink ? (
                        <FolderSymlink className="w-4 h-4 text-cyan-500 shrink-0" />
                      ) : (
                        <Folder className="w-4 h-4 text-amber-500 shrink-0 group-hover:scale-110 transition-transform" />
                      )}
                      <span className="font-mono truncate text-foreground font-medium group-hover:text-primary transition-colors">
                        {entry.name}
                      </span>
                      {entry.symlink && (
                        <Badge variant="outline" className="text-[10px] py-0 px-1.5">
                          软链接
                        </Badge>
                      )}
                    </div>
                    <div className="flex items-center text-muted-foreground group-hover:text-primary transition-colors">
                      <ChevronRight className="w-3.5 h-3.5" />
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>

        {/* Directory Live Validation Preview */}
        <div className="px-5 py-2.5 bg-muted/30 border-t flex items-center justify-between text-xs shrink-0">
          <div className="flex items-center gap-2 min-w-0">
            <span className="text-muted-foreground text-[11px] shrink-0">
              当前目录探测:
            </span>
            {validating ? (
              <span className="text-[11px] text-muted-foreground flex items-center gap-1">
                <RefreshCw className="w-3 h-3 animate-spin" />
                正在估算音频文件...
              </span>
            ) : validateResult ? (
              validateResult.ok ? (
                <div className="flex items-center gap-1.5">
                  <Badge variant="success" className="gap-1 font-mono text-[11px] py-0">
                    <CheckCircle2 className="w-3 h-3" />
                    {validateResult.audio_files_estimate !== undefined
                      ? `约 ${typeof validateResult.audio_files_estimate === 'number' ? validateResult.audio_files_estimate.toLocaleString() : validateResult.audio_files_estimate} 首音频`
                      : '有效目录'}
                  </Badge>
                  {validateResult.reason && (
                    <span className="text-[11px] text-amber-600 dark:text-amber-400">
                      ({validateResult.reason})
                    </span>
                  )}
                </div>
              ) : (
                <Badge variant="destructive" className="gap-1 font-mono text-[11px] py-0">
                  <AlertCircle className="w-3 h-3" />
                  {validateResult.reason || '非有效音频目录'}
                </Badge>
              )
            ) : (
              <span className="text-[11px] text-muted-foreground">-</span>
            )}
          </div>
        </div>

        {/* Footer */}
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 px-5 py-3 border-t bg-card shrink-0">
          <div className="text-xs font-mono truncate text-muted-foreground min-w-0">
            选中: <span className="text-foreground font-semibold">{currentPath || '/'}</span>
          </div>
          <div className="flex items-center gap-2 justify-end shrink-0">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={onClose}
              className="text-xs"
            >
              取消
            </Button>
            <Button
              type="button"
              size="sm"
              onClick={handleSelectCurrent}
              disabled={!currentPath || isAlreadyAdded || loading}
              className="gap-1.5 text-xs shadow-xs"
            >
              <Check className="w-3.5 h-3.5" />
              {isAlreadyAdded ? '已添加此目录' : '选择此文件夹'}
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
}