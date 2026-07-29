import { ScrollView, StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';

import { useSession } from '../../src/state/session';
import { Avatar, Button, Card, Row, Screen, SectionHeader } from '../../src/components/ui';
import { colors, space, type } from '../../src/design/tokens';

export default function ProfileScreen() {
  const { t } = useTranslation();
  const me = useSession((s) => s.me);
  const signOut = useSession((s) => s.signOut);

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.body} showsVerticalScrollIndicator={false}>
        <Card>
          <View style={styles.identity}>
            <Avatar name={me?.name ?? '·'} photoUrl={me?.photo_url} size={64} />
            <View style={styles.who}>
              <Text style={styles.name}>{me?.name ?? t('profile.title')}</Text>
              <Text testID="profile-phone" style={styles.phone}>
                {me?.phone}
              </Text>
            </View>
          </View>
        </Card>

        <SectionHeader title={t('profile.settings')} />
        <Card>
          <Row
            label={t('profile.womenOnly')}
            value={me?.women_only ? t('common.on') : t('common.off')}
          />
          <View style={styles.divider} />
          <Row label={t('profile.language')} value="Ελληνικά" />
        </Card>

        <Button
          testID="sign-out"
          label={t('profile.signOut')}
          variant="danger"
          onPress={() => void signOut()}
        />
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { gap: space.md, paddingBottom: 140 },
  identity: { flexDirection: 'row', alignItems: 'center', gap: space.lg },
  who: { gap: 2 },
  name: { ...type.title, color: colors.ink },
  phone: { ...type.caption, color: colors.muted },
  divider: { height: 1, backgroundColor: colors.line },
});
