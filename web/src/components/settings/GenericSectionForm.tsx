import { useState, useEffect, useRef } from 'react';
import {
  Save,
  RefreshCw,
  AlertTriangle,
  AlertCircle,
  Eye,
  EyeOff,
  Plus,
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
import { Badge } from '@/components/ui/badge';
import { useToast } from '@/components/ui/toast';
import {
  getSectionSettings,
  updateSectionSettings,
  type SchemaSection,
  type SchemaField,
} from '@/lib/api';

interface GenericSectionFormProps {
  section: SchemaSection;
  onSaved?: () => void;
}

// Helpers for dotted key access (e.g. "shell.enabled")
function getDottedValue(obj: Record<string, any>, key: string): any {
  if (obj == null) return undefined;
  if (key in obj) return obj[key];
  const parts = key.split('.');
  let curr = obj;
  for (const p of parts) {
    if (curr == null || typeof curr !== 'object') return undefined;
    curr = curr[p];
  }
  return curr;
}

function setDottedValue(
  obj: Record<string, any>,
  key: string,
  val: any
): Record<string, any> {
  const next = { ...obj };
  next[key] = val;
  return next;
}

export function GenericSectionForm({ section, onSaved }: GenericSectionFormProps) {
  const { success, error: toastError } = useToast();

  const [loading, setLoading] = useState(false);
  const [initialValues, setInitialValues] = useState<Record<string, any>>({});
  const [values, setValues] = useState<Record<string, any>>({});
  const [isSaving, setIsSaving] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<Record<string, string>>({});
  const [restartBanner, setRestartBanner] = useState(false);
  const [envLocked, setEnvLocked] = useState<Record<string, string>>({});

  // Secret visibility map
  const [showSecrets, setShowSecrets] = useState<Record<string, boolean>>({});

  // Temporary input for string[] fields
  const [chipInputs, setChipInputs] = useState<Record<string, string>>({});

  const isMountedRef = useRef(true);

  // Fetch section values
  const fetchValues = async () => {
    setLoading(true);
    setFieldErrors({});
    try {
      const data = await getSectionSettings(section.name);
      if (!isMountedRef.current) return;
      setEnvLocked((data && data.env_locked) || {});
      setInitialValues(data || {});
      setValues(data || {});
    } catch (err: any) {
      if (!isMountedRef.current) return;
      toastError(err.message || `加载配置项 [${section.name}] 失败`);
    } finally {
      if (isMountedRef.current) setLoading(false);
    }
  };

  useEffect(() => {
    isMountedRef.current = true;
    fetchValues();
    setRestartBanner(false);
    return () => {
      isMountedRef.current = false;
    };
  }, [section.name]);

  // Handle field change
  const handleChange = (key: string, val: any) => {
    setValues((prev) => setDottedValue(prev, key, val));
    if (fieldErrors[key]) {
      setFieldErrors((prev) => {
        const copy = { ...prev };
        delete copy[key];
        return copy;
      });
    }
  };

  // Add chip to string[] field
  const handleAddChip = (key: string) => {
    const raw = (chipInputs[key] || '').trim();
    if (!raw) return;
    const currentList: string[] = getDottedValue(values, key) || [];
    if (!currentList.includes(raw)) {
      handleChange(key, [...currentList, raw]);
    }
    setChipInputs((prev) => ({ ...prev, [key]: '' }));
  };

  // Remove chip from string[] field
  const handleRemoveChip = (key: string, index: number) => {
    const currentList: string[] = getDottedValue(values, key) || [];
    const next = currentList.filter((_, idx) => idx !== index);
    handleChange(key, next);
  };

  // Check dirty state
  const isDirty = section.fields.some((f) => {
    const initV = getDottedValue(initialValues, f.key);
    const currV = getDottedValue(values, f.key);

    if (f.secret) {
      // Secret is considered dirty only if user typed non-empty text different from empty
      return typeof currV === 'string' && currV.length > 0 && currV !== initV;
    }

    if (f.type === 'string[]') {
      const a: string[] = Array.isArray(initV) ? initV : [];
      const b: string[] = Array.isArray(currV) ? currV : [];
      if (a.length !== b.length) return true;
      return a.some((item, idx) => item !== b[idx]);
    }

    if (initV === undefined && currV === undefined) return false;
    return initV !== currV;
  });

  // Handle Save
  const handleSave = async () => {
    if (isSaving || !isDirty) return;
    setIsSaving(true);
    setFieldErrors({});

    try {
      // Prepare payload: omit empty secret inputs so existing secret is preserved
      const payload: Record<string, any> = {};
      for (const f of section.fields) {
        if (envLocked[f.key] || f.read_only) continue;
        const currV = getDottedValue(values, f.key);
        if (f.secret) {
          if (currV && typeof currV === 'string' && currV.trim().length > 0 && !currV.startsWith('****')) {
            payload[f.key] = currV.trim();
          }
        } else {
          payload[f.key] = currV !== undefined ? currV : f.default;
        }
      }

      const res = await updateSectionSettings(section.name, payload);
      setInitialValues({ ...values });

      if (res.restart_required) {
        setRestartBanner(true);
        success('配置已保存，部分设置需重启系统服务生效');
      } else {
        setRestartBanner(false);
        success('配置已成功保存');
      }

      onSaved?.();
    } catch (err: any) {
      if (err.fields && typeof err.fields === 'object') {
        setFieldErrors(err.fields);
      }
      toastError(err.message || '保存设置失败');
    } finally {
      if (isMountedRef.current) setIsSaving(false);
    }
  };

  // Render individual field
  const renderFieldInput = (field: SchemaField) => {
    const val = getDottedValue(values, field.key) ?? field.default ?? '';

    // If marked secret, render masked password input
    if (field.secret) {
      const isVisible = showSecrets[field.key];
      const hasExisting = Boolean(getDottedValue(initialValues, field.key));
      return (
        <div className="space-y-1 max-w-md">
          <div className="relative">
            <Input
              type={isVisible ? 'text' : 'password'}
              value={val && !String(val).startsWith('****') ? val : ''}
              onChange={(e) => handleChange(field.key, e.target.value)}
              placeholder={hasExisting ? '已设置密钥 (留空保持不变)' : '未设置，请输入密钥'}
              className="font-mono text-xs pr-9 bg-muted/20"
            />
            <button
              type="button"
              onClick={() =>
                setShowSecrets((prev) => ({
                  ...prev,
                  [field.key]: !prev[field.key],
                }))
              }
              className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground p-1 transition-colors"
              title={isVisible ? '隐藏' : '显示'}
            >
              {isVisible ? <EyeOff className="w-3.5 h-3.5" /> : <Eye className="w-3.5 h-3.5" />}
            </button>
          </div>
          {hasExisting && (
            <div className="text-[10px] text-muted-foreground font-mono">
              当前已配置鉴权 Token，留空保存不会覆盖原密码
            </div>
          )}
        </div>
      );
    }

    switch (field.type) {
      case 'bool':
        return (
          <div className="flex items-center gap-3">
            <label className="relative inline-flex items-center cursor-pointer select-none">
              <input
                type="checkbox"
                checked={Boolean(val)}
                onChange={(e) => handleChange(field.key, e.target.checked)}
                className="sr-only peer"
              />
              <div className="w-11 h-6 bg-muted peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-primary border border-border" />
              <span className="ml-3 text-xs font-mono text-muted-foreground">
                {val ? '已开启 (true)' : '已关闭 (false)'}
              </span>
            </label>
          </div>
        );

      case 'enum':
        return (
          <select
            value={val}
            onChange={(e) => handleChange(field.key, e.target.value)}
            className="flex h-9 w-full max-w-sm rounded-md border border-input bg-background px-3 py-1.5 text-xs font-medium shadow-xs transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
          >
            {field.enum?.map((opt) => (
              <option key={opt} value={opt}>
                {opt}
              </option>
            ))}
          </select>
        );

      case 'duration':
        return (
          <div className="max-w-sm space-y-1">
            <Input
              value={val}
              onChange={(e) => handleChange(field.key, e.target.value)}
              placeholder="如 6h, 30s, 15m"
              className="font-mono text-xs bg-muted/20"
            />
            <div className="text-[10px] text-muted-foreground font-mono">
              格式提示: 6h / 30s / 15m / 0 (单位支持 h, m, s)
            </div>
          </div>
        );

      case 'bytesize':
        return (
          <div className="max-w-sm space-y-1">
            <Input
              value={val}
              onChange={(e) => handleChange(field.key, e.target.value)}
              placeholder="如 64KiB, 128MiB, 10MiB"
              className="font-mono text-xs bg-muted/20"
            />
            <div className="text-[10px] text-muted-foreground font-mono">
              格式提示: 64KiB / 128MiB / 10MiB / 1GiB
            </div>
          </div>
        );

      case 'string[]': {
        const list: string[] = Array.isArray(val) ? val : [];
        return (
          <div className="space-y-2">
            <div className="flex flex-wrap items-center gap-1.5">
              {list.map((chip, idx) => (
                <div
                  key={idx}
                  className="inline-flex items-center gap-1 px-2.5 py-1 rounded-md text-xs font-mono font-medium bg-muted/60 border border-border/80 text-foreground"
                >
                  <span>{chip}</span>
                  <button
                    type="button"
                    onClick={() => handleRemoveChip(field.key, idx)}
                    className="text-muted-foreground hover:text-destructive rounded-full hover:bg-muted p-0.5 transition-colors"
                  >
                    <X className="w-3 h-3" />
                  </button>
                </div>
              ))}

              {/* Add chip input */}
              <div className="inline-flex items-center gap-1">
                <Input
                  value={chipInputs[field.key] || ''}
                  onChange={(e) =>
                    setChipInputs((prev) => ({
                      ...prev,
                      [field.key]: e.target.value,
                    }))
                  }
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      handleAddChip(field.key);
                    }
                  }}
                  placeholder="+ 新增"
                  className="h-7 w-24 text-xs font-mono px-2 bg-muted/20"
                />
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  onClick={() => handleAddChip(field.key)}
                  disabled={!chipInputs[field.key]?.trim()}
                  className="h-7 px-2 text-xs"
                >
                  <Plus className="w-3 h-3" />
                </Button>
              </div>
            </div>
          </div>
        );
      }

      case 'int':
        return (
          <Input
            type="number"
            step="1"
            value={val}
            onChange={(e) => handleChange(field.key, parseInt(e.target.value, 10) || 0)}
            className="font-mono text-xs max-w-sm bg-muted/20"
          />
        );

      case 'float':
        return (
          <Input
            type="number"
            step="0.1"
            value={val}
            onChange={(e) => handleChange(field.key, parseFloat(e.target.value) || 0)}
            className="font-mono text-xs max-w-sm bg-muted/20"
          />
        );

      default:
        return (
          <Input
            value={val}
            onChange={(e) => handleChange(field.key, e.target.value)}
            className="font-mono text-xs max-w-md bg-muted/20"
          />
        );
    }
  };

  return (
    <Card className="border shadow-xs">
      <CardHeader className="pb-3 border-b bg-muted/5">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-2">
          <div>
            <CardTitle>{section.title || section.name}</CardTitle>
            <CardDescription className="text-xs mt-0.5">
              模块配置架构基于系统参数规范动态驱动，修改后即时更新运行时
            </CardDescription>
          </div>
          <Button
            variant="ghost"
            size="sm"
            onClick={fetchValues}
            disabled={loading}
            className="gap-1.5 text-xs text-muted-foreground self-start sm:self-auto"
          >
            <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
            刷新此模块
          </Button>
        </div>
      </CardHeader>

      {/* Restart Required Banner */}
      {restartBanner && (
        <div className="mx-6 mt-4 p-3 bg-amber-500/10 border border-amber-500/25 rounded-lg flex items-start gap-2.5 text-xs text-amber-700 dark:text-amber-300 animate-in fade-in">
          <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5 text-amber-500" />
          <div className="space-y-0.5">
            <div className="font-semibold">配置已保存，部分修改需重启服务后生效</div>
            <div className="text-[11px] opacity-90">
              检测到核心网络/驱动参数变动，请在维护窗口重启 vox-mi-agent 守护进程以应用更新。
            </div>
          </div>
        </div>
      )}

      <CardContent className="p-6 space-y-6">
        {section.fields.map((field) => {
          const err = fieldErrors[field.key];
          return (
            <div
              key={field.key}
              className="space-y-1.5 pb-4 border-b border-border/50 last:border-0 last:pb-0"
            >
              <div className="flex items-center gap-2 flex-wrap">
                <label className="text-xs font-semibold font-mono text-foreground">
                  {field.key}
                </label>
                {field.restart_required && (
                  <Badge
                    variant="warning"
                    className="text-[10px] py-0 px-1.5 gap-0.5 font-sans"
                  >
                    需重启生效
                  </Badge>
                )}
              </div>

              {field.description && (
                <p className="text-[11px] text-muted-foreground leading-relaxed">
                  {field.description}
                </p>
              )}

              <div className="pt-1">
                {envLocked[field.key] || field.read_only ? (
                  <fieldset disabled className="opacity-70 [&_*]:cursor-not-allowed">
                    {renderFieldInput(field)}
                  </fieldset>
                ) : (
                  renderFieldInput(field)
                )}
                {envLocked[field.key] ? (
                  <p className="text-[11px] text-muted-foreground mt-1">
                    由环境变量 {envLocked[field.key]} 管理，在部署配置中修改（飞牛：应用设置）
                  </p>
                ) : field.read_only ? (
                  <p className="text-[11px] text-muted-foreground mt-1">
                    通过环境变量或配置文件设置（飞牛：应用设置）
                  </p>
                ) : null}
              </div>

              {/* Inline Field Error */}
              {err && (
                <div className="text-[11px] text-destructive flex items-center gap-1 font-sans bg-destructive/10 border border-destructive/20 rounded px-2 py-1 mt-1 w-fit">
                  <AlertCircle className="w-3.5 h-3.5 shrink-0" />
                  <span>{err}</span>
                </div>
              )}
            </div>
          );
        })}
      </CardContent>

      <CardFooter className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pt-4 border-t bg-muted/10">
        <div className="text-xs text-muted-foreground">
          {isDirty ? (
            <span className="text-amber-600 dark:text-amber-400 flex items-center gap-1.5 font-medium">
              <span className="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse" />
              检测到配置项变动，请点击保存更新
            </span>
          ) : (
            <span className="flex items-center gap-1.5 text-muted-foreground">
              <Check className="w-3.5 h-3.5 text-emerald-500" />
              配置已与服务保持同步
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
          {isSaving ? '正在保存...' : '保存配置'}
        </Button>
      </CardFooter>
    </Card>
  );
}