// Component tests run under jest-expo, which supplies the React Native module
// mocks. Only the modules that touch native code are stubbed by hand.
module.exports = {
  preset: 'jest-expo',
  setupFilesAfterEnv: ['<rootDir>/jest.setup.js'],
  transformIgnorePatterns: [
    'node_modules/(?!((jest-)?react-native|@react-native(-community)?|expo(nent)?|@expo(nent)?/.*|@expo-google-fonts/.*|react-navigation|@react-navigation/.*|@sentry/react-native|native-base|react-native-svg))',
  ],
  collectCoverageFrom: ['src/**/*.{ts,tsx}', 'app/**/*.tsx'],
  // react-query keeps a cache timer alive after a test finishes; without this
  // jest reports a leak that is really just an idle timer.
  forceExit: true,
};
