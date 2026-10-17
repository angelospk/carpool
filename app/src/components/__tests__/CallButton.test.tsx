import { act, fireEvent, render, screen } from '@testing-library/react-native';
import { Linking } from 'react-native';

import { CallButton } from '../CallButton';

const NOW = new Date('2026-10-17T10:00:00Z');
const until = (ms: number) => new Date(NOW.getTime() + ms).toISOString();

describe('CallButton', () => {
  let open: jest.SpyInstance;
  beforeEach(() => {
    jest.useFakeTimers();
    jest.setSystemTime(NOW);
    open = jest.spyOn(Linking, 'openURL').mockResolvedValue(true);
  });
  afterEach(() => {
    open.mockRestore();
    jest.useRealTimers();
  });

  it('is offered before the window closes, and dials', () => {
    render(<CallButton phone="+306911112222" until={until(60_000)} />);

    fireEvent.press(screen.getByTestId('call'));
    expect(open).toHaveBeenCalledWith('tel:+306911112222');
  });

  it('goes away by itself when the window closes', () => {
    render(<CallButton phone="+306911112222" until={until(60_000)} />);
    expect(screen.queryByTestId('call')).toBeTruthy();

    act(() => {
      jest.advanceTimersByTime(60_000);
    });
    expect(screen.queryByTestId('call')).toBeNull();
  });

  it('is not offered when the window has already closed', () => {
    render(<CallButton phone="+306911112222" until={until(-1)} />);
    expect(screen.queryByTestId('call')).toBeNull();
  });

  it('does not dial a press that lands after the window closed', () => {
    render(<CallButton phone="+306911112222" until={until(60_000)} />);
    const button = screen.getByTestId('call');

    // The clock moves on without the re-render timer having run.
    jest.setSystemTime(new Date(NOW.getTime() + 61_000));
    fireEvent.press(button);
    expect(open).not.toHaveBeenCalled();
  });
});
