// What a ride is like.
//
// A photo, a name and a rating all say who somebody is. None of them says what
// the two hours will be like, which is the thing a passenger is actually
// weighing up. Two lines and a handful of tags do, and they are the cheapest
// trust signal in the app.
//
// The tags describe; they do not enforce. "Δεν καπνίζω" is what to expect, not
// a promise the app polices — so nothing here filters or matches on them.

import { StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';

import { CONTRADICTIONS, PREFERENCE_TAGS, type PreferenceTag } from '../api/endpoints';
import { Chip } from './ui';
import { colors, space, type } from '../design/tokens';

/** What somebody says about themselves, shown to somebody else. */
export function PreferenceChips({
  tags,
  bio,
  testID,
}: {
  tags?: PreferenceTag[];
  bio?: string | null;
  testID?: string;
}) {
  const { t } = useTranslation();
  const shown = tags ?? [];
  if (shown.length === 0 && !bio) return null;

  return (
    <View testID={testID} style={styles.box}>
      {/* Rendered as plain text, never as markup: it is somebody else's words
          and there is nothing in it the app should be interpreting. */}
      {bio ? (
        <Text testID={testID ? `${testID}-bio` : undefined} style={styles.bio}>
          {bio}
        </Text>
      ) : null}
      {shown.length > 0 ? (
        <View style={styles.row}>
          {shown.map((tag) => (
            <Text
              key={tag}
              testID={testID ? `${testID}-${tag}` : undefined}
              style={styles.tag}
            >
              {t(`preferences.${tag}`)}
            </Text>
          ))}
        </View>
      ) : null}
    </View>
  );
}

/** Choosing your own. */
export function PreferencePicker({
  value,
  onChange,
  testID,
}: {
  value: PreferenceTag[];
  onChange: (tags: PreferenceTag[]) => void;
  testID?: string;
}) {
  const { t } = useTranslation();

  // Picking one side of a pair drops the other rather than refusing the tap.
  // The server would refuse it, but being told "those contradict" after the
  // fact is worse than the app simply doing what the tap obviously meant.
  const toggle = (tag: PreferenceTag) => {
    if (value.includes(tag)) return onChange(value.filter((x) => x !== tag));

    const opposite = CONTRADICTIONS.find((p) => p.includes(tag))?.find((x) => x !== tag);
    onChange([...value.filter((x) => x !== opposite), tag]);
  };

  return (
    <View testID={testID} style={styles.row}>
      {PREFERENCE_TAGS.map((tag) => (
        <Chip
          key={tag}
          testID={testID ? `${testID}-${tag}` : undefined}
          label={t(`preferences.${tag}`)}
          selected={value.includes(tag)}
          onPress={() => toggle(tag)}
        />
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  box: { gap: space.xs, marginTop: space.xs },
  row: { flexDirection: 'row', flexWrap: 'wrap', gap: space.xs },
  bio: { ...type.body, color: colors.ink },
  tag: {
    ...type.caption,
    color: colors.muted,
    backgroundColor: colors.sunken,
    paddingVertical: space.xs,
    paddingHorizontal: space.sm,
    borderRadius: 999,
    overflow: 'hidden',
  },
});
