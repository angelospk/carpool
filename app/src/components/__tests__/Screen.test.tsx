import { render, screen } from '@testing-library/react-native';
import { StyleSheet, Text } from 'react-native';
import * as SafeArea from 'react-native-safe-area-context';

import { Screen } from '../ui';

// The app draws edge to edge, so the bottom of every screen sits under the
// Android navigation bar. A button placed last on a screen — Next on sign-in,
// Publish, Confirm — was drawn behind it on a phone with three-button
// navigation, where the bar is 48 dp tall.
describe('Screen', () => {
  beforeEach(() => {
    jest
      .spyOn(SafeArea, 'useSafeAreaInsets')
      .mockReturnValue({ top: 24, bottom: 48, left: 0, right: 0 });
  });

  afterEach(() => jest.restoreAllMocks());

  const bottomOf = () => {
    const column = screen.getByText('last').parent?.parent;
    return StyleSheet.flatten(column?.props.style)?.paddingBottom ?? 0;
  };

  it('keeps its content clear of the navigation bar', () => {
    render(
      <Screen>
        <Text>last</Text>
      </Screen>,
    );
    expect(bottomOf()).toBeGreaterThanOrEqual(48);
  });

  it('does so on a screen with no padding as well', () => {
    render(
      <Screen padded={false}>
        <Text>last</Text>
      </Screen>,
    );
    expect(bottomOf()).toBeGreaterThanOrEqual(48);
  });
});
