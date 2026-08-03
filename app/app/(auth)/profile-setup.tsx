import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';
import { useLocalSearchParams } from 'expo-router';
import { useTranslation } from 'react-i18next';

import { api, type Region } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { useSession } from '../../src/state/session';
import { Button, Card, Field, Hint, Screen, Title } from '../../src/components/ui';
import { colors, radius, space, type } from '../../src/design/tokens';

export default function ProfileSetupScreen() {
  const { t } = useTranslation();
  const params = useLocalSearchParams<{ phone: string; code: string; regions: string }>();
  const signIn = useSession((s) => s.signIn);

  // Parsed defensively because this arrives as a URL query parameter on web,
  // where it is editable, truncatable and repeatable. A throw here happens
  // during render, so it takes the screen down rather than showing an error.
  const regions: Region[] = (() => {
    try {
      const parsed = params.regions ? JSON.parse(params.regions) : [];
      return Array.isArray(parsed) ? parsed : [];
    } catch {
      return [];
    }
  })();
  const [name, setName] = useState('');
  const [regionId, setRegionId] = useState(regions[0]?.id ?? '');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      const tokens = await api.verifyOtp(params.phone, params.code, name.trim(), regionId);
      await signIn({ accessToken: tokens.access_token, refreshToken: tokens.refresh_token });
      // The root layout redirects once the session flips to signedIn.
    } catch (e) {
      setError(
        e instanceof ApiError && e.code === 'invalid_code'
          ? t('auth.invalidCode')
          : t('common.error'),
      );
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

        <Hint>{t('auth.region')}</Hint>
        <View style={styles.regions}>
          {regions.map((r) => (
            <Pressable
              key={r.id}
              testID={`region-${r.id}`}
              accessibilityRole="button"
              accessibilityState={{ selected: regionId === r.id }}
              onPress={() => setRegionId(r.id)}
              style={[styles.region, regionId === r.id && styles.regionSelected]}
            >
              <Text style={[styles.regionLabel, regionId === r.id && styles.regionLabelSelected]}>
                {r.name}
              </Text>
            </Pressable>
          ))}
        </View>

        {error ? (
          <Text testID="profile-error" style={styles.error}>
            {error}
          </Text>
        ) : null}

        <Button
          testID="profile-submit"
          label={t('common.next')}
          onPress={submit}
          disabled={!name.trim() || !regionId || busy}
        />
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { gap: space.md, paddingBottom: space.xxl },
  regions: { flexDirection: 'row', flexWrap: 'wrap', gap: space.sm },
  region: {
    backgroundColor: colors.surface,
    borderRadius: radius.pill,
    paddingVertical: space.md,
    paddingHorizontal: space.lg,
  },
  regionSelected: { backgroundColor: colors.primary },
  regionLabel: { ...type.label, color: colors.ink },
  regionLabelSelected: { color: colors.primaryInk },
  error: { ...type.body, color: colors.danger },
});
