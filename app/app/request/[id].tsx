import { useLocalSearchParams, useRouter } from 'expo-router';
import { StyleSheet, Text } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';

import { api } from '../../src/api/endpoints';
import { Button, Hint, Screen, Title, colors } from '../../src/components/ui';

export default function RequestScreen() {
  const { t } = useTranslation();
  const router = useRouter();
  const { id } = useLocalSearchParams<{ id: string }>();

  const cancel = useMutation({
    mutationFn: () => api.cancelRequest(id),
    onSuccess: () => router.replace('/(tabs)'),
  });

  return (
    <Screen>
      <Title>{t('search.asking')}</Title>
      <Hint>{t('search.noDrivers')}</Hint>
      <Text style={styles.id}>{id}</Text>
      <Button
        testID="cancel-request"
        label={t('common.cancel')}
        variant="secondary"
        onPress={() => cancel.mutate()}
        disabled={cancel.isPending}
      />
    </Screen>
  );
}

const styles = StyleSheet.create({
  id: { fontSize: 12, color: colors.muted },
});
