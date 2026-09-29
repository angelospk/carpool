// How wide things are.
//
// The app was drawn for a phone and ran unchanged in a browser, so on a laptop
// every card stretched to the whole window: a 1370px form, a trip card with its
// time on one edge and its price on the other. Content now sits in a column
// capped by what it holds, and the screens with enough to show use the room for
// a second column instead of for stretching.

export type Breakpoint = 'compact' | 'medium' | 'wide';
export type ColumnKind = 'narrow' | 'normal' | 'wide';

export function breakpoint(width: number): Breakpoint {
  if (width >= 1024) return 'wide';
  if (width >= 600) return 'medium';
  return 'compact';
}

/** Sign-in fits in a phone's width; a form reads best at a book's; a dashboard gets the rest. */
const caps: Record<ColumnKind, number> = { narrow: 480, normal: 680, wide: 1120 };

export function columnWidth(kind: ColumnKind, windowWidth: number): number {
  return Math.min(caps[kind], windowWidth);
}
