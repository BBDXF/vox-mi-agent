export interface Conversation {
  id: string;
  title?: string;
  channel?: string;
  device_id?: string;
  device_name?: string;
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
  timeout?: string;
  extra_body?: Record<string, any>;
}

export interface ToolStatus {
  path: string;
  ok: boolean;
  version?: string;
  error?: string;
}

export type MediaTagMode = 'filename_first' | 'tags_first' | 'filename_only';

export interface MediaConfig {
  folders: string[];
  sync_interval: string;
  extensions: string[];
  accessible_roots?: string[];
  max_files?: number;
  filename_pattern?: string;
  sync_on_start?: boolean;
  tag_mode?: MediaTagMode;
}

export interface UpdateMediaSettingsRequest {
  folders?: string[];
  extensions?: string[];
  sync_interval?: string;
  filename_pattern?: string;
  max_files?: number;
  sync_on_start?: boolean;
  tag_mode?: MediaTagMode;
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

export type TrackTagSource = 'filename' | 'tags' | 'mixed';

export interface MediaTrackItem {
  id: string;
  title: string;
  artist: string;
  artists: string[];
  album: string;
  year?: number;
  track_no?: number;
  genre?: string;
  tag_source: TrackTagSource;
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
  read_only?: boolean;
  restart_required?: boolean;
  description?: string;
}

export interface SchemaSection {
  name: string; // 'llm' | 'media' | 'aliases' | 'search' | 'tools' | 'speaker' | 'server' | 'logging'
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
  media: MediaConfig;
  speaker: SpeakerConfig;
}

export interface UpdateLLMRequest {
  provider?: string;
  base_url?: string;
  model?: string;
  api_key?: string;
  timeout?: string;
  extra_body?: Record<string, any>;
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
    // If backend is unreachable, give helpful fallback or rethrow
    throw err;
  }
}

/**
 * POST api/conversations {title?}
 */
export async function createConversation(body?: {
  title?: string;
}): Promise<{ id: string } & Partial<Conversation>> {

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

  const res = await fetch(`api/conversations/${encodeURIComponent(conversationId)}/messages`);
  return handleResponse<MessageListResponse>(res);
}

/** DELETE api/conversations/{id}/messages (204) — clears messages, keeps the conversation */
export async function clearConversationMessages(id: string): Promise<void> {
  const res = await fetch(`api/conversations/${encodeURIComponent(id)}/messages`, { method: 'DELETE' });
  return handleResponse<void>(res);
}

/**
 * DELETE api/conversations/{id}
 */
export async function deleteConversation(id: string): Promise<void> {

  const res = await fetch(`api/conversations/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
  return handleResponse<void>(res);
}

/**
 * DELETE api/conversations -> {deleted:n}
 */
export async function deleteAllConversations(): Promise<{ deleted: number }> {

  const res = await fetch('api/conversations', {
    method: 'DELETE',
  });
  return handleResponse<{ deleted: number }>(res);
}

/**
 * GET api/llm/providers
 */
export async function getLLMProviders(): Promise<LLMProvidersResponse> {

  const res = await fetch('api/llm/providers');
  return handleResponse<LLMProvidersResponse>(res);
}

/**
 * POST api/llm/models {provider, base_url?, api_key?}
 */
export async function getLLMModels(body: GetLLMModelsRequest): Promise<LLMModelsResponse> {

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

  const res = await fetch('api/settings');
  return handleResponse<SettingsResponse>(res);
}

/**
 * PUT api/settings/llm
 */
export async function updateLLMSettings(body: UpdateLLMRequest): Promise<void> {

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

  const res = await fetch('api/media/status');
  return handleResponse<MediaStatus>(res);
}

/**
 * GET api/settings/media
 */
export async function getMediaSettings(): Promise<MediaConfig> {

  const res = await fetch('api/settings/media');
  return handleResponse<MediaConfig>(res);
}

/**
 * PUT api/settings/media {folders, extensions?, sync_interval?}
 */
export async function updateMediaSettings(
  body: UpdateMediaSettingsRequest
): Promise<UpdateMediaSettingsResponse> {

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

  const query = path ? `?path=${encodeURIComponent(path)}` : '';
  const res = await fetch(`api/fs/dirs${query}`);
  return handleResponse<FsDirsResponse>(res);
}

/**
 * POST api/fs/validate {path}
 */
export async function validateFsPath(path: string): Promise<ValidateFsPathResponse> {

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

  const res = await fetch('healthz');
  return handleResponse<HealthResponse>(res);
}


/**
 * GET api/settings/schema
 */
export async function getSettingsSchema(): Promise<SettingsSchemaResponse> {

  const res = await fetch('api/settings/schema');
  return handleResponse<SettingsSchemaResponse>(res);
}

/**
 * GET api/settings/{section}
 */
export async function getSectionSettings(section: string): Promise<Record<string, any>> {

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

  const res = await fetch('api/settings/aliases');
  return handleResponse<AliasesSettingsResponse>(res);
}

/**
 * PUT api/settings/aliases
 */
export async function updateAliasesSettings(
  body: AliasesSettingsResponse
): Promise<PutSectionSettingsResponse> {

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

  const res = await fetch(`api/conversations/${encodeURIComponent(id)}/trace`);
  return handleResponse<ConversationTraceResponse>(res);
}

/**
 * GET api/traces?status=&kind=&name=&since=&until=&conversation_id=&limit=100&before_id=
 */
export async function getTraces(
  params?: GetTracesParams
): Promise<TracesListResponse> {

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

  const res = await fetch(`api/traces/${encodeURIComponent(turnId)}/payload`);
  return handleResponse<TracePayloadResponse>(res);
}

/**
 * GET api/media/tracks?q=&artist=&sort=title|artist|path&limit=50&offset=0
 */
export async function getMediaTracks(
  params?: GetMediaTracksParams
): Promise<MediaTracksResponse> {

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

  const res = await fetch('api/media/artists');
  return handleResponse<MediaArtistsResponse>(res);
}

// ---------------- Agent / MCP ----------------
export interface AgentSettings {
  system_prompt: string;
  max_steps: number;
  temperature: number | null;
  turn_timeout: string;
  history_messages: number;
  idle_reset: string;
  context_tokens: number;
  tools_disabled: string[];
  default_system_prompt: string;
  prompt_context?: string;
  available_tools: { name: string; description: string }[];
}
export interface McpServer {
  name: string;
  enabled: boolean;
  transport: 'http' | 'sse' | 'stdio';
  url: string;
  headers: Record<string, string>;
  command: string;
  args: string[];
  env: Record<string, string>;
  timeout: string;
  tools_allow: string[];
}
export interface McpToolInfo { name: string; original: string; description?: string }
export interface McpStatusItem {
  name: string; enabled: boolean; transport: string; connected: boolean; error?: string; tools?: McpToolInfo[]; retrying?: boolean; next_retry?: string;
}
export interface McpTestResult { ok: boolean; error?: string; latency_ms?: number; tools?: McpToolInfo[] }

export async function getAgentSettings(): Promise<AgentSettings> {
  return handleResponse<AgentSettings>(await fetch('api/settings/agent'));
}
export async function getMcpSettings(): Promise<{ servers: McpServer[] }> {
  return handleResponse(await fetch('api/settings/mcp'));
}
export async function getMcpStatus(): Promise<{ servers: McpStatusItem[] }> {
  return handleResponse(await fetch('api/mcp/status'));
}
export async function testMcpServer(server: McpServer): Promise<McpTestResult> {
  return handleResponse(await fetch('api/mcp/test', {
    method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(server),
  }));
}
