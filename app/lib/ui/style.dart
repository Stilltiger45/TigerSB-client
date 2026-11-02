import 'package:flutter/material.dart';

/// The client's style, which the user picks in «О приложении». It sets the
/// main colour, the logo and the launcher icon, the words of the routing
/// modes and the shape of the effects. Widgets read it when they build, so
/// TigerApp rebuilds every widget when it changes, as with the theme.
final appStyle = ValueNotifier<AppStyle>(AppStyle.tiger);

/// What the background sparks and the ring's heads look like.
enum Mote { spark, flake, slash, note, sun, star }

enum AppStyle {
  tiger(
    title: 'Тигр',
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
      auto: 'Тигра не удержит ни одна клетка. Он сам находит лазейку там, где другие видят только решётку.',
      basic: 'Своя территория — открыто, чужая — бесшумной тенью. Тигр гуляет, где хочет.',
      all: 'Ни следа, ни звука. Один свободный прыжок сквозь ночь, и никто не узнает, куда.',
    ),
  ),
  // A faction of free stalkers: the Zone has no master. The wolf patch's
  // lime on black, dust rising in the dawn light.
  zarya(
    title: 'Заря',
    logo: 'assets/styles/zarya.png',
    mote: Mote.flake,
    accent: Color(0xFF8BE33A),
    accentDeep: Color(0xFF4CB82A),
    accentTop: Color(0xFFC8FF6A),
    accentLightDark: Color(0xFFB8F27A),
    accentLightLight: Color(0xFF3C8A25),
    second: Color(0xFFFFD66B),
    ink: Color(0xFF0D1A05),
    words: StyleWords(
      autoHint: 'ВОЛЬНЫЙ ХОД',
      basicHint: 'СВОИ НАПРЯМУЮ',
      allHint: 'ВЕСЬ ОТРЯД ЧЕРЕЗ VPN',
      connecting: 'ИЩУ ТРОПУ',
      idle: 'ЖМИ, СТАЛКЕР',
      auto: 'У Зоны нет хозяина. Сталкер сам выбирает тропу и ни у кого не спрашивает дороги.',
      basic: 'Своих пропускаем, блокпосты обходим стороной. Свобода не ждёт разрешения.',
      all: 'Отряд уходит в Зону целиком, и ни один блокпост не узнает куда. Свобода или ничего.',
    ),
  ),
  // The king of curses: crimson, slashes that cut the air, and a domain
  // spreading from the button.
  curse(
    title: 'Король проклятий',
    logo: 'assets/styles/curse.png',
    mote: Mote.slash,
    accent: Color(0xFFEE1C2E),
    accentDeep: Color(0xFF8E0E1F),
    accentTop: Color(0xFFFF5A5A),
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
      auto: 'Мне не нужно ничьё разрешение. Любую преграду я просто рассеку.',
      basic: 'Обеты я заключаю сам и сам решаю, чему подчиняться. Короля никто не свяжет.',
      all: 'Здесь моя территория. В ней нет правил, кроме моей воли.',
    ),
  ),
  // An idol's stage: teal with pink, notes flying and the ring pulsing to
  // the beat.
  miku(
    title: 'Айдол',
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
      auto: 'Мой голос пройдёт сквозь любые стены. Пой свободно, а я подстроюсь под тебя!',
      basic: 'Поём дуэтом: один голос остаётся дома, другой летит по всему миру. У музыки нет границ!',
      all: 'На полную громкость! Песню, которую поёт весь мир, никому не заглушить.',
    ),
  ),
  // The sun god of a rubber pirate: white and gold, the laughing drums of
  // liberation, sunlight scattered all over the screen.
  sun(
    title: 'Бог Солнца',
    logo: 'assets/styles/sun.png',
    mote: Mote.sun,
    accent: Color(0xFFFFD84A),
    accentDeep: Color(0xFFF5A300),
    accentTop: Color(0xFFFFF8D6),
    accentLightDark: Color(0xFFFFE680),
    accentLightLight: Color(0xFFA86F00),
    second: Color(0xFFFFFFFF),
    ink: Color(0xFF2B1B00),
    words: StyleWords(
      autoHint: 'РИТМ ОСВОБОЖДЕНИЯ',
      basicHint: 'РЕЗИНОВЫЙ МИР',
      allHint: 'АБСОЛЮТНАЯ СВОБОДА',
      connecting: 'СЛЫШУ БАРАБАНЫ',
      idle: 'ЖМИ, И НА СВОБОДУ!',
      auto: 'Слышишь барабаны? Это ритм освобождения. Я сражаюсь так, как хочу, и смеюсь!',
      basic: 'Мир гнётся, как резина, в любую сторону. Хочу — домой, хочу — в открытое море!',
      all: 'Солнце светит всем, и его не запереть. Нет никого свободнее того, кто смеётся.',
    ),
  ),
  // A star of a stand: violet, fists faster than the eye, and the moment
  // the world stands still.
  star(
    title: 'Звезда',
    logo: 'assets/styles/star.png',
    mote: Mote.star,
    accent: Color(0xFF9D5CFF),
    accentDeep: Color(0xFF5A22C9),
    accentTop: Color(0xFFD2B4FF),
    accentLightDark: Color(0xFFC6A2FF),
    accentLightLight: Color(0xFF6A2FD6),
    second: Color(0xFF6FB6FF),
    ink: Color(0xFFFBF7FF),
    words: StyleWords(
      autoHint: 'ОРА-ОРА',
      basicHint: 'ЗВЁЗДНЫЙ ПАЛЕЦ',
      allHint: 'ВРЕМЯ, СТОЙ',
      connecting: 'СТЕНД ПРОБУЖДАЕТСЯ',
      idle: 'ЖМИ. И ДЕЛО С КОНЦОМ',
      auto: 'Ора-ора-ора! Каждая преграда получает удар раньше, чем успевает моргнуть. Путь свободен.',
      basic: 'Один точный удар на каждую цель. Звёзды не знают границ, и я тоже.',
      all: 'Время остановилось. Пока весь мир замер, ты свободен идти куда угодно. Ну и дела.',
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
