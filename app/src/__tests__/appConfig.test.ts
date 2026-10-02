import fs from 'fs';
import os from 'os';
import path from 'path';

// app.config.js adds the two things push needs, and only when they exist: a
// build without them must still come out, with push simply off.
const makeConfig = require('../../app.config.js');

const base = () => ({ name: 'carpool', android: { package: 'gr.carpool.app' }, extra: { apiUrl: 'x' } });

afterEach(() => {
  delete process.env.EAS_PROJECT_ID;
  delete process.env.GOOGLE_SERVICES_JSON;
});

test('without either, the config is what app.json says', () => {
  process.env.GOOGLE_SERVICES_JSON = '/nonexistent/google-services.json';
  const out = makeConfig({ config: base() });
  expect(out.extra.eas).toBeUndefined();
  expect(out.android.googleServicesFile).toBeUndefined();
});

test('the project id and the Firebase file go in when they are there', () => {
  const file = path.join(fs.mkdtempSync(path.join(os.tmpdir(), 'gs-')), 'google-services.json');
  fs.writeFileSync(file, '{}');
  process.env.EAS_PROJECT_ID = '11111111-2222-3333-4444-555555555555';
  process.env.GOOGLE_SERVICES_JSON = file;

  const out = makeConfig({ config: base() });

  expect(out.extra.eas.projectId).toBe('11111111-2222-3333-4444-555555555555');
  expect(out.extra.apiUrl).toBe('x');
  expect(out.android.googleServicesFile).toBe(file);
  expect(out.android.package).toBe('gr.carpool.app');
});
