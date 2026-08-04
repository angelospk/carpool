import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import { Linking } from 'react-native';

import { SocialLinks } from '../../src/components/SocialLink';

const fb = 'https://facebook.com/giannis.k';
const ig = 'https://instagram.com/giannis.k';

test('a profile link is shown as the claim it is, not as a check we made', () => {
  render(<SocialLinks facebook={fb} instagram={null} />);

  expect(screen.getByTestId('social-facebook')).toBeTruthy();
  // The caveat is the point: nobody verified this profile belongs to them, and
  // a bare link would read as if somebody had.
  expect(screen.getByText(/επαληθε/)).toBeTruthy();
});

test('the handle is what you read, not the address', () => {
  // Two links to two platforms differ by one word in the middle of a URL. The
  // handle plus an icon is what tells them apart at a glance.
  render(<SocialLinks facebook={fb} instagram={ig} />);

  expect(screen.getAllByText('@giannis.k')).toHaveLength(2);
  expect(screen.queryByText(/facebook\.com\//)).toBeNull();
});

test('only the links that exist get a row', () => {
  render(<SocialLinks facebook={null} instagram={ig} />);

  expect(screen.queryByTestId('social-facebook')).toBeNull();
  expect(screen.getByTestId('social-instagram')).toBeTruthy();
});

test('no links means no rows at all', () => {
  const view = render(<SocialLinks facebook={null} instagram={null} />);

  expect(view.toJSON()).toBeNull();
});

test('tapping one opens that profile and nothing else', async () => {
  const open = jest.spyOn(Linking, 'openURL').mockResolvedValue(true);

  render(<SocialLinks facebook={fb} instagram={ig} />);
  fireEvent.press(screen.getByTestId('social-instagram'));

  await waitFor(() => expect(open).toHaveBeenCalledWith(ig));
  expect(open).not.toHaveBeenCalledWith(fb);
  open.mockRestore();
});
