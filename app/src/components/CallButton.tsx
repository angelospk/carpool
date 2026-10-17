import { useEffect, useState } from 'react';
import { Linking } from 'react-native';
import { useTranslation } from 'react-i18next';

import { Button } from './ui';

// setTimeout stores its delay in 32 bits and fires at once beyond this.
const MAX_TIMER_MS = 2 ** 31 - 1;

const open = (until?: string | null) => {
  if (!until) return true;
  const end = Date.parse(until);
  return Number.isNaN(end) || Date.now() < end;
};

/**
 * A call to the other person on the trip. The server hands over the number only
 * for a confirmed booking inside the call window and says when the window
 * closes (`until`), so a number cached past that moment is not dialled: the
 * button hides itself then, and a press re-checks the clock first. The number
 * is dialled, never printed.
 */
export function CallButton({
  phone,
  until,
  testID,
}: {
  phone?: string | null;
  until?: string | null;
  testID?: string;
}) {
  const { t } = useTranslation();
  const [, tick] = useState(0);

  useEffect(() => {
    if (!until) return;
    const end = Date.parse(until);
    if (Number.isNaN(end)) return;
    const left = end - Date.now();
    if (left <= 0) return;
    const timer = setTimeout(() => tick((n) => n + 1), Math.min(left, MAX_TIMER_MS));
    return () => clearTimeout(timer);
  }, [until, tick]);

  if (!phone || !open(until)) return null;
  return (
    <Button
      testID={testID ?? 'call'}
      label={t('trip.call')}
      variant="secondary"
      onPress={() => {
        if (open(until)) void Linking.openURL(`tel:${phone}`);
      }}
    />
  );
}
