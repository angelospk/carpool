import { useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';

import { api } from '../../src/api/endpoints';
import { ApiError, NetworkError, currentBaseUrl } from '../../src/api/client';
import { Button, Card, Field, Hint, Screen, Title } from '../../src/components/ui';
import { space } from '../../src/design/tokens';

export default function PhoneScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const [phone, setPhone] = useState('+30');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // E.164, matching the server's constraint. Validating here saves a round trip
  // and gives a clearer message than the server's generic rejection.
  const valid = /^\+[1-9][0-9]{7,14}$/.test(phone.replace(/\s/g, ''));

  const submit = async () => {
    setBusy(true);
    setError(null);
    const normalized = phone.replace(/\s/g, '');
    try {
      const sent = await api.requestOtp(normalized);
      // On a DEV_MODE server the code arrives here rather than by SMS; carrying
      // it forward saves the tester a trip to a log file they cannot reach.
      const devCode = sent && typeof sent === 'object' ? sent.dev_code : undefined;
      router.push({
        pathname: '/(auth)/otp',
        params: devCode ? { phone: normalized, devCode } : { phone: normalized },
      });
    } catch (e) {
      // The three failures here are not interchangeable, and the first one is
      // invisible everywhere else: a request that never reaches a server has no
      // status code and leaves no server-side log, so if this screen does not
      // name the address it tried, nothing does.
      if (e instanceof NetworkError) {
        setError(t('auth.unreachable', { url: currentBaseUrl() }));
      } else if (e instanceof ApiError && e.code === 'rate_limited') {
        setError(t('auth.tooManyRequests'));
      } else {
        setError(t('common.error'));
      }
    } finally {
      setBusy(false);
    }
  };

  return (
    <Screen>
      <View style={styles.top}>
        <Title>{t('auth.phoneTitle')}</Title>
        <Hint>{t('auth.phoneHint')}</Hint>
      </View>

      <Card>
        <Field
          testID="phone-input"
          label={t('auth.phoneTitle')}
          value={phone}
          onChangeText={setPhone}
          keyboardType="phone-pad"
          placeholder={t('auth.phonePlaceholder')}
          valid={valid}
        />
      </Card>

      {error ? <Hint>{error}</Hint> : null}

      <View style={styles.spacer} />

      <Button
        testID="phone-submit"
        label={t('common.next')}
        onPress={submit}
        disabled={!valid || busy}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  top: { gap: space.sm },
  spacer: { flex: 1 },
});
