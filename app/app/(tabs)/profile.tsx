import { useState } from 'react';
import { ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';

import { api } from '../../src/api/endpoints';
import { useSession } from '../../src/state/session';
import { Avatar, Button, Card, Row, Screen, SectionHeader } from '../../src/components/ui';
import { colors, radius, space, type } from '../../src/design/tokens';

export default function ProfileScreen() {
  const { t } = useTranslation();
  const me = useSession((s) => s.me);
  const setMe = useSession((s) => s.setMe);
  const signOut = useSession((s) => s.signOut);
  const [social, setSocial] = useState<string | null>(null);
  const [socialError, setSocialError] = useState(false);

  // The server holds the allowlist; this is the same rule stated early so the
  // answer is immediate rather than a round trip away.
  const looksRight = (url: string) =>
    url === '' || /^https:\/\/(www\.|m\.|web\.)?(facebook|instagram)\.com\/.+/.test(url);

  const saveSocial = useMutation({
    mutationFn: (url: string) => api.updateMe({ social_url: url }),
    onSuccess: (updated) => {
      setMe(updated);
      setSocial(null);
      setSocialError(false);
    },
    onError: () => setSocialError(true),
  });

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

        <SectionHeader title={t('profile.social')} />
        <Card>
          <TextInput
            testID="social-input"
            style={styles.input}
            value={social ?? me?.social_url ?? ''}
            onChangeText={(v) => {
              setSocial(v);
              setSocialError(false);
            }}
            placeholder="https://facebook.com/…"
            placeholderTextColor={colors.muted}
            autoCapitalize="none"
            autoCorrect={false}
            keyboardType="url"
          />
          <Text style={styles.hint}>
            {socialError ? t('profile.socialInvalid') : t('profile.socialHint')}
          </Text>
          {social !== null && social !== (me?.social_url ?? '') ? (
            <Button
              testID="social-save"
              label={t('common.save')}
              disabled={saveSocial.isPending}
              onPress={() => {
                if (!looksRight(social)) {
                  setSocialError(true);
                  return;
                }
                saveSocial.mutate(social);
              }}
            />
          ) : null}
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
  input: {
    ...type.body,
    color: colors.ink,
    backgroundColor: colors.sunken,
    borderRadius: radius.inner,
    paddingHorizontal: space.md,
    paddingVertical: space.sm,
  },
  hint: { ...type.caption, color: colors.muted, marginTop: space.xs, marginBottom: space.sm },
});
