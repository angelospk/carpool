import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';
import { Settings } from '../../src/components/Settings';
import { useRouter } from 'expo-router';

import { api } from '../../src/api/endpoints';
import { forgetPhoto } from '../../src/api/images';
import { pickPhoto } from '../../src/photo/pick';
import { TooBigError } from '../../src/photo/limits';
import { ApiError } from '../../src/api/client';
import { refusalText } from '../../src/api/errorText';
import { useSession } from '../../src/state/session';
import { Avatar, Button, Card, Field, Row, Screen, SectionHeader } from '../../src/components/ui';
import { PreferencePicker } from '../../src/components/Preferences';
import { type PreferenceTag } from '../../src/api/endpoints';
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
  const [bio, setBio] = useState<string | null>(null);
  // Preferences are edited in place: there is no "unsaved" state to reason
  // about because a tap is the whole decision, and the save button below
  // carries them along with everything else.
  const [tags, setTags] = useState<PreferenceTag[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const save = useMutation({
    mutationFn: () =>
      api.updateMe({
        ...(name !== null ? { name } : {}),
        ...(facebook !== null ? { facebook_handle: facebook } : {}),
        ...(instagram !== null ? { instagram_handle: instagram } : {}),
        ...(bio !== null ? { bio } : {}),
        ...(tags !== null ? { preferences: tags } : {}),
      }),
    onSuccess: (updated) => {
      setMe(updated);
      setName(null);
      setFacebook(null);
      setInstagram(null);
      setBio(null);
      setTags(null);
      setError(null);
    },
    onError: (e) =>
      setError(
        e instanceof ApiError && e.code === 'bio_too_long'
          ? t('profile.bioTooLong')
          : // One message for all four kinds. The code says which thing was
            // found, but naming it back at somebody who just typed their own
            // number reads as an accusation rather than as a warning.
            e instanceof ApiError && e.code.startsWith('bio_contains_')
          ? t('profile.bioHasContactDetails')
          : e instanceof ApiError && e.code === 'invalid_handle'
          ? t('profile.handleInvalid')
          : e instanceof ApiError && e.code === 'invalid_name'
            ? t('profile.nameInvalid')
            : (refusalText(e) ?? t('common.error')),
      ),
  });

  // Read from the store rather than from `me`, which is whatever this render
  // captured. Two mutations in flight would otherwise each write their own
  // stale snapshot back, and the second would undo the first.
  const patchMe = (change: { photo_url: string | null }) => {
    const current = useSession.getState().me;
    if (current) setMe({ ...current, ...change });
  };

  const uploadPhoto = useMutation({
    mutationFn: async (file: Blob) => api.uploadPhoto(file),
    onSuccess: (res) => {
      forgetPhoto(useSession.getState().me?.photo_url);
      patchMe({ photo_url: res.photo_url });
      setError(null);
    },
    onError: (e) => {
      // Only the two answers the server actually gives about the image are
      // reported as being about the image. Offline, or a dead session, is not
      // a problem with the photograph.
      if (e instanceof ApiError && e.code === 'too_large') return setError(t('profile.photoTooBig'));
      if (e instanceof ApiError && e.code === 'invalid_image') return setError(t('profile.photoInvalid'));
      setError(t('common.error'));
    },
  });

  const removePhoto = useMutation({
    mutationFn: () => api.removePhoto(),
    onSuccess: () => {
      forgetPhoto(useSession.getState().me?.photo_url);
      patchMe({ photo_url: null });
      setError(null);
    },
    onError: () => setError(t('common.error')),
  });

  // Both platforms can choose a photo. Which picker opens is decided by the
  // import — src/photo/pick has a web and a native implementation and Metro
  // resolves one — so nothing here needs to know where it is running.
  const choosePhoto = () => {
    if (uploadPhoto.isPending) return;
    pickPhoto()
      .then((file) => {
        if (file) uploadPhoto.mutate(file);
      })
      // A photo the browser could not shrink and the server will not take is a
      // problem with the photo, and gets the same answer as if it had been sent
      // and refused.
      .catch((e) => setError(t(e instanceof TooBigError ? 'profile.photoTooBig' : 'profile.photoFailed')));
  };

  const dirty =
    name !== null || facebook !== null || instagram !== null || bio !== null || tags !== null;

  return (
    <Screen topInset>
      <ScrollView contentContainerStyle={styles.body} showsVerticalScrollIndicator={false}>
        <Card>
          <View style={styles.identity}>
            <Pressable testID="photo-pick" onPress={choosePhoto}>
              <Avatar name={me?.name ?? '·'} photoUrl={me?.photo_url} size={64} />
            </Pressable>
            <View style={styles.who}>
              <Text style={styles.name}>{me?.name ?? t('profile.title')}</Text>
              <Text testID="profile-phone" style={styles.phone}>
                {me?.phone}
              </Text>
              <View style={styles.photoActions}>
                <Text testID="photo-change" style={styles.link} onPress={choosePhoto}>
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

        <SectionHeader title={t('preferences.title')} />
        <Card>
          <Field
            testID="bio-field"
            label={t('preferences.bioLabel')}
            value={bio ?? me?.bio ?? ''}
            onChangeText={setBio}
            placeholder={t('preferences.bioPlaceholder')}
            multiline
          />
          <View style={styles.divider} />
          <PreferencePicker
            testID="preference-picker"
            value={tags ?? me?.preferences ?? []}
            onChange={setTags}
          />
          <Text style={styles.hint}>{t('preferences.hint')}</Text>
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
        <Settings />
        <Card>
          <Row
            testID="my-routes"
            label={t('profile.routes')}
            value=""
            onPress={() => router.push('/profile/routes')}
          />
          <View style={styles.divider} />
          <Row
            testID="my-groups"
            label={t('groups.title')}
            value=""
            onPress={() => router.push('/profile/groups')}
          />
          <View style={styles.divider} />
          <Row
            testID="my-vehicles"
            label={t('garage.title')}
            value=""
            onPress={() => router.push('/profile/vehicles')}
          />
          <View style={styles.divider} />
          <Row
            testID="my-blocks"
            label={t('user.blockedTitle')}
            value=""
            onPress={() => router.push('/profile/blocked')}
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
  body: { gap: space.md, paddingBottom: space.xl },
  identity: { flexDirection: 'row', alignItems: 'center', gap: space.lg },
  who: { gap: 2, flex: 1 },
  name: { ...type.title, color: colors.ink },
  phone: { ...type.caption, color: colors.muted },
  photoActions: { flexDirection: 'row', gap: space.md, marginTop: space.xs },
  link: { ...type.caption, color: colors.ink },
  divider: { height: 1, backgroundColor: colors.line },
  hint: { ...type.caption, color: colors.muted, marginTop: space.sm },
  error: { ...type.caption, color: colors.danger },
});
