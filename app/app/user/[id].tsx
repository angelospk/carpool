import { useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useLocalSearchParams } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useMutation, useQueryClient } from '@tanstack/react-query';

import { api } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import {
  Avatar,
  Button,
  Card,
  Chip,
  Field,
  Screen,
  Spacer,
} from '../../src/components/ui';
import { colors, space, type } from '../../src/design/tokens';

// What a report can be about.
//
// A fixed list rather than a blank box, because a moderator working a queue
// needs to sort it, and because "what happened" is a hard question to answer in
// a text field while you are still upset about it. The values are stable
// identifiers and lead the reason text, so the queue can be read by eye.
//
// They are not stored as a column. The reason has always been free text and a
// moderator reads it; a category column would be the right shape the day
// somebody actually filters the queue, and speculative structure before that is
// a migration for nobody. Noted in RISKS.md.
const REASONS = ['harassment', 'unsafe_driving', 'no_show', 'money', 'fake_profile', 'other'] as const;
type Reason = (typeof REASONS)[number];

export default function UserScreen() {
  const { t } = useTranslation();
  const { id, name } = useLocalSearchParams<{ id: string; name?: string }>();
  const [done, setDone] = useState<string | null>(null);

  // Null means the report form is closed. Opening it files nothing: the old
  // screen filed on the first tap, which spent one of somebody's few daily
  // reports on a mis-tap and sent a moderator the words "reported from the app".
  const [reason, setReason] = useState<Reason | null>(null);
  const [detail, setDetail] = useState('');
  const [reporting, setReporting] = useState(false);

  // Known only for this visit: the screen is opened from a trip, where the
  // block has not happened yet. Older blocks are undone from the block list.
  const [blocked, setBlocked] = useState(false);
  const queryClient = useQueryClient();
  const block = useMutation({
    mutationFn: () => api.blockUser(id),
    onSuccess: () => {
      setBlocked(true);
      setDone(t('user.blocked'));
      void queryClient.invalidateQueries({ queryKey: ['blocks'] });
    },
    onError: () => setDone(t('common.error')),
  });
  const unblock = useMutation({
    mutationFn: () => api.unblockUser(id),
    onSuccess: () => {
      setBlocked(false);
      setDone(t('user.unblocked'));
      void queryClient.invalidateQueries({ queryKey: ['blocks'] });
    },
    onError: () => setDone(t('common.error')),
  });

  const report = useMutation({
    mutationFn: () => api.reportUser(id, composeReason(reason as Reason, detail)),
    onSuccess: () => {
      close();
      setDone(t('user.reported'));
    },
    onError: (e) =>
      setDone(
        e instanceof ApiError && e.code === 'report_rate_limited'
          ? t('user.reportRateLimited')
          : t('common.error'),
      ),
  });

  const close = () => {
    setReporting(false);
    setReason(null);
    setDetail('');
  };

  // "Other" with nothing typed is a report with no information in it, which is
  // the thing this screen exists to stop producing.
  const sendable = reason !== null && (reason !== 'other' || detail.trim() !== '');

  return (
    <Screen>
      <Card>
        <View style={styles.identity}>
          <Avatar name={name ?? id} size={56} />
          <Text style={styles.name}>{name ?? id}</Text>
        </View>
      </Card>

      {done ? (
        <Text testID="user-outcome" style={styles.done}>
          {done}
        </Text>
      ) : null}

      {reporting ? (
        <Card>
          <Text style={styles.label}>{t('user.reportWhat')}</Text>
          <View style={styles.reasons}>
            {REASONS.map((r) => (
              <Chip
                key={r}
                testID={`report-reason-${r}`}
                label={t(`user.reasons.${r}`)}
                selected={reason === r}
                onPress={() => setReason(r)}
              />
            ))}
          </View>

          <Field
            testID="report-detail"
            label={t('user.reportDetail')}
            value={detail}
            onChangeText={setDetail}
            placeholder={t('user.reportDetailPlaceholder')}
            multiline
            valid={sendable}
          />

          <Text style={styles.hint}>{t('user.reportHint')}</Text>

          <Button
            testID="report-send"
            label={t('user.reportSend')}
            variant="danger"
            disabled={!sendable || report.isPending}
            onPress={() => sendable && report.mutate()}
          />
          <Button testID="report-cancel" label={t('common.cancel')} variant="secondary" onPress={close} />
        </Card>
      ) : null}

      <Spacer />

      {blocked ? (
        <Button
          testID="unblock-user"
          label={t('user.unblock')}
          variant="secondary"
          disabled={unblock.isPending}
          onPress={() => unblock.mutate()}
        />
      ) : (
        <Button
          testID="block-user"
          label={t('user.block')}
          variant="danger"
          disabled={block.isPending}
          onPress={() => block.mutate()}
        />
      )}
      {/* What a block does and does not do, said where it is pressed. */}
      <Text style={styles.hint}>{t('user.blockHint')}</Text>
      {!reporting ? (
        <Button
          testID="report-user"
          label={t('user.report')}
          variant="secondary"
          onPress={() => {
            setDone(null);
            setReporting(true);
          }}
        />
      ) : null}
    </Screen>
  );
}

// composeReason puts the category first so a queue sorts by eye, then whatever
// the person wrote. The category is an identifier rather than the Greek label:
// a moderator reading six months of reports should not have to notice that the
// wording changed in March.
function composeReason(reason: Reason, detail: string): string {
  const said = detail.trim();
  return said === '' ? reason : `${reason}: ${said}`;
}

const styles = StyleSheet.create({
  identity: { flexDirection: 'row', alignItems: 'center', gap: space.lg },
  name: { ...type.title, color: colors.ink },
  done: { ...type.body, color: colors.muted },
  label: { ...type.body, color: colors.ink },
  hint: { ...type.caption, color: colors.muted },
  reasons: { flexDirection: 'row', flexWrap: 'wrap', gap: space.xs },
});
