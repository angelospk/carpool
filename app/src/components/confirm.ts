// Asks before doing something that cannot be taken back.
//
// Alert.alert is a no-op in react-native-web: the dialog never appears and the
// button behind it does nothing at all. So in a browser, cancelling a trip,
// retiring a route, leaving a group and replacing its code were all dead
// buttons. The browser's own confirm is plain, but it is a question that gets
// answered.

import { Alert, Platform } from 'react-native';

export function confirm(opts: {
  title: string;
  message: string;
  confirmLabel: string;
  cancelLabel: string;
  destructive?: boolean;
  onConfirm: () => void;
}): void {
  if (Platform.OS === 'web') {
    if (typeof window !== 'undefined' && window.confirm(`${opts.title}\n\n${opts.message}`)) {
      opts.onConfirm();
    }
    return;
  }
  Alert.alert(opts.title, opts.message, [
    { text: opts.cancelLabel, style: 'cancel' },
    {
      text: opts.confirmLabel,
      style: opts.destructive ? 'destructive' : 'default',
      onPress: opts.onConfirm,
    },
  ]);
}
