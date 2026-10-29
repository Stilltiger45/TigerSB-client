import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'core.dart';
import 'ui/palette.dart';
import 'screens/home_screen.dart';
import 'screens/key_screen.dart';

class TigerApp extends StatefulWidget {
  const TigerApp({super.key, required this.core});

  final Core core;

  @override
  State<TigerApp> createState() => _TigerAppState();
}

class _TigerAppState extends State<TigerApp> {
  @override
  void initState() {
    super.initState();
    lightTheme.addListener(_themeChanged);
    appStyle.addListener(_themeChanged);
  }

  @override
  void dispose() {
    lightTheme.removeListener(_themeChanged);
    appStyle.removeListener(_themeChanged);
    super.dispose();
  }

  /// Widgets take their colours from Palette when they build, and many are
  /// const, so every widget is built again. Screens keep their state.
  void _themeChanged() {
    setState(() {});
    void rebuild(Element e) {
      e.markNeedsBuild();
      e.visitChildren(rebuild);
    }

    (context as Element).visitChildren(rebuild);
  }

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'TigerSB',
      debugShowCheckedModeBanner: false,
      theme: tigerTheme(),
      // Palette changes at once, so the theme does too.
      themeAnimationDuration: Duration.zero,
      builder: (context, child) => AnnotatedRegion<SystemUiOverlayStyle>(value: overlayStyle(), child: child!),
      home: Root(core: widget.core),
    );
  }
}

/// Shows the key screen until a key is added, then the main screen.
class Root extends StatefulWidget {
  const Root({super.key, required this.core});

  final Core core;

  @override
  State<Root> createState() => _RootState();
}

class _RootState extends State<Root> {
  bool _loading = true;
  ProfileInfo? _profile;
  String? _incomingKey;
  StreamSubscription<String>? _keys;

  @override
  void initState() {
    super.initState();
    _load();
    _keys = widget.core.keys().listen(_onKey);
  }

  @override
  void dispose() {
    _keys?.cancel();
    super.dispose();
  }

  Future<void> _load() async {
    ProfileInfo? p;
    try {
      p = await widget.core.current();
    } on CoreError {
      p = null;
    }
    final key = await widget.core.takeKey();
    if (!mounted) return;
    setState(() {
      _profile = p;
      _loading = false;
    });
    if (key != null) _onKey(key);
  }

  /// A tigersb:// link opened the app.
  Future<void> _onKey(String key) async {
    if (_profile == null) {
      setState(() => _incomingKey = key);
      return;
    }
    final add = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Добавить профиль?'),
        content: const Text(
          'Приложение получило новый ключ. Он станет ещё одним профилем, и VPN подключится через него.',
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Отмена')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Добавить')),
        ],
      ),
    );
    if (add == true && mounted) await _addProfile(key);
  }

  /// Opens the key screen for one more profile; it connects through it.
  Future<void> _addProfile([String? key]) async {
    final p = await Navigator.of(context).push<ProfileInfo>(
      MaterialPageRoute(
        builder: (ctx) =>
            KeyScreen(core: widget.core, initialKey: key, adding: true, onAdded: (p) => Navigator.pop(ctx, p)),
      ),
    );
    if (p != null && mounted) setState(() => _profile = p);
  }

  @override
  Widget build(BuildContext context) {
    if (_loading) {
      // The core answers at once, but show what is going on if it does not.
      return Scaffold(
        backgroundColor: Palette.black,
        body: Center(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Image.asset(Palette.style.logo, width: 96, height: 96),
              const SizedBox(height: 24),
              SizedBox(
                width: 24,
                height: 24,
                child: CircularProgressIndicator(strokeWidth: 2.5, color: Palette.orange),
              ),
            ],
          ),
        ),
      );
    }
    final p = _profile;
    if (p == null) {
      return KeyScreen(
        key: ValueKey(_incomingKey),
        core: widget.core,
        initialKey: _incomingKey,
        onAdded: (p) => setState(() {
          _profile = p;
          _incomingKey = null;
        }),
      );
    }
    return HomeScreen(
      core: widget.core,
      profile: p,
      onProfile: (p) => setState(() => _profile = p),
      onAdd: _addProfile,
      onNoProfiles: () => setState(() => _profile = null),
    );
  }
}
