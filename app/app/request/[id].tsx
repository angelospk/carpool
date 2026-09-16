import { useLocalSearchParams, useRouter } from 'expo-router';
import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';

import { api } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { requestProgress, useOpenRequests } from '../../src/components/OpenRequests';
import { Button, Card, Hint, Screen, Title } from '../../src/components/ui';
import { colors, space, type } from '../../src/design/tokens';
import { athensDay, athensTime } from '../../src/design/format';

export default function RequestScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id: string }>();
  const queryClient = useQueryClient();
  const [outcome, setOutcome] = useState<string | null>(null);
  // A driver took it. Different from an ordinary failure: there is nothing left
  // to retry here, and there is now a trip to go and look at.
  const [matched, setMatched] = useState(false);

  // The same list, and the same sentence, as the card on the home screen that
  // leads here. Without it this screen was a pulsing dot over a raw uuid — less
  // than the card said, which made opening it a step backwards.
  //
  // It polls, so the count of drivers who could not climbs while somebody sits
  // here. A missing list and a missing request are different news from a road
  // nobody drives: loading, failure and closed each get their own sentence, and
  // only a request on an empty road may claim anything about supply.
  const requests = useOpenRequests();
  const mine = requests.data?.requests.find((q) => q.id === id) ?? null;
  const loading = requests.isLoading || (requests.isPending && !requests.data);

  const cancel = useMutation({
    mutationFn: () => api.cancelRequest(id),
    onSuccess: () => {
      // The home screen lists what is still being waited on, and this is not
      // one of those any more.
      void queryClient.invalidateQueries({ queryKey: ['requests'] });
      router.replace('/(tabs)');
    },
    // Leaving the screen is what tells somebody they are not going, so it only
    // happens on an actual cancellation. This screen is open exactly while
    // drivers are being asked, so a driver accepting mid-press is ordinary
    // rather than rare, and the seat is then held in their name.
    onError: (e: unknown) => {
      if (e instanceof ApiError && e.code === 'already_matched') {
        setMatched(true);
        return setOutcome(t('request.alreadyMatched'));
      }
      // Anything else may well work on a second press, so the button stays.
      setOutcome(t('common.error'));
    },
  });

  return (
    <Screen>
      {/* This is a receipt for something that has been asked, not a dispatch in
          progress. The journey reads first and the pulse is a small mark beside
          the status line, rather than a countdown filling the screen. */}
      <Title>{t('request.title')}</Title>

      <Card>
        {loading ? (
          <Hint testID="request-loading">{t('common.loading')}</Hint>
        ) : requests.isLoadingError ? (
          // isLoadingError, not isError: the first load failed and there is
          // nothing to show. A poll that fails later sets isError too — 5.101.4
          // reports status 'error' with the data still in hand — and the list
          // polls every thirty seconds, so on isError alone one bad moment of
          // signal replaces the request a passenger is reading with this and
          // then puts it back.
          //
          // Measured twice. The first measurement said a query holding data
          // stays 'success' and this guard was removed as dead code; that
          // reading came from asking the observer before react-query had
          // notified it, which is also how the test for it passed. Waiting for
          // the observer gives status='error', isError=true,
          // isRefetchError=true, data intact.
          <Hint testID="request-failed">{t('home.requestsFailed')}</Hint>
        ) : mine ? (
          <>
            <Text testID="request-route" style={styles.route}>
              {mine.origin_label} → {mine.destination_label}
            </Text>
            <Text style={styles.when}>
              {athensDay(mine.window_start)} · {athensTime(mine.window_start)}–
              {athensTime(mine.window_end)}
            </Text>
            <View style={styles.status}>
              {/* Asking is sequential and one driver at a time, so waiting is
                  the normal state here rather than a failure. */}
              <View style={styles.core} />
              <Text testID="request-progress" style={styles.progress}>
                {requestProgress(mine, t)}
              </Text>
            </View>
          </>
        ) : (
          // Matched, cancelled or expired: it left the open list on purpose,
          // and it is no longer being waited on.
          <Hint testID="request-closed">{t('request.closed')}</Hint>
        )}
      </Card>

      {/* Nobody should sit here watching. Saying so is the difference between a
          waiting room and a thing you can walk away from. */}
      <Hint>{t('request.leaveOk')}</Hint>

      {outcome ? (
        <Text testID="request-outcome" style={styles.outcome}>
          {outcome}
        </Text>
      ) : null}

      <View style={styles.spacer} />

      {matched ? (
        <Button
          testID="request-go-to-trips"
          label={t('request.seeMyTrips')}
          onPress={() => router.replace('/(tabs)')}
        />
      ) : (
        <View style={styles.actions}>
          {/* Leaving is the ordinary thing to do, so it is the ordinary button.
              A bare "Ακύρωση" beside it would read as cancelling the screen. */}
          <Button
            testID="request-back-home"
            label={t('request.backHome')}
            onPress={() => router.replace('/(tabs)')}
          />
          {/* Cancelling needs something to cancel. The button used to be drawn
              whatever state the screen was in, so a request that had already
              been matched or called off still offered to cancel itself — a
              DELETE against something that is not open — and while the list was
              still loading it offered to cancel a request nobody could see. */}
          {mine ? (
            <Button
              testID="cancel-request"
              label={t('request.cancel')}
              variant="secondary"
              onPress={() => cancel.mutate()}
              disabled={cancel.isPending}
            />
          ) : null}
        </View>
      )}
    </Screen>
  );
}

const styles = StyleSheet.create({
  // Place names wrap rather than truncate: "Αλεξανδρούπολη → Πεπλος" cut at
  // one line is a journey nobody can check.
  route: { ...type.title, color: colors.ink },
  when: { ...type.body, color: colors.muted, marginTop: 2 },
  status: { flexDirection: 'row', alignItems: 'center', gap: space.xs, marginTop: space.md },
  core: { width: 10, height: 10, borderRadius: 5, backgroundColor: colors.accent },
  progress: { ...type.caption, color: colors.accent, fontWeight: '700', flex: 1 },
  outcome: { ...type.body, color: colors.danger },
  spacer: { flex: 1 },
  actions: { gap: space.sm },
});
