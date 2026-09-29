import { breakpoint, columnWidth } from './layout';

test('a phone is compact, a tablet medium, a laptop wide', () => {
  expect(breakpoint(375)).toBe('compact');
  expect(breakpoint(599)).toBe('compact');
  expect(breakpoint(600)).toBe('medium');
  expect(breakpoint(1023)).toBe('medium');
  expect(breakpoint(1024)).toBe('wide');
  expect(breakpoint(1920)).toBe('wide');
});

// A form on a 1400px window used to be a 1370px form. Reading width is capped;
// the wide column is for screens laid out in two.
test('columns are capped by what they hold, never wider than the window', () => {
  expect(columnWidth('narrow', 1400)).toBe(480);
  expect(columnWidth('normal', 1400)).toBe(680);
  expect(columnWidth('wide', 1400)).toBe(1120);
  expect(columnWidth('wide', 375)).toBe(375);
});
