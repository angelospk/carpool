// Component tests run under jest-expo, which supplies the React Native module
// mocks. Only the modules that touch native code are stubbed by hand.
module.exports = {
  preset: 'jest-expo',
  setupFilesAfterEnv: ['<rootDir>/jest.setup.js'],
  transformIgnorePatterns: [
    'node_modules/(?!((jest-)?react-native|@react-native(-community)?|expo(nent)?|@expo(nent)?/.*|@expo-google-fonts/.*|react-navigation|@react-navigation/.*|@sentry/react-native|native-base|react-native-svg))',
  ],
  collectCoverageFrom: ['src/**/*.{ts,tsx}', 'app/**/*.tsx'],
  moduleNameMapper: {
    // maplibre-gl is WebGL and ESM: jsdom cannot run it and jest's resolver
    // cannot load it. See src/map/__stubs__/maplibre-gl.ts.
    '^maplibre-gl$': '<rootDir>/src/map/__stubs__/maplibre-gl.ts',
    // react-native-maps is a native module; outside a built app importing it
    // throws before a test can run. Same reasoning.
    '^react-native-maps$': '<rootDir>/src/map/__stubs__/react-native-maps.tsx',
  },
  // react-query keeps a cache timer alive after a test finishes; without this
  // jest reports a leak that is really just an idle timer.
  forceExit: true,
};
