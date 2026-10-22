import { useState, useEffect, useRef } from 'react';
import { accent } from '@/lib/accent';
import {
  Database,
  Plus,
  Trash2,
  Save,
  RefreshCw,
  X,
  Check,
  ShieldCheck,
  Eye,
  Play,
  AlertCircle,
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
import {
  getDataSources,
  getSectionSettings,
  updateSectionSettings,
  type DataSourceItem,
  type SourceItem,
} from '@/lib/api';
import { formatTime } from '@/lib/utils';
import { SourcePreviewDialog } from './SourcePreviewDialog';
import { SourceTestDialog } from './SourceTestDialog';

const FORMAT_OPTIONS = ['auto', 'lines', 'json', 'jsonl', 'yaml'] as const;

function formatBytes(bytes?: number): string {
  if (!bytes || bytes <= 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
}

export function SourcesTableEditor() {
  const { success, error: toastError } = useToast();

  const [loading, setLoading] = useState(false);
  const [dataSources, setDataSources] = useState<DataSourceItem[]>([]);

  // User sources editing state
  const [initialUserItems, setInitialUserItems] = useState<SourceItem[]>([]);
  const [userItems, setUserItems] = useState<SourceItem[]>([]);
  const [isSaving, setIsSaving] = useState(false);

  // Temporary inputs for keys/strip per row
  const [keyInputs, setKeyInputs] = useState<Record<number, string>>({});
  const [stripInputs, setStripInputs] = useState<Record<number, string>>({});

  // Preview & Test Dialog States
  const [previewSourceName, setPreviewSourceName] = useState<string | null>(null);
  const [testSourceName, setTestSourceName] = useState<string | null>(null);
  const [testSourceKeys, setTestSourceKeys] = useState<string[]>([]);

  const isMountedRef = useRef(true);

  const fetchAllSources = async () => {
    setLoading(true);
    try {
      const [sourcesRes, settingsRes] = await Promise.all([
        getDataSources().catch(() => ({ items: [] })),
        getSectionSettings('sources').catch(() => ({ items: [] })),
      ]);

      if (!isMountedRef.current) return;
      setDataSources(sourcesRes.items || []);

      // Filter out builtin sources from the editable list
      const rawUserItems: SourceItem[] = (settingsRes.items || []).filter(
        (it: SourceItem) => it.name !== 'media_library'
      );
      setInitialUserItems(JSON.parse(JSON.stringify(rawUserItems)));
      setUserItems(JSON.parse(JSON.stringify(rawUserItems)));
    } catch (err: any) {
      if (!isMountedRef.current) return;
      toastError(err.message || '获取数据源列表失败');
    } finally {
      if (isMountedRef.current) setLoading(false);
    }
  };

  useEffect(() => {
    isMountedRef.current = true;
    fetchAllSources();
    return () => {
      isMountedRef.current = false;
    };
  }, []);

  const isDirty = JSON.stringify(initialUserItems) !== JSON.stringify(userItems);

  // Builtin source
  const builtinSource =
    dataSources.find((s) => s.builtin || s.name === 'media_library') ||
    dataSources[0];

  const handleUpdate = <K extends keyof SourceItem>(
    index: number,
    field: K,
    val: SourceItem[K]
  ) => {
    setUserItems((prev) => {
      const next = [...prev];
      next[index] = { ...next[index], [field]: val };
      return next;
    });
  };

  const handleAddKey = (index: number) => {
    const raw = (keyInputs[index] || '').trim();
    if (!raw) return;
    const cur = userItems[index].keys || [];
    if (!cur.includes(raw)) {
      handleUpdate(index, 'keys', [...cur, raw]);
    }
    setKeyInputs((prev) => ({ ...prev, [index]: '' }));
  };

  const handleRemoveKey = (index: number, keyIdx: number) => {
    const cur = userItems[index].keys || [];
    handleUpdate(
      index,
      'keys',
      cur.filter((_, i) => i !== keyIdx)
    );
  };

  const handleAddStrip = (index: number) => {
    const raw = (stripInputs[index] || '').trim();
    if (!raw) return;
    const cur = userItems[index].strip || [];
    if (!cur.includes(raw)) {
      handleUpdate(index, 'strip', [...cur, raw]);
    }
    setStripInputs((prev) => ({ ...prev, [index]: '' }));
  };

  const handleRemoveStrip = (index: number, stripIdx: number) => {
    const cur = userItems[index].strip || [];
    handleUpdate(
      index,
      'strip',
      cur.filter((_, i) => i !== stripIdx)
    );
  };

  const handleAddRow = () => {
    setUserItems((prev) => [
      {
        name: 'new_source_' + (prev.length + 1),
        file: '/data/sources/data.json',
        format: 'json',
        records: 'items',
        keys: ['name'],
        extract: '',
        strip: [],
      },
      ...prev,
    ]);
  };

  const handleRemoveRow = (index: number) => {
    setUserItems((prev) => prev.filter((_, i) => i !== index));
  };

  const handleSave = async () => {
    if (isSaving || !isDirty) return;
    setIsSaving(true);
    try {
      // Never send media_library back in PUT
      const cleanUserItems = userItems.filter((it) => it.name !== 'media_library');
      await updateSectionSettings('sources', { items: cleanUserItems });
      setInitialUserItems(JSON.parse(JSON.stringify(cleanUserItems)));
      setUserItems(JSON.parse(JSON.stringify(cleanUserItems)));
      success('自定义数据源配置已保存');
    } catch (err: any) {
      toastError(err.message || '保存数据源失败');
    } finally {
      if (isMountedRef.current) setIsSaving(false);
    }
  };

  const openPreview = (name: string) => {
    setPreviewSourceName(name);
  };

  const openTest = (name: string, keys?: string[]) => {
    setTestSourceName(name);
    setTestSourceKeys(keys || ['title', 'artist']);
  };

  return (
    <div className="space-y-6">
      {/* 1. Builtin Media Library Card (Read-only) */}
      {builtinSource && (
        <Card className="border shadow-xs border-primary/20 bg-card">
          <CardHeader className="pb-3 border-b bg-primary/5">
            <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
              <div className="space-y-1">
                <div className="flex items-center gap-2">
                  <Database className={`w-4 h-4 ${accent('sources').text}`} />
                  <CardTitle className="text-sm font-bold font-mono">
                    {builtinSource.name}
                  </CardTitle>
                  <Badge variant="secondary" className="gap-1 font-sans text-[10px] py-0">
                    <ShieldCheck className="w-3 h-3 text-primary" />
                    内置 (Builtin)
                  </Badge>
                  {builtinSource.exists === false ? (
                    <Badge variant="destructive" className="gap-1 text-[10px] py-0">
                      <AlertCircle className="w-3 h-3" />
                      文件不存在
                    </Badge>
                  ) : null}
                  <Badge variant="outline" className="font-mono text-[10px] py-0 uppercase">
                    {builtinSource.format}
                  </Badge>
                </div>
                <CardDescription className="text-xs">
                  {builtinSource.description || 'NAS 本地曲库元数据全量只读索引快照 (songs.jsonl)'}
                </CardDescription>
              </div>

              {/* Action Buttons: 预览 & 试运行 */}
              <div className="flex items-center gap-2 shrink-0 self-start sm:self-auto">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => openPreview(builtinSource.name)}
                  className="gap-1 text-xs h-7.5"
                >
                  <Eye className={`w-3.5 h-3.5 ${accent('sources').text}`} />
                  预览 (20条)
                </Button>
                <Button
                  size="sm"
                  onClick={() => openTest(builtinSource.name, builtinSource.keys)}
                  className="gap-1 text-xs h-7.5 shadow-xs"
                >
                  <Play className="w-3 h-3 fill-current" />
                  试运行测试
                </Button>
              </div>
            </div>
          </CardHeader>

          <CardContent className="p-4 sm:p-5">
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3 text-xs">
              <div className="p-3 rounded-lg border bg-muted/20 space-y-1">
                <div className="text-[11px] text-muted-foreground">记录条目数</div>
                <div className="text-base font-bold font-mono text-foreground">
                  {builtinSource.count != null ? builtinSource.count.toLocaleString() : '未统计/缺失'}
                </div>
              </div>

              <div className="p-3 rounded-lg border bg-muted/20 space-y-1">
                <div className="text-[11px] text-muted-foreground">文件大小</div>
                <div className="text-base font-bold font-mono text-foreground">
                  {formatBytes(builtinSource.size_bytes)}
                </div>
              </div>

              <div className="p-3 rounded-lg border bg-muted/20 space-y-1">
                <div className="text-[11px] text-muted-foreground">存储路径</div>
                <div className="text-xs font-mono text-foreground truncate" title={builtinSource.file}>
                  {builtinSource.file}
                </div>
              </div>

              <div className="p-3 rounded-lg border bg-muted/20 space-y-1">
                <div className="text-[11px] text-muted-foreground">最近同步时间</div>
                <div className="text-xs font-mono text-foreground truncate">
                  {builtinSource.updated_at ? formatTime(builtinSource.updated_at) : '刚刚'}
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
      )}

      {/* 2. User Data Sources Card (Editable Table) */}
      <Card className="border shadow-xs">
        <CardHeader className="pb-3 border-b bg-muted/5">
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
            <div>
              <div className="flex items-center gap-2">
                <Database className={`w-4 h-4 ${accent('sources').text}`} />
                <CardTitle>自定义数据源 (User Sources)</CardTitle>
                <Badge variant="secondary" className="font-mono text-xs">
                  {userItems.length}
                </Badge>
              </div>
              <CardDescription className="text-xs mt-0.5">
                挂载外部只读结构化文件，供 Agent 在离线状态下检索元数据与字段映射
              </CardDescription>
            </div>

            <div className="flex items-center gap-2 shrink-0">
              <Button
                variant="outline"
                size="sm"
                onClick={fetchAllSources}
                disabled={loading}
                className="gap-1.5 text-xs h-8"
              >
                <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
                刷新
              </Button>
              <Button
                size="sm"
                onClick={handleAddRow}
                className="gap-1.5 text-xs h-8 shadow-xs"
              >
                <Plus className="w-3.5 h-3.5" />
                挂载新数据源
              </Button>
            </div>
          </div>
        </CardHeader>

        <CardContent className="p-4 sm:p-6 space-y-4">
          {userItems.length === 0 ? (
            <div className="py-8 text-center text-xs text-muted-foreground border border-dashed rounded-lg bg-muted/10">
              暂无已挂载的自定义数据源，点击右上角「挂载新数据源」添加
            </div>
          ) : (
            <div className="space-y-3">
              {userItems.map((src, idx) => (
                <div
                  key={idx}
                  className="p-3.5 sm:p-4 rounded-lg border bg-background/50 hover:border-primary/40 transition-colors space-y-3"
                >
                  {/* Header row: Name, Format, Preview, Test, Delete */}
                  <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
                    <div className="flex items-center gap-2 flex-1 min-w-0 max-w-sm">
                      <span className="text-[11px] font-medium text-muted-foreground shrink-0">
                        标识:
                      </span>
                      <Input
                        value={src.name}
                        onChange={(e) => handleUpdate(idx, 'name', e.target.value)}
                        placeholder="标识 (如 devices)"
                        className="h-8 font-mono font-bold text-xs bg-muted/20"
                      />
                    </div>

                    <div className="flex items-center gap-2 shrink-0 flex-wrap">
                      <span className="text-[11px] font-medium text-muted-foreground hidden sm:inline">
                        格式:
                      </span>
                      <select
                        value={src.format}
                        onChange={(e) =>
                          handleUpdate(idx, 'format', e.target.value as any)
                        }
                        className="h-8 rounded-md border border-input bg-background px-2 text-xs font-mono font-medium"
                      >
                        {FORMAT_OPTIONS.map((fmt) => (
                          <option key={fmt} value={fmt}>
                            {fmt.toUpperCase()}
                          </option>
                        ))}
                      </select>

                      {/* Preview Button */}
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => openPreview(src.name)}
                        className="h-8 text-xs gap-1"
                      >
                        <Eye className={`w-3.5 h-3.5 ${accent('sources').text}`} />
                        预览
                      </Button>

                      {/* Test Button */}
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => openTest(src.name, src.keys)}
                        className="h-8 text-xs gap-1"
                      >
                        <Play className="w-3 h-3 text-primary fill-current" />
                        试运行
                      </Button>

                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        onClick={() => handleRemoveRow(idx)}
                        className="h-8 w-8 text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                        title="移除此数据源"
                      >
                        <Trash2 className="w-4 h-4" />
                      </Button>
                    </div>
                  </div>

                  {/* Path & Records */}
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 text-xs">
                    <div>
                      <label className="text-[11px] text-muted-foreground block mb-1">
                        宿主文件路径 (File)
                      </label>
                      <Input
                        value={src.file}
                        onChange={(e) => handleUpdate(idx, 'file', e.target.value)}
                        placeholder="/data/sources/devices.json"
                        className="h-8 font-mono text-xs bg-muted/20"
                      />
                    </div>

                    <div>
                      <label className="text-[11px] text-muted-foreground block mb-1">
                        记录提取路径 (Records)
                      </label>
                      <Input
                        value={src.records || ''}
                        onChange={(e) => handleUpdate(idx, 'records', e.target.value)}
                        placeholder="auto 或 items 数组键"
                        className="h-8 font-mono text-xs bg-muted/20"
                      />
                    </div>
                  </div>

                  {/* Keys & Strip */}
                  <div className="pt-2 border-t border-border/50 grid grid-cols-1 sm:grid-cols-2 gap-3 text-xs">
                    {/* Keys chips */}
                    <div className="space-y-1.5">
                      <label className="text-[11px] text-muted-foreground block">
                        索引检索键 (Keys):
                      </label>
                      <div className="flex flex-wrap items-center gap-1.5">
                        {(src.keys || []).map((k, kIdx) => (
                          <div
                            key={kIdx}
                            className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] font-mono bg-muted border"
                          >
                            <span>{k}</span>
                            <button
                              type="button"
                              onClick={() => handleRemoveKey(idx, kIdx)}
                              className="text-muted-foreground hover:text-destructive"
                            >
                              <X className="w-2.5 h-2.5" />
                            </button>
                          </div>
                        ))}

                        <div className="inline-flex items-center gap-1">
                          <Input
                            value={keyInputs[idx] || ''}
                            onChange={(e) =>
                              setKeyInputs((prev) => ({
                                ...prev,
                                [idx]: e.target.value,
                              }))
                            }
                            onKeyDown={(e) => {
                              if (e.key === 'Enter') {
                                e.preventDefault();
                                handleAddKey(idx);
                              }
                            }}
                            placeholder="+ 键名"
                            className="h-6.5 w-18 text-[11px] font-mono px-1.5 bg-muted/20"
                          />
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            onClick={() => handleAddKey(idx)}
                            disabled={!keyInputs[idx]?.trim()}
                            className="h-6.5 px-1 text-[11px]"
                          >
                            <Plus className="w-3 h-3" />
                          </Button>
                        </div>
                      </div>
                    </div>

                    {/* Strip chips */}
                    <div className="space-y-1.5">
                      <label className="text-[11px] text-muted-foreground block">
                        过滤字段 (Strip):
                      </label>
                      <div className="flex flex-wrap items-center gap-1.5">
                        {(src.strip || []).map((s, sIdx) => (
                          <div
                            key={sIdx}
                            className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] font-mono bg-muted border"
                          >
                            <span>{s}</span>
                            <button
                              type="button"
                              onClick={() => handleRemoveStrip(idx, sIdx)}
                              className="text-muted-foreground hover:text-destructive"
                            >
                              <X className="w-2.5 h-2.5" />
                            </button>
                          </div>
                        ))}

                        <div className="inline-flex items-center gap-1">
                          <Input
                            value={stripInputs[idx] || ''}
                            onChange={(e) =>
                              setStripInputs((prev) => ({
                                ...prev,
                                [idx]: e.target.value,
                              }))
                            }
                            onKeyDown={(e) => {
                              if (e.key === 'Enter') {
                                e.preventDefault();
                                handleAddStrip(idx);
                              }
                            }}
                            placeholder="+ 过滤键"
                            className="h-6.5 w-18 text-[11px] font-mono px-1.5 bg-muted/20"
                          />
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            onClick={() => handleAddStrip(idx)}
                            disabled={!stripInputs[idx]?.trim()}
                            className="h-6.5 px-1 text-[11px]"
                          >
                            <Plus className="w-3 h-3" />
                          </Button>
                        </div>
                      </div>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </CardContent>

        <CardFooter className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pt-4 border-t bg-muted/10">
          <div className="text-xs text-muted-foreground">
            {isDirty ? (
              <span className="text-amber-600 dark:text-amber-400 flex items-center gap-1.5 font-medium">
                <span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse" />
                自定义数据源配置有未保存的修改
              </span>
            ) : (
              <span className="flex items-center gap-1.5 text-muted-foreground">
                <Check className="w-3.5 h-3.5 text-emerald-500" />
                数据源配置已同步
              </span>
            )}
          </div>

          <Button
            type="button"
            onClick={handleSave}
            disabled={!isDirty || isSaving}
            className="gap-2 text-xs shadow-xs self-end sm:self-auto"
          >
            {isSaving ? (
              <RefreshCw className="w-3.5 h-3.5 animate-spin" />
            ) : (
              <Save className="w-3.5 h-3.5" />
            )}
            {isSaving ? '正在保存...' : '保存数据源配置'}
          </Button>
        </CardFooter>
      </Card>

      {/* Preview Dialog */}
      <SourcePreviewDialog
        sourceName={previewSourceName}
        onClose={() => setPreviewSourceName(null)}
      />

      {/* Test Run Dialog */}
      <SourceTestDialog
        sourceName={testSourceName}
        defaultKeys={testSourceKeys}
        onClose={() => setTestSourceName(null)}
      />
    </div>
  );
}