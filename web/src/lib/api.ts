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
  sync_interval: string | number;
  extensions?: string[];
  max_files?: number;
}

export interface SpeakerConfig {
  driver: string;
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
    sync_interval: '6h0m0s',
    extensions: ['mp3', 'flac', 'm4a', 'wav', 'ogg', 'ape'],
    max_files: 100000,
  },
  speaker: {
    driver: 'xiaomi_miot',
  },
};

let mockMediaStatus: MediaStatus = {
  running: false,
  last_sync_at: new Date(Date.now() - 3600000 * 3).toISOString(),
  tracks: 14280,
  duration_ms: 1840,
  result: '已扫描 14,280 首曲目，无损格式占比 82%，模糊检索索引已更新',
};

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

async function handleResponse<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let errMsg = `请求失败 (${res.status} ${res.statusText})`;
    try {
      const json = await res.json();
      if (json?.error || json?.message) {
        errMsg = json.error || json.message;
      }
    } catch {
      // ignore
    }
    throw new Error(errMsg);
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
