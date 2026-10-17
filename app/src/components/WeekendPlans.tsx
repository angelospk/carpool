// «Απόδραση το Σαββατοκύριακο» on the home screen: the plans a passenger set
// once, the Thursday question, and the way in to make a new one.
//
// Nothing here asks a driver anything. The server does that each week; this is
// where the passenger hears the one question that is theirs, «Φεύγεις;», and
// where they stop a plan they no longer want.

import { useEffect, useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useRouter } from 'expo-router';

import { api, type WeekendPlan } from '../api/endpoints';
import { useSession } from '../state/session';
import { Badge, Button, Card } from './ui';
import { confirm } from './confirm';
import { colors, space, type } from '../design/tokens';
import { isAskedThisWeek, isoOf } from '../design/weekend';

/**
 * The caller's plans, asked the same way by the card and by the home screen's
 * empty state: a passenger whose only activity is a plan must not be told they
 * have nothing. Polled like the requests, because the state moves on the server.
 */
export function useWeekendPlans() {
  const signedIn = useSession((s) => s.status === 'signedIn');
  return useQuery({
    queryKey: ['weekend-plans'],
    queryFn: () => api.weekendPlans(),
    enabled: signedIn,
    refetchInterval: 30_000,
  });
}

/** The time now, ticking each minute so 18:00 on Thursday arrives on its own. */
function useNow(): Date {
  const [now, setNow] = useState(() => new Date());
  useEffect(() => {
    const timer = setInterval(() => setNow(new Date()), 60_000);
    return () => clearInterval(timer);
  }, []);
  return now;
}

export function WeekendPlans() {
  const { t } = useTranslation();
  const router = useRouter();
  const plans = useWeekendPlans();
  const now = useNow();

  return (
    <View testID="weekend-plans" style={styles.wrap}>
      {(plans.data?.plans ?? []).map((plan) => (
        <PlanCard key={plan.id} plan={plan} now={now} />
      ))}
      <Button
        testID="weekend-new"
        label={t('weekend.entry')}
        variant="secondary"
        onPress={() => router.push('/weekend/new')}
      />
    </View>
  );
}

function PlanCard({ plan, now }: { plan: WeekendPlan; now: Date }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [failed, setFailed] = useState(false);

  const refresh = () => {
    void queryClient.invalidateQueries({ queryKey: ['weekend-plans'] });
    void queryClient.invalidateQueries({ queryKey: ['requests'] });
  };
  const stop = useMutation({
    mutationFn: () => api.stopWeekendPlan(plan.id),
    onSuccess: refresh,
    onError: () => setFailed(true),
  });
  const going = useMutation({
    mutationFn: (yes: boolean) => api.weekendThisWeek(plan.id, yes),
    onSuccess: refresh,
    onError: () => setFailed(true),
  });
  const keep = useMutation({
    mutationFn: () => api.keepWeekendPlan(plan.id),
    onSuccess: refresh,
    onError: () => setFailed(true),
  });

  const day = t(`weekdaysShort.${isoOf(plan.depart_dow)}`);
  const returnDay = plan.return_dow === null ? null : t(`weekdaysShort.${isoOf(plan.return_dow)}`);

  // Asking to stop is the bigger question, and a plan that has been refused
  // three times is not best served by asking it «Φεύγεις;» again as well.
  const asksToStop = plan.ask_to_stop;
  const asksGoing = !asksToStop && isAskedThisWeek(plan, now);

  return (
    <Card testID={`weekend-plan-${plan.id}`}>
      <Text testID={`weekend-plan-${plan.id}-line`} style={styles.line}>
        {t('weekend.planLine', {
          from: plan.origin_label,
          to: plan.destination_label,
          day,
          time: plan.depart_time.slice(0, 5),
        })}
      </Text>
      <View style={styles.meta}>
        <Badge label={t(`weekend.state.${plan.this_week}`)} tone={plan.this_week === 'matched' ? 'success' : 'quiet'} />
        {returnDay && plan.return_time ? (
          <Text style={styles.caption}>
            {t('weekend.returnLine', { day: returnDay, time: plan.return_time.slice(0, 5) })}
          </Text>
        ) : null}
        {plan.every_week ? null : <Text style={styles.caption}>{t('weekend.onlyThisLine')}</Text>}
      </View>

      {asksToStop ? (
        <View testID={`weekend-stop-ask-${plan.id}`} style={styles.question}>
          <Text style={styles.ask}>{t('weekend.askStop')}</Text>
          <Text style={styles.caption}>{t('weekend.askStopBody')}</Text>
          <View style={styles.answers}>
            <View style={styles.answer}>
              <Button
                testID={`weekend-stop-yes-${plan.id}`}
                label={t('weekend.yesStop')}
                disabled={stop.isPending || keep.isPending}
                onPress={() => stop.mutate()}
              />
            </View>
            <View style={styles.answer}>
              <Button
                testID={`weekend-stop-no-${plan.id}`}
                label={t('weekend.no')}
                variant="secondary"
                disabled={stop.isPending || keep.isPending}
                onPress={() => keep.mutate()}
              />
            </View>
          </View>
        </View>
      ) : null}

      {asksGoing ? (
        <View testID={`weekend-going-ask-${plan.id}`} style={styles.question}>
          <Text style={styles.ask}>{t('weekend.thisWeek')}</Text>
          <View style={styles.answers}>
            <View style={styles.answer}>
              <Button
                testID={`weekend-going-yes-${plan.id}`}
                label={t('weekend.yes')}
                disabled={going.isPending}
                onPress={() => going.mutate(true)}
              />
            </View>
            <View style={styles.answer}>
              <Button
                testID={`weekend-going-no-${plan.id}`}
                label={t('weekend.notThisTime')}
                variant="secondary"
                disabled={going.isPending}
                onPress={() => going.mutate(false)}
              />
            </View>
          </View>
        </View>
      ) : null}

      {failed ? <Text style={styles.caption}>{t('common.error')}</Text> : null}

      <Button
        testID={`weekend-stop-${plan.id}`}
        label={t('weekend.stop')}
        variant="quietDanger"
        disabled={stop.isPending}
        onPress={() => {
          setFailed(false);
          confirm({
            title: t('weekend.stopTitle'),
            message: t('weekend.stopMessage'),
            confirmLabel: t('weekend.stop'),
            cancelLabel: t('common.cancel'),
            destructive: true,
            onConfirm: () => stop.mutate(),
          });
        }}
      />
    </Card>
  );
}

const styles = StyleSheet.create({
  wrap: { gap: space.sm },
  line: { ...type.heading, color: colors.ink },
  meta: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: space.sm, marginVertical: space.sm },
  caption: { ...type.caption, color: colors.muted },
  question: { gap: space.sm, marginBottom: space.sm },
  ask: { ...type.label, color: colors.ink },
  answers: { flexDirection: 'row', gap: space.sm },
  answer: { flex: 1 },
});
