import i18n from '../i18n';

import { detourText } from './detour';

const t = i18n.getFixedT('el');

test('a passenger already on the road costs no detour, and says so', () => {
  expect(detourText(0, 0, t)).toBe('Χωρίς παράκαμψη');
});

test('a real detour is given in minutes and kilometres', () => {
  expect(detourText(6.4, 2.13, t)).toBe('+6 λεπτά, +2.1 χλμ');
});
