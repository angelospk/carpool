// Deleting the account, which cannot be undone and so is not done on a session
// alone: the server texts a code to the account's own number and the deletion
// quotes it. Reached only after the profile screen has asked "are you sure".

import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';

import { api } from '../../src/api/endpoints';
import { ApiError, NetworkError, currentBaseUrl } from '../../src/api/client';
import { useSession } from '../../src/state/session';
import { Button, Card, Field, Hint, Screen, Spacer, Title } from '../../src/components/ui';

export default function DeleteAccountScreen() {
  const { t } = useTranslation();
  const signOut = useSession((s) => s.signOut);

  const [code, setCode] = useState('');
  const [error, setError] = useState<string | null>(null);

  const explain = (e: unknown) => {
    if (e instanceof NetworkError) return t('auth.unreachable', { url: currentBaseUrl() });
    if (e instanceof ApiError) {
      if (e.code === 'bad_code') return t('profile.deleteBadCode');
      if (e.code === 'rate_limited') return t('auth.tooManyRequests');
    }
    return t('common.error');
  };

  const request = useMutation({
    mutationFn: () => api.requestDeleteCode(),
    onSuccess: (res) => {
      // On a DEV_MODE server the code comes back rather than by SMS.
      if (res?.dev_code) setCode(res.dev_code);
      setError(null);
    },
    onError: (e) => setError(explain(e)),
  });

  // Asked for once, on arrival: the person has already said yes.
  const asked = useRef(false);
  useEffect(() => {
    if (asked.current) return;
    asked.current = true;
    request.mutate();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const remove = useMutation({
    mutationFn: () => api.deleteAccount(code),
    // The account and its sessions are gone; this only clears the device.
    onSuccess: () => void signOut(),
    onError: (e) => setError(explain(e)),
  });

  return (
    <Screen>
      <Title>{t('profile.deleteAccount')}</Title>
      {/* Only once the text has gone: before that it would be untrue. */}
      {request.isSuccess ? <Hint>{t('profile.deleteCodeSent')}</Hint> : null}

      <Card>
        <Field
          testID="delete-code"
          label={t('profile.deleteCodeLabel')}
          value={code}
          onChangeText={(v) => setCode(v.replace(/\D/g, '').slice(0, 6))}
          keyboardType="number-pad"
        />
      </Card>

      {error ? <Hint>{error}</Hint> : null}

      <Spacer />

      <Button
        testID="delete-confirm"
        label={t('profile.deleteAccount')}
        variant="danger"
        disabled={code.length !== 6 || remove.isPending}
        onPress={() => remove.mutate()}
      />
    </Screen>
  );
}
