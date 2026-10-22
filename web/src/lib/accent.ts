// Central accent palette: one color per functional area.
// Class names are written out in full so Tailwind can detect them.
export type AccentColor =
  | 'amber' | 'blue' | 'violet' | 'slate' | 'zinc' | 'indigo' | 'cyan'
  | 'teal' | 'orange' | 'rose' | 'gray' | 'sky' | 'green' | 'red';

export interface AccentClasses {
  text: string; // icon color
  chip: string; // tinted chip bg + text
  dot: string; // solid bg (small dots)
  ring: string; // border for round bullets
}

export const PALETTE: Record<AccentColor, AccentClasses> = {
  amber: { text: 'text-amber-600 dark:text-amber-400', chip: 'bg-amber-500/10 text-amber-600 dark:text-amber-400', dot: 'bg-amber-500', ring: 'border-amber-500/50' },
  blue: { text: 'text-blue-600 dark:text-blue-400', chip: 'bg-blue-500/10 text-blue-600 dark:text-blue-400', dot: 'bg-blue-500', ring: 'border-blue-500/50' },
  violet: { text: 'text-violet-600 dark:text-violet-400', chip: 'bg-violet-500/10 text-violet-600 dark:text-violet-400', dot: 'bg-violet-500', ring: 'border-violet-500/50' },
  slate: { text: 'text-slate-600 dark:text-slate-400', chip: 'bg-slate-500/10 text-slate-600 dark:text-slate-400', dot: 'bg-slate-500', ring: 'border-slate-500/50' },
  zinc: { text: 'text-zinc-600 dark:text-zinc-400', chip: 'bg-zinc-500/10 text-zinc-600 dark:text-zinc-400', dot: 'bg-zinc-500', ring: 'border-zinc-500/50' },
  indigo: { text: 'text-indigo-600 dark:text-indigo-400', chip: 'bg-indigo-500/10 text-indigo-600 dark:text-indigo-400', dot: 'bg-indigo-500', ring: 'border-indigo-500/50' },
  cyan: { text: 'text-cyan-600 dark:text-cyan-400', chip: 'bg-cyan-500/10 text-cyan-600 dark:text-cyan-400', dot: 'bg-cyan-500', ring: 'border-cyan-500/50' },
  teal: { text: 'text-teal-600 dark:text-teal-400', chip: 'bg-teal-500/10 text-teal-600 dark:text-teal-400', dot: 'bg-teal-500', ring: 'border-teal-500/50' },
  orange: { text: 'text-orange-600 dark:text-orange-400', chip: 'bg-orange-500/10 text-orange-600 dark:text-orange-400', dot: 'bg-orange-500', ring: 'border-orange-500/50' },
  rose: { text: 'text-rose-600 dark:text-rose-400', chip: 'bg-rose-500/10 text-rose-600 dark:text-rose-400', dot: 'bg-rose-500', ring: 'border-rose-500/50' },
  gray: { text: 'text-gray-600 dark:text-gray-400', chip: 'bg-gray-500/10 text-gray-600 dark:text-gray-400', dot: 'bg-gray-500', ring: 'border-gray-500/50' },
  sky: { text: 'text-sky-600 dark:text-sky-400', chip: 'bg-sky-500/10 text-sky-600 dark:text-sky-400', dot: 'bg-sky-500', ring: 'border-sky-500/50' },
  green: { text: 'text-green-600 dark:text-green-400', chip: 'bg-green-500/10 text-green-600 dark:text-green-400', dot: 'bg-green-500', ring: 'border-green-500/50' },
  red: { text: 'text-red-600 dark:text-red-400', chip: 'bg-red-500/10 text-red-600 dark:text-red-400', dot: 'bg-red-500', ring: 'border-red-500/50' },
};

// Functional areas (top-level nav + settings sections)
export const AREA = {
  guide: 'amber', chat: 'blue', media: 'violet', logs: 'slate', settings: 'zinc',
  llm: 'indigo', search: 'cyan', sources: 'teal', tools: 'orange',
  speaker: 'rose', server: 'gray', logging: 'slate', about: 'sky',
} as const satisfies Record<string, AccentColor>;
export type Area = keyof typeof AREA;

// Timeline steps
export const STEP = {
  detect: 'sky', interrupt: 'amber', llm: 'indigo', tool: 'orange',
  tts: 'green', play: 'violet', error: 'red', native: 'gray',
} as const satisfies Record<string, AccentColor>;

// Status
export const STATUS = { success: 'green', warning: 'amber', error: 'red' } as const satisfies Record<string, AccentColor>;

export const accent = (c: AccentColor | Area): AccentClasses =>
  PALETTE[(c in AREA ? AREA[c as Area] : c) as AccentColor];
