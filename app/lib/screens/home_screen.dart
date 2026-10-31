import 'dart:async';

import 'package:flutter/material.dart';

import '../core.dart';
import '../ui/buttons.dart';
import '../ui/fx.dart';
import '../ui/glass.dart';
import '../ui/mode_switch.dart';
import '../ui/palette.dart';
import '../ui/power_button.dart';
import 'about_screen.dart';
import 'apps_screen.dart';
import 'protocols_screen.dart';

/// Main screen: one big button, the mode and the profile.
class HomeScreen extends StatefulWidget {
  const HomeScreen({
    super.key,
    required this.core,
    required this.profile,
    required this.onProfile,
    required this.onAdd,
    required this.onNoProfiles,
  });

  final Core core;

  /// The profile in use.
  final ProfileInfo profile;
  final ValueChanged<ProfileInfo> onProfile;

  /// Opens the screen that adds one more profile.
  final VoidCallback onAdd;

  /// The last profile was deleted.
  final VoidCallback onNoProfiles;

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
  bool _updating = false;
  StreamSubscription<VpnStatus>? _sub;
  StreamSubscription<String>? _updateFailures;

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
    _loadProfiles();
    _updateFailures = widget.core.updateFailures().listen((e) {
      if (mounted) _snack(e);
    });
  }

  @override
  void dispose() {
    _sub?.cancel();
    _updateFailures?.cancel();
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

  /// Looks for a newer version of the app and, if the user agrees,
  /// downloads it and hands it to the system installer.
  Future<void> _checkUpdate() async {
    if (_updating) return;
    _updating = true;
    try {
      _snack('Проверяю обновление…');
      final UpdateInfo u;
      try {
        final found = await widget.core.checkUpdate();
        if (!mounted) return;
        if (found == null) return _snack('У вас последняя версия');
        u = found;
      } on CoreError catch (e) {
        if (mounted) _snack(e.message);
        return;
      }
      ScaffoldMessenger.of(context).hideCurrentSnackBar();
      final ok = await showDialog<bool>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('Есть обновление'),
          content: Text(
            'Версия ${u.version}, сборка ${u.build}. Скачать и установить? '
            'Во время установки VPN отключится, профили и настройки сохранятся.',
          ),
          actions: [
            TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Позже')),
            FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Обновить')),
          ],
        ),
      );
      if (ok != true || !mounted) return;
      final progress = showDialog<void>(
        context: context,
        barrierDismissible: false,
        builder: (ctx) => AlertDialog(
          title: const Text('Скачиваю обновление'),
          content: StreamBuilder<double>(
            stream: widget.core.updateProgress(),
            builder: (_, snap) => LinearProgressIndicator(value: snap.data),
          ),
        ),
      );
      bool started;
      try {
        started = await widget.core.installUpdate();
      } on CoreError catch (e) {
        if (mounted) {
          Navigator.of(context).pop();
          _snack(e.message);
        }
        return;
      }
      if (!mounted) return;
      Navigator.of(context).pop();
      await progress;
      if (!started) _snack('Разрешите TigerSB устанавливать приложения и проверьте обновление ещё раз');
    } finally {
      _updating = false;
    }
  }

  void _openApps() => Navigator.of(context).push(
    MaterialPageRoute<void>(
      builder: (_) => AppsScreen(core: widget.core, profile: widget.profile, onProfile: widget.onProfile),
    ),
  );

  void _openProtocols() => Navigator.of(context).push(
    MaterialPageRoute<void>(
      builder: (_) => ProtocolsScreen(core: widget.core, profile: widget.profile, onProfile: widget.onProfile),
    ),
  );

  void _openAbout() =>
      Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => AboutScreen(core: widget.core)));

  /// Every saved profile, for the list under the profile's name.
  List<ProfileInfo> _profiles = const [];
  final _profileMenu = MenuController();

  Future<void> _loadProfiles() async {
    try {
      final list = await widget.core.profiles();
      if (mounted) setState(() => _profiles = list);
    } on CoreError {
      // The list shows what it had.
    }
  }

  Future<void> _select(ProfileInfo p) async {
    _profileMenu.close();
    if (p.id == widget.profile.id) return;
    try {
      widget.onProfile(await widget.core.select(p.id));
      _snack('Профиль «${p.name}»');
    } on CoreError catch (e) {
      _snack(e.message);
    }
  }

  Future<void> _refreshProfile(ProfileInfo p) async {
    _profileMenu.close();
    if (p.id == widget.profile.id) return _refresh();
    try {
      await widget.core.refresh(p.id);
      _snack('Профиль «${p.name}» обновлён');
    } on CoreError catch (e) {
      _snack(e.message);
    }
    _loadProfiles();
  }

  Future<void> _copyLink(ProfileInfo p) async {
    _profileMenu.close();
    await widget.core.copySecret(p.key);
    _snack('Ссылка на профиль скопирована. Через минуту она сотрётся из буфера обмена');
  }

  Future<void> _delete(ProfileInfo p) async {
    _profileMenu.close();
    final inUse = p.id == widget.profile.id;
    final on = _status.phase == Phase.connected || _status.phase == Phase.connecting;
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Удалить профиль «${p.name}»?'),
        content: Text(
          inUse && on ? 'VPN отключится, профиль будет удалён с телефона.' : 'Профиль будет удалён с телефона.',
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Отмена')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Удалить')),
        ],
      ),
    );
    if (ok != true) return;
    try {
      if (inUse && on) {
        await widget.core.disconnect();
        // The service stops asynchronously; give it a moment before deleting.
        for (var i = 0; i < 20 && _status.phase != Phase.disconnected && _status.phase != Phase.error; i++) {
          await Future<void>.delayed(const Duration(milliseconds: 100));
        }
      }
      final next = await widget.core.delete(p.id);
      if (next == null) return widget.onNoProfiles();
      if (inUse) widget.onProfile(next);
      _snack('Профиль «${p.name}» удалён');
    } on CoreError catch (e) {
      _snack(e.message);
    }
    _loadProfiles();
  }

  void _add() {
    _profileMenu.close();
    widget.onAdd();
  }

  /// The mode's description, in the words of the style.
  String get _modeText => switch (_mode) {
    Mode.auto => Palette.style.words.auto,
    Mode.basic => Palette.style.words.basic,
    Mode.all => Palette.style.words.all,
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
      Mode.auto => Palette.style.words.autoHint,
      Mode.basic => Palette.style.words.basicHint,
      Mode.all => Palette.style.words.allHint,
    },
    Phase.connecting => Palette.style.words.connecting,
    _ => Palette.style.words.idle,
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
          title: _ProfileButton(
            profile: widget.profile,
            profiles: _profiles,
            controller: _profileMenu,
            onOpen: _loadProfiles,
            onSelect: _select,
            onRefresh: _refreshProfile,
            onLink: _copyLink,
            onDelete: _delete,
            onAdd: _add,
          ),
          actions: [
            GlassIcon(
              child: PopupMenuButton<String>(
                icon: Icon(Icons.more_horiz_rounded, color: Palette.white),
                offset: const Offset(0, 52),
                onSelected: (v) => switch (v) {
                  'update' => _checkUpdate(),
                  'apps' => _openApps(),
                  'protocols' => _openProtocols(),
                  'journal' => widget.core.shareJournal(),
                  _ => _openAbout(),
                },
                itemBuilder: (_) => const [
                  PopupMenuItem(value: 'update', child: Text('Проверить обновление')),
                  PopupMenuItem(value: 'apps', child: Text('Приложения мимо VPN')),
                  PopupMenuItem(value: 'protocols', child: Text('Протоколы')),
                  PopupMenuItem(value: 'journal', child: Text('Отправить журнал')),
                  PopupMenuItem(value: 'about', child: Text('О приложении')),
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
                        style: TextStyle(color: Palette.error),
                      ),
                    ],
                    // A connected VPN may carry advice, such as which
                    // protocols get past a network that freezes connections.
                    if (_status.phase == Phase.connected && _status.error != null) ...[
                      const SizedBox(height: 10),
                      Text(
                        _status.error!,
                        key: const Key('advice'),
                        textAlign: TextAlign.center,
                        style: TextStyle(color: Palette.orangeLight),
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
                        style: TextStyle(color: Palette.muted, fontSize: 14, height: 1.4),
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
                      Text('Профиль обновлён ${_date(fetched)}', style: TextStyle(color: Palette.faint, fontSize: 12)),
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

/// The profile's name in the bar; a tap opens the list of profiles: each can
/// be picked, updated, its link copied or deleted, and one more added below.
class _ProfileButton extends StatelessWidget {
  const _ProfileButton({
    required this.profile,
    required this.profiles,
    required this.controller,
    required this.onOpen,
    required this.onSelect,
    required this.onRefresh,
    required this.onLink,
    required this.onDelete,
    required this.onAdd,
  });

  final ProfileInfo profile;
  final List<ProfileInfo> profiles;
  final MenuController controller;
  final VoidCallback onOpen;
  final ValueChanged<ProfileInfo> onSelect, onRefresh, onLink, onDelete;
  final VoidCallback onAdd;

  @override
  Widget build(BuildContext context) {
    // Until the list has loaded, it holds the profile in use.
    final list = profiles.any((p) => p.id == profile.id) ? profiles : [profile];
    return MenuAnchor(
      controller: controller,
      onOpen: onOpen,
      alignmentOffset: const Offset(0, 8),
      style: MenuStyle(
        backgroundColor: WidgetStatePropertyAll(Palette.light ? Palette.popup : const Color(0xF2140D08)),
        surfaceTintColor: const WidgetStatePropertyAll(Colors.transparent),
        padding: const WidgetStatePropertyAll(EdgeInsets.symmetric(vertical: 8)),
        shape: WidgetStatePropertyAll(
          RoundedRectangleBorder(
            borderRadius: BorderRadius.circular(20),
            side: BorderSide(color: Palette.white.withValues(alpha: 0.12)),
          ),
        ),
      ),
      menuChildren: [
        for (final p in list)
          _ProfileRow(
            profile: p,
            inUse: p.id == profile.id,
            onSelect: () => onSelect(p),
            onRefresh: () => onRefresh(p),
            onLink: p.key.isEmpty ? null : () => onLink(p),
            onDelete: () => onDelete(p),
          ),
        Divider(height: 12, color: Palette.white.withValues(alpha: 0.1)),
        MenuItemButton(
          key: const Key('addProfile'),
          leadingIcon: Icon(Icons.add_rounded, color: Palette.orange),
          onPressed: onAdd,
          child: const Text('Добавить профиль'),
        ),
      ],
      builder: (context, c, _) => InkWell(
        key: const Key('profiles'),
        borderRadius: BorderRadius.circular(14),
        onTap: () => c.isOpen ? c.close() : c.open(),
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 4),
          child: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              Image.asset(Palette.style.logo, width: 30, height: 30),
              const SizedBox(width: 10),
              Flexible(child: Text(profile.name, overflow: TextOverflow.ellipsis)),
              if (profile.signed) ...[
                const SizedBox(width: 6),
                Tooltip(
                  message: 'Профиль подписан панелью TigerSB',
                  child: Icon(Icons.verified_rounded, size: 18, color: Palette.orange),
                ),
              ],
              const SizedBox(width: 2),
              Icon(c.isOpen ? Icons.expand_less_rounded : Icons.expand_more_rounded, color: Palette.muted),
            ],
          ),
        ),
      ),
    );
  }
}

/// One profile in the list: a tap picks it; refresh, link and delete beside.
class _ProfileRow extends StatelessWidget {
  const _ProfileRow({
    required this.profile,
    required this.inUse,
    required this.onSelect,
    required this.onRefresh,
    required this.onLink,
    required this.onDelete,
  });

  final ProfileInfo profile;
  final bool inUse;
  final VoidCallback onSelect, onRefresh, onDelete;
  final VoidCallback? onLink;

  @override
  Widget build(BuildContext context) {
    Widget action(IconData icon, String tip, VoidCallback? onPressed, [Color? color]) => IconButton(
      icon: Icon(icon, size: 20),
      color: color ?? Palette.muted,
      tooltip: tip,
      visualDensity: VisualDensity.compact,
      onPressed: onPressed,
    );
    return ConstrainedBox(
      constraints: const BoxConstraints(minWidth: 280, maxWidth: 340),
      child: InkWell(
        key: Key('profile-${profile.id}'),
        onTap: onSelect,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(14, 2, 6, 2),
          child: Row(
            children: [
              Icon(
                inUse ? Icons.radio_button_checked_rounded : Icons.radio_button_unchecked_rounded,
                size: 18,
                color: inUse ? Palette.orange : Palette.faint,
              ),
              const SizedBox(width: 10),
              Expanded(
                child: Row(
                  children: [
                    Flexible(
                      child: Text(
                        profile.name,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(fontWeight: inUse ? FontWeight.w700 : FontWeight.w500),
                      ),
                    ),
                    if (profile.signed) ...[
                      const SizedBox(width: 4),
                      Icon(Icons.verified_rounded, size: 15, color: Palette.orange),
                    ],
                  ],
                ),
              ),
              action(Icons.refresh_rounded, 'Обновить', onRefresh),
              if (onLink != null) action(Icons.link_rounded, 'Ссылка', onLink),
              action(Icons.delete_outline_rounded, 'Удалить', onDelete, Palette.error),
            ],
          ),
        ),
      ),
    );
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
            style: TextStyle(fontSize: 15, color: Palette.muted),
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
          Text('Обновляется раз в 5 минут', style: TextStyle(color: Palette.faint, fontSize: 12)),
        ],
      ),
    );
  }
}
