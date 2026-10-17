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

/**
 * Asks which of two things to do, or neither. On the web, where a dialog has
 * only OK and Cancel, it asks twice: the first question, then the second.
 */
export function choose(opts: {
  title: string;
  message: string;
  cancelLabel: string;
  first: { label: string; web: string; onPress: () => void };
  second: { label: string; web: string; onPress: () => void };
}): void {
  if (Platform.OS === 'web') {
    if (typeof window === 'undefined') return;
    if (window.confirm(`${opts.title}\n\n${opts.second.web}`)) return opts.second.onPress();
    if (window.confirm(`${opts.title}\n\n${opts.first.web}`)) opts.first.onPress();
    return;
  }
  Alert.alert(opts.title, opts.message, [
    { text: opts.cancelLabel, style: 'cancel' },
    { text: opts.first.label, style: 'destructive', onPress: opts.first.onPress },
    { text: opts.second.label, style: 'destructive', onPress: opts.second.onPress },
  ]);
}
