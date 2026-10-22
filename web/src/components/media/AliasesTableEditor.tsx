import { useState, useEffect, useRef } from 'react';
import { accent } from '@/lib/accent';
import {
  ListMusic,
  Plus,
  Trash2,
  Save,
  Search,
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
  getAliasesSettings,
  updateAliasesSettings,
  type AliasItem,
} from '@/lib/api';

export function AliasesTableEditor() {
  const { success, error: toastError } = useToast();

  const [loading, setLoading] = useState(false);
  const [initialItems, setInitialItems] = useState<AliasItem[]>([]);
  const [items, setItems] = useState<AliasItem[]>([]);
  const [searchQuery, setSearchQuery] = useState('');
  const [isSaving, setIsSaving] = useState(false);

  // Temporary chip inputs for adding an alias chip per row
  const [chipInputs, setChipInputs] = useState<Record<number, string>>({});

  const isMountedRef = useRef(true);

  const fetchAliases = async () => {
    setLoading(true);
    try {
      const res = await getAliasesSettings();
      if (!isMountedRef.current) return;
      const list = res.items || [];
      setInitialItems(JSON.parse(JSON.stringify(list)));
      setItems(JSON.parse(JSON.stringify(list)));
    } catch (err: any) {
      if (!isMountedRef.current) return;
      toastError(err.message || '获取音乐别名配置失败');
    } finally {
      if (isMountedRef.current) setLoading(false);
    }
  };

  useEffect(() => {
    isMountedRef.current = true;
    fetchAliases();
    return () => {
      isMountedRef.current = false;
    };
  }, []);

  // Filtered items by search query
  const filteredIndices = items
    .map((item, idx) => ({ item, idx }))
    .filter(({ item }) => {
      if (!searchQuery.trim()) return true;
      const q = searchQuery.toLowerCase();
      const inTitle = item.title.toLowerCase().includes(q);
      const inArtist = (item.artist || '').toLowerCase().includes(q);
      const inAliases = (item.aliases || []).some((a) =>
        a.toLowerCase().includes(q)
      );
      return inTitle || inArtist || inAliases;
    });

  // Check dirty state
  const isDirty = JSON.stringify(initialItems) !== JSON.stringify(items);

  // Row modifications
  const handleUpdateField = (index: number, field: 'title' | 'artist', val: string) => {
    setItems((prev) => {
      const next = [...prev];
      next[index] = { ...next[index], [field]: val };
      return next;
    });
  };

  const handleAddAliasChip = (index: number) => {
    const raw = (chipInputs[index] || '').trim();
    if (!raw) return;
    setItems((prev) => {
      const next = [...prev];
      const curAliases = next[index].aliases || [];
      if (!curAliases.includes(raw)) {
        next[index] = { ...next[index], aliases: [...curAliases, raw] };
      }
      return next;
    });
    setChipInputs((prev) => ({ ...prev, [index]: '' }));
  };

  const handleRemoveAliasChip = (index: number, chipIndex: number) => {
    setItems((prev) => {
      const next = [...prev];
      const curAliases = next[index].aliases || [];
      next[index] = {
        ...next[index],
        aliases: curAliases.filter((_, i) => i !== chipIndex),
      };
      return next;
    });
  };

  const handleRemoveRow = (index: number) => {
    setItems((prev) => prev.filter((_, i) => i !== index));
  };

  const handleAddRow = () => {
    setItems((prev) => [
      {
        title: '新歌曲',
        artist: '',
        aliases: [],
      },
      ...prev,
    ]);
  };

  // Save changes
  const handleSave = async () => {
    if (isSaving || !isDirty) return;
    setIsSaving(true);
    try {
      await updateAliasesSettings({ items });
      setInitialItems(JSON.parse(JSON.stringify(items)));
      success('音乐别名词典已保存');
    } catch (err: any) {
      toastError(err.message || '保存音乐别名失败');
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
              <ListMusic className={`w-4 h-4 ${accent('media').text}`} />
              <CardTitle>音乐别名词典 (Aliases)</CardTitle>
            </div>
            <CardDescription className="text-xs mt-0.5">
              用于拼音与多语种重定向，例如将「QingTian」或「周董晴天」映射至《晴天》
            </CardDescription>
          </div>

          <div className="flex items-center gap-2 shrink-0">
            <Button
              variant="outline"
              size="sm"
              onClick={fetchAliases}
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
              添加别名映射
            </Button>
          </div>
        </div>
      </CardHeader>

      <CardContent className="p-4 sm:p-6 space-y-4">
        {/* Search filter */}
        <div className="relative max-w-md">
          <Search className="w-4 h-4 absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="搜索歌名、歌手或别名标签..."
            className="pl-9 text-xs h-8.5 bg-muted/20"
          />
        </div>

        {/* Aliases Table */}
        <div className="border rounded-lg overflow-x-auto bg-background/50">
          <table className="w-full text-left text-xs">
            <thead className="bg-muted/50 border-b text-muted-foreground select-none">
              <tr>
                <th className="px-4 py-2.5 font-medium w-48">歌名 (Title)</th>
                <th className="px-4 py-2.5 font-medium w-36">歌手 (Artist)</th>
                <th className="px-4 py-2.5 font-medium">识别别名 (Aliases)</th>
                <th className="px-4 py-2.5 font-medium text-right w-16">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border/60">
              {filteredIndices.length > 0 ? (
                filteredIndices.map(({ item, idx }) => (
                  <tr key={idx} className="hover:bg-muted/30 transition-colors">
                    {/* Title */}
                    <td className="px-4 py-2.5 align-top">
                      <Input
                        value={item.title}
                        onChange={(e) =>
                          handleUpdateField(idx, 'title', e.target.value)
                        }
                        placeholder="歌名"
                        className="h-8 text-xs font-semibold font-mono bg-transparent"
                      />
                    </td>

                    {/* Artist */}
                    <td className="px-4 py-2.5 align-top">
                      <Input
                        value={item.artist || ''}
                        onChange={(e) =>
                          handleUpdateField(idx, 'artist', e.target.value)
                        }
                        placeholder="歌手 (选填)"
                        className="h-8 text-xs font-mono bg-transparent"
                      />
                    </td>

                    {/* Aliases Chips */}
                    <td className="px-4 py-2.5 align-top">
                      <div className="flex flex-wrap items-center gap-1.5 min-h-[32px]">
                        {(item.aliases || []).map((alias, chipIdx) => (
                          <div
                            key={chipIdx}
                            className="inline-flex items-center gap-1 px-2 py-0.5 rounded-md text-[11px] font-mono bg-muted/60 border border-border/80 text-foreground"
                          >
                            <span>{alias}</span>
                            <button
                              type="button"
                              onClick={() => handleRemoveAliasChip(idx, chipIdx)}
                              className="text-muted-foreground hover:text-destructive p-0.5 rounded"
                            >
                              <X className="w-2.5 h-2.5" />
                            </button>
                          </div>
                        ))}

                        {/* Inline chip adder */}
                        <div className="inline-flex items-center gap-1">
                          <Input
                            value={chipInputs[idx] || ''}
                            onChange={(e) =>
                              setChipInputs((prev) => ({
                                ...prev,
                                [idx]: e.target.value,
                              }))
                            }
                            onKeyDown={(e) => {
                              if (e.key === 'Enter') {
                                e.preventDefault();
                                handleAddAliasChip(idx);
                              }
                            }}
                            placeholder="+ 别名"
                            className="h-6.5 w-20 text-[11px] font-mono px-1.5 bg-muted/20"
                          />
                          <Button
                            type="button"
                            variant="ghost"
                            size="sm"
                            onClick={() => handleAddAliasChip(idx)}
                            disabled={!chipInputs[idx]?.trim()}
                            className="h-6.5 px-1 text-[11px]"
                          >
                            <Plus className="w-3 h-3" />
                          </Button>
                        </div>
                      </div>
                    </td>

                    {/* Remove row */}
                    <td className="px-4 py-2.5 text-right align-top">
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon"
                        onClick={() => handleRemoveRow(idx)}
                        className="h-7 w-7 text-muted-foreground hover:text-destructive hover:bg-destructive/10"
                        title="删除该映射"
                      >
                        <Trash2 className="w-3.5 h-3.5" />
                      </Button>
                    </td>
                  </tr>
                ))
              ) : (
                <tr>
                  <td
                    colSpan={4}
                    className="px-4 py-8 text-center text-muted-foreground text-xs"
                  >
                    {searchQuery ? '未找到符合条件的别名项' : '暂无别名配置，点击右上角添加'}
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </CardContent>

      <CardFooter className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pt-4 border-t bg-muted/10">
        <div className="text-xs text-muted-foreground">
          {isDirty ? (
            <span className="text-amber-600 dark:text-amber-400 flex items-center gap-1.5 font-medium">
              <span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse" />
              别名词典有未保存的修改
            </span>
          ) : (
            <span className="flex items-center gap-1.5 text-muted-foreground">
              <Check className="w-3.5 h-3.5 text-emerald-500" />
              别名词典已同步
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
          {isSaving ? '正在保存...' : '保存别名词典'}
        </Button>
      </CardFooter>
    </Card>
  );
}