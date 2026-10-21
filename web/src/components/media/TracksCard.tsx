import { useState, useEffect, useRef, useCallback } from 'react';
import {
  Music2,
  Search,
  RefreshCw,
  Copy,
  Check,
  Edit3,
  X,
  Plus,
  Save,
  Sparkles,
  ChevronLeft,
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
import {
  getMediaTracks,
  getMediaArtists,
  getAliasesSettings,
  updateAliasesSettings,
  type MediaTrackItem,
  type MediaArtistItem,
} from '@/lib/api';

export function TracksCard() {
  const { success, error: toastError } = useToast();

  const [tracks, setTracks] = useState<MediaTrackItem[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);

  // Search, Filter & Sort State
  const [searchQ, setSearchQ] = useState('');
  const [debouncedQ, setDebouncedQ] = useState('');
  const [selectedArtist, setSelectedArtist] = useState('');
  const [sortBy, setSortBy] = useState<'title' | 'artist' | 'path'>('title');

  // Artists Combobox State
  const [artists, setArtists] = useState<MediaArtistItem[]>([]);

  // Pagination
  const [limit] = useState(50);
  const [offset, setOffset] = useState(0);

  // Copy Feedback
  const [copiedId, setCopiedId] = useState<string | null>(null);
  const [copiedPath, setCopiedPath] = useState<string | null>(null);

  // Edit Alias Modal State
  const [editingTrack, setEditingTrack] = useState<MediaTrackItem | null>(null);
  const [aliasChips, setAliasChips] = useState<string[]>([]);
  const [newAliasInput, setNewAliasInput] = useState('');
  const [isSavingAlias, setIsSavingAlias] = useState(false);

  const isMountedRef = useRef(true);

  // Debounce search query
  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedQ(searchQ);
      setOffset(0);
    }, 300);
    return () => clearTimeout(timer);
  }, [searchQ]);

  // Load artists list
  const loadArtists = useCallback(async () => {
    try {
      const res = await getMediaArtists();
      if (isMountedRef.current) {
        setArtists(res.items || []);
      }
    } catch {
      // ignore
    }
  }, []);

  // Fetch tracks
  const fetchTracks = useCallback(async () => {
    setLoading(true);
    try {
      const res = await getMediaTracks({
        q: debouncedQ.trim() || undefined,
        artist: selectedArtist.trim() || undefined,
        sort: sortBy,
        limit,
        offset,
      });
      if (isMountedRef.current) {
        setTracks(res.items || []);
        setTotal(res.total || 0);
      }
    } catch (err: any) {
      if (isMountedRef.current) {
        toastError(err.message || '获取曲目列表失败');
      }
    } finally {
      if (isMountedRef.current) setLoading(false);
    }
  }, [debouncedQ, selectedArtist, sortBy, limit, offset, toastError]);

  useEffect(() => {
    isMountedRef.current = true;
    loadArtists();
    return () => {
      isMountedRef.current = false;
    };
  }, [loadArtists]);

  useEffect(() => {
    fetchTracks();
  }, [fetchTracks]);

  // Copy Helper
  const handleCopy = (text: string, type: 'id' | 'path') => {
    navigator.clipboard.writeText(text);
    if (type === 'id') {
      setCopiedId(text);
      setTimeout(() => setCopiedId(null), 1500);
    } else {
      setCopiedPath(text);
      setTimeout(() => setCopiedPath(null), 1500);
    }
    success('已复制到剪贴板');
  };

  // Open Edit Alias modal
  const handleOpenEditAlias = (track: MediaTrackItem) => {
    setEditingTrack(track);
    setAliasChips([...(track.alias || [])]);
    setNewAliasInput('');
  };

  const handleAddAliasChip = () => {
    const trimmed = newAliasInput.trim();
    if (!trimmed) return;
    if (!aliasChips.includes(trimmed)) {
      setAliasChips([...aliasChips, trimmed]);
    }
    setNewAliasInput('');
  };

  const handleRemoveAliasChip = (chipToRemove: string) => {
    setAliasChips(aliasChips.filter((c) => c !== chipToRemove));
  };

  // Save alias via settings/aliases API
  const handleSaveAlias = async () => {
    if (!editingTrack) return;
    setIsSavingAlias(true);
    try {
      const aliasesRes = await getAliasesSettings();
      const currentItems = aliasesRes.items || [];

      // Merge by title and artist
      const existingIdx = currentItems.findIndex(
        (it) =>
          it.title.trim().toLowerCase() === editingTrack.title.trim().toLowerCase() &&
          (!it.artist || !editingTrack.artist || it.artist.trim().toLowerCase() === editingTrack.artist.trim().toLowerCase())
      );

      let updatedItems = [...currentItems];
      if (existingIdx !== -1) {
        updatedItems[existingIdx] = {
          ...updatedItems[existingIdx],
          aliases: aliasChips,
        };
      } else {
        updatedItems.push({
          title: editingTrack.title,
          artist: editingTrack.artist,
          aliases: aliasChips,
        });
      }

      await updateAliasesSettings({ items: updatedItems });

      // Update local track alias display
      setTracks((prev) =>
        prev.map((t) =>
          t.id === editingTrack.id ? { ...t, alias: aliasChips } : t
        )
      );

      success('已保存，将在同步后生效');
      setEditingTrack(null);
    } catch (err: any) {
      toastError(err.message || '保存别名失败');
    } finally {
      setIsSavingAlias(false);
    }
  };

  // Total pages
  const totalPages = Math.ceil(total / limit) || 1;
  const currentPage = Math.floor(offset / limit) + 1;

  return (
    <Card className="border shadow-xs">
      <CardHeader className="pb-3 border-b bg-muted/5">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
          <div>
            <div className="flex items-center gap-2">
              <Music2 className="w-4 h-4 text-primary" />
              <CardTitle>曲目列表与检索 (Tracks)</CardTitle>
              <Badge variant="secondary" className="font-mono text-xs">
                {total.toLocaleString()} 首
              </Badge>
            </div>
            <CardDescription className="text-xs mt-0.5">
              基于 NAS 本地音频构建的拼音与分词检索快照，支持模糊搜歌与别名即时维护
            </CardDescription>
          </div>

          <Button
            variant="outline"
            size="sm"
            onClick={fetchTracks}
            disabled={loading}
            className="gap-1.5 text-xs shrink-0 self-start sm:self-auto"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
            刷新列表
          </Button>
        </div>
      </CardHeader>

      <CardContent className="p-4 sm:p-6 space-y-4">
        {/* Toolbar: Search + Artist combobox + Sort select */}
        <div className="flex flex-col md:flex-row items-stretch md:items-center justify-between gap-2.5 text-xs">
          {/* Search Input */}
          <div className="relative flex-1 max-w-md">
            <Search className="w-4 h-4 absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={searchQ}
              onChange={(e) => setSearchQ(e.target.value)}
              placeholder="拼音/声母/歌名/别名模糊检索..."
              className="pl-9 pr-8 text-xs h-8.5 bg-muted/20 font-sans"
            />
            {searchQ && (
              <button
                type="button"
                onClick={() => setSearchQ('')}
                className="absolute right-2.5 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
              >
                <X className="w-3.5 h-3.5" />
              </button>
            )}
          </div>

          {/* Artist Filter Combobox & Sort Selector */}
          <div className="flex flex-wrap items-center gap-2">
            {/* Artist Filter */}
            <div className="relative flex items-center">
              <select
                value={selectedArtist}
                onChange={(e) => {
                  setSelectedArtist(e.target.value);
                  setOffset(0);
                }}
                className="h-8.5 rounded-md border border-input bg-background px-3 text-xs font-medium shadow-xs max-w-[180px] sm:max-w-[220px]"
              >
                <option value="">全部歌手 (全部)</option>
                {artists.map((art) => (
                  <option key={art.name} value={art.name}>
                    {art.name} ({art.count})
                  </option>
                ))}
              </select>
            </div>

            {/* Sort Selector */}
            <div className="flex items-center gap-1.5">
              <select
                value={sortBy}
                onChange={(e) => {
                  setSortBy(e.target.value as any);
                  setOffset(0);
                }}
                disabled={Boolean(debouncedQ.trim())}
                className="h-8.5 rounded-md border border-input bg-background px-2.5 text-xs font-medium shadow-xs disabled:opacity-50"
              >
                <option value="title">按歌名排序 (title)</option>
                <option value="artist">按歌手排序 (artist)</option>
                <option value="path">按存储路径 (path)</option>
              </select>
            </div>
          </div>
        </div>

        {/* Tracks Table */}
        <div className="border rounded-lg overflow-x-auto bg-background/50">
          <table className="w-full text-left text-xs">
            <thead className="bg-muted/50 border-b text-muted-foreground select-none">
              <tr>
                <th className="px-4 py-2.5 font-medium w-48">歌名</th>
                <th className="px-4 py-2.5 font-medium w-36">歌手</th>
                <th className="px-4 py-2.5 font-medium w-40">专辑 · 年份</th>
                <th className="px-4 py-2.5 font-medium w-20">格式</th>
                <th className="px-4 py-2.5 font-medium">识别别名 (Aliases)</th>
                <th className="px-4 py-2.5 font-medium w-48">存储路径</th>
                <th className="px-4 py-2.5 font-medium w-24">ID</th>
                <th className="px-4 py-2.5 font-medium text-right w-24">操作</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border/60">
              {loading && tracks.length === 0 ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-muted-foreground">
                    <div className="flex flex-col items-center justify-center gap-2">
                      <RefreshCw className="w-5 h-5 animate-spin text-primary" />
                      <span>正在检索曲库索引快照...</span>
                    </div>
                  </td>
                </tr>
              ) : tracks.length === 0 ? (
                <tr>
                  <td colSpan={8} className="px-4 py-12 text-center text-muted-foreground">
                    未找到匹配的音频曲目
                  </td>
                </tr>
              ) : (
                tracks.map((track) => (
                  <tr key={track.id} className="hover:bg-muted/30 transition-colors">
                    {/* Title + Score Badge + tag_source badge */}
                    <td className="px-4 py-2.5 font-medium text-foreground">
                      <div className="flex items-center gap-1.5 flex-wrap">
                        <span className="font-semibold">{track.title}</span>
                        {debouncedQ.trim() && track.score !== undefined && (
                          <Badge
                            variant="success"
                            className="font-mono text-[10px] py-0 px-1 gap-0.5 shrink-0"
                          >
                            <Sparkles className="w-2.5 h-2.5" />
                            <span>{track.score}分</span>
                          </Badge>
                        )}
                        {track.tag_source && track.tag_source !== 'filename' && (
                          <Badge
                            variant="outline"
                            className="text-[9px] py-0 px-1 text-muted-foreground/80 font-sans border-border/70 bg-muted/30 shrink-0"
                            title={`元数据来源: ${track.tag_source === 'tags' ? '内嵌音频标签' : '文件名与标签混合'}`}
                          >
                            {track.tag_source === 'tags' ? '标签' : '混合'}
                          </Badge>
                        )}
                      </div>
                    </td>

                    {/* Artists Chips */}
                    <td className="px-4 py-2.5">
                      <div className="flex flex-wrap gap-1">
                        {(track.artists || [track.artist]).map((a) => (
                          <Badge
                            key={a}
                            variant="outline"
                            className="text-[10px] py-0 px-1.5 font-sans"
                          >
                            {a}
                          </Badge>
                        ))}
                      </div>
                    </td>

                    {/* 专辑 · 年份 */}
                    <td className="px-4 py-2.5 text-foreground">
                      {track.album ? (
                        <div
                          className="text-xs truncate max-w-[160px]"
                          title={`${track.album}${track.year ? ` · ${track.year}` : ''}`}
                        >
                          <span>{track.album}</span>
                          {track.year ? (
                            <span className="text-muted-foreground font-mono text-[11px] ml-1">
                              · {track.year}
                            </span>
                          ) : null}
                        </div>
                      ) : (
                        <span className="text-muted-foreground/50 text-[11px]">-</span>
                      )}
                    </td>

                    {/* Format / Ext */}
                    <td className="px-4 py-2.5">
                      <Badge
                        variant="secondary"
                        className="font-mono text-[10px] uppercase py-0 px-1.5"
                      >
                        .{track.ext}
                      </Badge>
                    </td>

                    {/* Alias chips */}
                    <td className="px-4 py-2.5">
                      {track.alias && track.alias.length > 0 ? (
                        <div className="flex flex-wrap items-center gap-1">
                          {track.alias.map((al) => (
                            <span
                              key={al}
                              className="font-mono text-[10px] px-1.5 py-0.5 rounded bg-muted/60 border border-border/80 text-foreground"
                            >
                              {al}
                            </span>
                          ))}
                        </div>
                      ) : (
                        <span className="text-muted-foreground/60 text-[11px]">-</span>
                      )}
                    </td>

                    {/* Path with copy button */}
                    <td className="px-4 py-2.5">
                      <div className="flex items-center gap-1 text-muted-foreground max-w-xs">
                        <span
                          className="font-mono text-[11px] truncate"
                          title={track.path}
                        >
                          {track.path}
                        </span>
                        <button
                          type="button"
                          onClick={() => handleCopy(track.path, 'path')}
                          className="p-1 hover:text-foreground rounded shrink-0 transition-colors"
                          title="复制路径"
                        >
                          {copiedPath === track.path ? (
                            <Check className="w-3 h-3 text-emerald-500" />
                          ) : (
                            <Copy className="w-3 h-3" />
                          )}
                        </button>
                      </div>
                    </td>

                    {/* ID with copy button */}
                    <td className="px-4 py-2.5">
                      <div className="flex items-center gap-1 font-mono text-[11px] text-muted-foreground">
                        <span>{track.id}</span>
                        <button
                          type="button"
                          onClick={() => handleCopy(track.id, 'id')}
                          className="p-1 hover:text-foreground rounded shrink-0 transition-colors"
                          title="复制 ID"
                        >
                          {copiedId === track.id ? (
                            <Check className="w-3 h-3 text-emerald-500" />
                          ) : (
                            <Copy className="w-3 h-3" />
                          )}
                        </button>
                      </div>
                    </td>

                    {/* Row action: 编辑别名 */}
                    <td className="px-4 py-2.5 text-right">
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => handleOpenEditAlias(track)}
                        className="h-7 text-xs gap-1 text-primary hover:text-primary hover:bg-primary/10"
                      >
                        <Edit3 className="w-3 h-3" />
                        编辑别名
                      </Button>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </CardContent>

      {/* Pagination Footer */}
      <CardFooter className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pt-3 border-t bg-muted/10 text-xs text-muted-foreground">
        <div>
          显示 {total > 0 ? offset + 1 : 0} - {Math.min(offset + limit, total)} 首，共{' '}
          {total.toLocaleString()} 首
        </div>

        <div className="flex items-center gap-2 self-end sm:self-auto">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setOffset(Math.max(0, offset - limit))}
            disabled={offset === 0 || loading}
            className="h-7 px-2 text-xs"
          >
            <ChevronLeft className="w-3.5 h-3.5 mr-0.5" />
            上一页
          </Button>
          <span className="font-mono text-xs px-1">
            {currentPage} / {totalPages}
          </span>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setOffset(offset + limit)}
            disabled={offset + limit >= total || loading}
            className="h-7 px-2 text-xs"
          >
            下一页
            <ChevronRight className="w-3.5 h-3.5 ml-0.5" />
          </Button>
        </div>
      </CardFooter>

      {/* Inline Alias Edit Dialog */}
      {editingTrack && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-background/80 backdrop-blur-xs animate-in fade-in duration-150">
          <div
            className="w-full max-w-md bg-card border rounded-xl shadow-xl overflow-hidden p-5 space-y-4 animate-in zoom-in-95 duration-150"
            onClick={(e) => e.stopPropagation()}
          >
            <div className="flex items-center justify-between border-b pb-3">
              <div>
                <h3 className="text-sm font-bold text-foreground">
                  编辑别名: 《{editingTrack.title}》
                </h3>
                <p className="text-xs text-muted-foreground mt-0.5">
                  歌手: {editingTrack.artist}
                </p>
              </div>
              <button
                type="button"
                onClick={() => setEditingTrack(null)}
                className="text-muted-foreground hover:text-foreground p-1 rounded"
              >
                <X className="w-4 h-4" />
              </button>
            </div>

            {/* Alias Chips Editor */}
            <div className="space-y-2">
              <label className="text-xs font-semibold text-foreground">
                当前识别别名列表:
              </label>
              <div className="flex flex-wrap items-center gap-1.5 p-3 rounded-lg border bg-muted/20 min-h-[50px]">
                {aliasChips.map((chip) => (
                  <div
                    key={chip}
                    className="inline-flex items-center gap-1 px-2.5 py-1 rounded-md text-xs font-mono bg-background border text-foreground"
                  >
                    <span>{chip}</span>
                    <button
                      type="button"
                      onClick={() => handleRemoveAliasChip(chip)}
                      className="text-muted-foreground hover:text-destructive p-0.5"
                    >
                      <X className="w-3 h-3" />
                    </button>
                  </div>
                ))}

                {/* Add chip input */}
                <div className="inline-flex items-center gap-1">
                  <Input
                    value={newAliasInput}
                    onChange={(e) => setNewAliasInput(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') {
                        e.preventDefault();
                        handleAddAliasChip();
                      }
                    }}
                    placeholder="+ 别名"
                    className="h-7 w-24 text-xs font-mono px-2 bg-background"
                  />
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={handleAddAliasChip}
                    disabled={!newAliasInput.trim()}
                    className="h-7 px-2 text-xs"
                  >
                    <Plus className="w-3 h-3" />
                  </Button>
                </div>
              </div>
              <p className="text-[11px] text-muted-foreground">
                提示：别名将自动同步合并至系统的音乐别名词典，用于拼音模糊与多语种点播识别
              </p>
            </div>

            {/* Footer Buttons */}
            <div className="flex items-center justify-end gap-2 pt-2 border-t">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setEditingTrack(null)}
                className="text-xs"
              >
                取消
              </Button>
              <Button
                size="sm"
                onClick={handleSaveAlias}
                disabled={isSavingAlias}
                className="gap-1.5 text-xs shadow-xs"
              >
                {isSavingAlias ? (
                  <RefreshCw className="w-3.5 h-3.5 animate-spin" />
                ) : (
                  <Save className="w-3.5 h-3.5" />
                )}
                {isSavingAlias ? '正在保存...' : '保存别名'}
              </Button>
            </div>
          </div>
        </div>
      )}
    </Card>
  );
}