import { useCallback, useEffect, useState } from 'react';
import { StyleSheet, Text } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useFocusEffect, useRouter } from 'expo-router';

import type { TripSummary } from '../api/endpoints';
import { detectionOn, drivingNow, syncDetectionWindows, watchDriving } from '../drive/detection';
import { useSession } from '../state/session';
import { colors, type } from '../design/tokens';
import { Button, Card } from './ui';

/**
 * «Φαίνεται ότι οδηγείς»: the home screen's guess that the driver is setting
 * off on one of their departures, with one button to the trip where «Φεύγω»
 * is. Only for a driver who turned detection on, only near a departure of
 * theirs, and only when the phone is confidently in a car.
 *
 * It keeps watching while the screen is open — standing by the car when the
 * screen first looked is not news, moving off a minute later is — and looks
 * again whenever the screen comes back into focus, which is how turning
 * detection on or off in Settings reaches it. It also keeps the Android
 * watch's windows current for this account, and empty for anybody else.
 */
export function DrivingPrompt({ trips }: { trips: TripSummary[] }) {
  const { t } = useTranslation();
  const router = useRouter();
  const account = useSession((s) => s.me?.id);
  const [tripId, setTripId] = useState<string | null>(null);
  const [focus, setFocus] = useState(0);
  useFocusEffect(useCallback(() => setFocus((n) => n + 1), []));

  useEffect(() => {
    // Nothing until the screen has been focused once: focusing is what runs
    // this, and running it on mount as well would start a check only to
    // throw it away.
    if (focus === 0) return;
    let live = true;
    let watch: { remove(): void } | null = null;
    void (async () => {
      await syncDetectionWindows(trips, account);
      if (!(await detectionOn(account))) {
        if (live) setTripId(null);
        return;
      }
      const now = await drivingNow(trips, Date.now());
      if (!live) return;
      setTripId(now);
      const w = await watchDriving(trips, Date.now(), (id) => live && setTripId(id));
      if (live) watch = w;
      else w?.remove();
    })();
    return () => {
      live = false;
      watch?.remove();
    };
  }, [trips, account, focus]);

  if (!tripId) return null;
  return (
    <Card testID="driving-prompt">
      <Text style={styles.title}>{t('detect.prompt')}</Text>
      <Button
        testID="driving-prompt-go"
        label={t('detect.promptGo')}
        onPress={() => router.push({ pathname: '/trip/[id]', params: { id: tripId } })}
      />
    </Card>
  );
}

const styles = StyleSheet.create({
  title: { ...type.title, color: colors.ink },
});
