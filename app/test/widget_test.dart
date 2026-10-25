import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:tigersb/app.dart';
import 'package:tigersb/core.dart';

class FakeCore implements Core {
  ProfileInfo? profile;
  Mode currentMode = Mode.main;
  final statuses = StreamController<VpnStatus>.broadcast();
  final incoming = StreamController<String>.broadcast();
  final added = <String>[];
  int connects = 0;

  ProfileInfo _profile(String name) =>
      ProfileInfo(name: name, hasRU: true, fetched: DateTime.now(), exits: const []);

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

  @override
  Future<String?> takeKey() async => null;

  @override
  Stream<String> keys() => incoming.stream;
}

void main() {
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

    await tester.tap(find.text('Всё за границу'));
    await tester.pumpAndSettle();
    expect(core.currentMode, Mode.all);

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
}
