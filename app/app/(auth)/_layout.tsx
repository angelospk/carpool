import { Stack } from 'expo-router';

// Without this file the three sign-in screens are not a group, they are three
// loose routes: `(auth)/phone`, `(auth)/otp`, `(auth)/profile-setup`. The root
// layout declares `<Stack.Screen name="(auth)" />`, which then points at
// nothing, and expo-router says so on every load —
//
//   No route named "(auth)" exists in nested children
//
// A warning nobody reads is still a navigator that does not exist, so the
// options set on it were never applied either.
//
// Signing in is one flow with three steps, and it owns its own header the way
// the tabs own theirs.
export default function AuthLayout() {
  return <Stack screenOptions={{ headerShown: false }} />;
}
