import { useState } from 'react';
import { StyleSheet, Text, TextInput, View } from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';

import { api } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { useSession } from '../../src/state/session';
import { Button, Card, Hint, Screen, Title } from '../../src/components/ui';
import { colors, radius, space, type } from '../../src/design/tokens';

// This screen used to fetch the list of regions and carry it forward, because
// the next screen had to send a name and a region along with the code — on
// every sign-in, not just the first. Somebody who joined in March was walked
// through onboarding again in April.
//
// Now the code is verified here, and what comes back decides where to go. A
// number that is already an account is simply signed in. A new one is handed a
// ticket, and the ticket is what travels to the profile screen — never the
// code, which has just been spent proving the number and would be refused if
// it were sent again.
export default function OtpScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { phone, devCode } = useLocalSearchParams<{ phone: string; devCode?: string }>();
  const signIn = useSession((s) => s.signIn);

  // Prefilled only when the server ran with DEV_MODE and handed the code back
  // instead of texting it. Everywhere else this is undefined and the field
  // starts empty.
  const [code, setCode] = useState(devCode ?? '');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const submit = async () => {
    setBusy(true);
    setError(null);
    try {
      const res = await api.verifyOtp(phone, code);

      if (res.signup_token) {
        router.push({
          pathname: '/(auth)/profile-setup',
          params: { signupToken: res.signup_token },
        });
        return;
      }

      await signIn({
        accessToken: res.access_token as string,
        refreshToken: res.refresh_token as string,
      });
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

      {error ? (
        <Text testID="otp-error" style={styles.error}>
          {error}
        </Text>
      ) : null}

      <View style={styles.spacer} />

      <Button
        testID="otp-submit"
        label={t('common.next')}
        onPress={submit}
        disabled={code.length !== 6 || busy}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  top: { gap: space.sm },
  spacer: { flex: 1 },
  error: { ...type.body, color: colors.danger },
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
