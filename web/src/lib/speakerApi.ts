import { ApiError } from './api';

export type AccountState = 'logged_out' | 'logging_in' | 'need_verify' | 'logged_in' | 'cooldown' | 'error';
export interface SpeakerAccount {
  state: AccountState;
  user_id?: string;
  method?: 'password' | 'token';
  hint?: string;
  message?: string;
  cooldown_until?: number | string;
  verify_methods?: ('phone' | 'email')[];
  notification_url?: string;
}
export interface SpeakerDeviceStatus {
  device_id: string;
  name: string;
  hardware?: string;
  enabled?: boolean;
  online?: boolean;
  conversation_id?: string;
  play_mode_effective?: string;
  tts_mode_effective?: string;
}
export interface SpeakerStatus {
  driver?: 'mock' | 'xiaomi';
  account?: SpeakerAccount;
  devices?: SpeakerDeviceStatus[];
  hook?: { running?: boolean; last_ask?: { device?: string; query?: string; time?: string | number; handled?: boolean } };
}
export interface DiscoveredDevice {
  device_id: string; name: string; hardware?: string; miot_did?: string; serial_number?: string;
}
export interface SpeakerTestReq {
  device_id?: string;
  action: 'tts' | 'play_url' | 'pause' | 'play' | 'stop' | 'volume' | 'status';
  text?: string; url?: string; volume?: number;
}
export interface SpeakerTestRes { ok: boolean; note?: string; action?: string; error?: string; elapsed_ms?: number; volume?: number; status?: { status: number; volume: number } }
export interface MockEvent {
  seq: number; ts: number | string; device_id?: string; conversation_id?: string; turn_id?: string;
  kind: string; data?: Record<string, unknown>;
  text?: string; url?: string; volume?: number; latency_ms?: number;
  title?: string; artist?: string; route?: 'direct' | 'artist_random' | 'agent' | 'ignored' | string; handled?: boolean;
  model?: string; duration_ms?: number; error?: string; canceled?: boolean;
  call_id?: string; name?: string; args?: unknown; ok?: boolean; result?: unknown;
}

async function req<T>(url: string, method = 'GET', body?: unknown): Promise<T> {
  const res = await fetch(url, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  if (!res.ok) {
    let msg = `请求失败 (${res.status})`;
    let j: any;
    try { j = await res.json(); msg = j?.error || j?.message || msg; } catch { /* */ }
    const err: any = new ApiError(msg, res.status);
    err.body = j;
    throw err;
  }
  if (res.status === 204) return {} as T;
  return res.json();
}

async function acct(url: string, body: unknown): Promise<SpeakerAccount> {
  try {
    const r = await req<{ account: SpeakerAccount }>(url, 'POST', body);
    return r.account;
  } catch (e: any) {
    if (e?.body?.account) e.account = e.body.account;
    throw e;
  }
}
export const getSpeakerStatus = () => req<SpeakerStatus>('api/speaker/status');
export const xiaomiLogin = (username: string, password: string) =>
  acct('api/speaker/xiaomi/login', { username, password });
export const xiaomiLoginToken = (user_id: string, pass_token: string, device_id?: string) =>
  acct('api/speaker/xiaomi/login-token', { user_id, pass_token, ...(device_id ? { device_id } : {}) });
export const xiaomiSendCode = (method: string) =>
  acct('api/speaker/xiaomi/verify/send', { method });
export const xiaomiVerify = (code: string) =>
  acct('api/speaker/xiaomi/verify', { code });
export const xiaomiRetry = () => acct('api/speaker/xiaomi/retry', {});
export const xiaomiLogout = () => acct('api/speaker/xiaomi/logout', {});
export const discoverDevices = () => req<{ driver?: string; devices: any[] }>('api/speaker/devices/discover');
export const speakerTest = (b: SpeakerTestReq) => req<SpeakerTestRes>('api/speaker/test', 'POST', b);
// Flow events carry details in `data`; flatten them (top-level fields win,
// so latency_ms stays "ms since the ask").
const flattenEvents = (r: { events: MockEvent[]; last_seq?: number }) => ({
  ...r,
  events: (r.events || []).map((e: any) => (e && e.data ? { ...e.data, ...e } : e)),
});
export const getSpeakerEvents = (after: number, conversation_id?: string) =>
  req<{ events: MockEvent[]; last_seq?: number }>(`api/speaker/events?after=${after}${conversation_id ? '&conversation_id=' + encodeURIComponent(conversation_id) : ''}`).then(flattenEvents);
export const mockAsk = (text: string, device_id?: string) =>
  req<{ ok?: boolean; conversation_id?: string }>('api/speaker/mock/ask', 'POST', { text, device_id });
export const getMockEvents = (after: number) =>
  req<{ events: MockEvent[]; last_seq?: number }>(`api/speaker/mock/events?after=${after}`);
