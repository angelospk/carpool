import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { MeetingPointField } from '../MeetingPointField';
import { api } from '../../api/endpoints';

jest.mock('../../api/endpoints', () => ({
  api: { meetingPoints: jest.fn() },
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const alexandroupoli = {
  lon: 25.877,
  lat: 40.845,
  label: 'Αλεξανδρούπολη',
  id: 'node/139260064',
};

const ktel = {
  id: 'node/1',
  place_id: 'node/139260064',
  kind: 'bus_station',
  name: 'ΚΤΕΛ Έβρου',
  lon: 25.87,
  lat: 40.84,
};

beforeEach(() => jest.clearAllMocks());

test('nothing is asked or shown before a settlement is chosen', () => {
  render(wrap(<MeetingPointField testID="mp" place={null} value={null} onChange={jest.fn()} />));
  expect(api.meetingPoints).not.toHaveBeenCalled();
  expect(screen.queryByTestId('mp')).toBeNull();
});

test('the landmarks of the chosen settlement are offered', async () => {
  (api.meetingPoints as jest.Mock).mockResolvedValue([ktel]);

  render(
    wrap(<MeetingPointField testID="mp" place={alexandroupoli} value={null} onChange={jest.fn()} />),
  );

  expect(await screen.findByText('ΚΤΕΛ Έβρου')).toBeTruthy();
  expect(api.meetingPoints).toHaveBeenCalledWith('node/139260064');
});

// The decision the whole dataset is built around. Four settlements in five have
// no landmark anybody would recognise, and the honest answer is to say so
// rather than send somebody to a coordinate in a field.
test('a settlement with no landmark says so instead of inventing one', async () => {
  (api.meetingPoints as jest.Mock).mockResolvedValue([]);

  render(
    wrap(<MeetingPointField testID="mp" place={alexandroupoli} value={null} onChange={jest.fn()} />),
  );

  expect(await screen.findByTestId('mp-none')).toBeTruthy();
  expect(screen.queryByTestId('mp')).toBeNull();
});

test('choosing a landmark hands it up, and choosing it again lets go', async () => {
  (api.meetingPoints as jest.Mock).mockResolvedValue([ktel]);
  const onChange = jest.fn();

  const { rerender } = render(
    wrap(<MeetingPointField testID="mp" place={alexandroupoli} value={null} onChange={onChange} />),
  );
  fireEvent.press(await screen.findByTestId('mp-node/1'));
  expect(onChange).toHaveBeenCalledWith(ktel);

  rerender(
    wrap(<MeetingPointField testID="mp" place={alexandroupoli} value={ktel} onChange={onChange} />),
  );
  fireEvent.press(screen.getByTestId('mp-node/1'));
  await waitFor(() => expect(onChange).toHaveBeenLastCalledWith(null));
});

// A coordinate somebody typed belongs to no settlement, so there is nothing to
// look up and nothing to offer.
test('a typed coordinate is not asked about', () => {
  render(
    wrap(
      <MeetingPointField
        testID="mp"
        place={{ lon: 25.8, lat: 40.8, label: '25.8, 40.8' }}
        value={null}
        onChange={jest.fn()}
      />,
    ),
  );
  expect(api.meetingPoints).not.toHaveBeenCalled();
});

// F12: the header says the choice is optional, a label that repeats the town
// shows once, and a bare OSM abbreviation gets its kind.
test('the header says the choice is optional', async () => {
  (api.meetingPoints as jest.Mock).mockResolvedValue([ktel]);
  render(
    wrap(<MeetingPointField testID="mp" place={alexandroupoli} value={null} onChange={jest.fn()} />),
  );
  expect(await screen.findByText('Πού ακριβώς; (προαιρετικό)')).toBeTruthy();
});

test('a label that repeats the town shows the town once', async () => {
  (api.meetingPoints as jest.Mock).mockResolvedValue([
    { ...ktel, id: 'node/2', kind: 'port', name: 'Αλεξανδρούπολη, Αλεξανδρούπολη Λιμάνι' },
  ]);
  render(
    wrap(<MeetingPointField testID="mp" place={alexandroupoli} value={null} onChange={jest.fn()} />),
  );
  expect(await screen.findByText('Αλεξανδρούπολη Λιμάνι')).toBeTruthy();
  expect(screen.queryByText('Αλεξανδρούπολη, Αλεξανδρούπολη Λιμάνι')).toBeNull();
});

test('a bare abbreviation gets its kind, once', async () => {
  (api.meetingPoints as jest.Mock).mockResolvedValue([
    { ...ktel, id: 'node/3', kind: 'fuel', name: 'EKO' },
    { ...ktel, id: 'node/4', kind: 'parking', name: 'Α.Π.' },
    { ...ktel, id: 'node/5', kind: 'other', name: 'ΑΠ' },
  ]);
  render(
    wrap(<MeetingPointField testID="mp" place={alexandroupoli} value={null} onChange={jest.fn()} />),
  );
  expect(await screen.findByText('EKO · βενζινάδικο')).toBeTruthy();
  expect(screen.getByText('Α.Π. · πάρκινγκ')).toBeTruthy();
  // No kind worth saying: left as it is, with the generic label beside it.
  expect(screen.getByText('ΑΠ')).toBeTruthy();
  expect(screen.queryByText('βενζινάδικο')).toBeNull();
});
