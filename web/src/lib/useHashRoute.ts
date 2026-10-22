import { useCallback, useEffect, useState } from 'react';

export type Tab = 'guide' | 'chat' | 'settings' | 'media' | 'logs';
export interface Route { tab: Tab; param?: string }

const TABS: Tab[] = ['guide', 'chat', 'settings', 'media', 'logs'];

export function parseHash(hash: string): Route {
  const parts = hash.replace(/^#\/?/, '').split('/').filter(Boolean);
  const tab = parts[0] as Tab;
  if (!TABS.includes(tab)) return { tab: 'chat' };
  const rest = parts.slice(1).join('/');
  return { tab, param: rest ? decodeURIComponent(rest) : undefined };
}

export function buildHash(tab: Tab, param?: string): string {
  return '#/' + tab + (param ? '/' + encodeURIComponent(param) : '');
}

/** Tiny hash router: every navigate() pushes a history entry (no duplicates). */
export function useHashRoute() {
  const [route, setRoute] = useState<Route>(() => parseHash(window.location.hash));
  useEffect(() => {
    const on = () => setRoute(parseHash(window.location.hash));
    window.addEventListener('hashchange', on);
    return () => window.removeEventListener('hashchange', on);
  }, []);
  const navigate = useCallback((tab: Tab, param?: string) => {
    const h = buildHash(tab, param);
    if (window.location.hash !== h) window.location.hash = h;
  }, []);
  return { route, navigate };
}
