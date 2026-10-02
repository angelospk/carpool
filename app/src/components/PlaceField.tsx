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
import * as Location from 'expo-location';
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
  here = false,
}: {
  label: string;
  /** The chosen place, or null while the text does not name one. */
  value: Pin | null;
  onChange: (pin: Pin | null) => void;
  testID?: string;
  placeholder?: string;
  /**
   * Offer «Από εδώ»: fill the field with the settlement the phone is in. Where
   * location is already allowed it fills itself on opening; it never asks for
   * the permission until pressed.
   */
  here?: boolean;
}) {
  const { t } = useTranslation();
  const [text, setText] = useState(value?.label ?? '');
  const [suggestions, setSuggestions] = useState<Place[] | null>(null);
  const [searched, setSearched] = useState(false);

  // Monotonic, so a slow answer to an earlier question cannot overwrite the
  // answer to the current one. Without it, typing "αλεξ" then "ορε" can leave
  // Alexandroupoli on screen under the text "ορε".
  const asked = useRef(0);
  // Bumped by anything that changes what the field holds, so a location
  // lookup that started before it can tell its answer is no longer wanted.
  const touched = useRef(0);

  // What this field last handed upwards. Deliberately not read from the `value`
  // prop: that only comes back after the parent re-renders, and the rule below
  // has to hold on the keystroke itself, not one render later.
  const selected = useRef<string | null>(value?.label ?? null);

  // A place handed down from outside — a suggestion, a route link — after the
  // field was drawn. useState only reads its initial value once, so without
  // this the field held the place and showed an empty box. Marked as selected
  // first, so the search the new text would start is not asked, and nothing
  // is handed back up: the parent already knows.
  const outside = value?.label ?? null;
  useEffect(() => {
    if (outside !== null && outside !== selected.current) {
      selected.current = outside;
      setText(outside);
    } else if (outside === null && selected.current !== null) {
      // And taken away from outside: the parent cleared a place this field
      // still showed. A keystroke cannot land here, because edit() lets go of
      // selected before it hands null up.
      selected.current = null;
      touched.current++;
      setText('');
    }
  }, [outside]);

  const edit = (next: string) => {
    touched.current++;
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
      // Bumped for the same reason as selecting: clearing the field ends every
      // question it had outstanding, and without this a slower answer lands
      // afterwards and opens a list of suggestions over an empty box.
      asked.current++;
      setSuggestions(null);
      setSearched(false);
      return;
    }

    // A raw coordinate is already a location, so there is nothing to look up.
    const typed = parsePin(query);
    if (typed) {
      // Bumped so that a search still in flight resolves as stale. Otherwise
      // its answer lands after this and reopens a list over a field that is
      // already settled.
      asked.current++;
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

  const [locating, setLocating] = useState(false);
  const [hereFailed, setHereFailed] = useState(false);

  // ask=false is the silent fill on opening: it uses a permission already
  // given and gives up quietly, because nobody pressed anything.
  const fromHere = async (ask: boolean) => {
    // Whatever happens to the field while the phone looks — a keystroke, a
    // choice from the list, the parent clearing it, another press — makes
    // this attempt's answer stale. A press also overtakes the silent attempt.
    const mine = ask ? ++touched.current : touched.current;
    const stale = () => touched.current !== mine;
    setHereFailed(false);
    if (ask) setLocating(true);
    try {
      const permission = ask
        ? await Location.requestForegroundPermissionsAsync()
        : await Location.getForegroundPermissionsAsync();
      if (!permission.granted) {
        if (ask && !stale()) setHereFailed(true);
        return;
      }
      const at = await position();
      const { place } = await api.placeNear(at.coords.longitude, at.coords.latitude);
      // What somebody did by hand while the phone was looking wins over what
      // the phone worked out.
      if (stale() || (!ask && selected.current !== null)) return;
      choose(place);
    } catch {
      if (ask && !stale()) setHereFailed(true);
    } finally {
      if (ask) setLocating(false);
    }
  };

  useEffect(() => {
    if (here && value === null) void fromHere(false);
    // Once, on opening: a field emptied later stays empty until asked.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const choose = (place: Place) => {
    // Same reason as the coordinate branch: choosing ends every question this
    // field has outstanding, so no answer to one may reopen the list.
    asked.current++;
    touched.current++;
    selected.current = place.name;
    setText(place.name);
    setSuggestions(null);
    setSearched(false);
    onChange({ lon: place.lon, lat: place.lat, label: place.name, id: place.id });
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

      {here ? (
        <Pressable
          testID={testID ? `${testID}-here` : undefined}
          accessibilityRole="button"
          onPress={() => void fromHere(true)}
          disabled={locating}
          style={styles.here}
        >
          <Text style={styles.hereLabel}>{t('places.here')}</Text>
        </Pressable>
      ) : null}
      {hereFailed ? (
        <Text testID={testID ? `${testID}-here-failed` : undefined} style={styles.empty}>
          {t('places.hereFailed')}
        </Text>
      ) : null}

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
              {/* Dozens of Greek villages share a name. The municipality is
                  what tells otherwise identical rows apart, and the kind is
                  what says which of them is a city and which is five houses. */}
              <Text style={styles.kind}>
                {place.area ? `${place.area} · ` : ''}
                {t(`places.${place.kind}`)}
              </Text>
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

// Where the phone is, to the nearest town. A fix from the last few minutes is
// as good as a new one at that scale and comes back at once; a fresh fix can
// hang on Android when no provider has one, so it gets ten seconds and then
// counts as a failure, which the field reports instead of sitting still.
async function position(): Promise<Location.LocationObject> {
  const recent = await Location.getLastKnownPositionAsync({ maxAge: 10 * 60_000 });
  if (recent) return recent;
  return Promise.race([
    Location.getCurrentPositionAsync({ accuracy: Location.Accuracy.Balanced }),
    new Promise<never>((_, reject) => setTimeout(() => reject(new Error('no fix')), 10_000)),
  ]);
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
  here: { alignSelf: 'flex-start', paddingVertical: space.xs, marginTop: space.xs },
  hereLabel: { ...typography.label, color: colors.primary },
  empty: { ...typography.caption, color: colors.muted, marginTop: space.xs, paddingHorizontal: space.md },
});
