import 'dart:async';
import 'dart:convert';
import 'dart:ffi' show Abi;
import 'dart:io';

import 'package:flutter/services.dart';

import 'core.dart';

/// The app's version and build, set by the build:
/// --dart-define=VERSION=0.1.0 --dart-define=BUILD=123.
const appVersion = String.fromEnvironment('VERSION', defaultValue: 'dev');
const appBuild = int.fromEnvironment('BUILD');

/// The core on Windows: the TigerSB VPN service, which holds the VPN, over a
/// local connection (package desktop in the Go code). The app runs without
/// administrator rights; closing it leaves the VPN as it is.
class DesktopCore implements Core, Desktop {
  DesktopCore(List<String> args) {
    _pendingKey = args.where((a) => a.startsWith('tigersb:')).firstOrNull;
    _win.setMethodCallHandler((call) async {
      if (call.method == 'key') _keys.add(call.arguments as String);
    });
  }

  static const _win = MethodChannel('tigersb/windows');
  static const _release = 'https://github.com/Stilltiger45/TigerSB-client/releases/latest/download/';

  String? _pendingKey;
  final _keys = StreamController<String>.broadcast();
  final _statuses = StreamController<VpnStatus>.broadcast();
  final _progress = StreamController<double>.broadcast();
  final _failures = StreamController<String>.broadcast();
  VpnStatus _last = const VpnStatus(Phase.disconnected);

  Socket? _socket;
  Future<Socket>? _connecting;
  int _nextId = 0;
  final _pending = <int, Completer<Object?>>{};
  Timer? _retry;

  /// The service's directory, with the control file.
  static String get _serviceDir => '${Platform.environment['ProgramData'] ?? r'C:\ProgramData'}\\TigerSB';

  /// The app's own settings, for this user.
  static String get _settingsPath => '${Platform.environment['APPDATA'] ?? '.'}\\TigerSB\\settings.json';

  static const _notRunning = 'Служба TigerSB VPN не отвечает. Перезагрузите компьютер или установите TigerSB заново.';

  Future<Socket> _open() async {
    final Map<String, dynamic> control;
    try {
      control = jsonDecode(await File('$_serviceDir\\control.json').readAsString()) as Map<String, dynamic>;
    } catch (_) {
      throw CoreError(_notRunning);
    }
    final Socket s;
    try {
      s = await Socket.connect(
        InternetAddress.loopbackIPv4,
        control['port'] as int,
        timeout: const Duration(seconds: 3),
      );
    } on SocketException {
      throw CoreError(_notRunning);
    }
    s.setOption(SocketOption.tcpNoDelay, true);
    s.write('${jsonEncode({'token': control['token']})}\n');
    s
        .cast<List<int>>()
        .transform(utf8.decoder)
        .transform(const LineSplitter())
        .listen(_onLine, onDone: () => _lost(s), onError: (_) => _lost(s), cancelOnError: true);
    return s;
  }

  Future<Socket> _connection() {
    final s = _socket;
    if (s != null) return Future.value(s);
    return _connecting ??= _open()
        .then((s) {
          _socket = s;
          return s;
        })
        .whenComplete(() => _connecting = null);
  }

  void _onLine(String line) {
    final Map<String, dynamic> m;
    try {
      m = jsonDecode(line) as Map<String, dynamic>;
    } catch (_) {
      return;
    }
    if (m['ev'] == 'status') {
      _last = VpnStatus.fromMap(m);
      _statuses.add(_last);
      return;
    }
    final c = _pending.remove(m['id']);
    if (c == null) return;
    if (m['e'] case final String e) {
      c.completeError(CoreError(e));
    } else {
      c.complete(m['r']);
    }
  }

  /// The service went away, say for an update: the waiting calls fail and
  /// the status comes back once it is up again.
  void _lost(Socket s) {
    if (_socket != s) return;
    _socket = null;
    s.destroy();
    for (final c in _pending.values) {
      c.completeError(CoreError(_notRunning));
    }
    _pending.clear();
    _retry?.cancel();
    _retry = Timer(const Duration(seconds: 2), _keepUp);
  }

  /// Keeps a connection while anyone listens to the status.
  Future<void> _keepUp() async {
    if (!_statuses.hasListener) return;
    try {
      await _connection();
    } catch (_) {
      _retry?.cancel();
      _retry = Timer(const Duration(seconds: 3), _keepUp);
    }
  }

  Future<T?> _call<T>(String method, [Map<String, Object?>? args]) async {
    final s = await _connection();
    final id = ++_nextId;
    final c = Completer<Object?>();
    _pending[id] = c;
    s.write('${jsonEncode({'id': id, 'm': method, 'a': ?args})}\n');
    // Adding a key downloads the profile and the lists; give it time.
    final r = await c.future.timeout(
      const Duration(minutes: 2),
      onTimeout: () {
        _pending.remove(id);
        throw CoreError(_notRunning);
      },
    );
    return r as T?;
  }

  Future<Map<String, dynamic>> _settings() async {
    try {
      return jsonDecode(await File(_settingsPath).readAsString()) as Map<String, dynamic>;
    } catch (_) {
      return {};
    }
  }

  Future<void> _set(String key, Object value) async {
    final s = await _settings();
    s[key] = value;
    final f = File(_settingsPath);
    await f.parent.create(recursive: true);
    await f.writeAsString(jsonEncode(s));
  }

  @override
  Future<ProfileInfo?> current() async => ProfileInfo.parse(await _call<String>('current'));

  @override
  Future<List<ProfileInfo>> profiles() async => [
    for (final p in jsonDecode(await _call<String>('profiles') ?? '[]') as List)
      ProfileInfo.fromJson(p as Map<String, dynamic>),
  ];

  @override
  Future<ProfileInfo> addKey(String key) async => ProfileInfo.parse(await _call<String>('addKey', {'key': key}))!;

  @override
  Future<ProfileInfo> addKeyAside(String key) async =>
      ProfileInfo.parse(await _call<String>('addKeyAside', {'key': key}))!;

  @override
  Future<bool> otherPanel(String key) async => await _call<bool>('otherPanel', {'key': key}) ?? false;

  @override
  Future<ProfileInfo> refresh([String? id]) async => ProfileInfo.parse(await _call<String>('refresh', {'id': id}))!;

  @override
  Future<ProfileInfo> select(String id) async => ProfileInfo.parse(await _call<String>('select', {'id': id}))!;

  @override
  Future<ProfileInfo?> delete(String id) async => ProfileInfo.parse(await _call<String>('delete', {'id': id}));

  @override
  Future<Mode> mode() async {
    final s = await _call<String>('mode');
    for (final m in Mode.values) {
      if (m.name == s) return m;
    }
    await setMode(Mode.auto);
    return Mode.auto;
  }

  @override
  Future<void> setMode(Mode m) => _call('setMode', {'mode': m.name});

  @override
  Future<bool> lightTheme() async => (await _settings())['lightTheme'] == true;

  @override
  Future<void> setLightTheme(bool light) => _set('lightTheme', light);

  @override
  Future<String> style() async => (await _settings())['style'] as String? ?? 'tiger';

  @override
  Future<void> setStyle(String style) => _set('style', style);

  /// Running programs, by name ("game.exe"): the service sends the
  /// connections of the ones the user picks direct.
  @override
  Future<List<AppInfo>> apps() async => [
    for (final p in await _call<List<Object?>>('apps') ?? const [])
      if (p is Map) AppInfo(package: p['name'] as String, label: p['name'] as String),
  ];

  @override
  Future<void> setUserApps(List<String> packages) => _call('setUserApps', {'apps': packages});

  @override
  Future<void> setProtocolsOff(List<String> names) => _call('setProtocolsOff', {'names': names});

  /// Opens the journal in the text editor.
  @override
  Future<void> shareJournal() async {
    final text = await _call<String>('journal') ?? '';
    final f = File('${Directory.systemTemp.path}\\TigerSB-журнал.txt');
    await f.writeAsString(text);
    await Process.start('explorer.exe', [f.path]);
  }

  @override
  Future<void> copySecret(String text) async {
    await Clipboard.setData(ClipboardData(text: text));
    Timer(const Duration(minutes: 1), () async {
      final now = await Clipboard.getData(Clipboard.kTextPlain);
      if (now?.text == text) await Clipboard.setData(const ClipboardData(text: ''));
    });
  }

  @override
  Future<AboutInfo> about() async {
    final j = jsonDecode(await _call<String>('about') ?? '{}') as Map<String, dynamic>;
    return AboutInfo(version: appVersion, xray: j['xray'] as String? ?? '', go: j['go'] as String? ?? '');
  }

  @override
  Future<bool> openUrl(String url) async {
    if (!url.startsWith('https://') && !url.startsWith('http://')) return false;
    final r = await Process.run('rundll32.exe', ['url.dll,FileProtocolHandler', url]);
    return r.exitCode == 0;
  }

  /// The installer for this computer: ARM64 or x64.
  static String get installerName =>
      Abi.current() == Abi.windowsArm64 ? 'TigerSB-windows-arm64.exe' : 'TigerSB-windows-x64.exe';

  Future<HttpClientResponse> _get(String url) async {
    final client = HttpClient()..connectionTimeout = const Duration(seconds: 15);
    final res = await (await client.getUrl(Uri.parse(url))).close();
    if (res.statusCode != 200) {
      client.close();
      throw CoreError('Сервер обновлений ответил ${res.statusCode}');
    }
    return res;
  }

  @override
  Future<UpdateInfo?> checkUpdate() async {
    try {
      final res = await _get('${_release}version-windows.json');
      final j = jsonDecode(await res.transform(utf8.decoder).join()) as Map<String, dynamic>;
      final build = j['build'] as int? ?? 0;
      if (build <= appBuild) return null;
      return UpdateInfo(version: j['version'] as String? ?? '', build: build);
    } on CoreError {
      rethrow;
    } catch (e) {
      throw CoreError('Не удалось проверить обновление: $e');
    }
  }

  /// Downloads the installer and starts it; Windows asks the user to let it
  /// run as administrator. It stops the service and the app, updates them
  /// and starts them again.
  @override
  Future<bool> installUpdate() async {
    final File file;
    try {
      final res = await _get('$_release$installerName');
      file = File('${Directory.systemTemp.path}\\$installerName');
      final sink = file.openWrite();
      final total = res.contentLength;
      var done = 0;
      await for (final chunk in res) {
        sink.add(chunk);
        done += chunk.length;
        if (total > 0) _progress.add(done / total);
      }
      await sink.close();
    } on CoreError {
      rethrow;
    } catch (e) {
      throw CoreError('Не удалось скачать обновление: $e');
    }
    // Through the shell, which asks for the administrator's consent.
    await Process.start('cmd.exe', ['/c', 'start', '', file.path, '/SILENT', '/SUPPRESSMSGBOXES', '/NOCANCEL']);
    return true;
  }

  @override
  Stream<double> updateProgress() => _progress.stream;

  @override
  Stream<String> updateFailures() => _failures.stream;

  @override
  Future<VpnStats> stats() async =>
      VpnStats.fromJson(jsonDecode(await _call<String>('stats') ?? '{}') as Map<String, dynamic>);

  @override
  Future<bool> connect() async => await _call<bool>('connect') ?? false;

  @override
  Future<void> disconnect() => _call('disconnect');

  @override
  Stream<VpnStatus> status() {
    late StreamController<VpnStatus> out;
    StreamSubscription<VpnStatus>? sub;
    out = StreamController<VpnStatus>(
      onListen: () {
        sub = _statuses.stream.listen(out.add);
        out.add(_last);
        _keepUp().then((_) {
          if (_socket == null) out.add(VpnStatus(Phase.error, _notRunning));
        });
      },
      onCancel: () => sub?.cancel(),
    );
    return out.stream;
  }

  @override
  Future<String?> takeKey() async {
    final k = _pendingKey;
    _pendingKey = null;
    return k;
  }

  @override
  Stream<String> keys() => _keys.stream;

  /// Started with Windows: the VPN comes back on if the user left it on.
  /// Right after login the service may still be starting: it is asked
  /// again for a minute.
  Future<void> resume() async {
    final until = DateTime.now().add(const Duration(minutes: 1));
    bool? wasOn;
    while (wasOn == null) {
      try {
        wasOn = await _call<bool>('wasOn') ?? false;
      } on CoreError {
        if (DateTime.now().isAfter(until)) return;
        await Future<void>.delayed(const Duration(seconds: 2));
      }
    }
    if (!wasOn) return;
    try {
      await connect();
    } on CoreError {
      // The status shows why.
    }
  }

  static const _runKey = r'HKCU\Software\Microsoft\Windows\CurrentVersion\Run';

  @override
  Future<bool> launchAtLogin() async {
    final r = await Process.run('reg.exe', ['query', _runKey, '/v', 'TigerSB']);
    return r.exitCode == 0;
  }

  @override
  Future<void> setLaunchAtLogin(bool on) async {
    if (!on) {
      await Process.run('reg.exe', ['delete', _runKey, '/v', 'TigerSB', '/f']);
      return;
    }
    await _writeRun(await startAs());
  }

  @override
  Future<StartAs> startAs() async {
    final s = await _settings();
    // Before the choice of three there was only "minimized".
    return StartAs.values.asNameMap()[s['startAs']] ?? (s['startMinimized'] == true ? StartAs.minimized : StartAs.open);
  }

  @override
  Future<void> setStartAs(StartAs as) async {
    await _set('startAs', as.name);
    if (await launchAtLogin()) await _writeRun(as);
  }

  Future<void> _writeRun(StartAs as) async {
    final flag = switch (as) {
      StartAs.open => '',
      StartAs.minimized => ' --minimized',
      StartAs.closed => ' --closed',
    };
    final command = '"${Platform.resolvedExecutable}" --autostart$flag';
    final r = await Process.run('reg.exe', ['add', _runKey, '/v', 'TigerSB', '/t', 'REG_SZ', '/d', command, '/f']);
    if (r.exitCode != 0) throw CoreError('Не удалось включить запуск с системой: ${r.stderr}');
  }
}
