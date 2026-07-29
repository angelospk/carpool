import { useEffect, useState } from 'react';
import { StyleSheet, TextInput } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';

import { api, type Region } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { Button, Hint, Screen, Title, colors } from '../../src/components/ui';

export default function OtpScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { phone } = useLocalSearchParams<{ phone: string }>();
  const [code, setCode] = useState('');
  const [regions, setRegions] = useState<Region[]>([]);
  const [error, setError] = useState<string | null>(null);

  // Regions are needed by the next step; fetching now means the profile screen
  // opens without a spinner.
  useEffect(() => {
    api
      .regions()
      .then((r) => setRegions(r.regions))
      .catch(() => setRegions([]));
  }, []);

  const submit = () => {
    setError(null);
    router.push({
      pathname: '/(auth)/profile-setup',
      params: { phone, code, regions: JSON.stringify(regions) },
    });
  };

  return (
    <Screen>
      <Title>{t('auth.otpTitle')}</Title>
      <Hint>{t('auth.otpHint')}</Hint>
      <TextInput
        testID="otp-input"
        style={styles.input}
        value={code}
        onChangeText={(v) => setCode(v.replace(/\D/g, '').slice(0, 6))}
        keyboardType="number-pad"
        autoComplete="sms-otp"
        textContentType="oneTimeCode"
        maxLength={6}
      />
      {error ? <Hint>{error}</Hint> : null}
      <Button
        testID="otp-submit"
        label={t('common.next')}
        onPress={submit}
        disabled={code.length !== 6}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  input: {
    borderWidth: 1,
    borderColor: colors.border,
    borderRadius: 12,
    padding: 16,
    fontSize: 28,
    letterSpacing: 8,
    textAlign: 'center',
  },
});
