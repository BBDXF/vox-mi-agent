import { useState, useEffect, useRef } from 'react';
import {
  Database,
  Plus,
  Trash2,
  Save,
  RefreshCw,
  X,
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
import { useToast } from '@/components/ui/toast';
import {
  getSectionSettings,
  updateSectionSettings,
  type SourceItem,
} from '@/lib/api';

const FORMAT_OPTIONS = ['auto', 'lines', 'json', 'jsonl', 'yaml'] as const;

export function SourcesTableEditor() {
  const { success, error: toastError } = useToast();

  const [loading, setLoading] = useState(false);
  const [initialItems, setInitialItems] = useState<SourceItem[]>([]);
  const [items, setItems] = useState<SourceItem[]>([]);
  const [isSaving, setIsSaving] = useState(false);

  // Temporary inputs for keys/strip per row
  const [keyInputs, setKeyInputs] = useState<Record<number, string>>({});
  const [stripInputs, setStripInputs] = useState<Record<number, string>>({});

  const isMountedRef = useRef(true);

  const fetchSources = async () => {
    setLoading(true);
    try {
      const res = await getSectionSettings('sources');
      if (!isMountedRef.current) return;
      const list = res.items || [];
      setInitialItems(JSON.parse(JSON.stringify(list)));
      setItems(JSON.parse(JSON.stringify(list)));
    } catch (err: any) {
      if (!isMountedRef.current) return;
      toastError(err.message || '获取数据源配置失败');
    } finally {
      if (isMountedRef.current) setLoading(false);
    }
  };

  useEffect(() => {
    isMountedRef.current = true;
    fetchSources();
    return () => {
      isMountedRef.current = false;
    };
  }, []);

  const isDirty = JSON.stringify(initialItems) !== JSON.stringify(items);

  const handleUpdate = <K extends keyof SourceItem>(
    index: number,
    field: K,
    val: SourceItem[K]
  ) => {
    setItems((prev) => {
      const next = [...prev];
      next[index] = { ...next[index], [field]: val };
      return next;
    });
  };

  const handleAddKey = (index: number) => {
    const raw = (keyInputs[index] || '').trim();
    if (!raw) return;
    const cur = items[index].keys || [];
    if (!cur.includes(raw)) {
      handleUpdate(index, 'keys', [...cur, raw]);
    }
    setKeyInputs((prev) => ({ ...prev, [index]: '' }));
  };

  const handleRemoveKey = (index: number, keyIdx: number) => {
    const cur = items[index].keys || [];
    handleUpdate(
      index,
      'keys',
      cur.filter((_, i) => i !== keyIdx)
    );
  };

  const handleAddStrip = (index: number) => {
    const raw = (stripInputs[index] || '').trim();
    if (!raw) return;
    const cur = items[index].strip || [];
    if (!cur.includes(raw)) {
      handleUpdate(index, 'strip', [...cur, raw]);
    }
    setStripInputs((prev) => ({ ...prev, [index]: '' }));
  };

  const handleRemoveStrip = (index: number, stripIdx: number) => {
    const cur = items[index].strip || [];
    handleUpdate(
      index,
      'strip',
      cur.filter((_, i) => i !== stripIdx)
    );
  };

  const handleAddRow = () => {
    setItems((prev) => [
      {
        name: 'new_source',
        file: '/data/sources/data.json',
        format: 'json',
        records: 'items',
        keys: ['title'],
        extract: '',
        strip: [],
      },
      ...prev,
    ]);
  };

  const handleRemoveRow = (index: number) => {
    setItems((prev) => prev.filter((_, i) => i !== index));
  };

  const handleSave = async () => {
    if (isSaving || !isDirty) return;
    setIsSaving(true);
    try {
      await updateSectionSettings('sources', { items });
      setInitialItems(JSON.parse(JSON.stringify(items)));
      success('数据源配置已保存');
    } catch (err: any) {
      toastError(err.message || '保存数据源失败');
    } finally {
      if (isMountedRef.current) setIsSaving(false);
    }
  };

  return (
    <Card className="border shadow-xs">
      <CardHeader className="pb-3 border-b bg-muted/5">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
          <div>
            <div className="flex items-center gap-2">
              <Database className="w-4 h-4 text-primary" />
              <CardTitle>已挂载数据源 (Data Sources)</CardTitle>
            </div>
            <CardDescription className="text-xs mt-0.5">
              注册外部只读结构化文件，供 Agent 在离线状态下检索元数据与字段映射
            </CardDescription>
          </div>

          <div className="flex items-center gap-2 shrink-0">
            <Button
              variant="outline"
              size="sm"
              onClick={fetchSources}
              disabled={loading}
              className="gap-1.5 text-xs"
            >
              <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
              刷新
            </Button>
            <Button
              size="sm"
              onClick={handleAddRow}
              className="gap-1.5 text-xs shadow-xs"
            >
              <Plus className="w-3.5 h-3.5" />
              挂载新数据源
            </Button>
          </div>
        </div>
      </CardHeader>

      <CardContent className="p-4 sm:p-6 space-y-4">
        {items.length === 0 ? (
          <div className="py-8 text-center text-xs text-muted-foreground border border-dashed rounded-lg bg-muted/10">
            暂无已挂载数据源，点击右上角「挂载新数据源」添加
          </div>
        ) : (
          <div className="space-y-3">
            {items.map((src, idx) => (
              <div
                key={idx}
                className="p-3.5 sm:p-4 rounded-lg border bg-background/50 hover:border-primary/40 transition-colors space-y-3"
              >
                {/* Header row: Name, Format, Delete */}
                <div className="flex items-center justify-between gap-2">
                  <div className="flex items-center gap-2 flex-1 min-w-0 max-w-sm">
                    <span className="text-[11px] font-medium text-muted-foreground shrink-0">
                      名称:
                    </span>
                    <Input
                      value={src.name}
                      onChange={(e) => handleUpdate(idx, 'name', e.target.value)}
                      placeholder="标识 (如 media_library)"
                      className="h-8 font-mono font-bold text-xs bg-muted/20"
                    />
                  </div>

                  <div className="flex items-center gap-2 shrink-0">
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
                      placeholder="/data/sources/songs.jsonl"
                      className="h-8 font-mono text-xs bg-muted/20"
                    />
                  </div>

                  <div>
                    <label className="text-[11px] text-muted-foreground block mb-1">
                      记录提取路径 (Records / Extract)
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
              数据源配置有未保存的修改
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
  );
}