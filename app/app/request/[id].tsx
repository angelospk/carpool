import { useLocalSearchParams, useRouter } from 'expo-router';
import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';

import { api } from '../../src/api/endpoints';
import { Button, Card, Hint, Screen, Title } from '../../src/components/ui';
import { colors, space, type } from '../../src/design/tokens';

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

      <Card>
        {/* Asking is sequential and one driver at a time, so waiting is the
            normal state here rather than a failure. */}
        <View style={styles.pulse}>
          <View style={styles.ring} />
          <View style={styles.core} />
        </View>
        <Hint>{t('search.noDrivers')}</Hint>
        <Text style={styles.id}>{id}</Text>
      </Card>

      <View style={styles.spacer} />

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
  pulse: { alignItems: 'center', justifyContent: 'center', height: 96 },
  ring: {
    position: 'absolute',
    width: 88,
    height: 88,
    borderRadius: 44,
    borderWidth: 2,
    borderColor: colors.accentSoft,
  },
  core: { width: 18, height: 18, borderRadius: 9, backgroundColor: colors.accent },
  id: { ...type.caption, fontSize: 11, color: colors.muted },
  spacer: { flex: 1 },
});
