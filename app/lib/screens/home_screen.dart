import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../core.dart';
import '../ui/buttons.dart';
import '../ui/fx.dart';
import '../ui/glass.dart';
import '../ui/mode_switch.dart';
import '../ui/palette.dart';
import '../ui/power_button.dart';
import 'apps_screen.dart';

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

  Future<void> _copyKey() async {
    await Clipboard.setData(ClipboardData(text: widget.profile.key));
    _snack('Ключ подписки скопирован');
  }

  void _openApps() => Navigator.of(context).push(
    MaterialPageRoute<void>(
      builder: (_) => AppsScreen(core: widget.core, profile: widget.profile, onProfile: widget.onProfile),
    ),
  );

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

  /// The look of the screen: the routing mode's while connected.
  Fx get _fx => switch (_status.phase) {
    Phase.connected => switch (_mode) {
      Mode.auto => Fx.smart,
      Mode.basic => Fx.basic,
      Mode.all => Fx.all,
    },
    Phase.connecting => Fx.connecting,
    _ => Fx.idle,
  };

  String get _hint => switch (_status.phase) {
    Phase.connected => switch (_mode) {
      Mode.auto => 'УМНАЯ МАРШРУТИЗАЦИЯ',
      Mode.basic => 'РОССИЯ НАПРЯМУЮ',
      Mode.all => 'ВСЁ ЧЕРЕЗ VPN',
    },
    Phase.connecting => 'ИЩУ СЕРВЕР',
    _ => 'НАЖМИТЕ, ЧТОБЫ ВКЛЮЧИТЬ',
  };

  @override
  Widget build(BuildContext context) {
    final on = _status.phase == Phase.connected;
    final label = switch (_status.phase) {
      Phase.connected => 'Подключено',
      Phase.connecting => 'Подключение…',
      Phase.error => 'Ошибка',
      Phase.disconnected => 'Отключено',
    };
    final fetched = widget.profile.fetched;

    return Ambient(
      fx: _fx,
      child: Scaffold(
        backgroundColor: Colors.transparent,
        appBar: AppBar(
          titleSpacing: 20,
          title: Row(
            children: [
              Image.asset('assets/tiger.png', width: 30, height: 30),
              const SizedBox(width: 10),
              Flexible(child: Text(widget.profile.name, overflow: TextOverflow.ellipsis)),
            ],
          ),
          actions: [
            GlassIcon(
              child: PopupMenuButton<String>(
                icon: const Icon(Icons.more_horiz_rounded, color: Palette.white),
                offset: const Offset(0, 52),
                onSelected: (v) => switch (v) {
                  'refresh' => _refresh(),
                  'copy' => _copyKey(),
                  'apps' => _openApps(),
                  _ => _forget(),
                },
                itemBuilder: (_) => [
                  const PopupMenuItem(value: 'refresh', child: Text('Обновить профиль')),
                  if (widget.profile.key.isNotEmpty)
                    const PopupMenuItem(value: 'copy', child: Text('Скопировать ключ подписки')),
                  const PopupMenuItem(value: 'apps', child: Text('Приложения мимо VPN')),
                  const PopupMenuItem(value: 'forget', child: Text('Сменить ключ')),
                ],
              ),
            ),
            const SizedBox(width: 16),
          ],
        ),
        body: SafeArea(
          child: Center(
            child: SingleChildScrollView(
              padding: const EdgeInsets.fromLTRB(20, 8, 20, 24),
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 460),
                child: Column(
                  children: [
                    Semantics(
                      button: true,
                      label: on ? 'Отключить VPN' : 'Подключить VPN',
                      child: PowerButton(fx: _fx, onPressed: _toggle),
                    ),
                    AnimatedSwitcher(
                      duration: const Duration(milliseconds: 300),
                      child: Text(
                        label,
                        key: ValueKey(label),
                        style: const TextStyle(fontSize: 28, fontWeight: FontWeight.w800, letterSpacing: 0.2),
                      ),
                    ),
                    const SizedBox(height: 6),
                    AnimatedSwitcher(
                      duration: const Duration(milliseconds: 300),
                      child: Text(
                        _hint,
                        key: ValueKey(_hint),
                        style: capsStyle.copyWith(color: on ? Palette.orangeLight : Palette.faint),
                      ),
                    ),
                    if (_status.phase == Phase.error && _status.error != null) ...[
                      const SizedBox(height: 10),
                      Text(
                        _status.error!,
                        textAlign: TextAlign.center,
                        style: const TextStyle(color: Palette.error),
                      ),
                    ],
                    const SizedBox(height: 28),
                    ModeSwitch(mode: _mode, onChanged: _setMode),
                    const SizedBox(height: 14),
                    AnimatedSwitcher(
                      duration: const Duration(milliseconds: 300),
                      child: Text(
                        _modeText,
                        key: ValueKey(_mode),
                        textAlign: TextAlign.center,
                        style: const TextStyle(color: Palette.muted, fontSize: 14, height: 1.4),
                      ),
                    ),
                    AnimatedSize(
                      duration: const Duration(milliseconds: 350),
                      curve: Curves.easeOut,
                      child: on
                          ? Padding(
                              padding: const EdgeInsets.only(top: 22),
                              child: _StatsCard(stats: _stats, upSpeed: _upSpeed, downSpeed: _downSpeed),
                            )
                          : const SizedBox(width: double.infinity),
                    ),
                    const SizedBox(height: 26),
                    if (_refreshing)
                      const SizedBox(width: 120, child: LinearProgressIndicator(minHeight: 2))
                    else if (fetched != null)
                      Text(
                        'Профиль обновлён ${_date(fetched)}',
                        style: const TextStyle(color: Palette.faint, fontSize: 12),
                      ),
                  ],
                ),
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
    final s = stats;
    Widget cell(String title, String down, String up) => Expanded(
      child: Column(
        children: [
          Text(title, style: capsStyle),
          const SizedBox(height: 10),
          Text(
            down,
            textAlign: TextAlign.center,
            style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w600),
          ),
          const SizedBox(height: 2),
          Text(
            up,
            textAlign: TextAlign.center,
            style: const TextStyle(fontSize: 15, color: Palette.muted),
          ),
        ],
      ),
    );
    final ping = s == null ? _dash : (s.ping < 0 ? 'нет ответа' : '${s.ping} мс');
    return Glass(
      key: const Key('stats'),
      radius: 24,
      tint: Palette.orange,
      padding: const EdgeInsets.fromLTRB(8, 18, 8, 14),
      child: Column(
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              cell('СКОРОСТЬ', '↓ ${_speed(downSpeed)}', '↑ ${_speed(upSpeed)}'),
              cell('ТРАФИК', '↓ ${s == null ? _dash : _bytes(s.down)}', '↑ ${s == null ? _dash : _bytes(s.up)}'),
              cell('ПИНГ', ping, ''),
            ],
          ),
          const SizedBox(height: 12),
          const Text('Обновляется раз в 5 минут', style: TextStyle(color: Palette.faint, fontSize: 12)),
        ],
      ),
    );
  }
}
