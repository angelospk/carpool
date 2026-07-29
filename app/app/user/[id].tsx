import { useState } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { useLocalSearchParams } from 'expo-router';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';

import { api } from '../../src/api/endpoints';
import { Avatar, Button, Card, Screen } from '../../src/components/ui';
import { colors, space, type } from '../../src/design/tokens';

export default function UserScreen() {
  const { t } = useTranslation();
  const { id, name } = useLocalSearchParams<{ id: string; name?: string }>();
  const [done, setDone] = useState<string | null>(null);

  const block = useMutation({
    mutationFn: () => api.blockUser(id),
    onSuccess: () => setDone(t('user.blocked')),
  });
  const report = useMutation({
    mutationFn: () => api.reportUser(id, 'reported from the app'),
    onSuccess: () => setDone(t('user.reported')),
  });

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

      <View style={styles.spacer} />

      <Button
        testID="block-user"
        label={t('user.block')}
        variant="danger"
        onPress={() => block.mutate()}
      />
      <Button
        testID="report-user"
        label={t('user.report')}
        variant="secondary"
        onPress={() => report.mutate()}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  identity: { flexDirection: 'row', alignItems: 'center', gap: space.lg },
  name: { ...type.title, color: colors.ink },
  done: { ...type.body, color: colors.muted },
  spacer: { flex: 1 },
});
