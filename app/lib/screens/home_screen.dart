import 'dart:async';

import 'package:flutter/material.dart';

import '../core.dart';

/// Main screen: one big button, the mode and the profile.
class HomeScreen extends StatefulWidget {
  const HomeScreen({
    super.key,
    required this.core,
    required this.profile,
    required this.onProfile,
    required this.onForgotten,
  });

  final Core core;
  final ProfileInfo profile;
  final ValueChanged<ProfileInfo> onProfile;
  final VoidCallback onForgotten;

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  /// The profile is fetched again on start when it is older than this.
  static const refreshAfter = Duration(hours: 6);

  /// How often traffic, speed and ping are read; the first time a little
  /// after connecting, so the speed has something to average over.
  static const statsEvery = Duration(minutes: 5);
  static const statsFirst = Duration(seconds: 10);

  VpnStatus _status = const VpnStatus(Phase.disconnected);
  Mode _mode = Mode.auto;
  bool _refreshing = false;
  StreamSubscription<VpnStatus>? _sub;

  VpnStats? _stats;
  // Bytes per second since the previous reading.
  double? _upSpeed, _downSpeed;
  Timer? _statsTimer;

  @override
  void initState() {
    super.initState();
    _sub = widget.core.status().listen((s) {
      if (!mounted) return;
      final was = _status.phase;
      setState(() => _status = s);
      if (s.phase == Phase.error && s.error != null) _snack(s.error!);
      if (s.phase == Phase.connected && was != Phase.connected) _startStats();
      if (s.phase != Phase.connected) _stopStats();
    });
    widget.core.mode().then((m) {
      if (mounted) setState(() => _mode = m);
    });
    final fetched = widget.profile.fetched;
    if (fetched == null || DateTime.now().difference(fetched) > refreshAfter) {
      _refresh(quiet: true);
    }
  }

  @override
  void dispose() {
    _sub?.cancel();
    _statsTimer?.cancel();
    super.dispose();
  }

  void _startStats() {
    _statsTimer?.cancel();
    _statsTimer = Timer(statsFirst, () {
      _readStats();
      _statsTimer = Timer.periodic(statsEvery, (_) => _readStats());
    });
  }

  void _stopStats() {
    _statsTimer?.cancel();
    _statsTimer = null;
    if (_stats != null) {
      setState(() {
        _stats = null;
        _upSpeed = _downSpeed = null;
      });
    }
  }

  Future<void> _readStats() async {
    final VpnStats s;
    try {
      s = await widget.core.stats();
    } on CoreError {
      return;
    }
    if (!mounted || _status.phase != Phase.connected) return;
    final prev = _stats;
    // Since the previous reading, or since the start for the first one; a
    // reconnect restarts the counters.
    final base = prev != null && s.uptime > prev.uptime && s.down >= prev.down && s.up >= prev.up ? prev : null;
    final secs = s.uptime - (base?.uptime ?? 0);
    setState(() {
      _stats = s;
      if (secs > 0) {
        _upSpeed = (s.up - (base?.up ?? 0)) / secs;
        _downSpeed = (s.down - (base?.down ?? 0)) / secs;
      }
    });
  }

  void _snack(String text) {
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(content: Text(text)));
  }

  Future<void> _toggle() async {
    try {
      if (_status.phase == Phase.connected || _status.phase == Phase.connecting) {
        await widget.core.disconnect();
      } else if (!await widget.core.connect()) {
        _snack('Без разрешения Android VPN не включится');
      }
    } on CoreError catch (e) {
      _snack(e.message);
    }
  }

  Future<void> _setMode(Mode m) async {
    setState(() => _mode = m);
    await widget.core.setMode(m);
  }

  Future<void> _refresh({bool quiet = false}) async {
    if (_refreshing) return;
    setState(() => _refreshing = true);
    try {
      final p = await widget.core.refresh();
      widget.onProfile(p);
      if (!quiet) _snack('Профиль обновлён');
    } on CoreError catch (e) {
      // A failed background refresh keeps the old profile working.
      if (!quiet) _snack(e.message);
    } finally {
      if (mounted) setState(() => _refreshing = false);
    }
  }

  Future<void> _forget() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Сменить ключ?'),
        content: const Text('VPN отключится, профиль будет удалён с телефона.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Отмена')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Сменить')),
        ],
      ),
    );
    if (ok != true) return;
    try {
      await widget.core.disconnect();
      // The service stops asynchronously; give it a moment before deleting.
      for (var i = 0; i < 20 && _status.phase != Phase.disconnected && _status.phase != Phase.error; i++) {
        await Future<void>.delayed(const Duration(milliseconds: 100));
      }
      await widget.core.forget();
      widget.onForgotten();
    } on CoreError catch (e) {
      _snack(e.message);
    }
  }

  String get _modeText => switch (_mode) {
    Mode.auto => 'Каждый сайт — через самый быстрый сервер, который его открывает, иначе напрямую.',
    Mode.basic => 'Российские сайты (.ru, .рф, российские IP) — напрямую, остальное — через VPN.',
    Mode.all => 'Весь трафик — через VPN.',
  };

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;
    final on = _status.phase == Phase.connected;
    final busy = _status.phase == Phase.connecting;
    final label = switch (_status.phase) {
      Phase.connected => 'Подключено',
      Phase.connecting => 'Подключение…',
      Phase.error => 'Ошибка',
      Phase.disconnected => 'Отключено',
    };
    final fetched = widget.profile.fetched;

    return Scaffold(
      appBar: AppBar(
        title: Text(widget.profile.name),
        actions: [
          PopupMenuButton<String>(
            onSelected: (v) => v == 'refresh' ? _refresh() : _forget(),
            itemBuilder: (_) => const [
              PopupMenuItem(value: 'refresh', child: Text('Обновить профиль')),
              PopupMenuItem(value: 'forget', child: Text('Сменить ключ')),
            ],
          ),
        ],
      ),
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 480),
              child: Column(
                children: [
                  Semantics(
                    button: true,
                    label: on ? 'Отключить VPN' : 'Подключить VPN',
                    child: SizedBox.square(
                      dimension: 184,
                      child: FilledButton(
                        key: const Key('power'),
                        style: FilledButton.styleFrom(
                          shape: const CircleBorder(),
                          backgroundColor: on ? scheme.primary : scheme.surfaceContainerHighest,
                          foregroundColor: on ? scheme.onPrimary : scheme.onSurfaceVariant,
                        ),
                        onPressed: _toggle,
                        child: busy
                            ? const SizedBox.square(dimension: 56, child: CircularProgressIndicator())
                            : const Icon(Icons.power_settings_new, size: 80),
                      ),
                    ),
                  ),
                  const SizedBox(height: 16),
                  Text(label, style: theme.textTheme.titleLarge),
                  if (_status.phase == Phase.error && _status.error != null) ...[
                    const SizedBox(height: 8),
                    Text(
                      _status.error!,
                      textAlign: TextAlign.center,
                      style: TextStyle(color: scheme.error),
                    ),
                  ],
                  const SizedBox(height: 32),
                  SegmentedButton<Mode>(
                    showSelectedIcon: false,
                    segments: const [
                      ButtonSegment(value: Mode.auto, label: Text('Умная')),
                      ButtonSegment(value: Mode.basic, label: Text('Базовая')),
                      ButtonSegment(value: Mode.all, label: Text('Все')),
                    ],
                    selected: {_mode},
                    onSelectionChanged: (s) => _setMode(s.first),
                  ),
                  const SizedBox(height: 12),
                  Text(_modeText, textAlign: TextAlign.center, style: theme.textTheme.bodyMedium),
                  if (on) ...[
                    const SizedBox(height: 24),
                    _StatsCard(stats: _stats, upSpeed: _upSpeed, downSpeed: _downSpeed),
                  ],
                  const SizedBox(height: 32),
                  if (_refreshing)
                    const LinearProgressIndicator()
                  else if (fetched != null)
                    Text('Профиль обновлён ${_date(fetched)}', style: theme.textTheme.bodySmall),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  static String _date(DateTime t) {
    String two(int v) => v.toString().padLeft(2, '0');
    return '${two(t.day)}.${two(t.month)} в ${two(t.hour)}:${two(t.minute)}';
  }
}

/// Speed, traffic and ping of the running VPN, read every few minutes.
class _StatsCard extends StatelessWidget {
  const _StatsCard({required this.stats, required this.upSpeed, required this.downSpeed});

  final VpnStats? stats;
  final double? upSpeed, downSpeed;

  static const _dash = '—';

  static String _bytes(num b) {
    const units = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ'];
    var v = b.toDouble();
    var i = 0;
    while (v >= 1024 && i < units.length - 1) {
      v /= 1024;
      i++;
    }
    final n = i == 0 || v >= 100 ? v.toStringAsFixed(0) : v.toStringAsFixed(1);
    return '$n ${units[i]}';
  }

  static String _speed(double? bps) {
    if (bps == null) return _dash;
    // Network speeds are counted in bits.
    final bits = bps * 8;
    if (bits >= 1e6) return '${(bits / 1e6).toStringAsFixed(1)} Мбит/с';
    return '${(bits / 1e3).toStringAsFixed(0)} кбит/с';
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final s = stats;
    Widget cell(String title, String down, String up) => Expanded(
      child: Column(
        children: [
          Text(title, style: theme.textTheme.labelMedium),
          const SizedBox(height: 6),
          Text(down, textAlign: TextAlign.center, style: theme.textTheme.bodyMedium),
          Text(up, textAlign: TextAlign.center, style: theme.textTheme.bodyMedium),
        ],
      ),
    );
    final ping = s == null ? _dash : (s.ping < 0 ? 'нет ответа' : '${s.ping} мс');
    return Card(
      key: const Key('stats'),
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 16, horizontal: 8),
        child: Column(
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                cell('Скорость', '↓ ${_speed(downSpeed)}', '↑ ${_speed(upSpeed)}'),
                cell('Трафик', '↓ ${s == null ? _dash : _bytes(s.down)}', '↑ ${s == null ? _dash : _bytes(s.up)}'),
                cell('Пинг', ping, ''),
              ],
            ),
            const SizedBox(height: 8),
            Text('Обновляется раз в 5 минут', style: theme.textTheme.bodySmall),
          ],
        ),
      ),
    );
  }
}
