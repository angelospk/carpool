import { render, screen, fireEvent } from '@testing-library/react-native';

import { PreferenceChips, PreferencePicker } from '../Preferences';
import { type PreferenceTag } from '../../api/endpoints';

describe('showing what a ride is like', () => {
  test('the words and the tags are both shown', () => {
    render(
      <PreferenceChips
        bio="Ξεκινάω νωρίς και σταματάω μία φορά για καφέ."
        tags={['no_smoking', 'music_on']}
        testID="pc"
      />,
    );

    expect(screen.getByTestId('pc-bio')).toBeTruthy();
    expect(screen.getByTestId('pc-no_smoking')).toBeTruthy();
    expect(screen.getByTestId('pc-music_on')).toBeTruthy();
  });

  // Somebody else's words, rendered as words. Nothing in a bio is markup, a
  // link, or anything the app should be interpreting.
  test('a bio is shown as plain text whatever is in it', () => {
    render(<PreferenceChips bio={'<b>κοίτα</b> https://example.com'} testID="pc" />);

    const bio = screen.getByTestId('pc-bio');
    expect(bio.props.children).toBe('<b>κοίτα</b> https://example.com');
  });

  // A profile with nothing filled in should take up no room rather than
  // leaving an empty box on the screen.
  test('an empty profile renders nothing at all', () => {
    render(<PreferenceChips tags={[]} bio={null} testID="pc" />);
    expect(screen.queryByTestId('pc')).toBeNull();
  });

  test('a missing list is the same as an empty one', () => {
    render(<PreferenceChips testID="pc" />);
    expect(screen.queryByTestId('pc')).toBeNull();
  });
});

describe('choosing your own', () => {
  function Picker({ initial }: { initial: PreferenceTag[] }) {
    const chosen: PreferenceTag[] = initial;
    return <PreferencePicker value={chosen} onChange={onChange} testID="pp" />;
  }
  const onChange = jest.fn();
  beforeEach(() => jest.clearAllMocks());

  test('tapping one adds it', () => {
    render(<Picker initial={[]} />);
    fireEvent.press(screen.getByTestId('pp-no_smoking'));
    expect(onChange).toHaveBeenCalledWith(['no_smoking']);
  });

  test('tapping a chosen one removes it', () => {
    render(<Picker initial={['no_smoking']} />);
    fireEvent.press(screen.getByTestId('pp-no_smoking'));
    expect(onChange).toHaveBeenCalledWith([]);
  });

  // The server refuses a contradictory pair. Being told that after tapping is
  // worse than the app simply doing what the tap obviously meant.
  test('picking one side of a pair drops the other rather than refusing', () => {
    render(<Picker initial={['no_smoking', 'chatty']} />);
    fireEvent.press(screen.getByTestId('pp-smoking_ok'));

    expect(onChange).toHaveBeenCalledWith(['chatty', 'smoking_ok']);
  });

  test('what is already chosen reads as chosen', () => {
    render(<Picker initial={['pets_ok']} />);
    expect(screen.getByTestId('pp-pets_ok').props.accessibilityState?.selected).toBe(true);
    expect(screen.getByTestId('pp-no_pets').props.accessibilityState?.selected).toBe(false);
  });
});
