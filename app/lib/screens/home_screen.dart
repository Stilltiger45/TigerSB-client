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

  VpnStatus _status = const VpnStatus(Phase.disconnected);
  Mode _mode = Mode.main;
  bool _refreshing = false;
  StreamSubscription<VpnStatus>? _sub;

  @override
  void initState() {
    super.initState();
    _sub = widget.core.status().listen((s) {
      if (!mounted) return;
      setState(() => _status = s);
      if (s.phase == Phase.error && s.error != null) _snack(s.error!);
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
    super.dispose();
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

  String get _modeText {
    if (_mode == Mode.all) return 'Всё — через сервер за границей. Банки и госуслуги — напрямую.';
    if (widget.profile.hasRU) {
      return 'Заблокированное — через сервер за границей, остальное — через сервер в России, '
          'банки и госуслуги — напрямую.';
    }
    return 'Заблокированное — через VPN, банки и госуслуги — напрямую.';
  }

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
                    Text(_status.error!, textAlign: TextAlign.center, style: TextStyle(color: scheme.error)),
                  ],
                  const SizedBox(height: 32),
                  SegmentedButton<Mode>(
                    segments: const [
                      ButtonSegment(value: Mode.main, label: Text('Умный'), icon: Icon(Icons.alt_route)),
                      ButtonSegment(value: Mode.all, label: Text('Всё за границу'), icon: Icon(Icons.public)),
                    ],
                    selected: {_mode},
                    onSelectionChanged: (s) => _setMode(s.first),
                  ),
                  const SizedBox(height: 12),
                  Text(_modeText, textAlign: TextAlign.center, style: theme.textTheme.bodyMedium),
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
