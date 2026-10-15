import { Pressable, StyleSheet, Text, useWindowDimensions, View } from 'react-native';
import { Tabs } from 'expo-router';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useTranslation } from 'react-i18next';

import { colors, size, space, type } from '../../src/design/tokens';
import { Icon, type IconName } from '../../src/components/Icon';
import { breakpoint } from '../../src/design/layout';

// A plain strip along the bottom with a hairline over it: an icon and its word
// for every tab, the active one in ink and the rest muted. The screen's own
// black button sits above it, so the bar itself stays quiet.
const ICONS: Record<string, [IconName, IconName]> = {
  index: ['home-outline', 'home'],
  search: ['search-outline', 'search'],
  publish: ['add-circle-outline', 'add-circle'],
  profile: ['person-circle-outline', 'person-circle'],
};

export default function TabsLayout() {
  const { t } = useTranslation();
  return (
    <Tabs
      screenOptions={{ headerShown: false, sceneStyle: { backgroundColor: colors.paper } }}
      tabBar={(props) => <TabBar {...props} />}
    >
      <Tabs.Screen name="index" options={{ title: t('tabs.home') }} />
      <Tabs.Screen name="search" options={{ title: t('tabs.search') }} />
      <Tabs.Screen name="publish" options={{ title: t('tabs.publish') }} />
      <Tabs.Screen name="profile" options={{ title: t('tabs.profile') }} />
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

function TabBar({ state, descriptors, navigation }: TabBarProps) {
  const insets = useSafeAreaInsets();
  // Every tab is named, on a phone too: a row of bare icons asks somebody to
  // guess, and four short words fit in a thumb's width at this size.
  const roomy = breakpoint(useWindowDimensions().width) !== 'compact';

  return (
    <View style={[styles.bar, { paddingBottom: Math.max(insets.bottom, space.sm) }]}>
      <View style={[styles.row, roomy && styles.rowRoomy]}>
        {state.routes.map((route, index) => {
          const { options } = descriptors[route.key];
          const focused = state.index === index;
          const label = options.title ?? route.name;
          const [idle, active] = ICONS[route.name] ?? ['ellipse-outline', 'ellipse'];

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
              style={({ pressed }) => [styles.tab, pressed && styles.pressed]}
            >
              <Icon name={focused ? active : idle} size={24} color={focused ? colors.ink : colors.muted} />
              <Text numberOfLines={1} style={[styles.label, focused && styles.labelActive]}>
                {label}
              </Text>
            </Pressable>
          );
        })}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  bar: {
    backgroundColor: colors.paper,
    borderTopWidth: 1,
    borderTopColor: colors.line,
    paddingTop: 6,
  },
  row: { flexDirection: 'row', alignSelf: 'stretch' },
  rowRoomy: { alignSelf: 'center', width: '100%', maxWidth: 560 },
  tab: {
    flex: 1,
    minHeight: size.tap + 8,
    alignItems: 'center',
    justifyContent: 'center',
    gap: 2,
    paddingHorizontal: 2,
  },
  pressed: { opacity: 0.7 },
  label: { ...type.label, fontSize: 12, lineHeight: 15, color: colors.muted },
  labelActive: { fontFamily: type.button.fontFamily, color: colors.ink },
});
