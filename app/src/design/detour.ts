import type { TFunction } from 'i18next';

import { decimal } from './format';

/**
 * The detour a driver is asked to make, in words.
 *
 * A passenger already on the driver's road costs nothing, and "+0 λεπτά,
 * +0.0 χλμ" read as a figure that failed to load rather than as good news.
 */
export function detourText(minutes: number, km: number, t: TFunction): string {
  const m = Math.round(minutes);
  if (m === 0 && km < 0.05) return t('ask.noDetour');
  // "+0 λεπτά" beside a real distance reads as no detour at all, and count
  // picks «λεπτό» for one.
  return t('ask.detour', { count: Math.max(1, m), km: decimal(km, 1) });
}
