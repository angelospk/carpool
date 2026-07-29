import { Tabs } from 'expo-router';
import { useTranslation } from 'react-i18next';

import { colors } from '../../src/components/ui';

export default function TabsLayout() {
  const { t } = useTranslation();
  return (
    <Tabs screenOptions={{ tabBarActiveTintColor: colors.accent }}>
      <Tabs.Screen name="index" options={{ title: t('home.title') }} />
      <Tabs.Screen name="search" options={{ title: t('search.title') }} />
      <Tabs.Screen name="publish" options={{ title: t('publish.title') }} />
      <Tabs.Screen name="profile" options={{ title: t('profile.title') }} />
    </Tabs>
  );
}
