import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';

import { useSession } from '../../src/state/session';
import { Button, Screen, Title, colors } from '../../src/components/ui';

export default function ProfileScreen() {
  const { t } = useTranslation();
  const me = useSession((s) => s.me);
  const signOut = useSession((s) => s.signOut);

  return (
    <Screen>
      <Title>{me?.name ?? t('profile.title')}</Title>
      <View style={styles.field}>
        <Text style={styles.label}>{t('auth.phoneTitle')}</Text>
        <Text testID="profile-phone" style={styles.value}>
          {me?.phone}
        </Text>
      </View>
      <Button
        testID="sign-out"
        label={t('profile.signOut')}
        variant="danger"
        onPress={() => void signOut()}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  field: { gap: 4, paddingVertical: 12, borderBottomWidth: 1, borderBottomColor: colors.border },
  label: { fontSize: 13, color: colors.muted },
  value: { fontSize: 17, color: colors.text },
});
