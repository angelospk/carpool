import { useState } from 'react';
import { ScrollView, StyleSheet, Text } from 'react-native';
import { useLocalSearchParams } from 'expo-router';
import { useTranslation } from 'react-i18next';

import { api } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { useSession } from '../../src/state/session';
import { PlaceField } from '../../src/components/PlaceField';
import { type Pin } from '../../src/design/pin';
import { Button, Card, Field, Hint, Screen, Title } from '../../src/components/ui';
import { colors, space, type } from '../../src/design/tokens';

// Joining, once the number is proved.
//
// The region used to be picked from a list. That worked while the list had one
// entry; with the whole country open it has seventy-five, and asking somebody
// whether they live in "Περιφερειακή Ενότητα Δυτικού Τομέα Αθηνών" is asking
// the wrong question. They know their town. The server reads the prefecture off
// it, which is a thing a map can answer and a person should not have to.
export default function ProfileSetupScreen() {
  const { t } = useTranslation();
  const { signupToken } = useLocalSearchParams<{ signupToken: string }>();
  const signIn = useSession((s) => s.signIn);

  const [name, setName] = useState('');
  const [home, setHome] = useState<Pin | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // A place typed as a raw coordinate has no id, and the server takes ids
  // only — so it is not a home town, however well-formed it looks.
  const placeID = home?.id ?? null;

  const submit = async () => {
    if (!placeID) return;
    setBusy(true);
    setError(null);
    try {
      const tokens = await api.completeSignup({
        signup_token: signupToken,
        name: name.trim(),
        home_place_id: placeID,
      });
      await signIn({ accessToken: tokens.access_token, refreshToken: tokens.refresh_token });
      // The root layout redirects once the session flips to signedIn.
    } catch (e) {
      if (e instanceof ApiError && e.code === 'invalid_token') {
        setError(t('auth.signupExpired'));
      } else if (e instanceof ApiError && e.code === 'unknown_place') {
        setError(t('auth.unknownPlace'));
      } else {
        setError(t('common.error'));
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.body} showsVerticalScrollIndicator={false}>
        <Title>{t('auth.profileTitle')}</Title>

        <Card>
          <Field
            testID="name-input"
            label={t('auth.namePlaceholder')}
            value={name}
            onChangeText={setName}
            placeholder={t('auth.namePlaceholder')}
            valid={!!name.trim()}
          />
        </Card>

        <Hint>{t('auth.homeTownHint')}</Hint>
        <Card>
          <PlaceField
            testID="home-place"
            label={t('auth.homeTown')}
            value={home}
            onChange={setHome}
            placeholder={t('auth.homeTownPlaceholder')}
          />
        </Card>

        {error ? (
          <Text testID="profile-error" style={styles.error}>
            {error}
          </Text>
        ) : null}

        <Button
          testID="profile-submit"
          label={t('common.next')}
          onPress={submit}
          disabled={!name.trim() || !placeID || busy}
        />
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { gap: space.md, paddingBottom: space.xxl },
  error: { ...type.body, color: colors.danger },
});
