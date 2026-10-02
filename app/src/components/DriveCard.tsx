import { useState } from "react";
import { StyleSheet, Text, View } from "react-native";
import { useTranslation } from "react-i18next";
import { useMutation } from "@tanstack/react-query";

import { api, type TripDetail } from "../api/endpoints";
import { refusalText } from "../api/errorText";
import { athensTime } from "../design/format";
import { colors, space, type } from "../design/tokens";
import { Button, Card, Chip, Hint } from "./ui";

/**
 * «Φεύγω»: the driver says the minute they leave.
 *
 * A schedule says "between 07:00 and 07:30", and with only that the app could
 * not tell anybody when the car would reach them. Pinning the minute lets the
 * matcher ask the people on the road who will be there before the car — and
 * nobody else, because the driver never waits. See specs/DRIVING-NOW.md.
 *
 * The time is worked out when the button is pressed and sent as a time, not
 * as "in ten minutes", so a retry is the same request and passengers are told
 * once.
 */
export function DriveCard({
  trip,
  now = () => Date.now(),
  onDone,
}: {
  trip: TripDetail;
  now?: () => number;
  onDone: () => void;
}) {
  const { t } = useTranslation();
  const [inMin, setInMin] = useState<number>(0);
  const [detour, setDetour] = useState<number>(
    nearestDetour(trip.detour_min ?? 5),
  );
  const [error, setError] = useState<string | null>(null);

  const go = useMutation({
    mutationFn: (v: { departsAt: string; detour: number }) =>
      api.startDrive(trip.id, v.departsAt, v.detour),
    onSuccess: () => {
      setError(null);
      onDone();
    },
    onError: (e) => setError(refusalText(e) ?? t("drive.failed")),
  });

  const pinned = !!trip.drive_set_at;
  return (
    <Card testID="drive-card">
      <Text style={styles.title}>
        {pinned
          ? t("drive.leavingAt", { time: athensTime(trip.depart_earliest) })
          : t("drive.title")}
      </Text>

      <Text style={styles.label}>{t("drive.when")}</Text>
      <View style={styles.row}>
        {WHEN.map((m) => (
          <Chip
            key={m}
            testID={`drive-when-${m}`}
            label={m === 0 ? t("drive.now") : t("drive.inMin", { n: m })}
            selected={inMin === m}
            onPress={() => setInMin(m)}
            disabled={go.isPending}
          />
        ))}
      </View>

      <Text style={styles.label}>{t("drive.detour")}</Text>
      <View style={styles.row}>
        {DETOURS.map((m) => (
          <Chip
            key={m}
            testID={`drive-detour-${m}`}
            label={m === 0 ? t("drive.onMyRoad") : t("drive.plusMin", { n: m })}
            selected={detour === m}
            onPress={() => setDetour(m)}
            disabled={go.isPending}
          />
        ))}
      </View>

      <Hint>{t("drive.hint")}</Hint>
      {error ? (
        <Text testID="drive-error" style={styles.error}>
          {error}
        </Text>
      ) : null}
      <Button
        testID="drive-go"
        label={pinned ? t("drive.change") : t("drive.go")}
        disabled={go.isPending}
        onPress={() =>
          go.mutate({
            departsAt: new Date(
              Math.floor((now() + inMin * 60_000) / 60_000) * 60_000,
            ).toISOString(),
            detour,
          })
        }
      />
    </Card>
  );
}

const WHEN = [0, 5, 10, 15] as const;
const DETOURS = [0, 3, 5, 10] as const;
const HOUR = 60 * 60_000;

function nearestDetour(min: number): number {
  return DETOURS.reduce(
    (best, d) => (Math.abs(d - min) < Math.abs(best - min) ? d : best),
    5,
  );
}

/**
 * Whether the card belongs on the screen: the driver's own trip, still running,
 * from an hour before its published window to an hour after — the same bounds
 * the server holds an empty car to — and, once a minute is pinned, until it.
 */
export function driveCardShown(trip: TripDetail, at: number): boolean {
  if (trip.role !== "driver" || trip.status !== "scheduled") return false;
  if (trip.drive_set_at) return at < Date.parse(trip.depart_earliest);
  const opens = Date.parse(trip.scheduled_earliest ?? trip.depart_earliest);
  const closes = Date.parse(trip.scheduled_latest ?? trip.depart_latest);
  return at >= opens - HOUR && at <= closes + HOUR;
}

const styles = StyleSheet.create({
  title: { ...type.title, color: colors.ink },
  label: { ...type.label, color: colors.muted, marginTop: space.md },
  row: {
    flexDirection: "row",
    flexWrap: "wrap",
    gap: space.sm,
    marginTop: space.xs,
  },
  error: { ...type.body, color: colors.danger, marginTop: space.sm },
});
