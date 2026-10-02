import { useState } from 'react';
import { StyleSheet, Switch, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation } from '@tanstack/react-query';

import { api, type Me } from '../api/endpoints';
import { refusalText } from '../api/errorText';
import { useSession } from '../state/session';
import { LANGUAGES, setLanguage } from '../i18n/language';
import { Card, Chip } from './ui';
import { colors, space, type } from '../design/tokens';

const GENDERS = ['female', 'male', 'other'] as const;

type Patch = Partial<Pick<Me, 'gender' | 'women_only' | 'asks_paused'>>;

/**
 * The profile's settings card: gender, women drivers only, pausing asks, and
 * language.
 *
 * Each saves on the tap rather than waiting for the profile's save button. A
 * switch that shows "on" before anything is stored is the thing this setting
 * used to be, so the switch follows what the server returned, and goes back
 * when it is refused.
 *
 * Gender is asked here only because women drivers only needs it. It is
 * optional, never shown to anybody else, and the hint says so.
 */
export function Settings() {
  const { t, i18n } = useTranslation();
  const me = useSession((s) => s.me);
  const setMe = useSession((s) => s.setMe);
  const [error, setError] = useState<string | null>(null);

  // One change at a time: every control below is shut while one is in flight,
  // so two answers cannot arrive in the wrong order.
  const update = useMutation({
    mutationFn: (v: { patch: Patch; account: string }) => api.updateMe(v.patch),
    onSuccess: (updated, { account }) => {
      setError(null);
      // Only onto the account that asked. Somebody who signed out, or in as
      // somebody else, while this was in flight must not get it back.
      const current = useSession.getState().me;
      if (!current || current.id !== account) return;
      // This card's own fields, onto the latest profile, and nothing else: the
      // rest of this response is as old as the request, and a photo uploaded
      // in the meantime is newer than it. All three rather than only the one
      // asked for, because they depend on each other on the server — a gender
      // other than female turns women drivers only off, and a switch left
      // showing "on" over a setting that is off is the worst lie here.
      setMe({
        ...current,
        gender: updated.gender,
        women_only: updated.women_only,
        asks_paused: updated.asks_paused,
      });
    },
    onError: (e) => setError(refusalText(e) ?? t('common.error')),
  });
  const change = (patch: Patch) => {
    if (me) update.mutate({ patch, account: me.id });
  };

  const female = me?.gender === 'female';
  const womenOnly = !!me?.women_only;

  return (
    <Card>
      <Text style={styles.label}>{t('settings.gender')}</Text>
      <View style={styles.chips}>
        {GENDERS.map((g) => (
          <Chip
            key={g}
            testID={`gender-${g}`}
            label={t(`settings.gender_${g}`)}
            selected={me?.gender === g}
            disabled={update.isPending}
            onPress={() => change({ gender: g })}
          />
        ))}
      </View>
      <Text style={styles.hint}>{t('settings.genderHint')}</Text>

      <View style={styles.divider} />

      <View style={styles.row}>
        <Text style={styles.rowLabel}>{t('profile.womenOnly')}</Text>
        <Switch
          testID="women-only"
          value={womenOnly}
          disabled={!female || update.isPending}
          onValueChange={(on) => change({ women_only: on })}
          trackColor={{ true: colors.primary, false: colors.line }}
        />
      </View>
      <Text testID={female ? undefined : 'women-only-hint'} style={styles.hint}>
        {female ? t('settings.womenOnlyHint') : t('settings.womenOnlyNeedsGender')}
      </Text>

      {/* Only for somebody with a car: the server leaves it out otherwise. A
          driver away for a week used to have to retire every route to stop
          being asked. */}
      {me?.asks_paused !== undefined ? (
        <>
          <View style={styles.divider} />
          <View style={styles.row}>
            <Text style={styles.rowLabel}>{t('settings.asksPaused')}</Text>
            <Switch
              testID="asks-paused"
              value={me.asks_paused}
              disabled={update.isPending}
              onValueChange={(on) => change({ asks_paused: on })}
              trackColor={{ true: colors.accent, false: colors.line }}
            />
          </View>
          <Text style={styles.hint}>
            {me.asks_paused ? t('settings.asksPausedOn') : t('settings.asksPausedOff')}
          </Text>
        </>
      ) : null}

      <View style={styles.divider} />

      <Text style={styles.label}>{t('profile.language')}</Text>
      <View style={styles.chips}>
        {LANGUAGES.map((lng) => (
          <Chip
            key={lng}
            testID={`language-${lng}`}
            label={t(`settings.language_${lng}`)}
            selected={i18n.language === lng}
            onPress={() => void setLanguage(lng)}
          />
        ))}
      </View>

      {error ? (
        <Text testID="settings-error" style={styles.error}>
          {error}
        </Text>
      ) : null}
    </Card>
  );
}

const styles = StyleSheet.create({
  label: { ...type.label, color: colors.muted },
  chips: { flexDirection: 'row', gap: space.sm, marginTop: space.sm, flexWrap: 'wrap' },
  hint: { ...type.caption, color: colors.muted, marginTop: space.sm },
  row: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between' },
  rowLabel: { ...type.body, color: colors.ink },
  divider: { height: 1, backgroundColor: colors.line, marginVertical: space.md },
  error: { ...type.caption, color: colors.danger, marginTop: space.sm },
});
