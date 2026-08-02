import { useEffect, useState } from 'react';
import { StyleSheet, TextInput, View } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';

import { api, type Region } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { Button, Card, Hint, Screen, Title } from '../../src/components/ui';
import { colors, radius, space, type } from '../../src/design/tokens';

export default function OtpScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { phone, devCode } = useLocalSearchParams<{ phone: string; devCode?: string }>();
  // Prefilled only when the server ran with DEV_MODE and handed the code back
  // instead of texting it. Everywhere else this is undefined and the field
  // starts empty, exactly as before.
  const [code, setCode] = useState(devCode ?? '');
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
      <View style={styles.top}>
        <Title>{t('auth.otpTitle')}</Title>
        <Hint>{t('auth.otpHint')}</Hint>
      </View>

      <Card>
        <TextInput
          testID="otp-input"
          style={styles.input}
          value={code}
          onChangeText={(v) => setCode(v.replace(/\D/g, '').slice(0, 6))}
          keyboardType="number-pad"
          autoComplete="sms-otp"
          textContentType="oneTimeCode"
          maxLength={6}
          placeholder="······"
          placeholderTextColor={colors.line}
        />
      </Card>

      {error ? <Hint>{error}</Hint> : null}

      <View style={styles.spacer} />

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
  top: { gap: space.sm },
  spacer: { flex: 1 },
  input: {
    ...type.display,
    fontSize: 32,
    letterSpacing: 12,
    textAlign: 'center',
    color: colors.ink,
    backgroundColor: colors.sunken,
    borderRadius: radius.inner,
    paddingVertical: space.lg,
  },
});
