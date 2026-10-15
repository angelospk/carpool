// Design tokens: "Mono Ticket".
//
// Black type on white paper, like a printed timetable. Every ride is a ticket.
// The only colour is a lime stamp on the fare somebody has chosen, plus a small
// dot of it inside the screen's main button. Depth comes from borders, never
// from shadows. The full system, with the reasons, is DESIGN.md in the design
// library (ui-vault, mono-ticket).

export const colors = {
  /** Everything that matters: text, selected borders, primary buttons, avatars. */
  ink: '#000000',
  /** The screen and the card. */
  paper: '#FFFFFF',
  /** Segmented-control tracks, disabled fills, skeletons, rows inside a card. */
  surface: '#F2F2F2',
  /** Resting borders and hairlines. */
  line: '#D9D9D9',
  /** Secondary text. 5.3:1 on paper. */
  muted: '#6B6B6B',
  /**
   * Lime. Only the fare on the selected ticket and the dot in the primary
   * button. Never text on white, never a section background.
   */
  accent: '#C8F53C',
  onAccent: '#000000',
  /** Errors and destructive actions. Not decoration. */
  danger: '#C4161C',
} as const;

export const radius = {
  chip: 10,
  control: 12,
  button: 14,
  card: 16,
  full: 9999,
} as const;

export const space = {
  xs: 4,
  sm: 8,
  md: 12,
  lg: 16,
  gutter: 20,
  xl: 24,
  xxl: 32,
} as const;

/** 1.5px at rest, 3px when selected or focused. */
export const border = {
  rest: 1.5,
  selected: 3,
} as const;

/** The smallest thing a thumb can be asked to hit, and the main button's height. */
export const size = {
  tap: 44,
  cta: 56,
} as const;

/**
 * Commissioner, one file per weight: React Native cannot pick a weight out of
 * one family, so a role names its file and never sets fontWeight.
 */
export const fonts = {
  black: 'Commissioner_900Black',
  extraBold: 'Commissioner_800ExtraBold',
  semiBold: 'Commissioner_600SemiBold',
  regular: 'Commissioner_400Regular',
} as const;

export const type = {
  /** The big line at the top of a screen. */
  display: { fontFamily: fonts.black, fontSize: 32, lineHeight: 35, letterSpacing: -0.6 },
  /** Departure and arrival on a ticket: the largest text on a ride. */
  time: { fontFamily: fonts.black, fontSize: 26, lineHeight: 28, letterSpacing: -0.5 },
  title: { fontFamily: fonts.extraBold, fontSize: 22, lineHeight: 27 },
  heading: { fontFamily: fonts.extraBold, fontSize: 17, lineHeight: 22 },
  body: { fontFamily: fonts.regular, fontSize: 16, lineHeight: 23 },
  label: { fontFamily: fonts.semiBold, fontSize: 15, lineHeight: 20 },
  caption: { fontFamily: fonts.regular, fontSize: 13, lineHeight: 18 },
  button: { fontFamily: fonts.extraBold, fontSize: 16, lineHeight: 20 },
} as const;

/** Up to two letters, for an avatar with no photo. Greek names keep their case. */
export function initials(name: string): string {
  const words = name.trim().split(/\s+/).filter(Boolean);
  if (!words.length) return '·';
  return words
    .slice(0, 2)
    .map((w) => [...w][0]?.toLocaleUpperCase('el-GR') ?? '')
    .join('');
}
