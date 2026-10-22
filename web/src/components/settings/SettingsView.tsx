import React, { useState, useEffect } from 'react';
import {
  Cpu,
  Search,
  Database,
  Wrench,
  Volume2,
  Server,
  ScrollText,
  RefreshCw,
  Sliders,
  Info,
  Bot,
  Plug,
} from 'lucide-react';
import { accent, type Area } from '@/lib/accent';
import { IconChip } from '@/components/ui/icon-chip';
import { AboutCard } from '@/components/about/AboutCard';
import { Button } from '@/components/ui/button';
import { useToast } from '@/components/ui/toast';
import {
  getSettingsSchema,
  getSettings,
  type SettingsSchemaResponse,
  type SettingsResponse,
} from '@/lib/api';
import { LLMProviderCard } from './LLMProviderCard';
import { SourcesTableEditor } from './SourcesTableEditor';
import { SpeakerSettings } from './speaker/SpeakerSettings';
import { AgentSettings } from './AgentSettings';
import { McpSettings } from './McpSettings';
import { GenericSectionForm } from './GenericSectionForm';

// Mapping section keys to display names and icons (曲库 and 别名 are now on the dedicated 曲库 page)
const SECTION_METADATA: Record<
  string,
  { label: string; icon: React.ComponentType<{ className?: string }> }
> = {
  llm: { label: '大模型', icon: Cpu },
  search: { label: '检索', icon: Search },
  sources: { label: '数据源', icon: Database },
  tools: { label: '工具', icon: Wrench },
  speaker: { label: '音箱', icon: Volume2 },
  server: { label: '服务', icon: Server },
  logging: { label: '日志', icon: ScrollText },
};

const EXTRA_SECTIONS = [
  { name: 'agent', label: '智能体', icon: Bot },
  { name: 'mcp', label: 'MCP', icon: Plug },
];

const EXCLUDED_SECTIONS = new Set(['media', 'aliases']);

export function SettingsView({ section, onSectionChange }: { section?: string; onSectionChange?: (s: string) => void } = {}) {
  const { error: toastError } = useToast();

  const [loading, setLoading] = useState(false);
  const [schema, setSchema] = useState<SettingsSchemaResponse>({ sections: [] } as any);
  const [legacySettings, setLegacySettings] = useState<SettingsResponse | null>(null);
  const activeSection = section || 'llm';
  const setActiveSection = (s: string) => onSectionChange?.(s);

  const loadAll = async () => {
    setLoading(true);
    try {
      const [sSchema, sSettings] = await Promise.all([
        getSettingsSchema(),
        getSettings().catch(() => null),
      ]);
      if (sSchema && sSchema.sections && sSchema.sections.length > 0) {
        setSchema(sSchema);
      }
      if (sSettings) {
        setLegacySettings(sSettings);
      }
    } catch (err: any) {
      toastError(err.message || '加载配置元数据失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadAll();
  }, []);

  // Filter out media and aliases from settings tabs (they live on the 曲库 page)
  const rawSections = schema.sections || [];
  const sectionsList = rawSections.filter((s) => !EXCLUDED_SECTIONS.has(s.name));
  const currentSection =
    sectionsList.find((s) => s.name === activeSection) || sectionsList[0];

  return (
    <div className="h-full overflow-y-auto p-4 md:p-8 space-y-6 max-w-6xl mx-auto">
      {/* Top Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pb-2 border-b">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-foreground flex items-center gap-2">
            <IconChip icon={Sliders} color="settings" />
            系统配置与参数规范
          </h1>
          <p className="text-xs text-muted-foreground mt-1">
            动态参数架构驱动：大模型、拼音检索、数据源、音箱驱动与调用链审计策略
          </p>
        </div>

        <Button
          variant="outline"
          size="sm"
          onClick={loadAll}
          disabled={loading}
          className="gap-2 shrink-0 self-start sm:self-auto"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
          刷新配置
        </Button>
      </div>

      {/* Main Settings Layout with Sidebar/Tabs */}
      <div className="grid grid-cols-1 md:grid-cols-12 gap-6 items-start">
        {/* Left Section Navigation Tabs */}
        <div className="md:col-span-3 lg:col-span-3 flex md:flex-col gap-1 overflow-x-auto md:overflow-visible pb-2 md:pb-0 scrollbar-none border-b md:border-b-0 md:border-r pr-0 md:pr-4">
          {sectionsList.map((sec) => {
            const meta = SECTION_METADATA[sec.name] || {
              label: sec.title || sec.name,
              icon: Sliders,
            };
            const Icon = meta.icon;
            const isActive = activeSection === sec.name;

            return (
              <button
                key={sec.name}
                type="button"
                onClick={() => setActiveSection(sec.name)}
                className={`flex items-center gap-2.5 px-3 py-2 rounded-lg text-xs font-medium transition-colors shrink-0 md:w-full text-left select-none ${
                  isActive
                    ? 'bg-primary text-primary-foreground shadow-xs font-semibold'
                    : 'text-muted-foreground hover:bg-muted/80 hover:text-foreground'
                }`}
              >
                <Icon className={`w-4 h-4 shrink-0 ${isActive ? 'text-primary-foreground' : accent(sec.name in SECTION_METADATA ? (sec.name as Area) : 'settings').text}`} />
                <span className="truncate">{meta.label}</span>
              </button>
            );
          })}
          {EXTRA_SECTIONS.map((x) => (
            <button
              key={x.name}
              type="button"
              onClick={() => setActiveSection(x.name)}
              className={`flex items-center gap-2.5 px-3 py-2 rounded-lg text-xs font-medium transition-colors shrink-0 md:w-full text-left select-none ${
                activeSection === x.name
                  ? 'bg-primary text-primary-foreground shadow-xs font-semibold'
                  : 'text-muted-foreground hover:bg-muted/80 hover:text-foreground'
              }`}
            >
              <x.icon className={`w-4 h-4 shrink-0 ${activeSection === x.name ? 'text-primary-foreground' : accent(x.name as Area).text}`} />
              <span className="truncate">{x.label}</span>
            </button>
          ))}
          <button
            type="button"
            onClick={() => setActiveSection('about')}
            className={`flex items-center gap-2.5 px-3 py-2 rounded-lg text-xs font-medium transition-colors shrink-0 md:w-full text-left select-none ${
              activeSection === 'about'
                ? 'bg-primary text-primary-foreground shadow-xs font-semibold'
                : 'text-muted-foreground hover:bg-muted/80 hover:text-foreground'
            }`}
          >
            <Info className={`w-4 h-4 shrink-0 ${activeSection === 'about' ? 'text-primary-foreground' : accent('about').text}`} />
            <span className="truncate">关于</span>
          </button>
        </div>

        {/* Right Content Area */}
        <div className="md:col-span-9 lg:col-span-9 min-w-0 space-y-6">
          {activeSection === 'about' ? <AboutCard /> : activeSection === 'agent' ? <AgentSettings /> : activeSection === 'mcp' ? <McpSettings /> : !currentSection ? (
            <div className="text-sm text-muted-foreground py-12 text-center">
              {loading ? '正在加载配置…' : '配置加载失败，请点击「刷新配置」重试'}
            </div>
          ) : (<>
          {/* Custom LLM Card */}
          {activeSection === 'llm' && (
            legacySettings?.llm ? (
              <LLMProviderCard
                currentSettings={legacySettings.llm}
                onSaved={loadAll}
              />
            ) : (
              <GenericSectionForm section={currentSection} onSaved={loadAll} />
            )
          )}

          {/* Dedicated Sources Table Editor */}
          {activeSection === 'sources' && <SourcesTableEditor />}

          {/* Generic Schema-Driven Section Form for search, tools, speaker, server, logging */}
          {activeSection === 'speaker' && <SpeakerSettings />}

          {activeSection !== 'llm' && activeSection !== 'sources' && activeSection !== 'speaker' && (
            <GenericSectionForm
              key={activeSection}
              section={currentSection}
              onSaved={loadAll}
            />
          )}
          </>)}
        </div>
      </div>
    </div>
  );
}