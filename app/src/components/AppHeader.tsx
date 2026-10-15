// The header over every stacked screen: a round back button with a hairline
// ring, and the title beside it. Replaces the platform's own bar so a screen
// reads the same on Android, iOS and the web.

import { StyleSheet, Text, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useTranslation } from 'react-i18next';

import { IconButton } from './ui';
import { colors, space, type } from '../design/tokens';

// expo-router hands the header the native-stack props; these are the parts used.
type HeaderProps = {
  navigation: { goBack: () => void; canGoBack?: () => boolean };
  options: { title?: string };
  route: { name: string };
  back?: unknown;
};

export function AppHeader({ navigation, options, route, back }: HeaderProps) {
  const { t } = useTranslation();
  const insets = useSafeAreaInsets();
  return (
    <View style={[styles.bar, { paddingTop: insets.top + space.sm }]}>
      {back ? (
        <IconButton
          testID="header-back"
          name="arrow-back"
          label={t('common.back')}
          onPress={() => navigation.goBack()}
        />
      ) : null}
      <Text style={styles.title} numberOfLines={2} accessibilityRole="header">
        {options.title ?? route.name}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  bar: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.md,
    paddingHorizontal: space.gutter,
    paddingBottom: space.sm,
    backgroundColor: colors.paper,
  },
  title: { ...type.heading, color: colors.ink, flex: 1 },
});
