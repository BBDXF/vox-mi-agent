import { useState, useEffect, useRef } from 'react';
import {
  Folder,
  FolderPlus,
  Trash2,
  Save,
  RefreshCw,
  AlertCircle,
  CheckCircle2,
  Clock,
  FileAudio,
  Plus,
  X,
  Sliders,
  Check,
} from 'lucide-react';
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { useToast } from '@/components/ui/toast';
import { DirectoryBrowserDialog } from './DirectoryBrowserDialog';
import {
  updateMediaSettings,
  validateFsPath,
  type MediaConfig,
  type ValidateFsPathResponse,
} from '@/lib/api';

interface MediaSettingsCardProps {
  mediaConfig?: MediaConfig;
  onSaved: (syncStarted: boolean) => void;
  isSyncing?: boolean;
}

interface ValidationState {
  loading: boolean;
  ok?: boolean;
  estimate?: number | string;
  reason?: string;
}

const STANDARD_INTERVALS: Array<{ value: string; label: string }> = [
  { value: '0', label: '手动同步 (不自动)' },
  { value: '1h', label: '每 1 小时 (1h)' },
  { value: '6h', label: '每 6 小时 (6h - 推荐)' },
  { value: '12h', label: '每 12 小时 (12h)' },
  { value: '24h', label: '每 24 小时 (24h)' },
];

export function MediaSettingsCard({
  mediaConfig,
  onSaved,
  isSyncing = false,
}: MediaSettingsCardProps) {
  const { success, error: toastError } = useToast();

  // Baseline initial state (for dirty tracking)
  const [initialFolders, setInitialFolders] = useState<string[]>([]);
  const [initialExtensions, setInitialExtensions] = useState<string[]>([]);
  const [initialSyncInterval, setInitialSyncInterval] = useState<string>('6h');

  // Form editing state
  const [folders, setFolders] = useState<string[]>([]);
  const [extensions, setExtensions] = useState<string[]>([]);
  const [syncInterval, setSyncInterval] = useState<string>('6h');

  // Per-folder validation cache
  const [validations, setValidations] = useState<Record<string, ValidationState>>({});

  // Inline field errors from PUT response
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});

  // New extension tag input
  const [newExtInput, setNewExtInput] = useState<string>('');

  // Saving state
  const [isSaving, setIsSaving] = useState<boolean>(false);

  // Directory browser modal open state
  const [isBrowserOpen, setIsBrowserOpen] = useState<boolean>(false);

  const isMountedRef = useRef(true);

  useEffect(() => {
    isMountedRef.current = true;
    return () => {
      isMountedRef.current = false;
    };
  }, []);

  // Run validation for a folder path
  const validateFolder = async (path: string) => {
    setValidations((prev) => ({
      ...prev,
      [path]: { loading: true },
    }));

    try {
      const res: ValidateFsPathResponse = await validateFsPath(path);
      if (!isMountedRef.current) return;
      setValidations((prev) => ({
        ...prev,
        [path]: {
          loading: false,
          ok: res.ok,
          estimate: res.audio_files_estimate,
          reason: res.reason,
        },
      }));
    } catch (err: any) {
      if (!isMountedRef.current) return;
      setValidations((prev) => ({
        ...prev,
        [path]: {
          loading: false,
          ok: false,
          reason: err.message || '校验路径出错',
        },
      }));
    }
  };

  // Sync initial values when mediaConfig prop is loaded or updated
  useEffect(() => {
    if (mediaConfig) {
      const rawFolders = mediaConfig.folders || [];
      const rawExtensions = mediaConfig.extensions || ['mp3', 'flac', 'm4a', 'wav', 'ogg', 'ape'];
      const rawInterval = String(mediaConfig.sync_interval || '6h');

      setInitialFolders(rawFolders);
      setInitialExtensions(rawExtensions);
      setInitialSyncInterval(rawInterval);

      setFolders(rawFolders);
      setExtensions(rawExtensions);
      setSyncInterval(rawInterval);
      setFieldErrors({});

      // Trigger validation for each existing folder
      for (const folder of rawFolders) {
        validateFolder(folder);
      }
    }
  }, [mediaConfig]);

  // Dirty check: has anything changed?
  const isFoldersChanged =
    folders.length !== initialFolders.length ||
    folders.some((f, idx) => f !== initialFolders[idx]);

  const isExtensionsChanged =
    extensions.length !== initialExtensions.length ||
    extensions.some((e, idx) => e !== initialExtensions[idx]);

  const isIntervalChanged = syncInterval !== initialSyncInterval;

  const isDirty = isFoldersChanged || isExtensionsChanged || isIntervalChanged;

  // Compute interval dropdown options (preserve unknown current value)
  const isKnownInterval = STANDARD_INTERVALS.some((item) => item.value === syncInterval);
  const intervalOptions = isKnownInterval
    ? STANDARD_INTERVALS
    : [{ value: syncInterval, label: `当前设置 (${syncInterval})` }, ...STANDARD_INTERVALS];

  // Folder actions
  const handleAddFolder = (selectedPath: string) => {
    if (!folders.includes(selectedPath)) {
      setFolders((prev) => [...prev, selectedPath]);
      validateFolder(selectedPath);
      // Clear field error if any
      setFieldErrors((prev) => {
        const next = { ...prev };
        delete next[selectedPath];
        return next;
      });
    }
  };

  const handleRemoveFolder = (targetPath: string) => {
    setFolders((prev) => prev.filter((p) => p !== targetPath));
    setFieldErrors((prev) => {
      const next = { ...prev };
      delete next[targetPath];
      return next;
    });
  };

  // Extension actions
  const handleAddExtension = () => {
    const clean = newExtInput.trim().toLowerCase().replace(/^\./, '');
    if (!clean) return;
    if (!extensions.includes(clean)) {
      setExtensions((prev) => [...prev, clean]);
    }
    setNewExtInput('');
  };

  const handleRemoveExtension = (extToRemove: string) => {
    setExtensions((prev) => prev.filter((e) => e !== extToRemove));
  };

  // Save handler
  const handleSave = async () => {
    if (isSaving || !isDirty) return;

    setIsSaving(true);
    setFieldErrors({});

    try {
      const res = await updateMediaSettings({
        folders,
        extensions,
        sync_interval: syncInterval,
      });

      // Update baseline to match saved state
      setInitialFolders([...folders]);
      setInitialExtensions([...extensions]);
      setInitialSyncInterval(syncInterval);

      if (res.sync_started) {
        success('已保存，正在同步');
      } else {
        success('曲库设置已保存');
      }

      onSaved(res.sync_started);
    } catch (err: any) {
      if (err.fields && typeof err.fields === 'object') {
        setFieldErrors(err.fields);
      }
      toastError(err.message || '保存曲库设置失败');
    } finally {
      if (isMountedRef.current) {
        setIsSaving(false);
      }
    }
  };

  return (
    <>
      <Card className="border shadow-xs">
        <CardHeader className="pb-4">
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <Sliders className="w-4 h-4 text-primary" />
              <CardTitle>曲库设置</CardTitle>
            </div>
            {isDirty && (
              <Badge variant="warning" className="w-fit text-[11px] gap-1 animate-pulse">
                <span className="w-1.5 h-1.5 rounded-full bg-amber-500" />
                有未保存的更改
              </Badge>
            )}
          </div>
          <CardDescription>
            配置 NAS 音频扫描目录、文件过滤白名单以及自动周期同步策略
          </CardDescription>
        </CardHeader>

        <CardContent className="space-y-6">
          {/* Section 1: Folder list */}
          <div className="space-y-3">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <Folder className="w-4 h-4 text-primary" />
                <span className="text-xs font-semibold text-foreground">
                  音频扫描目录 (Folders)
                </span>
                <span className="text-[11px] text-muted-foreground font-mono">
                  ({folders.length})
                </span>
              </div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setIsBrowserOpen(true)}
                className="gap-1.5 text-xs h-8"
              >
                <FolderPlus className="w-3.5 h-3.5 text-primary" />
                添加文件夹
              </Button>
            </div>

            {folders.length === 0 ? (
              <div className="text-xs text-muted-foreground text-center py-6 border border-dashed rounded-lg bg-muted/10 space-y-1">
                <div className="font-medium text-foreground">暂无配置的扫描目录</div>
                <div className="text-[11px]">
                  请点击右上角「添加文件夹」从 NAS 存储中选择包含音乐的目录
                </div>
              </div>
            ) : (
              <div className="border rounded-lg divide-y divide-border/60 bg-background/50 overflow-hidden">
                {folders.map((folderPath) => {
                  const val = validations[folderPath];
                  const fieldErr = fieldErrors[folderPath];

                  return (
                    <div
                      key={folderPath}
                      className="p-3 sm:px-4 sm:py-3 flex flex-col sm:flex-row sm:items-center justify-between gap-2 hover:bg-muted/30 transition-colors"
                    >
                      {/* Path & Validation info */}
                      <div className="min-w-0 flex-1 space-y-1">
                        <div className="flex items-center gap-2">
                          <Folder className="w-4 h-4 text-amber-500 shrink-0" />
                          <span className="text-xs font-mono font-medium truncate text-foreground">
                            {folderPath}
                          </span>
                        </div>

                        {/* Validation estimate / reason */}
                        <div className="flex items-center gap-2 pl-6">
                          {val?.loading ? (
                            <span className="text-[11px] text-muted-foreground flex items-center gap-1">
                              <RefreshCw className="w-3 h-3 animate-spin text-primary" />
                              校验中...
                            </span>
                          ) : val?.ok ? (
                            <div className="flex items-center gap-2 flex-wrap">
                              <Badge
                                variant="success"
                                className="font-mono text-[11px] py-0 px-1.5 gap-1 inline-flex items-center"
                              >
                                <CheckCircle2 className="w-3 h-3" />
                                {val.estimate !== undefined
                                  ? `约 ${typeof val.estimate === 'number' ? val.estimate.toLocaleString() : val.estimate} 首`
                                  : '校验通过'}
                              </Badge>
                              {val.reason && (
                                <span className="text-[11px] text-amber-600 dark:text-amber-400">
                                  ({val.reason})
                                </span>
                              )}
                            </div>
                          ) : val && !val.ok ? (
                            <span className="text-[11px] text-destructive flex items-center gap-1 font-mono">
                              <AlertCircle className="w-3 h-3 shrink-0" />
                              {val.reason || '无法访问该目录'}
                            </span>
                          ) : null}
                        </div>

                        {/* Per-folder inline field error from PUT */}
                        {fieldErr && (
                          <div className="pl-6 pt-1">
                            <div className="text-[11px] text-destructive flex items-center gap-1 bg-destructive/10 border border-destructive/25 rounded px-2 py-1 font-sans">
                              <AlertCircle className="w-3.5 h-3.5 shrink-0" />
                              <span>{fieldErr}</span>
                            </div>
                          </div>
                        )}
                      </div>

                      {/* Remove Button */}
                      <div className="flex items-center justify-end shrink-0 pl-6 sm:pl-0">
                        <Button
                          type="button"
                          variant="ghost"
                          size="icon"
                          onClick={() => handleRemoveFolder(folderPath)}
                          className="h-7 w-7 text-muted-foreground hover:text-destructive hover:bg-destructive/10 transition-colors"
                          title="移除此目录"
                        >
                          <Trash2 className="w-3.5 h-3.5" />
                        </Button>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>

          {/* Section 2: Extensions */}
          <div className="space-y-3 pt-2 border-t">
            <div className="space-y-1">
              <div className="flex items-center gap-2">
                <FileAudio className="w-4 h-4 text-primary" />
                <span className="text-xs font-semibold text-foreground">
                  音频格式白名单 (Extensions)
                </span>
              </div>
              <p className="text-[11px] text-muted-foreground">
                扫描时仅对符合以下后缀的音频文件提取 ID3 标签并构建模糊索引
              </p>
            </div>

            <div className="flex flex-wrap items-center gap-1.5">
              {extensions.map((ext) => (
                <div
                  key={ext}
                  className="inline-flex items-center gap-1 px-2.5 py-1 rounded-md text-xs font-mono font-medium bg-muted/60 border border-border/80 text-foreground group"
                >
                  <span>.{ext}</span>
                  <button
                    type="button"
                    onClick={() => handleRemoveExtension(ext)}
                    className="text-muted-foreground hover:text-destructive rounded-full hover:bg-muted p-0.5 transition-colors"
                    title={`移除 .${ext}`}
                  >
                    <X className="w-3 h-3" />
                  </button>
                </div>
              ))}

              {/* Add extension input */}
              <div className="inline-flex items-center gap-1">
                <Input
                  value={newExtInput}
                  onChange={(e) => setNewExtInput(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      handleAddExtension();
                    }
                  }}
                  placeholder="如 ape, dsf"
                  className="h-7 w-24 text-xs font-mono px-2 bg-muted/20"
                />
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={handleAddExtension}
                  disabled={!newExtInput.trim()}
                  className="h-7 px-2 text-xs"
                  title="添加格式"
                >
                  <Plus className="w-3 h-3" />
                </Button>
              </div>
            </div>

            {extensions.length === 0 && (
              <div className="text-[11px] text-amber-600 dark:text-amber-400">
                提示：未配置任何扩展名将导致无法识别音频文件。
                <button
                  type="button"
                  onClick={() =>
                    setExtensions(['mp3', 'flac', 'm4a', 'wav', 'ogg', 'ape'])
                  }
                  className="ml-1 underline hover:text-amber-500"
                >
                  恢复推荐配置
                </button>
              </div>
            )}
          </div>

          {/* Section 3: Sync Interval */}
          <div className="space-y-3 pt-2 border-t">
            <div className="space-y-1">
              <div className="flex items-center gap-2">
                <Clock className="w-4 h-4 text-primary" />
                <span className="text-xs font-semibold text-foreground">
                  后台同步间隔 (Sync Interval)
                </span>
              </div>
              <p className="text-[11px] text-muted-foreground">
                定时比对 NAS 媒体目录 mtime 变动并更新 fzq 搜索快照，设置为「手动」则仅由用户触发
              </p>
            </div>

            <div className="max-w-xs">
              <select
                value={syncInterval}
                onChange={(e) => setSyncInterval(e.target.value)}
                className="flex h-9 w-full rounded-md border border-input bg-background px-3 py-1.5 text-xs font-medium shadow-xs transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              >
                {intervalOptions.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label}
                  </option>
                ))}
              </select>
            </div>
          </div>
        </CardContent>

        {/* Card Footer: Save Button & Dirty state */}
        <CardFooter className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pt-4 border-t bg-muted/10">
          <div className="text-xs text-muted-foreground">
            {isSyncing ? (
              <span className="text-amber-600 dark:text-amber-400 flex items-center gap-1.5 font-medium">
                <RefreshCw className="w-3.5 h-3.5 animate-spin text-amber-500" />
                后台正在执行同步中...
              </span>
            ) : isDirty ? (
              <span className="text-amber-600 dark:text-amber-400 flex items-center gap-1.5 font-medium">
                <span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse" />
                检测到配置变动，点击右侧保存以生效
              </span>
            ) : (
              <span className="flex items-center gap-1.5 text-muted-foreground">
                <Check className="w-3.5 h-3.5 text-emerald-500" />
                当前配置与服务端保持一致
              </span>
            )}
          </div>

          <div className="flex items-center gap-2 self-end sm:self-auto">
            <Button
              type="button"
              onClick={handleSave}
              disabled={!isDirty || isSaving || isSyncing}
              className="gap-2 text-xs shadow-xs"
            >
              {isSaving ? (
                <RefreshCw className="w-3.5 h-3.5 animate-spin" />
              ) : (
                <Save className="w-3.5 h-3.5" />
              )}
              {isSaving ? '正在保存...' : '保存曲库设置'}
            </Button>
          </div>
        </CardFooter>
      </Card>

      {/* Directory Browser Modal Dialog */}
      <DirectoryBrowserDialog
        isOpen={isBrowserOpen}
        onClose={() => setIsBrowserOpen(false)}
        onSelect={handleAddFolder}
        accessibleRoots={mediaConfig?.accessible_roots || []}
        alreadySelectedFolders={folders}
        initialPath={
          mediaConfig?.accessible_roots && mediaConfig.accessible_roots.length > 0
            ? mediaConfig.accessible_roots[0]
            : undefined
        }
      />
    </>
  );
}