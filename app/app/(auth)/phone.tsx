import { useState } from 'react';
import { StyleSheet, View } from 'react-native';
import { useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';

import { api } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
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
      await api.requestOtp(normalized);
      router.push({ pathname: '/(auth)/otp', params: { phone: normalized } });
    } catch (e) {
      setError(
        e instanceof ApiError && e.code === 'rate_limited'
          ? t('auth.tooManyRequests')
          : t('common.error'),
      );
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
