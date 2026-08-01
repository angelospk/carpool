import { Linking, StyleSheet, Text, TouchableOpacity } from 'react-native';
import { useTranslation } from 'react-i18next';

import { colors, space, type } from '../design/tokens';

/**
 * Where someone says they can also be found.
 *
 * Labelled as their own claim, because that is exactly what it is: nobody
 * verified that this profile belongs to this person, and a link presented as
 * proof would be a stronger promise than the data supports. The server already
 * refuses anything that is not a facebook.com or instagram.com address, so what
 * arrives here cannot be an arbitrary redirect.
 */
export function SocialLink({ url, testID }: { url: string | null; testID?: string }) {
  const { t } = useTranslation();
  if (!url) return null;

  const shown = url.replace(/^https:\/\/(www\.|m\.|web\.)?/, '');

  return (
    <TouchableOpacity
      testID={testID ?? 'social-link'}
      accessibilityRole="link"
      onPress={() => void Linking.openURL(url)}
    >
      <Text style={styles.link} numberOfLines={1}>
        {shown}
      </Text>
      <Text style={styles.caveat}>{t('trip.socialSelfDeclared')}</Text>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  link: { ...type.caption, color: colors.primary, marginTop: space.xs },
  caveat: { ...type.caption, color: colors.muted, fontSize: 11 },
});
