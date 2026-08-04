// What a photo is reduced to before it is sent.
//
// These live apart from the pickers so both platforms answer to the same
// numbers, and so a test can read them rather than repeat them.

/**
 * The longest side a photo is reduced to before upload.
 *
 * It matches the server's own MaxSide, so nothing is uploaded that the server
 * would only throw away. On a device this is what makes the feature work at
 * all: a phone photo is several megabytes against a 2 MiB limit, and an iPhone
 * hands over HEIC, which the server's decoders do not read.
 *
 * Re-encoding on the device has a second effect worth saying out loud. The
 * server strips EXIF by re-encoding, but by then it has already received the
 * original — including where the photo was taken. Doing it here means those
 * coordinates never leave the phone.
 */
export const MAX_SIDE = 512;

/** JPEG quality for the re-encode. The server re-encodes again at 82. */
export const QUALITY = 0.85;

/**
 * The most the server will accept, matching its own MaxUpload.
 *
 * It is only consulted when the reduction above could not be done at all. Bytes
 * that are certain to be refused are worth refusing here, on a phone
 * connection, rather than after they have been sent.
 */
export const MAX_UPLOAD_BYTES = 2 * 1024 * 1024;

/**
 * Thrown when a chosen file cannot be made into something the server would
 * take. It lives here rather than in either picker so the screen can test for
 * it without importing a platform.
 */
export class TooBigError extends Error {}
