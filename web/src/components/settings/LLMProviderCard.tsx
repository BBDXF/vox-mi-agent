import { useState, useEffect, useRef } from 'react';
import { IconChip } from '@/components/ui/icon-chip';
import type { FormEvent } from 'react';
import {
  Cpu,
  Sparkles,
  Bot,
  Cloud,
  Server,
  Sliders,
  Lock,
  Unlock,
  Save,
  RefreshCw,
  CheckCircle2,
  AlertCircle,
  AlertTriangle,
  EyeOff,
  ExternalLink,
  Zap,
  Timer,
  ChevronDown,
  Search,
  Check,
  Layers,
  Globe,
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
  getLLMProviders,
  getLLMModels,
  testLLM,
  updateLLMSettings,
  type LLMProvider,
  type LLMSettings,
  type UpdateLLMRequest,
  type LLMModelItem,
  type TestLLMResponse,
} from '@/lib/api';
import { cn } from '@/lib/utils';

interface LLMProviderCardProps {
  currentSettings: LLMSettings;
  onSaved: () => Promise<void> | void;
}

function fmtEb(v: any): string {
  return v && typeof v === 'object' && Object.keys(v).length ? JSON.stringify(v, null, 2) : '';
}

export function LLMProviderCard({ currentSettings, onSaved }: LLMProviderCardProps) {
  const { success, error: toastError } = useToast();

  const locked: Record<string, string> = (currentSettings as any).env_locked || {};
  const isLocked = (k: string) => Boolean(locked[k]);
  const lockHint = (k: string) => (
    <span className="text-[11px] text-amber-600 dark:text-amber-400 block">
      由环境变量 {locked[k]} 管理，在部署配置中修改（飞牛：应用设置）
    </span>
  );

  // Provider presets
  const [providers, setProviders] = useState<LLMProvider[]>([]);
  const [loadingProviders, setLoadingProviders] = useState(false);

  // Form State
  const [selectedProviderId, setSelectedProviderId] = useState<string>(
    currentSettings.provider || 'opencode-zen'
  );
  const [baseUrl, setBaseUrl] = useState<string>(currentSettings.base_url || '');
  const [model, setModel] = useState<string>(currentSettings.model || '');
  const [apiKeyInput, setApiKeyInput] = useState<string>('');
  const [timeoutStr, setTimeoutStr] = useState<string>(String((currentSettings as any).timeout ?? ''));
  const [extraBodyStr, setExtraBodyStr] = useState<string>(fmtEb((currentSettings as any).extra_body));

  // Track original server state to detect provider switching & API key persistence
  const [initialProviderId, setInitialProviderId] = useState<string>(
    currentSettings.provider || 'opencode-zen'
  );
  const [hasApiKeyOnServer, setHasApiKeyOnServer] = useState<boolean>(
    Boolean(currentSettings.has_api_key)
  );
  const [apiKeyMaskedOnServer, setApiKeyMaskedOnServer] = useState<string>(
    currentSettings.api_key_masked || ''
  );

  // Models Combobox State
  const [modelsList, setModelsList] = useState<LLMModelItem[]>([]);
  const [loadingModels, setLoadingModels] = useState(false);
  const [modelsDropdownOpen, setModelsDropdownOpen] = useState(false);
  const [modelSearch, setModelSearch] = useState('');
  const [modelsFetchError, setModelsFetchError] = useState<string | null>(null);
  const comboboxRef = useRef<HTMLDivElement>(null);

  // Testing & Saving State
  const [isTesting, setIsTesting] = useState(false);
  const [testResult, setTestResult] = useState<TestLLMResponse | null>(null);
  const [isSaving, setIsSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);

  // Load provider presets on mount
  useEffect(() => {
    let mounted = true;
    async function loadProviders() {
      setLoadingProviders(true);
      try {
        const res = await getLLMProviders();
        if (mounted && res?.items && res.items.length > 0) {
          setProviders(res.items);
        }
      } catch {
        // ignore: keep empty preset list
      } finally {
        if (mounted) setLoadingProviders(false);
      }
    }
    loadProviders();
    return () => {
      mounted = false;
    };
  }, []);

  // Sync state when currentSettings prop updates
  useEffect(() => {
    const provId = currentSettings.provider || 'opencode-zen';
    setSelectedProviderId(provId);
    setInitialProviderId(provId);
    setBaseUrl(currentSettings.base_url || '');
    setModel(currentSettings.model || '');
    setTimeoutStr(String((currentSettings as any).timeout ?? ''));
    setExtraBodyStr(fmtEb((currentSettings as any).extra_body));
    setHasApiKeyOnServer(Boolean(currentSettings.has_api_key));
    setApiKeyMaskedOnServer(currentSettings.api_key_masked || '');
    setApiKeyInput('');
    setSaveError(null);
    setTestResult(null);
    setModelsFetchError(null);
  }, [currentSettings]);

  // Close combobox when clicking outside
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (comboboxRef.current && !comboboxRef.current.contains(event.target as Node)) {
        setModelsDropdownOpen(false);
      }
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => {
      document.removeEventListener('mousedown', handleClickOutside);
    };
  }, []);

  const currentPreset = providers.find((p) => p.id === selectedProviderId) || {
    id: selectedProviderId,
    name: selectedProviderId,
    base_url: baseUrl,
    default_model: model,
    key_required: true,
    base_url_editable: true,
    docs_url: '',
  };

  const isProviderChanged = selectedProviderId !== initialProviderId;

  // Handle switching provider
  const handleSelectProvider = (preset: LLMProvider) => {
    if (preset.id === selectedProviderId || isLocked('provider')) return;

    setSelectedProviderId(preset.id);
    setSaveError(null);
    setTestResult(null);
    setModelsFetchError(null);
    setModelsList([]);
    setModelsDropdownOpen(false);
    setModelSearch('');
    setApiKeyInput('');

    // Update Base URL: if switching back to initial saved provider, restore saved URL
    if (preset.id === initialProviderId && currentSettings.base_url) {
      setBaseUrl(currentSettings.base_url);
    } else if (!preset.base_url_editable) {
      setBaseUrl(preset.base_url);
    } else {
      const prevPreset = providers.find((p) => p.id === selectedProviderId);
      if (!prevPreset?.base_url_editable || !baseUrl) {
        setBaseUrl(preset.base_url);
      }
    }

    // Update Model: if switching back to initial saved provider, restore saved model
    if (preset.id === initialProviderId && currentSettings.model) {
      setModel(currentSettings.model);
    } else {
      const prevPreset = providers.find((p) => p.id === selectedProviderId);
      if (!model || model === prevPreset?.default_model) {
        setModel(preset.default_model);
      }
    }
  };

  // Fetch models for current provider
  const handleFetchModels = async () => {
    setLoadingModels(true);
    setSaveError(null);
    setModelsFetchError(null);
    try {
      const res = await getLLMModels({
        provider: selectedProviderId,
        base_url: baseUrl.trim() || undefined,
        api_key: apiKeyInput.trim() || undefined,
      });
      if (res?.items) {
        setModelsList(res.items);
        setModelsDropdownOpen(true);
        if (res.items.length === 0) {
          toastError('未获取到该端点下的可用模型');
        } else {
          success(`已成功拉取 ${res.items.length} 个模型`);
        }
      }
    } catch (err: any) {
      const errMsg = err.message || '获取模型列表失败，请检查密钥与端点连接';
      setModelsFetchError(errMsg);
      toastError(errMsg);
    } finally {
      setLoadingModels(false);
    }
  };

  // Test connection
  const handleTestConnection = async () => {
    if (!model.trim()) {
      toastError('请先指定要测试的模型名称');
      return;
    }

    setIsTesting(true);
    setTestResult(null);
    setSaveError(null);

    try {
      const res = await testLLM({
        provider: selectedProviderId,
        base_url: baseUrl.trim() || undefined,
        api_key: apiKeyInput.trim() || undefined,
        model: model.trim(),
      });
      setTestResult(res);
      if (res.ok) {
        success(`连接成功！响应耗时 ${res.latency_ms} ms`);
      } else {
        toastError(res.error || '连接测试未通过');
      }
    } catch (err: any) {
      const errMsg = err.message || '连接测试请求失败';
      setTestResult({ ok: false, error: errMsg });
      toastError(errMsg);
    } finally {
      setIsTesting(false);
    }
  };

  // Save form
  const handleSave = async (e: FormEvent) => {
    e.preventDefault();
    setIsSaving(true);
    setSaveError(null);

    try {
      const payload: UpdateLLMRequest = {};
      if (!isLocked('provider')) payload.provider = selectedProviderId;
      if (!isLocked('model')) payload.model = model.trim();
      if (timeoutStr.trim() && !isLocked('timeout')) payload.timeout = timeoutStr.trim();
      const ebText = extraBodyStr.trim();
      if (!isLocked('extra_body')) try {
        payload.extra_body = ebText ? JSON.parse(ebText) : {};
        if (typeof payload.extra_body !== 'object' || Array.isArray(payload.extra_body)) throw new Error();
      } catch {
        throw new Error('extra_body 必须是合法的 JSON 对象');
      }

      if (baseUrl.trim() && !isLocked('base_url')) {
        payload.base_url = baseUrl.trim();
      }

      // Only include api_key if typed
      if (apiKeyInput.trim() && !isLocked('api_key')) {
        payload.api_key = apiKeyInput.trim();
      }

      await updateLLMSettings(payload);
      success('LLM 模型服务配置已保存并生效');
      setApiKeyInput('');
      await onSaved();
    } catch (err: any) {
      const errorText = err.message || '保存配置失败 (400 Bad Request)';
      setSaveError(errorText);
      toastError(errorText);
    } finally {
      setIsSaving(false);
    }
  };

  // Filtered models for combobox
  const filteredModels = modelsList.filter((m) =>
    m.id.toLowerCase().includes(modelSearch.toLowerCase().trim())
  );

  // Helper icons for provider cards
  const getProviderIcon = (id: string) => {
    switch (id) {
      case 'opencode-zen':
        return <Sparkles className="w-4 h-4 text-amber-500 shrink-0" />;
      case 'deepseek':
        return <Bot className="w-4 h-4 text-blue-500 shrink-0" />;
      case 'dashscope':
        return <Cloud className="w-4 h-4 text-sky-500 shrink-0" />;
      case 'openai':
        return <Cpu className="w-4 h-4 text-emerald-500 shrink-0" />;
      case 'siliconflow':
        return <Layers className="w-4 h-4 text-cyan-500 shrink-0" />;
      case 'openrouter':
        return <Globe className="w-4 h-4 text-purple-500 shrink-0" />;
      case 'ollama':
        return <Server className="w-4 h-4 text-orange-500 shrink-0" />;
      case 'custom':
      default:
        return <Sliders className="w-4 h-4 text-violet-500 shrink-0" />;
    }
  };

  return (
    <Card className="border shadow-xs overflow-hidden">
      <CardHeader className="pb-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <IconChip icon={Cpu} color="llm" />
            <CardTitle>大语言模型 (LLM Provider)</CardTitle>
          </div>
          {loadingProviders && (
            <span className="text-xs text-muted-foreground flex items-center gap-1.5">
              <RefreshCw className="w-3.5 h-3.5 animate-spin" />
              正在同步预设...
            </span>
          )}
        </div>
        <CardDescription>
          负责智能语音意图识别、曲库多维度模糊检索编排与智能对话生成
        </CardDescription>
      </CardHeader>

      <CardContent className="space-y-6">
        {/* 1. Provider Selector Grid */}
        <div className="space-y-2.5">
          <div className="flex items-center justify-between">
            <label className="text-xs font-semibold text-foreground">
              选择模型服务商 (Provider)
            </label>
            <span className="text-[11px] text-muted-foreground">
              点击卡片快速切换服务商配置与接口预设
            </span>
          </div>

          {isLocked('provider') && lockHint('provider')}
          <div className={cn('grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2.5', isLocked('provider') && 'opacity-60 pointer-events-none')}>
            {providers.map((preset) => {
              const isSelected = selectedProviderId === preset.id;
              return (
                <div
                  key={preset.id}
                  onClick={() => handleSelectProvider(preset)}
                  role="button"
                  tabIndex={0}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      handleSelectProvider(preset);
                    }
                  }}
                  className={cn(
                    'group relative flex flex-col justify-between p-3 rounded-xl border text-left cursor-pointer transition-all duration-200 select-none outline-none focus-visible:ring-2 focus-visible:ring-primary',
                    isSelected
                      ? 'border-primary bg-primary/[0.05] dark:bg-primary/[0.09] ring-1 ring-primary/30 shadow-xs'
                      : 'border-border/70 hover:border-border hover:bg-muted/40'
                  )}
                >
                  <div className="flex items-start justify-between gap-2 mb-2">
                    <div className="flex items-center gap-2 min-w-0">
                      <div className="p-1.5 rounded-lg bg-background border shadow-xs group-hover:scale-105 transition-transform">
                        {getProviderIcon(preset.id)}
                      </div>
                      <div className="min-w-0">
                        <div className="font-semibold text-xs text-foreground truncate">
                          {preset.name}
                        </div>
                        <div className="text-[10px] text-muted-foreground truncate font-mono">
                          {preset.id}
                        </div>
                      </div>
                    </div>

                    <div className="flex items-center gap-1 shrink-0">
                      {isSelected ? (
                        <div className="w-4 h-4 rounded-full bg-primary text-primary-foreground flex items-center justify-center text-[10px]">
                          <Check className="w-2.5 h-2.5 stroke-[3]" />
                        </div>
                      ) : (
                        <div className="w-4 h-4 rounded-full border border-muted-foreground/30 group-hover:border-primary/50 transition-colors" />
                      )}
                    </div>
                  </div>

                  <div className="flex items-center justify-between text-[11px] pt-1 border-t border-border/40 mt-1">
                    <div className="flex items-center gap-1.5 min-w-0">
                      {!preset.key_required ? (
                        <span className="text-[10px] font-medium text-emerald-600 dark:text-emerald-400 bg-emerald-500/10 px-1.5 py-0.2 rounded">
                          免密钥
                        </span>
                      ) : preset.id === 'opencode-zen' || preset.id === 'openrouter' ? (
                        <span className="text-[10px] font-medium text-amber-600 dark:text-amber-400 bg-amber-500/10 px-1.5 py-0.2 rounded">
                          含免费模型
                        </span>
                      ) : (
                        <span className="text-[10px] text-muted-foreground">
                          {preset.base_url_editable ? '自建端点' : '官方接入'}
                        </span>
                      )}
                    </div>

                    {preset.docs_url && (
                      <a
                        href={preset.docs_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        onClick={(e) => e.stopPropagation()}
                        title={`查看 ${preset.name} 接入文档`}
                        className="inline-flex items-center gap-1 text-[11px] text-muted-foreground hover:text-primary transition-colors hover:underline"
                      >
                        <span>文档</span>
                        <ExternalLink className="w-2.5 h-2.5" />
                      </a>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        </div>

        {/* 2. Provider Switch Warning Alert */}
        {isProviderChanged && (
          <div className="rounded-xl border border-amber-500/30 bg-amber-500/10 dark:bg-amber-950/20 p-3 text-xs text-amber-700 dark:text-amber-300 flex items-start gap-2.5 animate-in fade-in-50">
            <AlertTriangle className="w-4 h-4 shrink-0 mt-0.5 text-amber-600 dark:text-amber-400" />
            <div className="space-y-0.5">
              <div className="font-semibold">已切换服务商为「{currentPreset.name}」</div>
              <p className="text-[11px] text-amber-800/90 dark:text-amber-300/90 leading-relaxed">
                原服务商配置的 API 密钥不会自动沿用。
                {currentPreset.key_required
                  ? '请在下方填入该服务商的有效 API Key 后再保存或测试。'
                  : '该服务商默认无需 API Key，如有自建鉴权也可选填。'}
              </p>
            </div>
          </div>
        )}

        {/* 3. Base URL & Model Inputs Form */}
        <form onSubmit={handleSave} id="llm-form" className="space-y-4">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            {/* Base URL */}
            <div className="space-y-1.5">
              <div className="flex items-center justify-between">
                <label className="text-xs font-semibold text-foreground flex items-center gap-1.5">
                  Base URL (API 接入端点)
                </label>
                {!currentPreset.base_url_editable ? (
                  <Badge
                    variant="outline"
                    className="text-[10px] text-muted-foreground gap-1 px-1.5 py-0 font-normal bg-muted/40"
                  >
                    <Lock className="w-2.5 h-2.5" />
                    固定官方端点
                  </Badge>
                ) : (
                  <Badge
                    variant="secondary"
                    className="text-[10px] text-primary gap-1 px-1.5 py-0 font-normal"
                  >
                    <Unlock className="w-2.5 h-2.5" />
                    可自定义端点
                  </Badge>
                )}
              </div>

              {!currentPreset.base_url_editable ? (
                <div className="flex h-9 w-full items-center rounded-md border border-input/60 bg-muted/50 px-3 py-1 font-mono text-xs text-muted-foreground select-all shadow-xs">
                  {baseUrl || currentPreset.base_url}
                </div>
              ) : (
                <Input
                  value={baseUrl}
                  onChange={(e) => setBaseUrl(e.target.value)}
                  placeholder={currentPreset.base_url || 'http://localhost:11434/v1'}
                  className="font-mono text-xs shadow-xs"
                  required
                  disabled={isLocked('base_url')}
                />
              )}
              {isLocked('base_url') && lockHint('base_url')}

              <span className="text-[11px] text-muted-foreground block">
                {!currentPreset.base_url_editable
                  ? '此服务商接入地址由官方锁定，防止输入格式偏差导致鉴权失败'
                  : '支持填入局域网私有化部署地址或自建网关，如 http://localhost:11434/v1'}
              </span>
            </div>

            {/* Model (Combobox) */}
            <div className="space-y-1.5 relative" ref={comboboxRef}>
              <div className="flex items-center justify-between">
                <label className="text-xs font-semibold text-foreground flex items-center gap-1.5">
                  模型名称 (Model)
                </label>
                {selectedProviderId === 'opencode-zen' && (
                  <span className="text-[11px] text-emerald-600 dark:text-emerald-400 flex items-center gap-1 font-medium">
                    <Sparkles className="w-3 h-3" />
                    -free 结尾为免费模型
                  </span>
                )}
                {selectedProviderId === 'openrouter' && (
                  <span className="text-[11px] text-emerald-600 dark:text-emerald-400 flex items-center gap-1 font-medium">
                    <Sparkles className="w-3 h-3" />
                    :free 结尾为免费模型
                  </span>
                )}
              </div>

              <div className="flex items-center gap-2">
                <div className="relative flex-1">
                  <Input
                    value={model}
                    onChange={(e) => {
                      setModel(e.target.value);
                      if (modelsList.length > 0 && !modelsDropdownOpen) {
                        setModelsDropdownOpen(true);
                      }
                    }}
                    onFocus={() => {
                      if (modelsList.length > 0) setModelsDropdownOpen(true);
                    }}
                    placeholder={
                      currentPreset.default_model ||
                      (selectedProviderId === 'openrouter'
                        ? '点击“获取模型列表”或输入模型 ID'
                        : '例如: gpt-4o-mini')
                    }
                    className="font-mono text-xs pr-8 shadow-xs"
                    required
                    disabled={isLocked('model')}
                  />
                  {modelsList.length > 0 && (
                    <button
                      type="button"
                      onClick={() => setModelsDropdownOpen(!modelsDropdownOpen)}
                      className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground p-1 transition-transform"
                      tabIndex={-1}
                      title="展开可用模型下拉列表"
                    >
                      <ChevronDown
                        className={cn(
                          'w-3.5 h-3.5 transition-transform duration-200',
                          modelsDropdownOpen && 'rotate-180'
                        )}
                      />
                    </button>
                  )}
                </div>

                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={loadingModels}
                  onClick={handleFetchModels}
                  className="gap-1.5 shrink-0 h-9 px-3 text-xs shadow-xs"
                >
                  <RefreshCw
                    className={cn('w-3.5 h-3.5', loadingModels && 'animate-spin')}
                  />
                  {loadingModels ? '拉取中...' : '获取模型列表'}
                </Button>
              </div>

              {isLocked('model') && lockHint('model')}
              {modelsFetchError ? (
                <span className="text-[11px] text-destructive flex items-center gap-1">
                  <AlertCircle className="w-3 h-3 shrink-0" />
                  {modelsFetchError}
                </span>
              ) : (
                <span className="text-[11px] text-muted-foreground block">
                  支持直接手动输入任意模型 ID，或点击右侧按钮在线获取可用列表
                </span>
              )}

              {/* Combobox Dropdown Popover */}
              {modelsDropdownOpen && modelsList.length > 0 && (
                <div className="absolute z-30 left-0 right-0 top-[60px] bg-popover text-popover-foreground border rounded-xl shadow-xl overflow-hidden animate-in fade-in-50 zoom-in-95">
                  <div className="p-2 border-b bg-muted/40 flex items-center justify-between gap-2">
                    <div className="relative flex-1">
                      <Search className="w-3.5 h-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 text-muted-foreground" />
                      <input
                        type="text"
                        value={modelSearch}
                        onChange={(e) => setModelSearch(e.target.value)}
                        placeholder="快速过滤模型..."
                        className="w-full bg-background rounded-md pl-8 pr-2.5 py-1 text-xs border border-input focus:outline-none focus:ring-1 focus:ring-primary"
                        autoFocus
                      />
                    </div>
                    <span className="text-[10px] text-muted-foreground shrink-0 font-mono">
                      共 {modelsList.length} 个
                    </span>
                  </div>

                  <div className="max-h-52 overflow-y-auto divide-y divide-border/40 p-1">
                    {filteredModels.length > 0 ? (
                      filteredModels.map((item) => {
                        const isFree =
                          (selectedProviderId === 'opencode-zen' && item.id.endsWith('-free')) ||
                          (selectedProviderId === 'openrouter' && item.id.endsWith(':free'));
                        const isCurrent = model === item.id;

                        return (
                          <div
                            key={item.id}
                            onClick={() => {
                              setModel(item.id);
                              setModelsDropdownOpen(false);
                            }}
                            className={cn(
                              'flex items-center justify-between px-3 py-2 rounded-lg cursor-pointer text-xs transition-colors',
                              isCurrent
                                ? 'bg-primary/10 text-primary font-medium'
                                : 'hover:bg-muted/80 text-foreground',
                              isFree && !isCurrent && 'hover:bg-emerald-500/10'
                            )}
                          >
                            <div className="flex items-center gap-2 min-w-0">
                              <span className="font-mono text-xs truncate">{item.id}</span>
                              {isFree && (
                                <Badge
                                  variant="success"
                                  className="gap-1 text-[10px] px-1.5 py-0 font-medium shrink-0"
                                >
                                  <Sparkles className="w-2.5 h-2.5" />
                                  免费
                                </Badge>
                              )}
                            </div>
                            {isCurrent && (
                              <Check className="w-3.5 h-3.5 text-primary shrink-0" />
                            )}
                          </div>
                        );
                      })
                    ) : (
                      <div className="p-4 text-center text-xs text-muted-foreground">
                        未匹配到模型名称
                      </div>
                    )}
                  </div>
                </div>
              )}
            </div>
          </div>

          {/* API Key */}
          <div className="space-y-1.5 pt-1">
            <div className="flex items-center justify-between">
              <label className="text-xs font-semibold text-foreground flex items-center gap-1.5">
                <EyeOff className="w-3.5 h-3.5 text-muted-foreground" />
                API 密钥 (API Key)
                {!currentPreset.key_required && (
                  <span className="text-muted-foreground font-normal">(可选)</span>
                )}
              </label>

              {!isProviderChanged && hasApiKeyOnServer && (
                <Badge
                  variant="secondary"
                  className="gap-1 font-mono text-[10px] bg-muted/60"
                >
                  <CheckCircle2 className="w-3 h-3 text-emerald-500" />
                  已配置: {apiKeyMaskedOnServer}
                </Badge>
              )}

              {!currentPreset.key_required && (
                <Badge
                  variant="outline"
                  className="text-[10px] text-muted-foreground font-normal"
                >
                  免密钥认证
                </Badge>
              )}
            </div>

            <Input
              type="password"
              value={apiKeyInput}
              onChange={(e) => setApiKeyInput(e.target.value)}
              placeholder={
                !isProviderChanged && hasApiKeyOnServer
                  ? `当前已配置 (${apiKeyMaskedOnServer})，留空保持不变`
                  : !currentPreset.key_required
                  ? '本地或无需鉴权服务可留空 (如有网关令牌可填入)'
                  : `请输入 ${currentPreset.name} API Key`
              }
              autoComplete="new-password"
              disabled={isLocked('api_key')}
              className="font-mono text-xs shadow-xs"
            />

            {isLocked('api_key') && lockHint('api_key')}
            <span className="text-[11px] text-muted-foreground block">
              {!isProviderChanged && hasApiKeyOnServer
                ? '出于安全考虑，已保存的密钥以脱敏形式展现且不回填明文。留空提交将继续沿用现有密钥。'
                : currentPreset.key_required
                ? '用于调用 LLM 推理接口的身份鉴权，由后端配置文件安全持久化。'
                : '该服务商默认不强制鉴权。若自建环境设置了 Authorization Bearer，可在此输入。'}
            </span>
          </div>

          {/* Timeout */}
          <div className="space-y-1.5 pt-2">
            <label className="text-xs font-semibold text-foreground">请求超时 (timeout)</label>
            <Input disabled={isLocked('timeout')} value={timeoutStr} onChange={(e) => setTimeoutStr(e.target.value)} placeholder="如 60s、2m；留空使用默认" className="font-mono text-xs shadow-xs" />
            <span className="text-[11px] text-muted-foreground block">范围 0 ~ 10m。</span>
            {isLocked('timeout') && lockHint('timeout')}
          </div>

          {/* extra_body */}
          <div className="space-y-1.5">
            <label className="text-xs font-semibold text-foreground">额外请求参数 (extra_body)</label>
            <textarea
              value={extraBodyStr}
              onChange={(e) => setExtraBodyStr(e.target.value)}
              rows={4}
              disabled={isLocked('extra_body')}
              placeholder='{"enable_thinking": false}'
              className="w-full rounded-md border bg-background p-3 font-mono text-xs outline-none focus:ring-2 focus:ring-ring resize-y"
            />
            <span className="text-[11px] text-muted-foreground block">JSON 对象，会合并进每次模型请求体；留空表示不添加。</span>
            {isLocked('extra_body') && lockHint('extra_body')}
          </div>
        </form>

        {/* 4. Test Result Inline Display */}
        {testResult && (
          <div className="animate-in fade-in-50 duration-200">
            {testResult.ok ? (
              <div className="rounded-xl border border-emerald-500/30 bg-emerald-500/10 dark:bg-emerald-950/25 p-3.5 space-y-2 text-xs">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2 font-semibold text-emerald-600 dark:text-emerald-400">
                    <CheckCircle2 className="w-4 h-4" />
                    <span>连接测试成功！通信与推理渠道正常</span>
                  </div>
                  <Badge variant="success" className="gap-1 font-mono font-normal">
                    <Timer className="w-3 h-3" />
                    延时 {testResult.latency_ms} ms
                  </Badge>
                </div>
                <div className="bg-background/80 dark:bg-background/50 rounded-lg p-2.5 text-muted-foreground border border-emerald-500/20 text-[11px] leading-relaxed">
                  <span className="font-semibold text-foreground mr-1">模型回复:</span>
                  “{testResult.reply}”
                </div>
              </div>
            ) : (
              <div className="rounded-xl border border-destructive/30 bg-destructive/10 dark:bg-destructive/950/25 p-3.5 space-y-1.5 text-xs text-destructive">
                <div className="flex items-center gap-2 font-semibold">
                  <AlertCircle className="w-4 h-4 shrink-0" />
                  <span>连接测试失败</span>
                </div>
                <div className="text-[11px] font-mono break-all pl-6 text-destructive/90 bg-background/50 p-2 rounded border border-destructive/20">
                  {testResult.error}
                </div>
                <div className="text-[11px] text-muted-foreground pl-6">
                  排查提示：请确认所选服务商的 API Key 是否有效、网络端点是否通畅以及模型名称拼写是否正确。
                </div>
              </div>
            )}
          </div>
        )}

        {/* 5. Backend 400 Save Error Alert */}
        {saveError && (
          <div className="rounded-xl border border-destructive/40 bg-destructive/10 dark:bg-destructive/950/25 p-3.5 text-xs text-destructive flex items-start gap-2.5 animate-in fade-in-50">
            <AlertCircle className="w-4 h-4 shrink-0 mt-0.5" />
            <div className="space-y-1 min-w-0">
              <div className="font-semibold">保存配置失败 (400 校验错误)</div>
              <div className="font-mono text-[11px] text-destructive/90 break-all">
                {saveError}
              </div>
            </div>
          </div>
        )}
      </CardContent>

      {/* Card Footer Actions */}
      <CardFooter className="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-3 border-t bg-muted/20 px-6 py-3">
        <span className="text-xs text-muted-foreground">
          配置成功更新后将在下一次音箱对话与工具编排中立即生效
        </span>

        <div className="flex items-center justify-end gap-2.5 shrink-0">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={handleTestConnection}
            disabled={isTesting || isSaving}
            className="gap-1.5 shadow-xs"
          >
            {isTesting ? (
              <RefreshCw className="w-3.5 h-3.5 animate-spin" />
            ) : (
              <Zap className="w-3.5 h-3.5 text-amber-500" />
            )}
            {isTesting ? '正在测试...' : '测试连接'}
          </Button>

          <Button
            type="submit"
            form="llm-form"
            size="sm"
            disabled={isSaving || isTesting}
            className="gap-2 shadow-xs"
          >
            {isSaving ? (
              <RefreshCw className="w-3.5 h-3.5 animate-spin" />
            ) : (
              <Save className="w-3.5 h-3.5" />
            )}
            {isSaving ? '正在保存...' : '保存配置'}
          </Button>
        </div>
      </CardFooter>
    </Card>
  );
}
