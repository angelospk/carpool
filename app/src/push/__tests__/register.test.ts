import { register, type PushDeps } from '../register';

function deps(over: Partial<PushDeps> = {}): PushDeps {
  return {
    isDevice: true,
    platform: 'android',
    signedIn: true,
    getPermissions: jest.fn().mockResolvedValue({ granted: true, canAskAgain: true }),
    requestPermissions: jest.fn().mockResolvedValue({ granted: true }),
    getToken: jest.fn().mockResolvedValue({ data: 'ExponentPushToken[abc]' }),
    send: jest.fn().mockResolvedValue(undefined),
    ...over,
  };
}

it('tells the server the token when permission is already given', async () => {
  const d = deps();

  const result = await register(d);

  expect(result).toEqual({ done: true, token: 'ExponentPushToken[abc]' });
  expect(d.send).toHaveBeenCalledWith('ExponentPushToken[abc]', 'android');
  expect(d.requestPermissions).not.toHaveBeenCalled();
});

it('asks for permission when it has not been asked before', async () => {
  const d = deps({
    getPermissions: jest.fn().mockResolvedValue({ granted: false, canAskAgain: true }),
  });

  await register(d);

  expect(d.requestPermissions).toHaveBeenCalled();
  expect(d.send).toHaveBeenCalled();
});

// The system prompt is one-shot on iOS. Asking again is not possible through
// it, and what is possible — our own dialog every launch — is how an app earns
// being muted rather than being read.
it('does not ask again once somebody has said no', async () => {
  const d = deps({
    getPermissions: jest.fn().mockResolvedValue({ granted: false, canAskAgain: false }),
  });

  const result = await register(d);

  expect(result).toEqual({ done: false, why: 'permission-denied' });
  expect(d.requestPermissions).not.toHaveBeenCalled();
  expect(d.send).not.toHaveBeenCalled();
});

it('sends nothing when permission is refused at the prompt', async () => {
  const d = deps({
    getPermissions: jest.fn().mockResolvedValue({ granted: false, canAskAgain: true }),
    requestPermissions: jest.fn().mockResolvedValue({ granted: false }),
  });

  expect(await register(d)).toEqual({ done: false, why: 'permission-denied' });
  expect(d.send).not.toHaveBeenCalled();
});

// Web and simulators have no Expo push token. Asking anyway throws, and a
// thrown error on launch is a worse bug than no notifications.
it.each([
  ['a simulator', { isDevice: false, platform: 'ios' }],
  ['the web', { isDevice: true, platform: 'web' }],
])('does not try on %s', async (_name, over) => {
  const d = deps(over);

  expect(await register(d)).toEqual({ done: false, why: 'not-a-device' });
  expect(d.getPermissions).not.toHaveBeenCalled();
});

it('does nothing for somebody who is not signed in', async () => {
  const d = deps({ signedIn: false });

  expect(await register(d)).toEqual({ done: false, why: 'signed-out' });
  expect(d.getPermissions).not.toHaveBeenCalled();
});

// The token comes from Expo's servers, so this fails on a bad connection and on
// a build without push credentials. Neither is worth interrupting somebody who
// opened the app to find a lift; the next launch tries again.
it('survives the token fetch failing', async () => {
  const boom = new Error('offline');
  const d = deps({ getToken: jest.fn().mockRejectedValue(boom) });

  expect(await register(d)).toEqual({ done: false, why: 'failed', error: boom });
});

it('survives the server rejecting the token', async () => {
  const boom = new Error('502');
  const d = deps({ send: jest.fn().mockRejectedValue(boom) });

  expect(await register(d)).toEqual({ done: false, why: 'failed', error: boom });
});
