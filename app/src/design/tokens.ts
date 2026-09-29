// Design tokens.
//
// The palette is deliberately not the indigo-on-cool-grey of every ride-hailing
// app. This is a rural cost-sharing app for Thrace: warm paper, deep pine for
// anything you press, amber for anything that needs answering. The structure —
// a soft canvas with floating white cards and a pill navigation bar — is the
// part worth borrowing from modern carpool UI; the colour is ours.

export const colors = {
  /** Warm paper. Every screen sits on this, never on white. */
  canvas: '#F2F0EA',
  /** Cards float above the canvas. */
  surface: '#FFFFFF',
  /** A second surface for rows inside a card. */
  sunken: '#F7F5F0',

  ink: '#14171C',
  muted: '#6B6F76',
  /** Hairlines only; never a border around a whole card. */
  line: '#E4E0D6',

  /** Pressable things, the active tab, the route stroke. */
  primary: '#2C4A3B',
  primaryInk: '#F6F5F1',
  /** Something needs you: an ask waiting, seats running out. */
  accent: '#E4873B',
  accentSoft: '#FBEAD8',
  /** Text on accentSoft. The accent itself is too light to read there. */
  accentInk: '#8A4B17',

  danger: '#B3402E',
  success: '#3F7A57',

  /** Icon tiles. Assigned by hashing a label, so a place keeps its colour. */
  tiles: ['#DCE6DA', '#F0E3D0', '#DDE5EE', '#F2DCD5', '#E7E2D3'],
} as const;

export const radius = {
  card: 24,
  inner: 16,
  pill: 999,
} as const;

export const space = {
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  xl: 24,
  xxl: 32,
} as const;

export const type = {
  display: { fontSize: 28, fontWeight: '700', letterSpacing: -0.5 },
  title: { fontSize: 20, fontWeight: '700', letterSpacing: -0.3 },
  body: { fontSize: 16, fontWeight: '500' },
  label: { fontSize: 14, fontWeight: '600' },
  caption: { fontSize: 13, fontWeight: '500' },
  mono: { fontSize: 13, fontWeight: '600', letterSpacing: 0.4 },
} as const;

/**
 * One soft shadow, used only to lift a card off the canvas. Android reads
 * `elevation` and ignores the rest.
 */
export const lift = {
  shadowColor: '#2B2A24',
  shadowOpacity: 0.07,
  shadowRadius: 16,
  shadowOffset: { width: 0, height: 6 },
  elevation: 2,
} as const;

/**
 * Picks a stable tile colour for a label. The same place keeps the same colour
 * across screens and restarts, which is what makes a list scannable.
 */
export function tileColor(seed: string): string {
  let hash = 0;
  for (let i = 0; i < seed.length; i += 1) hash = (hash * 31 + seed.charCodeAt(i)) >>> 0;
  return colors.tiles[hash % colors.tiles.length];
}

/** Up to two letters, for an avatar with no photo. Greek names keep their case. */
export function initials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (!words.length) return '·';
  return words
    .slice(0, 2)
    .map((w) => [...w][0]?.toLocaleUpperCase('el-GR') ?? '')
    .join('');
}
