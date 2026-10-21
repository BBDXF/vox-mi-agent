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
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { useToast } from '@/components/ui/toast';
import {
  getSettingsSchema,
  getSettings,
  mockSettingsSchema,
  type SettingsSchemaResponse,
  type SettingsResponse,
} from '@/lib/api';
import { LLMProviderCard } from './LLMProviderCard';
import { SourcesTableEditor } from './SourcesTableEditor';
import { GenericSectionForm } from './GenericSectionForm';

// Mapping section keys to display names and icons (曲库 and 别名 are now on the dedicated 曲库 page)
const SECTION_METADATA: Record<
  string,
  { label: string; icon: React.ComponentType<{ className?: string }> }
> = {
  llm: { label: 'LLM', icon: Cpu },
  search: { label: '检索', icon: Search },
  sources: { label: '数据源', icon: Database },
  tools: { label: '工具', icon: Wrench },
  speaker: { label: '音箱', icon: Volume2 },
  server: { label: '服务', icon: Server },
  logging: { label: '日志', icon: ScrollText },
};

const EXCLUDED_SECTIONS = new Set(['media', 'aliases']);

export function SettingsView() {
  const { error: toastError } = useToast();

  const [loading, setLoading] = useState(false);
  const [schema, setSchema] = useState<SettingsSchemaResponse>(mockSettingsSchema);
  const [legacySettings, setLegacySettings] = useState<SettingsResponse | null>(null);
  const [activeSection, setActiveSection] = useState<string>('llm');

  const loadAll = async () => {
    setLoading(true);
    try {
      const [sSchema, sSettings] = await Promise.all([
        getSettingsSchema().catch(() => mockSettingsSchema),
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
  const rawSections = schema.sections || mockSettingsSchema.sections;
  const sectionsList = rawSections.filter((s) => !EXCLUDED_SECTIONS.has(s.name));
  const currentSection =
    sectionsList.find((s) => s.name === activeSection) || sectionsList[0];

  return (
    <div className="h-full overflow-y-auto p-4 md:p-8 space-y-6 max-w-6xl mx-auto">
      {/* Top Header */}
      <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pb-2 border-b">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-foreground flex items-center gap-2">
            <Sliders className="w-5 h-5 text-primary" />
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
                <Icon className={`w-4 h-4 shrink-0 ${isActive ? 'text-primary-foreground' : 'text-primary'}`} />
                <span className="truncate">{meta.label}</span>
              </button>
            );
          })}
        </div>

        {/* Right Content Area */}
        <div className="md:col-span-9 lg:col-span-9 min-w-0 space-y-6">
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
          {activeSection !== 'llm' && activeSection !== 'sources' && (
            <GenericSectionForm
              key={activeSection}
              section={currentSection}
              onSaved={loadAll}
            />
          )}
        </div>
      </div>
    </div>
  );
}