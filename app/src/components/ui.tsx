// The shared UI language.
//
// Everything here is built from four ideas: a warm canvas, white cards floating
// on it, pill-shaped controls, and the route glyph (components/RouteGlyph) that
// gives every trip its own recognisable mark. See src/design/tokens.ts for why
// the palette is what it is.

import {
  ActivityIndicator,
  Image,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  useWindowDimensions,
  View,
} from 'react-native';
import Svg, { Circle, Path } from 'react-native-svg';
import { useEffect, useState, type ReactNode } from 'react';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { colors, initials, lift, radius, space, tileColor, type } from '../design/tokens';
import { photoSource } from '../api/images';
import { breakpoint, columnWidth, type Breakpoint, type ColumnKind } from '../design/layout';

export { colors };

// The app draws edge to edge (`edgeToEdgeEnabled` in gradle.properties), so a
// screen with no navigation header above it starts underneath the Android
// status bar. The tab bar already reads insets.bottom; topInset is the same
// debt at the other end.
//
// It is opt-in rather than automatic because most screens here sit under a
// header, and react-navigation has already moved their content clear of the
// status bar — adding the inset there would pad twice. The screens that ask for
// it are the ones whose layout sets headerShown: false, which is the tabs and
// the sign-in flow.
export function Screen({
  children,
  scroll,
  padded = true,
  topInset = false,
  column = 'normal',
}: {
  children: ReactNode;
  scroll?: boolean;
  padded?: boolean;
  topInset?: boolean;
  /**
   * How wide the content may grow. On a phone all three are the phone; in a
   * browser a form stops at a readable width instead of spanning the window.
   */
  column?: ColumnKind;
}) {
  const insets = useSafeAreaInsets();
  const { width } = useWindowDimensions();
  const roomy = breakpoint(width) !== 'compact';
  return (
    <View style={styles.screen}>
      <Backdrop />
      <View
        style={[
          styles.column,
          { maxWidth: columnWidth(column, width) },
          padded && styles.screenPadded,
          padded && roomy && styles.screenRoomy,
          topInset && { paddingTop: insets.top + (padded ? (roomy ? space.xl : space.lg) : 0) },
        ]}
      >
        {children}
      </View>
    </View>
  );
}

/**
 * Faint roads across the top of the canvas.
 *
 * The same idea as the route glyph, used as texture: a few lines that bend the
 * way the roads here do, too pale to read as anything but paper. Behind the
 * content and out of the way of touches.
 */
function Backdrop() {
  // Drawn in pixels at the window's width rather than scaled from a viewBox:
  // scaling stretched the strokes into blobs on a wide window.
  const { width } = useWindowDimensions();
  const h = 200;
  const x = (f: number) => (f * width).toFixed(1);
  return (
    <View pointerEvents="none" style={styles.backdrop}>
      <Svg width={width} height={h}>
        <Path
          d={`M${x(-0.05)} 150 C ${x(0.15)} 118, ${x(0.28)} 172, ${x(0.45)} 128 S ${x(0.75)} 58, ${x(1.05)} 92`}
          stroke={colors.primary}
          strokeOpacity={0.06}
          strokeWidth={14}
          strokeLinecap="round"
          fill="none"
        />
        <Path
          d={`M${x(-0.05)} 56 C ${x(0.2)} 86, ${x(0.35)} 22, ${x(0.58)} 48 S ${x(0.86)} 132, ${x(1.05)} 104`}
          stroke={colors.primary}
          strokeOpacity={0.08}
          strokeWidth={2}
          strokeDasharray="2 10"
          strokeLinecap="round"
          fill="none"
        />
      </Svg>
    </View>
  );
}

/**
 * Pushes what follows to the bottom of a phone screen, where a thumb reaches
 * it. On anything wider the bottom of the window is nowhere near the form, so
 * it is only a gap.
 */
export function Spacer() {
  const roomy = useBreakpoint() !== 'compact';
  return <View style={roomy ? styles.spacerRoomy : styles.spacer} />;
}

/**
 * The name and what it is for, over the sign-in. The first thing somebody sees
 * should say what they are signing in to.
 */
export function BrandMark({ tagline }: { tagline: string }) {
  return (
    <View style={styles.brand}>
      <View style={styles.brandGlyph}>
        <Svg width={28} height={28} viewBox="0 0 28 28">
          <Path
            d="M5 21 C 9 12, 14 20, 18 11 S 23 6, 23 6"
            stroke={colors.primaryInk}
            strokeWidth={2.5}
            strokeLinecap="round"
            fill="none"
          />
          <Circle cx={5} cy={21} r={2.5} fill={colors.primaryInk} />
          <Circle cx={23} cy={6} r={3} fill={colors.accent} />
        </Svg>
      </View>
      <View>
        <Text style={styles.brandName}>carpool</Text>
        <Text style={styles.brandTagline}>{tagline}</Text>
      </View>
    </View>
  );
}

/** Which breakpoint the window is at, for screens that lay out differently. */
export function useBreakpoint(): Breakpoint {
  return breakpoint(useWindowDimensions().width);
}

/** The big line at the top of a screen. */
export function Title({ children }: { children: ReactNode }) {
  return <Text style={styles.display}>{children}</Text>;
}

/** A quieter heading over a list, optionally with an action on the right. */
export function SectionHeader({ title, action }: { title: string; action?: ReactNode }) {
  return (
    <View style={styles.sectionHeader}>
      <Text style={styles.sectionTitle}>{title}</Text>
      {action}
    </View>
  );
}

export function Hint({ children, testID }: { children: ReactNode; testID?: string }) {
  return (
    <Text testID={testID} style={styles.hint}>
      {children}
    </Text>
  );
}

/** The floating white surface everything else sits in. */
export function Card({
  children,
  onPress,
  testID,
  tone = 'plain',
}: {
  children: ReactNode;
  onPress?: () => void;
  testID?: string;
  tone?: 'plain' | 'accent';
}) {
  const body = (
    <View style={[styles.card, tone === 'accent' && styles.cardAccent]}>{children}</View>
  );
  if (!onPress) {
    return testID ? <View testID={testID}>{body}</View> : body;
  }
  return (
    <Pressable
      testID={testID}
      accessibilityRole="button"
      onPress={onPress}
      style={({ pressed }) => (pressed ? styles.pressed : undefined)}
    >
      {body}
    </Pressable>
  );
}

export function Button({
  label,
  onPress,
  disabled,
  variant = 'primary',
  testID,
}: {
  label: string;
  onPress: () => void;
  disabled?: boolean;
  variant?: 'primary' | 'secondary' | 'danger';
  testID?: string;
}) {
  return (
    <Pressable
      testID={testID}
      accessibilityRole="button"
      aria-disabled={!!disabled}
      onPress={onPress}
      disabled={disabled}
      style={({ pressed }) => [
        styles.button,
        variant === 'secondary' && styles.buttonSecondary,
        variant === 'danger' && styles.buttonDanger,
        disabled && styles.buttonDisabled,
        pressed && styles.pressed,
      ]}
    >
      <Text style={[styles.buttonLabel, variant === 'secondary' && styles.buttonLabelSecondary]}>
        {label}
      </Text>
    </Pressable>
  );
}

/** A small pill. `tone="accent"` is for anything that wants an answer. */
export function Badge({
  label,
  tone = 'quiet',
  testID,
}: {
  label: string;
  tone?: 'quiet' | 'accent' | 'primary' | 'success' | 'danger';
  testID?: string;
}) {
  return (
    <View
      testID={testID}
      style={[
        styles.badge,
        tone === 'accent' && styles.badgeAccent,
        tone === 'primary' && styles.badgePrimary,
        tone === 'success' && styles.badgeSuccess,
        tone === 'danger' && styles.badgeDanger,
      ]}
    >
      <Text
        style={[
          styles.badgeLabel,
          tone === 'accent' && styles.badgeLabelAccent,
          tone === 'primary' && styles.badgeLabelPrimary,
          tone === 'success' && styles.badgeLabelSuccess,
          tone === 'danger' && styles.badgeLabelDanger,
        ]}
      >
        {label}
      </Text>
    </View>
  );
}

/**
 * Seats as pips rather than "3/4".
 *
 * How full a car is decides whether you bother tapping, so it should read
 * without being parsed. Filled pips are taken seats; the last free one turns
 * amber, which is the only warning the list needs.
 */
export function SeatPips({ free, total, testID }: { free: number; total: number; testID?: string }) {
  const capped = Math.max(0, Math.min(total, 8));
  const taken = Math.max(0, capped - Math.max(0, free));
  return (
    <View testID={testID} style={styles.pips} accessibilityLabel={`${free}/${total}`}>
      {Array.from({ length: capped }, (_, i) => (
        <View
          key={i}
          style={[
            styles.pip,
            i < taken && styles.pipTaken,
            free === 1 && i === capped - 1 && styles.pipLast,
          ]}
        />
      ))}
    </View>
  );
}

/** A circular tile carrying an emoji or a letter, coloured from its label. */
export function IconTile({ label, glyph, size = 44 }: { label: string; glyph: string; size?: number }) {
  return (
    <View
      style={[
        styles.tile,
        { width: size, height: size, borderRadius: size / 2, backgroundColor: tileColor(label) },
      ]}
    >
      <Text style={{ fontSize: size * 0.42 }}>{glyph}</Text>
    </View>
  );
}

export function Avatar({
  name,
  photoUrl,
  size = 36,
}: {
  name: string;
  photoUrl?: string | null;
  size?: number;
}) {
  const shape = { width: size, height: size, borderRadius: size / 2 };

  // Photos live behind the session, so the bytes are fetched rather than handed
  // to <Image> as a URL. Until they arrive, and if they never do, the initials
  // stand in: a face that fails to load should look like no face, not like a
  // broken image.
  const [uri, setUri] = useState<string | null>(null);
  useEffect(() => {
    let alive = true;
    setUri(null);
    void photoSource(photoUrl).then((u) => {
      if (alive) setUri(u);
    });
    return () => {
      alive = false;
    };
  }, [photoUrl]);

  if (uri) {
    return <Image source={{ uri }} style={[styles.avatar, shape]} />;
  }
  return (
    <View style={[styles.avatar, shape, { backgroundColor: tileColor(name) }]}>
      <Text style={[styles.avatarInitials, { fontSize: size * 0.36 }]}>{initials(name)}</Text>
    </View>
  );
}

/** Overlapping avatars, with a count when there are more than fit. */
export function AvatarStack({
  people,
  max = 3,
}: {
  people: { name: string; photo_url?: string | null }[];
  max?: number;
}) {
  const shown = people.slice(0, max);
  const rest = people.length - shown.length;
  return (
    <View style={styles.stack}>
      {shown.map((p, i) => (
        <View key={`${p.name}-${i}`} style={i > 0 ? styles.stacked : undefined}>
          <Avatar name={p.name} photoUrl={p.photo_url} size={28} />
        </View>
      ))}
      {rest > 0 ? (
        <View style={[styles.stacked, styles.stackRest]}>
          <Text style={styles.stackRestLabel}>+{rest}</Text>
        </View>
      ) : null}
    </View>
  );
}

/**
 * The origin → destination spine.
 *
 * Two stops joined by a line, which is the one thing every screen in this app is
 * really about. Used wherever there is no room for the glyph.
 */
export function RouteSpine({
  from,
  to,
  fromNote,
  toNote,
}: {
  from: string;
  to: string;
  fromNote?: string;
  toNote?: string;
}) {
  return (
    <View style={styles.spine}>
      <View style={styles.spineRail}>
        <View style={styles.spineDot} />
        <View style={styles.spineLine} />
        <View style={[styles.spineDot, styles.spineDotEnd]} />
      </View>
      <View style={styles.spineStops}>
        <View>
          <Text style={styles.spineStop}>{from}</Text>
          {fromNote ? <Text style={styles.spineNote}>{fromNote}</Text> : null}
        </View>
        <View>
          <Text style={styles.spineStop}>{to}</Text>
          {toNote ? <Text style={styles.spineNote}>{toNote}</Text> : null}
        </View>
      </View>
    </View>
  );
}

/** A label/value row, the building block of the settings-style cards. */
export function Row({
  label,
  value,
  onPress,
  testID,
}: {
  label: string;
  value?: ReactNode;
  onPress?: () => void;
  testID?: string;
}) {
  const content = (
    <View style={styles.row}>
      <Text style={styles.rowLabel}>{label}</Text>
      {typeof value === 'string' ? <Text style={styles.rowValue}>{value}</Text> : value}
    </View>
  );
  if (!onPress) return testID ? <View testID={testID}>{content}</View> : content;
  return (
    <Pressable testID={testID} accessibilityRole="button" onPress={onPress}>
      {content}
    </Pressable>
  );
}

/** A labelled input. The label sits inside the field, as on the reference. */
export function Field({
  label,
  value,
  onChangeText,
  placeholder,
  testID,
  keyboardType,
  autoCapitalize,
  valid,
  compact,
  multiline,
}: {
  label: string;
  value: string;
  onChangeText: (v: string) => void;
  placeholder?: string;
  testID?: string;
  keyboardType?: 'default' | 'number-pad' | 'phone-pad';
  autoCapitalize?: 'none' | 'sentences';
  /** Draws the pine underline once the value parses. */
  valid?: boolean;
  compact?: boolean;
  /** For a few sentences rather than a name. */
  multiline?: boolean;
}) {
  return (
    <View style={[styles.field, compact && styles.fieldCompact, valid && styles.fieldValid]}>
      <Text style={styles.fieldLabel}>{label}</Text>
      <TextInput
        testID={testID}
        style={[styles.fieldInput, multiline && styles.fieldMultiline]}
        value={value}
        onChangeText={onChangeText}
        placeholder={placeholder}
        placeholderTextColor={colors.muted}
        keyboardType={keyboardType}
        autoCapitalize={autoCapitalize}
        multiline={multiline}
        textAlignVertical={multiline ? 'top' : undefined}
      />
    </View>
  );
}

/** Seats and the like, as a control rather than a number pad. */
export function Stepper({
  value,
  onChange,
  min = 1,
  max = 6,
  step = 1,
  testID,
}: {
  value: number;
  onChange: (v: number) => void;
  min?: number;
  max?: number;
  /** How far one press moves. Kilometres are not chosen one at a time. */
  step?: number;
  testID?: string;
}) {
  return (
    <View testID={testID} style={styles.stepper}>
      <Pressable
        testID={testID ? `${testID}-minus` : undefined}
        accessibilityRole="button"
        accessibilityLabel="−"
        onPress={() => onChange(Math.max(min, value - step))}
        style={styles.stepperButton}
      >
        <Text style={styles.stepperGlyph}>−</Text>
      </Pressable>
      <Text style={styles.stepperValue}>{value}</Text>
      <Pressable
        testID={testID ? `${testID}-plus` : undefined}
        accessibilityRole="button"
        accessibilityLabel="+"
        onPress={() => onChange(Math.min(max, value + step))}
        style={styles.stepperButton}
      >
        <Text style={styles.stepperGlyph}>+</Text>
      </Pressable>
    </View>
  );
}

/** A selectable pill, used for weekdays. */
export function Chip({
  label,
  glyph,
  selected,
  onPress,
  testID,
  fit,
  disabled,
}: {
  label: string;
  /** Sized to the label rather than an equal share of the row, for words too
      long to squeeze: the row then wraps instead of cutting them. */
  fit?: boolean;
  /** A pictogram shown before the label. Decoration over a word that already
      says it, so it is hidden from a screen reader rather than read out under
      whatever name the platform happens to give the character. */
  glyph?: string;
  selected: boolean;
  onPress: () => void;
  testID?: string;
  /** Shut while whatever it sets is still being saved. */
  disabled?: boolean;
}) {
  return (
    <Pressable
      testID={testID}
      accessibilityRole="button"
      accessibilityLabel={label}
      aria-selected={selected}
      aria-disabled={!!disabled}
      disabled={disabled}
      onPress={onPress}
      style={[
        styles.chip,
        fit && styles.chipFit,
        selected && styles.chipSelected,
        disabled && styles.buttonDisabled,
      ]}
    >
      {glyph ? (
        <Text
          testID={testID ? `${testID}-glyph` : undefined}
          accessibilityElementsHidden
          importantForAccessibility="no"
          style={styles.chipGlyph}
        >
          {glyph}
        </Text>
      ) : null}
      <Text style={[styles.chipLabel, selected && styles.chipLabelSelected]}>{label}</Text>
    </Pressable>
  );
}

/** One place to render the loading / error / empty triad every list needs. */
export function AsyncState({
  loading,
  error,
  onRetry,
  empty,
  emptyLabel,
  emptySlot,
  children,
}: {
  loading: boolean;
  error: unknown;
  onRetry?: () => void;
  empty?: boolean;
  emptyLabel?: string;
  /** Shown instead of `emptyLabel` when there is something to offer instead. */
  emptySlot?: ReactNode;
  children: ReactNode;
}) {
  if (loading) return <ActivityIndicator testID="loading" color={colors.primary} style={styles.centered} />;

  if (error) {
    return (
      <View style={styles.centered}>
        <Text testID="error" style={styles.error}>
          {error instanceof Error ? error.message : String(error)}
        </Text>
        {onRetry ? <Button label="Δοκίμασε ξανά" onPress={onRetry} variant="secondary" /> : null}
      </View>
    );
  }

  if (empty) {
    // An empty screen is the first thing most people will see, so it is the
    // one place worth handing a caller the whole space rather than a sentence.
    if (emptySlot) return <>{emptySlot}</>;
    return (
      <View style={styles.centered}>
        <Text testID="empty" style={styles.hint}>
          {emptyLabel}
        </Text>
      </View>
    );
  }

  return <>{children}</>;
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.canvas },
  column: { flex: 1, width: '100%', alignSelf: 'center', gap: space.md },
  screenPadded: { paddingHorizontal: space.lg, paddingTop: space.lg },
  screenRoomy: { paddingHorizontal: space.xl },
  backdrop: { position: 'absolute', top: 0, left: 0, right: 0, height: 200 },
  spacer: { flex: 1 },
  spacerRoomy: { height: space.xl },
  brand: { flexDirection: 'row', alignItems: 'center', gap: space.md, marginBottom: space.md },
  brandGlyph: {
    width: 48,
    height: 48,
    borderRadius: radius.inner,
    backgroundColor: colors.primary,
    alignItems: 'center',
    justifyContent: 'center',
    ...lift,
  },
  brandName: { ...type.title, color: colors.primary, letterSpacing: -0.2 },
  brandTagline: { ...type.caption, color: colors.muted },

  display: { ...type.display, color: colors.ink },
  sectionHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginTop: space.sm,
  },
  sectionTitle: { ...type.title, color: colors.ink },
  hint: { ...type.body, color: colors.muted, lineHeight: 22 },
  error: { ...type.body, color: colors.danger, textAlign: 'center' },
  centered: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: space.md, padding: space.xl },

  card: {
    backgroundColor: colors.surface,
    borderRadius: radius.card,
    padding: space.lg,
    gap: space.md,
    ...lift,
  },
  cardAccent: { backgroundColor: colors.accentSoft },
  pressed: { opacity: 0.85, transform: [{ scale: 0.995 }] },

  button: {
    backgroundColor: colors.primary,
    paddingVertical: space.lg,
    paddingHorizontal: space.xl,
    borderRadius: radius.pill,
    alignItems: 'center',
  },
  buttonSecondary: { backgroundColor: colors.sunken },
  buttonDanger: { backgroundColor: colors.danger },
  buttonDisabled: { opacity: 0.35 },
  buttonLabel: { ...type.label, fontSize: 16, color: colors.primaryInk },
  buttonLabelSecondary: { color: colors.ink },

  badge: {
    paddingHorizontal: space.md,
    paddingVertical: 5,
    borderRadius: radius.pill,
    backgroundColor: colors.sunken,
    alignSelf: 'flex-start',
  },
  badgeAccent: { backgroundColor: colors.accentSoft },
  badgePrimary: { backgroundColor: colors.primary },
  badgeLabel: { ...type.caption, color: colors.muted },
  badgeLabelAccent: { color: '#8A4A12' },
  badgeLabelPrimary: { color: colors.primaryInk },
  badgeSuccess: { backgroundColor: '#E1EEE5' },
  badgeLabelSuccess: { color: colors.success },
  badgeDanger: { backgroundColor: '#F6E0DB' },
  badgeLabelDanger: { color: colors.danger },

  pips: { flexDirection: 'row', gap: 5, alignItems: 'center' },
  pip: { width: 8, height: 8, borderRadius: 4, backgroundColor: colors.line },
  pipTaken: { backgroundColor: colors.primary },
  pipLast: { backgroundColor: colors.accent },

  tile: { alignItems: 'center', justifyContent: 'center' },

  avatar: { alignItems: 'center', justifyContent: 'center', backgroundColor: colors.sunken },
  avatarInitials: { ...type.label, color: colors.ink },
  stack: { flexDirection: 'row', alignItems: 'center' },
  stacked: { marginLeft: -10 },
  stackRest: {
    width: 28,
    height: 28,
    borderRadius: 14,
    backgroundColor: colors.sunken,
    alignItems: 'center',
    justifyContent: 'center',
  },
  stackRestLabel: { ...type.caption, fontSize: 11, color: colors.muted },

  spine: { flexDirection: 'row', gap: space.md },
  spineRail: { alignItems: 'center', paddingVertical: 6 },
  spineDot: {
    width: 10,
    height: 10,
    borderRadius: 5,
    borderWidth: 2.5,
    borderColor: colors.primary,
  },
  spineDotEnd: { backgroundColor: colors.primary },
  spineLine: { flex: 1, width: 2, minHeight: 26, backgroundColor: colors.line },
  spineStops: { flex: 1, justifyContent: 'space-between', gap: space.lg },
  spineStop: { ...type.body, color: colors.ink },
  spineNote: { ...type.caption, color: colors.muted, marginTop: 2 },

  row: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingVertical: space.md,
    gap: space.md,
  },
  rowLabel: { ...type.body, color: colors.ink, flexShrink: 1 },
  rowValue: { ...type.label, color: colors.primary },

  field: {
    backgroundColor: colors.sunken,
    borderRadius: radius.inner,
    paddingHorizontal: space.lg,
    paddingVertical: space.md,
    borderBottomWidth: 2,
    borderBottomColor: 'transparent',
  },
  fieldCompact: { flex: 1 },
  fieldValid: { borderBottomColor: colors.primary },
  fieldMultiline: { minHeight: 88 },
  fieldLabel: { ...type.caption, fontSize: 12, color: colors.muted },
  fieldInput: { ...type.body, color: colors.ink, paddingVertical: 4, minHeight: 30 },

  stepper: {
    flexDirection: 'row',
    alignItems: 'center',
    backgroundColor: colors.sunken,
    borderRadius: radius.pill,
    padding: 4,
    gap: space.sm,
  },
  stepperButton: {
    width: 34,
    height: 34,
    borderRadius: 17,
    backgroundColor: colors.surface,
    alignItems: 'center',
    justifyContent: 'center',
  },
  stepperGlyph: { ...type.title, color: colors.ink, lineHeight: 24 },
  stepperValue: { ...type.label, fontSize: 16, color: colors.ink, minWidth: 18, textAlign: 'center' },

  chip: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    paddingVertical: space.md,
    // A long Greek word wraps inside the chip instead of touching its edges;
    // «Σαββατοκύριακο» did at the width of an ordinary phone.
    paddingHorizontal: space.xs,
    borderRadius: radius.inner,
    backgroundColor: colors.sunken,
  },
  chipFit: { flexGrow: 1, flexBasis: 'auto', paddingHorizontal: space.md },
  chipGlyph: { fontSize: 20, lineHeight: 24 },
  chipSelected: { backgroundColor: colors.primary },
  chipLabel: { ...type.label, color: colors.muted, textAlign: 'center' },
  chipLabelSelected: { color: colors.primaryInk },
});
