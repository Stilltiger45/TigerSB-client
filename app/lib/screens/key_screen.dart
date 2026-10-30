import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../core.dart';
import '../ui/buttons.dart';
import '../ui/fx.dart';
import '../ui/palette.dart';
import 'scan_screen.dart';

/// First screen: paste or scan the key, then the VPN starts at once. With
/// [adding], it adds one more profile and has a way back.
class KeyScreen extends StatefulWidget {
  const KeyScreen({super.key, required this.core, required this.onAdded, this.initialKey, this.adding = false});

  final Core core;
  final ValueChanged<ProfileInfo> onAdded;
  final String? initialKey;
  final bool adding;

  @override
  State<KeyScreen> createState() => _KeyScreenState();
}

class _KeyScreenState extends State<KeyScreen> {
  final _text = TextEditingController();
  bool _busy = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    final k = widget.initialKey;
    if (k != null) {
      _text.text = k;
      WidgetsBinding.instance.addPostFrameCallback((_) => _submit());
    }
  }

  @override
  void dispose() {
    _text.dispose();
    super.dispose();
  }

  Future<void> _paste() async {
    final data = await Clipboard.getData(Clipboard.kTextPlain);
    final s = data?.text?.trim();
    if (s == null || s.isEmpty) return;
    _text.text = s;
    await _submit();
  }

  Future<void> _scan() async {
    final s = await Navigator.of(context).push<String>(MaterialPageRoute(builder: (_) => const ScanScreen()));
    if (s == null || !mounted) return;
    _text.text = s;
    await _submit();
  }

  Future<void> _submit() async {
    final key = _text.text.trim();
    if (key.isEmpty || _busy) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final p = await widget.core.addKey(key);
      // "Вставил ключ и запустил": connect right away.
      await widget.core.connect();
      widget.onAdded(p);
    } on CoreError catch (e) {
      if (mounted) setState(() => _error = e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Ambient(
      fx: _busy ? Fx.connecting : Fx.idle,
      child: Scaffold(
        backgroundColor: Colors.transparent,
        appBar: widget.adding ? AppBar(title: const Text('Новый профиль')) : null,
        body: SafeArea(
          child: Center(
            child: SingleChildScrollView(
              padding: const EdgeInsets.all(24),
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 460),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    const _Logo(),
                    const SizedBox(height: 18),
                    const Text(
                      'TIGERSB',
                      textAlign: TextAlign.center,
                      style: TextStyle(fontSize: 40, fontWeight: FontWeight.w900, letterSpacing: 6, height: 1),
                    ),
                    const SizedBox(height: 8),
                    Text('V P N', textAlign: TextAlign.center, style: capsStyle.copyWith(letterSpacing: 6)),
                    const SizedBox(height: 22),
                    Text(
                      'Вставьте ключ или ссылку на подписку любого VPN, либо отсканируйте QR-код.',
                      textAlign: TextAlign.center,
                      style: TextStyle(color: Palette.muted, fontSize: 15, height: 1.4),
                    ),
                    const SizedBox(height: 26),
                    TextField(
                      controller: _text,
                      enabled: !_busy,
                      minLines: 2,
                      maxLines: 4,
                      style: const TextStyle(fontSize: 14),
                      decoration: InputDecoration(
                        labelText: 'Ключ',
                        hintText: 'tigersb://…, https://…, vless://… или конфиг AmneziaWG',
                        errorText: _error,
                        errorMaxLines: 4,
                      ),
                      onSubmitted: (_) => _submit(),
                    ),
                    const SizedBox(height: 18),
                    FireButton(
                      onPressed: _busy ? null : _submit,
                      busy: _busy,
                      icon: Icons.power_settings_new_rounded,
                      label: _busy ? 'Загружаю профиль…' : 'Подключить',
                    ),
                    const SizedBox(height: 12),
                    Row(
                      children: [
                        Expanded(
                          child: GlassButton(
                            onPressed: _busy ? null : _paste,
                            icon: Icons.content_paste_rounded,
                            label: 'Вставить',
                          ),
                        ),
                        const SizedBox(width: 12),
                        Expanded(
                          child: GlassButton(
                            onPressed: _busy ? null : _scan,
                            icon: Icons.qr_code_scanner_rounded,
                            label: 'QR-код',
                          ),
                        ),
                      ],
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
}

/// The tiger in a glass ring, glowing.
class _Logo extends StatelessWidget {
  const _Logo();

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Container(
        width: 132,
        height: 132,
        decoration: BoxDecoration(
          shape: BoxShape.circle,
          gradient: const RadialGradient(colors: [Color(0x1FFF7A1A), Color(0x05FFFFFF)]),
          border: Border.all(color: Palette.white.withValues(alpha: 0.14)),
          boxShadow: [BoxShadow(color: Palette.orange.withValues(alpha: 0.25), blurRadius: 50)],
        ),
        padding: const EdgeInsets.all(14),
        child: Image.asset(Palette.style.logo),
      ),
    );
  }
}
