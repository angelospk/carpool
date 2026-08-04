import { useState } from 'react';
import { Alert, Platform, Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';
import { useRouter } from 'expo-router';

import { api } from '../../src/api/endpoints';
import { forgetPhoto } from '../../src/api/images';
import { ApiError } from '../../src/api/client';
import { useSession } from '../../src/state/session';
import { Avatar, Button, Card, Field, Row, Screen, SectionHeader } from '../../src/components/ui';
import { colors, radius, space, type } from '../../src/design/tokens';

export default function ProfileScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const me = useSession((s) => s.me);
  const setMe = useSession((s) => s.setMe);
  const signOut = useSession((s) => s.signOut);

  // null means "not being edited". Distinct from an empty string, which is a
  // person deliberately clearing a field.
  const [name, setName] = useState<string | null>(null);
  const [facebook, setFacebook] = useState<string | null>(null);
  const [instagram, setInstagram] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const save = useMutation({
    mutationFn: () =>
      api.updateMe({
        ...(name !== null ? { name } : {}),
        ...(facebook !== null ? { facebook_handle: facebook } : {}),
        ...(instagram !== null ? { instagram_handle: instagram } : {}),
      }),
    onSuccess: (updated) => {
      setMe(updated);
      setName(null);
      setFacebook(null);
      setInstagram(null);
      setError(null);
    },
    onError: (e) =>
      setError(
        e instanceof ApiError && e.code === 'invalid_handle'
          ? t('profile.handleInvalid')
          : e instanceof ApiError && e.code === 'invalid_name'
            ? t('profile.nameInvalid')
            : t('common.error'),
      ),
  });

  const uploadPhoto = useMutation({
    mutationFn: async (file: Blob) => api.uploadPhoto(file),
    onSuccess: (res) => {
      forgetPhoto(me?.photo_url);
      if (me) setMe({ ...me, photo_url: res.photo_url });
    },
    onError: (e) =>
      setError(e instanceof ApiError && e.code === 'too_large' ? t('profile.photoTooBig') : t('profile.photoInvalid')),
  });

  const removePhoto = useMutation({
    mutationFn: () => api.removePhoto(),
    onSuccess: () => {
      forgetPhoto(me?.photo_url);
      if (me) setMe({ ...me, photo_url: null });
    },
  });

  // Picking a file is the one thing the two platforms do completely
  // differently, and the web build is where this gets tested, so it is the one
  // wired up. On a device this needs a picker; until then the button says so
  // rather than doing nothing.
  const pickPhoto = () => {
    if (Platform.OS !== 'web') {
      Alert.alert(t('profile.photo'), t('profile.photoOnWebOnly'));
      return;
    }
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = 'image/jpeg,image/png';
    input.onchange = () => {
      const file = input.files?.[0];
      if (file) uploadPhoto.mutate(file);
    };
    input.click();
  };

  const dirty = name !== null || facebook !== null || instagram !== null;

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.body} showsVerticalScrollIndicator={false}>
        <Card>
          <View style={styles.identity}>
            <Pressable testID="photo-pick" onPress={pickPhoto}>
              <Avatar name={me?.name ?? '·'} photoUrl={me?.photo_url} size={64} />
            </Pressable>
            <View style={styles.who}>
              <Text style={styles.name}>{me?.name ?? t('profile.title')}</Text>
              <Text testID="profile-phone" style={styles.phone}>
                {me?.phone}
              </Text>
              <View style={styles.photoActions}>
                <Text testID="photo-change" style={styles.link} onPress={pickPhoto}>
                  {uploadPhoto.isPending ? t('common.loading') : t('profile.photoChange')}
                </Text>
                {me?.photo_url ? (
                  <Text
                    testID="photo-remove"
                    style={styles.link}
                    onPress={() => removePhoto.mutate()}
                  >
                    {t('profile.photoRemove')}
                  </Text>
                ) : null}
              </View>
            </View>
          </View>
        </Card>

        <SectionHeader title={t('profile.you')} />
        <Card>
          <Field
            testID="name-field"
            label={t('profile.name')}
            value={name ?? me?.name ?? ''}
            onChangeText={setName}
          />
          <View style={styles.divider} />
          <Row
            label={t('profile.phone')}
            value={me?.phone ?? ''}
            onPress={() => router.push('/profile/phone')}
            testID="change-phone"
          />
        </Card>

        <SectionHeader title={t('profile.social')} />
        <Card>
          {/* Handles, not URLs. A person knows they are @something; they do not
              know which of the shapes of Facebook URL is theirs. */}
          <Field
            testID="facebook-field"
            label="Facebook"
            value={facebook ?? me?.facebook_handle ?? ''}
            onChangeText={setFacebook}
            placeholder="@giorgos"
            autoCapitalize="none"
          />
          <View style={styles.divider} />
          <Field
            testID="instagram-field"
            label="Instagram"
            value={instagram ?? me?.instagram_handle ?? ''}
            onChangeText={setInstagram}
            placeholder="@giorgos"
            autoCapitalize="none"
          />
          <Text style={styles.hint}>{t('profile.socialHint')}</Text>
        </Card>

        {error ? <Text style={styles.error}>{error}</Text> : null}

        {dirty ? (
          <Button
            testID="profile-save"
            label={t('common.save')}
            disabled={save.isPending}
            onPress={() => save.mutate()}
          />
        ) : null}

        <SectionHeader title={t('profile.settings')} />
        <Card>
          <Row
            label={t('profile.womenOnly')}
            value={me?.women_only ? t('common.on') : t('common.off')}
          />
          <View style={styles.divider} />
          <Row label={t('profile.language')} value="Ελληνικά" />
          <View style={styles.divider} />
          <Row
            testID="my-routes"
            label={t('profile.routes')}
            value=""
            onPress={() => router.push('/profile/routes')}
          />
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
  who: { gap: 2, flex: 1 },
  name: { ...type.title, color: colors.ink },
  phone: { ...type.caption, color: colors.muted },
  photoActions: { flexDirection: 'row', gap: space.md, marginTop: space.xs },
  link: { ...type.caption, color: colors.primary },
  divider: { height: 1, backgroundColor: colors.line },
  hint: { ...type.caption, color: colors.muted, marginTop: space.sm },
  error: { ...type.caption, color: colors.danger },
});
