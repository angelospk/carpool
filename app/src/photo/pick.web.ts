// Picking a photo in a browser.
//
// A hidden file input, which is the only way a page can open the file chooser,
// and it has to be triggered by the click that is already being handled or the
// browser blocks it. Nothing is appended to the document: click() works on a
// detached input, and an element added to the page is one more thing to remove
// on every path out of here.

import { MAX_SIDE, MAX_UPLOAD_BYTES, QUALITY, TooBigError } from './limits';

export async function pickPhoto(): Promise<Blob | null> {
  const file = await chooseFile();
  if (!file) return null;

  const smaller = await shrink(file);
  if (smaller) return smaller;

  // The browser could not decode it. The server is the authority on what counts
  // as a usable image, so the original goes as it is and the server gives the
  // verdict — unless it is already past the size the server takes, in which
  // case sending it is a slow way of being told no.
  if (file.size > MAX_UPLOAD_BYTES) throw new TooBigError();
  return file;
}

function chooseFile(): Promise<File | null> {
  return new Promise((resolve) => {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = 'image/jpeg,image/png,image/webp';

    // Three things can call this and more than one of them will. A second
    // resolve on a settled promise is ignored, so there is no flag here to
    // guard against it — one would be code no test could tell the absence of.
    const done = (value: File | null) => {
      input.onchange = null;
      input.oncancel = null;
      window.removeEventListener('focus', onFocus);
      resolve(value);
    };

    // Dismissing the dialog fires nothing at all in older browsers, and
    // 'cancel' is recent enough that some people will not have it. Without a
    // third way out the promise never settles and the button reads "loading"
    // for the rest of the session.
    //
    // Closing the dialog returns focus to the window either way. The delay is
    // because a chosen file arrives just after that focus, not before it.
    const onFocus = () => {
      setTimeout(() => done(input.files?.[0] ?? null), cancelGrace);
    };

    input.onchange = () => done(input.files?.[0] ?? null);
    input.oncancel = () => done(null);
    window.addEventListener('focus', onFocus);
    input.click();
  });
}

// How long after the window regains focus to wait for a 'change' that may still
// be coming. Long enough not to race a slow browser, short enough that backing
// out does not feel like a hang.
const cancelGrace = 400;

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
