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
  const [regions, setRegions] = useState<Region[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Regions are needed by the next step, which has no way to fetch its own: it
  // reads the list out of the route parameters and picks the first one. So an
  // empty list there is not a slow start, it is a dead end. Its submit button
  // stays disabled with nothing on screen to say why, and no response arriving
  // later can repair it, because this screen has already been left behind.
  //
  // Hence null for "not yet" as distinct from an empty list, and a submit that
  // waits. With the code prefilled on a DEV_MODE server there is nothing to
  // type, so the press can easily land before the fetch returns.
  const load = () => {
    setError(null);
    api
      .regions()
      .then((r) => setRegions(r.regions))
      .catch(() => setError(t('auth.regionsUnavailable')));
  };

  useEffect(load, []);

  const submit = () => {
    if (!regions) {
      return;
    }
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

      {error ? (
        <Button testID="otp-retry" label={t('common.retry')} onPress={load} />
      ) : null}

      <Button
        testID="otp-submit"
        label={t('common.next')}
        onPress={submit}
        disabled={code.length !== 6 || !regions}
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
