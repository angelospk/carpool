// Hands a CSV to the person who asked for it.
//
// In a browser that is a download, which is where an admin reconciling a month
// is likely to be sitting. On a phone it goes to the share sheet as text, which
// reaches mail and messaging apps; a month of one group's trips is a few
// hundred lines at most, well inside what a share sheet carries.

import { Platform, Share } from 'react-native';

export async function saveCsv(filename: string, csv: string): Promise<void> {
  if (Platform.OS === 'web' && typeof document !== 'undefined') {
    // The server sends a byte-order mark so a spreadsheet reads Greek as Greek;
    // kept, or added if something upstream stripped it.
    const body = csv.startsWith('\uFEFF') ? csv : '\uFEFF' + csv;
    const url = URL.createObjectURL(new Blob([body], { type: 'text/csv;charset=utf-8' }));
    const a = document.createElement('a');
    a.href = url;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    a.remove();
    // Revoked on the next tick: some browsers start the download after click()
    // returns, and a URL revoked first downloads nothing.
    setTimeout(() => URL.revokeObjectURL(url), 0);
    return;
  }
  await Share.share({ title: filename, message: csv });
}
