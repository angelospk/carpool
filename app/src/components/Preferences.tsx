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
import { border, colors, fonts, radius, space, type } from '../design/tokens';
import { Icon, type IconName } from './Icon';

/**
 * One picture per tag.
 *
 * Nine tags in a wrapped row of pills came out as three cramped columns of
 * half-read words, and the reading was the whole point. An icon is read before
 * the sentence beside it, which is what lets these be a list of switches rather
 * than a wall.
 *
 * It lives here rather than in the translations because it is not language: a
 * paw is a paw in both files, and duplicating it into each one is how the two
 * drift apart. Ionicons, never emoji: see components/Icon.
 */
export const PREFERENCE_ICON: Record<PreferenceTag, IconName> = {
  no_smoking: 'ban-outline',
  smoking_ok: 'flame-outline',
  no_pets: 'close-circle-outline',
  pets_ok: 'paw-outline',
  quiet_ride: 'volume-mute-outline',
  music_on: 'musical-notes-outline',
  chatty: 'chatbubbles-outline',
  keep_to_myself: 'headset-outline',
  air_conditioning: 'snow-outline',
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
  const shown = (tags ?? []).filter((tag) => tag in PREFERENCE_ICON);
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
            <View key={tag} style={styles.tag}>
              <Icon name={PREFERENCE_ICON[tag]} size={16} color={colors.ink} />
              <Text testID={testID ? `${testID}-${tag}` : undefined} style={styles.tagLabel}>
                {t(`preferences.${tag}`)}
              </Text>
            </View>
          ))}
        </View>
      ) : null}
    </View>
  );
}

/**
 * Choosing your own.
 *
 * One switch per line: the icon, the sentence, and a state you can see from
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
            <Icon
              testID={testID ? `${testID}-${tag}-icon` : undefined}
              name={PREFERENCE_ICON[tag]}
              size={20}
              color={on ? colors.ink : colors.muted}
            />
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
  box: { gap: space.sm, marginTop: space.xs },
  row: { flexDirection: 'row', flexWrap: 'wrap', gap: space.xs },
  bio: { ...type.body, color: colors.ink },
  tag: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    paddingVertical: 5,
    paddingHorizontal: 10,
    borderRadius: radius.chip,
    borderWidth: border.rest,
    borderColor: colors.line,
  },
  tagLabel: { ...type.caption, color: colors.ink },

  list: { gap: space.sm },
  item: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.md,
    minHeight: 52,
    paddingVertical: space.sm,
    paddingHorizontal: space.md + border.selected - border.rest,
    borderRadius: radius.control,
    borderWidth: border.rest,
    borderColor: colors.line,
  },
  // The 3px border takes its width out of the padding, so the row does not move.
  itemOn: { borderWidth: border.selected, borderColor: colors.ink, paddingHorizontal: space.md },
  itemLabel: { ...type.body, color: colors.muted, flex: 1 },
  itemLabelOn: { color: colors.ink },
  state: { ...type.caption, color: colors.muted },
  stateOn: { color: colors.ink, fontFamily: fonts.extraBold },
});
