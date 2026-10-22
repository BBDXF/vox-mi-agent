import type { ComponentType } from 'react';
import { accent, type Area, type AccentColor } from '@/lib/accent';
import { cn } from '@/lib/utils';

interface IconChipProps {
  icon: ComponentType<{ className?: string }>;
  color: AccentColor | Area;
  className?: string;
  iconClassName?: string;
}

/** Soft tinted icon chip used for page/card headers. */
export function IconChip({ icon: Icon, color, className, iconClassName }: IconChipProps) {
  return (
    <span className={cn('inline-flex items-center justify-center rounded-md p-1.5 shrink-0', accent(color).chip, className)}>
      <Icon className={cn('w-4 h-4', iconClassName)} />
    </span>
  );
}
