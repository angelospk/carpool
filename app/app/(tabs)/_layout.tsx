import { Pressable, StyleSheet, Text, View } from 'react-native';
import { Tabs } from 'expo-router';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useTranslation } from 'react-i18next';

import { colors, lift, radius, space, type } from '../../src/design/tokens';

// The bar floats over the content as a single pill, and the active tab is a
// filled pill inside it — the whole navigation is one object rather than a strip
// welded to the bottom of the screen.
const GLYPHS: Record<string, string> = {
  index: '⌂',
  search: '⌕',
  publish: '＋',
  profile: '☺',
};

export default function TabsLayout() {
  const { t } = useTranslation();
  return (
    <Tabs screenOptions={{ headerShown: false }} tabBar={(props) => <PillTabBar {...props} />}>
      <Tabs.Screen name="index" options={{ title: t('home.title') }} />
      <Tabs.Screen name="search" options={{ title: t('search.title') }} />
      <Tabs.Screen name="publish" options={{ title: t('publish.title') }} />
      <Tabs.Screen name="profile" options={{ title: t('profile.title') }} />
    </Tabs>
  );
}

// expo-router re-exports the navigator but not its prop types, so the three
// pieces the bar actually uses are named here.
type TabBarProps = {
  state: { index: number; routes: { key: string; name: string }[] };
  descriptors: Record<string, { options: { title?: string } }>;
  navigation: {
    emit: (e: { type: 'tabPress'; target: string; canPreventDefault: true }) => {
      defaultPrevented: boolean;
    };
    navigate: (name: string) => void;
  };
};

function PillTabBar({ state, descriptors, navigation }: TabBarProps) {
  const insets = useSafeAreaInsets();

  return (
    <View style={[styles.dock, { paddingBottom: Math.max(insets.bottom, space.md) }]}>
      <View style={styles.bar}>
        {state.routes.map((route, index) => {
          const { options } = descriptors[route.key];
          const focused = state.index === index;
          const label = options.title ?? route.name;

          return (
            <Pressable
              key={route.key}
              testID={`tab-${route.name}`}
              accessibilityRole="button"
              aria-selected={focused}
              accessibilityLabel={label}
              onPress={() => {
                const event = navigation.emit({
                  type: 'tabPress',
                  target: route.key,
                  canPreventDefault: true,
                });
                if (!focused && !event.defaultPrevented) navigation.navigate(route.name);
              }}
              style={[styles.tab, focused && styles.tabActive]}
            >
              <Text style={[styles.glyph, focused && styles.glyphActive]}>
                {GLYPHS[route.name] ?? '•'}
              </Text>
              {focused ? <Text style={styles.label}>{label}</Text> : null}
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  dock: {
    position: 'absolute',
    left: 0,
    right: 0,
    bottom: 0,
    alignItems: 'center',
    paddingHorizontal: space.lg,
  },
  bar: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.xs,
    backgroundColor: colors.surface,
    borderRadius: radius.pill,
    padding: 6,
    ...lift,
    shadowOpacity: 0.12,
    elevation: 6,
  },
  tab: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.sm,
    paddingHorizontal: space.lg,
    paddingVertical: space.md,
    borderRadius: radius.pill,
  },
  tabActive: { backgroundColor: colors.primary },
  glyph: { fontSize: 20, color: colors.muted, lineHeight: 22 },
  glyphActive: { color: colors.primaryInk },
  label: { ...type.label, fontSize: 13, color: colors.primaryInk },
});
