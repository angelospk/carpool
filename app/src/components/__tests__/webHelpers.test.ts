import { Alert, Platform, Share } from 'react-native';

import { confirm } from '../confirm';
import { shareText } from '../shareText';

const originalOS = Platform.OS;
afterEach(() => {
  Object.defineProperty(Platform, 'OS', { value: originalOS, configurable: true });
  jest.restoreAllMocks();
});

function onWeb() {
  Object.defineProperty(Platform, 'OS', { value: 'web', configurable: true });
}

describe('confirm', () => {
  // Alert.alert does nothing in a browser, which left four buttons dead there.
  it('asks the browser on the web and acts only on yes', () => {
    onWeb();
    const act = jest.fn();
    const ask = jest.fn().mockReturnValueOnce(false).mockReturnValueOnce(true);
    (globalThis as { window?: unknown }).window = { confirm: ask };
    const alert = jest.spyOn(Alert, 'alert');

    const opts = { title: 'T', message: 'M', confirmLabel: 'OK', cancelLabel: 'No', onConfirm: act };
    confirm(opts);
    expect(act).not.toHaveBeenCalled();
    confirm(opts);
    expect(act).toHaveBeenCalledTimes(1);
    expect(alert).not.toHaveBeenCalled();
  });

  it('uses the native dialog on a phone', () => {
    const alert = jest.spyOn(Alert, 'alert').mockImplementation(() => {});
    confirm({ title: 'T', message: 'M', confirmLabel: 'OK', cancelLabel: 'No', onConfirm: jest.fn() });
    expect(alert).toHaveBeenCalledWith('T', 'M', expect.any(Array));
  });
});

describe('shareText', () => {
  it('copies on a browser that has no share sheet', async () => {
    onWeb();
    const writeText = jest.fn().mockResolvedValue(undefined);
    Object.defineProperty(globalThis, 'navigator', { value: { clipboard: { writeText } }, configurable: true });
    const share = jest.spyOn(Share, 'share');

    await expect(shareText('hello')).resolves.toBe('copied');
    expect(writeText).toHaveBeenCalledWith('hello');
    expect(share).not.toHaveBeenCalled();
  });

  it('opens the share sheet on a phone', async () => {
    const share = jest.spyOn(Share, 'share').mockResolvedValue({ action: 'sharedAction' });
    await expect(shareText('hello')).resolves.toBe('shared');
    expect(share).toHaveBeenCalledWith({ message: 'hello' });
  });
});
