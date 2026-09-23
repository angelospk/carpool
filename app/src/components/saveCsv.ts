// Hands a CSV to the person who asked for it.
//
// In a browser that is a download, which is where an admin reconciling a month
// is likely to be sitting. On a phone it goes to the share sheet as text, which
// reaches mail and messaging apps; a month of one group's trips is a few
// hundred lines at most, well inside what a share sheet carries.

import { Platform, Share } from 'react-native';

export async function saveCsv(filename: string, csv: string): Promise<void> {
  if (Platform.OS === 'web' && typeof document !== 'undefined') {
    const url = URL.createObjectURL(new Blob([csv], { type: 'text/csv;charset=utf-8' }));
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
    return;
  }
  await Share.share({ title: filename, message: csv });
}
