// What a ride is like.
//
// A photo, a name and a rating all say who somebody is. None of them says what
// the two hours will be like, which is the thing a passenger is actually
// weighing up. Two lines and a handful of tags do, and they are the cheapest
// trust signal in the app.
//
// The tags describe; they do not enforce. "Δεν καπνίζω" is what to expect, not
// a promise the app polices — so nothing here filters or matches on them.

import { Pressable, StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';

import { CONTRADICTIONS, PREFERENCE_TAGS, type PreferenceTag } from '../api/endpoints';
import { colors, radius, space, type } from '../design/tokens';

/**
 * One picture per tag.
 *
 * Nine tags in a wrapped row of pills came out as three cramped columns of
 * half-read words, and the reading was the whole point. A glyph is read before
 * the sentence beside it, which is what lets these be a list of switches rather
 * than a wall.
 *
 * It lives here rather than in the translations because it is not language: a
 * cigarette is a cigarette in both files, and duplicating it into each one is
 * how the two drift apart.
 */
export const PREFERENCE_EMOJI: Record<PreferenceTag, string> = {
  no_smoking: '🚭',
  smoking_ok: '🚬',
  no_pets: '🐾',
  pets_ok: '🐕',
  quiet_ride: '🤫',
  music_on: '🎵',
  chatty: '💬',
  keep_to_myself: '🎧',
  air_conditioning: '❄️',
};

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
  // A tag this build has never heard of is dropped rather than rendered as a
  // raw key: the vocabulary is the server's, and it may grow before the app on
  // somebody's phone does.
  const shown = (tags ?? []).filter((tag) => tag in PREFERENCE_EMOJI);
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
              {PREFERENCE_EMOJI[tag]} {t(`preferences.${tag}`)}
            </Text>
          ))}
        </View>
      ) : null}
    </View>
  );
}

/**
 * Choosing your own.
 *
 * One switch per line: the glyph, the sentence, and a state you can see from
 * across the room. The list is long enough that it only belongs inside
 * something that scrolls, which is where both of its callers put it.
 */
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
    <View testID={testID} style={styles.list}>
      {PREFERENCE_TAGS.map((tag) => {
        const on = value.includes(tag);
        return (
          <Pressable
            key={tag}
            testID={testID ? `${testID}-${tag}` : undefined}
            accessibilityRole="switch"
            // A switch reports on or off, and `aria-checked` is the spelling
            // that survives to both platforms — see ContributionChoice. Without
            // it all nine announced as "Δεν καπνίζω, switch" and nothing else,
            // which is the same wall the visible Ενεργό/Ανενεργό was added to
            // knock down for sighted users.
            aria-checked={on}
            accessibilityLabel={t(`preferences.${tag}`)}
            onPress={() => toggle(tag)}
            style={[styles.item, on && styles.itemOn]}
          >
            <Text style={styles.emoji}>{PREFERENCE_EMOJI[tag]}</Text>
            <Text style={[styles.itemLabel, on && styles.itemLabelOn]}>
              {t(`preferences.${tag}`)}
            </Text>
            {/* A word as well as a colour. Colour alone is not a state
                somebody with any of the common kinds of colour blindness can
                read, and this list is nine of them stacked. */}
            <Text style={[styles.state, on && styles.stateOn]}>
              {t(on ? 'common.on' : 'common.off')}
            </Text>
          </Pressable>
        );
      })}
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
    borderRadius: radius.pill,
    overflow: 'hidden',
  },

  list: { gap: space.xs },
  item: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.md,
    paddingVertical: space.sm,
    paddingHorizontal: space.md,
    borderRadius: radius.inner,
    backgroundColor: colors.sunken,
  },
  itemOn: { backgroundColor: colors.accentSoft },
  emoji: { fontSize: 20 },
  itemLabel: { ...type.body, color: colors.muted, flex: 1 },
  itemLabelOn: { color: colors.ink },
  state: { ...type.caption, color: colors.muted },
  stateOn: { color: colors.primary, fontWeight: '700' },
});
