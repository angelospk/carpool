import i18n from '../i18n';

import { shareRequestMessage } from './shareRequest';

const t = i18n.getFixedT('el');

const request = {
  origin_label: 'Αλεξανδρούπολη',
  destination_label: 'Κομοτηνή',
  // 07:00–08:00 Athens.
  window_start: '2026-09-29T04:00:00Z',
  window_end: '2026-09-29T05:00:00Z',
  seats: 1,
};

// The passenger's half of the village group: the driver's share says "I have
// seats", this says "I need one", in the same shape so the two read as a pair.
test('says where, when, and what to do about it', () => {
  const [line1, line2] = shareRequestMessage(request, t).split('\n');
  expect(line1).toMatch(/^Ψάχνω θέση · Αλεξανδρούπολη → Κομοτηνή · .+ · 07:00–08:00$/);
  expect(line2).toBe('Αν πηγαίνεις, στείλε μου μήνυμα.');
});

test('says how many when it is more than one', () => {
  expect(shareRequestMessage({ ...request, seats: 3 }, t)).toMatch(/^Ψάχνω 3 θέσεις ·/);
});

test('a missing place says less rather than leaving a gap', () => {
  const line1 = shareRequestMessage({ ...request, destination_label: '  ' }, t).split('\n')[0];
  expect(line1).toMatch(/^Ψάχνω θέση · Αλεξανδρούπολη · /);
  expect(line1).not.toContain('→');
});
