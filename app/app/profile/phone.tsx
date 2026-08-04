// Moving the account to a different number.
//
// Two steps on one screen, because they are one act: the code that arrives is
// the answer to the number just typed, and sending somebody to another screen
// to enter it loses that. The number being moved to is never sent again after
// the first step — the server takes it from the challenge — so nothing here can
// point the change somewhere else.

import { useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';

import { api } from '../../src/api/endpoints';
import { ApiError, NetworkError, currentBaseUrl } from '../../src/api/client';
import { useSession } from '../../src/state/session';
import { Button, Card, Field, Hint, Screen, Title } from '../../src/components/ui';
import { colors, space, type } from '../../src/design/tokens';

export default function ChangePhoneScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const me = useSession((s) => s.me);
  // signIn, because that is exactly what this is: the old session was revoked
  // by the change, and this pair is a new one.
  const adopt = useSession((s) => s.signIn);

  const [phone, setPhone] = useState('+30');
  const [challenge, setChallenge] = useState<string | null>(null);
  const [code, setCode] = useState('');
  const [error, setError] = useState<string | null>(null);

  const valid = /^\+[1-9][0-9]{7,14}$/.test(phone.replace(/\s/g, ''));

  const explain = (e: unknown) => {
    if (e instanceof NetworkError) return t('auth.unreachable', { url: currentBaseUrl() });
    if (e instanceof ApiError) {
      switch (e.code) {
        case 'same_number':
          return t('phone.sameNumber');
        case 'phone_unavailable':
          return t('phone.unavailable');
        case 'rate_limited':
          return t('auth.tooManyRequests');
        case 'invalid_code':
          return t('auth.invalidCode');
      }
    }
    return t('common.error');
  };

  const request = useMutation({
    mutationFn: () => api.requestPhoneChange(phone.replace(/\s/g, '')),
    onSuccess: (res) => {
      setChallenge(res.challenge_id);
      // On a DEV_MODE server the code comes back rather than by SMS, the same
      // as at sign-in, so testing does not need a second handset.
      setCode(res.dev_code ?? '');
      setError(null);
    },
    onError: (e) => setError(explain(e)),
  });

  const confirm = useMutation({
    mutationFn: () => api.confirmPhoneChange(challenge as string, code),
    onSuccess: async (pair) => {
      // Every previous session was revoked by the change, this one included, so
      // the pair that came back is the only way back in.
      await adopt({ accessToken: pair.access_token, refreshToken: pair.refresh_token });
      router.back();
    },
    onError: (e) => setError(explain(e)),
  });

  return (
    <Screen>
      <View style={styles.top}>
        <Title>{t('phone.title')}</Title>
        <Hint>{challenge ? t('phone.codeHint') : t('phone.hint')}</Hint>
      </View>

      <Card>
        {challenge ? (
          <Field
            testID="new-code"
            label={t('phone.code')}
            value={code}
            onChangeText={(v) => setCode(v.replace(/\D/g, '').slice(0, 6))}
            keyboardType="number-pad"
          />
        ) : (
          <Field
            testID="new-phone"
            label={t('phone.newNumber')}
            value={phone}
            onChangeText={setPhone}
            keyboardType="phone-pad"
            valid={valid}
          />
        )}
      </Card>

      <Text style={styles.current}>{t('phone.current', { phone: me?.phone ?? '' })}</Text>
      {/* Said before it happens rather than discovered afterwards: the change
          signs every other device out. */}
      <Text style={styles.warning}>{t('phone.signsOutOthers')}</Text>

      {error ? <Hint>{error}</Hint> : null}

      <View style={styles.spacer} />

      {challenge ? (
        <Button
          testID="phone-confirm"
          label={t('common.save')}
          disabled={code.length !== 6 || confirm.isPending}
          onPress={() => confirm.mutate()}
        />
      ) : (
        <Button
          testID="phone-request"
          label={t('common.next')}
          disabled={!valid || request.isPending}
          onPress={() => request.mutate()}
        />
      )}
    </Screen>
  );
}

const styles = StyleSheet.create({
  top: { gap: space.sm },
  spacer: { flex: 1 },
  current: { ...type.caption, color: colors.muted },
  warning: { ...type.caption, color: colors.muted },
});
