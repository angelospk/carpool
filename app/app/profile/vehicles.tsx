// The garage.
//
// A driver with one car never opens this screen — saying what they drive on the
// profile puts a car in it, and that is the whole interaction. It exists for the
// person with a van at the weekend and a small diesel in the week, who until now
// had to edit one profile before every trip and silently repriced every trip
// they had already published when they did.
//
// What it will not let anybody do is type a consumption figure. The car is
// described — the things printed on the άδεια κυκλοφορίας — and the server works
// out what it drinks. That distinction is the reason the whole pricing formula
// can be argued for; see docs/pricing.md.

import { useState } from 'react';
import { ScrollView, StyleSheet, Text, View } from 'react-native';
import { useTranslation } from 'react-i18next';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api, type Vehicle } from '../../src/api/endpoints';
import { ApiError } from '../../src/api/client';
import {
  AsyncState,
  Button,
  Card,
  Chip,
  Field,
  Hint,
  Row,
  Screen,
  SectionHeader,
  Title,
} from '../../src/components/ui';
import { colors, space, type } from '../../src/design/tokens';
import { decimal } from '../../src/design/format';

const FUELS = ['petrol', 'diesel', 'petrol_hybrid', 'diesel_hybrid'] as const;

export default function VehiclesScreen() {
  const { t } = useTranslation();
  const client = useQueryClient();
  const refresh = () => client.invalidateQueries({ queryKey: ['vehicles'] });

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['vehicles'],
    queryFn: api.vehicles,
  });

  const [adding, setAdding] = useState(false);
  const [make, setMake] = useState('');
  const [model, setModel] = useState('');
  const [fuel, setFuel] = useState<string>('petrol');
  const [engineCC, setEngineCC] = useState('');
  const [plate, setPlate] = useState('');
  const [label, setLabel] = useState('');
  const [problem, setProblem] = useState<string | null>(null);

  const add = useMutation({
    mutationFn: () =>
      api.addVehicle({
        make: make.trim(),
        model: model.trim(),
        fuel,
        engine_cc: Number(engineCC) || undefined,
        plate: plate.trim(),
        label: label.trim() || undefined,
      }),
    onSuccess: () => {
      setAdding(false);
      setMake('');
      setModel('');
      setEngineCC('');
      setPlate('');
      setLabel('');
      setProblem(null);
      refresh();
    },
    onError: (e) =>
      setProblem(
        e instanceof ApiError && e.code === 'too_many_vehicles'
          ? t('garage.tooMany')
          : e instanceof ApiError && e.isClientError
            ? t('garage.notACar')
            : t('common.error'),
      ),
  });

  const makeDefault = useMutation({
    mutationFn: (id: string) => api.setDefaultVehicle(id),
    onSuccess: refresh,
  });

  const retire = useMutation({
    mutationFn: (id: string) => api.retireVehicle(id),
    onSuccess: () => {
      setProblem(null);
      refresh();
    },
    onError: (e) =>
      setProblem(
        e instanceof ApiError && e.code === 'vehicle_in_use'
          ? t('garage.inUse')
          : t('common.error'),
      ),
  });

  return (
    <Screen>
      <ScrollView contentContainerStyle={styles.body} showsVerticalScrollIndicator={false}>
        <Title>{t('garage.title')}</Title>
        <Hint>{t('garage.hint')}</Hint>

        <AsyncState
          loading={isLoading}
          error={error}
          onRetry={() => refetch()}
          empty={(data ?? []).length === 0}
          emptyLabel={t('garage.empty')}
        >
          {(data ?? []).map((v) => (
            <VehicleCard
              key={v.id}
              vehicle={v}
              onMakeDefault={() => makeDefault.mutate(v.id)}
              onRetire={() => retire.mutate(v.id)}
              canRetire={(data ?? []).length > 1}
            />
          ))}
        </AsyncState>

        {problem ? (
          <Text testID="garage-problem" style={styles.problem}>
            {problem}
          </Text>
        ) : null}

        {adding ? (
          <Card>
            <SectionHeader title={t('garage.addTitle')} />
            <Field
              testID="garage-make"
              label={t('garage.make')}
              value={make}
              onChangeText={setMake}
              placeholder="Toyota"
            />
            <Field
              testID="garage-model"
              label={t('garage.model')}
              value={model}
              onChangeText={setModel}
              placeholder="Yaris"
            />
            <View style={styles.fuels}>
              {FUELS.map((f) => (
                <Chip
                  key={f}
                  testID={`garage-fuel-${f}`}
                  label={t(`garage.fuel.${f}`)}
                  selected={fuel === f}
                  onPress={() => setFuel(f)}
                />
              ))}
            </View>
            {/* Required, not optional, and this is why: with no displacement
                the server can place the car in no band at all, so when the
                dataset does not know the model there is nothing left to fall
                back on. It answered ErrUnusable, the app showed "check the
                make, model and fuel" — all three of which were filled in — and
                the driver had a form that would not save and would not say
                what was wrong. */}
            <Field
              testID="garage-cc"
              label={t('garage.engineCC')}
              value={engineCC}
              onChangeText={(v) => setEngineCC(v.replace(/\D/g, '').slice(0, 4))}
              placeholder="1329"
              keyboardType="number-pad"
            />
            <Hint>{t('garage.engineCCHint')}</Hint>
            <Field
              testID="garage-plate"
              label={t('garage.plate')}
              value={plate}
              onChangeText={setPlate}
              placeholder="ΑΒΓ-1234"
            />
            <Field
              testID="garage-label"
              label={t('garage.label')}
              value={label}
              onChangeText={setLabel}
              placeholder={t('garage.labelPlaceholder')}
            />
            <Hint>{t('garage.consumptionNotAsked')}</Hint>
            <Button
              testID="garage-save"
              label={t('common.save')}
              disabled={
                !make.trim() ||
                !model.trim() ||
                !plate.trim() ||
                Number(engineCC) <= 0 ||
                add.isPending
              }
              onPress={() => add.mutate()}
            />
          </Card>
        ) : (
          <Button testID="garage-add" label={t('garage.add')} onPress={() => setAdding(true)} />
        )}
      </ScrollView>
    </Screen>
  );
}

function VehicleCard({
  vehicle,
  onMakeDefault,
  onRetire,
  canRetire,
}: {
  vehicle: Vehicle;
  onMakeDefault: () => void;
  onRetire: () => void;
  canRetire: boolean;
}) {
  const { t } = useTranslation();

  return (
    <Card testID={`garage-car-${vehicle.id}`}>
      <Row
        label={vehicle.name}
        value={vehicle.is_default ? t('garage.isDefault') : ''}
      />
      <Text style={styles.detail}>
        {vehicle.make} {vehicle.model} · {t(`garage.fuel.${vehicle.fuel}`)} · {vehicle.plate}
      </Text>
      {/* The number every contribution on this car is worked out from, and how
          firm it is. Shown because the driver is the one who can correct the
          car it came from — and because a figure at the ceiling should not
          quietly look like a measurement. */}
      <Text style={styles.detail}>
        {t('garage.drinks', { litres: decimal(vehicle.consumption_l_100km, 1) })}
        {vehicle.consumption_source === 'band' ? ` · ${t('garage.sourceBand')}` : ''}
        {vehicle.consumption_capped ? ` · ${t('garage.capped')}` : ''}
      </Text>

      <View style={styles.actions}>
        {vehicle.is_default ? null : (
          <Chip
            testID={`garage-default-${vehicle.id}`}
            label={t('garage.makeDefault')}
            selected={false}
            onPress={onMakeDefault}
          />
        )}
        {canRetire ? (
          <Chip
            testID={`garage-retire-${vehicle.id}`}
            label={t('garage.retire')}
            selected={false}
            onPress={onRetire}
          />
        ) : null}
      </View>
    </Card>
  );
}

const styles = StyleSheet.create({
  body: { gap: space.md, paddingBottom: space.xxl },
  detail: { ...type.caption, color: colors.muted },
  actions: { flexDirection: 'row', gap: space.xs, marginTop: space.xs },
  fuels: { flexDirection: 'row', flexWrap: 'wrap', gap: space.xs },
  problem: { ...type.body, color: colors.danger },
});
