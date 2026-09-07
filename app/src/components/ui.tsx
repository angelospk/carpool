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
  View,
} from 'react-native';
import { useEffect, useState, type ReactNode } from 'react';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { colors, initials, lift, radius, space, tileColor, type } from '../design/tokens';
import { photoSource } from '../api/images';

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
}: {
  children: ReactNode;
  scroll?: boolean;
  padded?: boolean;
  topInset?: boolean;
}) {
  const insets = useSafeAreaInsets();
  return (
    <View
      style={[
        styles.screen,
        padded && styles.screenPadded,
        topInset && { paddingTop: insets.top + (padded ? space.lg : 0) },
      ]}
    >
      {children}
    </View>
  );
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

export function Hint({ children }: { children: ReactNode }) {
  return <Text style={styles.hint}>{children}</Text>;
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
  tone?: 'quiet' | 'accent' | 'primary';
  testID?: string;
}) {
  return (
    <View
      testID={testID}
      style={[
        styles.badge,
        tone === 'accent' && styles.badgeAccent,
        tone === 'primary' && styles.badgePrimary,
      ]}
    >
      <Text
        style={[
          styles.badgeLabel,
          tone === 'accent' && styles.badgeLabelAccent,
          tone === 'primary' && styles.badgeLabelPrimary,
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
}: {
  label: string;
  /** A pictogram shown before the label. Decoration over a word that already
      says it, so it is hidden from a screen reader rather than read out under
      whatever name the platform happens to give the character. */
  glyph?: string;
  selected: boolean;
  onPress: () => void;
  testID?: string;
}) {
  return (
    <Pressable
      testID={testID}
      accessibilityRole="button"
      accessibilityLabel={label}
      aria-selected={selected}
      onPress={onPress}
      style={[styles.chip, selected && styles.chipSelected]}
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
  screen: { flex: 1, backgroundColor: colors.canvas, gap: space.md },
  screenPadded: { paddingHorizontal: space.lg, paddingTop: space.lg },

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
    paddingVertical: space.md,
    borderRadius: radius.inner,
    backgroundColor: colors.sunken,
  },
  chipGlyph: { fontSize: 20, lineHeight: 24 },
  chipSelected: { backgroundColor: colors.primary },
  chipLabel: { ...type.label, color: colors.muted },
  chipLabelSelected: { color: colors.primaryInk },
});
