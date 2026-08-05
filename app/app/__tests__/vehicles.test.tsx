import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import VehiclesScreen from '../profile/vehicles';
import { api } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn(), replace: jest.fn() }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: {
    vehicles: jest.fn(),
    addVehicle: jest.fn(),
    setDefaultVehicle: jest.fn(),
    retireVehicle: jest.fn(),
  },
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
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
const van = {
  ...yaris,
  id: 'v2',
  name: 'το βαν',
  label: 'το βαν',
  model: 'Ducato',
  fuel: 'diesel',
  is_default: false,
};

beforeEach(() => {
  jest.clearAllMocks();
  (api.vehicles as jest.Mock).mockResolvedValue([yaris, van]);
  (api.addVehicle as jest.Mock).mockResolvedValue(van);
  (api.setDefaultVehicle as jest.Mock).mockResolvedValue(undefined);
  (api.retireVehicle as jest.Mock).mockResolvedValue(undefined);
});

test('the cars are listed with what they are priced as drinking', async () => {
  render(wrap(<VehiclesScreen />));

  expect(await screen.findByTestId('garage-car-v1')).toBeTruthy();
  expect(screen.getByTestId('garage-car-v2')).toBeTruthy();
  // The number every contribution on this car comes from. Hiding it would mean
  // the driver cannot tell a measurement from a class floor.
  expect(screen.getAllByText(/4\.6/).length).toBe(2);
});

// Nowhere on this screen may somebody type what their car drinks. That is the
// distinction the whole pricing argument rests on.
test('a car is described, never rated', async () => {
  render(wrap(<VehiclesScreen />));
  fireEvent.press(await screen.findByTestId('garage-add'));

  fireEvent.changeText(screen.getByTestId('garage-make'), 'Fiat');
  fireEvent.changeText(screen.getByTestId('garage-model'), 'Ducato');
  fireEvent.press(screen.getByTestId('garage-fuel-diesel'));
  fireEvent.changeText(screen.getByTestId('garage-cc'), '2287');
  fireEvent.changeText(screen.getByTestId('garage-plate'), 'ΖΗΘ-9999');
  fireEvent.press(screen.getByTestId('garage-save'));

  await waitFor(() => expect(api.addVehicle).toHaveBeenCalled());
  const body = (api.addVehicle as jest.Mock).mock.calls[0][0];
  expect(body).toEqual(
    expect.objectContaining({ make: 'Fiat', model: 'Ducato', fuel: 'diesel', engine_cc: 2287 }),
  );
  expect(Object.keys(body)).not.toContain('consumption_l_100km');
});

test('a car cannot be saved without a make, a model and a plate', async () => {
  render(wrap(<VehiclesScreen />));
  fireEvent.press(await screen.findByTestId('garage-add'));

  const save = () => screen.getByTestId('garage-save');
  expect(save().props.accessibilityState?.disabled).toBe(true);

  fireEvent.changeText(screen.getByTestId('garage-make'), 'Fiat');
  fireEvent.changeText(screen.getByTestId('garage-model'), 'Ducato');
  expect(save().props.accessibilityState?.disabled).toBe(true);

  fireEvent.changeText(screen.getByTestId('garage-plate'), 'ΖΗΘ-9999');
  expect(save().props.accessibilityState?.disabled).toBe(false);
});

test('the default can be moved, and only from the one that is not it', async () => {
  render(wrap(<VehiclesScreen />));

  await screen.findByTestId('garage-car-v1');
  expect(screen.queryByTestId('garage-default-v1')).toBeNull();

  fireEvent.press(screen.getByTestId('garage-default-v2'));
  await waitFor(() => expect(api.setDefaultVehicle).toHaveBeenCalledWith('v2'));
});

// Retiring a car a published route still names would leave that route unable to
// be priced. The order is stated rather than the failure being generic.
test('a car still under a route says why it cannot go', async () => {
  (api.retireVehicle as jest.Mock).mockRejectedValue(
    new ApiError(409, 'vehicle_in_use', 'in use'),
  );

  render(wrap(<VehiclesScreen />));
  fireEvent.press(await screen.findByTestId('garage-retire-v2'));

  const problem = await screen.findByTestId('garage-problem');
  expect(problem.props.children).toMatch(/διαδρομή/i);
});

// The last car has no retire button: a driver with none cannot publish at all.
test('the only car cannot be retired', async () => {
  (api.vehicles as jest.Mock).mockResolvedValue([yaris]);

  render(wrap(<VehiclesScreen />));

  await screen.findByTestId('garage-car-v1');
  expect(screen.queryByTestId('garage-retire-v1')).toBeNull();
});
