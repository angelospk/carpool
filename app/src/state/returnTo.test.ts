import { rememberReturn, takeReturn } from './returnTo';

describe('returning to a route link after sign-in', () => {
  it('gives back a route link once', () => {
    rememberReturn('/r/oX_KGPs9D7RUAGAlz2ecvQ');
    expect(takeReturn()).toBe('/r/oX_KGPs9D7RUAGAlz2ecvQ');
    expect(takeReturn()).toBeNull();
  });

  it('keeps nothing that is not a route link', () => {
    for (const path of ['/profile', 'https://evil.example/r/abcdefghijklmnop', '/r/../../x', '/r/short']) {
      rememberReturn(path);
      expect(takeReturn()).toBeNull();
    }
  });
});
