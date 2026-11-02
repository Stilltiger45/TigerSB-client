import 'package:flutter/material.dart';

import '../core.dart';
import '../ui/fx.dart';
import '../ui/glass.dart';
import '../ui/palette.dart';

/// «Настройки запуска» on a computer: whether TigerSB starts with Windows,
/// and whether it then starts open, minimized or closed. The VPN itself
/// lives in a system service and comes back on after a restart when it was
/// on.
class StartupScreen extends StatefulWidget {
  const StartupScreen({super.key, required this.desktop});

  final Desktop desktop;

  @override
  State<StartupScreen> createState() => _StartupScreenState();
}

class _StartupScreenState extends State<StartupScreen> {
  bool? _atLogin;
  StartAs _as = StartAs.open;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final atLogin = await widget.desktop.launchAtLogin();
    final startAs = await widget.desktop.startAs();
    if (!mounted) return;
    setState(() {
      _atLogin = atLogin;
      _as = startAs;
    });
  }

  Future<void> _change(Future<void> Function() save, VoidCallback show) async {
    setState(show);
    try {
      await save();
    } on CoreError catch (e) {
      if (mounted) ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(e.message)));
      await _load();
    }
  }

  @override
  Widget build(BuildContext context) {
    final atLogin = _atLogin;
    return Ambient(
      fx: Fx.idle,
      child: Scaffold(
        backgroundColor: Colors.transparent,
        appBar: AppBar(title: const Text('Настройки запуска')),
        body: SafeArea(
          top: false,
          child: atLogin == null
              ? const Center(child: CircularProgressIndicator())
              : ListView(
                  padding: const EdgeInsets.only(bottom: 24),
                  children: [
                    Padding(
                      padding: const EdgeInsets.fromLTRB(16, 4, 16, 12),
                      child: Glass(
                        radius: 20,
                        padding: const EdgeInsets.all(16),
                        child: Row(
                          children: [
                            Icon(Icons.rocket_launch_rounded, color: Palette.orange, size: 28),
                            const SizedBox(width: 14),
                            Expanded(
                              child: Text(
                                'VPN работает и при закрытом окне. Если он был включён, после перезагрузки '
                                'он включится сам, когда TigerSB запустится вместе с Windows.',
                                style: TextStyle(
                                  color: Palette.white.withValues(alpha: 0.85),
                                  fontSize: 13.5,
                                  height: 1.35,
                                ),
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
                    _row(
                      'Запуск с системой',
                      'TigerSB открывается, когда вы входите в Windows',
                      atLogin,
                      (v) => _change(() => widget.desktop.setLaunchAtLogin(v), () => _atLogin = v),
                    ),
                    Padding(
                      padding: const EdgeInsets.fromLTRB(20, 16, 20, 4),
                      child: Text('ЗАПУСК В ВИДЕ', style: capsStyle.copyWith(color: atLogin ? null : Palette.faint)),
                    ),
                    for (final (choice, title, subtitle) in _choices) _choice(choice, title, subtitle, atLogin),
                  ],
                ),
        ),
      ),
    );
  }

  static const _choices = [
    (StartAs.open, 'Открытом', 'Окно TigerSB открывается'),
    (StartAs.minimized, 'Свёрнутом', 'Окно свёрнуто на панели задач'),
    (StartAs.closed, 'Закрытом', 'Окна нет: VPN включается сам, если был включён'),
  ];

  /// One of the ways to start, which only means something when the app
  /// starts with Windows.
  Widget _choice(StartAs choice, String title, String subtitle, bool enabled) {
    final on = _as == choice;
    return ListTile(
      key: ValueKey(choice),
      contentPadding: const EdgeInsets.symmetric(horizontal: 20),
      enabled: enabled,
      title: Text(title),
      subtitle: Text(subtitle, style: TextStyle(color: Palette.muted, fontSize: 12.5)),
      trailing: Icon(
        on ? Icons.radio_button_checked_rounded : Icons.radio_button_off_rounded,
        color: on && enabled ? Palette.orange : Palette.muted,
      ),
      selected: on,
      onTap: !enabled || on ? null : () => _change(() => widget.desktop.setStartAs(choice), () => _as = choice),
    );
  }

  Widget _row(String title, String subtitle, bool value, ValueChanged<bool>? onChanged) {
    return ListTile(
      contentPadding: const EdgeInsets.symmetric(horizontal: 20),
      enabled: onChanged != null,
      title: Text(title),
      subtitle: Text(subtitle, style: TextStyle(color: Palette.muted, fontSize: 12.5)),
      trailing: Switch(
        value: value,
        activeThumbColor: Palette.ink,
        activeTrackColor: Palette.orange,
        inactiveThumbColor: Palette.muted,
        inactiveTrackColor: Palette.white.withValues(alpha: 0.06),
        onChanged: onChanged,
      ),
      onTap: onChanged == null ? null : () => onChanged(!value),
    );
  }
}
