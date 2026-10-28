import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:tigersb/app.dart';
import 'package:tigersb/core.dart';
import 'package:tigersb/licenses.dart';
import 'package:tigersb/screens/about_screen.dart';
import 'package:tigersb/ui/palette.dart';

class FakeCore implements Core {
  ProfileInfo? profile;
  Mode currentMode = Mode.auto;
  final statuses = StreamController<VpnStatus>.broadcast();
  final incoming = StreamController<String>.broadcast();
  final added = <String>[];
  int connects = 0;

  /// The saved profiles besides [profile], the one in use.
  final others = <ProfileInfo>[];

  ProfileInfo _profile(String name, {List<String> userApps = const []}) => ProfileInfo(
    id: name,
    name: name,
    hasRU: true,
    fetched: DateTime.now(),
    exits: const [],
    shieldApps: const ['ru.oneme.app'],
    userApps: userApps,
  );

  @override
  Future<ProfileInfo?> current() async => profile;

  @override
  Future<List<ProfileInfo>> profiles() async => [?profile, ...others];

  @override
  Future<ProfileInfo> addKey(String key) async {
    if (!key.startsWith('tigersb://')) throw CoreError('неверный ключ');
    added.add(key);
    if (profile != null) others.add(profile!);
    return profile = _profile(added.length == 1 ? 'Дом' : 'Ключ ${added.length}');
  }

  final refreshed = <String>[];

  @override
  Future<ProfileInfo> refresh([String? id]) async {
    refreshed.add(id ?? profile!.id);
    return id == null || id == profile!.id ? profile! : others.firstWhere((p) => p.id == id);
  }

  @override
  Future<ProfileInfo> select(String id) async {
    final p = others.firstWhere((p) => p.id == id);
    others
      ..remove(p)
      ..insert(0, profile!);
    return profile = p;
  }

  @override
  Future<ProfileInfo?> delete(String id) async {
    if (profile?.id == id) {
      profile = others.isEmpty ? null : others.removeAt(0);
    } else {
      others.removeWhere((p) => p.id == id);
    }
    return profile;
  }

  @override
  Future<Mode> mode() async => currentMode;

  @override
  Future<void> setMode(Mode m) async => currentMode = m;

  bool light = false;

  @override
  Future<bool> lightTheme() async => light;

  @override
  Future<void> setLightTheme(bool v) async => light = v;

  VpnStats nextStats = const VpnStats(up: 0, down: 0, ping: -1, uptime: 0);
  int statsCalls = 0;

  @override
  Future<VpnStats> stats() async {
    statsCalls++;
    return nextStats;
  }

  @override
  Future<bool> connect() async {
    connects++;
    statuses.add(const VpnStatus(Phase.connected));
    return true;
  }

  @override
  Future<void> disconnect() async => statuses.add(const VpnStatus(Phase.disconnected));

  @override
  Stream<VpnStatus> status() => statuses.stream;

  List<String> userApps = [];

  @override
  Future<List<AppInfo>> apps() async => const [
    AppInfo(package: 'ru.oneme.app', label: 'MAX'),
    AppInfo(package: 'org.example.game', label: 'Игра'),
  ];

  @override
  Future<void> setUserApps(List<String> packages) async {
    userApps = packages;
    profile = _profile(profile!.name, userApps: packages);
  }

  int journals = 0;
  final opened = <String>[];

  @override
  Future<AboutInfo> about() async => const AboutInfo(version: '0.1.0', xray: '26.3.27', go: 'go1.26.0');

  @override
  Future<bool> openUrl(String url) async {
    opened.add(url);
    return true;
  }

  @override
  Future<void> shareJournal() async => journals++;

  UpdateInfo? update;
  bool installAllowed = true;
  int installs = 0;
  final progress = StreamController<double>.broadcast();
  final failures = StreamController<String>.broadcast();

  @override
  Future<UpdateInfo?> checkUpdate() async => update;

  @override
  Future<bool> installUpdate() async {
    installs++;
    progress.add(0.5);
    return installAllowed;
  }

  @override
  Stream<double> updateProgress() => progress.stream;

  @override
  Stream<String> updateFailures() => failures.stream;

  @override
  Future<String?> takeKey() async => null;

  @override
  Stream<String> keys() => incoming.stream;
}

void main() {
  // The sparks and rings move forever; pumpAndSettle needs a still screen,
  // as when the system asks to remove animations.
  setUp(() {
    TestWidgetsFlutterBinding.ensureInitialized().platformDispatcher.accessibilityFeaturesTestValue =
        const FakeAccessibilityFeatures(disableAnimations: true);
  });
  tearDown(() => TestWidgetsFlutterBinding.instance.platformDispatcher.clearAccessibilityFeaturesTestValue());

  testWidgets('a bad key shows the error, a good one connects', (tester) async {
    final core = FakeCore();
    await tester.pumpWidget(TigerApp(core: core));
    await tester.pumpAndSettle();
    expect(find.text('Подключить'), findsOneWidget);

    await tester.enterText(find.byType(TextField), 'nonsense');
    await tester.tap(find.text('Подключить'));
    await tester.pumpAndSettle();
    expect(find.text('неверный ключ'), findsOneWidget);
    expect(core.connects, 0);

    await tester.enterText(find.byType(TextField), 'tigersb://add?url=https://x&pk=y');
    await tester.tap(find.text('Подключить'));
    await tester.pumpAndSettle();
    expect(core.connects, 1);
    expect(find.text('Дом'), findsOneWidget);
  });

  testWidgets('main screen switches mode and disconnects', (tester) async {
    final core = FakeCore()..profile = ProfileInfo(name: 'Дом', hasRU: true, fetched: DateTime.now(), exits: const []);
    await tester.pumpWidget(TigerApp(core: core));
    await tester.pumpAndSettle();
    expect(find.text('Отключено'), findsOneWidget);

    await tester.tap(find.byKey(const Key('power')));
    await tester.pumpAndSettle();
    expect(find.text('Подключено'), findsOneWidget);

    expect(find.textContaining('самый быстрый сервер'), findsOneWidget);
    await tester.tap(find.text('Базовая'));
    await tester.pumpAndSettle();
    expect(core.currentMode, Mode.basic);
    expect(find.textContaining('Российские сайты'), findsOneWidget);
    await tester.tap(find.text('Все'));
    await tester.pumpAndSettle();
    expect(core.currentMode, Mode.all);
    expect(find.text('Весь трафик — через VPN.'), findsOneWidget);

    await tester.tap(find.byKey(const Key('power')));
    await tester.pumpAndSettle();
    expect(find.text('Отключено'), findsOneWidget);
  });

  testWidgets('a tigersb:// link fills the key screen and connects', (tester) async {
    final core = FakeCore();
    await tester.pumpWidget(TigerApp(core: core));
    await tester.pumpAndSettle();
    core.incoming.add('tigersb://add?url=https://x&pk=y');
    await tester.pumpAndSettle();
    expect(core.added, ['tigersb://add?url=https://x&pk=y']);
    expect(core.connects, 1);
  });

  testWidgets('connected: speed, traffic and ping, read every 5 minutes', (tester) async {
    final core = FakeCore()
      ..profile = ProfileInfo(
        name: 'Чужой VPN',
        hasRU: false,
        foreign: true,
        fetched: DateTime.now(),
        exits: [
          ExitInfo(id: 'sub', role: 'foreign', transports: ['NL', 'DE', 'FI']),
        ],
      );
    await tester.pumpWidget(TigerApp(core: core));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('stats')), findsNothing);
    expect(find.textContaining('Серверов'), findsNothing);

    await tester.tap(find.byKey(const Key('power')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('stats')), findsOneWidget);
    expect(core.statsCalls, 0);

    // 10 MB down and 1 MB up in 10 seconds.
    core.nextStats = const VpnStats(up: 1 << 20, down: 10 << 20, ping: 42, uptime: 10);
    await tester.pump(const Duration(seconds: 10));
    await tester.pump();
    expect(core.statsCalls, 1);
    expect(find.text('↓ 10.0 МБ'), findsOneWidget);
    expect(find.text('↓ 8.4 Мбит/с'), findsOneWidget);
    expect(find.text('42 мс'), findsOneWidget);

    // The next reading comes five minutes later; the speed is over those minutes.
    core.nextStats = const VpnStats(up: 1 << 20, down: 10 << 20, ping: -1, uptime: 310);
    await tester.pump(const Duration(minutes: 4));
    expect(core.statsCalls, 1);
    await tester.pump(const Duration(minutes: 1));
    await tester.pump();
    expect(core.statsCalls, 2);
    expect(find.text('↓ 0 кбит/с'), findsOneWidget);
    expect(find.text('нет ответа'), findsOneWidget);

    await tester.tap(find.byKey(const Key('power')));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('stats')), findsNothing);
  });

  testWidgets('the profile list picks, updates, copies the link and deletes profiles', (tester) async {
    const key = 'tigersb://add?url=https%3A%2F%2Fpanel.example.org%2Fsub%2Fabc&pk=y';
    final core = FakeCore()
      ..profile = ProfileInfo(id: 'home', name: 'Дом', hasRU: true, fetched: DateTime.now(), exits: const [], key: key)
      ..others.add(ProfileInfo(id: 'work', name: 'Работа', hasRU: true, fetched: DateTime.now(), exits: const []));
    String? copied;
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform, (call) async {
      if (call.method == 'Clipboard.setData') copied = (call.arguments as Map)['text'] as String;
      return null;
    });
    await tester.pumpWidget(TigerApp(core: core));
    await tester.pumpAndSettle();

    // The menu on the right no longer changes or copies the key.
    await tester.tap(find.byType(PopupMenuButton<String>));
    await tester.pumpAndSettle();
    expect(find.text('Сменить ключ'), findsNothing);
    expect(find.text('Скопировать ключ подписки'), findsNothing);
    await tester.tap(find.text('Отправить журнал'));
    await tester.pumpAndSettle();
    expect(core.journals, 1);

    Future<void> open() async {
      await tester.tap(find.byKey(const Key('profiles')));
      await tester.pumpAndSettle();
    }

    Finder inRow(String id, String tip) =>
        find.descendant(of: find.byKey(Key('profile-$id')), matching: find.byTooltip(tip));

    await open();
    expect(find.byKey(const Key('profile-home')), findsOneWidget);
    expect(find.byKey(const Key('profile-work')), findsOneWidget);
    expect(find.text('Добавить профиль'), findsOneWidget);
    // A profile without a key to share has no link button.
    expect(inRow('work', 'Ссылка'), findsNothing);
    await tester.tap(inRow('home', 'Ссылка'));
    await tester.pumpAndSettle();
    expect(copied, key);

    await open();
    await tester.tap(inRow('work', 'Обновить'));
    await tester.pumpAndSettle();
    expect(core.refreshed, ['work']);

    await open();
    await tester.tap(find.byKey(const Key('profile-work')));
    await tester.pumpAndSettle();
    expect(core.profile!.name, 'Работа');
    expect(find.text('Работа'), findsOneWidget);

    await open();
    await tester.tap(inRow('home', 'Удалить'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Удалить'));
    await tester.pumpAndSettle();
    expect(core.others, isEmpty);
    expect(core.profile!.name, 'Работа');

    // The last one goes: back to the key screen.
    await open();
    await tester.tap(inRow('work', 'Удалить'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Удалить'));
    await tester.pumpAndSettle();
    expect(core.profile, isNull);
    expect(find.text('Подключить'), findsOneWidget);
  });

  testWidgets('one more profile is added from the list and from a link', (tester) async {
    final core = FakeCore()
      ..profile = ProfileInfo(id: 'home', name: 'Дом', hasRU: true, fetched: DateTime.now(), exits: const []);
    await tester.pumpWidget(TigerApp(core: core));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('profiles')));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Добавить профиль'));
    await tester.pumpAndSettle();
    expect(find.text('Новый профиль'), findsOneWidget);
    await tester.enterText(find.byType(TextField), 'tigersb://add?url=https://two&pk=y');
    await tester.tap(find.text('Подключить'));
    await tester.pumpAndSettle();
    expect(core.connects, 1);
    expect(core.others.map((p) => p.id), ['home']);
    expect(find.text(core.profile!.name), findsOneWidget);

    core.incoming.add('tigersb://add?url=https://three&pk=y');
    await tester.pumpAndSettle();
    await tester.tap(find.text('Добавить'));
    await tester.pumpAndSettle();
    expect(core.added.last, 'tigersb://add?url=https://three&pk=y');
    expect(core.others.length, 2);
    expect(core.connects, 2);
  });

  testWidgets('the menu opens apps outside the VPN: MAX is locked, others toggle', (tester) async {
    final core = FakeCore()
      ..profile = ProfileInfo(
        name: 'Дом',
        hasRU: true,
        fetched: DateTime.now(),
        exits: const [],
        shieldApps: const ['ru.oneme.app'],
      );
    await tester.pumpWidget(TigerApp(core: core));
    await tester.pumpAndSettle();
    await tester.tap(find.byType(PopupMenuButton<String>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Приложения мимо VPN'));
    await tester.pumpAndSettle();

    expect(find.text('MAX'), findsOneWidget);
    expect(find.text('ВСЕГДА НАПРЯМУЮ'), findsOneWidget);
    expect(find.byType(Switch), findsOneWidget);
    await tester.tap(find.byType(Switch));
    await tester.pumpAndSettle();
    expect(core.userApps, ['org.example.game']);
    expect(core.profile!.userApps, ['org.example.game']);

    await tester.enterText(find.byType(TextField), 'игр');
    await tester.pumpAndSettle();
    expect(find.text('MAX'), findsNothing);
    expect(find.text('Игра'), findsOneWidget);
  });

  testWidgets('the menu opens about: version, source link and every license', (tester) async {
    final core = FakeCore()..profile = ProfileInfo(name: 'Дом', hasRU: true, fetched: DateTime.now(), exits: const []);
    await tester.pumpWidget(TigerApp(core: core));
    await tester.pumpAndSettle();
    await tester.tap(find.byType(PopupMenuButton<String>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('О приложении'));
    await tester.pumpAndSettle();

    expect(find.text('Версия 0.1.0'), findsOneWidget);
    await tester.scrollUntilVisible(find.text('Исходный код на GitHub'), 200);
    await tester.ensureVisible(find.text('Исходный код на GitHub'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Исходный код на GitHub'));
    expect(core.opened, [sourceUrl]);

    await tester.scrollUntilVisible(find.text('Лицензии компонентов'), 200);
    expect(find.text('26.3.27 · MPL 2.0'), findsOneWidget);
    await tester.tap(find.text('Лицензии компонентов'));
    await tester.pumpAndSettle();
    expect(find.byType(LicensePage), findsOneWidget);
  });

  testWidgets('about switches the theme at once and saves it', (tester) async {
    addTearDown(() => lightTheme.value = false);
    final core = FakeCore()..profile = ProfileInfo(name: 'Дом', hasRU: true, fetched: DateTime.now(), exits: const []);
    await tester.pumpWidget(TigerApp(core: core));
    await tester.pumpAndSettle();
    await tester.tap(find.byType(PopupMenuButton<String>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('О приложении'));
    await tester.pumpAndSettle();

    Color? titleColor() => tester.widget<Text>(find.text('TigerSB')).style?.color;
    expect(titleColor(), const Color(0xFFF7F3EE));
    await tester.tap(find.text('Светлая'));
    await tester.pumpAndSettle();
    expect(core.light, isTrue);
    expect(Palette.light, isTrue);
    expect(Theme.of(tester.element(find.text('TigerSB'))).brightness, Brightness.light);
    // Widgets that read Palette are built again with the light colours.
    expect(titleColor(), Palette.white);
    expect(titleColor(), isNot(const Color(0xFFF7F3EE)));

    // The main screen under it kept its state and is light too.
    await tester.pageBack();
    await tester.pumpAndSettle();
    expect(find.text('Дом'), findsOneWidget);
    await tester.tap(find.byType(PopupMenuButton<String>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('О приложении'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Тёмная'));
    await tester.pumpAndSettle();
    expect(core.light, isFalse);
    expect(titleColor(), const Color(0xFFF7F3EE));
  });

  testWidgets('the menu checks for an update and installs it', (tester) async {
    final core = FakeCore()..profile = ProfileInfo(name: 'Дом', hasRU: true, fetched: DateTime.now(), exits: const []);
    await tester.pumpWidget(TigerApp(core: core));
    await tester.pumpAndSettle();
    expect(find.text('Обновить профиль'), findsNothing);

    Future<void> check() async {
      await tester.tap(find.byType(PopupMenuButton<String>));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Проверить обновление'));
      await tester.pumpAndSettle();
    }

    await check();
    expect(find.text('У вас последняя версия'), findsOneWidget);

    core.update = const UpdateInfo(version: '0.1.0', build: 140);
    await check();
    expect(find.textContaining('сборка 140'), findsOneWidget);
    await tester.tap(find.text('Позже'));
    await tester.pumpAndSettle();
    expect(core.installs, 0);

    await check();
    await tester.tap(find.text('Обновить'));
    await tester.pumpAndSettle();
    expect(core.installs, 1);
    expect(find.text('Скачиваю обновление'), findsNothing);

    // Not yet allowed to install apps: the setting opens and the user is told.
    core.installAllowed = false;
    await check();
    await tester.tap(find.text('Обновить'));
    await tester.pumpAndSettle();
    expect(find.textContaining('Разрешите TigerSB'), findsOneWidget);

    core.failures.add('Не хватает места для обновления');
    await tester.pumpAndSettle();
    expect(find.text('Не хватает места для обновления'), findsOneWidget);
  });

  test('the licenses list TigerSB and the Go libraries of the core', () async {
    registerLicenses();
    final packages = <String>{await for (final l in LicenseRegistry.licenses) ...l.packages};
    expect(packages, containsAll(['TigerSB', 'Go', 'github.com/xtls/xray-core', 'github.com/sagernet/sing-quic']));
  });

  testWidgets('every mode animates without errors', (tester) async {
    tester.platformDispatcher.clearAccessibilityFeaturesTestValue();
    final core = FakeCore()..profile = ProfileInfo(name: 'Дом', hasRU: true, fetched: DateTime.now(), exits: const []);
    await tester.pumpWidget(TigerApp(core: core));
    await tester.pump(const Duration(seconds: 1));
    await tester.tap(find.byKey(const Key('power')));
    for (final m in ['Базовая', 'Все', 'Умная']) {
      await tester.pump(const Duration(milliseconds: 500));
      await tester.tap(find.text(m));
      for (var i = 0; i < 30; i++) {
        await tester.pump(const Duration(milliseconds: 100));
      }
    }
    expect(tester.takeException(), isNull);
    // Leave a still screen behind.
    await tester.pumpWidget(const SizedBox());
  });
}
