import 'package:flutter/material.dart';

/// The colours the app draws with. Quiet, adult, one accent.
class Ink {
  const Ink._();

  /// Text and the marks that are not the accent.
  static const Color ink = Color(0xFF14211C);

  /// Secondary text.
  static const Color muted = Color(0xFF5F6B66);

  /// Hairline dividers and control borders.
  static const Color line = Color(0xFFDDE3E0);

  /// The app background.
  static const Color surface = Color(0xFFF4F6F5);

  /// The deep green: primary buttons, selected items, the route line.
  static const Color accent = Color(0xFF1F5C45);

  /// Text on the accent.
  static const Color onAccent = Color(0xFFFFFFFF);

  /// Refusals and destructive action.
  static const Color danger = Color(0xFFB3261E);

  /// A road that is not the one picked.
  static const Color alternative = Color(0xFF9AA5A0);

  /// The walker's own position.
  static const Color me = Color(0xFF2F6FDE);
}

/// The one theme of the app.
ThemeData carpoolTheme() {
  const scheme = ColorScheme.light(
    primary: Ink.accent,
    onPrimary: Ink.onAccent,
    secondary: Ink.accent,
    onSecondary: Ink.onAccent,
    surface: Ink.surface,
    onSurface: Ink.ink,
    error: Ink.danger,
    onError: Colors.white,
    outline: Ink.line,
    outlineVariant: Ink.line,
  );

  final base = ThemeData(useMaterial3: true, colorScheme: scheme);
  final text = base.textTheme.apply(bodyColor: Ink.ink, displayColor: Ink.ink);

  const buttonShape = RoundedRectangleBorder(
    borderRadius: BorderRadius.all(Radius.circular(12)),
  );

  return base.copyWith(
    scaffoldBackgroundColor: Ink.surface,
    textTheme: text,
    appBarTheme: const AppBarTheme(
      backgroundColor: Ink.surface,
      foregroundColor: Ink.ink,
      elevation: 0,
      scrolledUnderElevation: 0,
      surfaceTintColor: Colors.transparent,
      centerTitle: false,
    ),
    dividerTheme: const DividerThemeData(color: Ink.line, thickness: 1, space: 1),
    snackBarTheme: const SnackBarThemeData(
      backgroundColor: Ink.ink,
      contentTextStyle: TextStyle(color: Colors.white, fontSize: 15),
      behavior: SnackBarBehavior.floating,
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        backgroundColor: Ink.accent,
        foregroundColor: Ink.onAccent,
        disabledBackgroundColor: Ink.line,
        disabledForegroundColor: Ink.muted,
        minimumSize: const Size(0, 52),
        shape: buttonShape,
        textStyle: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        foregroundColor: Ink.ink,
        backgroundColor: Colors.white,
        disabledForegroundColor: Ink.muted,
        minimumSize: const Size(0, 52),
        side: const BorderSide(color: Ink.line),
        shape: buttonShape,
        textStyle: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600),
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(
        foregroundColor: Ink.accent,
        textStyle: const TextStyle(fontSize: 15, fontWeight: FontWeight.w600),
      ),
    ),
    segmentedButtonTheme: SegmentedButtonThemeData(
      style: ButtonStyle(
        shape: const WidgetStatePropertyAll(buttonShape),
        side: const WidgetStatePropertyAll(BorderSide(color: Ink.line)),
        backgroundColor: WidgetStateProperty.resolveWith(
          (states) => states.contains(WidgetState.selected) ? Ink.accent : Colors.white,
        ),
        foregroundColor: WidgetStateProperty.resolveWith(
          (states) => states.contains(WidgetState.selected) ? Ink.onAccent : Ink.ink,
        ),
        textStyle: const WidgetStatePropertyAll(
          TextStyle(fontSize: 15, fontWeight: FontWeight.w600),
        ),
      ),
    ),
    chipTheme: ChipThemeData(
      backgroundColor: Colors.white,
      selectedColor: Ink.accent,
      side: const BorderSide(color: Ink.line),
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.all(Radius.circular(12))),
      labelStyle: const TextStyle(color: Ink.ink, fontSize: 14),
      secondaryLabelStyle: const TextStyle(color: Ink.onAccent, fontSize: 14),
      showCheckmark: false,
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: Colors.white,
      hintStyle: const TextStyle(color: Ink.muted),
      contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 14),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(12),
        borderSide: const BorderSide(color: Ink.line),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(12),
        borderSide: const BorderSide(color: Ink.line),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(12),
        borderSide: const BorderSide(color: Ink.accent),
      ),
    ),
    dialogTheme: const DialogThemeData(
      backgroundColor: Colors.white,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.all(Radius.circular(16))),
    ),
    bottomSheetTheme: const BottomSheetThemeData(
      backgroundColor: Colors.white,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
    ),
  );
}
