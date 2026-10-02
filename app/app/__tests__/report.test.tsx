import { render as rtlRender, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactElement } from 'react';

import UserScreen from '../user/[id]';
import { api } from '../../src/api/endpoints';

jest.mock('expo-router', () => ({
  useLocalSearchParams: () => ({ id: 'u-1', name: 'Γιάννης' }),
  useRouter: () => ({ push: jest.fn(), replace: jest.fn(), back: jest.fn() }),
}));

jest.mock('../../src/api/endpoints', () => ({
  api: { blockUser: jest.fn(), unblockUser: jest.fn(), reportUser: jest.fn() },
}));

// A fresh client per render, so a mutation left in flight by one test cannot
// resolve into the next one.
function render(node: ReactElement) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return rtlRender(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
}

beforeEach(() => {
  jest.clearAllMocks();
  (api.reportUser as jest.Mock).mockResolvedValue(undefined);
  (api.blockUser as jest.Mock).mockResolvedValue(undefined);
  (api.unblockUser as jest.Mock).mockResolvedValue(undefined);
});

describe('reporting somebody', () => {
  // The button used to file immediately, with the words "reported from the
  // app" as the reason. That is a report a moderator cannot act on and a tap
  // somebody cannot take back — and it spends one of their few daily reports.
  it('does not file on the first tap', async () => {
    render(<UserScreen />);

    fireEvent.press(screen.getByTestId('report-user'));

    expect(api.reportUser).not.toHaveBeenCalled();
    expect(await screen.findByTestId('report-reason-harassment')).toBeTruthy();
  });

  it('sends the chosen reason, not a placeholder', async () => {
    render(<UserScreen />);

    fireEvent.press(screen.getByTestId('report-user'));
    fireEvent.press(await screen.findByTestId('report-reason-no_show'));
    fireEvent.press(screen.getByTestId('report-send'));

    await waitFor(() => expect(api.reportUser).toHaveBeenCalled());
    const [id, reason] = (api.reportUser as jest.Mock).mock.calls[0];
    expect(id).toBe('u-1');
    // The category leads, so a moderator reading a queue can sort by it.
    expect(reason).toMatch(/^no_show/);
    expect(reason).not.toMatch(/reported from the app/);
  });

  it('carries what the person typed as well as the category', async () => {
    render(<UserScreen />);

    fireEvent.press(screen.getByTestId('report-user'));
    fireEvent.press(await screen.findByTestId('report-reason-other'));
    fireEvent.changeText(
      screen.getByTestId('report-detail'),
      'Με περίμενε μισή ώρα σε λάθος σημείο.',
    );
    fireEvent.press(screen.getByTestId('report-send'));

    await waitFor(() => expect(api.reportUser).toHaveBeenCalled());
    const [, reason] = (api.reportUser as jest.Mock).mock.calls[0];
    expect(reason).toContain('Με περίμενε μισή ώρα');
  });

  // "Other" with nothing typed is a report with no information in it, which is
  // the thing this screen exists to stop producing.
  it('will not send "other" with nothing said', async () => {
    render(<UserScreen />);

    fireEvent.press(screen.getByTestId('report-user'));
    fireEvent.press(await screen.findByTestId('report-reason-other'));

    expect(screen.getByTestId('report-send').props.accessibilityState?.disabled).toBe(true);
    fireEvent.press(screen.getByTestId('report-send'));
    expect(api.reportUser).not.toHaveBeenCalled();
  });

  it('can be backed out of without filing anything', async () => {
    render(<UserScreen />);

    fireEvent.press(screen.getByTestId('report-user'));
    fireEvent.press(await screen.findByTestId('report-reason-harassment'));
    fireEvent.press(screen.getByTestId('report-cancel'));

    expect(api.reportUser).not.toHaveBeenCalled();
    expect(screen.queryByTestId('report-send')).toBeNull();
  });

  // A daily limit somebody hit is not "something went wrong": it is a specific
  // thing they need to be told, because their report was not filed.
  it('says so when the daily limit is reached', async () => {
    const { ApiError } = jest.requireActual('../../src/api/client');
    (api.reportUser as jest.Mock).mockRejectedValue(
      new ApiError(429, 'report_rate_limited', 'too many'),
    );

    render(<UserScreen />);
    fireEvent.press(screen.getByTestId('report-user'));
    fireEvent.press(await screen.findByTestId('report-reason-harassment'));
    fireEvent.press(screen.getByTestId('report-send'));

    const outcome = await screen.findByTestId('user-outcome');
    expect(outcome.props.children).toMatch(/αναφορ/i);
  });
});

// Blocking is immediate and reversible, and it protects the person doing it
// right now. It deliberately does not get the same ceremony as a report.
it('blocks on one tap', async () => {
  render(<UserScreen />);

  fireEvent.press(screen.getByTestId('block-user'));

  await waitFor(() => expect(api.blockUser).toHaveBeenCalledWith('u-1'));
});

// Reversible was the promise; the undo was an endpoint nothing called.
it('a block can be undone from the same screen', async () => {
  render(<UserScreen />);

  fireEvent.press(screen.getByTestId('block-user'));
  fireEvent.press(await screen.findByTestId('unblock-user'));

  await waitFor(() => expect(api.unblockUser).toHaveBeenCalledWith('u-1'));
  expect(await screen.findByTestId('block-user')).toBeTruthy();
});
