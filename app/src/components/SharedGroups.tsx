// "From your school": the one line on a stranger's card that says why they
// might not be one.
//
// Renders nothing when there is nothing shared. An empty badge, or one saying
// "no shared groups", would read as a warning about somebody who has simply
// never joined a group.

import { useTranslation } from 'react-i18next';

import { Badge } from './ui';

export function SharedGroups({ names, testID }: { names?: string[]; testID?: string }) {
  const { t } = useTranslation();
  if (!names || names.length === 0) return null;
  return <Badge testID={testID} tone="primary" label={t('groups.shared', { names: names.join(', ') })} />;
}
