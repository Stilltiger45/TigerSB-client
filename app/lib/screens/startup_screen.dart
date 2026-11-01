import 'package:flutter/material.dart';

import '../core.dart';
import '../ui/fx.dart';
import '../ui/glass.dart';
import '../ui/palette.dart';

/// «Настройки запуска» on a computer: whether TigerSB starts with Windows,
/// and whether it then starts minimized. The VPN itself lives in a system
/// service and comes back on after a restart when it was on.
class StartupScreen extends StatefulWidget {
  const StartupScreen({super.key, required this.desktop});

  final Desktop desktop;

  @override
  State<StartupScreen> createState() => _StartupScreenState();
}

class _StartupScreenState extends State<StartupScreen> {
  bool? _atLogin;
  bool _minimized = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final atLogin = await widget.desktop.launchAtLogin();
    final minimized = await widget.desktop.startMinimized();
    if (!mounted) return;
    setState(() {
      _atLogin = atLogin;
      _minimized = minimized;
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
                    _row(
                      'Запуск в свёрнутом виде',
                      'При запуске с системой окно сразу свёрнуто',
                      _minimized,
                      atLogin ? (v) => _change(() => widget.desktop.setStartMinimized(v), () => _minimized = v) : null,
                    ),
                  ],
                ),
        ),
      ),
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
