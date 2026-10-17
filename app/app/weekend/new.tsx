// «Απόδραση το Σαββατοκύριακο»: say once where you go most weekends.
//
// The plan is the whole of what the passenger does. Each week the server asks
// the drivers on the road and tells the passenger only when one says yes, so
// this screen asks for what a request asks for — the two ends, the day and
// hours, seats and bags — plus whether to come back and whether to repeat.

import { useRef, useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';
import { useRouter } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { newAttemptId } from '../../src/api/client';
import { api, type LuggageSize } from '../../src/api/endpoints';
import { refusalText } from '../../src/api/errorText';
import {
  BottomBar,
  Button,
  Card,
  Chip,
  Field,
  Hint,
  Screen,
  SectionHeader,
  Stepper,
  Title,
} from '../../src/components/ui';
import { PlaceField } from '../../src/components/PlaceField';
import { LuggagePicker } from '../../src/components/LuggagePicker';
import { colors, space, type } from '../../src/design/tokens';
import { pointLabel, type Pin } from '../../src/design/pin';
import { parseTime } from '../../src/design/when';
import { WEEKEND_DOWS, isoOf } from '../../src/design/weekend';

export default function NewWeekendPlanScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const queryClient = useQueryClient();

  const [from, setFrom] = useState<Pin | null>(null);
  const [to, setTo] = useState<Pin | null>(null);
  // Friday afternoon is the commonest way to leave town for the weekend.
  const [departDow, setDepartDow] = useState<number>(5);
  const [departTime, setDepartTime] = useState('15:00');
  const [leftDepart, setLeftDepart] = useState(false);
  // Null is no return: many leave and come back some other way.
  const [returnDow, setReturnDow] = useState<number | null>(null);
  const [returnTime, setReturnTime] = useState('18:00');
  const [leftReturn, setLeftReturn] = useState(false);
  const [seats, setSeats] = useState(1);
  const [luggage, setLuggage] = useState<LuggageSize>('none');
  const [luggageCount, setLuggageCount] = useState(0);
  // Every weekend is what the screen is for; «Μόνο αυτό» is the exception.
  const [everyWeek, setEveryWeek] = useState(true);
  const [outcome, setOutcome] = useState<string | null>(null);

  const departAt = parseTime(departTime);
  const returnAt = parseTime(returnTime);
  const departBad = departAt === null;
  const returnBad = returnDow !== null && returnAt === null;
  // The server refuses a return that is not after the outbound (invalid_return).
  const returnEarly =
    returnDow !== null &&
    departAt !== null &&
    returnAt !== null &&
    `${isoOf(returnDow)}${returnAt}` <= `${isoOf(departDow)}${departAt}`;

  // The key belongs to what is sent: a save sent again unchanged after a
  // timeout is the plan the first one made, not a second. If the passenger
  // edits the form after a failure, the first save may still have gone
  // through, and the old key would hand back that plan and call it this one.
  const attempt = useRef<{ key: string; payload: string } | null>(null);

  const save = useMutation({
    mutationFn: () => {
      const plan = {
        origin_lon: (from as Pin).lon,
        origin_lat: (from as Pin).lat,
        destination_lon: (to as Pin).lon,
        destination_lat: (to as Pin).lat,
        origin_label: pointLabel(from as Pin, null),
        destination_label: pointLabel(to as Pin, null),
        depart_dow: departDow,
        depart_time: departAt as string,
        ...(returnDow !== null ? { return_dow: returnDow, return_time: returnAt as string } : {}),
        seats,
        luggage,
        luggage_count: luggageCount,
        every_week: everyWeek,
      };
      const payload = JSON.stringify(plan);
      if (attempt.current?.payload !== payload) attempt.current = { key: newAttemptId(), payload };
      return api.createWeekendPlan({ ...plan, client_key: attempt.current.key });
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['weekend-plans'] });
      router.replace('/(tabs)');
    },
    onError: (e) => setOutcome(refusalText(e) ?? t('common.error')),
  });

  const days = (selected: number | null, onPick: (dow: number) => void, prefix: string) =>
    WEEKEND_DOWS.map((dow) => (
      <Chip
        key={dow}
        testID={`${prefix}-${dow}`}
        label={t(`weekdays.${isoOf(dow)}`)}
        selected={selected === dow}
        onPress={() => onPick(dow)}
      />
    ));

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.body} showsVerticalScrollIndicator={false}>
        <Title>{t('weekend.title')}</Title>
        <Hint>{t('weekend.intro')}</Hint>

        <Card>
          <PlaceField
            testID="weekend-from"
            label={t('search.from')}
            value={from}
            onChange={setFrom}
            placeholder={t('search.placeholder')}
            near={to}
            here
          />
          <PlaceField
            testID="weekend-to"
            label={t('search.to')}
            value={to}
            onChange={setTo}
            placeholder={t('search.placeholder')}
            near={from}
          />
        </Card>

        <SectionHeader title={t('weekend.outbound')} />
        <Card>
          <Text style={styles.label}>{t('weekend.outboundDay')}</Text>
          <View style={styles.days}>{days(departDow, setDepartDow, 'weekend-out-day')}</View>
          {/* Digits are enough: "1500", "15:00" and "3:00" all parse, as on the
              publish screen. */}
          <Field
            testID="weekend-out-time"
            label={t('weekend.from')}
            value={departTime}
            onChangeText={setDepartTime}
            onBlur={() => {
              setLeftDepart(true);
              setDepartTime((v) => parseTime(v) ?? v);
            }}
            selectTextOnFocus
            keyboardType="number-pad"
            compact
          />
          {leftDepart && departBad ? (
            <Text testID="weekend-out-time-error" style={styles.error}>
              {t('weekend.timeInvalid')}
            </Text>
          ) : null}
        </Card>

        <SectionHeader title={t('weekend.returnTitle')} />
        <Card>
          <View style={styles.days}>
            <Chip
              testID="weekend-return-none"
              label={t('weekend.noReturn')}
              selected={returnDow === null}
              onPress={() => setReturnDow(null)}
            />
            {days(returnDow, setReturnDow, 'weekend-return-day')}
          </View>
          {returnDow !== null ? (
            <>
              <Field
                testID="weekend-return-time"
                label={t('weekend.returnTime')}
                value={returnTime}
                onChangeText={setReturnTime}
                onBlur={() => {
                  setLeftReturn(true);
                  setReturnTime((v) => parseTime(v) ?? v);
                }}
                selectTextOnFocus
                keyboardType="number-pad"
                compact
              />
              {(leftReturn && returnBad) || returnEarly ? (
                <Text testID="weekend-return-error" style={styles.error}>
                  {returnEarly ? t('weekend.returnBefore') : t('weekend.timeInvalid')}
                </Text>
              ) : null}
            </>
          ) : null}
        </Card>

        <Card>
          <LuggagePicker
            testID="weekend-luggage"
            value={luggage}
            onChange={setLuggage}
            count={luggageCount}
            onCountChange={setLuggageCount}
          />
          <View style={styles.seats}>
            <Text style={styles.seatsLabel}>{t('search.seats')}</Text>
            <Stepper testID="weekend-seats" value={seats} onChange={setSeats} min={1} max={4} />
          </View>
        </Card>

        <Card>
          <View style={styles.days}>
            <Chip
              testID="weekend-every"
              label={t('weekend.everyWeek')}
              selected={everyWeek}
              onPress={() => setEveryWeek(true)}
            />
            <Chip
              testID="weekend-once"
              label={t('weekend.onlyThis')}
              selected={!everyWeek}
              onPress={() => setEveryWeek(false)}
            />
          </View>
        </Card>
      </ScrollView>
      <BottomBar bleed inset={false}>
        {outcome ? <Hint testID="weekend-outcome">{outcome}</Hint> : null}
        {!from || !to ? <Hint testID="weekend-needs-places">{t('weekend.needPlaces')}</Hint> : null}
        <Button
          testID="weekend-submit"
          label={t('weekend.save')}
          dot
          onPress={() => {
            setOutcome(null);
            save.mutate();
          }}
          disabled={!from || !to || departBad || returnBad || returnEarly || save.isPending}
        />
      </BottomBar>
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { gap: space.md, paddingBottom: space.xl },
  label: { ...type.caption, color: colors.muted },
  days: { flexDirection: 'row', flexWrap: 'wrap', gap: 6 },
  seats: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
  seatsLabel: { ...type.body, color: colors.ink },
  error: { ...type.caption, color: colors.danger },
});
