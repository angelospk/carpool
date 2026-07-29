import { useState } from 'react';
import { Pressable, ScrollView, StyleSheet, Text, TextInput, View } from 'react-native';
import { useLocalSearchParams } from 'expo-router';
import { useTranslation } from 'react-i18next';

import { api, type Region } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { useSession } from '../../src/state/session';
import { Button, Hint, Title, colors } from '../../src/components/ui';

export default function ProfileSetupScreen() {
  const { t } = useTranslation();
  const params = useLocalSearchParams<{ phone: string; code: string; regions: string }>();
  const signIn = useSession((s) => s.signIn);

  const regions: Region[] = params.regions ? JSON.parse(params.regions) : [];
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
    <ScrollView contentContainerStyle={styles.screen}>
      <Title>{t('auth.profileTitle')}</Title>
      <TextInput
        testID="name-input"
        style={styles.input}
        value={name}
        onChangeText={setName}
        placeholder={t('auth.namePlaceholder')}
        autoComplete="name"
      />

      <Hint>{t('auth.region')}</Hint>
      <View style={styles.regions}>
        {regions.map((r) => (
          <Pressable
            key={r.id}
            testID={`region-${r.id}`}
            onPress={() => setRegionId(r.id)}
            style={[styles.region, regionId === r.id && styles.regionSelected]}
          >
            <Text style={regionId === r.id ? styles.regionLabelSelected : undefined}>{r.name}</Text>
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
  );
}

const styles = StyleSheet.create({
  screen: { padding: 20, gap: 12, backgroundColor: colors.bg, flexGrow: 1 },
  input: { borderWidth: 1, borderColor: colors.border, borderRadius: 12, padding: 16, fontSize: 18 },
  regions: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  region: { borderWidth: 1, borderColor: colors.border, borderRadius: 999, paddingVertical: 10, paddingHorizontal: 16 },
  regionSelected: { backgroundColor: colors.accent, borderColor: colors.accent },
  regionLabelSelected: { color: '#fff', fontWeight: '600' },
  error: { color: colors.danger },
});
