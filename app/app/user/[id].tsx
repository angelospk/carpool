import { useState } from 'react';
import { Text } from 'react-native';
import { useLocalSearchParams } from 'expo-router';
import { useMutation } from '@tanstack/react-query';

import { api } from '../../src/api/endpoints';
import { Button, Screen, Title } from '../../src/components/ui';

export default function UserScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const [done, setDone] = useState<string | null>(null);

  const block = useMutation({
    mutationFn: () => api.blockUser(id),
    onSuccess: () => setDone('blocked'),
  });
  const report = useMutation({
    mutationFn: () => api.reportUser(id, 'reported from the app'),
    onSuccess: () => setDone('reported'),
  });

  return (
    <Screen>
      <Title>{id}</Title>
      {done ? <Text testID="user-outcome">{done}</Text> : null}
      <Button testID="block-user" label="Block" variant="danger" onPress={() => block.mutate()} />
      <Button
        testID="report-user"
        label="Report"
        variant="secondary"
        onPress={() => report.mutate()}
      />
    </Screen>
  );
}
