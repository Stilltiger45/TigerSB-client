import 'dart:async';

import 'package:flutter/material.dart';

import 'core.dart';
import 'screens/home_screen.dart';
import 'screens/key_screen.dart';

class TigerApp extends StatelessWidget {
  const TigerApp({super.key, required this.core});

  final Core core;

  @override
  Widget build(BuildContext context) {
    const seed = Color(0xFFF08A24);
    return MaterialApp(
      title: 'TigerSB',
      debugShowCheckedModeBanner: false,
      theme: ThemeData(colorSchemeSeed: seed, brightness: Brightness.light),
      darkTheme: ThemeData(colorSchemeSeed: seed, brightness: Brightness.dark),
      home: Root(core: core),
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
    final replace = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Заменить ключ?'),
        content: const Text('Приложение получило новый ключ. Текущий профиль будет заменён.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Отмена')),
          FilledButton(onPressed: () => Navigator.pop(ctx, true), child: const Text('Заменить')),
        ],
      ),
    );
    if (replace != true || !mounted) return;
    await widget.core.disconnect();
    setState(() {
      _profile = null;
      _incomingKey = key;
    });
  }

  @override
  Widget build(BuildContext context) {
    if (_loading) return const Scaffold(body: Center(child: CircularProgressIndicator()));
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
      onForgotten: () => setState(() => _profile = null),
    );
  }
}
