import 'package:flutter/material.dart';

/// TigerSB colours: orange, black and white.
abstract final class Palette {
  static const black = Color(0xFF070708);
  static const ink = Color(0xFF1A0D03); // text on orange
  static const orange = Color(0xFFFF7A1A);
  static const orangeLight = Color(0xFFFFB15C);
  static const orangeDeep = Color(0xFFFF5A00);
  static const white = Color(0xFFF7F3EE);
  static const muted = Color(0xA6F7F3EE); // 65 % white
  static const faint = Color(0x61F7F3EE); // 38 % white
  static const error = Color(0xFFFF6B6B);

  /// The orange fill of the power button, the mode thumb and the main button.
  static const fire = LinearGradient(
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
    colors: [orangeLight, orange, orangeDeep],
    stops: [0, 0.55, 1],
  );
}

/// The app's only theme: dark, whatever the system says.
ThemeData tigerTheme() {
  final scheme = ColorScheme.fromSeed(seedColor: Palette.orange, brightness: Brightness.dark).copyWith(
    primary: Palette.orange,
    onPrimary: Palette.ink,
    secondary: Palette.orangeLight,
    surface: Palette.black,
    onSurface: Palette.white,
    onSurfaceVariant: Palette.muted,
    error: Palette.error,
    outline: const Color(0x33F7F3EE),
  );
  final base = ThemeData(colorScheme: scheme, useMaterial3: true, brightness: Brightness.dark);
  final shape = RoundedRectangleBorder(
    borderRadius: BorderRadius.circular(20),
    side: const BorderSide(color: Color(0x26F7F3EE)),
  );
  return base.copyWith(
    scaffoldBackgroundColor: Palette.black,
    textTheme: base.textTheme.apply(bodyColor: Palette.white, displayColor: Palette.white),
    appBarTheme: const AppBarTheme(
      backgroundColor: Colors.transparent,
      surfaceTintColor: Colors.transparent,
      elevation: 0,
      foregroundColor: Palette.white,
      titleTextStyle: TextStyle(fontSize: 18, fontWeight: FontWeight.w600, color: Palette.white),
    ),
    popupMenuTheme: PopupMenuThemeData(
      color: const Color(0xF0141416),
      surfaceTintColor: Colors.transparent,
      shape: shape,
      textStyle: const TextStyle(color: Palette.white, fontSize: 15),
    ),
    dialogTheme: DialogThemeData(
      backgroundColor: const Color(0xFF121214),
      surfaceTintColor: Colors.transparent,
      shape: shape,
    ),
    snackBarTheme: SnackBarThemeData(
      behavior: SnackBarBehavior.floating,
      backgroundColor: const Color(0xF01B1B1E),
      contentTextStyle: const TextStyle(color: Palette.white),
      shape: shape,
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(backgroundColor: Palette.orange, foregroundColor: Palette.ink),
    ),
    textButtonTheme: TextButtonThemeData(style: TextButton.styleFrom(foregroundColor: Palette.orangeLight)),
    progressIndicatorTheme: const ProgressIndicatorThemeData(color: Palette.orange),
    textSelectionTheme: const TextSelectionThemeData(
      cursorColor: Palette.orange,
      selectionColor: Color(0x55FF7A1A),
      selectionHandleColor: Palette.orange,
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: const Color(0x0FF7F3EE),
      hintStyle: const TextStyle(color: Palette.faint),
      labelStyle: const TextStyle(color: Palette.muted),
      floatingLabelStyle: const TextStyle(color: Palette.orangeLight),
      border: OutlineInputBorder(borderRadius: BorderRadius.circular(18)),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(18),
        borderSide: const BorderSide(color: Color(0x26F7F3EE)),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(18),
        borderSide: const BorderSide(color: Palette.orange, width: 1.4),
      ),
      errorBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(18),
        borderSide: const BorderSide(color: Palette.error),
      ),
      focusedErrorBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(18),
        borderSide: const BorderSide(color: Palette.error, width: 1.4),
      ),
    ),
  );
}

/// Small uppercase label with wide tracking, as section captions.
const capsStyle = TextStyle(fontSize: 11, fontWeight: FontWeight.w600, letterSpacing: 2, color: Palette.orangeLight);
