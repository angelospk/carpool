import { render, screen } from '@testing-library/react-native';

import { SharedGroups } from '../SharedGroups';

describe('SharedGroups', () => {
  it('names every group the two people share', () => {
    render(<SharedGroups testID="shared" names={['ΔΠΘ', 'Νοσοκομείο']} />);
    expect(screen.getByText('Κοινή ομάδα: ΔΠΘ, Νοσοκομείο')).toBeTruthy();
  });

  // Nothing shared is not news about the other person, and a badge saying so
  // would read as a warning about somebody who has simply joined no group.
  it('says nothing at all when nothing is shared', () => {
    for (const names of [[], undefined]) {
      const { toJSON } = render(<SharedGroups testID="shared" names={names} />);
      expect(toJSON()).toBeNull();
    }
  });
});
