// A few shared primitives. Deliberately plain: the pilot needs the flows to
// work, and a design system can come once real people have used it.

import { ActivityIndicator, Pressable, StyleSheet, Text, View } from 'react-native';
import type { ReactNode } from 'react';

export const colors = {
  text: '#111827',
  muted: '#6b7280',
  border: '#e5e7eb',
  accent: '#1d4ed8',
  danger: '#b91c1c',
  bg: '#ffffff',
};

export function Screen({ children }: { children: ReactNode }) {
  return <View style={styles.screen}>{children}</View>;
}

export function Title({ children }: { children: ReactNode }) {
  return <Text style={styles.title}>{children}</Text>;
}

export function Hint({ children }: { children: ReactNode }) {
  return <Text style={styles.hint}>{children}</Text>;
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
      accessibilityState={{ disabled: !!disabled }}
      onPress={onPress}
      disabled={disabled}
      style={[
        styles.button,
        variant === 'secondary' && styles.buttonSecondary,
        variant === 'danger' && styles.buttonDanger,
        disabled && styles.buttonDisabled,
      ]}
    >
      <Text style={[styles.buttonLabel, variant === 'secondary' && styles.buttonLabelSecondary]}>
        {label}
      </Text>
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
  children,
}: {
  loading: boolean;
  error: unknown;
  onRetry?: () => void;
  empty?: boolean;
  emptyLabel?: string;
  children: ReactNode;
}) {
  if (loading) return <ActivityIndicator testID="loading" style={styles.centered} />;

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
  screen: { flex: 1, padding: 20, backgroundColor: colors.bg, gap: 12 },
  title: { fontSize: 24, fontWeight: '600', color: colors.text },
  hint: { fontSize: 15, color: colors.muted, lineHeight: 21 },
  error: { fontSize: 15, color: colors.danger, textAlign: 'center' },
  centered: { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 12, padding: 20 },
  button: {
    backgroundColor: colors.accent,
    paddingVertical: 16,
    paddingHorizontal: 20,
    borderRadius: 12,
    alignItems: 'center',
  },
  buttonSecondary: { backgroundColor: 'transparent', borderWidth: 1, borderColor: colors.border },
  buttonDanger: { backgroundColor: colors.danger },
  buttonDisabled: { opacity: 0.4 },
  buttonLabel: { color: '#fff', fontSize: 16, fontWeight: '600' },
  buttonLabelSecondary: { color: colors.text },
});
