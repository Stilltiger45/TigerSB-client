import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

/// Which theme is on: false is the dark one. Widgets read [Palette] when
/// they build, so TigerApp rebuilds every widget when this changes.
final lightTheme = ValueNotifier<bool>(false);

/// TigerSB colours: orange, black and white. In the light theme black and
/// white swap places: [black] is the background and [white] the text.
abstract final class Palette {
  static bool get light => lightTheme.value;

  static Color get black => light ? const Color(0xFFF6F1EA) : const Color(0xFF070708);
  static const ink = Color(0xFF1A0D03); // text on orange
  static const orange = Color(0xFFFF7A1A);
  static Color get orangeLight => light ? const Color(0xFFD25A00) : const Color(0xFFFFB15C);
  static const orangeDeep = Color(0xFFFF5A00);
  static Color get white => light ? const Color(0xFF1E150D) : const Color(0xFFF7F3EE);
  static Color get muted => white.withValues(alpha: 0.65);
  static Color get faint => white.withValues(alpha: 0.38);
  static Color get error => light ? const Color(0xFFD33434) : const Color(0xFFFF6B6B);

  /// The colour of light on glass: white on the dark theme, a warm grey on
  /// the light one, where white would not show.
  static Color get shine => light ? const Color(0xFF9C8A78) : const Color(0xFFFFFFFF);

  /// Glass fill from its top left to its bottom right, and its thin edge.
  static List<Color> get glassFill =>
      light ? const [Color(0xB3FFFFFF), Color(0x73FFFFFF)] : const [Color(0x17FFFFFF), Color(0x05FFFFFF)];
  static List<Color> get glassEdge => light
      ? const [Color(0xFFFFFFFF), Color(0x141E150D), Color(0x261E150D)]
      : const [Color(0x59FFFFFF), Color(0x14FFFFFF), Color(0x26FFFFFF)];

  /// Menus, dialogs and snack bars.
  static Color get popup => light ? const Color(0xF7FFFFFF) : const Color(0xF0141416);

  /// The orange fill of the power button, the mode thumb and the main button.
  static const fire = LinearGradient(
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
    colors: [Color(0xFFFFB15C), orange, orangeDeep],
    stops: [0, 0.55, 1],
  );
}

/// The app's theme, dark or light as the user chose in «О приложении»,
/// whatever the system says.
ThemeData tigerTheme() {
  final brightness = Palette.light ? Brightness.light : Brightness.dark;
  final scheme = ColorScheme.fromSeed(seedColor: Palette.orange, brightness: brightness).copyWith(
    primary: Palette.orange,
    onPrimary: Palette.ink,
    secondary: Palette.orangeLight,
    surface: Palette.black,
    onSurface: Palette.white,
    onSurfaceVariant: Palette.muted,
    error: Palette.error,
    outline: Palette.white.withValues(alpha: 0.2),
  );
  final base = ThemeData(colorScheme: scheme, useMaterial3: true, brightness: brightness);
  final shape = RoundedRectangleBorder(
    borderRadius: BorderRadius.circular(20),
    side: BorderSide(color: Palette.white.withValues(alpha: 0.15)),
  );
  return base.copyWith(
    scaffoldBackgroundColor: Palette.black,
    textTheme: base.textTheme.apply(bodyColor: Palette.white, displayColor: Palette.white),
    appBarTheme: AppBarTheme(
      backgroundColor: Colors.transparent,
      surfaceTintColor: Colors.transparent,
      elevation: 0,
      foregroundColor: Palette.white,
      titleTextStyle: TextStyle(fontSize: 18, fontWeight: FontWeight.w600, color: Palette.white),
      systemOverlayStyle: overlayStyle(),
    ),
    popupMenuTheme: PopupMenuThemeData(
      color: Palette.popup,
      surfaceTintColor: Colors.transparent,
      shape: shape,
      textStyle: TextStyle(color: Palette.white, fontSize: 15),
    ),
    dialogTheme: DialogThemeData(
      backgroundColor: Palette.light ? const Color(0xFFFFFBF6) : const Color(0xFF121214),
      surfaceTintColor: Colors.transparent,
      shape: shape,
    ),
    snackBarTheme: SnackBarThemeData(
      behavior: SnackBarBehavior.floating,
      backgroundColor: Palette.popup,
      contentTextStyle: TextStyle(color: Palette.white),
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
      fillColor: Palette.light ? const Color(0x99FFFFFF) : Palette.white.withValues(alpha: 0.06),
      hintStyle: TextStyle(color: Palette.faint),
      labelStyle: TextStyle(color: Palette.muted),
      floatingLabelStyle: TextStyle(color: Palette.orangeLight),
      border: OutlineInputBorder(borderRadius: BorderRadius.circular(18)),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(18),
        borderSide: BorderSide(color: Palette.white.withValues(alpha: 0.15)),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(18),
        borderSide: const BorderSide(color: Palette.orange, width: 1.4),
      ),
      errorBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(18),
        borderSide: BorderSide(color: Palette.error),
      ),
      focusedErrorBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(18),
        borderSide: BorderSide(color: Palette.error, width: 1.4),
      ),
    ),
  );
}

/// Status bar and navigation bar icons that show on the theme's background.
SystemUiOverlayStyle overlayStyle() =>
    (Palette.light ? SystemUiOverlayStyle.dark : SystemUiOverlayStyle.light).copyWith(
      statusBarColor: Colors.transparent,
      systemNavigationBarColor: Palette.black,
      systemNavigationBarIconBrightness: Palette.light ? Brightness.dark : Brightness.light,
    );

/// Small uppercase label with wide tracking, as section captions.
TextStyle get capsStyle =>
    TextStyle(fontSize: 11, fontWeight: FontWeight.w600, letterSpacing: 2, color: Palette.orangeLight);
