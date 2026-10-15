import { colors, fonts, type, initials } from './tokens';

// Mono Ticket is black on white with one lime accent. A colour creeping back in
// through the tokens would undo the whole system, so the palette is pinned.
test('the palette is ink on paper with one lime accent', () => {
  expect(colors).toEqual({
    ink: '#000000',
    paper: '#FFFFFF',
    surface: '#F2F2F2',
    line: '#D9D9D9',
    muted: '#6B6B6B',
    accent: '#C8F53C',
    onAccent: '#000000',
    danger: '#C4161C',
  });
});

// A role that sets fontWeight on top of a custom family makes Android and the
// browser synthesise a weight from the wrong file.
test('every type role names a Commissioner file and no weight', () => {
  const files = Object.values(fonts) as string[];
  for (const role of Object.values(type)) {
    expect(files).toContain(role.fontFamily);
    expect(role).not.toHaveProperty('fontWeight');
  }
  expect(type.time.fontFamily).toBe(fonts.black);
});

test('initials handle Greek names, one word, and nothing at all', () => {
  expect(initials('Χαράλαμπος Ροβάς')).toBe('ΧΡ');
  expect(initials('Μαρία')).toBe('Μ');
  expect(initials('  ')).toBe('·');
});
