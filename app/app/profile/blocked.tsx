// The people somebody blocked, and the way back.
//
// A block could be undone only from the other person's profile, which is
// reached from a trip you shared — and a block is usually why there is no
// next trip. This list is the only other way in.

import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from '../../src/api/endpoints';
import { AsyncState, Avatar, Button, Card, Hint, Screen } from '../../src/components/ui';
import { colors, space, type } from '../../src/design/tokens';

export default function BlockedScreen() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const blocks = useQuery({ queryKey: ['blocks'], queryFn: () => api.myBlocks() });

  const unblock = useMutation({
    mutationFn: (id: string) => api.unblockUser(id),
    onSettled: () => void queryClient.invalidateQueries({ queryKey: ['blocks'] }),
  });

  const list = blocks.data?.blocked ?? [];

  return (
    <Screen>
      <AsyncState
        loading={blocks.isLoading}
        error={blocks.error}
        onRetry={() => void blocks.refetch()}
        empty={list.length === 0}
        emptySlot={<Hint testID="blocked-empty">{t('user.blockedEmpty')}</Hint>}
      >
        <Card>
          {list.map((p, i) => (
            <View key={p.id} style={[styles.row, i > 0 && styles.divided]}>
              <Avatar name={p.name} photoUrl={p.photo_url} size={40} />
              <Text style={styles.name} numberOfLines={1}>
                {p.name}
              </Text>
              <Button
                testID={`unblock-${p.id}`}
                label={t('user.unblock')}
                variant="secondary"
                disabled={unblock.isPending}
                onPress={() => unblock.mutate(p.id)}
              />
            </View>
          ))}
        </Card>
        {unblock.isError ? <Text style={styles.error}>{t('common.error')}</Text> : null}
      </AsyncState>
    </Screen>
  );
}

const styles = StyleSheet.create({
  row: { flexDirection: 'row', alignItems: 'center', gap: space.md, paddingVertical: space.sm },
  divided: { borderTopWidth: 1, borderTopColor: colors.line },
  name: { ...type.body, color: colors.ink, flex: 1 },
  error: { ...type.caption, color: colors.danger },
});
