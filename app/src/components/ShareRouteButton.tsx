// The one way out of the app.
//
// The native sheet rejects on a real failure, and an unhandled rejection
// there takes the hosting screen down. So the rejection is caught and shown
// as the same quiet hint the rest of the app uses, and a dismissed sheet is
// silence rather than an error: closing it without sending means nothing went
// wrong.

import { useState } from 'react';
import { Share } from 'react-native';
import { useTranslation } from 'react-i18next';

import { shareRouteMessage, type ShareableRoute } from '../design/shareRoute';
import { Button, Hint } from './ui';

export function ShareRouteButton({
  route,
  testID,
  failedTestID,
}: {
  route: ShareableRoute;
  testID: string;
  failedTestID: string;
}) {
  const { t } = useTranslation();
  const [failed, setFailed] = useState(false);
  const [sharing, setSharing] = useState(false);

  const onPress = async () => {
    // A second press while the sheet is still open would queue a second
    // sheet behind the first, so it is ignored.
    if (sharing) return;
    // A failure belongs to the attempt that produced it, not to the next one.
    setFailed(false);
    setSharing(true);
    try {
      await Share.share({ message: shareRouteMessage(route, t) });
    } catch {
      setFailed(true);
    } finally {
      setSharing(false);
    }
  };

  return (
    <>
      <Button
        testID={testID}
        label={t('share.button')}
        variant="secondary"
        onPress={() => void onPress()}
      />
      {failed ? <Hint testID={failedTestID}>{t('common.error')}</Hint> : null}
    </>
  );
}
