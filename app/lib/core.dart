import 'dart:async';
import 'dart:convert';

import 'package:flutter/services.dart';

/// The imported profile as the core describes it.
class ProfileInfo {
  ProfileInfo({
    required this.name,
    required this.hasRU,
    required this.fetched,
    required this.exits,
    this.hasLists = true,
    this.foreign = false,
    this.key = '',
  });

  final String name;
  final bool hasRU;

  /// The profile has lists of blocked sites, so the smart mode differs from
  /// sending everything through the VPN.
  final bool hasLists;

  /// Another VPN's subscription rather than a TigerSB panel.
  final bool foreign;
  final DateTime? fetched;
  final List<ExitInfo> exits;

  /// The key the profile was imported from, to copy and share.
  final String key;

  factory ProfileInfo.fromJson(Map<String, dynamic> j) => ProfileInfo(
    name: j['name'] as String? ?? '',
    hasRU: j['hasRU'] as bool? ?? false,
    hasLists: j['hasLists'] as bool? ?? true,
    foreign: j['foreign'] as bool? ?? false,
    key: j['key'] as String? ?? '',
    fetched: DateTime.tryParse(j['fetched'] as String? ?? '')?.toLocal(),
    exits: [
      for (final e in (j['exits'] as List? ?? const []))
        ExitInfo(
          id: e['id'] as String,
          role: e['role'] as String,
          transports: List<String>.from(e['transports'] as List? ?? const []),
        ),
    ],
  );

  static ProfileInfo? parse(String? s) =>
      s == null || s.isEmpty ? null : ProfileInfo.fromJson(jsonDecode(s) as Map<String, dynamic>);
}

class ExitInfo {
  ExitInfo({required this.id, required this.role, required this.transports});
  final String id;
  final String role;
  final List<String> transports;
}

enum Phase { disconnected, connecting, connected, error }

class VpnStatus {
  const VpnStatus(this.phase, [this.error]);
  final Phase phase;
  final String? error;

  static VpnStatus fromMap(Map<dynamic, dynamic> m) => VpnStatus(
    Phase.values.firstWhere((p) => p.name == m['phase'], orElse: () => Phase.disconnected),
    m['error'] as String?,
  );
}

/// Routing modes, as the core names them: auto checks every site through all
/// servers, basic sends Russian sites direct, all sends everything through
/// the VPN.
enum Mode { auto, basic, all }

/// What the core reports about the running VPN.
class VpnStats {
  const VpnStats({required this.up, required this.down, required this.ping, required this.uptime});

  /// Bytes sent and received through the servers since the VPN started.
  final int up;
  final int down;

  /// Milliseconds to the server in use, or -1 when it does not answer.
  final int ping;

  /// Seconds since the VPN started.
  final int uptime;

  factory VpnStats.fromJson(Map<String, dynamic> j) => VpnStats(
    up: (j['up'] as num?)?.toInt() ?? 0,
    down: (j['down'] as num?)?.toInt() ?? 0,
    ping: (j['ping'] as num?)?.toInt() ?? -1,
    uptime: (j['uptime'] as num?)?.toInt() ?? 0,
  );
}

/// Everything the screens need from the platform. [PlatformCore] talks to the
/// Android side; tests use a fake.
abstract class Core {
  Future<ProfileInfo?> current();
  Future<ProfileInfo> addKey(String key);
  Future<ProfileInfo> refresh();
  Future<void> forget();
  Future<Mode> mode();
  Future<void> setMode(Mode m);

  /// Traffic and ping of the running VPN; takes up to a few seconds.
  Future<VpnStats> stats();

  /// Asks for the VPN permission if needed and starts the VPN. Returns false
  /// when the user declined.
  Future<bool> connect();
  Future<void> disconnect();
  Stream<VpnStatus> status();

  /// A key the app was opened with (a tigersb:// link), once.
  Future<String?> takeKey();

  /// Keys that arrive while the app is open.
  Stream<String> keys();
}

class CoreError implements Exception {
  CoreError(this.message);
  final String message;
  @override
  String toString() => message;
}

class PlatformCore implements Core {
  PlatformCore() {
    _ch.setMethodCallHandler((call) async {
      if (call.method == 'key') _keys.add(call.arguments as String);
    });
  }

  static const _ch = MethodChannel('tigersb/core');
  static const _events = EventChannel('tigersb/status');
  final _keys = StreamController<String>.broadcast();

  Future<T?> _call<T>(String method, [Map<String, Object?>? args]) async {
    try {
      return await _ch.invokeMethod<T>(method, args);
    } on PlatformException catch (e) {
      throw CoreError(e.message ?? e.code);
    }
  }

  @override
  Future<ProfileInfo?> current() async => ProfileInfo.parse(await _call<String>('current'));

  @override
  Future<ProfileInfo> addKey(String key) async => ProfileInfo.parse(await _call<String>('addKey', {'key': key}))!;

  @override
  Future<ProfileInfo> refresh() async => ProfileInfo.parse(await _call<String>('refresh'))!;

  @override
  Future<void> forget() => _call('forget');

  @override
  Future<Mode> mode() async {
    final s = await _call<String>('mode');
    for (final m in Mode.values) {
      if (m.name == s) return m;
    }
    // A mode the app no longer offers (the old "main") becomes the default.
    await setMode(Mode.auto);
    return Mode.auto;
  }

  @override
  Future<VpnStats> stats() async =>
      VpnStats.fromJson(jsonDecode(await _call<String>('stats') ?? '{}') as Map<String, dynamic>);

  @override
  Future<void> setMode(Mode m) => _call('setMode', {'mode': m.name});

  @override
  Future<bool> connect() async => await _call<bool>('connect') ?? false;

  @override
  Future<void> disconnect() => _call('disconnect');

  @override
  Stream<VpnStatus> status() =>
      _events.receiveBroadcastStream().map((e) => VpnStatus.fromMap(e as Map<dynamic, dynamic>));

  @override
  Future<String?> takeKey() => _call<String>('takeKey');

  @override
  Stream<String> keys() => _keys.stream;
}
