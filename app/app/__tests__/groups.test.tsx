import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';

import GroupsScreen from '../profile/groups';
import { ApiError } from '../../src/api/client';
import { api } from '../../src/api/endpoints';

jest.mock('../../src/api/endpoints', () => ({
  api: {
    myGroups: jest.fn(),
    createGroup: jest.fn(),
    joinGroup: jest.fn(),
    leaveGroup: jest.fn(),
    rotateGroupCode: jest.fn(),
    groupTripsCSV: jest.fn(),
  },
}));

function wrap(node: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { mutations: { retry: false }, queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{node}</QueryClientProvider>;
}

beforeEach(() => {
  jest.clearAllMocks();
  (api.myGroups as jest.Mock).mockResolvedValue({
    groups: [
      { id: 'g1', name: 'ΔΠΘ', role: 'admin', members: 12, code: 'ACDEFGHJ' },
      { id: 'g2', name: 'Νοσοκομείο', role: 'member', members: 3 },
    ],
  });
});

// The code lets anybody in, so it is on the screen of the person who hands it
// out and nobody else's.
test('an admin sees the code and a member does not', async () => {
  render(wrap(<GroupsScreen />));
  await waitFor(() => expect(screen.getByTestId('group-code-g1')).toBeTruthy());
  expect(screen.getByText('ACDEFGHJ')).toBeTruthy();
  expect(screen.queryByTestId('group-code-g2')).toBeNull();
  expect(screen.queryByTestId('group-share-g2')).toBeNull();
  expect(screen.getByText('12 μέλη')).toBeTruthy();
});

test('a wrong code says it is wrong, not that something went wrong', async () => {
  (api.joinGroup as jest.Mock).mockRejectedValue(new ApiError(400, 'code_not_usable', 'no'));
  render(wrap(<GroupsScreen />));
  await waitFor(() => expect(screen.getByTestId('group-join-code')).toBeTruthy());

  fireEvent.changeText(screen.getByTestId('group-join-code'), 'wrongone');
  fireEvent.press(screen.getByTestId('group-join'));

  await waitFor(() => expect(api.joinGroup).toHaveBeenCalledWith('WRONGONE'));
  await waitFor(() =>
    expect(screen.getByTestId('groups-error').props.children).toBe(
      'Αυτός ο κωδικός δεν ανοίγει καμία ομάδα.',
    ),
  );
});

test('creating a group sends the name and clears the field', async () => {
  (api.createGroup as jest.Mock).mockResolvedValue({ id: 'g3', name: 'Γραφείο', role: 'admin', members: 1 });
  render(wrap(<GroupsScreen />));
  await waitFor(() => expect(screen.getByTestId('group-create-name')).toBeTruthy());

  fireEvent.changeText(screen.getByTestId('group-create-name'), 'Γραφείο');
  fireEvent.press(screen.getByTestId('group-create'));

  await waitFor(() => expect(api.createGroup).toHaveBeenCalledWith('Γραφείο'));
  await waitFor(() => expect(screen.getByTestId('group-create-name').props.value).toBe(''));
});

// The export is an admin's, like the code. A member has nothing to reconcile.
test('an admin can pull the month for the organisation and a member cannot', async () => {
  (api.groupTripsCSV as jest.Mock).mockResolvedValue('date,departure\n');
  const share = jest.spyOn(require('react-native').Share, 'share').mockResolvedValue({ action: 'sharedAction' });

  render(wrap(<GroupsScreen />));
  await waitFor(() => expect(screen.getByTestId('group-code-g1')).toBeTruthy());

  const buttons = screen.getAllByTestId(/^group-export-g1-/);
  expect(buttons).toHaveLength(2);
  expect(screen.queryAllByTestId(/^group-export-g2-/)).toHaveLength(0);

  fireEvent.press(buttons[0]);
  await waitFor(() => expect(api.groupTripsCSV).toHaveBeenCalledWith('g1', expect.stringMatching(/^\d{4}-\d{2}$/)));
  await waitFor(() => expect(share).toHaveBeenCalledWith(expect.objectContaining({ message: 'date,departure\n' })));
});
