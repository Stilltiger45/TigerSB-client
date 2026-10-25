import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../core.dart';
import 'scan_screen.dart';

/// First screen: paste or scan the key, then the VPN starts at once.
class KeyScreen extends StatefulWidget {
  const KeyScreen({super.key, required this.core, required this.onAdded, this.initialKey});

  final Core core;
  final ValueChanged<ProfileInfo> onAdded;
  final String? initialKey;

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
    final theme = Theme.of(context);
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 480),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Icon(Icons.shield_outlined, size: 72, color: theme.colorScheme.primary),
                  const SizedBox(height: 16),
                  Text('TigerSB', textAlign: TextAlign.center, style: theme.textTheme.headlineMedium),
                  const SizedBox(height: 8),
                  Text(
                    'Вставьте ключ, который вам дал администратор, или отсканируйте QR-код.',
                    textAlign: TextAlign.center,
                    style: theme.textTheme.bodyLarge,
                  ),
                  const SizedBox(height: 24),
                  TextField(
                    controller: _text,
                    enabled: !_busy,
                    minLines: 2,
                    maxLines: 4,
                    decoration: InputDecoration(
                      border: const OutlineInputBorder(),
                      labelText: 'Ключ',
                      hintText: 'tigersb://… или vless://…',
                      errorText: _error,
                      errorMaxLines: 4,
                    ),
                    onSubmitted: (_) => _submit(),
                  ),
                  const SizedBox(height: 16),
                  FilledButton.icon(
                    onPressed: _busy ? null : _submit,
                    icon: _busy
                        ? const SizedBox.square(dimension: 18, child: CircularProgressIndicator(strokeWidth: 2))
                        : const Icon(Icons.power_settings_new),
                    label: Text(_busy ? 'Загружаю профиль…' : 'Подключить'),
                  ),
                  const SizedBox(height: 8),
                  Row(
                    children: [
                      Expanded(
                        child: OutlinedButton.icon(
                          onPressed: _busy ? null : _paste,
                          icon: const Icon(Icons.content_paste),
                          label: const Text('Вставить'),
                        ),
                      ),
                      const SizedBox(width: 8),
                      Expanded(
                        child: OutlinedButton.icon(
                          onPressed: _busy ? null : _scan,
                          icon: const Icon(Icons.qr_code_scanner),
                          label: const Text('QR-код'),
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
    );
  }
}
