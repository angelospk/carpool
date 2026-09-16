// The one way out of the app.
//
// The native sheet rejects on a real failure, and an unhandled rejection
// there takes the hosting screen down. So the rejection is caught and shown
// as the same quiet hint the rest of the app uses, and a dismissed sheet is
// silence rather than an error: closing it without sending means nothing went
// wrong.

import { useRef, useState } from 'react';
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
  // A ref rather than state, and this is the whole reason: `setSharing(true)`
  // does not take effect until React re-renders, so two touches landing in one
  // frame both read the old value and both open a sheet. A ref is written
  // before the await and read by the second press immediately.
  //
  // The test for this cannot tell the two apart — testing-library wraps every
  // press in act() and flushes state between them, so a state flag passes it
  // too. It is here because it is correct on a device, not because a test
  // proved it.
  const sharing = useRef(false);

  const onPress = async () => {
    if (sharing.current) return;
    sharing.current = true;
    // A failure belongs to the attempt that produced it, not to the next one.
    setFailed(false);
    try {
      await Share.share({ message: shareRouteMessage(route, t) });
    } catch {
      setFailed(true);
    } finally {
      // Released on both paths: a sheet that failed must not leave a dead
      // button behind it.
      sharing.current = false;
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
