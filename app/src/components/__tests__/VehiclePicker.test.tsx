import { act, render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import { VehiclePicker } from '../VehiclePicker';
import { api } from '../../api/endpoints';

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

// A driver with one car is not asked a question with one answer.
//
// The flush matters: without it the component is still waiting for the list, so
// it renders nothing whatever the rule is, and the assertion passes for a
// picker that would have shown one lonely chip.
test('one car means no choice to make', async () => {
  (api.vehicles as jest.Mock).mockResolvedValue([yaris]);
  const onChange = jest.fn();

  render(wrap(<VehiclePicker value={null} onChange={onChange} testID="veh" />));

  await waitFor(() => expect(api.vehicles).toHaveBeenCalled());
  await act(async () => {});
  expect(screen.queryByTestId('veh')).toBeNull();
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

// A driver who has not said what they drive cannot publish, and the picker is
// not the place to explain that — but it must not pretend there is a car.
test('an empty garage offers nothing', async () => {
  (api.vehicles as jest.Mock).mockResolvedValue([]);

  render(wrap(<VehiclePicker value={null} onChange={jest.fn()} testID="veh" />));

  await waitFor(() => expect(api.vehicles).toHaveBeenCalled());
  await act(async () => {});
  expect(screen.queryByTestId('veh')).toBeNull();
});
