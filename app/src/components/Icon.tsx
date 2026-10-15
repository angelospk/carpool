// Icons: Ionicons from @expo/vector-icons, outline style. Never emoji: an emoji
// is drawn by the phone's own font, in its own colours, and reads aloud under
// whatever name the platform gives it.

import Ionicons from '@expo/vector-icons/Ionicons';

export type IconName = keyof typeof Ionicons.glyphMap;

export function Icon({
  name,
  size = 20,
  color,
  testID,
  hidden = true,
}: {
  name: IconName;
  size?: number;
  color: string;
  testID?: string;
  /** Decoration next to a word that already says it. Icon-only buttons label
      the button instead. */
  hidden?: boolean;
}) {
  return (
    <Ionicons
      testID={testID}
      name={name}
      size={size}
      color={color}
      accessibilityElementsHidden={hidden}
      importantForAccessibility={hidden ? 'no-hide-descendants' : 'auto'}
      aria-hidden={hidden}
    />
  );
}
