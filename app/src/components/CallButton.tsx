import { Linking } from 'react-native';
import { useTranslation } from 'react-i18next';

import { Button } from './ui';

/**
 * A call to the other person on the trip. The server hands over the number only
 * for a confirmed booking on the day of the trip, so its absence is the whole
 * rule here. The number is dialled, never printed.
 */
export function CallButton({ phone, testID }: { phone?: string | null; testID?: string }) {
  const { t } = useTranslation();
  if (!phone) return null;
  return (
    <Button
      testID={testID ?? 'call'}
      label={t('trip.call')}
      variant="secondary"
      onPress={() => void Linking.openURL(`tel:${phone}`)}
    />
  );
}
