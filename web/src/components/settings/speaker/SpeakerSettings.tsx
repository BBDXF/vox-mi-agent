import { useEffect, useState, useCallback } from 'react';
import { Save, Plus, X, Trash2, Search, FlaskConical, LogOut, ExternalLink, RefreshCw } from 'lucide-react';
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from '@/components/ui/card';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Badge } from '@/components/ui/badge';
import { useToast } from '@/components/ui/toast';
import { getSectionSettings, updateSectionSettings } from '@/lib/api';
import {
  getSpeakerStatus, xiaomiLogin, xiaomiSendCode, xiaomiVerify, xiaomiRetry, xiaomiLogout,
  discoverDevices, speakerTest,
  type SpeakerStatus, type SpeakerAccount, type SpeakerTestRes,
} from '@/lib/speakerApi';

const selectCls = 'h-9 rounded-md border border-input bg-background px-2 text-sm focus:outline-none focus:ring-2 focus:ring-ring';
const helpCls = 'text-xs text-muted-foreground mt-1';

function Field({ label, help, error, children }: { label: string; help?: string; error?: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1">
      <label className="text-xs font-medium text-foreground">{label}</label>
      {children}
      {help && <p className={helpCls}>{help}</p>}
      {error && <p className="text-xs text-destructive">{error}</p>}
    </div>
  );
}

const STATE_META: Record<string, { label: string; variant: any }> = {
  logged_out: { label: '未登录', variant: 'secondary' },
  logging_in: { label: '登录中…', variant: 'warning' },
  need_verify: { label: '需要验证', variant: 'warning' },
  logged_in: { label: '已登录', variant: 'success' },
  cooldown: { label: '冷却中', variant: 'warning' },
  error: { label: '出错', variant: 'destructive' },
};

function AccountCard({ account, onChange }: { account?: SpeakerAccount; onChange: (a: SpeakerAccount) => void }) {
  const { error: toastError } = useToast();
  const [user, setUser] = useState('');
  const [pwd, setPwd] = useState('');
  const [method, setMethod] = useState<string>('');
  const [code, setCode] = useState('');
  const [busy, setBusy] = useState(false);
  const [now, setNow] = useState(Date.now());
  const state = account?.state || 'logged_out';
  const meta = STATE_META[state] || STATE_META.error;

  useEffect(() => {
    if (state !== 'cooldown') return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [state]);

  const run = async (fn: () => Promise<SpeakerAccount>) => {
    setBusy(true);
    try { const a = await fn(); if (a?.state) onChange(a); } catch (e: any) { if (e.account?.state) onChange(e.account); toastError(e.message || '操作失败'); } finally { setBusy(false); }
  };

  let remain = 0;
  if (account?.cooldown_until) {
    const v = account.cooldown_until;
    const ms = typeof v === 'number' ? (v < 1e12 ? v * 1000 : v) : Date.parse(v);
    remain = Math.max(0, Math.ceil((ms - now) / 1000));
  }
  const methods = account?.verify_methods?.length ? account.verify_methods : (['phone', 'email'] as const);
  const mLabel = (m: string) => (m === 'phone' ? '手机' : '邮箱');

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between gap-2">
          <CardTitle>小米账号</CardTitle>
          <Badge variant={meta.variant}>{meta.label}</Badge>
        </div>
        <CardDescription>登录后才能发现并控制小米音箱。</CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        {account?.message && <p className="text-xs text-muted-foreground">{account.message}</p>}

        {(state === 'logged_out' || state === 'error' || state === 'logging_in') && (
          <form className="grid sm:grid-cols-2 gap-3" onSubmit={(e) => { e.preventDefault(); run(() => xiaomiLogin(user.trim(), pwd)); }}>
            <Field label="账号" help="手机号 / 邮箱 / 小米 ID">
              <Input value={user} onChange={(e) => setUser(e.target.value)} autoComplete="username" />
            </Field>
            <Field label="密码" help="密码仅用于登录，不保存">
              <Input type="password" value={pwd} onChange={(e) => setPwd(e.target.value)} autoComplete="current-password" />
            </Field>
            <div className="sm:col-span-2">
              <Button type="submit" size="sm" disabled={busy || !user || !pwd || state === 'logging_in'}>登录</Button>
            </div>
          </form>
        )}

        {state === 'need_verify' && (
          <div className="space-y-3">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs text-muted-foreground">验证方式</span>
              <div className="inline-flex rounded-md border overflow-hidden">
                {methods.map((m) => (
                  <button key={m} type="button" onClick={() => setMethod(m)}
                    className={`px-3 py-1 text-xs ${(method || methods[0]) === m ? 'bg-primary text-primary-foreground' : 'hover:bg-muted'}`}>
                    {mLabel(m)}
                  </button>
                ))}
              </div>
              <Button size="sm" variant="outline" disabled={busy} onClick={() => run(() => xiaomiSendCode(method || methods[0]))}>发送验证码</Button>
            </div>
            <div className="flex gap-2 max-w-sm">
              <Input placeholder="验证码" value={code} onChange={(e) => setCode(e.target.value)} />
              <Button size="sm" disabled={busy || !code} onClick={() => run(() => xiaomiVerify(code.trim()))}>提交</Button>
            </div>
            {account?.notification_url && (
              <div className="flex flex-wrap items-center gap-3 text-xs">
                <a href={account.notification_url} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-primary hover:underline">
                  <ExternalLink className="w-3 h-3" />在浏览器中完成验证
                </a>
                <Button size="sm" variant="outline" disabled={busy} onClick={() => run(xiaomiRetry)}>我已完成，重试</Button>
              </div>
            )}
          </div>
        )}

        {state === 'cooldown' && (
          <div className="flex items-center gap-3 text-sm">
            <span>请 <b className="font-mono">{remain}</b> 秒后再试</span>
            <Button size="sm" variant="outline" disabled={busy || remain > 0} onClick={() => run(xiaomiRetry)}>重试</Button>
          </div>
        )}

        {state === 'logged_in' && (
          <div className="flex items-center justify-between gap-3">
            <span className="text-sm">用户 ID：<span className="font-mono">{account?.user_id || '-'}</span></span>
            <Button size="sm" variant="outline" className="gap-1" disabled={busy} onClick={() => run(xiaomiLogout)}>
              <LogOut className="w-3.5 h-3.5" />退出登录
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function TestPanel({ deviceId, defVolume }: { deviceId: string; defVolume: number }) {
  const [text, setText] = useState('你好，我是你的音乐助手');
  const [url, setUrl] = useState('');
  const [vol, setVol] = useState(defVolume || 30);
  const [res, setRes] = useState<SpeakerTestRes | null>(null);
  const [busy, setBusy] = useState(false);
  const go = async (action: any, extra: any = {}) => {
    setBusy(true);
    try { setRes(await speakerTest({ device_id: deviceId, action, ...extra })); }
    catch (e: any) { setRes({ ok: false, error: e.message }); }
    finally { setBusy(false); }
  };
  return (
    <div className="rounded-lg border bg-muted/30 p-3 space-y-2 text-xs">
      <div className="flex gap-2"><Input value={text} onChange={(e) => setText(e.target.value)} placeholder="播报文字" />
        <Button size="sm" disabled={busy || !text} onClick={() => go('tts', { text })}>播报</Button></div>
      <div className="flex gap-2"><Input value={url} onChange={(e) => setUrl(e.target.value)} placeholder="播放 URL" />
        <Button size="sm" disabled={busy || !url} onClick={() => go('play_url', { url })}>播放</Button></div>
      <div className="flex flex-wrap items-center gap-2">
        <Button size="sm" variant="outline" disabled={busy} onClick={() => go('pause')}>暂停</Button>
        <Button size="sm" variant="outline" disabled={busy} onClick={() => go('play')}>继续</Button>
        <Button size="sm" variant="outline" disabled={busy} onClick={() => go('stop')}>停止</Button>
        <Button size="sm" variant="outline" disabled={busy} onClick={() => go('status')}>状态</Button>
        <span className="ml-2 text-muted-foreground">音量 {vol}</span>
        <input type="range" min={0} max={100} value={vol} onChange={(e) => setVol(+e.target.value)} onMouseUp={() => go('volume', { volume: vol })} onTouchEnd={() => go('volume', { volume: vol })} className="w-32" />
      </div>
      {res && (
        <div className={`rounded border p-2 font-mono break-all ${res.ok ? 'border-emerald-500/30' : 'border-destructive/40 text-destructive'}`}>
          {res.ok ? '成功' : '失败'}{res.elapsed_ms != null && ` · ${res.elapsed_ms}ms`}
          {res.error && <div>{res.error}</div>}
          {res.status && <div className="text-muted-foreground">状态：{['空闲', '播放中', '已暂停', '已停止'][res.status.status] ?? res.status.status} · 音量 {res.status.volume}</div>}
          {res.ok && res.volume != null && <div className="text-muted-foreground">音量已设为 {res.volume}</div>}
        </div>
      )}
    </div>
  );
}

function Chips({ label, help, items, onChange, error }: { label: string; help?: string; items: string[]; onChange: (x: string[]) => void; error?: string }) {
  const [t, setT] = useState('');
  const add = () => { const k = t.trim(); if (k && !items.includes(k)) onChange([...items, k]); setT(''); };
  return (
    <Field label={label} help={help} error={error}>
      <div className="flex flex-wrap gap-1.5">
        {items.map((k, i) => (
          <Badge key={k} variant="secondary" className="gap-1">{k}
            <button type="button" onClick={() => onChange(items.filter((_, j) => j !== i))}><X className="w-3 h-3" /></button>
          </Badge>
        ))}
      </div>
      <div className="flex gap-2 max-w-sm mt-1">
        <Input value={t} onChange={(e) => setT(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); add(); } }} placeholder="输入后回车" />
        <Button size="sm" variant="outline" onClick={add}>添加</Button>
      </div>
    </Field>
  );
}

export function SpeakerSettings() {
  const { success, error: toastError } = useToast();
  const [v, setV] = useState<Record<string, any> | null>(null);
  const [errs, setErrs] = useState<Record<string, string>>({});
  const [status, setStatus] = useState<SpeakerStatus | null>(null);
  const [saving, setSaving] = useState(false);
  const [found, setFound] = useState<any[] | null>(null);
  const [finding, setFinding] = useState(false);
  const [testing, setTesting] = useState<string | null>(null);

  const loadStatus = useCallback(() => getSpeakerStatus().then(setStatus).catch(() => {}), []);
  useEffect(() => {
    getSectionSettings('speaker').then(setV).catch((e) => toastError(e.message || '加载音箱配置失败'));
    loadStatus();
    const t = setInterval(loadStatus, 5000);
    return () => clearInterval(t);
  }, [loadStatus]);

  if (!v) return <div className="text-sm text-muted-foreground py-12 text-center">正在加载…</div>;

  const set = (k: string, val: any) => { setV((p) => ({ ...(p || {}), [k]: val })); setErrs((p) => { const c = { ...p }; delete c[k]; return c; }); };
  const hook = v.hook || {};
  const setHook = (k: string, val: any) => set('hook', { ...hook, [k]: val });
  const devices: any[] = v.xiaomi?.devices || [];
  const setDevices = (d: any[]) => set('xiaomi', { ...(v.xiaomi || {}), devices: d });
  const patchDev = (i: number, p: any) => setDevices(devices.map((d, j) => (j === i ? { ...d, ...p } : d)));
  const keywords: string[] = hook.keywords || [];
  const driver = v.driver || 'mock';
  const eff = (id: string) => status?.devices?.find((d) => d.device_id === id);
  const E = (k: string) => errs[k] || errs[k.replace(/\.\d+/g, '')];
  const num = (x: string) => (x === '' ? '' : Number(x));

  const save = async () => {
    setSaving(true); setErrs({});
    try {
      const r = await updateSectionSettings('speaker', v);
      success((r as any)?.restart_required ? '已保存，部分设置需重启生效' : '音箱配置已保存');
      loadStatus();
    } catch (e: any) {
      if (e.fields) setErrs(e.fields);
      toastError(e.message || '保存失败');
    } finally { setSaving(false); }
  };

  const discover = async () => {
    setFinding(true);
    try { setFound((await discoverDevices()).devices || []); } catch (e: any) { toastError(e.message || '发现设备失败'); } finally { setFinding(false); }
  };
  const addDev = (d: any) => {
    if (devices.some((x) => x.device_id === d.device_id)) return;
    setDevices([...devices, { device_id: d.device_id, name: d.name, hardware: d.hardware || '', enabled: d.enabled ?? true, play_mode: d.play_mode || 'auto', tts_mode: d.tts_mode || 'auto', miot_tts: d.miot_tts || '' }]);
  };
  const last = status?.hook?.last_ask;

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader><CardTitle>驱动</CardTitle><CardDescription>选择音箱后端：模拟用于调试，小米用于真实设备。</CardDescription></CardHeader>
        <CardContent className="grid sm:grid-cols-2 gap-4">
          <Field label="驱动" error={E('driver')}>
            <select className={selectCls + ' w-full'} value={driver} onChange={(e) => set('driver', e.target.value)}>
              <option value="mock">模拟音箱</option><option value="log">仅日志</option><option value="xiaomi">小米音箱</option>
            </select>
          </Field>
          <Field label="对外地址" error={E('public_base_url')} help="音箱需要能访问到的 NAS 局域网地址，例如 http://192.168.1.10:8081；Docker 默认网络下自动探测的地址通常不可用">
            <Input value={v.public_base_url ?? ''} onChange={(e) => set('public_base_url', e.target.value)} placeholder="http://192.168.1.10:8081" />
          </Field>
          <Field label="链接有效期（如 1h，最小 1m）" error={E('url_ttl')}>
            <Input value={v.url_ttl ?? ''} placeholder="1h" onChange={(e) => set('url_ttl', e.target.value)} />
          </Field>
          <Field label="默认音量（0-100，0 = 不调整音量）" error={E('volume_default')}>
            <Input type="number" min={0} max={100} value={v.volume_default ?? ''} onChange={(e) => set('volume_default', num(e.target.value))} />
          </Field>
        </CardContent>
      </Card>

      {driver === 'mock' && (
        <Card>
          <CardHeader><CardTitle>模拟音箱选项</CardTitle><CardDescription>用于调试：人为增加延迟或随机失败。</CardDescription></CardHeader>
          <CardContent className="grid sm:grid-cols-2 gap-4">
            <Field label="每次调用延迟（毫秒，0–60000）" error={E('mock.latency_ms')}>
              <Input type="number" value={v.mock?.latency_ms ?? 0} onChange={(e) => set('mock', { ...(v.mock || {}), latency_ms: num(e.target.value) })} />
            </Field>
            <Field label="随机失败概率（0–1）" error={E('mock.fail_rate')}>
              <Input type="number" step="0.05" value={v.mock?.fail_rate ?? 0} onChange={(e) => set('mock', { ...(v.mock || {}), fail_rate: num(e.target.value) })} />
            </Field>
          </CardContent>
        </Card>
      )}

      {driver === 'xiaomi' && <AccountCard account={status?.account} onChange={(a) => setStatus((s) => ({ ...(s || {}), account: a }))} />}

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-2">
            <CardTitle>设备</CardTitle>
            <Button size="sm" variant="outline" className="gap-1" onClick={discover} disabled={finding}>
              {finding ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <Search className="w-3.5 h-3.5" />}发现设备
            </Button>
          </div>
          <CardDescription>添加后记得点击底部「保存」。</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {found && (
            <div className="rounded-lg border p-3 space-y-2">
              <div className="text-xs font-medium">发现 {found.length} 台设备</div>
              {found.map((d) => {
                const added = devices.some((x) => x.device_id === d.device_id);
                return (
                  <div key={d.device_id} className="flex items-center justify-between gap-2 text-xs">
                    <div className="min-w-0"><span className="font-medium">{d.name}</span> <span className="text-muted-foreground font-mono">{d.hardware} · {d.device_id}</span></div>
                    <Button size="sm" variant="outline" disabled={added} onClick={() => addDev(d)} className="gap-1 shrink-0"><Plus className="w-3 h-3" />{added ? '已添加' : '添加'}</Button>
                  </div>
                );
              })}
            </div>
          )}
          {E('xiaomi.devices') && <p className="text-xs text-destructive">{E('xiaomi.devices')}</p>}
          {devices.length === 0 ? (
            <p className="text-xs text-muted-foreground py-4 text-center">尚未配置设备</p>
          ) : (
            <div className="space-y-3">
              {devices.map((d, i) => {
                const st = eff(d.device_id);
                return (
                  <div key={d.device_id + i} className="rounded-lg border p-3 space-y-3">
                    <div className="flex flex-wrap items-center gap-3">
                      <Input className="w-40 h-8" value={d.name || ''} onChange={(e) => patchDev(i, { name: e.target.value })} />
                      <span className="text-xs text-muted-foreground font-mono">{d.hardware || '-'}</span>
                      {st?.online != null && <Badge variant={st.online ? 'success' : 'secondary'}>{st.online ? '在线' : '离线'}</Badge>}
                      <label className="ml-auto flex items-center gap-2 text-xs cursor-pointer">
                        <input type="checkbox" role="switch" checked={!!d.enabled} onChange={(e) => patchDev(i, { enabled: e.target.checked })} className="accent-primary w-4 h-4" />启用
                      </label>
                    </div>
                    <div className="flex flex-wrap items-end gap-4 text-xs">
                      <div>
                        <div className="mb-1">播放方式</div>
                        <select className={selectCls} value={d.play_mode || 'auto'} onChange={(e) => patchDev(i, { play_mode: e.target.value })}>
                          <option value="auto">自动</option><option value="url">URL</option><option value="music">音乐接口</option>
                        </select>
                        {st?.play_mode_effective && <div className="text-muted-foreground mt-1">实际：{st.play_mode_effective}</div>}
                      </div>
                      <div>
                        <div className="mb-1">播报方式</div>
                        <select className={selectCls} value={d.tts_mode || 'auto'} onChange={(e) => patchDev(i, { tts_mode: e.target.value })}>
                          <option value="auto">自动</option><option value="mina">MiNA</option><option value="miot">MIoT</option>
                        </select>
                        {st?.tts_mode_effective && <div className="text-muted-foreground mt-1">实际：{st.tts_mode_effective}</div>}
                      </div>
                      {d.tts_mode === 'miot' && (
                        <div>
                          <div className="mb-1">MIoT siid-aiid</div>
                          <Input className="w-28 h-9" placeholder="5-1" value={d.miot_tts || ''} onChange={(e) => patchDev(i, { miot_tts: e.target.value })} />
                        </div>
                      )}
                      <div className="ml-auto flex gap-2">
                        <Button size="sm" variant="outline" className="gap-1" onClick={() => setTesting(testing === d.device_id ? null : d.device_id)}><FlaskConical className="w-3.5 h-3.5" />测试</Button>
                        <Button size="sm" variant="outline" className="gap-1 text-destructive" onClick={() => setDevices(devices.filter((_, j) => j !== i))}><Trash2 className="w-3.5 h-3.5" />删除</Button>
                      </div>
                    </div>
                    {Object.entries(errs).filter(([k]) => k.startsWith(`xiaomi.devices[${i}]`)).map(([k, m]) => (
                      <p key={k} className="text-xs text-destructive">{k.split('.').pop()}：{m}</p>
                    ))}
                    {testing === d.device_id && <TestPanel deviceId={d.device_id} defVolume={v.volume_default} />}
                  </div>
                );
              })}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader><CardTitle>对话接管</CardTitle><CardDescription>监听音箱的提问，按规则交给本 Agent 回答。</CardDescription></CardHeader>
        <CardContent className="space-y-4">
          <div className="inline-flex rounded-md border overflow-hidden">
            {[['music', '音乐点播'], ['keyword', '关键词触发'], ['all', '全部接管'], ['off', '关闭']].map(([k, l]) => (
              <button key={k} type="button" onClick={() => setHook('mode', k)}
                className={`px-3 py-1.5 text-xs ${(hook.mode || 'music') === k ? 'bg-primary text-primary-foreground' : 'hover:bg-muted'}`}>{l}{k === 'music' && '（默认）'}</button>
            ))}
          </div>
          {(hook.mode || 'music') === 'music' && <p className="text-xs text-muted-foreground">只接管「播放/来一首/我想听…」等点歌请求，暂停、下一首、天气等仍由小爱处理。</p>}
          <Chips label="唤醒词" help="识别前会去掉（默认 小爱同学 / 小爱）" items={hook.wake_words || []} onChange={(x) => setHook('wake_words', x)} error={E('hook.wake_words')} />
          {(hook.mode || 'music') === 'music' && (
            <>
              <Chips label="点歌触发词" items={hook.music_triggers || []} onChange={(x) => setHook('music_triggers', x)} error={E('hook.music_triggers')} />
              <Chips label="排除词" help="包含这些词的提问不会被当作点歌，例如 放假、听说" items={hook.music_exclude || []} onChange={(x) => setHook('music_exclude', x)} error={E('hook.music_exclude')} />
              <Field label="直接播放阈值（0-100）" help="搜索结果唯一且得分不低于此值时不经大模型直接播放，更快" error={E('hook.direct_play_score')}>
                <Input className="max-w-[10rem]" type="number" min={0} max={100} value={hook.direct_play_score ?? ''} onChange={(e) => setHook('direct_play_score', num(e.target.value))} />
              </Field>
            </>
          )}
          {hook.mode === 'keyword' && (
            <Chips label="关键词" help="以这些词开头的提问交给本 Agent，其余仍由小爱回答（默认 问问）" items={keywords} onChange={(x) => setHook('keywords', x)} error={E('hook.keywords')} />
          )}
          <div className="grid sm:grid-cols-2 gap-4">
            <Field label="轮询间隔（500ms–5s）" error={E('hook.poll_interval')}>
              <Input value={hook.poll_interval ?? ''} placeholder="1s" onChange={(e) => setHook('poll_interval', e.target.value)} />
            </Field>
            <Field label="打断窗口（0–30s）" error={E('hook.interrupt_window')}>
              <Input value={hook.interrupt_window ?? ''} placeholder="3s" onChange={(e) => setHook('interrupt_window', e.target.value)} />
            </Field>
          </div>
          <div className="text-xs text-muted-foreground flex flex-wrap items-center gap-2">
            <Badge variant={status?.hook?.running ? 'success' : 'secondary'}>{status?.hook?.running ? '运行中' : '未运行'}</Badge>
            {last?.query ? <span>最近提问：「{last.query}」{last.device && ` · ${last.device}`} · {last.handled ? '已接管' : '未接管'}</span> : <span>暂无提问记录</span>}
          </div>
        </CardContent>
        <CardFooter className="justify-end">
          <Button onClick={save} disabled={saving} className="gap-2"><Save className="w-4 h-4" />{saving ? '保存中…' : '保存音箱配置'}</Button>
        </CardFooter>
      </Card>
    </div>
  );
}
