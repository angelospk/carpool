import { readdirSync, readFileSync, statSync } from 'fs';
import { join } from 'path';

// Every screen the root Stack pushes needs a declared title. Without one,
// expo-router falls back to the route's own path, so the header of a trip reads
// "trip/[id]" — a file path, shown to a passenger, above their trip.
//
// This walks the routes rather than listing them, so the next dynamic route
// added fails here instead of shipping with a path for a name.
const appDir = join(__dirname, '..');
const layout = readFileSync(join(appDir, '_layout.tsx'), 'utf8');

const routeGroups = readdirSync(appDir).filter(
  (e) => statSync(join(appDir, e)).isDirectory() && !e.startsWith('_') && e !== '__tests__',
);

// Groups in parentheses carry their own layout and are declared as a unit.
const pushed = routeGroups.filter((e) => !e.startsWith('('));

describe('the root stack', () => {
  it('has routes to declare', () => {
    // Guards the walk itself: a rename that empties this list would otherwise
    // make every assertion below vacuously true.
    expect(pushed.length).toBeGreaterThan(0);
  });

  it.each(pushed)('gives %s a title of its own', (route) => {
    const declared = new RegExp(`name=["']${route}/`).test(layout);
    expect(declared).toBe(true);
  });

  it.each(routeGroups.filter((e) => e.startsWith('(')))('declares the %s group', (group) => {
    expect(layout).toContain(`name="${group}"`);
  });
});
