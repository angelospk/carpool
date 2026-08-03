// Where somebody says where they are going.
//
// Until now both ends of a route were typed as "25.8743, 40.8476
// Αλεξανδρούπολη", which nobody does twice. This asks the server as the name is
// typed and gives back the same {lon, lat, label} the parsed string produced,
// so the screens around it did not have to change shape.
//
// One rule governs the whole component: the text on screen and the place that
// will be sent must never disagree. Selecting fills both; typing anything
// afterwards clears the selection immediately, even though the text still looks
// like a town. A field that kept the old coordinate under edited text would
// send a passenger somewhere they did not ask for, with nothing on screen
// saying so.

import { useEffect, useRef, useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';

import { api, type Place } from '../api/endpoints';
import { Field } from './ui';
import { parsePin, type Pin } from '../design/pin';
import { colors, radius, space, type as typography } from '../design/tokens';

// Long enough that a typed word is one request rather than five, short enough
// that the list is there by the time the finger stops.
const DEBOUNCE_MS = 300;

export function PlaceField({
  label,
  value,
  onChange,
  testID,
  placeholder,
}: {
  label: string;
  /** The chosen place, or null while the text does not name one. */
  value: Pin | null;
  onChange: (pin: Pin | null) => void;
  testID?: string;
  placeholder?: string;
}) {
  const { t } = useTranslation();
  const [text, setText] = useState(value?.label ?? '');
  const [suggestions, setSuggestions] = useState<Place[] | null>(null);
  const [searched, setSearched] = useState(false);

  // Monotonic, so a slow answer to an earlier question cannot overwrite the
  // answer to the current one. Without it, typing "αλεξ" then "ορε" can leave
  // Alexandroupoli on screen under the text "ορε".
  const asked = useRef(0);

  // What this field last handed upwards. Deliberately not read from the `value`
  // prop: that only comes back after the parent re-renders, and the rule below
  // has to hold on the keystroke itself, not one render later.
  const selected = useRef<string | null>(value?.label ?? null);

  const edit = (next: string) => {
    setText(next);
    // Cleared on the first keystroke rather than on blur. In between, the field
    // would be showing one place and holding another.
    if (selected.current !== null && next !== selected.current) {
      selected.current = null;
      onChange(null);
    }
  };

  useEffect(() => {
    const query = text.trim();
    if (!query) {
      setSuggestions(null);
      setSearched(false);
      return;
    }

    // A raw coordinate is already a location, so there is nothing to look up.
    const typed = parsePin(query);
    if (typed) {
      setSuggestions(null);
      setSearched(false);
      selected.current = query;
      onChange(typed);
      return;
    }

    // Not searched while a place is selected: the text equals its name, and
    // asking again would reopen the list the user just closed.
    if (selected.current === query) return;

    const mine = ++asked.current;
    const timer = setTimeout(() => {
      api
        .places(query)
        .then((res) => {
          if (asked.current !== mine) return;
          setSuggestions(res.places);
          setSearched(true);
        })
        .catch(() => {
          // Offline, or a server that cannot be reached. The field goes quiet
          // rather than showing an error under every letter; the screen's own
          // submit path reports the failure that matters.
          if (asked.current !== mine) return;
          setSuggestions(null);
          setSearched(false);
        });
    }, DEBOUNCE_MS);

    return () => clearTimeout(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [text]);

  const choose = (place: Place) => {
    selected.current = place.name;
    setText(place.name);
    setSuggestions(null);
    setSearched(false);
    onChange({ lon: place.lon, lat: place.lat, label: place.name });
  };

  return (
    <View>
      <Field
        testID={testID ? `${testID}-input` : undefined}
        label={label}
        value={text}
        onChangeText={edit}
        placeholder={placeholder}
        autoCapitalize="sentences"
        valid={value !== null}
      />

      {suggestions && suggestions.length > 0 ? (
        <View testID={testID ? `${testID}-suggestions` : undefined} style={styles.list}>
          {suggestions.map((place) => (
            <Pressable
              key={place.id}
              testID={testID ? `${testID}-${place.id}` : undefined}
              onPress={() => choose(place)}
              style={styles.row}
            >
              <Text style={styles.name}>{place.name}</Text>
              {/* Two villages here share a name, so the kind is what tells
                  otherwise identical rows apart. */}
              <Text style={styles.kind}>{t(`places.${place.kind}`)}</Text>
            </Pressable>
          ))}
        </View>
      ) : null}

      {searched && suggestions?.length === 0 ? (
        <Text testID={testID ? `${testID}-empty` : undefined} style={styles.empty}>
          {t('places.none')}
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  list: {
    marginTop: space.xs,
    backgroundColor: colors.surface,
    borderRadius: radius.inner,
    overflow: 'hidden',
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    paddingVertical: space.sm,
    paddingHorizontal: space.md,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.line,
  },
  name: { ...typography.body, color: colors.ink },
  kind: { ...typography.caption, color: colors.muted },
  empty: { ...typography.caption, color: colors.muted, marginTop: space.xs, paddingHorizontal: space.md },
});
