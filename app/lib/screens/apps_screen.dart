import 'package:flutter/material.dart';

import '../core.dart';
import '../ui/fx.dart';
import '../ui/glass.dart';
import '../ui/palette.dart';

/// Apps left out of the VPN. They work as if there were no VPN, so they cannot
/// study the tunnel or see the server: MAX, VK, Yandex, banks and government
/// apps always, plus any the user picks.
class AppsScreen extends StatefulWidget {
  const AppsScreen({super.key, required this.core, required this.profile, required this.onProfile});

  final Core core;
  final ProfileInfo profile;
  final ValueChanged<ProfileInfo> onProfile;

  @override
  State<AppsScreen> createState() => _AppsScreenState();
}

class _AppsScreenState extends State<AppsScreen> {
  List<AppInfo>? _apps;
  String? _error;
  late final Set<String> _user = {...widget.profile.userApps};
  late final Set<String> _shield = {...widget.profile.shieldApps};
  String _query = '';

  @override
  void initState() {
    super.initState();
    widget.core.apps().then(
      (apps) {
        // Apps already outside the VPN on top; the order stays put while the
        // user toggles.
        int rank(AppInfo a) => _shield.contains(a.package) ? 0 : (_user.contains(a.package) ? 1 : 2);
        final sorted = [...apps]..sort((a, b) => rank(a) - rank(b));
        if (mounted) setState(() => _apps = sorted);
      },
      onError: (Object e) {
        if (mounted) setState(() => _error = e.toString());
      },
    );
  }

  Future<void> _toggle(String package, bool out) async {
    setState(() => out ? _user.add(package) : _user.remove(package));
    try {
      await widget.core.setUserApps(_user.toList()..sort());
      final p = await widget.core.current();
      if (p != null) widget.onProfile(p);
    } on CoreError catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(e.message)));
    }
  }

  @override
  Widget build(BuildContext context) {
    final q = _query.toLowerCase();
    final apps = _apps?.where((a) => q.isEmpty || a.label.toLowerCase().contains(q) || a.package.contains(q)).toList();
    return Ambient(
      fx: Fx.idle,
      child: Scaffold(
        backgroundColor: Colors.transparent,
        appBar: AppBar(title: const Text('Приложения мимо VPN')),
        body: SafeArea(
          top: false,
          child: Column(
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 4, 16, 12),
                child: Glass(
                  radius: 20,
                  padding: const EdgeInsets.all(16),
                  child: Row(
                    children: [
                      const Icon(Icons.shield_moon_rounded, color: Palette.orange, size: 28),
                      const SizedBox(width: 14),
                      Expanded(
                        child: Text(
                          'Эти приложения работают напрямую, как без VPN, и не видят туннель. '
                          'MAX, ВК, Яндекс, банки и госприложения — всегда.',
                          style: TextStyle(color: Palette.white.withValues(alpha: 0.85), fontSize: 13.5, height: 1.35),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
                child: TextField(
                  decoration: const InputDecoration(
                    prefixIcon: Icon(Icons.search_rounded),
                    hintText: 'Найти приложение',
                  ),
                  onChanged: (v) => setState(() => _query = v.trim()),
                ),
              ),
              Expanded(
                child: switch ((apps, _error)) {
                  (_, final String e) => Center(
                    child: Text(e, style: const TextStyle(color: Palette.error)),
                  ),
                  (null, _) => const Center(child: CircularProgressIndicator(color: Palette.orange)),
                  (final List<AppInfo> list, _) => ListView.builder(
                    padding: const EdgeInsets.only(bottom: 24),
                    itemCount: list.length,
                    itemBuilder: (_, i) => _row(list[i]),
                  ),
                },
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _row(AppInfo a) {
    final always = _shield.contains(a.package);
    final icon = a.icon;
    return ListTile(
      key: ValueKey(a.package),
      contentPadding: const EdgeInsets.symmetric(horizontal: 20),
      leading: ClipRRect(
        borderRadius: BorderRadius.circular(10),
        child: icon == null
            ? const SizedBox(width: 40, height: 40, child: Icon(Icons.android_rounded, color: Palette.faint))
            : Image.memory(icon, width: 40, height: 40, gaplessPlayback: true),
      ),
      title: Text(a.label, maxLines: 1, overflow: TextOverflow.ellipsis),
      subtitle: always ? Text('ВСЕГДА НАПРЯМУЮ', style: capsStyle.copyWith(fontSize: 10)) : null,
      trailing: always
          ? const Icon(Icons.lock_rounded, color: Palette.orange, size: 20)
          : Switch(
              value: _user.contains(a.package),
              activeThumbColor: Palette.ink,
              activeTrackColor: Palette.orange,
              inactiveThumbColor: Palette.muted,
              inactiveTrackColor: Colors.white.withValues(alpha: 0.06),
              onChanged: (v) => _toggle(a.package, v),
            ),
      onTap: always ? null : () => _toggle(a.package, !_user.contains(a.package)),
    );
  }
}
