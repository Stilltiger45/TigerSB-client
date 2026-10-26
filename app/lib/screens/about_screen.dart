import 'package:flutter/material.dart';

import '../core.dart';
import '../ui/buttons.dart';
import '../ui/fx.dart';
import '../ui/glass.dart';
import '../ui/palette.dart';

const sourceUrl = 'https://github.com/Stilltiger45/TigerSB-client';

/// What the app is, its version, its license and where its source is. The
/// licenses of everything inside are one tap away.
class AboutScreen extends StatefulWidget {
  const AboutScreen({super.key, required this.core});

  final Core core;

  @override
  State<AboutScreen> createState() => _AboutScreenState();
}

class _AboutScreenState extends State<AboutScreen> {
  AboutInfo _about = const AboutInfo();

  @override
  void initState() {
    super.initState();
    widget.core.about().then((a) {
      if (mounted) setState(() => _about = a);
    }, onError: (Object _) {});
  }

  Future<void> _openSource() async {
    if (await widget.core.openUrl(sourceUrl) || !mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(const SnackBar(content: Text('Нет браузера, чтобы открыть ссылку')));
  }

  void _openLicenses() => showLicensePage(
    context: context,
    applicationName: 'TigerSB',
    applicationVersion: _about.version,
    applicationIcon: Padding(
      padding: const EdgeInsets.all(12),
      child: Image.asset('assets/tiger.png', width: 56, height: 56),
    ),
    applicationLegalese: '© 2026 авторы TigerSB. GNU GPL 3.0.',
  );

  @override
  Widget build(BuildContext context) {
    const body = TextStyle(color: Palette.white, fontSize: 14, height: 1.4);
    return Ambient(
      fx: Fx.idle,
      child: Scaffold(
        backgroundColor: Colors.transparent,
        appBar: AppBar(title: const Text('О приложении')),
        body: SafeArea(
          top: false,
          child: ListView(
            padding: const EdgeInsets.fromLTRB(16, 8, 16, 24),
            children: [
              Image.asset('assets/tiger.png', width: 96, height: 96),
              const SizedBox(height: 12),
              const Text(
                'TigerSB',
                textAlign: TextAlign.center,
                style: TextStyle(color: Palette.white, fontSize: 26, fontWeight: FontWeight.w700),
              ),
              const SizedBox(height: 4),
              Text(
                _about.version.isEmpty ? ' ' : 'Версия ${_about.version}',
                textAlign: TextAlign.center,
                style: const TextStyle(color: Palette.muted, fontSize: 14),
              ),
              const SizedBox(height: 20),
              const _Card(
                icon: Icons.bolt_rounded,
                title: 'Что это',
                child: Text(
                  'VPN-клиент для подписок TigerSB и других VPN. Сам выбирает быстрый сервер и путь для каждого '
                  'сайта, а банки, госуслуги и российские сервисы оставляет без VPN.',
                  style: body,
                ),
              ),
              const SizedBox(height: 12),
              _Card(
                icon: Icons.gavel_rounded,
                title: 'Лицензия',
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text(
                      'TigerSB — свободная программа под лицензией GNU GPL 3.0. Её можно использовать, изучать, '
                      'менять и распространять, если сохранять ту же лицензию и открывать исходный код. '
                      'Программа предоставляется без каких-либо гарантий.',
                      style: body,
                    ),
                    const SizedBox(height: 10),
                    InkWell(
                      onTap: _openSource,
                      borderRadius: BorderRadius.circular(8),
                      child: const Padding(
                        padding: EdgeInsets.symmetric(vertical: 6),
                        child: Row(
                          children: [
                            Icon(Icons.code_rounded, size: 18, color: Palette.orangeLight),
                            SizedBox(width: 8),
                            Expanded(
                              child: Text(
                                'Исходный код на GitHub',
                                style: TextStyle(color: Palette.orangeLight, fontSize: 14, fontWeight: FontWeight.w600),
                              ),
                            ),
                            Icon(Icons.open_in_new_rounded, size: 16, color: Palette.faint),
                          ],
                        ),
                      ),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 12),
              _Card(
                icon: Icons.memory_rounded,
                title: 'Внутри',
                child: Column(
                  children: [
                    _Part('Xray-core', _about.xray.isEmpty ? 'MPL 2.0' : '${_about.xray} · MPL 2.0'),
                    const _Part('sing-quic (TUIC)', 'GPL 3.0'),
                    const _Part('gVisor', 'Apache 2.0'),
                    _Part('Go', _about.go.isEmpty ? 'BSD' : '${_about.go.replaceFirst('go', '')} · BSD'),
                    const _Part('Flutter', 'BSD'),
                  ],
                ),
              ),
              const SizedBox(height: 20),
              GlassButton(label: 'Лицензии компонентов', icon: Icons.description_outlined, onPressed: _openLicenses),
            ],
          ),
        ),
      ),
    );
  }
}

class _Card extends StatelessWidget {
  const _Card({required this.icon, required this.title, required this.child});

  final IconData icon;
  final String title;
  final Widget child;

  @override
  Widget build(BuildContext context) => Glass(
    radius: 20,
    padding: const EdgeInsets.all(16),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Icon(icon, color: Palette.orange, size: 20),
            const SizedBox(width: 8),
            Text(
              title,
              style: const TextStyle(color: Palette.white, fontSize: 16, fontWeight: FontWeight.w700),
            ),
          ],
        ),
        const SizedBox(height: 10),
        child,
      ],
    ),
  );
}

class _Part extends StatelessWidget {
  const _Part(this.name, this.detail);

  final String name;
  final String detail;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 4),
    child: Row(
      children: [
        Expanded(
          child: Text(name, style: const TextStyle(color: Palette.white, fontSize: 14)),
        ),
        Text(detail, style: const TextStyle(color: Palette.muted, fontSize: 13)),
      ],
    ),
  );
}
