import 'package:flutter/material.dart';

/// The client's style, which the user picks in «О приложении». It sets the
/// main colour, the logo and the launcher icon, the words of the routing
/// modes and the shape of the effects. Widgets read it when they build, so
/// TigerApp rebuilds every widget when it changes, as with the theme.
final appStyle = ValueNotifier<AppStyle>(AppStyle.tiger);

/// What the background sparks and the ring's heads look like.
enum Mote { spark, flake, slash, note }

enum AppStyle {
  tiger(
    title: 'Tiger-SB',
    logo: 'assets/tiger.png',
    mote: Mote.spark,
    accent: Color(0xFFFF7A1A),
    accentDeep: Color(0xFFFF5A00),
    accentTop: Color(0xFFFFB15C),
    accentLightDark: Color(0xFFFFB15C),
    accentLightLight: Color(0xFFD25A00),
    second: null,
    ink: Color(0xFF1A0D03),
    words: StyleWords(
      autoHint: 'УМНАЯ МАРШРУТИЗАЦИЯ',
      basicHint: 'РОССИЯ НАПРЯМУЮ',
      allHint: 'ВСЁ ЧЕРЕЗ VPN',
      connecting: 'ИЩУ СЕРВЕР',
      idle: 'НАЖМИТЕ, ЧТОБЫ ВКЛЮЧИТЬ',
      auto: 'Как «Базовая», и обход блокировок DPI подбирается сам под вашу сеть.',
      basic: 'Российские сайты (.ru, .рф, российские IP) — напрямую, остальное — через VPN.',
      all: 'Весь трафик — через VPN.',
    ),
  ),
  // A faction of free stalkers: the Zone has no master. Dawn gold over its
  // green, and dust rising in the morning light.
  zarya(
    title: 'Заря',
    logo: 'assets/styles/zarya.png',
    mote: Mote.flake,
    accent: Color(0xFF8FD14F),
    accentDeep: Color(0xFF3FAE4A),
    accentTop: Color(0xFFFFE27A),
    accentLightDark: Color(0xFFFFD66B),
    accentLightLight: Color(0xFF3C8A25),
    second: Color(0xFFFFD66B),
    ink: Color(0xFF0D1A05),
    words: StyleWords(
      autoHint: 'ВОЛЬНЫЙ ХОД',
      basicHint: 'СВОИ НАПРЯМУЮ',
      allHint: 'ВЕСЬ ОТРЯД ЧЕРЕЗ VPN',
      connecting: 'ИЩУ ТРОПУ',
      idle: 'ЖМИ, СТАЛКЕР',
      auto: 'Как «Базовая», и тропу в обход блокировок DPI находит сам: у Зоны нет хозяина.',
      basic: 'Российские сайты (.ru, .рф, российские IP) — напрямую, остальное — через VPN, в обход блокпостов.',
      all: 'Весь трафик — через VPN: ни один байт не пройдёт через блокпост.',
    ),
  ),
  // The king of curses: crimson, slashes that cut the air, and a domain
  // spreading from the button.
  curse(
    title: 'Король проклятий',
    logo: 'assets/styles/curse.png',
    mote: Mote.slash,
    accent: Color(0xFFE5243B),
    accentDeep: Color(0xFF8E0E1F),
    accentTop: Color(0xFFFF7A86),
    accentLightDark: Color(0xFFFF7A86),
    accentLightLight: Color(0xFFB5162B),
    second: null,
    ink: Color(0xFFFFF3F4),
    words: StyleWords(
      autoHint: 'РАССЕЧЕНИЕ',
      basicHint: 'СВЯЗАННЫЙ ОБЕТ',
      allHint: 'РАСШИРЕНИЕ ТЕРРИТОРИИ',
      connecting: 'ВЫБИРАЮ ЦЕЛЬ',
      idle: 'НАЖМИ, ЕСЛИ ОСМЕЛИШЬСЯ',
      auto: 'Как «Базовая», и обход блокировок DPI сам рассекает их под вашу сеть.',
      basic: 'Обет: российские сайты (.ru, .рф, российские IP) — напрямую, всё остальное — через VPN.',
      all: 'Весь трафик — на моей территории, через VPN.',
    ),
  ),
  // An idol's stage: teal with pink, notes flying and the ring pulsing to
  // the beat.
  miku(
    title: 'Mike',
    logo: 'assets/styles/miku.png',
    mote: Mote.note,
    accent: Color(0xFF39C5BB),
    accentDeep: Color(0xFF13998F),
    accentTop: Color(0xFF8FF5EA),
    accentLightDark: Color(0xFF8FF0E6),
    accentLightLight: Color(0xFF0F8A82),
    second: Color(0xFFFF5FA2),
    ink: Color(0xFF00211F),
    words: StyleWords(
      autoHint: 'АВТОТЮН',
      basicHint: 'ДУЭТ',
      allHint: 'НА ПОЛНУЮ ГРОМКОСТЬ',
      connecting: 'НАСТРАИВАЮСЬ',
      idle: 'НАЖМИ, И НАЧНЁМ',
      auto: 'Как «Базовая», и обход блокировок DPI сам подстраивается под вашу сеть, как автотюн под голос.',
      basic: 'Дуэт: российские сайты (.ru, .рф, российские IP) — напрямую, остальное — через VPN.',
      all: 'Весь трафик — через VPN, на полную громкость.',
    ),
  );

  const AppStyle({
    required this.title,
    required this.logo,
    required this.mote,
    required this.accent,
    required this.accentDeep,
    required this.accentTop,
    required this.accentLightDark,
    required this.accentLightLight,
    required this.second,
    required this.ink,
    required this.words,
  });

  final String title;

  /// The in-app logo, as the launcher icon.
  final String logo;
  final Mote mote;

  /// The main colour, its dark end and its light top for gradients.
  final Color accent, accentDeep, accentTop;

  /// The lighter main colour for text on the dark theme and its darker
  /// counterpart on the light one.
  final Color accentLightDark, accentLightLight;

  /// A second colour for a share of the sparks; null for the light main one.
  final Color? second;

  /// Text and icons on the main colour.
  final Color ink;

  final StyleWords words;

  /// The saved style by its name; the standard one for an unknown name.
  static AppStyle byName(String? name) => values.firstWhere((s) => s.name == name, orElse: () => tiger);
}

/// The words of the main screen in a style: the caption under the button
/// for each mode and state, and each mode's description.
class StyleWords {
  const StyleWords({
    required this.autoHint,
    required this.basicHint,
    required this.allHint,
    required this.connecting,
    required this.idle,
    required this.auto,
    required this.basic,
    required this.all,
  });

  final String autoHint, basicHint, allHint, connecting, idle;
  final String auto, basic, all;
}
