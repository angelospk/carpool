// The shared UI language.
//
// "Mono Ticket": black type on white paper, outlined cards with no shadows,
// selection drawn as a thick black border, one black button per screen. The
// ride itself is a ticket (components/Ticket). See src/design/tokens.ts for the
// palette and why it has one colour.

import {
  AccessibilityInfo,
  Animated,
  Easing,
  Image,
  Platform,
  Pressable,
  StyleSheet,
  Text,
  TextInput,
  useWindowDimensions,
  View,
  type DimensionValue,
} from 'react-native';
import Svg, { Circle, Path } from 'react-native-svg';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { border, colors, initials, radius, size as sizes, space, type } from '../design/tokens';
import { Icon, type IconName } from './Icon';
import { photoSource } from '../api/images';
import { breakpoint, columnWidth, type Breakpoint, type ColumnKind } from '../design/layout';

export { colors };

const AnimatedPressable = Animated.createAnimatedComponent(Pressable);
export type { IconName };

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
      <View
        style={[
          styles.column,
          { maxWidth: columnWidth(column, width) },
          padded && styles.screenPadded,
          padded && roomy && styles.screenRoomy,
          topInset && { paddingTop: insets.top + (padded ? (roomy ? space.xl : space.lg) : 0) },
          // The bottom is not opt-in: no header moves content off the
          // navigation bar, and a screen's last button is usually its action.
          { paddingBottom: insets.bottom + (padded ? space.md : 0) },
        ]}
      >
        {children}
      </View>
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
            stroke={colors.paper}
            strokeWidth={2.5}
            strokeLinecap="round"
            fill="none"
          />
          <Circle cx={5} cy={21} r={2.5} fill={colors.paper} />
          <Circle cx={23} cy={6} r={3} fill={colors.paper} />
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

/**
 * Press feedback: a 0.98 scale over 120ms, on a timing curve. No spring, no
 * bounce, and nothing at all when the system asks for reduced motion.
 */
export function usePressScale() {
  const scale = useRef(new Animated.Value(1)).current;
  const [still, setStill] = useState(false);
  useEffect(() => {
    let alive = true;
    void AccessibilityInfo.isReduceMotionEnabled?.()
      .then((on) => alive && setStill(!!on))
      .catch(() => undefined);
    return () => {
      alive = false;
    };
  }, []);
  const to = (value: number) =>
    still
      ? undefined
      : Animated.timing(scale, {
          toValue: value,
          duration: 120,
          easing: Easing.out(Easing.quad),
          useNativeDriver: Platform.OS !== 'web',
        }).start();
  return {
    style: { transform: [{ scale }] },
    onPressIn: () => to(0.98),
    onPressOut: () => to(1),
  };
}

/** One fade-in per screen at most: 200ms, opacity only. */
export function FadeIn({ children, style }: { children: ReactNode; style?: object }) {
  const opacity = useRef(new Animated.Value(0)).current;
  useEffect(() => {
    Animated.timing(opacity, {
      toValue: 1,
      duration: 200,
      useNativeDriver: Platform.OS !== 'web',
    }).start();
  }, [opacity]);
  return <Animated.View style={[{ opacity }, style]}>{children}</Animated.View>;
}

/**
 * The outlined white surface everything else sits in. `tone="accent"` is for
 * something that wants an answer, and is drawn with the selected border.
 */
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
  const press = usePressScale();
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
      onPressIn={press.onPressIn}
      onPressOut={press.onPressOut}
    >
      <Animated.View style={press.style}>{body}</Animated.View>
    </Pressable>
  );
}

/**
 * Primary is ink with paper text, secondary paper with an ink outline, danger
 * paper with a red outline. `quietDanger` is a red word on a grey outline, for
 * a destructive action that is not the point of the screen. `dot` puts the
 * lime dot in front of the label: the one main action on a screen gets it.
 */
export function Button({
  label,
  onPress,
  disabled,
  variant = 'primary',
  testID,
  icon,
  dot,
}: {
  label: string;
  onPress: () => void;
  disabled?: boolean;
  variant?: 'primary' | 'secondary' | 'danger' | 'quietDanger';
  testID?: string;
  icon?: IconName;
  dot?: boolean;
}) {
  const press = usePressScale();
  const tint =
    variant === 'primary'
      ? colors.paper
      : variant === 'danger' || variant === 'quietDanger'
        ? colors.danger
        : colors.ink;
  return (
    <AnimatedPressable
      testID={testID}
      accessibilityRole="button"
      aria-disabled={!!disabled}
      onPress={onPress}
      onPressIn={press.onPressIn}
      onPressOut={press.onPressOut}
      disabled={disabled}
      style={[
        styles.button,
        variant === 'secondary' && styles.buttonSecondary,
        variant === 'danger' && styles.buttonDanger,
        variant === 'quietDanger' && styles.buttonQuietDanger,
        disabled && (variant === 'primary' ? styles.buttonDisabled : styles.buttonDisabledOutline),
        press.style,
      ]}
    >
      {dot && variant === 'primary' && !disabled ? <View style={styles.buttonDot} /> : null}
      {icon ? <Icon name={icon} size={20} color={disabled && variant === 'primary' ? colors.paper : tint} /> : null}
      <Text
        numberOfLines={2}
        style={[styles.buttonLabel, { color: tint }, disabled && variant !== 'primary' && styles.textDisabled]}
      >
        {label}
      </Text>
    </AnimatedPressable>
  );
}

/**
 * A small outlined tag. `tone="accent"` is for anything that wants an answer;
 * it is drawn in ink, because the one colour here belongs to the fare.
 */
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
        tone === 'success' && styles.badgeAccent,
        tone === 'danger' && styles.badgeDanger,
      ]}
    >
      {tone === 'success' ? <Icon name="checkmark" size={14} color={colors.ink} /> : null}
      <Text
        style={[
          styles.badgeLabel,
          (tone === 'accent' || tone === 'success') && styles.badgeLabelAccent,
          tone === 'primary' && styles.badgeLabelPrimary,
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
 * without being parsed. Filled pips are taken seats; the last free one is a
 * thick ring, which is the only warning the list needs.
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
    <View style={[styles.avatar, shape]}>
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
  /** Draws the border in ink once the value parses. */
  valid?: boolean;
  compact?: boolean;
  /** For a few sentences rather than a name. */
  multiline?: boolean;
}) {
  const [focused, setFocused] = useState(false);
  return (
    <View
      style={[
        styles.field,
        compact && styles.fieldCompact,
        valid && styles.fieldValid,
        focused && styles.fieldFocused,
      ]}
    >
      <Text style={styles.fieldLabel}>{label}</Text>
      <TextInput
        testID={testID}
        style={[styles.fieldInput, multiline && styles.fieldMultiline]}
        value={value}
        onChangeText={onChangeText}
        onFocus={() => setFocused(true)}
        onBlur={() => setFocused(false)}
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
        <Icon name="remove" size={20} color={colors.ink} />
      </Pressable>
      <Text style={styles.stepperValue}>{value}</Text>
      <Pressable
        testID={testID ? `${testID}-plus` : undefined}
        accessibilityRole="button"
        accessibilityLabel="+"
        onPress={() => onChange(Math.min(max, value + step))}
        style={styles.stepperButton}
      >
        <Icon name="add" size={20} color={colors.ink} />
      </Pressable>
    </View>
  );
}

/** A selectable outlined chip, used for weekdays. Selected is the 3px ink border. */
export function Chip({
  label,
  icon,
  selected,
  onPress,
  testID,
  fit,
  minWidth,
  disabled,
}: {
  label: string;
  /** The least share of the row this chip takes, e.g. '30%' for three to a
      row. The row then wraps into an even grid instead of a ragged one. */
  minWidth?: DimensionValue;
  /** Sized to the label rather than an equal share of the row, for words too
      long to squeeze: the row then wraps instead of cutting them. */
  fit?: boolean;
  /** An icon shown before the label. Decoration over a word that already
      says it, so it is hidden from a screen reader rather than read out under
      whatever name the platform happens to give it. */
  icon?: IconName;
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
        minWidth !== undefined && { minWidth, flexGrow: 1, flexBasis: 'auto' },
        selected && styles.chipSelected,
        disabled && styles.buttonDisabled,
      ]}
    >
      {icon ? (
        <Icon
          testID={testID ? `${testID}-glyph` : undefined}
          hidden
          name={icon}
          size={20}
          color={selected ? colors.ink : colors.muted}
        />
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
  // Skeleton blocks in the shape of a list, not a spinner over the screen.
  if (loading) return <Skeleton testID="loading" />;

  if (error) {
    return (
      <View style={styles.centered}>
        <Icon name="alert-circle-outline" size={32} color={colors.ink} />
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
        <Icon name="file-tray-outline" size={32} color={colors.ink} />
        <Text testID="empty" style={styles.hint}>
          {emptyLabel}
        </Text>
      </View>
    );
  }

  return <>{children}</>;
}

/** Grey blocks with the shape of the content, while it loads. */
export function Skeleton({ rows = 3, testID }: { rows?: number; testID?: string }) {
  return (
    <View testID={testID} style={styles.skeleton} accessibilityLabel="…">
      {Array.from({ length: rows }, (_, i) => (
        <View key={i} style={styles.skeletonBlock} />
      ))}
    </View>
  );
}

/**
 * Two to four options in a grey track; the chosen one is paper with an ink
 * outline. For sorting and for tabs inside a screen.
 */
export function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
  testID,
}: {
  options: readonly { value: T; label: string }[];
  value: T;
  onChange: (v: T) => void;
  testID?: string;
}) {
  return (
    <View testID={testID} style={styles.segTrack} accessibilityRole="tablist">
      {options.map((o) => {
        const on = o.value === value;
        return (
          <Pressable
            key={o.value}
            testID={testID ? `${testID}-${o.value}` : undefined}
            accessibilityRole="tab"
            aria-selected={on}
            onPress={() => onChange(o.value)}
            style={[styles.seg, on && styles.segOn]}
          >
            <Text numberOfLines={1} style={[styles.segLabel, on && styles.segLabelOn]}>
              {o.label}
            </Text>
          </Pressable>
        );
      })}
    </View>
  );
}

/**
 * The bottom of a flow screen: a paper strip with a hairline over it, holding
 * the one black button. It sits in the layout under a scrolling body rather
 * than floating over it, so it never covers the last field.
 */
export function BottomBar({ children }: { children: ReactNode }) {
  const insets = useSafeAreaInsets();
  return (
    <View style={[styles.bottomBar, { paddingBottom: Math.max(insets.bottom, space.md) }]}>
      {children}
    </View>
  );
}

/** A round 44pt icon button with a hairline ring. Icon-only, so it must be labelled. */
export function IconButton({
  name,
  label,
  onPress,
  testID,
}: {
  name: IconName;
  label: string;
  onPress: () => void;
  testID?: string;
}) {
  return (
    <Pressable
      testID={testID}
      accessibilityRole="button"
      accessibilityLabel={label}
      onPress={onPress}
      style={({ pressed }) => [styles.iconButton, pressed && styles.pressed]}
    >
      <Icon name={name} size={22} color={colors.ink} />
    </Pressable>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: colors.paper },
  column: { flex: 1, width: '100%', alignSelf: 'center', gap: space.md },
  screenPadded: { paddingHorizontal: space.gutter, paddingTop: space.lg },
  screenRoomy: { paddingHorizontal: space.xl },
  spacer: { flex: 1 },
  spacerRoomy: { height: space.xl },
  brand: { flexDirection: 'row', alignItems: 'center', gap: space.md, marginBottom: space.md },
  brandGlyph: {
    width: 48,
    height: 48,
    borderRadius: radius.button,
    backgroundColor: colors.ink,
    alignItems: 'center',
    justifyContent: 'center',
  },
  brandName: { ...type.title, color: colors.ink },
  brandTagline: { ...type.caption, color: colors.muted },

  display: { ...type.display, color: colors.ink },
  sectionHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    marginTop: space.sm,
  },
  sectionTitle: { ...type.title, color: colors.ink },
  hint: { ...type.body, color: colors.muted },
  error: { ...type.body, color: colors.danger, textAlign: 'center' },
  centered: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: space.md, padding: space.xl },

  card: {
    backgroundColor: colors.paper,
    borderRadius: radius.card,
    borderWidth: border.rest,
    borderColor: colors.line,
    padding: space.lg,
    gap: space.md,
  },
  // The border grows inward by the padding it takes, so the content does not move.
  cardAccent: {
    borderWidth: border.selected,
    borderColor: colors.ink,
    padding: space.lg - (border.selected - border.rest),
  },
  pressed: { opacity: 0.7 },

  button: {
    minHeight: sizes.cta,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: space.sm,
    backgroundColor: colors.ink,
    paddingVertical: space.sm,
    paddingHorizontal: space.xl,
    borderRadius: radius.button,
    borderWidth: border.rest,
    borderColor: colors.ink,
  },
  buttonSecondary: { backgroundColor: colors.paper },
  buttonDanger: { backgroundColor: colors.paper, borderColor: colors.danger },
  buttonQuietDanger: { backgroundColor: colors.paper, borderColor: colors.line },
  // Grey, not faded: a see-through black button reads as a pressed one.
  buttonDisabled: { backgroundColor: '#9E9E9E', borderColor: '#9E9E9E' },
  buttonDisabledOutline: { borderColor: colors.line },
  textDisabled: { color: colors.muted },
  buttonDot: { width: 8, height: 8, borderRadius: 4, backgroundColor: colors.accent },
  buttonLabel: { ...type.button, color: colors.paper, textAlign: 'center', flexShrink: 1 },

  badge: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.xs,
    paddingHorizontal: 10,
    paddingVertical: 4,
    borderRadius: radius.chip,
    borderWidth: border.rest,
    borderColor: colors.line,
    backgroundColor: colors.paper,
    alignSelf: 'flex-start',
  },
  badgeAccent: { borderColor: colors.ink },
  badgePrimary: { backgroundColor: colors.ink, borderColor: colors.ink },
  badgeDanger: { borderColor: colors.danger },
  badgeLabel: { ...type.label, fontSize: 13, lineHeight: 17, color: colors.muted },
  badgeLabelAccent: { color: colors.ink },
  badgeLabelPrimary: { color: colors.paper },
  badgeLabelDanger: { color: colors.danger },

  pips: { flexDirection: 'row', gap: 5, alignItems: 'center' },
  pip: { width: 10, height: 10, borderRadius: 5, borderWidth: border.rest, borderColor: colors.ink },
  pipTaken: { backgroundColor: colors.ink },
  pipLast: { borderWidth: border.selected },

  avatar: { alignItems: 'center', justifyContent: 'center', backgroundColor: colors.ink },
  avatarInitials: { ...type.button, color: colors.paper },
  stack: { flexDirection: 'row', alignItems: 'center' },
  stacked: { marginLeft: -10 },
  stackRest: {
    width: 28,
    height: 28,
    borderRadius: 14,
    backgroundColor: colors.surface,
    borderWidth: border.rest,
    borderColor: colors.paper,
    alignItems: 'center',
    justifyContent: 'center',
  },
  stackRestLabel: { ...type.caption, fontSize: 11, color: colors.ink },

  spine: { flexDirection: 'row', gap: space.md },
  spineRail: { alignItems: 'center', paddingVertical: 6 },
  spineDot: {
    width: 10,
    height: 10,
    borderRadius: 5,
    borderWidth: 2,
    borderColor: colors.ink,
    backgroundColor: colors.paper,
  },
  spineDotEnd: { backgroundColor: colors.ink },
  spineLine: { flex: 1, width: 2, minHeight: 26, backgroundColor: colors.ink },
  spineStops: { flex: 1, justifyContent: 'space-between', gap: space.lg },
  spineStop: { ...type.label, fontSize: 16, color: colors.ink },
  spineNote: { ...type.caption, color: colors.muted, marginTop: 2 },

  row: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    minHeight: 52,
    paddingVertical: space.sm,
    gap: space.md,
  },
  rowLabel: { ...type.body, color: colors.ink, flexShrink: 1 },
  rowValue: { ...type.label, color: colors.ink },

  field: {
    backgroundColor: colors.paper,
    borderRadius: radius.control,
    borderWidth: border.rest,
    borderColor: colors.line,
    minHeight: sizes.cta,
    paddingHorizontal: space.lg - border.rest,
    paddingVertical: 7,
    justifyContent: 'center',
  },
  fieldCompact: { flex: 1 },
  fieldValid: { borderColor: colors.ink },
  fieldFocused: {
    borderWidth: border.selected,
    borderColor: colors.ink,
    paddingHorizontal: space.lg - border.selected,
    paddingVertical: 7 - (border.selected - border.rest),
  },
  fieldMultiline: { minHeight: 88 },
  fieldLabel: { ...type.caption, fontSize: 12, lineHeight: 16, color: colors.muted },
  fieldInput: { ...type.body, color: colors.ink, paddingVertical: 2, minHeight: 26 },

  stepper: { flexDirection: 'row', alignItems: 'center', gap: space.md },
  stepperButton: {
    width: sizes.tap,
    height: sizes.tap,
    borderRadius: sizes.tap / 2,
    borderWidth: border.rest,
    borderColor: colors.line,
    backgroundColor: colors.paper,
    alignItems: 'center',
    justifyContent: 'center',
  },
  stepperValue: { ...type.time, fontSize: 22, color: colors.ink, minWidth: 22, textAlign: 'center' },

  chip: {
    flex: 1,
    flexDirection: 'row',
    gap: 6,
    alignItems: 'center',
    justifyContent: 'center',
    minHeight: sizes.tap,
    paddingVertical: space.sm,
    // A long Greek word wraps inside the chip instead of touching its edges;
    // «Σαββατοκύριακο» did at the width of an ordinary phone.
    paddingHorizontal: space.xs + 1.5,
    borderRadius: radius.chip,
    borderWidth: border.rest,
    borderColor: colors.line,
    backgroundColor: colors.paper,
  },
  chipFit: { flexGrow: 1, flexBasis: 'auto', paddingHorizontal: space.md },
  chipSelected: {
    borderWidth: border.selected,
    borderColor: colors.ink,
    paddingVertical: space.sm - 1.5,
    paddingHorizontal: space.xs,
  },
  chipLabel: { ...type.label, color: colors.muted, textAlign: 'center', flexShrink: 1 },
  chipLabelSelected: { color: colors.ink },

  skeleton: { gap: space.md, paddingVertical: space.sm },
  skeletonBlock: { height: 96, borderRadius: radius.card, backgroundColor: colors.surface },

  segTrack: {
    flexDirection: 'row',
    backgroundColor: colors.surface,
    borderRadius: radius.control,
    padding: 4,
    gap: 4,
  },
  seg: {
    flex: 1,
    minHeight: 40,
    alignItems: 'center',
    justifyContent: 'center',
    borderRadius: radius.control - 3,
    borderWidth: border.rest,
    borderColor: 'transparent',
    paddingHorizontal: space.sm,
  },
  segOn: { backgroundColor: colors.paper, borderColor: colors.ink },
  segLabel: { ...type.label, color: colors.muted },
  segLabelOn: { color: colors.ink },

  bottomBar: {
    backgroundColor: colors.paper,
    borderTopWidth: 1,
    borderTopColor: colors.line,
    paddingHorizontal: space.gutter,
    paddingTop: space.md,
    gap: space.sm,
  },

  iconButton: {
    width: sizes.tap,
    height: sizes.tap,
    borderRadius: sizes.tap / 2,
    borderWidth: border.rest,
    borderColor: colors.line,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.paper,
  },
});
