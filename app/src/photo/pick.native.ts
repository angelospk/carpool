// Picking a photo on a phone.
//
// Three steps, and each one is there for a reason the web version does not
// have. The picker returns a path rather than bytes. The bytes behind it are
// several megabytes of HEIC or JPEG straight off a camera, against a server
// that takes 2 MiB and does not decode HEIC. And the file it hands back has to
// become something fetch will accept as a body.

import * as ImagePicker from 'expo-image-picker';
import { ImageManipulator, SaveFormat } from 'expo-image-manipulator';
import { File } from 'expo-file-system';

import { MAX_SIDE, QUALITY } from './limits';

export async function pickPhoto(): Promise<Blob | null> {
  const result = await ImagePicker.launchImageLibraryAsync({
    // One image. Videos and live photos have no meaning as an avatar, and
    // multiple selection would leave the caller choosing on the user's behalf.
    mediaTypes: ['images'],
    allowsMultipleSelection: false,
    // The EXIF block is not wanted anywhere in this flow, and the re-encode
    // below drops it regardless. Not asking for it is one less copy of the
    // coordinates in the process.
    exif: false,
  });
  if (result.canceled) return null;

  const asset = result.assets?.[0];
  if (!asset) return null;

  // Only ever downwards. Enlarging a small photo to MAX_SIDE would send more
  // bytes than were chosen and buy nothing back.
  const longest = Math.max(asset.width, asset.height);
  const context = ImageManipulator.manipulate(asset.uri);
  if (longest > MAX_SIDE) {
    const scale = MAX_SIDE / longest;
    context.resize({
      width: Math.max(1, Math.round(asset.width * scale)),
      height: Math.max(1, Math.round(asset.height * scale)),
    });
  }

  const image = await context.renderAsync();
  // Saved as JPEG whatever came in, which is what turns an iPhone's HEIC into
  // something the server can decode, and what leaves the location metadata
  // behind on the phone.
  const saved = await image.saveAsync({ format: SaveFormat.JPEG, compress: QUALITY });

  // expo-file-system's File implements Blob over a path, so the upload streams
  // from disk instead of the whole image being read into memory first.
  return new File(saved.uri) as unknown as Blob;
}
