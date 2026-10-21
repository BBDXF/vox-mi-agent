export interface Conversation {
  id: string;
  title?: string;
  channel?: string;
  created_at: string;
  updated_at: string;
}

export interface ConversationListResponse {
  items: Conversation[];
  next_before?: string;
}

export interface ToolCall {
  id: string;
  name: string;
  arguments: string | Record<string, unknown>;
}

export interface Message {
  id: string;
  role: 'user' | 'assistant' | 'tool';
  content: string;
  tool_calls?: ToolCall[];
  tool_call_id?: string;
  name?: string;
  created_at?: string;
  timings?: {
    first_token_ms?: number;
    total_ms?: number;
  };
  streaming?: boolean;
}

export interface MessageListResponse {
  items: Message[];
}

export interface LLMProvider {
  id: string; // 'opencode-zen' | 'deepseek' | 'dashscope' | 'openai' | 'ollama' | 'custom'
  name: string;
  base_url: string;
  default_model: string;
  key_required: boolean;
  base_url_editable: boolean;
  docs_url?: string;
}

export interface LLMProvidersResponse {
  items: LLMProvider[];
}

export interface LLMModelItem {
  id: string;
}

export interface LLMModelsResponse {
  items: LLMModelItem[];
}

export interface GetLLMModelsRequest {
  provider: string;
  base_url?: string;
  api_key?: string;
}

export interface TestLLMRequest {
  provider: string;
  base_url?: string;
  api_key?: string;
  model: string;
}

export interface TestLLMSuccessResponse {
  ok: true;
  latency_ms: number;
  reply: string;
}

export interface TestLLMFailureResponse {
  ok: false;
  error: string;
}

export type TestLLMResponse = TestLLMSuccessResponse | TestLLMFailureResponse;

export interface LLMSettings {
  provider?: string;
  base_url: string;
  model: string;
  api_key_masked: string;
  has_api_key?: boolean;
  temperature: number;
  timeout_seconds?: number;
}

export interface ToolStatus {
  path: string;
  ok: boolean;
  version?: string;
  error?: string;
}

export interface SourceInfo {
  name: string;
  format: string;
  file: string;
  records?: string;
  keys?: string[];
  extract?: string;
  strip?: string[];
}

export interface MediaConfig {
  folders: string[];
  sync_interval: string;
  extensions: string[];
  accessible_roots?: string[];
  max_files?: number;
  filename_pattern?: string;
  sync_on_start?: boolean;
}

export interface UpdateMediaSettingsRequest {
  folders?: string[];
  extensions?: string[];
  sync_interval?: string;
  filename_pattern?: string;
  max_files?: number;
  sync_on_start?: boolean;
}

export interface UpdateMediaSettingsResponse {
  sync_started: boolean;
}

export interface FsDirEntry {
  name: string;
  path: string;
  symlink?: boolean;
}

export interface FsDirsResponse {
  path: string;
  parent: string | null;
  entries: FsDirEntry[];
  truncated?: boolean;
}

export interface ValidateFsPathResponse {
  ok: boolean;
  reason?: string;
  audio_files_estimate?: number | string;
}

export interface SpeakerConfig {
  driver: string;
}

// ---------------- Media Tracks Types ----------------

export interface MediaTrackItem {
  id: string;
  title: string;
  artist: string;
  artists: string[];
  album?: string;
  alias: string[];
  path: string;
  ext: string;
  score?: number;
}

export interface MediaTracksResponse {
  total: number;
  items: MediaTrackItem[];
}

export interface GetMediaTracksParams {
  q?: string;
  artist?: string;
  sort?: 'title' | 'artist' | 'path';
  limit?: number;
  offset?: number;
}

export interface MediaArtistItem {
  name: string;
  count: number;
}

export interface MediaArtistsResponse {
  items: MediaArtistItem[];
}

// ---------------- Data Sources Types ----------------

export interface DataSourceItem {
  name: string;
  builtin: boolean;
  format: string;
  file: string;
  exists?: boolean;
  records?: string;
  keys?: string[];
  extract?: string;
  strip?: string[];
  count?: number | null;
  size_bytes?: number;
  updated_at?: string | null;
  description?: string;
}

export interface DataSourcesResponse {
  items: DataSourceItem[];
}

export interface SourcePreviewResponse {
  total: number;
  items: any[];
  error?: string;
}

export interface SourceTestRequest {
  tool: 'fuzzy_match' | 'query_json' | 'query_yaml';
  query?: string;
  filter?: string;
  expr?: string;
  limit?: number;
  keys?: string[];
  slurp?: boolean;
  raw?: boolean;
  output_format?: string;
}

export interface SourceTestResponse {
  elapsed_ms?: number;
  [key: string]: any;
}

// ---------------- Schema-Driven Settings Types ----------------

export type FieldType =
  | 'string'
  | 'int'
  | 'float'
  | 'bool'
  | 'duration'
  | 'bytesize'
  | 'string[]'
  | 'enum'
  | 'object[]'
  | 'map'
  | 'object';

export interface SchemaField {
  key: string;
  type: FieldType;
  default: any;
  enum?: string[];
  secret?: boolean;
  restart_required?: boolean;
  description?: string;
}

export interface SchemaSection {
  name: string; // 'llm' | 'media' | 'aliases' | 'search' | 'sources' | 'tools' | 'speaker' | 'server' | 'logging'
  title?: string;
  fields: SchemaField[];
}

export interface SettingsSchemaResponse {
  sections: SchemaSection[];
}

export interface PutSectionSettingsResponse {
  restart_required: boolean;
}

export interface AliasItem {
  title: string;
  artist?: string;
  aliases: string[];
}

export interface AliasesSettingsResponse {
  items: AliasItem[];
}

export interface SourceItem {
  name: string;
  file: string;
  format: 'auto' | 'lines' | 'json' | 'jsonl' | 'yaml';
  records?: string;
  keys?: string[];
  extract?: string;
  strip?: string[];
}

// ---------------- Tracing Types ----------------

export interface TraceStep {
  id: string;
  ts: string;
  step: number;
  kind: 'turn' | 'llm' | 'tool';
  name: string;
  status: 'ok' | 'error' | 'canceled' | string;
  duration_ms: number;
  first_token_ms?: number;
  prompt_tokens?: number;
  completion_tokens?: number;
  finish_reason?: string;
  args?: any;
  result_summary?: string;
  error?: string;
}

export interface TraceTurn {
  turn_id: string;
  ts: string;
  status: 'ok' | 'error' | 'canceled';
  duration_ms: number;
  first_token_ms: number;
  steps: TraceStep[];
}

export interface ConversationTraceResponse {
  turns: TraceTurn[];
}

export interface TraceItem extends TraceStep {
  conversation_id: string;
  turn_id: string;
  channel?: string;
}

export interface GetTracesParams {
  status?: string;
  kind?: string;
  name?: string;
  since?: string;
  until?: string;
  conversation_id?: string;
  limit?: number;
  before_id?: string;
}

export interface TracesListResponse {
  items: TraceItem[];
  next_before_id?: string;
}

export interface TracePayloadStep {
  ts: string;
  step: number;
  kind: 'llm' | 'tool';
  request?: any;
  response?: any;
  tool?: string;
  args?: any;
  result?: any;
}

export interface TracePayloadResponse {
  available: boolean;
  items: TracePayloadStep[];
}

export interface SettingsResponse {
  llm: LLMSettings;
  tools: Record<string, ToolStatus>;
  sources: SourceInfo[];
  media: MediaConfig;
  speaker: SpeakerConfig;
}

export interface UpdateLLMRequest {
  provider: string;
  base_url?: string;
  model: string;
  api_key?: string;
  temperature?: number;
}

export interface MediaStatus {
  running: boolean;
  last_sync_at?: string;
  tracks?: number;
  duration_ms?: number;
  result?: string;
  error?: string;
}

export interface HealthResponse {
  status: string;
  tools?: Record<string, boolean | ToolStatus>;
}

// ---------------- Mock State Storage ----------------
let mockConversations: Conversation[] = [
  {
    id: 'conv_music_01',
    title: '播放周杰伦《晴天》',
    channel: 'speaker',
    created_at: new Date(Date.now() - 3600000 * 2).toISOString(),
    updated_at: new Date(Date.now() - 3600000 * 2 + 15000).toISOString(),
  },
  {
    id: 'conv_weather_02',
    title: '明日天气查询与穿衣提醒',
    channel: 'speaker',
    created_at: new Date(Date.now() - 3600000 * 5).toISOString(),
    updated_at: new Date(Date.now() - 3600000 * 5 + 8000).toISOString(),
  },
  {
    id: 'conv_debug_03',
    title: 'NAS 本地曲库索引自检',
    channel: 'web',
    created_at: new Date(Date.now() - 86400000).toISOString(),
    updated_at: new Date(Date.now() - 86400000 + 45000).toISOString(),
  },
];

let mockMessages: Record<string, Message[]> = {
  conv_music_01: [
    {
      id: 'm1',
      role: 'user',
      content: '小爱同学，播放周杰伦的晴天',
      created_at: new Date(Date.now() - 3600000 * 2).toISOString(),
    },
    {
      id: 'm2',
      role: 'assistant',
      content: '正在本地曲库模糊匹配《晴天》...',
      tool_calls: [
        {
          id: 'call_fzq_101',
          name: 'fzq',
          arguments: {
            queries: '周杰伦 晴天',
            limit: 3,
            candidates: 500,
          },
        },
      ],
      created_at: new Date(Date.now() - 3600000 * 2 + 1000).toISOString(),
    },
    {
      id: 'm3',
      role: 'tool',
      name: 'fzq',
      tool_call_id: 'call_fzq_101',
      content: JSON.stringify([
        {
          artist: '周杰伦',
          title: '晴天',
          file: '/vol1/1000/Music/周杰伦 - 晴天.mp3',
          score: 98.5,
          duration: '04:29',
        },
      ], null, 2),
      created_at: new Date(Date.now() - 3600000 * 2 + 1500).toISOString(),
    },
    {
      id: 'm4',
      role: 'assistant',
      content: '已命中本地无损音源，正在推送播放流至音箱驱动...',
      tool_calls: [
        {
          id: 'call_speaker_102',
          name: 'speaker_play',
          arguments: {
            file: '/vol1/1000/Music/周杰伦 - 晴天.mp3',
            title: '晴天',
            artist: '周杰伦',
          },
        },
      ],
      created_at: new Date(Date.now() - 3600000 * 2 + 2000).toISOString(),
    },
    {
      id: 'm5',
      role: 'tool',
      name: 'speaker_play',
      tool_call_id: 'call_speaker_102',
      content: JSON.stringify({
        status: 'playing',
        device: '小爱音箱 Pro',
        driver: 'xiaomi_miot',
        volume: 45,
      }, null, 2),
      created_at: new Date(Date.now() - 3600000 * 2 + 2500).toISOString(),
    },
    {
      id: 'm6',
      role: 'assistant',
      content: '🎵 为您播放周杰伦的《晴天》，歌曲来自家庭存储本地曲库。',
      created_at: new Date(Date.now() - 3600000 * 2 + 3000).toISOString(),
      timings: {
        first_token_ms: 185,
        total_ms: 1240,
      },
    },
  ],
  conv_weather_02: [
    {
      id: 'w1',
      role: 'user',
      content: '明天出门需要带伞吗？',
      created_at: new Date(Date.now() - 3600000 * 5).toISOString(),
    },
    {
      id: 'w2',
      role: 'assistant',
      content: '明天当地天气多云转晴，气温 18℃ ~ 26℃，降水概率仅 10%，无需携带雨伞，适宜户外出行。',
      created_at: new Date(Date.now() - 3600000 * 5 + 1200).toISOString(),
      timings: {
        first_token_ms: 145,
        total_ms: 820,
      },
    },
  ],
  conv_debug_03: [
    {
      id: 'd1',
      role: 'user',
      content: '检查系统环境与工具链版本',
      created_at: new Date(Date.now() - 86400000).toISOString(),
    },
    {
      id: 'd2',
      role: 'assistant',
      content: '系统核心组件均正常就绪：\n- `fzq` 模糊拼音搜索引擎：v0.2.1 (正常)\n- `jq` JSON 处理器：v1.7.1 (正常)\n- `yq` YAML 解析器：v4.44.1 (正常)\n曲库缓存健康，已监听音箱端事件通道。',
      created_at: new Date(Date.now() - 86400000 + 900).toISOString(),
      timings: {
        first_token_ms: 160,
        total_ms: 950,
      },
    },
  ],
};

export const mockProviders: LLMProvider[] = [
  {
    id: 'opencode-zen',
    name: 'OpenCode Zen',
    base_url: 'https://opencode.ai/zen/v1',
    default_model: 'kimi-k1.5-free',
    key_required: true,
    base_url_editable: false,
    docs_url: 'https://opencode.ai/docs/zen',
  },
  {
    id: 'deepseek',
    name: 'DeepSeek',
    base_url: 'https://api.deepseek.com/v1',
    default_model: 'deepseek-chat',
    key_required: true,
    base_url_editable: false,
    docs_url: 'https://platform.deepseek.com',
  },
  {
    id: 'dashscope',
    name: '阿里百炼 (DashScope)',
    base_url: 'https://dashscope.aliyuncs.com/compatible-mode/v1',
    default_model: 'qwen-plus',
    key_required: true,
    base_url_editable: false,
    docs_url: 'https://help.aliyun.com/dashscope',
  },
  {
    id: 'openai',
    name: 'OpenAI',
    base_url: 'https://api.openai.com/v1',
    default_model: 'gpt-4o-mini',
    key_required: true,
    base_url_editable: false,
    docs_url: 'https://platform.openai.com',
  },
  {
    id: 'siliconflow',
    name: '硅基流动',
    base_url: 'https://api.siliconflow.cn/v1',
    default_model: 'Qwen/Qwen3-8B',
    key_required: true,
    base_url_editable: false,
    docs_url: 'https://docs.siliconflow.cn',
  },
  {
    id: 'openrouter',
    name: 'OpenRouter',
    base_url: 'https://openrouter.ai/api/v1',
    default_model: '',
    key_required: true,
    base_url_editable: false,
    docs_url: 'https://openrouter.ai/docs',
  },
  {
    id: 'ollama',
    name: 'Ollama (本地私有化)',
    base_url: 'http://localhost:11434/v1',
    default_model: 'qwen2.5:7b',
    key_required: false,
    base_url_editable: true,
    docs_url: 'https://ollama.com',
  },
  {
    id: 'custom',
    name: '自定义端点 (Custom)',
    base_url: 'http://127.0.0.1:8000/v1',
    default_model: 'custom-model',
    key_required: false,
    base_url_editable: true,
    docs_url: '',
  },
];

let mockSettings: SettingsResponse = {
  llm: {
    provider: 'opencode-zen',
    base_url: 'https://opencode.ai/zen/v1',
    model: 'kimi-k1.5-free',
    api_key_masked: 'zen-****************************39df',
    has_api_key: true,
    temperature: 0.3,
    timeout_seconds: 60,
  },
  tools: {
    fzq: {
      path: '/usr/local/bin/fzq',
      ok: true,
      version: '0.2.1',
    },
    jq: {
      path: '/usr/bin/jq',
      ok: true,
      version: '1.7.1',
    },
    yq: {
      path: '/usr/bin/yq',
      ok: true,
      version: '4.44.1',
    },
  },
  sources: [
    {
      name: 'media_library',
      format: 'jsonl',
      file: '/data/media/songs.jsonl',
      records: 'auto',
      keys: ['title', 'artist', 'alias'],
    },
    {
      name: 'devices',
      format: 'json',
      file: '/data/sources/devices.json',
      records: 'items',
      keys: ['name', 'room', 'type'],
    },
  ],
  media: {
    folders: ['/vol1/1000/Music', '/vol2/Audio/Lossless'],
    sync_interval: '6h',
    extensions: ['mp3', 'flac', 'm4a', 'wav', 'ogg', 'ape'],
    accessible_roots: ['/vol1/1000', '/vol2/Audio'],
    max_files: 100000,
    filename_pattern: '(?:(?P<artist>.+?) - )?(?P<title>.+)\\.\\w+$',
    sync_on_start: true,
  },
  speaker: {
    driver: 'xiaomi_miot',
  },
};

// ---------------- Mock Schema & Section Values ----------------

export const mockSettingsSchema: SettingsSchemaResponse = {
  sections: [
    {
      name: 'llm',
      title: '大模型推理 (LLM)',
      fields: [
        { key: 'provider', type: 'string', default: 'opencode-zen', description: '模型服务商标识' },
        { key: 'base_url', type: 'string', default: 'https://opencode.ai/zen/v1', description: 'API 接口基地址' },
        { key: 'model', type: 'string', default: 'kimi-k1.5-free', description: '调用的模型版本名称' },
        { key: 'api_key', type: 'string', secret: true, default: '', description: '服务商鉴权密钥 Token' },
        { key: 'temperature', type: 'float', default: 0.3, description: '生成采样温度 (0.0 ~ 1.0)' },
        { key: 'timeout_seconds', type: 'duration', default: '60s', restart_required: true, description: '单次推理请求超时限制' },
      ],
    },
    {
      name: 'media',
      title: '本地曲库 (Media)',
      fields: [
        { key: 'folders', type: 'string[]', default: ['/vol1/1000/Music', '/vol2/Audio/Lossless'], description: '音频扫描根目录列表' },
        { key: 'sync_interval', type: 'duration', default: '6h', description: '全量扫描与索引构建周期 (如 6h / 30m / 0)' },
        { key: 'extensions', type: 'string[]', default: ['mp3', 'flac', 'm4a', 'wav', 'ogg', 'ape'], description: '音频解析文件后缀白名单' },
        { key: 'filename_pattern', type: 'string', default: '(?:(?P<artist>.+?) - )?(?P<title>.+)\\.\\w+$', description: '从文件名提取元数据的正则表达式 (命名分组: title, artist)' },
        { key: 'max_files', type: 'int', default: 100000, description: '最大允许索引歌曲文件数量上限' },
        { key: 'sync_on_start', type: 'bool', default: true, restart_required: true, description: '服务启动时自动触发一次本地曲库索引同步' },
      ],
    },
    {
      name: 'aliases',
      title: '音乐别名 (Aliases)',
      fields: [
        { key: 'items', type: 'object[]', default: [], description: '歌曲简称、拼音及多语种别名映射字典' },
      ],
    },
    {
      name: 'search',
      title: '拼音检索 (Search)',
      fields: [
        { key: 'pinyin.enabled', type: 'bool', default: true, description: '启用多音字全拼及首字母双向索引' },
        { key: 'fuzzy.min_score', type: 'float', default: 60.0, description: '模糊匹配最低相似度分值阈值 (0-100)' },
        { key: 'candidates.limit', type: 'int', default: 500, description: '拼音搜索引擎单次候选结果召回上限' },
        { key: 'cache.ttl', type: 'duration', default: '24h', description: '搜索命中结果热点缓存时长 (如 24h / 30s)' },
        { key: 'cache.max_memory', type: 'bytesize', default: '128MiB', restart_required: true, description: '模糊检索索引最大常驻内存限制' },
      ],
    },
    {
      name: 'sources',
      title: '数据源 (Sources)',
      fields: [
        { key: 'items', type: 'object[]', default: [], description: '只读结构化数据源注册清单' },
      ],
    },
    {
      name: 'tools',
      title: '系统工具 (Tools)',
      fields: [
        { key: 'shell.enabled', type: 'bool', default: false, restart_required: true, description: '允许 Agent 在受限白名单内调用宿主 Shell' },
        { key: 'fzq.path', type: 'string', default: '/usr/local/bin/fzq', restart_required: true, description: 'fzq 高速拼音搜索引擎可执行文件路径' },
        { key: 'jq.path', type: 'string', default: '/usr/bin/jq', restart_required: true, description: 'jq JSON 过滤器可执行文件路径' },
        { key: 'timeout', type: 'duration', default: '15s', description: '外部命令行工具执行最大超时时长' },
      ],
    },
    {
      name: 'speaker',
      title: '音箱驱动 (Speaker)',
      fields: [
        { key: 'driver', type: 'enum', enum: ['xiaomi_miot', 'mock', 'dlna'], default: 'xiaomi_miot', restart_required: true, description: '智能音箱硬件底层驱动协议通道' },
        { key: 'device_did', type: 'string', default: 'miot.speaker.pro.01', description: '小米 MIoT 智能设备实体 DID 标识' },
        { key: 'miot.token', type: 'string', secret: true, default: '', description: '米家局域网通信 Token 鉴权凭据' },
        { key: 'default_volume', type: 'int', default: 45, description: '语音播报及默认音乐点播音量 (0-100)' },
        { key: 'tts.rate', type: 'int', default: 100, description: '小爱 TTS 朗读语速百分比 (50-200)' },
      ],
    },
    {
      name: 'server',
      title: '服务网络 (Server)',
      fields: [
        { key: 'http.port', type: 'int', default: 8080, restart_required: true, description: 'Web 控制台与 API 监听端口' },
        { key: 'http.host', type: 'string', default: '0.0.0.0', restart_required: true, description: '系统网络监听绑定的 IP 地址' },
        { key: 'auth.require_admin', type: 'bool', default: false, restart_required: true, description: '访问管理设置与调试功能需管理员身份验证' },
        { key: 'admin.password', type: 'string', secret: true, default: '', description: '控制台管理员访问密码' },
        { key: 'max_request_size', type: 'bytesize', default: '10MiB', restart_required: true, description: 'HTTP 请求体最大允许尺寸限制' },
      ],
    },
    {
      name: 'logging',
      title: '日志调试 (Logging)',
      fields: [
        { key: 'level', type: 'enum', enum: ['debug', 'info', 'warn', 'error'], default: 'info', description: '标准输出控制台日志记录等级' },
        { key: 'debug_mode', type: 'bool', default: true, description: '开启全量输入输出原始报文 Payload 调试存储' },
        { key: 'retention', type: 'duration', default: '72h', description: '调用链追踪记录在本地磁盘的保留期限' },
        { key: 'max_log_size', type: 'bytesize', default: '64MiB', restart_required: true, description: '单卷追踪日志文件轮转尺寸上限' },
      ],
    },
  ],
};

let mockAliases: AliasItem[] = [
  { title: '晴天', artist: '周杰伦', aliases: ['QingTian', 'Sunny Day', '周董晴天'] },
  { title: '七里香', artist: '周杰伦', aliases: ['QiLiXiang', 'Common Jasmine Orange'] },
  { title: '十年', artist: '陈奕迅', aliases: ['ShiNian', 'Ten Years'] },
  { title: '富士山下', artist: '陈奕迅', aliases: ['FuShiShanXia', '爱情转移粤语版'] },
  { title: '青花瓷', artist: '周杰伦', aliases: ['QingHuaCi', 'Blue and White Porcelain'] },
];

export let mockTracks: MediaTrackItem[] = [
  {
    id: 'trk_01',
    title: '晴天',
    artist: '周杰伦',
    artists: ['周杰伦'],
    album: '叶惠美',
    alias: ['QingTian', 'Sunny Day', '周董晴天'],
    path: '/vol1/1000/Music/周杰伦/2003 - 叶惠美/03 晴天.flac',
    ext: 'flac',
  },
  {
    id: 'trk_02',
    title: '七里香',
    artist: '周杰伦',
    artists: ['周杰伦'],
    album: '七里香',
    alias: ['QiLiXiang', 'Common Jasmine Orange'],
    path: '/vol1/1000/Music/周杰伦/2004 - 七里香/01 七里香.flac',
    ext: 'flac',
  },
  {
    id: 'trk_03',
    title: '青花瓷',
    artist: '周杰伦',
    artists: ['周杰伦'],
    album: '我很忙',
    alias: ['QingHuaCi', 'Blue and White Porcelain'],
    path: '/vol1/1000/Music/周杰伦/2007 - 我很忙/02 青花瓷.flac',
    ext: 'flac',
  },
  {
    id: 'trk_04',
    title: '稻香',
    artist: '周杰伦',
    artists: ['周杰伦'],
    album: '魔杰座',
    alias: ['DaoXiang', 'Rice Field'],
    path: '/vol1/1000/Music/周杰伦/2008 - 魔杰座/03 稻香.mp3',
    ext: 'mp3',
  },
  {
    id: 'trk_05',
    title: '夜曲',
    artist: '周杰伦',
    artists: ['周杰伦'],
    album: '十一月的萧邦',
    alias: ['YeQu', 'Nocturne'],
    path: '/vol1/1000/Music/周杰伦/2005 - 十一月的萧邦/01 夜曲.flac',
    ext: 'flac',
  },
  {
    id: 'trk_06',
    title: '简单爱',
    artist: '周杰伦',
    artists: ['周杰伦'],
    album: '范特西',
    alias: ['JianDanAi', 'Simple Love'],
    path: '/vol1/1000/Music/周杰伦/2001 - 范特西/02 简单爱.flac',
    ext: 'flac',
  },
  {
    id: 'trk_07',
    title: '枫',
    artist: '周杰伦',
    artists: ['周杰伦'],
    album: '十一月的萧邦',
    alias: ['Feng', 'Maple'],
    path: '/vol1/1000/Music/周杰伦/2005 - 十一月的萧邦/04 枫.mp3',
    ext: 'mp3',
  },
  {
    id: 'trk_08',
    title: '十年',
    artist: '陈奕迅',
    artists: ['陈奕迅'],
    album: '黑·白·灰',
    alias: ['ShiNian', 'Ten Years'],
    path: '/vol1/1000/Music/陈奕迅/2003 - 黑白灰/02 十年.flac',
    ext: 'flac',
  },
  {
    id: 'trk_09',
    title: '富士山下',
    artist: '陈奕迅',
    artists: ['陈奕迅'],
    album: 'What\'s Going On...?',
    alias: ['FuShiShanXia', '爱情转移粤语版'],
    path: '/vol1/1000/Music/陈奕迅/2006 - Whats Going On/05 富士山下.flac',
    ext: 'flac',
  },
  {
    id: 'trk_10',
    title: '浮夸',
    artist: '陈奕迅',
    artists: ['陈奕迅'],
    album: 'U87',
    alias: ['FuKua', 'Exaggerated'],
    path: '/vol1/1000/Music/陈奕迅/2005 - U87/03 浮夸.flac',
    ext: 'flac',
  },
  {
    id: 'trk_11',
    title: '孤勇者',
    artist: '陈奕迅',
    artists: ['陈奕迅'],
    album: '英雄联盟动画双城之战',
    alias: ['GuYongZhe', 'Lonely Warrior'],
    path: '/vol1/1000/Music/陈奕迅/2021 - 孤勇者/孤勇者.flac',
    ext: 'flac',
  },
  {
    id: 'trk_12',
    title: '好久不见',
    artist: '陈奕迅',
    artists: ['陈奕迅'],
    album: '认了吧',
    alias: ['HaoJiuBuJian', 'Long Time No See'],
    path: '/vol1/1000/Music/陈奕迅/2007 - 认了吧/04 好久不见.mp3',
    ext: 'mp3',
  },
  {
    id: 'trk_13',
    title: '光年之外',
    artist: '邓紫棋',
    artists: ['邓紫棋'],
    album: '太空旅客原声',
    alias: ['GuangNianZhiWai', 'Light Years Away', 'GEM光年之外'],
    path: '/vol2/Audio/Lossless/邓紫棋/2016 - 光年之外.flac',
    ext: 'flac',
  },
  {
    id: 'trk_14',
    title: '泡沫',
    artist: '邓紫棋',
    artists: ['邓紫棋'],
    album: 'Xposed',
    alias: ['PaoMo', 'Bubble'],
    path: '/vol2/Audio/Lossless/邓紫棋/2012 - Xposed/02 泡沫.flac',
    ext: 'flac',
  },
  {
    id: 'trk_15',
    title: '句号',
    artist: '邓紫棋',
    artists: ['邓紫棋'],
    album: '摩天动物园',
    alias: ['JuHao', 'Full Stop'],
    path: '/vol2/Audio/Lossless/邓紫棋/2019 - 摩天动物园/04 句号.flac',
    ext: 'flac',
  },
  {
    id: 'trk_16',
    title: '来自天堂的魔鬼',
    artist: '邓紫棋',
    artists: ['邓紫棋'],
    album: '新的心跳',
    alias: ['Demons', 'Away'],
    path: '/vol2/Audio/Lossless/邓紫棋/2015 - 新的心跳/03 来自天堂的魔鬼.mp3',
    ext: 'mp3',
  },
  {
    id: 'trk_17',
    title: '红豆',
    artist: '王菲',
    artists: ['王菲'],
    album: '唱游',
    alias: ['HongDou', 'Red Bean', '偿还国语版'],
    path: '/vol1/1000/Music/王菲/1998 - 唱游/02 红豆.flac',
    ext: 'flac',
  },
  {
    id: 'trk_18',
    title: '匆匆那年',
    artist: '王菲',
    artists: ['王菲'],
    album: '匆匆那年电影原声',
    alias: ['CongCongNaNian', 'Fleet of Time'],
    path: '/vol1/1000/Music/王菲/2014 - 匆匆那年/匆匆那年.flac',
    ext: 'flac',
  },
  {
    id: 'trk_19',
    title: '传奇',
    artist: '王菲',
    artists: ['王菲'],
    album: '传奇',
    alias: ['ChuanQi', 'Legend'],
    path: '/vol1/1000/Music/王菲/2010 - 传奇/传奇.wav',
    ext: 'wav',
  },
  {
    id: 'trk_20',
    title: '遇见',
    artist: '孙燕姿',
    artists: ['孙燕姿'],
    album: 'The Moment',
    alias: ['YuJian', 'Encounter', '向左走向右走主题曲'],
    path: '/vol1/1000/Music/孙燕姿/2003 - The Moment/03 遇见.flac',
    ext: 'flac',
  },
  {
    id: 'trk_21',
    title: '我怀念的',
    artist: '孙燕姿',
    artists: ['孙燕姿'],
    album: '逆光',
    alias: ['WoHuaiNianDe', 'What I Miss'],
    path: '/vol1/1000/Music/孙燕姿/2007 - 逆光/02 我怀念的.flac',
    ext: 'flac',
  },
  {
    id: 'trk_22',
    title: '绿光',
    artist: '孙燕姿',
    artists: ['孙燕姿'],
    album: '风筝',
    alias: ['LvGuang', 'Green Light'],
    path: '/vol1/1000/Music/孙燕姿/2001 - 风筝/01 绿光.mp3',
    ext: 'mp3',
  },
  {
    id: 'trk_23',
    title: '江南',
    artist: '林俊杰',
    artists: ['林俊杰'],
    album: '第二天堂',
    alias: ['JiangNan', 'River South'],
    path: '/vol1/1000/Music/林俊杰/2004 - 第二天堂/02 江南.flac',
    ext: 'flac',
  },
  {
    id: 'trk_24',
    title: '修炼爱情',
    artist: '林俊杰',
    artists: ['林俊杰'],
    album: '因你而在',
    alias: ['XiuLianAiQing', 'Practice Love'],
    path: '/vol1/1000/Music/林俊杰/2013 - 因你而在/03 修炼爱情.flac',
    ext: 'flac',
  },
  {
    id: 'trk_25',
    title: '突然好想你',
    artist: '五月天',
    artists: ['五月天'],
    album: '后青春期的诗',
    alias: ['Suddenly Missing You'],
    path: '/vol1/1000/Music/五月天/2008 - 后青春期的诗/08 突然好想你.flac',
    ext: 'flac',
  },
  {
    id: 'trk_26',
    title: '倔强',
    artist: '五月天',
    artists: ['五月天'],
    album: '神的孩子都在跳舞',
    alias: ['JueQiang', 'Stubborn'],
    path: '/vol1/1000/Music/五月天/2004 - 神的孩子都在跳舞/05 倔强.mp3',
    ext: 'mp3',
  },
  {
    id: 'trk_27',
    title: '珊瑚海',
    artist: '周杰伦 / 梁心颐',
    artists: ['周杰伦', '梁心颐'],
    album: '十一月的萧邦',
    alias: ['ShanHuHai', 'Coral Sea'],
    path: '/vol1/1000/Music/周杰伦/2005 - 十一月的萧邦/05 珊瑚海.flac',
    ext: 'flac',
  },
  {
    id: 'trk_28',
    title: '阴天',
    artist: '莫文蔚',
    artists: ['莫文蔚'],
    album: '你可以',
    alias: ['YinTian', 'Overcast'],
    path: '/vol1/1000/Music/莫文蔚/1999 - 你可以/03 阴天.flac',
    ext: 'flac',
  },
];

let mockDataSources: DataSourceItem[] = [
  {
    name: 'media_library',
    builtin: true,
    format: 'jsonl',
    file: '/data/media/songs.jsonl',
    exists: true,
    records: 'auto',
    keys: ['title', 'artist', 'alias'],
    extract: '',
    strip: [],
    count: 14280,
    size_bytes: 48920100,
    updated_at: new Date(Date.now() - 3600000 * 3).toISOString(),
    description: 'NAS 本地曲库元数据全量只读索引快照 (songs.jsonl)',
  },
  {
    name: 'devices',
    builtin: false,
    format: 'json',
    file: '/data/sources/devices.json',
    exists: true,
    records: 'items',
    keys: ['name', 'room', 'type'],
    extract: '',
    strip: [],
    count: 12,
    size_bytes: 4096,
    updated_at: new Date(Date.now() - 86400000).toISOString(),
    description: '智能家居设备房间分布与 MIoT 设备实体清单',
  },
  {
    name: 'radio_stations',
    builtin: false,
    format: 'yaml',
    file: '/data/sources/radio.yaml',
    exists: true,
    records: 'stations',
    keys: ['title', 'category', 'freq'],
    extract: '',
    strip: [],
    count: 24,
    size_bytes: 8192,
    updated_at: new Date(Date.now() - 86400000 * 2).toISOString(),
    description: '网络广播流地址与在线 FM 电台列表',
  },
];

// mockSources represents user-configurable sources only (media_library is NEVER included)
let mockSources: SourceItem[] = [
  {
    name: 'devices',
    file: '/data/sources/devices.json',
    format: 'json',
    records: 'items',
    keys: ['name', 'room', 'type'],
    extract: '',
    strip: [],
  },
  {
    name: 'radio_stations',
    file: '/data/sources/radio.yaml',
    format: 'yaml',
    records: 'stations',
    keys: ['title', 'category', 'freq'],
    extract: '',
    strip: [],
  },
];

let mockSectionValues: Record<string, Record<string, any>> = {
  llm: {
    provider: 'opencode-zen',
    base_url: 'https://opencode.ai/zen/v1',
    model: 'kimi-k1.5-free',
    api_key: '****39df',
    temperature: 0.3,
    timeout_seconds: '60s',
  },
  media: {
    folders: ['/vol1/1000/Music', '/vol2/Audio/Lossless'],
    sync_interval: '6h',
    extensions: ['mp3', 'flac', 'm4a', 'wav', 'ogg', 'ape'],
    max_files: 100000,
    filename_pattern: '(?:(?P<artist>.+?) - )?(?P<title>.+)\\.\\w+$',
    sync_on_start: true,
  },
  aliases: {
    items: mockAliases,
  },
  search: {
    'pinyin.enabled': true,
    'fuzzy.min_score': 60.0,
    'candidates.limit': 500,
    'cache.ttl': '24h',
    'cache.max_memory': '128MiB',
  },
  sources: {
    items: mockSources,
  },
  tools: {
    'shell.enabled': false,
    'fzq.path': '/usr/local/bin/fzq',
    'jq.path': '/usr/bin/jq',
    timeout: '15s',
  },
  speaker: {
    driver: 'xiaomi_miot',
    device_did: 'miot.speaker.pro.01',
    'miot.token': '****9a8f',
    default_volume: 45,
    'tts.rate': 100,
  },
  server: {
    'http.port': 8080,
    'http.host': '0.0.0.0',
    'auth.require_admin': false,
    'admin.password': '****pass',
    max_request_size: '10MiB',
  },
  logging: {
    level: 'info',
    debug_mode: true,
    retention: '72h',
    max_log_size: '64MiB',
  },
};

// ---------------- Mock Tracing Data ----------------

let mockConversationTraces: Record<string, ConversationTraceResponse> = {
  conv_music_01: {
    turns: [
      {
        turn_id: 'turn_music_01',
        ts: new Date(Date.now() - 3600000 * 2).toISOString(),
        status: 'ok',
        duration_ms: 1240,
        first_token_ms: 185,
        steps: [
          {
            id: 'step_1_llm_01',
            ts: new Date(Date.now() - 3600000 * 2).toISOString(),
            step: 1,
            kind: 'llm',
            name: 'kimi-k1.5-free',
            status: 'ok',
            duration_ms: 320,
            first_token_ms: 185,
            prompt_tokens: 350,
            completion_tokens: 42,
            finish_reason: 'tool_calls',
            args: { query: '小爱同学，播放周杰伦的晴天' },
          },
          {
            id: 'step_2_tool_01',
            ts: new Date(Date.now() - 3600000 * 2 + 1000).toISOString(),
            step: 2,
            kind: 'tool',
            name: 'fzq',
            status: 'ok',
            duration_ms: 22,
            args: { queries: '周杰伦 晴天', limit: 3, candidates: 500 },
            result_summary: '找到 1 首匹配曲目: 《晴天》 (周杰伦, 得分 98.5)',
          },
          {
            id: 'step_3_llm_02',
            ts: new Date(Date.now() - 3600000 * 2 + 1500).toISOString(),
            step: 3,
            kind: 'llm',
            name: 'kimi-k1.5-free',
            status: 'ok',
            duration_ms: 280,
            first_token_ms: 110,
            prompt_tokens: 412,
            completion_tokens: 38,
            finish_reason: 'tool_calls',
          },
          {
            id: 'step_4_tool_02',
            ts: new Date(Date.now() - 3600000 * 2 + 2000).toISOString(),
            step: 4,
            kind: 'tool',
            name: 'speaker_play',
            status: 'ok',
            duration_ms: 140,
            args: {
              file: '/vol1/1000/Music/周杰伦 - 晴天.mp3',
              title: '晴天',
              artist: '周杰伦',
            },
            result_summary: '推送播放流成功 (device: 小爱音箱 Pro, volume: 45)',
          },
          {
            id: 'step_5_llm_03',
            ts: new Date(Date.now() - 3600000 * 2 + 2500).toISOString(),
            step: 5,
            kind: 'llm',
            name: 'kimi-k1.5-free',
            status: 'ok',
            duration_ms: 478,
            first_token_ms: 95,
            prompt_tokens: 520,
            completion_tokens: 65,
            finish_reason: 'stop',
          },
        ],
      },
    ],
  },
  conv_weather_02: {
    turns: [
      {
        turn_id: 'turn_weather_02',
        ts: new Date(Date.now() - 3600000 * 5).toISOString(),
        status: 'ok',
        duration_ms: 820,
        first_token_ms: 145,
        steps: [
          {
            id: 'step_w1_llm',
            ts: new Date(Date.now() - 3600000 * 5).toISOString(),
            step: 1,
            kind: 'llm',
            name: 'kimi-k1.5-free',
            status: 'ok',
            duration_ms: 820,
            first_token_ms: 145,
            prompt_tokens: 280,
            completion_tokens: 72,
            finish_reason: 'stop',
            args: { query: '明天出门需要带伞吗？' },
          },
        ],
      },
    ],
  },
  conv_debug_03: {
    turns: [
      {
        turn_id: 'turn_debug_03',
        ts: new Date(Date.now() - 86400000).toISOString(),
        status: 'ok',
        duration_ms: 950,
        first_token_ms: 160,
        steps: [
          {
            id: 'step_d1_llm',
            ts: new Date(Date.now() - 86400000).toISOString(),
            step: 1,
            kind: 'llm',
            name: 'kimi-k1.5-free',
            status: 'ok',
            duration_ms: 310,
            first_token_ms: 160,
            prompt_tokens: 190,
            completion_tokens: 30,
            finish_reason: 'tool_calls',
          },
          {
            id: 'step_d2_tool',
            ts: new Date(Date.now() - 86400000 + 400).toISOString(),
            step: 2,
            kind: 'tool',
            name: 'system_check',
            status: 'ok',
            duration_ms: 45,
            args: { check: 'tools' },
            result_summary: 'fzq: v0.2.1, jq: v1.7.1, yq: v4.44.1 均正常',
          },
          {
            id: 'step_d3_llm',
            ts: new Date(Date.now() - 86400000 + 500).toISOString(),
            step: 3,
            kind: 'llm',
            name: 'kimi-k1.5-free',
            status: 'ok',
            duration_ms: 595,
            first_token_ms: 90,
            prompt_tokens: 380,
            completion_tokens: 88,
            finish_reason: 'stop',
          },
        ],
      },
    ],
  },
};

let mockTracePayloads: Record<string, TracePayloadResponse> = {
  turn_music_01: {
    available: true,
    items: [
      {
        ts: new Date(Date.now() - 3600000 * 2).toISOString(),
        step: 1,
        kind: 'llm',
        request: {
          model: 'kimi-k1.5-free',
          messages: [
            { role: 'system', content: 'You are Vox MI companion agent on NAS.' },
            { role: 'user', content: '小爱同学，播放周杰伦的晴天' },
          ],
          tools: [{ type: 'function', function: { name: 'fzq', description: 'fuzzy music search' } }],
          temperature: 0.3,
        },
        response: {
          id: 'chatcmpl-789a',
          choices: [
            {
              message: {
                role: 'assistant',
                tool_calls: [
                  {
                    id: 'call_fzq_101',
                    type: 'function',
                    function: { name: 'fzq', arguments: '{"queries":"周杰伦 晴天","limit":3}' },
                  },
                ],
              },
              finish_reason: 'tool_calls',
            },
          ],
          usage: { prompt_tokens: 350, completion_tokens: 42, total_tokens: 392 },
        },
      },
      {
        ts: new Date(Date.now() - 3600000 * 2 + 1000).toISOString(),
        step: 2,
        kind: 'tool',
        tool: 'fzq',
        args: { queries: '周杰伦 晴天', limit: 3, candidates: 500 },
        result: [
          {
            artist: '周杰伦',
            title: '晴天',
            file: '/vol1/1000/Music/周杰伦 - 晴天.mp3',
            score: 98.5,
            duration: '04:29',
          },
        ],
      },
      {
        ts: new Date(Date.now() - 3600000 * 2 + 1500).toISOString(),
        step: 3,
        kind: 'llm',
        request: {
          model: 'kimi-k1.5-free',
          messages: [
            { role: 'tool', tool_call_id: 'call_fzq_101', content: '...' },
          ],
        },
        response: {
          choices: [
            {
              message: {
                role: 'assistant',
                tool_calls: [
                  {
                    id: 'call_speaker_102',
                    type: 'function',
                    function: {
                      name: 'speaker_play',
                      arguments: '{"file":"/vol1/1000/Music/周杰伦 - 晴天.mp3","title":"晴天","artist":"周杰伦"}',
                    },
                  },
                ],
              },
              finish_reason: 'tool_calls',
            },
          ],
        },
      },
      {
        ts: new Date(Date.now() - 3600000 * 2 + 2000).toISOString(),
        step: 4,
        kind: 'tool',
        tool: 'speaker_play',
        args: { file: '/vol1/1000/Music/周杰伦 - 晴天.mp3', title: '晴天', artist: '周杰伦' },
        result: { status: 'playing', device: '小爱音箱 Pro', driver: 'xiaomi_miot', volume: 45 },
      },
    ],
  },
  turn_weather_02: {
    available: true,
    items: [
      {
        ts: new Date(Date.now() - 3600000 * 5).toISOString(),
        step: 1,
        kind: 'llm',
        request: { model: 'kimi-k1.5-free', messages: [{ role: 'user', content: '明天出门需要带伞吗？' }] },
        response: { choices: [{ message: { content: '明天多云转晴，降水概率仅 10%，无需带伞。' } }] },
      },
    ],
  },
  turn_debug_03: {
    available: true,
    items: [
      {
        ts: new Date(Date.now() - 86400000).toISOString(),
        step: 1,
        kind: 'llm',
        request: { model: 'kimi-k1.5-free', messages: [{ role: 'user', content: '检查系统环境与工具链版本' }] },
        response: { choices: [{ message: { content: '系统核心组件均正常就绪' } }] },
      },
    ],
  },
};

let mockAllTraceItems: TraceItem[] = [
  {
    id: 'tr_turn_01',
    ts: new Date(Date.now() - 3600000 * 2).toISOString(),
    step: 0,
    kind: 'turn',
    name: '播放周杰伦《晴天》',
    status: 'ok',
    duration_ms: 1240,
    first_token_ms: 185,
    prompt_tokens: 1282,
    completion_tokens: 145,
    conversation_id: 'conv_music_01',
    turn_id: 'turn_music_01',
    channel: 'speaker',
  },
  {
    id: 'tr_llm_01',
    ts: new Date(Date.now() - 3600000 * 2).toISOString(),
    step: 1,
    kind: 'llm',
    name: 'kimi-k1.5-free',
    status: 'ok',
    duration_ms: 320,
    first_token_ms: 185,
    prompt_tokens: 350,
    completion_tokens: 42,
    finish_reason: 'tool_calls',
    conversation_id: 'conv_music_01',
    turn_id: 'turn_music_01',
    channel: 'speaker',
  },
  {
    id: 'tr_tool_01',
    ts: new Date(Date.now() - 3600000 * 2 + 1000).toISOString(),
    step: 2,
    kind: 'tool',
    name: 'fzq',
    status: 'ok',
    duration_ms: 22,
    args: { queries: '周杰伦 晴天', limit: 3 },
    result_summary: '找到 1 首匹配曲目: 《晴天》 (周杰伦, 得分 98.5)',
    conversation_id: 'conv_music_01',
    turn_id: 'turn_music_01',
    channel: 'speaker',
  },
  {
    id: 'tr_tool_02',
    ts: new Date(Date.now() - 3600000 * 2 + 2000).toISOString(),
    step: 4,
    kind: 'tool',
    name: 'speaker_play',
    status: 'ok',
    duration_ms: 140,
    args: { file: '/vol1/1000/Music/周杰伦 - 晴天.mp3', title: '晴天' },
    result_summary: '已推送音频流到小爱音箱 Pro',
    conversation_id: 'conv_music_01',
    turn_id: 'turn_music_01',
    channel: 'speaker',
  },
  {
    id: 'tr_turn_02',
    ts: new Date(Date.now() - 3600000 * 5).toISOString(),
    step: 0,
    kind: 'turn',
    name: '明日天气查询与穿衣提醒',
    status: 'ok',
    duration_ms: 820,
    first_token_ms: 145,
    prompt_tokens: 280,
    completion_tokens: 72,
    conversation_id: 'conv_weather_02',
    turn_id: 'turn_weather_02',
    channel: 'speaker',
  },
  {
    id: 'tr_err_01',
    ts: new Date(Date.now() - 3600000 * 12).toISOString(),
    step: 2,
    kind: 'tool',
    name: 'speaker_play',
    status: 'error',
    duration_ms: 5012,
    args: { file: '/vol1/1000/Music/NotExist.mp3' },
    error: 'MIoT 设备无响应 (504 Gateway Timeout)，音箱处于离线静音模式',
    result_summary: '调用失败: 设备超时未应答',
    conversation_id: 'conv_music_01',
    turn_id: 'turn_music_01',
    channel: 'speaker',
  },
  {
    id: 'tr_turn_03',
    ts: new Date(Date.now() - 86400000).toISOString(),
    step: 0,
    kind: 'turn',
    name: 'NAS 本地曲库索引自检',
    status: 'ok',
    duration_ms: 950,
    first_token_ms: 160,
    prompt_tokens: 570,
    completion_tokens: 118,
    conversation_id: 'conv_debug_03',
    turn_id: 'turn_debug_03',
    channel: 'web',
  },
  {
    id: 'tr_tool_03',
    ts: new Date(Date.now() - 86400000 + 400).toISOString(),
    step: 2,
    kind: 'tool',
    name: 'system_check',
    status: 'ok',
    duration_ms: 45,
    args: { check: 'tools' },
    result_summary: 'fzq: v0.2.1, jq: v1.7.1, yq: v4.44.1 正常',
    conversation_id: 'conv_debug_03',
    turn_id: 'turn_debug_03',
    channel: 'web',
  },
];

let mockMediaStatus: MediaStatus = {
  running: false,
  last_sync_at: new Date(Date.now() - 3600000 * 3).toISOString(),
  tracks: 14280,
  duration_ms: 1840,
  result: '已扫描 14,280 首曲目，无损格式占比 82%，模糊检索索引已更新',
};

// ---------------- Mock Filesystem Tree ----------------
interface MockDirNode {
  parent: string | null;
  entries: FsDirEntry[];
  truncated?: boolean;
}

const mockFsTree: Record<string, MockDirNode> = {
  '/': {
    parent: null,
    entries: [
      { name: 'vol1', path: '/vol1' },
      { name: 'vol2', path: '/vol2' },
      { name: 'home', path: '/home' },
      { name: 'etc', path: '/etc' },
      { name: 'var', path: '/var' },
    ],
  },
  '/vol1': {
    parent: '/',
    entries: [
      { name: '1000', path: '/vol1/1000' },
      { name: 'public', path: '/vol1/public' },
    ],
  },
  '/vol1/public': {
    parent: '/vol1',
    entries: [
      { name: 'Shared_Audio', path: '/vol1/public/Shared_Audio' },
    ],
  },
  '/vol1/1000': {
    parent: '/vol1',
    entries: [
      { name: 'Music', path: '/vol1/1000/Music' },
      { name: 'Audiobooks', path: '/vol1/1000/Audiobooks' },
      { name: 'Media_Symlink', path: '/vol1/1000/Media_Symlink', symlink: true },
      { name: 'Photos', path: '/vol1/1000/Photos' },
      { name: 'Videos', path: '/vol1/1000/Videos' },
      { name: 'Downloads', path: '/vol1/1000/Downloads' },
    ],
  },
  '/vol1/1000/Music': {
    parent: '/vol1/1000',
    entries: [
      { name: 'JayChou', path: '/vol1/1000/Music/JayChou' },
      { name: 'EasonChan', path: '/vol1/1000/Music/EasonChan' },
      { name: 'Classical', path: '/vol1/1000/Music/Classical' },
      { name: 'Hi-Res Lossless', path: '/vol1/1000/Music/Hi-Res Lossless' },
      { name: 'Soundtracks', path: '/vol1/1000/Music/Soundtracks' },
    ],
  },
  '/vol1/1000/Music/JayChou': {
    parent: '/vol1/1000/Music',
    entries: [
      { name: '2000 - Jay', path: '/vol1/1000/Music/JayChou/2000 - Jay' },
      { name: '2001 - Fantasy', path: '/vol1/1000/Music/JayChou/2001 - Fantasy' },
      { name: '2004 - Common Jasmine Orange', path: '/vol1/1000/Music/JayChou/2004 - Common Jasmine Orange' },
    ],
  },
  '/vol1/1000/Music/EasonChan': {
    parent: '/vol1/1000/Music',
    entries: [
      { name: 'U87', path: '/vol1/1000/Music/EasonChan/U87' },
      { name: 'H3M', path: '/vol1/1000/Music/EasonChan/H3M' },
    ],
  },
  '/vol1/1000/Music/Classical': {
    parent: '/vol1/1000/Music',
    entries: [
      { name: 'Beethoven', path: '/vol1/1000/Music/Classical/Beethoven' },
      { name: 'Bach', path: '/vol1/1000/Music/Classical/Bach' },
      { name: 'Chopin', path: '/vol1/1000/Music/Classical/Chopin' },
    ],
  },
  '/vol1/1000/Music/Soundtracks': {
    parent: '/vol1/1000/Music',
    entries: [
      { name: 'Interstellar', path: '/vol1/1000/Music/Soundtracks/Interstellar' },
      { name: 'Ghibli', path: '/vol1/1000/Music/Soundtracks/Ghibli' },
    ],
  },
  '/vol1/1000/Music/Hi-Res Lossless': {
    parent: '/vol1/1000/Music',
    entries: [
      { name: 'DSD-128', path: '/vol1/1000/Music/Hi-Res Lossless/DSD-128' },
      { name: 'FLAC-24bit', path: '/vol1/1000/Music/Hi-Res Lossless/FLAC-24bit' },
    ],
  },
  '/vol1/1000/Audiobooks': {
    parent: '/vol1/1000',
    entries: [
      { name: 'ThreeBody', path: '/vol1/1000/Audiobooks/ThreeBody' },
      { name: 'SciFi_Novels', path: '/vol1/1000/Audiobooks/SciFi_Novels' },
    ],
  },
  '/vol2': {
    parent: '/',
    entries: [
      { name: 'Audio', path: '/vol2/Audio' },
      { name: 'Backups', path: '/vol2/Backups' },
    ],
  },
  '/vol2/Audio': {
    parent: '/vol2',
    entries: [
      { name: 'Lossless', path: '/vol2/Audio/Lossless' },
      { name: 'Podcasts', path: '/vol2/Audio/Podcasts' },
      { name: 'HiFi_DSD', path: '/vol2/Audio/HiFi_DSD' },
    ],
  },
  '/vol2/Audio/Lossless': {
    parent: '/vol2/Audio',
    entries: [
      { name: 'DSD_SACD', path: '/vol2/Audio/Lossless/DSD_SACD' },
      { name: 'Jazz', path: '/vol2/Audio/Lossless/Jazz' },
      { name: 'Pop', path: '/vol2/Audio/Lossless/Pop' },
      { name: 'Rock', path: '/vol2/Audio/Lossless/Rock' },
    ],
  },
  '/vol2/Audio/Lossless/Jazz': {
    parent: '/vol2/Audio/Lossless',
    entries: [
      { name: 'Miles Davis', path: '/vol2/Audio/Lossless/Jazz/Miles Davis' },
      { name: 'Bill Evans', path: '/vol2/Audio/Lossless/Jazz/Bill Evans' },
    ],
  },
  '/vol2/Audio/Lossless/Pop': {
    parent: '/vol2/Audio/Lossless',
    entries: [
      { name: 'Chinese', path: '/vol2/Audio/Lossless/Pop/Chinese' },
      { name: 'Western', path: '/vol2/Audio/Lossless/Pop/Western' },
    ],
  },
  '/vol2/Audio/Podcasts': {
    parent: '/vol2/Audio',
    entries: [
      { name: 'TechTalk', path: '/vol2/Audio/Podcasts/TechTalk' },
      { name: 'History', path: '/vol2/Audio/Podcasts/History' },
    ],
  },
  '/vol2/Audio/HiFi_DSD': {
    parent: '/vol2/Audio',
    entries: [
      { name: 'SACD_ISO', path: '/vol2/Audio/HiFi_DSD/SACD_ISO' },
      { name: 'DFF', path: '/vol2/Audio/HiFi_DSD/DFF' },
    ],
  },
  '/vol2/Backups': {
    parent: '/vol2',
    entries: [
      { name: 'Snapshot_2026', path: '/vol2/Backups/Snapshot_2026' },
    ],
  },
  '/etc': {
    parent: '/',
    entries: [
      { name: 'nginx', path: '/etc/nginx' },
      { name: 'ssl', path: '/etc/ssl' },
    ],
  },
  '/home': {
    parent: '/',
    entries: [
      { name: 'user', path: '/home/user' },
    ],
  },
};

async function mockGetFsDirs(path?: string): Promise<FsDirsResponse> {
  await new Promise((r) => setTimeout(r, 120));
  // If path is omitted, default to primary fnOS media folder
  const targetPath = !path || !path.trim() ? '/vol1/1000' : path.trim().replace(/\/+$/, '') || '/';

  if (targetPath === '/restricted' || targetPath === '/root') {
    throw new ApiError('访问被拒绝：权限不足 (403 Forbidden)', 403);
  }
  if (targetPath.startsWith('/nonexistent') || targetPath.startsWith('/invalid')) {
    throw new ApiError('目录未找到 (404 Not Found)', 404);
  }

  const node = mockFsTree[targetPath];
  if (node) {
    return {
      path: targetPath,
      parent: node.parent,
      entries: [...node.entries],
      truncated: node.truncated,
    };
  }

  const lastSlash = targetPath.lastIndexOf('/');
  const parent = lastSlash <= 0 ? '/' : targetPath.substring(0, lastSlash);
  return {
    path: targetPath,
    parent: targetPath === '/' ? null : parent,
    entries: [],
  };
}

async function mockValidateFsPath(path: string): Promise<ValidateFsPathResponse> {
  await new Promise((r) => setTimeout(r, 150));
  const cleanPath = path.trim().replace(/\/+$/, '') || '/';

  if (
    cleanPath.startsWith('/etc') ||
    cleanPath.startsWith('/var') ||
    cleanPath === '/root' ||
    cleanPath.includes('restricted')
  ) {
    return {
      ok: false,
      reason: '权限不足：该目录不在 fnOS 授权访问范围内',
    };
  }

  if (cleanPath.startsWith('/nonexistent') || cleanPath.startsWith('/invalid')) {
    return {
      ok: false,
      reason: '目录不存在或无法读取',
    };
  }

  const knownEstimates: Record<string, number> = {
    '/vol1/1000/Music': 8420,
    '/vol2/Audio/Lossless': 5860,
    '/vol1/1000/Music/JayChou': 320,
    '/vol1/1000/Music/JayChou/2000 - Jay': 10,
    '/vol1/1000/Music/JayChou/2001 - Fantasy': 10,
    '/vol1/1000/Music/JayChou/2004 - Common Jasmine Orange': 10,
    '/vol1/1000/Music/EasonChan': 215,
    '/vol1/1000/Music/Classical': 1450,
    '/vol1/1000/Music/Soundtracks': 380,
    '/vol1/1000/Music/Hi-Res Lossless': 1120,
    '/vol1/1000/Audiobooks': 48,
    '/vol2/Audio/Lossless/DSD_SACD': 450,
    '/vol2/Audio/Lossless/Jazz': 380,
    '/vol2/Audio/Lossless/Pop': 2400,
    '/vol2/Audio/Lossless/Rock': 960,
    '/vol2/Audio/HiFi_DSD': 680,
    '/vol2/Audio/Podcasts': 120,
    '/vol1/1000': 8468,
    '/vol2/Audio': 6540,
  };

  if (knownEstimates[cleanPath] !== undefined) {
    return {
      ok: true,
      audio_files_estimate: knownEstimates[cleanPath],
    };
  }

  if (
    cleanPath.includes('Photo') ||
    cleanPath.includes('Video') ||
    cleanPath.includes('Download') ||
    cleanPath.includes('Backup')
  ) {
    return {
      ok: true,
      audio_files_estimate: 0,
      reason: '未检测到支持的音频文件',
    };
  }

  return {
    ok: true,
    audio_files_estimate: Math.max(10, ((cleanPath.length * 37) % 650) + 15),
  };
}

async function mockUpdateMediaSettings(
  body: UpdateMediaSettingsRequest
): Promise<UpdateMediaSettingsResponse> {
  await new Promise((r) => setTimeout(r, 200));

  const fieldErrors: Record<string, string> = {};
  if (body.folders) {
    for (const folder of body.folders) {
      if (
        folder.includes('error') ||
        folder.startsWith('/etc') ||
        folder.startsWith('/var') ||
        folder === '/root'
      ) {
        fieldErrors[folder] = '目录超出 fnOS 授权访问范围，请使用授权存储卷';
      }
    }
  }

  if (body.filename_pattern !== undefined && body.filename_pattern.trim().length > 0) {
    if (!body.filename_pattern.includes('?P<title>') && !body.filename_pattern.includes('(?P<title>')) {
      fieldErrors['filename_pattern'] = '正则表达式必须包含命名分组 title，例如 (?P<title>.+)';
    }
  }

  if (body.max_files !== undefined) {
    if (body.max_files <= 0) {
      fieldErrors['max_files'] = '最大文件数必须大于 0';
    }
  }

  if (Object.keys(fieldErrors).length > 0) {
    throw new ApiError('部分媒体设置项校验失败，请修改后重试', 400, fieldErrors);
  }

  if (body.folders !== undefined) {
    mockSettings.media.folders = [...body.folders];
    mockSectionValues.media.folders = [...body.folders];
  }
  if (body.extensions !== undefined) {
    mockSettings.media.extensions = [...body.extensions];
    mockSectionValues.media.extensions = [...body.extensions];
  }
  if (body.sync_interval !== undefined) {
    mockSettings.media.sync_interval = body.sync_interval;
    mockSectionValues.media.sync_interval = body.sync_interval;
  }
  if (body.filename_pattern !== undefined) {
    mockSettings.media.filename_pattern = body.filename_pattern;
    mockSectionValues.media.filename_pattern = body.filename_pattern;
  }
  if (body.max_files !== undefined) {
    mockSettings.media.max_files = body.max_files;
    mockSectionValues.media.max_files = body.max_files;
  }
  if (body.sync_on_start !== undefined) {
    mockSettings.media.sync_on_start = body.sync_on_start;
    mockSectionValues.media.sync_on_start = body.sync_on_start;
  }

  const syncStarted = true;
  if (syncStarted) {
    mockMediaStatus.running = true;
    mockMediaStatus.result = '曲库配置已更新，正在执行全量文件重新扫描...';
    setTimeout(() => {
      mockMediaStatus.running = false;
      mockMediaStatus.last_sync_at = new Date().toISOString();
      mockMediaStatus.tracks = 14280 + (mockSettings.media.folders?.length || 0) * 15;
      mockMediaStatus.duration_ms = 1520;
      mockMediaStatus.result = `已扫描并同步完成，共 ${mockMediaStatus.tracks} 首曲目`;
    }, 3000);
  }

  return { sync_started: syncStarted };
}

// ---------------- Mock Mode Utilities ----------------

export function isMockMode(): boolean {
  if (typeof window === 'undefined') return false;
  const params = new URLSearchParams(window.location.search);
  if (params.get('mock') === '1' || params.get('mock') === 'true') {
    return true;
  }
  return localStorage.getItem('vox_mock') === 'true';
}

export function setMockMode(enabled: boolean): void {
  if (typeof window === 'undefined') return;
  if (enabled) {
    localStorage.setItem('vox_mock', 'true');
  } else {
    localStorage.removeItem('vox_mock');
  }
}

// ---------------- Helper API Client with Relative Paths ----------------

export class ApiError extends Error {
  status: number;
  fields?: Record<string, string>;

  constructor(message: string, status: number, fields?: Record<string, string>) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.fields = fields;
  }
}

async function handleResponse<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let errMsg = `请求失败 (${res.status} ${res.statusText})`;
    let fields: Record<string, string> | undefined;
    try {
      const json = await res.json();
      if (json?.error || json?.message) {
        errMsg = json.error || json.message;
      }
      if (json?.fields) {
        fields = json.fields;
      }
    } catch {
      // ignore
    }
    throw new ApiError(errMsg, res.status, fields);
  }
  if (res.status === 204) {
    return {} as T;
  }
  return res.json();
}

// ---------------- Exported API Methods ----------------

/**
 * GET api/conversations?limit=50&before=<id>
 */
export async function getConversations(params?: {
  limit?: number;
  before?: string;
}): Promise<ConversationListResponse> {
  if (isMockMode()) {
    let items = [...mockConversations];
    if (params?.before) {
      const idx = items.findIndex((c) => c.id === params.before);
      if (idx !== -1) {
        items = items.slice(idx + 1);
      }
    }
    const limit = params?.limit ?? 50;
    const paged = items.slice(0, limit);
    return {
      items: paged,
      next_before: paged.length === limit && items.length > limit ? paged[paged.length - 1].id : undefined,
    };
  }

  const query = new URLSearchParams();
  if (params?.limit) query.set('limit', String(params.limit));
  if (params?.before) query.set('before', params.before);
  const qStr = query.toString();
  // HARD REQUIREMENT: relative path
  const url = `api/conversations${qStr ? `?${qStr}` : ''}`;

  try {
    const res = await fetch(url);
    return await handleResponse<ConversationListResponse>(res);
  } catch (err) {
    // If backend is unreachable and not explicitly mocked, give helpful fallback or rethrow
    throw err;
  }
}

/**
 * POST api/conversations {title?}
 */
export async function createConversation(body?: {
  title?: string;
}): Promise<{ id: string } & Partial<Conversation>> {
  if (isMockMode()) {
    const newConv: Conversation = {
      id: 'conv_' + Math.random().toString(36).substring(2, 9),
      title: body?.title || '新对话',
      channel: 'web',
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };
    mockConversations.unshift(newConv);
    mockMessages[newConv.id] = [];
    return newConv;
  }

  const res = await fetch('api/conversations', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body || {}),
  });
  return handleResponse<{ id: string } & Partial<Conversation>>(res);
}

/**
 * GET api/conversations/{id}/messages
 */
export async function getMessages(conversationId: string): Promise<MessageListResponse> {
  if (isMockMode()) {
    return {
      items: mockMessages[conversationId] || [],
    };
  }

  const res = await fetch(`api/conversations/${encodeURIComponent(conversationId)}/messages`);
  return handleResponse<MessageListResponse>(res);
}

/**
 * GET api/llm/providers
 */
export async function getLLMProviders(): Promise<LLMProvidersResponse> {
  if (isMockMode()) {
    return { items: JSON.parse(JSON.stringify(mockProviders)) };
  }

  const res = await fetch('api/llm/providers');
  return handleResponse<LLMProvidersResponse>(res);
}

/**
 * POST api/llm/models {provider, base_url?, api_key?}
 */
export async function getLLMModels(body: GetLLMModelsRequest): Promise<LLMModelsResponse> {
  if (isMockMode()) {
    await new Promise((r) => setTimeout(r, 350));
    const modelsByProvider: Record<string, string[]> = {
      'opencode-zen': [
        'kimi-k1.5-free',
        'minimax-01-free',
        'glm-4-flash-free',
        'qwen-2.5-coder-free',
        'deepseek-v3',
        'deepseek-r1',
        'claude-3-5-sonnet',
      ],
      'deepseek': [
        'deepseek-chat',
        'deepseek-reasoner',
      ],
      'dashscope': [
        'qwen-plus',
        'qwen-max',
        'qwen-turbo',
        'qwen-2.5-72b-instruct',
        'qwen-2.5-coder-32b-instruct',
      ],
      'openai': [
        'gpt-4o-mini',
        'gpt-4o',
        'o3-mini',
        'gpt-3.5-turbo',
      ],
      'siliconflow': [
        'Qwen/Qwen3-8B',
        'deepseek-ai/DeepSeek-V3',
        'deepseek-ai/DeepSeek-R1',
        'THUDM/glm-4-9b-chat',
        'Pro/deepseek-ai/DeepSeek-V3',
        'BAAI/bge-m3',
      ],
      'openrouter': [
        'deepseek/deepseek-r1:free',
        'meta-llama/llama-3.3-70b-instruct:free',
        'google/gemini-2.0-flash-exp:free',
        'mistralai/mistral-7b-instruct:free',
        'anthropic/claude-3.5-sonnet',
        'openai/gpt-4o-mini',
        'deepseek/deepseek-chat',
      ],
      'ollama': [
        'qwen2.5:7b',
        'llama3.2:3b',
        'deepseek-r1:8b',
        'mistral:7b',
        'nomic-embed-text',
      ],
      'custom': [
        'custom-model',
        'vllm-model',
        'llama-3.1',
      ],
    };
    const list = modelsByProvider[body.provider] || ['custom-model'];
    return {
      items: list.map((id) => ({ id })),
    };
  }

  const res = await fetch('api/llm/models', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  return handleResponse<LLMModelsResponse>(res);
}

/**
 * POST api/llm/test {provider, base_url?, api_key?, model}
 */
export async function testLLM(body: TestLLMRequest): Promise<TestLLMResponse> {
  if (isMockMode()) {
    await new Promise((r) => setTimeout(r, 450));
    const preset = mockProviders.find((p) => p.id === body.provider);
    const keyRequired = preset ? preset.key_required : true;
    const hasKey =
      (body.api_key && body.api_key.trim().length > 0) ||
      (mockSettings.llm.provider === body.provider && mockSettings.llm.has_api_key);

    if (keyRequired && !hasKey) {
      return {
        ok: false,
        error: `服务商 [${preset?.name || body.provider}] 需要 API Key 鉴权，但当前未提供密钥 (401 Unauthorized)`,
      };
    }

    if (!body.model || !body.model.trim()) {
      return {
        ok: false,
        error: '未指定模型名称 (Model 不能为空)',
      };
    }

    return {
      ok: true,
      latency_ms: Math.floor(Math.random() * 85) + 160,
      reply: `你好！来自 ${preset?.name || body.provider}（模型: ${body.model}）的响应测试正常，意图识别通道就绪。`,
    };
  }

  try {
    const res = await fetch('api/llm/test', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });

    const data = await res.json().catch(() => null);
    if (!res.ok) {
      return {
        ok: false,
        error: data?.error || data?.message || `测试请求失败 (${res.status} ${res.statusText})`,
      };
    }
    return data as TestLLMResponse;
  } catch (err: any) {
    return {
      ok: false,
      error: err.message || '网络通信故障，无法连接至后端测试服务',
    };
  }
}

/**
 * GET api/settings
 */
export async function getSettings(): Promise<SettingsResponse> {
  if (isMockMode()) {
    return JSON.parse(JSON.stringify(mockSettings));
  }

  const res = await fetch('api/settings');
  return handleResponse<SettingsResponse>(res);
}

/**
 * PUT api/settings/llm
 */
export async function updateLLMSettings(body: UpdateLLMRequest): Promise<void> {
  if (isMockMode()) {
    await new Promise((r) => setTimeout(r, 200));
    if (!body.provider) {
      throw new Error('未选择模型服务商 (Provider 不能为空)');
    }
    if (!body.model || !body.model.trim()) {
      throw new Error('模型名称不能为空 (Model 不能为空)');
    }
    const preset = mockProviders.find((p) => p.id === body.provider);
    if (preset?.key_required) {
      const isSwitching = mockSettings.llm.provider !== body.provider;
      if (isSwitching && (!body.api_key || !body.api_key.trim())) {
        throw new Error(`服务商 [${preset.name}] 需要 API Key，切换后请填入有效密钥`);
      }
    }

    mockSettings.llm.provider = body.provider;
    mockSettings.llm.model = body.model.trim();
    if (body.base_url !== undefined) mockSettings.llm.base_url = body.base_url.trim();
    if (body.temperature !== undefined) mockSettings.llm.temperature = body.temperature;
    if (body.api_key && body.api_key.trim().length > 0) {
      const k = body.api_key.trim();
      mockSettings.llm.has_api_key = true;
      mockSettings.llm.api_key_masked = `${k.slice(0, 5)}****************${k.slice(-4)}`;
    } else if (mockSettings.llm.provider !== body.provider) {
      mockSettings.llm.has_api_key = false;
      mockSettings.llm.api_key_masked = '';
    }
    return;
  }

  const res = await fetch('api/settings/llm', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  await handleResponse<void>(res);
}

/**
 * GET api/media/status
 */
export async function getMediaStatus(): Promise<MediaStatus> {
  if (isMockMode()) {
    return { ...mockMediaStatus };
  }

  const res = await fetch('api/media/status');
  return handleResponse<MediaStatus>(res);
}

/**
 * GET api/settings/media
 */
export async function getMediaSettings(): Promise<MediaConfig> {
  if (isMockMode()) {
    return { ...mockSettings.media };
  }

  const res = await fetch('api/settings/media');
  return handleResponse<MediaConfig>(res);
}

/**
 * PUT api/settings/media {folders, extensions?, sync_interval?}
 */
export async function updateMediaSettings(
  body: UpdateMediaSettingsRequest
): Promise<UpdateMediaSettingsResponse> {
  if (isMockMode()) {
    return mockUpdateMediaSettings(body);
  }

  const res = await fetch('api/settings/media', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  return handleResponse<UpdateMediaSettingsResponse>(res);
}

/**
 * GET api/fs/dirs?path=/abs
 */
export async function getFsDirs(path?: string): Promise<FsDirsResponse> {
  if (isMockMode()) {
    return mockGetFsDirs(path);
  }

  const query = path ? `?path=${encodeURIComponent(path)}` : '';
  const res = await fetch(`api/fs/dirs${query}`);
  return handleResponse<FsDirsResponse>(res);
}

/**
 * POST api/fs/validate {path}
 */
export async function validateFsPath(path: string): Promise<ValidateFsPathResponse> {
  if (isMockMode()) {
    return mockValidateFsPath(path);
  }

  const res = await fetch('api/fs/validate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ path }),
  });
  return handleResponse<ValidateFsPathResponse>(res);
}

/**
 * POST api/media/sync
 */
export async function triggerMediaSync(): Promise<void> {
  if (isMockMode()) {
    mockMediaStatus.running = true;
    mockMediaStatus.result = '曲库扫描与索引构建进行中...';
    // simulate background finish in mock
    setTimeout(() => {
      mockMediaStatus.running = false;
      mockMediaStatus.last_sync_at = new Date().toISOString();
      mockMediaStatus.tracks = (mockMediaStatus.tracks || 14280) + Math.floor(Math.random() * 5);
      mockMediaStatus.duration_ms = 1450;
      mockMediaStatus.result = `已扫描并同步完成，共 ${mockMediaStatus.tracks} 首曲目`;
    }, 3000);
    return;
  }

  const res = await fetch('api/media/sync', {
    method: 'POST',
  });
  if (res.status !== 202 && !res.ok) {
    throw new Error(`启动同步失败: ${res.statusText}`);
  }
}

/**
 * GET healthz
 */
export async function getHealth(): Promise<HealthResponse> {
  if (isMockMode()) {
    return {
      status: 'ok',
      tools: {
        fzq: true,
        jq: true,
        yq: true,
      },
    };
  }

  const res = await fetch('healthz');
  return handleResponse<HealthResponse>(res);
}

/**
 * Add message to local mock store when user sends in mock mode
 */
export function addMockUserMessage(conversationId: string, text: string): Message {
  const msg: Message = {
    id: 'user_' + Date.now(),
    role: 'user',
    content: text,
    created_at: new Date().toISOString(),
  };
  if (!mockMessages[conversationId]) {
    mockMessages[conversationId] = [];
  }
  mockMessages[conversationId].push(msg);

  // Update conversation title if first message
  const conv = mockConversations.find((c) => c.id === conversationId);
  if (conv) {
    conv.updated_at = new Date().toISOString();
    if (conv.title === '新对话') {
      conv.title = text.slice(0, 20);
    }
  }
  return msg;
}

export function appendMockAssistantMessage(conversationId: string, msg: Message): void {
  if (!mockMessages[conversationId]) {
    mockMessages[conversationId] = [];
  }
  mockMessages[conversationId].push(msg);
}

/**
 * GET api/settings/schema
 */
export async function getSettingsSchema(): Promise<SettingsSchemaResponse> {
  if (isMockMode()) {
    return JSON.parse(JSON.stringify(mockSettingsSchema));
  }

  const res = await fetch('api/settings/schema');
  return handleResponse<SettingsSchemaResponse>(res);
}

/**
 * GET api/settings/{section}
 */
export async function getSectionSettings(section: string): Promise<Record<string, any>> {
  if (isMockMode()) {
    const val = mockSectionValues[section] || {};
    return JSON.parse(JSON.stringify(val));
  }

  const res = await fetch(`api/settings/${encodeURIComponent(section)}`);
  return handleResponse<Record<string, any>>(res);
}

/**
 * PUT api/settings/{section}
 */
export async function updateSectionSettings(
  section: string,
  body: Record<string, any>
): Promise<PutSectionSettingsResponse> {
  if (isMockMode()) {
    await new Promise((r) => setTimeout(r, 200));

    // Simulate validation error if any value contains __error__
    const fieldErrors: Record<string, string> = {};
    for (const [k, v] of Object.entries(body)) {
      if (typeof v === 'string' && v.includes('__error__')) {
        fieldErrors[k] = '配置值不符合安全规范';
      }
    }
    if (Object.keys(fieldErrors).length > 0) {
      throw new ApiError('部分设置项校验失败', 400, fieldErrors);
    }

    mockSectionValues[section] = {
      ...(mockSectionValues[section] || {}),
      ...body,
    };

    // Check if any updated key in schema has restart_required
    if (section === 'sources' && body.items) {
      // Never accept media_library in PUT api/settings/sources
      const userItems = (body.items as SourceItem[]).filter(
        (it) => it.name !== 'media_library'
      );
      mockSources = userItems;
      body.items = userItems;
    }

    const secSchema = mockSettingsSchema.sections.find((s) => s.name === section);
    let restartRequired = false;
    if (secSchema) {
      for (const k of Object.keys(body)) {
        const f = secSchema.fields.find((fld) => fld.key === k);
        if (f?.restart_required) {
          restartRequired = true;
          break;
        }
      }
    }

    return { restart_required: restartRequired };
  }

  const res = await fetch(`api/settings/${encodeURIComponent(section)}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  return handleResponse<PutSectionSettingsResponse>(res);
}

/**
 * GET api/settings/aliases
 */
export async function getAliasesSettings(): Promise<AliasesSettingsResponse> {
  if (isMockMode()) {
    return { items: JSON.parse(JSON.stringify(mockAliases)) };
  }

  const res = await fetch('api/settings/aliases');
  return handleResponse<AliasesSettingsResponse>(res);
}

/**
 * PUT api/settings/aliases
 */
export async function updateAliasesSettings(
  body: AliasesSettingsResponse
): Promise<PutSectionSettingsResponse> {
  if (isMockMode()) {
    await new Promise((r) => setTimeout(r, 200));
    mockAliases = JSON.parse(JSON.stringify(body.items || []));
    mockSectionValues.aliases = { items: mockAliases };
    return { restart_required: false };
  }

  const res = await fetch('api/settings/aliases', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  return handleResponse<PutSectionSettingsResponse>(res);
}

/**
 * GET api/conversations/{id}/trace
 */
export async function getConversationTrace(
  id: string
): Promise<ConversationTraceResponse> {
  if (isMockMode()) {
    if (mockConversationTraces[id]) {
      return JSON.parse(JSON.stringify(mockConversationTraces[id]));
    }
    // Fallback: generate a synthetic trace turn for other mock conversations
    return {
      turns: [
        {
          turn_id: 'turn_' + id,
          ts: new Date().toISOString(),
          status: 'ok',
          duration_ms: 680,
          first_token_ms: 120,
          steps: [
            {
              id: 'st_1_' + id,
              ts: new Date().toISOString(),
              step: 1,
              kind: 'llm',
              name: 'kimi-k1.5-free',
              status: 'ok',
              duration_ms: 680,
              first_token_ms: 120,
              prompt_tokens: 220,
              completion_tokens: 58,
              finish_reason: 'stop',
            },
          ],
        },
      ],
    };
  }

  const res = await fetch(`api/conversations/${encodeURIComponent(id)}/trace`);
  return handleResponse<ConversationTraceResponse>(res);
}

/**
 * GET api/traces?status=&kind=&name=&since=&until=&conversation_id=&limit=100&before_id=
 */
export async function getTraces(
  params?: GetTracesParams
): Promise<TracesListResponse> {
  if (isMockMode()) {
    await new Promise((r) => setTimeout(r, 150));
    let items = [...mockAllTraceItems];

    if (params?.status) {
      items = items.filter((it) => it.status === params.status);
    }
    if (params?.kind) {
      items = items.filter((it) => it.kind === params.kind);
    }
    if (params?.name) {
      const q = params.name.toLowerCase();
      items = items.filter((it) => it.name.toLowerCase().includes(q));
    }
    if (params?.conversation_id) {
      items = items.filter((it) => it.conversation_id === params.conversation_id);
    }
    if (params?.before_id) {
      const idx = items.findIndex((it) => it.id === params.before_id);
      if (idx !== -1) {
        items = items.slice(idx + 1);
      }
    }

    const limit = params?.limit ?? 100;
    const paged = items.slice(0, limit);
    return {
      items: paged,
      next_before_id:
        paged.length === limit && items.length > limit
          ? paged[paged.length - 1].id
          : undefined,
    };
  }

  const query = new URLSearchParams();
  if (params?.status) query.set('status', params.status);
  if (params?.kind) query.set('kind', params.kind);
  if (params?.name) query.set('name', params.name);
  if (params?.since) query.set('since', params.since);
  if (params?.until) query.set('until', params.until);
  if (params?.conversation_id) query.set('conversation_id', params.conversation_id);
  if (params?.limit) query.set('limit', String(params.limit));
  if (params?.before_id) query.set('before_id', params.before_id);

  const qStr = query.toString();
  const url = `api/traces${qStr ? `?${qStr}` : ''}`;
  const res = await fetch(url);
  return handleResponse<TracesListResponse>(res);
}

/**
 * GET api/traces/{turn_id}/payload
 */
export async function getTracePayload(
  turnId: string
): Promise<TracePayloadResponse> {
  if (isMockMode()) {
    await new Promise((r) => setTimeout(r, 150));
    // If debug_mode is disabled in mock logging settings:
    if (mockSectionValues.logging?.debug_mode === false) {
      return { available: false, items: [] };
    }
    const payload = mockTracePayloads[turnId];
    if (payload) {
      return JSON.parse(JSON.stringify(payload));
    }
    return {
      available: true,
      items: [
        {
          ts: new Date().toISOString(),
          step: 1,
          kind: 'llm',
          request: { turn_id: turnId, model: 'kimi-k1.5-free' },
          response: { turn_id: turnId, reply: '模拟调试报文详情' },
        },
      ],
    };
  }

  const res = await fetch(`api/traces/${encodeURIComponent(turnId)}/payload`);
  return handleResponse<TracePayloadResponse>(res);
}

/**
 * GET api/media/tracks?q=&artist=&sort=title|artist|path&limit=50&offset=0
 */
export async function getMediaTracks(
  params?: GetMediaTracksParams
): Promise<MediaTracksResponse> {
  if (isMockMode()) {
    await new Promise((r) => setTimeout(r, 120));
    let list = [...mockTracks];

    // Filter by artist
    if (params?.artist && params.artist.trim()) {
      const art = params.artist.trim().toLowerCase();
      list = list.filter(
        (t) =>
          t.artist.toLowerCase().includes(art) ||
          t.artists.some((a) => a.toLowerCase().includes(art))
      );
    }

    // Fuzzy search by q
    if (params?.q && params.q.trim()) {
      const q = params.q.trim().toLowerCase();
      const scored = list
        .map((t) => {
          let score = 0;
          const tLower = t.title.toLowerCase();
          const artLower = t.artist.toLowerCase();
          const albLower = (t.album || '').toLowerCase();
          const aliasMatch = (t.alias || []).some((al) => al.toLowerCase().includes(q));

          if (tLower === q) {
            score = 99;
          } else if (tLower.startsWith(q)) {
            score = 95;
          } else if (tLower.includes(q)) {
            score = 90;
          } else if (aliasMatch) {
            score = 88;
          } else if (artLower.includes(q)) {
            score = 84;
          } else if (albLower.includes(q)) {
            score = 78;
          } else if (t.path.toLowerCase().includes(q)) {
            score = 70;
          }

          return { ...t, score };
        })
        .filter((t) => (t.score || 0) > 0)
        .sort((a, b) => (b.score || 0) - (a.score || 0));

      list = scored;
    } else {
      // Sort
      const sortBy = params?.sort || 'title';
      list.sort((a, b) => {
        if (sortBy === 'artist') return a.artist.localeCompare(b.artist, 'zh-CN');
        if (sortBy === 'path') return a.path.localeCompare(b.path);
        return a.title.localeCompare(b.title, 'zh-CN');
      });
    }

    const total = list.length;
    const offset = params?.offset ?? 0;
    const limit = params?.limit ?? 50;
    const paged = list.slice(offset, offset + limit);

    return { total, items: paged };
  }

  const query = new URLSearchParams();
  if (params?.q) query.set('q', params.q);
  if (params?.artist) query.set('artist', params.artist);
  if (params?.sort) query.set('sort', params.sort);
  if (params?.limit !== undefined) query.set('limit', String(params.limit));
  if (params?.offset !== undefined) query.set('offset', String(params.offset));

  const qStr = query.toString();
  const url = `api/media/tracks${qStr ? `?${qStr}` : ''}`;
  const res = await fetch(url);
  return handleResponse<MediaTracksResponse>(res);
}

/**
 * GET api/media/artists
 */
export async function getMediaArtists(): Promise<MediaArtistsResponse> {
  if (isMockMode()) {
    await new Promise((r) => setTimeout(r, 80));
    const counts: Record<string, number> = {};
    for (const trk of mockTracks) {
      for (const a of trk.artists) {
        counts[a] = (counts[a] || 0) + 1;
      }
    }
    const items = Object.entries(counts)
      .map(([name, count]) => ({ name, count }))
      .sort((a, b) => b.count - a.count);
    return { items };
  }

  const res = await fetch('api/media/artists');
  return handleResponse<MediaArtistsResponse>(res);
}

/**
 * GET api/sources
 */
export async function getDataSources(): Promise<DataSourcesResponse> {
  if (isMockMode()) {
    await new Promise((r) => setTimeout(r, 100));
    return { items: JSON.parse(JSON.stringify(mockDataSources)) };
  }

  const res = await fetch('api/sources');
  return handleResponse<DataSourcesResponse>(res);
}

/**
 * GET api/sources/{name}/preview?limit=20
 */
export async function getSourcePreview(
  name: string,
  limit: number = 20
): Promise<SourcePreviewResponse> {
  if (isMockMode()) {
    await new Promise((r) => setTimeout(r, 150));
    if (name === 'media_library') {
      const items = mockTracks.slice(0, limit).map((t) => ({
        id: t.id,
        title: t.title,
        artist: t.artist,
        album: t.album,
        alias: t.alias,
        path: t.path,
        ext: t.ext,
      }));
      return { total: 14280, items };
    }
    if (name === 'devices') {
      const items = [
        { id: 'dev_01', name: '客厅小爱音箱 Pro', room: '客厅', type: 'speaker', ip: '192.168.1.101' },
        { id: 'dev_02', name: '主卧吸顶灯', room: '主卧', type: 'light', ip: '192.168.1.102' },
        { id: 'dev_03', name: '阳台温湿度计', room: '阳台', type: 'sensor', ip: '192.168.1.103' },
        { id: 'dev_04', name: '书房空调整机', room: '书房', type: 'climate', ip: '192.168.1.104' },
      ];
      return { total: 12, items: items.slice(0, limit) };
    }
    if (name === 'radio_stations') {
      const items = [
        { id: 'rad_01', title: '中国之声', category: '国家级', freq: 'FM 106.1', url: 'http://live.cnr.cn/hls/zgzs.m3u8' },
        { id: 'rad_02', title: '音乐之声', category: '音乐', freq: 'FM 90.0', url: 'http://live.cnr.cn/hls/yyzs.m3u8' },
        { id: 'rad_03', title: '经济之声', category: '资讯', freq: 'FM 96.6', url: 'http://live.cnr.cn/hls/jjzs.m3u8' },
      ];
      return { total: 24, items: items.slice(0, limit) };
    }
    return { total: 0, items: [] };
  }

  const res = await fetch(`api/sources/${encodeURIComponent(name)}/preview?limit=${limit}`);
  return handleResponse<SourcePreviewResponse>(res);
}

/**
 * POST api/sources/{name}/test
 */
export async function testDataSource(
  name: string,
  body: SourceTestRequest
): Promise<SourceTestResponse> {
  if (isMockMode()) {
    await new Promise((r) => setTimeout(r, 220));
    const start = performance.now();

    if (body.tool === 'fuzzy_match') {
      const q = (body.query || '').toLowerCase().trim();
      let records: any[] = [];
      if (name === 'media_library') records = mockTracks;
      else if (name === 'devices') {
        records = [
          { id: 'dev_01', name: '客厅小爱音箱 Pro', room: '客厅', type: 'speaker' },
          { id: 'dev_02', name: '主卧吸顶灯', room: '主卧', type: 'light' },
        ];
      } else {
        records = [{ title: '中国之声', category: '国家级' }];
      }

      const matches = records.filter((r) => {
        if (!q) return true;
        const text = JSON.stringify(r).toLowerCase();
        return text.includes(q);
      });

      const elapsed = Math.round(performance.now() - start);
      return {
        query: body.query || '',
        count: matches.length,
        ambiguous: matches.length > 3,
        results: matches.slice(0, body.limit || 5).map((m, idx) => ({
          rank: idx + 1,
          score: Math.max(60, 98 - idx * 4),
          match: m.title || m.name || '匹配条目',
          reason: 'fzq 拼音声母与模糊字符双向召回',
          record: m,
        })),
        elapsed_ms: elapsed + 12,
      };
    }

    if (body.tool === 'query_json') {
      const elapsed = Math.round(performance.now() - start);
      // Simulate jq output
      if (body.filter?.includes('邓紫棋')) {
        return {
          output: ['光年之外', '泡沫', '句号', '来自天堂的魔鬼'],
          truncated: false,
          elapsed_ms: elapsed + 15,
        };
      }
      if (body.filter?.includes('length')) {
        return {
          output: 14280,
          truncated: false,
          elapsed_ms: elapsed + 8,
        };
      }
      if (body.filter?.includes('group_by')) {
        return {
          output: [
            { artist: '周杰伦', n: 10 },
            { artist: '陈奕迅', n: 7 },
            { artist: '邓紫棋', n: 7 },
            { artist: '王菲', n: 6 },
            { artist: '孙燕姿', n: 5 },
          ],
          truncated: false,
          elapsed_ms: elapsed + 22,
        };
      }
      return {
        output: mockTracks.slice(0, 3).map((t) => ({ title: t.title, artist: t.artist })),
        truncated: false,
        elapsed_ms: elapsed + 11,
      };
    }

    if (body.tool === 'query_yaml') {
      const elapsed = Math.round(performance.now() - start);
      return {
        output: 'stations:\n  - title: 中国之声\n    freq: FM 106.1\n  - title: 音乐之声\n    freq: FM 90.0',
        truncated: false,
        elapsed_ms: elapsed + 14,
      };
    }

    return { output: '执行完毕', elapsed_ms: 10 };
  }

  const res = await fetch(`api/sources/${encodeURIComponent(name)}/test`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  return handleResponse<SourceTestResponse>(res);
}
