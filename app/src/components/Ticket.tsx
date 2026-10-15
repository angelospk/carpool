// The ticket: the one recognisable thing in this app.
//
// A ride drawn like a coach ticket. Departure on the left and arrival on the
// right in the heaviest type on the screen, the distance over a dashed rule
// between them, then a perforated divider with a half-circle notch at each
// edge, then who is driving and the contribution. Selected is a 3px ink
// border, and only then does the fare get the lime stamp.
//
// Presentation only. Whoever uses it decides what the times are; an arrival
// that is worked out rather than known is passed with `approx` and shows «≈».

import { useState, type ReactNode } from 'react';
import { Animated, Pressable, StyleSheet, Text, View, type LayoutChangeEvent } from 'react-native';
import Svg, { Line } from 'react-native-svg';

import { Avatar, usePressScale } from './ui';
import { border, colors, radius, space, type } from '../design/tokens';

export type TicketEnd = { time: string; place: string; approx?: boolean };

export function Ticket({
  depart,
  arrive,
  distance,
  header,
  note,
  aside,
  person,
  lowerLeft,
  fare,
  fareLabel,
  footer,
  selected,
  onPress,
  testID,
  accessibilityLabel,
}: {
  depart: TicketEnd;
  arrive: TicketEnd;
  /** Shown over the dashed rule, e.g. «57,9 χλμ». */
  distance?: string;
  /** A line above the times: the day, a role, a status. */
  header?: ReactNode;
  /** A caption under the places: pickup window, distance from you. */
  note?: ReactNode;
  /** Something small beside the note, such as the route glyph. */
  aside?: ReactNode;
  /** The lower half: the driver, with one muted line under the name. */
  person?: { name: string; photoUrl?: string | null; line?: string; testID?: string };
  /** In place of the person, when there is nobody to show: a driver's own seats. */
  lowerLeft?: ReactNode;
  /** «2,37 €». */
  fare?: string;
  /** A word over the fare, because bare euros beside a journey read as a fare. */
  fareLabel?: string;
  /** Replaces the lower half entirely. */
  footer?: ReactNode;
  selected?: boolean;
  onPress?: () => void;
  testID?: string;
  accessibilityLabel?: string;
}) {
  const press = usePressScale();
  const lower = footer ?? (person || lowerLeft || fare ? (
    <View style={styles.lower}>
      {person ? (
        <View testID={person.testID} style={styles.person}>
          <Avatar name={person.name} photoUrl={person.photoUrl} size={40} />
          <View style={styles.personText}>
            <Text style={styles.personName} numberOfLines={1}>
              {person.name}
            </Text>
            {person.line ? (
              <Text style={styles.personLine} numberOfLines={2}>
                {person.line}
              </Text>
            ) : null}
          </View>
        </View>
      ) : (
        <View style={styles.person}>{lowerLeft}</View>
      )}
      {fare ? (
        <View style={styles.fareBox}>
          {fareLabel ? <Text style={styles.fareLabel}>{fareLabel}</Text> : null}
          <View style={[styles.fare, selected && styles.fareSelected]}>
            <Text style={styles.fareText}>{fare}</Text>
          </View>
        </View>
      ) : null}
    </View>
  ) : null);

  const body = (
    <View style={[styles.ticket, selected && styles.ticketSelected]}>
      <View style={styles.upper}>
        {header ? <View style={styles.header}>{header}</View> : null}
        <View style={styles.ends}>
          <View style={styles.end}>
            <Text style={styles.time}>{depart.time}</Text>
          </View>
          <View style={styles.middle}>
            {distance ? (
              <Text style={styles.distance} numberOfLines={1}>
                {distance}
              </Text>
            ) : null}
            <Dashes />
          </View>
          <View style={[styles.end, styles.endRight]}>
            <Text style={styles.time}>
              {arrive.approx ? '≈' : ''}
              {arrive.time}
            </Text>
          </View>
        </View>
        <View style={styles.places}>
          <Text style={styles.place} numberOfLines={2}>
            {depart.place}
          </Text>
          <Text style={[styles.place, styles.placeRight]} numberOfLines={2}>
            {arrive.place}
          </Text>
        </View>
        {note || aside ? (
          <View style={styles.noteRow}>
            <View style={styles.noteText}>
              {typeof note === 'string' ? <Text style={styles.note}>{note}</Text> : note}
            </View>
            {aside}
          </View>
        ) : null}
      </View>
      {lower ? (
        <>
          <Perforation selected={!!selected} />
          <View style={styles.lowerWrap}>{lower}</View>
        </>
      ) : null}
    </View>
  );

  if (!onPress) return <View testID={testID}>{body}</View>;
  return (
    <Pressable
      testID={testID}
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
      aria-selected={selected}
      onPress={onPress}
      onPressIn={press.onPressIn}
      onPressOut={press.onPressOut}
    >
      <Animated.View style={press.style}>{body}</Animated.View>
    </Pressable>
  );
}

/** A dashed rule as wide as its parent. Drawn, because RN dashes one side of a border badly. */
function Dashes({ color = colors.line }: { color?: string }) {
  const [width, setWidth] = useState(0);
  return (
    <View style={styles.dashes} onLayout={(e: LayoutChangeEvent) => setWidth(e.nativeEvent.layout.width)}>
      {width > 0 ? (
        <Svg width={width} height={2}>
          <Line x1={0} y1={1} x2={width} y2={1} stroke={color} strokeWidth={1.5} strokeDasharray="6 5" />
        </Svg>
      ) : null}
    </View>
  );
}

/**
 * The tear line: dashes between two half-circle notches cut into the edges.
 * Each notch is a paper circle with the card's border, clipped to the half
 * that lies inside the ticket, so it reads as a bite out of the outline.
 */
function Perforation({ selected }: { selected: boolean }) {
  const edge = selected ? border.selected : border.rest;
  const ring = { borderWidth: edge, borderColor: selected ? colors.ink : colors.line };
  return (
    <View style={styles.perforation}>
      <View style={[styles.notchClip, { left: -edge }]}>
        <View style={[styles.notch, ring, { left: -NOTCH / 2 }]} />
      </View>
      <View style={styles.perforationLine}>
        <Dashes />
      </View>
      <View style={[styles.notchClip, { right: -edge }]}>
        <View style={[styles.notch, ring, { left: 0 }]} />
      </View>
    </View>
  );
}

const NOTCH = 20;

const styles = StyleSheet.create({
  ticket: {
    backgroundColor: colors.paper,
    borderRadius: radius.card,
    borderWidth: border.rest,
    borderColor: colors.line,
  },
  ticketSelected: { borderWidth: border.selected, borderColor: colors.ink },
  upper: { padding: space.lg, paddingBottom: space.md, gap: 6 },
  header: { flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', gap: space.sm, marginBottom: 2 },
  ends: { flexDirection: 'row', alignItems: 'center', gap: space.md },
  end: { flexShrink: 0 },
  endRight: { alignItems: 'flex-end' },
  time: { ...type.time, color: colors.ink },
  middle: { flex: 1, alignItems: 'center', gap: 2, minWidth: 40 },
  distance: { ...type.label, fontSize: 13, lineHeight: 17, color: colors.muted },
  dashes: { alignSelf: 'stretch', height: 2 },
  places: { flexDirection: 'row', justifyContent: 'space-between', gap: space.md },
  place: { ...type.label, color: colors.muted, flexShrink: 1 },
  placeRight: { textAlign: 'right' },
  noteRow: { flexDirection: 'row', alignItems: 'center', gap: space.md, marginTop: 4 },
  noteText: { flex: 1 },
  note: { ...type.caption, color: colors.muted },

  perforation: { height: NOTCH, justifyContent: 'center' },
  perforationLine: { marginHorizontal: NOTCH / 2 + 4 },
  notchClip: {
    position: 'absolute',
    top: 0,
    width: NOTCH / 2 + 1,
    height: NOTCH,
    overflow: 'hidden',
  },
  notch: {
    position: 'absolute',
    top: 0,
    width: NOTCH,
    height: NOTCH,
    borderRadius: NOTCH / 2,
    backgroundColor: colors.paper,
  },

  lowerWrap: { padding: space.lg, paddingTop: space.sm },
  lower: { flexDirection: 'row', alignItems: 'center', gap: space.md },
  person: { flex: 1, flexDirection: 'row', alignItems: 'center', gap: space.md },
  personText: { flex: 1, gap: 2 },
  personName: { ...type.heading, color: colors.ink },
  personLine: { ...type.caption, color: colors.muted },
  fareBox: { alignItems: 'flex-end', gap: 2 },
  fareLabel: { ...type.caption, fontSize: 12, lineHeight: 15, color: colors.muted },
  fare: { paddingHorizontal: 0, paddingVertical: 2, borderRadius: radius.chip },
  fareSelected: { backgroundColor: colors.accent, paddingHorizontal: 10, paddingVertical: 6 },
  fareText: { ...type.time, fontSize: 20, lineHeight: 24, color: colors.onAccent },
});
