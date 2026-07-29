// Greek is the default; English is the fallback. The pilot community is Greek,
// so untranslated English strings would be the visible bug, not the reverse.

import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';

import el from './el.json';
import en from './en.json';

void i18n.use(initReactI18next).init({
  resources: { el: { translation: el }, en: { translation: en } },
  lng: 'el',
  fallbackLng: 'en',
  interpolation: { escapeValue: false },
});

export default i18n;
