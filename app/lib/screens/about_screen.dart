import 'package:flutter/material.dart';

import '../core.dart';
import '../ui/buttons.dart';
import '../ui/fx.dart';
import '../ui/glass.dart';
import '../ui/mode_switch.dart';
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

  /// Switches the theme at once and saves it for the next start.
  void _setTheme(bool light) {
    lightTheme.value = light;
    widget.core.setLightTheme(light).catchError((Object _) {});
  }

  /// Switches the style at once and saves it, with its launcher icon.
  void _setStyle(AppStyle style) {
    appStyle.value = style;
    widget.core.setStyle(style.name).catchError((Object _) {});
  }

  void _openLicenses() => showLicensePage(
    context: context,
    applicationName: 'TigerSB',
    applicationVersion: _about.version,
    applicationIcon: Padding(
      padding: const EdgeInsets.all(12),
      child: Image.asset(Palette.style.logo, width: 56, height: 56),
    ),
    applicationLegalese: '© 2026 авторы TigerSB. GNU GPL 3.0.',
  );

  @override
  Widget build(BuildContext context) {
    final body = TextStyle(color: Palette.white, fontSize: 14, height: 1.4);
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
              Image.asset(Palette.style.logo, width: 96, height: 96),
              const SizedBox(height: 12),
              Text(
                'TigerSB',
                textAlign: TextAlign.center,
                style: TextStyle(color: Palette.white, fontSize: 26, fontWeight: FontWeight.w700),
              ),
              const SizedBox(height: 4),
              Text(
                _about.version.isEmpty ? ' ' : 'Версия ${_about.version}',
                textAlign: TextAlign.center,
                style: TextStyle(color: Palette.muted, fontSize: 14),
              ),
              const SizedBox(height: 20),
              _Card(
                icon: Icons.contrast_rounded,
                title: 'Тема',
                child: Segments(
                  key: const Key('theme'),
                  labels: const {false: 'Тёмная', true: 'Светлая'},
                  value: Palette.light,
                  onChanged: _setTheme,
                  height: 40,
                ),
              ),
              const SizedBox(height: 12),
              _Card(
                icon: Icons.palette_outlined,
                title: 'Стиль',
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    // Two in a row.
                    for (var i = 0; i < AppStyle.values.length; i += 2) ...[
                      if (i > 0) const SizedBox(height: 10),
                      Row(
                        children: [
                          for (final style in AppStyle.values.skip(i).take(2)) ...[
                            if (style != AppStyle.values[i]) const SizedBox(width: 10),
                            Expanded(
                              child: _StyleTile(
                                style: style,
                                chosen: style == Palette.style,
                                onTap: () => _setStyle(style),
                              ),
                            ),
                          ],
                        ],
                      ),
                    ],
                    const SizedBox(height: 10),
                    Text(
                      'Меняет цвет, значок, описания режимов и эффекты. Значок на рабочем столе сменится через '
                      'несколько секунд; если он пропал, добавьте его снова из списка приложений.',
                      style: TextStyle(color: Palette.muted, fontSize: 12.5, height: 1.35),
                    ),
                  ],
                ),
              ),
              const SizedBox(height: 12),
              _Card(
                icon: Icons.bolt_rounded,
                title: 'Что это',
                child: Text(
                  'VPN-клиент для подписок TigerSB и других VPN. Сам выбирает быстрый сервер и обход блокировок '
                  'под вашу сеть, а банки, госуслуги и российские сервисы оставляет без VPN.',
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
                    Text(
                      'TigerSB — свободная программа под лицензией GNU GPL 3.0. Её можно использовать, изучать, '
                      'менять и распространять, если сохранять ту же лицензию и открывать исходный код. '
                      'Программа предоставляется без каких-либо гарантий.',
                      style: body,
                    ),
                    const SizedBox(height: 10),
                    InkWell(
                      onTap: _openSource,
                      borderRadius: BorderRadius.circular(8),
                      child: Padding(
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
                    const _Part('mieru', 'GPL 3.0'),
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
              style: TextStyle(color: Palette.white, fontSize: 16, fontWeight: FontWeight.w700),
            ),
          ],
        ),
        const SizedBox(height: 10),
        child,
      ],
    ),
  );
}

/// A style to pick: its logo and name in its own colour.
class _StyleTile extends StatelessWidget {
  const _StyleTile({required this.style, required this.chosen, required this.onTap});

  final AppStyle style;
  final bool chosen;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final color = Palette.light ? style.accentLightLight : style.accentLightDark;
    return Semantics(
      selected: chosen,
      button: true,
      child: InkWell(
        key: Key('style-${style.name}'),
        onTap: onTap,
        borderRadius: BorderRadius.circular(16),
        child: AnimatedContainer(
          duration: const Duration(milliseconds: 250),
          height: 64,
          padding: const EdgeInsets.symmetric(horizontal: 10),
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(16),
            color: style.accent.withValues(alpha: chosen ? 0.16 : 0.05),
            border: Border.all(color: chosen ? style.accent : Palette.white.withValues(alpha: 0.12), width: 1.4),
            boxShadow: chosen ? [BoxShadow(color: style.accent.withValues(alpha: 0.3), blurRadius: 16)] : null,
          ),
          child: Row(
            children: [
              Image.asset(style.logo, width: 34, height: 34),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  style.title,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                    color: chosen ? color : Palette.white,
                    fontSize: 13.5,
                    height: 1.15,
                    fontWeight: chosen ? FontWeight.w700 : FontWeight.w500,
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
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
          child: Text(name, style: TextStyle(color: Palette.white, fontSize: 14)),
        ),
        Text(detail, style: TextStyle(color: Palette.muted, fontSize: 13)),
      ],
    ),
  );
}
