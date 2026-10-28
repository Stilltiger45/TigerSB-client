import 'dart:math' as math;
import 'dart:ui' show lerpDouble;

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/scheduler.dart';

import 'palette.dart';

/// What the screen shows: the VPN is off, connecting, or on in one of the
/// routing modes. Each has its own look of the background and the button.
enum Fx { idle, connecting, smart, basic, all }

/// The look of an Fx, which the screen blends between when it changes.
@immutable
class FxStyle {
  const FxStyle({
    required this.glow,
    required this.particles,
    required this.speed,
    required this.white,
    required this.split,
  });

  /// Brightness of the orange haze, 0..1.
  final double glow;

  /// Share of the sparks that are shown, 0..1.
  final double particles;

  /// How fast the sparks rise.
  final double speed;

  /// Share of white sparks among the orange ones, 0..1.
  final double white;

  /// The basic mode's two sides: white haze on the left (direct), orange on
  /// the right (VPN), 0..1.
  final double split;

  static const idle = FxStyle(glow: 0.3, particles: 0.35, speed: 0.45, white: 0.75, split: 0);
  static const connecting = FxStyle(glow: 0.6, particles: 0.55, speed: 1.1, white: 0.3, split: 0);
  static const smart = FxStyle(glow: 0.8, particles: 0.7, speed: 0.8, white: 0.08, split: 0);
  static const basic = FxStyle(glow: 0.7, particles: 0.75, speed: 0.7, white: 0.5, split: 1);
  static const all = FxStyle(glow: 1, particles: 1, speed: 1.5, white: 0, split: 0);

  static FxStyle of(Fx fx) => switch (fx) {
    Fx.idle => idle,
    Fx.connecting => connecting,
    Fx.smart => smart,
    Fx.basic => basic,
    Fx.all => all,
  };

  static FxStyle lerp(FxStyle a, FxStyle b, double t) => FxStyle(
    glow: lerpDouble(a.glow, b.glow, t)!,
    particles: lerpDouble(a.particles, b.particles, t)!,
    speed: lerpDouble(a.speed, b.speed, t)!,
    white: lerpDouble(a.white, b.white, t)!,
    split: lerpDouble(a.split, b.split, t)!,
  );
}

class FxStyleTween extends Tween<FxStyle> {
  FxStyleTween({super.begin, super.end});

  @override
  FxStyle lerp(double t) => FxStyle.lerp(begin!, end!, t);
}

/// A clock for endless effects: seconds since it started, standing still
/// when the system asks to remove animations (and in tests).
class FxClock extends StatefulWidget {
  const FxClock({super.key, required this.builder});

  final Widget Function(BuildContext context, ValueListenable<double> seconds) builder;

  @override
  State<FxClock> createState() => _FxClockState();
}

class _FxClockState extends State<FxClock> with SingleTickerProviderStateMixin {
  final _seconds = ValueNotifier<double>(0);
  late final Ticker _ticker = createTicker((e) => _seconds.value = e.inMicroseconds / 1e6);

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    final still = MediaQuery.maybeDisableAnimationsOf(context) ?? false;
    if (still && _ticker.isActive) _ticker.stop();
    if (!still && !_ticker.isActive) _ticker.start();
  }

  @override
  void dispose() {
    _ticker.dispose();
    _seconds.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => widget.builder(context, _seconds);
}

/// The background of every screen: black with a warm haze and rising sparks
/// whose look follows the Fx.
class Ambient extends StatelessWidget {
  const Ambient({super.key, required this.fx, required this.child});

  final Fx fx;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    return TweenAnimationBuilder<FxStyle>(
      tween: FxStyleTween(end: FxStyle.of(fx)),
      duration: const Duration(milliseconds: 900),
      curve: Curves.easeInOut,
      builder: (context, style, child) => FxClock(
        builder: (context, seconds) => Stack(
          fit: StackFit.expand,
          children: [
            RepaintBoundary(
              child: _AmbientLayer(style: style, seconds: seconds),
            ),
            child!,
          ],
        ),
      ),
      child: child,
    );
  }
}

/// Keeps how far the sparks have travelled, so a change of their speed
/// does not make them jump.
class _AmbientLayer extends StatefulWidget {
  const _AmbientLayer({required this.style, required this.seconds});

  final FxStyle style;
  final ValueListenable<double> seconds;

  @override
  State<_AmbientLayer> createState() => _AmbientLayerState();
}

class _AmbientLayerState extends State<_AmbientLayer> {
  final _travel = ValueNotifier<double>(0);
  double _last = 0;

  @override
  void initState() {
    super.initState();
    widget.seconds.addListener(_tick);
  }

  @override
  void didUpdateWidget(_AmbientLayer old) {
    super.didUpdateWidget(old);
    if (old.seconds != widget.seconds) {
      old.seconds.removeListener(_tick);
      widget.seconds.addListener(_tick);
    }
  }

  void _tick() {
    final now = widget.seconds.value;
    final dt = (now - _last).clamp(0.0, 0.1);
    _last = now;
    _travel.value += dt * widget.style.speed;
  }

  @override
  void dispose() {
    widget.seconds.removeListener(_tick);
    _travel.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => CustomPaint(
    painter: _AmbientPainter(style: widget.style, seconds: widget.seconds, travel: _travel),
  );
}

class _Spark {
  _Spark(math.Random r)
    : x = r.nextDouble(),
      phase = r.nextDouble(),
      speed = 0.6 + r.nextDouble() * 0.8,
      size = 0.8 + r.nextDouble() * 1.8,
      sway = 6 + r.nextDouble() * 18,
      tone = r.nextDouble();

  final double x, phase, speed, size, sway, tone;
}

final _sparks = List.generate(56, (_) => _Spark(_rand));
final _rand = math.Random(7);

class _AmbientPainter extends CustomPainter {
  _AmbientPainter({required this.style, required this.seconds, required this.travel}) : super(repaint: travel);

  final FxStyle style;
  final ValueListenable<double> seconds;
  final ValueListenable<double> travel;
  final bool light = Palette.light;

  @override
  void paint(Canvas canvas, Size size) {
    final t = seconds.value;
    final w = size.width, h = size.height;
    canvas.drawRect(Offset.zero & size, Paint()..color = Palette.black);

    void haze(Offset c, double r, Color color, double alpha) {
      if (alpha <= 0.01) return;
      canvas.drawCircle(
        c,
        r,
        Paint()
          ..shader = RadialGradient(
            colors: [
              color.withValues(alpha: alpha),
              color.withValues(alpha: 0),
            ],
          ).createShader(Rect.fromCircle(center: c, radius: r)),
      );
    }

    final g = style.glow;
    // Two slow orange clouds and, in the basic mode, a white one on the left.
    haze(
      Offset(w * (0.78 + 0.08 * math.sin(t * 0.13)), h * (0.22 + 0.06 * math.cos(t * 0.11))),
      w * 0.9,
      Palette.orange,
      0.22 * g,
    );
    haze(
      Offset(w * (0.2 + 0.08 * math.cos(t * 0.09)), h * (0.78 + 0.05 * math.sin(t * 0.12))),
      w * 0.8,
      Palette.orangeDeep,
      0.16 * g * (1 - 0.6 * style.split),
    );
    haze(
      Offset(w * (0.12 + 0.05 * math.sin(t * 0.1)), h * (0.4 + 0.08 * math.sin(t * 0.07))),
      w * 0.9,
      Palette.shine,
      0.15 * style.split,
    );
    // The button's light from the middle of the screen.
    haze(Offset(w / 2, h * 0.36), w * 0.55, Palette.orange, 0.12 * g);

    final shown = (style.particles * _sparks.length).round();
    for (var i = 0; i < shown; i++) {
      final s = _sparks[i];
      // Rise from below the screen to above it, then start again.
      final p = (s.phase + travel.value * 0.035 * s.speed) % 1.0;
      final y = h * (1.08 - p * 1.16);
      final x = w * s.x + math.sin(t * 0.6 * s.speed + s.phase * 6.28) * s.sway;
      final fade = math.sin(p * math.pi); // dim at both ends
      final twinkle = 0.65 + 0.35 * math.sin(t * 2.2 * s.speed + s.phase * 20);
      final white = s.tone < style.white;
      final color = white ? Palette.shine : (s.tone > 0.7 ? Palette.orangeLight : Palette.orange);
      final a = (0.85 * fade * twinkle).clamp(0.0, 1.0);
      final c = Offset(x, y);
      canvas.drawCircle(
        c,
        s.size * 3,
        Paint()
          ..color = color.withValues(alpha: a * 0.35)
          ..maskFilter = MaskFilter.blur(BlurStyle.normal, s.size * 1.6),
      );
      canvas.drawCircle(c, s.size, Paint()..color = color.withValues(alpha: a));
    }
  }

  @override
  bool shouldRepaint(_AmbientPainter old) => old.style != style || old.light != light;
}
