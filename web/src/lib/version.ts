export const REPO_URL = 'https://github.com/BBDXF/vox-mi-agent';
export const RELEASES_URL = REPO_URL + '/releases';

export interface VersionInfo {
  version: string;
  commit?: string;
  build_date?: string;
  go_version?: string;
  os?: string;
  arch?: string;
  started_at?: string;
  uptime_s?: number;
  tools?: Record<string, string>;
  data_dir?: string;
}

/** GET api/version; tolerates 404 / errors → {version:'dev'} */
export async function getVersion(): Promise<VersionInfo> {
  try {
    const res = await fetch('api/version');
    if (!res.ok) return { version: 'dev' };
    const v = await res.json();
    return { ...v, version: v.version || 'dev' };
  } catch {
    return { version: 'dev' };
  }
}

export function humanizeUptime(s?: number): string {
  if (s == null) return '-';
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
  const parts: string[] = [];
  if (d) parts.push(`${d} 天`);
  if (h) parts.push(`${h} 小时`);
  if (m || !parts.length) parts.push(`${m} 分钟`);
  return parts.join(' ');
}
