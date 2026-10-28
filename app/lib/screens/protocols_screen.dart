import 'package:flutter/material.dart';

import '../core.dart';
import '../ui/fx.dart';
import '../ui/glass.dart';
import '../ui/palette.dart';

/// The profile's protocols, each with a switch. Networks differ: a protocol
/// that works poorly on the user's ones can be turned off, and the VPN goes
/// through the others. The choice stays on this device for every profile.
class ProtocolsScreen extends StatefulWidget {
  const ProtocolsScreen({super.key, required this.core, required this.profile, required this.onProfile});

  final Core core;
  final ProfileInfo profile;
  final ValueChanged<ProfileInfo> onProfile;

  @override
  State<ProtocolsScreen> createState() => _ProtocolsScreenState();
}

class _ProtocolsScreenState extends State<ProtocolsScreen> {
  // Off on this device, other profiles' protocols too.
  late final Set<String> _off = {...widget.profile.protocolsOff};

  Future<void> _toggle(String name, bool on) async {
    setState(() => on ? _off.remove(name) : _off.add(name));
    try {
      await widget.core.setProtocolsOff(_off.toList()..sort());
      final p = await widget.core.current();
      if (p != null) widget.onProfile(p);
    } on CoreError catch (e) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(e.message)));
    }
  }

  @override
  Widget build(BuildContext context) {
    final all = widget.profile.protocols;
    final onCount = all.where((p) => !_off.contains(p)).length;
    return Ambient(
      fx: Fx.idle,
      child: Scaffold(
        backgroundColor: Colors.transparent,
        appBar: AppBar(title: const Text('Протоколы')),
        body: SafeArea(
          top: false,
          child: ListView(
            padding: const EdgeInsets.only(bottom: 24),
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 4, 16, 12),
                child: Glass(
                  radius: 20,
                  padding: const EdgeInsets.all(16),
                  child: Row(
                    children: [
                      const Icon(Icons.tune_rounded, color: Palette.orange, size: 28),
                      const SizedBox(width: 14),
                      Expanded(
                        child: Text(
                          'В каждой сети протоколы работают по-разному. Выключите те, что у вас '
                          'работают плохо: VPN пойдёт через остальные.',
                          style: TextStyle(color: Palette.white.withValues(alpha: 0.85), fontSize: 13.5, height: 1.35),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
              if (all.isEmpty)
                Padding(
                  padding: const EdgeInsets.all(24),
                  child: Text(
                    'В профиле нет протоколов.',
                    textAlign: TextAlign.center,
                    style: TextStyle(color: Palette.muted),
                  ),
                ),
              for (final name in all) _row(name, last: onCount == 1 && !_off.contains(name)),
            ],
          ),
        ),
      ),
    );
  }

  /// A protocol's row; the last one left on cannot be turned off.
  Widget _row(String name, {required bool last}) {
    final on = !_off.contains(name);
    return ListTile(
      key: ValueKey(name),
      contentPadding: const EdgeInsets.symmetric(horizontal: 20),
      title: Text(name, maxLines: 1, overflow: TextOverflow.ellipsis),
      subtitle: last ? Text('ОСТАЁТСЯ ХОТЯ БЫ ОДИН', style: capsStyle.copyWith(fontSize: 10)) : null,
      trailing: Switch(
        value: on,
        activeThumbColor: Palette.ink,
        activeTrackColor: Palette.orange,
        inactiveThumbColor: Palette.muted,
        inactiveTrackColor: Palette.white.withValues(alpha: 0.06),
        onChanged: last ? null : (v) => _toggle(name, v),
      ),
      onTap: last ? null : () => _toggle(name, !on),
    );
  }
}
