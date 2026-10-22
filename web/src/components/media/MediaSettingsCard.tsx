import { useState, useEffect, useRef } from 'react';
import { accent } from '@/lib/accent';
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
  ChevronDown,
  ChevronRight,
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
  type MediaTagMode,
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
  const [initialTagMode, setInitialTagMode] = useState<MediaTagMode>('filename_first');
  const [initialFilenamePattern, setInitialFilenamePattern] = useState<string>(
    '(?:(?P<artist>.+?) - )?(?P<title>.+)\\.\\w+$'
  );
  const [initialMaxFiles, setInitialMaxFiles] = useState<number>(100000);
  const [initialSyncOnStart, setInitialSyncOnStart] = useState<boolean>(true);

  // Form editing state
  const [folders, setFolders] = useState<string[]>([]);
  const [extensions, setExtensions] = useState<string[]>([]);
  const [syncInterval, setSyncInterval] = useState<string>('6h');
  const [tagMode, setTagMode] = useState<MediaTagMode>('filename_first');
  const [filenamePattern, setFilenamePattern] = useState<string>(
    '(?:(?P<artist>.+?) - )?(?P<title>.+)\\.\\w+$'
  );
  const [maxFiles, setMaxFiles] = useState<number>(100000);
  const [syncOnStart, setSyncOnStart] = useState<boolean>(true);

  // Collapsible Advanced section (collapsed by default)
  const [isAdvancedOpen, setIsAdvancedOpen] = useState<boolean>(false);

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
      const rawPattern =
        mediaConfig.filename_pattern ?? '(?:(?P<artist>.+?) - )?(?P<title>.+)\\.\\w+$';
      const rawMaxFiles = mediaConfig.max_files ?? 100000;
      const rawSyncOnStart = mediaConfig.sync_on_start ?? true;
      const rawTagMode = mediaConfig.tag_mode || 'filename_first';

      setInitialFolders(rawFolders);
      setInitialExtensions(rawExtensions);
      setInitialSyncInterval(rawInterval);
      setInitialTagMode(rawTagMode);
      setInitialFilenamePattern(rawPattern);
      setInitialMaxFiles(rawMaxFiles);
      setInitialSyncOnStart(rawSyncOnStart);

      setFolders(rawFolders);
      setExtensions(rawExtensions);
      setSyncInterval(rawInterval);
      setTagMode(rawTagMode);
      setFilenamePattern(rawPattern);
      setMaxFiles(rawMaxFiles);
      setSyncOnStart(rawSyncOnStart);
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
  const isTagModeChanged = tagMode !== initialTagMode;
  const isPatternChanged = filenamePattern !== initialFilenamePattern;
  const isMaxFilesChanged = maxFiles !== initialMaxFiles;
  const isSyncOnStartChanged = syncOnStart !== initialSyncOnStart;

  const isDirty =
    isFoldersChanged ||
    isExtensionsChanged ||
    isIntervalChanged ||
    isTagModeChanged ||
    isPatternChanged ||
    isMaxFilesChanged ||
    isSyncOnStartChanged;

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
        tag_mode: tagMode,
        filename_pattern: filenamePattern,
        max_files: maxFiles,
        sync_on_start: syncOnStart,
      });

      // Update baseline to match saved state
      setInitialFolders([...folders]);
      setInitialExtensions([...extensions]);
      setInitialSyncInterval(syncInterval);
      setInitialTagMode(tagMode);
      setInitialFilenamePattern(filenamePattern);
      setInitialMaxFiles(maxFiles);
      setInitialSyncOnStart(syncOnStart);

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
              <Sliders className={`w-4 h-4 ${accent('media').text}`} />
              <CardTitle>曲库设置 (Library Configuration)</CardTitle>
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
                <Folder className={`w-4 h-4 ${accent('media').text}`} />
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
                <FolderPlus className={`w-3.5 h-3.5 ${accent('media').text}`} />
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
                <FileAudio className={`w-4 h-4 ${accent('media').text}`} />
                <span className="text-xs font-semibold text-foreground">
                  音频格式白名单 (Extensions)
                </span>
              </div>
              <p className="text-[11px] text-muted-foreground">
                扫描时仅对符合以下后缀的音频文件读取标签并构建模糊索引
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
                <Clock className={`w-4 h-4 ${accent('media').text}`} />
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

          {/* Section: 歌名/歌手来源 (tag_mode) */}
          <div className="space-y-3 pt-2 border-t">
            <div className="space-y-1">
              <div className="flex items-center gap-2">
                <FileAudio className={`w-4 h-4 ${accent('media').text}`} />
                <span className="text-xs font-semibold text-foreground">
                  歌名/歌手来源
                </span>
              </div>
              <p className="text-[11px] text-muted-foreground">
                专辑、年份、曲序、流派始终读取文件标签，仅文件名模式除外
              </p>
              <ul className="text-[11px] text-muted-foreground list-disc pl-4 space-y-0.5">
                <li>支持读取标签：MP3（ID3v1/v2）、FLAC、M4A/AAC（MP4 容器）、OGG</li>
                <li>不读取标签：WAV、APE 及裸 AAC 流，这些文件只用文件名</li>
                <li>只读取文件内已有的标签，不联网补全；文件内没有的字段就留空</li>
                <li>老旧 MP3 的 ID3v1 标签若为 GBK 编码，中文可能乱码，此时建议用「文件名优先」</li>
                <li>「文件名优先」：文件名同时解析出歌手和歌名才采用，否则改用标签</li>
                <li>修改后会重新同步；标签读取结果有缓存，只重读有变化的文件</li>
              </ul>
            </div>

            <div className="max-w-xs">
              <select
                value={tagMode}
                onChange={(e) => setTagMode(e.target.value as any)}
                className="flex h-9 w-full rounded-md border border-input bg-background px-3 py-1.5 text-xs font-medium shadow-xs transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
              >
                <option value="filename_first">文件名优先</option>
                <option value="tags_first">标签优先</option>
                <option value="filename_only">仅文件名</option>
              </select>
            </div>
          </div>

          {/* Section 4: 高级 (collapsed by default) */}
          <div className="pt-2 border-t space-y-4">
            <button
              type="button"
              onClick={() => setIsAdvancedOpen((prev) => !prev)}
              className="flex items-center justify-between w-full py-1 text-left text-xs font-semibold text-foreground hover:text-primary transition-colors select-none"
            >
              <div className="flex items-center gap-2">
                <Sliders className={`w-4 h-4 ${accent('media').text}`} />
                <span>高级设置 (Advanced)</span>
                <span className="text-[11px] font-normal text-muted-foreground">
                  文件名正则解析、索引规模上限、开机启动同步
                </span>
              </div>
              {isAdvancedOpen ? (
                <ChevronDown className="w-4 h-4 text-muted-foreground" />
              ) : (
                <ChevronRight className="w-4 h-4 text-muted-foreground" />
              )}
            </button>

            {isAdvancedOpen && (
              <div className="p-4 rounded-lg border bg-muted/20 space-y-4 text-xs animate-in fade-in duration-150">
                {/* Field: filename_pattern */}
                <div className="space-y-1.5">
                  <label className="font-semibold text-foreground flex items-center justify-between">
                    <span>文件名提取正则 (Filename Pattern)</span>
                    <span className="text-[10px] text-muted-foreground font-mono">
                      正则分组支持
                    </span>
                  </label>
                  <Input
                    value={filenamePattern}
                    onChange={(e) => setFilenamePattern(e.target.value)}
                    placeholder="(?:(?P<artist>.+?) - )?(?P<title>.+)\.\w+$"
                    className="font-mono text-xs bg-background"
                  />
                  <p className="text-[11px] text-muted-foreground leading-relaxed">
                    提示: 必须包含命名分组 <code>title</code>，可选 <code>artist</code>，例如{' '}
                    <code>(?:(?P&lt;artist&gt;.+?) - )?(?P&lt;title&gt;.+)\.\w+$</code>
                  </p>
                  {fieldErrors['filename_pattern'] && (
                    <div className="text-[11px] text-destructive flex items-center gap-1 font-sans bg-destructive/10 border border-destructive/20 rounded px-2 py-1 mt-1">
                      <AlertCircle className="w-3.5 h-3.5 shrink-0" />
                      <span>{fieldErrors['filename_pattern']}</span>
                    </div>
                  )}
                </div>

                {/* Field: max_files */}
                <div className="space-y-1.5 max-w-sm">
                  <label className="font-semibold text-foreground">
                    最大允许索引文件数 (Max Files)
                  </label>
                  <Input
                    type="number"
                    step="1000"
                    min="1"
                    value={maxFiles}
                    onChange={(e) => setMaxFiles(parseInt(e.target.value, 10) || 100000)}
                    className="font-mono text-xs bg-background"
                  />
                  <p className="text-[11px] text-muted-foreground">
                    控制单实例内存索引快照文件数上限（默认 100,000）
                  </p>
                  {fieldErrors['max_files'] && (
                    <div className="text-[11px] text-destructive flex items-center gap-1 font-sans bg-destructive/10 border border-destructive/20 rounded px-2 py-1 mt-1">
                      <AlertCircle className="w-3.5 h-3.5 shrink-0" />
                      <span>{fieldErrors['max_files']}</span>
                    </div>
                  )}
                </div>

                {/* Field: sync_on_start */}
                <div className="space-y-1.5 pt-2 border-t border-border/60">
                  <div className="flex items-center justify-between">
                    <div className="space-y-0.5">
                      <div className="flex items-center gap-2">
                        <span className="font-semibold text-foreground">
                          系统启动时自动同步 (Sync On Start)
                        </span>
                        <Badge
                          variant="warning"
                          className="text-[10px] py-0 px-1.5 font-sans"
                        >
                          下次启动生效
                        </Badge>
                      </div>
                      <p className="text-[11px] text-muted-foreground">
                        在服务或守护进程重启时，自动全量比对媒体目录更新 songs.jsonl
                      </p>
                    </div>

                    <label className="relative inline-flex items-center cursor-pointer select-none shrink-0">
                      <input
                        type="checkbox"
                        checked={syncOnStart}
                        onChange={(e) => setSyncOnStart(e.target.checked)}
                        className="sr-only peer"
                      />
                      <div className="w-11 h-6 bg-muted peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-primary border border-border" />
                    </label>
                  </div>
                  {fieldErrors['sync_on_start'] && (
                    <div className="text-[11px] text-destructive flex items-center gap-1 font-sans bg-destructive/10 border border-destructive/20 rounded px-2 py-1 mt-1">
                      <AlertCircle className="w-3.5 h-3.5 shrink-0" />
                      <span>{fieldErrors['sync_on_start']}</span>
                    </div>
                  )}
                </div>
              </div>
            )}
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