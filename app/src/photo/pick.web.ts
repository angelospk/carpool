// Picking a photo in a browser.
//
// A hidden file input, which is the only way a page can open the file chooser,
// and it has to be triggered by the click that is already being handled or the
// browser blocks it. Nothing is appended to the document: click() works on a
// detached input, and an element added to the page is one more thing to remove
// on every path out of here.

import { MAX_SIDE, QUALITY } from './limits';

export async function pickPhoto(): Promise<Blob | null> {
  const file = await chooseFile();
  if (!file) return null;
  // A browser that cannot decode the file leaves it alone and lets the server
  // give the verdict, which is the same verdict either way.
  return (await shrink(file)) ?? file;
}

function chooseFile(): Promise<File | null> {
  return new Promise((resolve) => {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = 'image/jpeg,image/png,image/webp';

    // A cancelled chooser fires no event in most browsers, so without this the
    // promise never settles and the button stays showing "loading" forever.
    // 'cancel' is not universal either, which is why both are wired up.
    const done = (value: File | null) => {
      input.onchange = null;
      input.oncancel = null;
      resolve(value);
    };
    input.onchange = () => done(input.files?.[0] ?? null);
    input.oncancel = () => done(null);
    input.click();
  });
}

// Reduces the image to MAX_SIDE and re-encodes it as JPEG, so that what leaves
// the browser is the size the server keeps rather than whatever came off a
// camera. Returns null if anything about the decode fails, leaving the original
// to be uploaded and judged by the server.
async function shrink(file: Blob): Promise<Blob | null> {
  try {
    const bitmap = await createImageBitmap(file);
    try {
      const scale = Math.min(1, MAX_SIDE / Math.max(bitmap.width, bitmap.height));
      const width = Math.max(1, Math.round(bitmap.width * scale));
      const height = Math.max(1, Math.round(bitmap.height * scale));

      const canvas = document.createElement('canvas');
      canvas.width = width;
      canvas.height = height;
      const ctx = canvas.getContext('2d');
      if (!ctx) return null;
      // A transparent PNG drawn straight onto a canvas encodes its transparent
      // pixels as black in JPEG. White is what a face on a white card looks
      // like, and is the less alarming of the two.
      ctx.fillStyle = '#ffffff';
      ctx.fillRect(0, 0, width, height);
      ctx.drawImage(bitmap, 0, 0, width, height);

      return await new Promise<Blob | null>((resolve) => {
        canvas.toBlob((blob) => resolve(blob), 'image/jpeg', QUALITY);
      });
    } finally {
      bitmap.close();
    }
  } catch {
    return null;
  }
}
