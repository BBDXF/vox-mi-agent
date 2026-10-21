import { useState, useEffect, useRef } from 'react';
import {
  Table,
  FileCode,
  RefreshCw,
  X,
  Copy,
  Check,
  Eye,
  AlertCircle,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { useToast } from '@/components/ui/toast';
import { getSourcePreview, type SourcePreviewResponse } from '@/lib/api';

interface SourcePreviewDialogProps {
  sourceName: string | null;
  onClose: () => void;
}

export function SourcePreviewDialog({
  sourceName,
  onClose,
}: SourcePreviewDialogProps) {
  const { success } = useToast();

  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<SourcePreviewResponse | null>(null);
  const [viewMode, setViewMode] = useState<'table' | 'json'>('table');
  const [copied, setCopied] = useState(false);

  const isMountedRef = useRef(true);

  useEffect(() => {
    isMountedRef.current = true;
    if (!sourceName) {
      setData(null);
      return;
    }

    setLoading(true);
    getSourcePreview(sourceName, 20)
      .then((res) => {
        if (isMountedRef.current) {
          setData(res);
        }
      })
      .catch(() => {
        if (isMountedRef.current) {
          setData({ total: 0, items: [] });
        }
      })
      .finally(() => {
        if (isMountedRef.current) {
          setLoading(false);
        }
      });

    return () => {
      isMountedRef.current = false;
    };
  }, [sourceName]);

  if (!sourceName) return null;

  const items = data?.items || [];
  const total = data?.total ?? 0;

  // Derive unique headers for table view
  const headers = Array.from(
    new Set(
      items.flatMap((it) =>
        it && typeof it === 'object' ? Object.keys(it) : ['value']
      )
    )
  );

  const handleCopyJSON = () => {
    navigator.clipboard.writeText(JSON.stringify(items, null, 2));
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
    success('已复制预览数据为 JSON');
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-3 sm:p-4 bg-background/80 backdrop-blur-xs animate-in fade-in duration-150">
      <div
        className="w-full max-w-4xl max-h-[85vh] bg-card border rounded-xl shadow-2xl flex flex-col overflow-hidden animate-in zoom-in-95 duration-200"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="px-5 py-3.5 border-b bg-muted/20 flex items-center justify-between shrink-0">
          <div className="flex items-center gap-2">
            <Eye className="w-4 h-4 text-primary" />
            <div>
              <div className="flex items-center gap-2">
                <h3 className="text-sm font-bold text-foreground">
                  数据源内容预览: {sourceName}
                </h3>
                <Badge variant="secondary" className="font-mono text-[10px] py-0">
                  共 {total.toLocaleString()} 条
                </Badge>
              </div>
              <p className="text-[11px] text-muted-foreground mt-0.5">
                当前提取前 20 条结构化记录快照
              </p>
            </div>
          </div>

          <div className="flex items-center gap-2">
            {/* View Mode Toggle */}
            <div className="flex items-center border rounded-md p-0.5 bg-muted/40 text-xs">
              <button
                type="button"
                onClick={() => setViewMode('table')}
                className={`flex items-center gap-1 px-2.5 py-1 rounded text-xs transition-colors ${
                  viewMode === 'table'
                    ? 'bg-background font-semibold text-foreground shadow-xs'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                <Table className="w-3.5 h-3.5" />
                <span>表格视图</span>
              </button>
              <button
                type="button"
                onClick={() => setViewMode('json')}
                className={`flex items-center gap-1 px-2.5 py-1 rounded text-xs transition-colors ${
                  viewMode === 'json'
                    ? 'bg-background font-semibold text-foreground shadow-xs'
                    : 'text-muted-foreground hover:text-foreground'
                }`}
              >
                <FileCode className="w-3.5 h-3.5" />
                <span>JSON 视图</span>
              </button>
            </div>

            <button
              type="button"
              onClick={onClose}
              className="p-1.5 text-muted-foreground hover:text-foreground rounded transition-colors"
            >
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>

        {/* Content Body */}
        <div className="flex-1 overflow-y-auto p-4 sm:p-5">
          {data?.error && (
            <div className="p-3 bg-amber-500/10 border border-amber-500/25 rounded-lg text-amber-700 dark:text-amber-300 text-xs flex items-center gap-2 mb-3">
              <AlertCircle className="w-4 h-4 text-amber-500 shrink-0" />
              <span>{data.error}</span>
            </div>
          )}

          {loading ? (
            <div className="h-60 flex flex-col items-center justify-center gap-2 text-muted-foreground text-xs">
              <RefreshCw className="w-5 h-5 animate-spin text-primary" />
              <span>正在加载数据源记录...</span>
            </div>
          ) : items.length === 0 ? (
            <div className="h-60 flex flex-col items-center justify-center text-center p-4 text-muted-foreground text-xs border border-dashed rounded-lg bg-muted/10 gap-1.5">
              <AlertCircle className="w-6 h-6 text-muted-foreground/60 mb-0.5" />
              <span className="font-semibold text-foreground">
                {data?.error ? data.error : '数据源暂无有效可读取记录'}
              </span>
              <span className="text-[11px]">
                {data?.error ? '底层文件可能尚未生成或已被清理' : '文件为空或记录过滤器未命中任何内容'}
              </span>
            </div>
          ) : viewMode === 'table' ? (
            <div className="border rounded-lg overflow-x-auto bg-background/50">
              <table className="w-full text-left text-xs">
                <thead className="bg-muted/50 border-b text-muted-foreground select-none">
                  <tr>
                    <th className="px-3 py-2 font-mono w-12 text-center">#</th>
                    {headers.map((h) => (
                      <th key={h} className="px-3 py-2 font-mono font-medium whitespace-nowrap">
                        {h}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody className="divide-y divide-border/60">
                  {items.map((row, rIdx) => (
                    <tr key={rIdx} className="hover:bg-muted/30 transition-colors">
                      <td className="px-3 py-2 text-center text-muted-foreground font-mono text-[11px]">
                        {rIdx + 1}
                      </td>
                      {headers.map((h) => {
                        const cellVal =
                          row && typeof row === 'object' ? row[h] : row;
                        const isArr = Array.isArray(cellVal);
                        const isObj =
                          cellVal && typeof cellVal === 'object' && !isArr;

                        return (
                          <td
                            key={h}
                            className="px-3 py-2 font-mono text-[11px] text-foreground max-w-xs truncate"
                            title={
                              typeof cellVal === 'string'
                                ? cellVal
                                : JSON.stringify(cellVal)
                            }
                          >
                            {isArr ? (
                              <div className="flex flex-wrap gap-1">
                                {cellVal.map((v: any, i: number) => (
                                  <span
                                    key={i}
                                    className="px-1.5 py-0.2 rounded bg-muted text-[10px]"
                                  >
                                    {String(v)}
                                  </span>
                                ))}
                              </div>
                            ) : isObj ? (
                              JSON.stringify(cellVal)
                            ) : (
                              String(cellVal ?? '-')
                            )}
                          </td>
                        );
                      })}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="relative">
              <div className="absolute right-3 top-3 z-10">
                <Button
                  size="sm"
                  variant="outline"
                  onClick={handleCopyJSON}
                  className="h-7 text-xs gap-1 bg-background"
                >
                  {copied ? (
                    <Check className="w-3 h-3 text-emerald-500" />
                  ) : (
                    <Copy className="w-3 h-3" />
                  )}
                  <span>复制 JSON</span>
                </Button>
              </div>
              <pre className="p-4 rounded-lg border bg-muted/30 font-mono text-xs overflow-x-auto max-h-[60vh] leading-relaxed">
                {JSON.stringify(items, null, 2)}
              </pre>
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="px-5 py-3 border-t bg-muted/10 flex items-center justify-between text-xs text-muted-foreground shrink-0">
          <div>共获取 {items.length} 条记录采样</div>
          <Button variant="outline" size="sm" onClick={onClose} className="text-xs">
            关闭
          </Button>
        </div>
      </div>
    </div>
  );
}