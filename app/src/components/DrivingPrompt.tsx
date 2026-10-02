import { useEffect, useState } from 'react';
import { StyleSheet, Text } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useRouter } from 'expo-router';

import type { TripSummary } from '../api/endpoints';
import { detectionOn, drivingNow, syncDetectionWindows } from '../drive/detection';
import { colors, type } from '../design/tokens';
import { Button, Card } from './ui';

/**
 * «Φαίνεται ότι οδηγείς»: the home screen's guess that the driver is setting
 * off on one of their departures, with one button to the trip where «Φεύγω»
 * is. Only for a driver who turned detection on, only near a departure of
 * theirs, and only when the phone is confidently in a car. It also keeps the
 * Android watch's windows current, which costs nothing when detection is off.
 */
export function DrivingPrompt({ trips }: { trips: TripSummary[] }) {
  const { t } = useTranslation();
  const router = useRouter();
  const [tripId, setTripId] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    void (async () => {
      await syncDetectionWindows(trips);
      if (!(await detectionOn())) return;
      const id = await drivingNow(trips, Date.now());
      if (live) setTripId(id);
    })();
    return () => {
      live = false;
    };
  }, [trips]);

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
