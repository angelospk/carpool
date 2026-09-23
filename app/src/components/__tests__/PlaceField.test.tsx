import { render, screen, fireEvent, waitFor, act, within } from '@testing-library/react-native';

import { PlaceField } from '../PlaceField';
import { api } from '../../api/endpoints';

jest.mock('../../api/endpoints', () => ({
  api: { places: jest.fn() },
}));

const evros = [
  { id: 'node/1', area: 'Αλεξανδρούπολης', kind: 'city', name: 'Αλεξανδρούπολη', lon: 25.8743, lat: 40.8476 },
  { id: 'node/2', area: 'Ορεστιάδας', kind: 'town', name: 'Ορεστιάδα', lon: 26.5297, lat: 41.5039 },
];

const setup = () => {
  const onChange = jest.fn();
  render(<PlaceField testID="from" label="Από" value={null} onChange={onChange} />);
  return onChange;
};

const type = async (text: string) => {
  fireEvent.changeText(screen.getByTestId('from-input'), text);
  await act(async () => {
    jest.advanceTimersByTime(400);
  });
};

beforeEach(() => {
  jest.useFakeTimers();
  (api.places as jest.Mock).mockResolvedValue({ places: evros });
});
afterEach(() => {
  jest.useRealTimers();
  jest.clearAllMocks();
});

describe('PlaceField', () => {
  it('waits for a pause before asking, so a typed word is one request', async () => {
    setup();
    const input = screen.getByTestId('from-input');
    for (const text of ['α', 'αλ', 'αλε', 'αλεξ']) {
      fireEvent.changeText(input, text);
      jest.advanceTimersByTime(50);
    }
    expect(api.places).not.toHaveBeenCalled();

    await act(async () => {
      jest.advanceTimersByTime(400);
    });
    expect(api.places).toHaveBeenCalledTimes(1);
    expect(api.places).toHaveBeenCalledWith('αλεξ');
  });

  it('gives back the place that was chosen, coordinates and all', async () => {
    const onChange = setup();
    await type('αλεξ');
    await waitFor(() => expect(screen.getByText('Αλεξανδρούπολη')).toBeTruthy());

    fireEvent.press(screen.getByText('Αλεξανδρούπολη'));
    expect(onChange).toHaveBeenCalledWith({
      lon: 25.8743,
      lat: 40.8476,
      label: 'Αλεξανδρούπολη',
      // Carried so the field below can offer that settlement's meeting points.
      // A typed coordinate has no id, which is how the two are told apart.
      id: 'node/1',
    });
  });

  it('forgets the choice as soon as the text is edited', async () => {
    // The invariant that matters. If the field kept the old coordinate while
    // showing new text, a passenger would be picked up somewhere they did not
    // ask for and nothing on screen would say so.
    const onChange = setup();
    await type('αλεξ');
    await waitFor(() => expect(screen.getByText('Αλεξανδρούπολη')).toBeTruthy());
    fireEvent.press(screen.getByText('Αλεξανδρούπολη'));
    onChange.mockClear();

    fireEvent.changeText(screen.getByTestId('from-input'), 'Αλεξανδρούπολ');
    expect(onChange).toHaveBeenCalledWith(null);
  });

  it('ignores an answer to a question that is no longer being asked', async () => {
    // Two requests in flight, the first one slower. Without a guard the stale
    // list lands last and the user picks from suggestions for text they have
    // already replaced.
    let resolveSlow!: (v: unknown) => void;
    (api.places as jest.Mock)
      .mockReturnValueOnce(new Promise((r) => (resolveSlow = r)))
      .mockResolvedValueOnce({ places: [evros[1]] });

    setup();
    await type('αλεξ');
    await type('ορε');
    await waitFor(() => expect(screen.getByText('Ορεστιάδα')).toBeTruthy());

    await act(async () => {
      resolveSlow({ places: [evros[0]] });
    });
    expect(screen.queryByText('Αλεξανδρούπολη')).toBeNull();
  });

  it('still takes a raw coordinate, without asking the server about it', async () => {
    // The pilot was run this way and the format is in older notes, so it keeps
    // working. Nothing is looked up: it is already a location.
    const onChange = setup();
    await type('25.8743, 40.8476 Σπίτι');

    expect(api.places).not.toHaveBeenCalled();
    expect(onChange).toHaveBeenCalledWith({ lon: 25.8743, lat: 40.8476, label: 'Σπίτι' });
  });

  it('does not reopen the list with an answer that arrived after the choice', async () => {
    // A request is still in flight when the user taps a suggestion. Without a
    // guard its answer lands afterwards and the list springs back open over a
    // field that is already filled in, inviting a second, contradictory choice.
    let resolveSlow!: (v: unknown) => void;
    (api.places as jest.Mock).mockReturnValueOnce(new Promise((r) => (resolveSlow = r)));

    setup();
    await type('αλεξ');
    await act(async () => {
      resolveSlow({ places: evros });
    });
    await waitFor(() => expect(screen.getByText('Αλεξανδρούπολη')).toBeTruthy());

    // Choose, then let a second, older request answer.
    let resolveOlder!: (v: unknown) => void;
    (api.places as jest.Mock).mockReturnValueOnce(new Promise((r) => (resolveOlder = r)));
    fireEvent.changeText(screen.getByTestId('from-input'), 'ορε');
    await act(async () => {
      jest.advanceTimersByTime(400);
    });
    fireEvent.press(screen.getByText('Αλεξανδρούπολη'));

    await act(async () => {
      resolveOlder({ places: evros });
    });
    expect(screen.queryByTestId('from-suggestions')).toBeNull();
  });

  it('does not fill an emptied field with an answer to what used to be in it', async () => {
    // Clearing the field ends the question it was asking. Without that, the
    // slower answer lands afterwards and opens a list over an empty box.
    let resolveSlow!: (v: unknown) => void;
    (api.places as jest.Mock).mockReturnValueOnce(new Promise((r) => (resolveSlow = r)));

    setup();
    await type('αλεξ');
    fireEvent.changeText(screen.getByTestId('from-input'), '');
    await act(async () => {
      resolveSlow({ places: evros });
    });

    expect(screen.queryByTestId('from-suggestions')).toBeNull();
  });

  it('says when it found nothing, rather than showing an empty box', async () => {
    (api.places as jest.Mock).mockResolvedValue({ places: [] });
    setup();
    await type('zzzz');
    await waitFor(() => expect(screen.getByTestId('from-empty')).toBeTruthy());
  });

  it('survives the server being unreachable', async () => {
    (api.places as jest.Mock).mockRejectedValue(new Error('offline'));
    const onChange = setup();
    await type('αλεξ');
    // No crash, no suggestions, and nothing selected on the caller's behalf.
    expect(screen.queryByTestId('from-suggestions')).toBeNull();
    expect(onChange).not.toHaveBeenCalledWith(expect.objectContaining({ lon: expect.anything() }));
  });

  it('shows each suggestion its municipality, so two of the same name differ', async () => {
    // Greece has dozens of Άγιος Γεώργιος. A list that shows the name alone is
    // a list of identical rows, which is no more useful than no list at all.
    (api.places as jest.Mock).mockResolvedValue({
      places: [
        { id: 'node/10', area: 'Ανατολικής Μάνης', kind: 'village', name: 'Άγιος Γεώργιος', lon: 22.4, lat: 36.7 },
        { id: 'node/11', area: 'Γρεβενών', kind: 'village', name: 'Άγιος Γεώργιος', lon: 21.4, lat: 40.1 },
      ],
    });
    setup();
    await type('αγιος γεωργιος');

    expect(within(screen.getByTestId('from-node/10')).getByText(/Ανατολικής Μάνης/)).toBeTruthy();
    expect(within(screen.getByTestId('from-node/11')).getByText(/Γρεβενών/)).toBeTruthy();
  });
});

// QA found this: a place handed down after the field was drawn — a suggestion
// on the home screen, or a route link — was held but never shown, so the form
// looked empty while it was about to search for two towns.
describe('PlaceField given a place from outside', () => {
  it('shows a place its parent sets after it was drawn', () => {
    const onChange = jest.fn();
    const { rerender } = render(<PlaceField testID="from" label="Από" value={null} onChange={onChange} />);
    rerender(
      <PlaceField
        testID="from"
        label="Από"
        value={{ label: 'Φέρες', lon: 26.17, lat: 40.89 }}
        onChange={onChange}
      />,
    );
    expect(screen.getByTestId('from-input').props.value).toBe('Φέρες');
    // Showing it is not a change the field made, so nothing goes back up.
    expect(onChange).not.toHaveBeenCalled();
  });
});
