import { Linking, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import Svg, { Path, Rect, Circle } from 'react-native-svg';

import { colors, space, type } from '../design/tokens';

/**
 * Where someone says they can also be found.
 *
 * Labelled as their own claim, because that is exactly what it is: nobody
 * verified that this profile belongs to this person, and a link presented as
 * proof would be a stronger promise than the data supports.
 *
 * The address is composed on the server from a fixed origin and a handle that
 * has already been checked, so what arrives here cannot be an arbitrary
 * redirect. The icon is what makes two rows readable at a glance, since the two
 * addresses otherwise differ by one word in the middle.
 */
export function SocialLinks({
  facebook,
  instagram,
  testID,
}: {
  facebook: string | null;
  instagram: string | null;
  testID?: string;
}) {
  const { t } = useTranslation();
  if (!facebook && !instagram) return null;

  return (
    <View testID={testID ?? 'social-links'} style={styles.rows}>
      {facebook ? <Link url={facebook} icon={<FacebookMark />} testID="social-facebook" /> : null}
      {instagram ? <Link url={instagram} icon={<InstagramMark />} testID="social-instagram" /> : null}
      <Text style={styles.caveat}>{t('trip.socialSelfDeclared')}</Text>
    </View>
  );
}

function Link({ url, icon, testID }: { url: string; icon: React.ReactNode; testID: string }) {
  // Shown as the handle rather than the address: it is what the person calls
  // themselves, and it fits on one line next to a car's details.
  const handle = url.replace(/^https:\/\/[^/]+\//, '@');

  return (
    <TouchableOpacity
      testID={testID}
      accessibilityRole="link"
      style={styles.row}
      onPress={() => void Linking.openURL(url)}
    >
      {icon}
      <Text style={styles.link} numberOfLines={1}>
        {handle}
      </Text>
    </TouchableOpacity>
  );
}

const SIZE = 14;

function FacebookMark() {
  return (
    <Svg width={SIZE} height={SIZE} viewBox="0 0 24 24">
      <Path
        fill={colors.muted}
        d="M24 12.07C24 5.4 18.63 0 12 0S0 5.4 0 12.07C0 18.1 4.39 23.1 10.13 24v-8.44H7.08v-3.49h3.05V9.41c0-3.02 1.79-4.69 4.53-4.69 1.31 0 2.68.24 2.68.24v2.97h-1.51c-1.49 0-1.96.93-1.96 1.89v2.25h3.33l-.53 3.49h-2.8V24C19.61 23.1 24 18.1 24 12.07"
      />
    </Svg>
  );
}

function InstagramMark() {
  // Drawn from primitives rather than the brand's own path data: three shapes
  // are enough to be recognisable at fourteen pixels, and it keeps a trademarked
  // outline out of the repository.
  return (
    <Svg width={SIZE} height={SIZE} viewBox="0 0 24 24">
      <Rect
        x={2.5}
        y={2.5}
        width={19}
        height={19}
        rx={5.5}
        stroke={colors.muted}
        strokeWidth={2}
        fill="none"
      />
      <Circle cx={12} cy={12} r={4.5} stroke={colors.muted} strokeWidth={2} fill="none" />
      <Circle cx={17.5} cy={6.5} r={1.4} fill={colors.muted} />
    </Svg>
  );
}

const styles = StyleSheet.create({
  rows: { marginTop: space.xs, gap: 2 },
  row: { flexDirection: 'row', alignItems: 'center', gap: space.xs },
  link: { ...type.caption, color: colors.ink, textDecorationLine: 'underline', flexShrink: 1 },
  caveat: { ...type.caption, color: colors.muted, fontSize: 11 },
});
