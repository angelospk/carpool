import { act, render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { VehiclePicker } from '../VehiclePicker';
import { api } from '../../api/endpoints';

const mockPush = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush }),
}));

jest.mock('../../api/endpoints', () => ({
  api: { vehicles: jest.fn() },
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

const yaris = {
  id: 'v1',
  name: 'Toyota Yaris',
  label: '',
  make: 'Toyota',
  model: 'Yaris',
  fuel: 'petrol_hybrid',
  colour: 'λευκό',
  plate: 'ΑΒΓ-1234',
  consumption_l_100km: 4.6,
  consumption_source: 'dataset' as const,
  consumption_capped: false,
  is_default: true,
};
const van = { ...yaris, id: 'v2', name: 'το βαν', label: 'το βαν', model: 'Ducato', is_default: false };

beforeEach(() => jest.clearAllMocks());

// One car is a fact rather than a question, and it used to be neither: the
// section hid itself, so the driver never saw which car they were publishing
// with or which plate the passenger would be told to look for.
test('one car is stated, with its colour and plate', async () => {
  (api.vehicles as jest.Mock).mockResolvedValue([yaris]);

  render(wrap(<VehiclePicker value={null} onChange={jest.fn()} testID="veh" />));

  expect(await screen.findByTestId('veh-only')).toBeTruthy();
  expect(screen.getByTestId('veh-detail').props.children).toContain('ΑΒΓ-1234');
});

test('two cars are offered, and choosing one reports it', async () => {
  (api.vehicles as jest.Mock).mockResolvedValue([yaris, van]);
  const onChange = jest.fn();

  render(wrap(<VehiclePicker value={null} onChange={onChange} testID="veh" />));

  expect(await screen.findByTestId('veh-v2')).toBeTruthy();
  fireEvent.press(screen.getByTestId('veh-v2'));
  expect(onChange).toHaveBeenCalledWith('v2');
});

// Until the driver picks, the trip goes out in whatever the server would have
// chosen anyway — so the default is shown as selected rather than nothing being.
test('the default reads as chosen before anything is pressed', async () => {
  (api.vehicles as jest.Mock).mockResolvedValue([yaris, van]);

  render(wrap(<VehiclePicker value={null} onChange={jest.fn()} testID="veh" />));

  const first = await screen.findByTestId('veh-v1');
  expect(first.props.accessibilityState?.selected).toBe(true);
  expect(screen.getByTestId('veh-v2').props.accessibilityState?.selected).toBe(false);
});

// A car retired from the garage while the form was open must not leave the
// section pointing at something that is no longer there.
test('a chosen car that has gone falls back to the default', async () => {
  (api.vehicles as jest.Mock).mockResolvedValue([yaris, van]);

  render(wrap(<VehiclePicker value="gone" onChange={jest.fn()} testID="veh" />));

  const first = await screen.findByTestId('veh-v1');
  expect(first.props.accessibilityState?.selected).toBe(true);
});

// An empty garage is the reason publishing is about to fail, so it is said out
// loud with the way to fix it, rather than the section disappearing.
test('an empty garage says so and offers the way to the garage', async () => {
  (api.vehicles as jest.Mock).mockResolvedValue([]);

  render(wrap(<VehiclePicker value={null} onChange={jest.fn()} testID="veh" />));

  fireEvent.press(await screen.findByTestId('veh-add'));
  expect(mockPush).toHaveBeenCalledWith('/profile/vehicles');
});

// A garage that did not load is not a garage with no cars in it, and telling a
// driver they own nothing because the network dropped is the worse of the two.
test('a failed load offers a retry rather than claiming an empty garage', async () => {
  (api.vehicles as jest.Mock).mockRejectedValue(new Error('offline'));

  render(wrap(<VehiclePicker value={null} onChange={jest.fn()} testID="veh" />));

  await waitFor(() => expect(screen.getByTestId('veh-retry')).toBeTruthy());
  await act(async () => {});
  expect(screen.queryByTestId('veh-add')).toBeNull();
});

// A car retired from the garage screen leaves the publish form holding an id
// the server will refuse — and the refusal reads "add your car first" to a
// driver who has two. The picker already drew the fallback; it has to tell the
// form the same thing, or the screen and the request disagree.
test('a car retired out from under the form is replaced in the form, not just on screen', async () => {
  (api.vehicles as jest.Mock).mockResolvedValue([yaris, van]);
  const onChange = jest.fn();

  render(wrap(<VehiclePicker testID="veh" value="v-gone" onChange={onChange} />));

  await waitFor(() => expect(onChange).toHaveBeenCalledWith('v1'));
});

// The ordinary case must stay quiet: a form that already names a car it owns is
// not corrected, and a re-render is not a change of mind.
test('a car that is still there is left alone', async () => {
  (api.vehicles as jest.Mock).mockResolvedValue([yaris, van]);
  const onChange = jest.fn();

  render(wrap(<VehiclePicker testID="veh" value="v2" onChange={onChange} />));

  await waitFor(() => expect(screen.getByTestId('veh-v2')).toBeTruthy());
  expect(onChange).not.toHaveBeenCalled();
});
