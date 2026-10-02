import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import * as Location from 'expo-location';

import { PlaceField } from '../PlaceField';
import { api } from '../../api/endpoints';

jest.mock('../../api/endpoints', () => ({
  api: { places: jest.fn(), placeNear: jest.fn() },
}));

jest.mock('expo-location', () => ({
  Accuracy: { Balanced: 3 },
  getForegroundPermissionsAsync: jest.fn(),
  requestForegroundPermissionsAsync: jest.fn(),
  getCurrentPositionAsync: jest.fn(),
  getLastKnownPositionAsync: jest.fn(),
}));

const soufli = { id: 'node/3', area: 'Σουφλίου', kind: 'town', name: 'Σουφλί', lon: 26.3, lat: 41.19 };

beforeEach(() => {
  (Location.getForegroundPermissionsAsync as jest.Mock).mockResolvedValue({ granted: false });
  (Location.requestForegroundPermissionsAsync as jest.Mock).mockResolvedValue({ granted: true });
  (Location.getLastKnownPositionAsync as jest.Mock).mockResolvedValue(null);
  (Location.getCurrentPositionAsync as jest.Mock).mockResolvedValue({
    coords: { longitude: 26.3051, latitude: 41.1958 },
  });
  (api.placeNear as jest.Mock).mockResolvedValue({ place: soufli });
});
afterEach(() => jest.clearAllMocks());

// «Από εδώ»: the start of a trip is usually where the phone already is, and
// typing the name of the town you are standing in is a step nobody should take.
describe('PlaceField «Από εδώ»', () => {
  it('fills the field with the settlement the phone is in', async () => {
    const onChange = jest.fn();
    render(<PlaceField testID="from" label="Από" value={null} onChange={onChange} here />);

    fireEvent.press(screen.getByTestId('from-here'));

    await waitFor(() =>
      expect(onChange).toHaveBeenCalledWith(
        expect.objectContaining({ label: 'Σουφλί', id: 'node/3', lon: 26.3, lat: 41.19 }),
      ),
    );
    expect(api.placeNear).toHaveBeenCalledWith(26.3051, 41.1958);
  });

  it('fills itself on opening when location is already allowed', async () => {
    (Location.getForegroundPermissionsAsync as jest.Mock).mockResolvedValue({ granted: true });
    const onChange = jest.fn();
    render(<PlaceField testID="from" label="Από" value={null} onChange={onChange} here />);

    await waitFor(() => expect(onChange).toHaveBeenCalledWith(expect.objectContaining({ label: 'Σουφλί' })));
    // Without asking: a permission prompt nobody tapped for is not offered.
    expect(Location.requestForegroundPermissionsAsync).not.toHaveBeenCalled();
  });

  it('does not ask for location on its own', async () => {
    const onChange = jest.fn();
    render(<PlaceField testID="from" label="Από" value={null} onChange={onChange} here />);

    await waitFor(() => expect(Location.getForegroundPermissionsAsync).toHaveBeenCalled());
    expect(Location.requestForegroundPermissionsAsync).not.toHaveBeenCalled();
    expect(onChange).not.toHaveBeenCalled();
  });

  it('says so when location is refused', async () => {
    (Location.requestForegroundPermissionsAsync as jest.Mock).mockResolvedValue({ granted: false });
    const onChange = jest.fn();
    render(<PlaceField testID="from" label="Από" value={null} onChange={onChange} here />);

    fireEvent.press(screen.getByTestId('from-here'));

    expect(await screen.findByTestId('from-here-failed')).toBeTruthy();
    expect(onChange).not.toHaveBeenCalled();
  });

  it('uses a recent fix without waiting for a new one', async () => {
    (Location.getLastKnownPositionAsync as jest.Mock).mockResolvedValue({
      coords: { longitude: 26.17, latitude: 40.89 },
    });
    const onChange = jest.fn();
    render(<PlaceField testID="from" label="Από" value={null} onChange={onChange} here />);

    fireEvent.press(screen.getByTestId('from-here'));

    await waitFor(() => expect(api.placeNear).toHaveBeenCalledWith(26.17, 40.89));
    expect(Location.getCurrentPositionAsync).not.toHaveBeenCalled();
  });

  it('is not offered on a field that did not ask for it', () => {
    render(<PlaceField testID="to" label="Προς" value={null} onChange={jest.fn()} />);
    expect(screen.queryByTestId('to-here')).toBeNull();
  });
});
