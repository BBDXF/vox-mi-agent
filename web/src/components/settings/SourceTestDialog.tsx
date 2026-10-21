import { useState, useRef } from 'react';
import {
  Play,
  RefreshCw,
  X,
  Clock,
  Sparkles,
  Check,
  Plus,
  Copy,
  AlertCircle,
  FileTerminal,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { Textarea } from '@/components/ui/textarea';
import { useToast } from '@/components/ui/toast';
import {
  testDataSource,
  type SourceTestRequest,
  type SourceTestResponse,
} from '@/lib/api';

interface SourceTestDialogProps {
  sourceName: string | null;
  defaultKeys?: string[];
  onClose: () => void;
}

const JQ_EXAMPLES = [
  {
    label: '条件过滤: 匹配歌手为「邓紫棋」的歌名',
    filter: 'select(.artist|test("邓紫棋"))|.title',
    slurp: false,
  },
  {
    label: '聚合统计: 按歌手歌曲数分组排名前 10 (Slurp)',
    filter: '[.[]|.artist]|group_by(.)|map({artist:.[0],n:length})|sort_by(-.n)|.[:10]',
    slurp: true,
  },
  {
    label: '总记录行数统计: length (Slurp)',
    filter: 'length',
    slurp: true,
  },
];

export function SourceTestDialog({
  sourceName,
  defaultKeys = ['title', 'artist'],
  onClose,
}: SourceTestDialogProps) {
  const { success, error: toastError } = useToast();

  const [tool, setTool] = useState<'fuzzy_match' | 'query_json' | 'query_yaml'>('fuzzy_match');

  // fuzzy_match state
  const [fuzzyQuery, setFuzzyQuery] = useState('晴天');
  const [fuzzyKeys, setFuzzyKeys] = useState<string[]>(defaultKeys.length > 0 ? defaultKeys : ['title', 'artist']);
  const [newKeyInput, setNewKeyInput] = useState('');
  const [fuzzyLimit, setFuzzyLimit] = useState(5);

  // query_json state
  const [jsonFilter, setJsonFilter] = useState('select(.artist|test("邓紫棋"))|.title');
  const [slurp, setSlurp] = useState(false);
  const [raw, setRaw] = useState(false);

  // query_yaml state
  const [yamlExpr, setYamlExpr] = useState('.stations[] | .title');
  const [yamlOutputFormat, setYamlOutputFormat] = useState('json');

  // Execution result
  const [running, setRunning] = useState(false);
  const [result, setResult] = useState<SourceTestResponse | null>(null);
  const [copiedResult, setCopiedResult] = useState(false);

  const isMountedRef = useRef(true);

  if (!sourceName) return null;

  const handleAddFuzzyKey = () => {
    const k = newKeyInput.trim();
    if (!k) return;
    if (!fuzzyKeys.includes(k)) {
      setFuzzyKeys([...fuzzyKeys, k]);
    }
    setNewKeyInput('');
  };

  const handleRemoveFuzzyKey = (keyToRemove: string) => {
    setFuzzyKeys(fuzzyKeys.filter((k) => k !== keyToRemove));
  };

  const handleSelectExample = (ex: (typeof JQ_EXAMPLES)[0]) => {
    setJsonFilter(ex.filter);
    setSlurp(ex.slurp);
  };

  const handleRun = async () => {
    setRunning(true);
    setResult(null);

    const body: SourceTestRequest = {
      tool,
    };

    if (tool === 'fuzzy_match') {
      body.query = fuzzyQuery;
      body.keys = fuzzyKeys;
      body.limit = fuzzyLimit;
    } else if (tool === 'query_json') {
      body.filter = jsonFilter;
      body.slurp = slurp;
      body.raw = raw;
    } else if (tool === 'query_yaml') {
      body.expr = yamlExpr;
      body.output_format = yamlOutputFormat;
    }

    try {
      const res = await testDataSource(sourceName, body);
      if (isMountedRef.current) {
        setResult(res);
      }
    } catch (err: any) {
      if (isMountedRef.current) {
        toastError(err.message || '执行数据源测试失败');
        setResult({ error: err.message || '执行失败' });
      }
    } finally {
      if (isMountedRef.current) {
        setRunning(false);
      }
    }
  };

  const handleCopyResult = () => {
    if (!result) return;
    navigator.clipboard.writeText(JSON.stringify(result, null, 2));
    setCopiedResult(true);
    setTimeout(() => setCopiedResult(false), 1500);
    success('已复制测试结果');
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center p-3 sm:p-4 bg-background/80 backdrop-blur-xs animate-in fade-in duration-150">
      <div
        className="w-full max-w-3xl max-h-[90vh] bg-card border rounded-xl shadow-2xl flex flex-col overflow-hidden animate-in zoom-in-95 duration-200"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="px-5 py-3.5 border-b bg-muted/20 flex items-center justify-between shrink-0">
          <div className="flex items-center gap-2">
            <FileTerminal className="w-4 h-4 text-primary" />
            <div>
              <h3 className="text-sm font-bold text-foreground">
                数据源交互测试 (Test Run): {sourceName}
              </h3>
              <p className="text-[11px] text-muted-foreground mt-0.5">
                模拟 Agent 调用底层 fzq 拼音匹配与 jq / yq 表达式处理管道
              </p>
            </div>
          </div>
          <button
            type="button"
            onClick={onClose}
            className="p-1.5 text-muted-foreground hover:text-foreground rounded transition-colors"
          >
            <X className="w-4 h-4" />
          </button>
        </div>

        {/* Content Body */}
        <div className="flex-1 overflow-y-auto p-5 space-y-5 text-xs">
          {/* Tool Selector Tabs */}
          <div className="space-y-1.5">
            <label className="font-semibold text-foreground">选择执行测试工具 (Tool):</label>
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                onClick={() => setTool('fuzzy_match')}
                className={`px-3 py-1.5 rounded-lg border font-mono text-xs transition-colors ${
                  tool === 'fuzzy_match'
                    ? 'bg-primary text-primary-foreground font-semibold shadow-xs'
                    : 'bg-muted/40 hover:bg-muted text-foreground'
                }`}
              >
                fuzzy_match (fzq 模糊检索)
              </button>
              <button
                type="button"
                onClick={() => setTool('query_json')}
                className={`px-3 py-1.5 rounded-lg border font-mono text-xs transition-colors ${
                  tool === 'query_json'
                    ? 'bg-primary text-primary-foreground font-semibold shadow-xs'
                    : 'bg-muted/40 hover:bg-muted text-foreground'
                }`}
              >
                query_json (jq 语法过滤)
              </button>
              <button
                type="button"
                onClick={() => setTool('query_yaml')}
                className={`px-3 py-1.5 rounded-lg border font-mono text-xs transition-colors ${
                  tool === 'query_yaml'
                    ? 'bg-primary text-primary-foreground font-semibold shadow-xs'
                    : 'bg-muted/40 hover:bg-muted text-foreground'
                }`}
              >
                query_yaml (yq 语法解析)
              </button>
            </div>
          </div>

          {/* Tool 1: fuzzy_match form */}
          {tool === 'fuzzy_match' && (
            <div className="p-4 rounded-lg border bg-muted/20 space-y-3">
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                <div className="space-y-1">
                  <label className="text-[11px] font-medium text-muted-foreground">
                    检索意图词 (Query)
                  </label>
                  <Input
                    value={fuzzyQuery}
                    onChange={(e) => setFuzzyQuery(e.target.value)}
                    placeholder="如 晴天 或 Jay"
                    className="h-8 text-xs bg-background"
                  />
                </div>

                <div className="space-y-1">
                  <label className="text-[11px] font-medium text-muted-foreground">
                    单次返回候选上限 (Limit)
                  </label>
                  <Input
                    type="number"
                    min="1"
                    max="50"
                    value={fuzzyLimit}
                    onChange={(e) => setFuzzyLimit(parseInt(e.target.value, 10) || 5)}
                    className="h-8 text-xs font-mono bg-background"
                  />
                </div>
              </div>

              {/* Keys chips */}
              <div className="space-y-1.5">
                <label className="text-[11px] font-medium text-muted-foreground">
                  参与拼音加权字段 (Keys):
                </label>
                <div className="flex flex-wrap items-center gap-1.5">
                  {fuzzyKeys.map((k) => (
                    <div
                      key={k}
                      className="inline-flex items-center gap-1 px-2 py-0.5 rounded text-[11px] font-mono bg-background border text-foreground"
                    >
                      <span>{k}</span>
                      <button
                        type="button"
                        onClick={() => handleRemoveFuzzyKey(k)}
                        className="text-muted-foreground hover:text-destructive"
                      >
                        <X className="w-2.5 h-2.5" />
                      </button>
                    </div>
                  ))}

                  <div className="inline-flex items-center gap-1">
                    <Input
                      value={newKeyInput}
                      onChange={(e) => setNewKeyInput(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault();
                          handleAddFuzzyKey();
                        }
                      }}
                      placeholder="+ 字段"
                      className="h-6.5 w-18 text-[11px] font-mono px-1.5 bg-background"
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      onClick={handleAddFuzzyKey}
                      disabled={!newKeyInput.trim()}
                      className="h-6.5 px-1 text-[11px]"
                    >
                      <Plus className="w-3 h-3" />
                    </Button>
                  </div>
                </div>
              </div>
            </div>
          )}

          {/* Tool 2: query_json form */}
          {tool === 'query_json' && (
            <div className="p-4 rounded-lg border bg-muted/20 space-y-3">
              {/* Examples Dropdown */}
              <div className="space-y-1">
                <div className="flex items-center justify-between">
                  <label className="text-[11px] font-medium text-muted-foreground flex items-center gap-1">
                    <Sparkles className="w-3 h-3 text-primary" />
                    常用 jq 示例模版 (Examples):
                  </label>
                </div>
                <div className="grid grid-cols-1 gap-1.5 pt-0.5">
                  {JQ_EXAMPLES.map((ex, idx) => (
                    <button
                      key={idx}
                      type="button"
                      onClick={() => handleSelectExample(ex)}
                      className="text-left px-2.5 py-1.5 rounded border bg-background hover:bg-muted/80 text-[11px] transition-colors flex items-center justify-between group"
                    >
                      <span className="font-sans font-medium text-foreground">
                        {ex.label}
                      </span>
                      <code className="text-[10px] font-mono text-muted-foreground group-hover:text-primary">
                        {ex.filter.slice(0, 30)}...
                      </code>
                    </button>
                  ))}
                </div>
              </div>

              {/* Filter Textarea */}
              <div className="space-y-1">
                <label className="text-[11px] font-medium text-muted-foreground">
                  jq 过滤器表达式 (Filter):
                </label>
                <Textarea
                  value={jsonFilter}
                  onChange={(e) => setJsonFilter(e.target.value)}
                  placeholder="select(.artist|test('...'))"
                  rows={3}
                  className="font-mono text-xs bg-background"
                />
              </div>

              {/* Switches: slurp & raw */}
              <div className="flex items-center gap-6 pt-1">
                <label className="flex items-center gap-2 cursor-pointer select-none">
                  <input
                    type="checkbox"
                    checked={slurp}
                    onChange={(e) => setSlurp(e.target.checked)}
                    className="rounded border-input text-primary focus:ring-primary"
                  />
                  <span className="text-[11px] font-mono">
                    slurp (读取全量数组)
                  </span>
                </label>

                <label className="flex items-center gap-2 cursor-pointer select-none">
                  <input
                    type="checkbox"
                    checked={raw}
                    onChange={(e) => setRaw(e.target.checked)}
                    className="rounded border-input text-primary focus:ring-primary"
                  />
                  <span className="text-[11px] font-mono">
                    raw (输出纯文本)
                  </span>
                </label>
              </div>
            </div>
          )}

          {/* Tool 3: query_yaml form */}
          {tool === 'query_yaml' && (
            <div className="p-4 rounded-lg border bg-muted/20 space-y-3">
              <div className="space-y-1">
                <label className="text-[11px] font-medium text-muted-foreground">
                  yq 语法解析表达式 (Expression):
                </label>
                <Input
                  value={yamlExpr}
                  onChange={(e) => setYamlExpr(e.target.value)}
                  placeholder=".stations[] | .title"
                  className="font-mono text-xs bg-background h-8"
                />
              </div>

              <div className="space-y-1 max-w-xs">
                <label className="text-[11px] font-medium text-muted-foreground">
                  输出格式 (Output Format):
                </label>
                <select
                  value={yamlOutputFormat}
                  onChange={(e) => setYamlOutputFormat(e.target.value)}
                  className="h-8 rounded-md border border-input bg-background px-2 text-xs font-mono"
                >
                  <option value="json">JSON</option>
                  <option value="yaml">YAML</option>
                </select>
              </div>
            </div>
          )}

          {/* Run Action Bar */}
          <div className="flex items-center justify-between pt-1">
            <Button
              type="button"
              onClick={handleRun}
              disabled={running}
              size="sm"
              className="gap-2 text-xs shadow-xs"
            >
              {running ? (
                <RefreshCw className="w-3.5 h-3.5 animate-spin" />
              ) : (
                <Play className="w-3.5 h-3.5 fill-current" />
              )}
              {running ? '正在执行查询...' : '运行测试 (Execute)'}
            </Button>

            {result?.elapsed_ms !== undefined && (
              <Badge variant="outline" className="font-mono text-[11px] gap-1">
                <Clock className="w-3 h-3 text-emerald-500" />
                耗时: {result.elapsed_ms}ms
              </Badge>
            )}
          </div>

          {/* Result Panel */}
          {result && (
            <div className="space-y-2 pt-2 border-t">
              <div className="flex items-center justify-between">
                <div className="font-semibold text-foreground flex items-center gap-1.5">
                  <span>执行输出结果 (Result):</span>
                  {result.error && (
                    <Badge variant="destructive" className="text-[10px] py-0">
                      发生异常
                    </Badge>
                  )}
                </div>

                <Button
                  variant="ghost"
                  size="sm"
                  onClick={handleCopyResult}
                  className="h-7 text-xs gap-1"
                >
                  {copiedResult ? (
                    <Check className="w-3 h-3 text-emerald-500" />
                  ) : (
                    <Copy className="w-3 h-3" />
                  )}
                  <span>复制结果</span>
                </Button>
              </div>

              {result.error ? (
                <div className="p-3 bg-destructive/10 border border-destructive/20 text-destructive rounded-lg font-mono text-xs flex items-start gap-2">
                  <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />
                  <div>{result.error}</div>
                </div>
              ) : (
                <pre className="p-3.5 rounded-lg border bg-muted/40 font-mono text-xs overflow-x-auto max-h-64 leading-relaxed whitespace-pre-wrap">
                  {JSON.stringify(result, null, 2)}
                </pre>
              )}
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="px-5 py-3 border-t bg-muted/10 flex items-center justify-end text-xs shrink-0">
          <Button variant="outline" size="sm" onClick={onClose} className="text-xs">
            关闭
          </Button>
        </div>
      </div>
    </div>
  );
}