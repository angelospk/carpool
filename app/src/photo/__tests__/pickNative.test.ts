// The device picker.
//
// The three native modules are mocked, because there is no camera roll in a
// test runner. What is left to check is everything that is actually a decision:
// that only one image can be chosen, that a photo is only ever shrunk, and that
// whatever comes in leaves as JPEG — which is what makes an iPhone's HEIC
// readable by a server that does not decode it.

import { pickPhoto } from '../pick.native';
import { MAX_SIDE, QUALITY } from '../limits';

const mockLaunch = jest.fn();
const mockResize = jest.fn();
const mockSaveAsync = jest.fn();
const mockManipulate = jest.fn();

jest.mock('expo-image-picker', () => ({
  launchImageLibraryAsync: (...args: unknown[]) => mockLaunch(...args),
}));

jest.mock('expo-image-manipulator', () => ({
  SaveFormat: { JPEG: 'jpeg', PNG: 'png' },
  ImageManipulator: { manipulate: (...args: unknown[]) => mockManipulate(...args) },
}));

// A plain assignment rather than a TypeScript parameter property: the latter
// reads as an out-of-scope reference to jest's hoisting check and the whole
// suite refuses to run.
jest.mock('expo-file-system', () => ({
  File: class {
    uri: string;
    constructor(path: string) {
      this.uri = path;
    }
  },
}));

beforeEach(() => {
  jest.clearAllMocks();

  const context = {
    resize: (...args: unknown[]) => {
      mockResize(...args);
      return context;
    },
    renderAsync: async () => ({ saveAsync: mockSaveAsync }),
  };
  mockManipulate.mockReturnValue(context);
  mockSaveAsync.mockResolvedValue({ uri: 'file:///cache/out.jpg' });
});

const picked = (width: number, height: number) => ({
  canceled: false,
  assets: [{ uri: 'file:///DCIM/IMG_0001.HEIC', width, height }],
});

test('the picker is asked for exactly one image', async () => {
  mockLaunch.mockResolvedValue({ canceled: true, assets: null });

  await pickPhoto();

  expect(mockLaunch).toHaveBeenCalledWith(
    expect.objectContaining({ mediaTypes: ['images'], allowsMultipleSelection: false }),
  );
});

test('backing out returns nothing', async () => {
  mockLaunch.mockResolvedValue({ canceled: true, assets: null });
  await expect(pickPhoto()).resolves.toBeNull();
});

test('a result with no asset returns nothing rather than crashing', async () => {
  // canceled: false with an empty assets array is a shape the picker is typed
  // to allow, and reading [0] of it blindly is a crash on somebody's phone.
  mockLaunch.mockResolvedValue({ canceled: false, assets: [] });
  await expect(pickPhoto()).resolves.toBeNull();
});

test('a camera photo is scaled down to the long side', async () => {
  mockLaunch.mockResolvedValue(picked(4032, 3024));

  await pickPhoto();

  expect(mockResize).toHaveBeenCalledWith({ width: MAX_SIDE, height: 384 });
});

test('a portrait photo is scaled by its height', async () => {
  mockLaunch.mockResolvedValue(picked(3024, 4032));

  await pickPhoto();

  expect(mockResize).toHaveBeenCalledWith({ width: 384, height: MAX_SIDE });
});

test('a photo already small enough is not resized at all', async () => {
  mockLaunch.mockResolvedValue(picked(200, 150));

  await pickPhoto();

  expect(mockResize).not.toHaveBeenCalled();
});

test('whatever was chosen is saved as JPEG', async () => {
  // The asset above is HEIC, which the server's decoders do not read. Without
  // this the feature fails on every iPhone and works on every Android.
  mockLaunch.mockResolvedValue(picked(4032, 3024));

  const file = await pickPhoto();

  expect(mockSaveAsync).toHaveBeenCalledWith({ format: 'jpeg', compress: QUALITY });
  expect(file).toMatchObject({ uri: 'file:///cache/out.jpg' });
});
