import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:tigersb/app.dart';
import 'package:tigersb/core.dart';

class FakeCore implements Core {
  ProfileInfo? profile;
  Mode currentMode = Mode.auto;
  final statuses = StreamController<VpnStatus>.broadcast();
  final incoming = StreamController<String>.broadcast();
  final added = <String>[];
  int connects = 0;

  ProfileInfo _profile(String name, {List<String> userApps = const []}) => ProfileInfo(
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
  Future<ProfileInfo> addKey(String key) async {
    if (!key.startsWith('tigersb://')) throw CoreError('неверный ключ');
    added.add(key);
    return profile = _profile('Дом');
  }

  @override
  Future<ProfileInfo> refresh() async => profile!;

  @override
  Future<void> forget() async => profile = null;

  @override
  Future<Mode> mode() async => currentMode;

  @override
  Future<void> setMode(Mode m) async => currentMode = m;

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

  testWidgets('the menu copies the subscription key', (tester) async {
    const key = 'tigersb://add?url=https%3A%2F%2Fpanel.example.org%2Fsub%2Fabc&pk=y';
    final core = FakeCore()
      ..profile = ProfileInfo(name: 'Дом', hasRU: true, fetched: DateTime.now(), exits: const [], key: key);
    String? copied;
    tester.binding.defaultBinaryMessenger.setMockMethodCallHandler(SystemChannels.platform, (call) async {
      if (call.method == 'Clipboard.setData') copied = (call.arguments as Map)['text'] as String;
      return null;
    });
    await tester.pumpWidget(TigerApp(core: core));
    await tester.pumpAndSettle();

    await tester.tap(find.byType(PopupMenuButton<String>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Скопировать ключ подписки'));
    await tester.pumpAndSettle();
    expect(copied, key);
    expect(find.text('Ключ подписки скопирован'), findsOneWidget);
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
