// The groups somebody is in, and the two ways into one: making it, or typing
// the code somebody handed over.
//
// The code is only on an admin's screen. Everybody holding it can let anybody
// in, so it is shown where it is needed to hand it out and nowhere else, and
// it can be replaced when it has travelled somewhere it should not have.

import { useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, type Group } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import { Badge, Button, Card, Field, Hint, Screen, SectionHeader, Title } from '../../src/components/ui';
import { colors, radius, space, type } from '../../src/design/tokens';
import { exportMonths, monthName } from '../../src/design/months';
import { saveCsv } from '../../src/components/saveCsv';
import { confirm } from '../../src/components/confirm';
import { shareText } from '../../src/components/shareText';
import { GroupStats } from '../../src/components/GroupStats';

export default function GroupsScreen() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const groups = useQuery({ queryKey: ['groups'], queryFn: () => api.myGroups() });

  const [name, setName] = useState('');
  const [code, setCode] = useState('');
  const [error, setError] = useState<string | null>(null);

  const refresh = () => void queryClient.invalidateQueries({ queryKey: ['groups'] });

  // One sentence per thing that can go wrong, because "something went wrong"
  // after typing a code tells nobody whether to try again.
  const explain = (e: unknown) => {
    const code = e instanceof ApiError ? e.code : '';
    const key =
      {
        invalid_name: 'groups.badName',
        code_not_usable: 'groups.badCode',
        too_many_attempts: 'groups.tooManyAttempts',
        too_many_groups: 'groups.tooMany',
      }[code] ?? 'common.error';
    setError(t(key));
  };

  const create = useMutation({
    mutationFn: () => api.createGroup(name.trim()),
    onSuccess: () => {
      setName('');
      setError(null);
      refresh();
    },
    onError: explain,
  });
  const join = useMutation({
    mutationFn: () => api.joinGroup(code.trim()),
    onSuccess: () => {
      setCode('');
      setError(null);
      refresh();
    },
    onError: explain,
  });
  const leave = useMutation({
    mutationFn: (id: string) => api.leaveGroup(id),
    onSuccess: refresh,
    onError: explain,
  });
  // The new code goes into the cache from the answer itself, so the old one is
  // never on screen to be shared after it stopped working.
  const rotate = useMutation({
    mutationFn: (id: string) => api.rotateGroupCode(id),
    onSuccess: ({ code }, id) => {
      queryClient.setQueryData<{ groups: Group[] }>(['groups'], (old) =>
        old ? { groups: old.groups.map((g) => (g.id === id ? { ...g, code } : g)) } : old,
      );
      refresh();
    },
    onError: explain,
  });

  // What an organisation pays against: confirmed trips between members, last
  // month or this one. See groups.Export on the server for what is left out.
  const exportTrips = useMutation({
    mutationFn: async ({ g, month }: { g: Group; month: string }) =>
      saveCsv(`trips-${month}.csv`, await api.groupTripsCSV(g.id, month)),
    onError: explain,
  });

  const confirmLeave = (g: Group) =>
    confirm({
      title: t('groups.leave'),
      message: t('groups.leaveConfirm', { name: g.name }),
      confirmLabel: t('groups.leave'),
      cancelLabel: t('common.cancel'),
      destructive: true,
      onConfirm: () => leave.mutate(g.id),
    });
  const confirmRotate = (g: Group) =>
    confirm({
      title: t('groups.newCode'),
      message: t('groups.newCodeConfirm'),
      confirmLabel: t('groups.newCode'),
      cancelLabel: t('common.cancel'),
      onConfirm: () => rotate.mutate(g.id),
    });

  const [notice, setNotice] = useState<string | null>(null);
  const shareCode = async (g: Group) => {
    setNotice(null);
    try {
      const how = await shareText(t('groups.shareMessage', { name: g.name, code: g.code }));
      if (how === 'copied') setNotice(t('share.copied'));
    } catch {
      setError(t('common.error'));
    }
  };

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.body} showsVerticalScrollIndicator={false}>
        <Title>{t('groups.title')}</Title>
        <Hint>{t('groups.intro')}</Hint>

        {notice ? <Hint testID="groups-notice">{notice}</Hint> : null}
        {error ? (
          <Text testID="groups-error" style={styles.error}>
            {error}
          </Text>
        ) : null}

        {groups.isLoading ? <Hint>{t('common.loading')}</Hint> : null}
        {groups.isError ? (
          <Button testID="groups-retry" label={t('common.retry')} onPress={() => void groups.refetch()} />
        ) : null}
        {groups.data?.groups.length === 0 ? <Hint testID="groups-none">{t('groups.none')}</Hint> : null}

        {groups.data?.groups.map((g) => (
          <Card key={g.id} testID={`group-${g.id}`}>
            <View style={styles.head}>
              <Text style={styles.name}>{g.name}</Text>
              {g.role === 'admin' ? <Badge label={t('groups.admin')} tone="primary" /> : null}
            </View>
            <Text style={styles.meta}>{t('groups.members', { count: g.members })}</Text>

            {/* The admin's view of the month. The code is on an admin's
                screen only, so its presence is the admin test used here too. */}
            {g.code ? <GroupStats groupId={g.id} /> : null}

            {g.code ? (
              <View style={styles.codeRow}>
                <Text style={styles.meta}>{t('groups.code')}</Text>
                <Text testID={`group-code-${g.id}`} selectable style={styles.code}>
                  {g.code}
                </Text>
              </View>
            ) : null}

            <View style={styles.actions}>
              {g.code ? (
                <>
                  <Button
                    testID={`group-share-${g.id}`}
                    label={t('groups.shareCode')}
                    variant="secondary"
                    disabled={rotate.isPending}
                    onPress={() => void shareCode(g)}
                  />
                  <Button
                    testID={`group-rotate-${g.id}`}
                    label={t('groups.newCode')}
                    variant="secondary"
                    disabled={rotate.isPending}
                    onPress={() => confirmRotate(g)}
                  />
                </>
              ) : null}
              {g.code
                ? exportMonths(new Date()).map((month) => (
                    <Button
                      key={month}
                      testID={`group-export-${g.id}-${month}`}
                      label={t('groups.export', { month: monthName(month) })}
                      variant="secondary"
                      disabled={exportTrips.isPending}
                      onPress={() => exportTrips.mutate({ g, month })}
                    />
                  ))
                : null}
              <Button
                testID={`group-leave-${g.id}`}
                label={t('groups.leave')}
                variant="danger"
                disabled={leave.isPending}
                onPress={() => confirmLeave(g)}
              />
            </View>
          </Card>
        ))}

        <SectionHeader title={t('groups.join')} />
        <Field
          testID="group-join-code"
          label={t('groups.code')}
          value={code}
          onChangeText={(v) => setCode(v.toUpperCase())}
          placeholder={t('groups.joinPlaceholder')}
          autoCapitalize="none"
        />
        <Button
          testID="group-join"
          label={t('groups.joinButton')}
          disabled={code.trim().length < 6 || join.isPending}
          onPress={() => join.mutate()}
        />

        <SectionHeader title={t('groups.create')} />
        <Field
          testID="group-create-name"
          label={t('groups.create')}
          value={name}
          onChangeText={setName}
          placeholder={t('groups.createPlaceholder')}
        />
        <Button
          testID="group-create"
          label={t('groups.createButton')}
          variant="secondary"
          disabled={name.trim().length < 2 || create.isPending}
          onPress={() => create.mutate()}
        />
      </ScrollView>
    </Screen>
  );
}

const styles = StyleSheet.create({
  body: { gap: space.md, paddingBottom: space.xl },
  head: { flexDirection: 'row', alignItems: 'center', gap: space.sm },
  name: { ...type.body, color: colors.ink, flex: 1 },
  meta: { ...type.caption, color: colors.muted },
  error: { ...type.body, color: colors.danger },
  codeRow: { flexDirection: 'row', alignItems: 'center', gap: space.sm, marginTop: space.sm },
  code: {
    ...type.mono,
    color: colors.ink,
    backgroundColor: colors.sunken,
    borderRadius: radius.inner,
    paddingHorizontal: space.md,
    paddingVertical: space.xs,
    letterSpacing: 2,
  },
  actions: { flexDirection: 'row', flexWrap: 'wrap', gap: space.sm, marginTop: space.md },
});
