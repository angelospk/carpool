import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { Linking } from 'react-native';

import { SocialLink } from '../../src/components/SocialLink';

test('a profile link is shown as the claim it is, not as a check we made', () => {
  render(<SocialLink url="https://www.facebook.com/giannis.k" />);

  expect(screen.getByText('facebook.com/giannis.k')).toBeTruthy();
  // The caveat is the point: nobody verified this profile belongs to them, and
  // a bare link would read as if somebody had.
  expect(screen.getByText(/επαληθε/)).toBeTruthy();
});

test('the www prefix is dropped so the handle is what you read', () => {
  render(<SocialLink url="https://m.facebook.com/giannis.k" />);

  expect(screen.queryByText(/m\.facebook\.com/)).toBeNull();
  expect(screen.getByText('facebook.com/giannis.k')).toBeTruthy();
});

test('no link means no row at all', () => {
  const view = render(<SocialLink url={null} />);

  expect(view.toJSON()).toBeNull();
});

test('tapping it opens that profile and nothing else', async () => {
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(true);

  render(<SocialLink url="https://instagram.com/giannis.k" />);
  fireEvent.press(screen.getByTestId('social-link'));

  await waitFor(() => expect(open).toHaveBeenCalledWith('https://instagram.com/giannis.k'));
  open.mockRestore();
});
