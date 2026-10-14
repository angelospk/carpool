// The ΚΤΕΛ a stranded passenger can fall back on: the one of the prefecture
// the trip starts from. Only the Thracian corridor the pilot runs along is
// known by name; anywhere else keeps ΚΤΕΛ Έβρου, where the pilot started.

export type Ktel = { name: string; url: string };

const EVROS: Ktel = { name: 'Έβρου', url: 'https://ktelevrou.gr/' };
const RODOPI: Ktel = { name: 'Ροδόπης', url: 'https://ktelrodopis.gr/' };
const XANTHI: Ktel = { name: 'Ξάνθης', url: 'https://www.ktelxanthis.gr/' };

const fold = (s: string) =>
  s
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .toLocaleLowerCase('el')
    .trim();

// Greek and Latin spellings of the towns people start from.
// A Map, not an object: a label like «constructor» must not find anything.
const TOWNS = new Map<string, Ktel>();
for (const town of ['Κομοτηνή', 'Komotini', 'Σάπες', 'Sapes', 'Ίασμος', 'Iasmos', 'Ξυλαγανή', 'Μαρώνεια', 'Αρριανά', 'Οργάνη']) {
  TOWNS.set(fold(town), RODOPI);
}
for (const town of ['Ξάνθη', 'Xanthi', 'Άβδηρα', 'Avdira', 'Σταυρούπολη', 'Γενισέα', 'Εχίνος', 'Μύκη']) {
  TOWNS.set(fold(town), XANTHI);
}

export function ktelFor(originLabel: string): Ktel {
  return TOWNS.get(fold(originLabel)) ?? EVROS;
}
