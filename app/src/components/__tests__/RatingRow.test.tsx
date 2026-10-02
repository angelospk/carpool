import { render as rtlRender, screen } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactElement } from 'react';

import { RatingRow } from '../RatingRow';

jest.mock('../../api/endpoints', () => ({ api: { rateTrip: jest.fn() } }));

function wrap(node: ReactElement) {
  return <QueryClientProvider client={new QueryClient()}>{node}</QueryClientProvider>;
}

// Rated from another device: the next fetch says so, and the row on screen
// has to follow it rather than keep offering stars.
test('a row turns done when the server says it was rated', () => {
  const person = { id: 'drv', name: 'Γιώργος', photo_url: null };
  const { rerender } = rtlRender(
    wrap(<RatingRow tripId="t" person={person} rated={false} onRated={() => {}} />),
  );
  expect(screen.getByTestId('rate-drv-send')).toBeTruthy();

  rerender(wrap(<RatingRow tripId="t" person={person} rated onRated={() => {}} />));

  expect(screen.getByTestId('rate-drv-done')).toBeTruthy();
  expect(screen.queryByTestId('rate-drv-send')).toBeNull();
});
