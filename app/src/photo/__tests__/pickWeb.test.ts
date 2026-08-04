/**
 * @jest-environment jsdom
 */
// The browser picker.
//
// jsdom has no canvas and no createImageBitmap, so both are stood up here. That
// is not a reason to skip the tests: what is worth pinning is the arithmetic
// and the decisions around it — how far an image is scaled, which way the
// fallback goes when a decode fails, and that backing out settles the promise
// rather than leaving the button spinning forever.

import { pickPhoto } from '../pick.web';
import { MAX_SIDE, QUALITY } from '../limits';

type FakeInput = {
  type: string;
  accept: string;
  files: File[] | null;
  onchange: (() => void) | null;
  oncancel: (() => void) | null;
  click: () => void;
};

let input: FakeInput;
let canvas: { width: number; height: number; getContext: jest.Mock; toBlob: jest.Mock };
let drawn: { width: number; height: number } | null;
let filled: { style: string; width: number; height: number } | null;
let encoded: { type: string; quality: number } | null;

// What the chooser will do when opened: pick a file, or back out.
let onOpen: () => void;

const jpeg = (name = 'photo.jpg') => new File(['bytes'], name, { type: 'image/jpeg' });

beforeEach(() => {
  drawn = null;
  filled = null;
  encoded = null;

  canvas = {
    width: 0,
    height: 0,
    getContext: jest.fn(() => ({
      set fillStyle(value: string) {
        filled = { style: value, width: 0, height: 0 };
      },
      fillRect: (_x: number, _y: number, w: number, h: number) => {
        if (filled) filled = { ...filled, width: w, height: h };
      },
      drawImage: (_img: unknown, _x: number, _y: number, w: number, h: number) => {
        drawn = { width: w, height: h };
      },
    })),
    toBlob: jest.fn((cb: (b: Blob | null) => void, type: string, quality: number) => {
      encoded = { type, quality };
      cb(new Blob(['smaller'], { type }));
    }),
  };

  jest.spyOn(document, 'createElement').mockImplementation((tag: string) => {
    if (tag === 'canvas') return canvas as unknown as HTMLCanvasElement;
    input = {
      type: '',
      accept: '',
      files: null,
      onchange: null,
      oncancel: null,
      click: () => onOpen(),
    };
    return input as unknown as HTMLInputElement;
  });

  // A decoded image of whatever size the test asked for.
  (globalThis as { createImageBitmap?: unknown }).createImageBitmap = jest.fn(async () => ({
    width: bitmapSize.width,
    height: bitmapSize.height,
    close: jest.fn(),
  }));
});

let bitmapSize = { width: 0, height: 0 };

afterEach(() => jest.restoreAllMocks());

const choose = (file: File) => {
  onOpen = () => {
    input.files = [file];
    input.onchange?.();
  };
};

test('backing out of the chooser resolves rather than hanging', async () => {
  onOpen = () => input.oncancel?.();
  await expect(pickPhoto()).resolves.toBeNull();
});

test('a chooser that fires no event at all still settles on cancel', async () => {
  // Most browsers fire nothing when the dialog is dismissed; oncancel is the
  // one that covers it, and this is the case that used to hang.
  onOpen = () => input.oncancel?.();
  await expect(pickPhoto()).resolves.not.toBeUndefined();
});

test('a big landscape photo is scaled to the long side and re-encoded as JPEG', async () => {
  bitmapSize = { width: 4032, height: 3024 };
  choose(jpeg());

  await pickPhoto();

  expect(drawn).toEqual({ width: MAX_SIDE, height: 384 });
  expect(canvas.width).toBe(MAX_SIDE);
  expect(canvas.height).toBe(384);
  expect(encoded).toEqual({ type: 'image/jpeg', quality: QUALITY });
});

test('a portrait photo is scaled by its height, not its width', async () => {
  // The bug this catches is scaling on width alone, which leaves a tall photo
  // far larger than the limit and the upload rejected for size.
  bitmapSize = { width: 3024, height: 4032 };
  choose(jpeg());

  await pickPhoto();

  expect(drawn).toEqual({ width: 384, height: MAX_SIDE });
});

test('a photo already smaller than the limit is not enlarged', async () => {
  bitmapSize = { width: 120, height: 90 };
  choose(jpeg());

  await pickPhoto();

  expect(drawn).toEqual({ width: 120, height: 90 });
});

test('the canvas is filled before the image is drawn, so transparency is not black', async () => {
  bitmapSize = { width: 100, height: 100 };
  choose(new File(['bytes'], 'face.png', { type: 'image/png' }));

  await pickPhoto();

  expect(filled).toEqual({ style: '#ffffff', width: 100, height: 100 });
});

test('an image the browser cannot decode is sent as it is', async () => {
  // The server is the authority on what is a usable image. Refusing here would
  // reject something it would have accepted, and for a reason the person
  // cannot act on.
  (globalThis as { createImageBitmap?: unknown }).createImageBitmap = jest.fn(async () => {
    throw new Error('not an image');
  });
  const original = jpeg('odd.jpg');
  choose(original);

  await expect(pickPhoto()).resolves.toBe(original);
});
