import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useRouter } from 'expo-router';

import { api } from '../src/api/endpoints';
import { detectionWindows } from '../src/drive/detection';
import { Button, Hint, Screen, Title } from '../src/components/ui';

/**
 * carpool://driving — where an iPhone Shortcuts automation lands when Driving
 * Focus or CarPlay turns on. It opens the driver's departure near now, where
 * «Φεύγω» is, or says there is none.
 */
export default function DrivingScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const [none, setNone] = useState(false);

  useEffect(() => {
    let live = true;
    void api
      .upcomingTrips()
      .then(({ trips }) => {
        const now = Date.now();
        const hit = detectionWindows(trips).find(([start, end]) => now >= start && now <= end);
        if (!live) return;
        if (hit) router.replace({ pathname: '/trip/[id]', params: { id: hit[2] } });
        else setNone(true);
      })
      .catch(() => live && setNone(true));
    return () => {
      live = false;
    };
  }, [router]);

  return (
    <Screen>
      <Title>{t('drive.driving')}</Title>
      {none ? (
        <>
          <Hint testID="driving-none">{t('detect.none')}</Hint>
          <Button label={t('common.back')} onPress={() => router.replace('/(tabs)')} />
        </>
      ) : null}
    </Screen>
  );
}
