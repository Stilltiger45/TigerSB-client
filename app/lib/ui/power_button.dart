import 'dart:math' as math;
import 'dart:ui';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import 'fx.dart';
import 'palette.dart';

/// The big round button. Off it is clear glass; on it glows in the style's
/// colour, and the ring around it moves in the way of the routing mode, its
/// own for each style. In the standard one:
/// - smart: three sparks circle the button, looking for the best way;
/// - basic: a white and an orange arc turn against each other, direct and VPN;
/// - all: waves run out of the button, everything goes through the tunnel.
class PowerButton extends StatelessWidget {
  const PowerButton({super.key, required this.fx, required this.onPressed, this.size = 200});

  final Fx fx;
  final VoidCallback onPressed;
  final double size;

  @override
  Widget build(BuildContext context) {
    final on = fx != Fx.idle && fx != Fx.connecting;
    final ring = size * 0.2;
    return SizedBox.square(
      dimension: size + ring * 2,
      child: Stack(
        alignment: Alignment.center,
        children: [
          // The glow behind the button.
          AnimatedContainer(
            duration: const Duration(milliseconds: 700),
            width: size,
            height: size,
            decoration: BoxDecoration(
              shape: BoxShape.circle,
              boxShadow: [
                BoxShadow(
                  color: Palette.orange.withValues(alpha: on ? 0.55 : (fx == Fx.connecting ? 0.25 : 0.0)),
                  blurRadius: size * 0.45,
                  spreadRadius: size * 0.02,
                ),
              ],
            ),
          ),
          Positioned.fill(
            child: IgnorePointer(
              child: FxClock(
                builder: (context, seconds) => CustomPaint(
                  painter: _RingPainter(fx: fx, seconds: seconds, radius: size / 2),
                ),
              ),
            ),
          ),
          _Disc(on: on, busy: fx == Fx.connecting, size: size, onPressed: onPressed),
        ],
      ),
    );
  }
}

class _Disc extends StatelessWidget {
  const _Disc({required this.on, required this.busy, required this.size, required this.onPressed});

  final bool on, busy;
  final double size;
  final VoidCallback onPressed;

  @override
  Widget build(BuildContext context) {
    return SizedBox.square(
      dimension: size,
      child: ClipOval(
        child: BackdropFilter(
          filter: ImageFilter.blur(sigmaX: 14, sigmaY: 14),
          child: Material(
            type: MaterialType.transparency,
            child: InkWell(
              key: const Key('power'),
              customBorder: const CircleBorder(),
              splashColor: Palette.white.withValues(alpha: 0.12),
              onTap: onPressed,
              child: AnimatedContainer(
                duration: const Duration(milliseconds: 600),
                curve: Curves.easeOut,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  gradient: on
                      ? Palette.fire
                      : LinearGradient(
                          begin: Alignment.topLeft,
                          end: Alignment.bottomRight,
                          colors: Palette.light
                              ? const [Color(0xD9FFFFFF), Color(0x99FFFFFF)]
                              : const [Color(0x26FFFFFF), Color(0x0AFFFFFF)],
                        ),
                  border: Border.all(
                    color: (on || !Palette.light ? Colors.white : Palette.white).withValues(alpha: on ? 0.35 : 0.22),
                  ),
                ),
                child: Stack(
                  alignment: Alignment.center,
                  children: [
                    // Liquid glass: light caught along the upper rim and a
                    // softer glint at the bottom.
                    Positioned.fill(
                      child: CustomPaint(painter: _GlintPainter(on: on)),
                    ),
                    AnimatedSwitcher(
                      duration: const Duration(milliseconds: 300),
                      child: busy
                          ? SizedBox.square(
                              key: const ValueKey('busy'),
                              dimension: size * 0.3,
                              child: CircularProgressIndicator(strokeWidth: 3, color: Palette.orangeLight),
                            )
                          : Icon(
                              Icons.power_settings_new_rounded,
                              key: ValueKey(on),
                              size: size * 0.4,
                              color: on ? Palette.ink : Palette.white,
                            ),
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

class _GlintPainter extends CustomPainter {
  _GlintPainter({required this.on});

  final bool on;

  @override
  void paint(Canvas canvas, Size size) {
    final r = size.width / 2;
    final c = size.center(Offset.zero);
    final rim = Rect.fromCircle(center: c, radius: r - 5);
    canvas.drawArc(
      rim,
      math.pi * 1.08,
      math.pi * 0.84,
      false,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = r * 0.07
        ..strokeCap = StrokeCap.round
        ..maskFilter = MaskFilter.blur(BlurStyle.normal, r * 0.03)
        ..shader = LinearGradient(
          begin: Alignment.centerLeft,
          end: Alignment.centerRight,
          colors: [
            Colors.white.withValues(alpha: 0),
            Colors.white.withValues(alpha: on ? 0.55 : 0.3),
            Colors.white.withValues(alpha: 0),
          ],
        ).createShader(rim),
    );
    canvas.drawArc(
      rim,
      math.pi * 0.25,
      math.pi * 0.5,
      false,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = r * 0.04
        ..strokeCap = StrokeCap.round
        ..maskFilter = MaskFilter.blur(BlurStyle.normal, r * 0.03)
        ..color = Colors.white.withValues(alpha: on ? 0.22 : 0.1),
    );
  }

  @override
  bool shouldRepaint(_GlintPainter old) => old.on != on;
}

class _RingPainter extends CustomPainter {
  _RingPainter({required this.fx, required this.seconds, required this.radius}) : super(repaint: seconds);

  final bool light = Palette.light;
  final AppStyle look = Palette.style;

  final Fx fx;
  final ValueListenable<double> seconds;
  final double radius;

  @override
  void paint(Canvas canvas, Size size) {
    final t = seconds.value;
    final c = size.center(Offset.zero);
    final r = radius + 16;

    // A thin glass ring is always there.
    canvas.drawCircle(
      c,
      r,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1
        ..color = Palette.white.withValues(alpha: 0.1),
    );

    final outer = size.width / 2;
    switch (fx) {
      case Fx.idle:
        break;
      case Fx.connecting:
        _arc(canvas, c, r, t * 4.2, 1.6, Palette.orangeLight, 3);
      // Each style has its own effect for each mode.
      case Fx.smart:
        switch (look) {
          case AppStyle.tiger:
            _orbits(canvas, c, t, r);
          case AppStyle.zarya:
            _radar(canvas, c, t, r, outer);
          case AppStyle.curse:
            _cleave(canvas, c, t, r, outer);
          case AppStyle.miku:
            _autotune(canvas, c, t, r, outer);
        }
      case Fx.basic:
        switch (look) {
          case AppStyle.tiger:
            _arc(canvas, c, r, t * 0.8, 1.9, Palette.white, 2.4);
            _arc(canvas, c, r, -t * 0.8 + math.pi, 1.9, Palette.orange, 2.4);
          case AppStyle.zarya:
            _compass(canvas, c, t, r, outer);
          case AppStyle.curse:
            _binding(canvas, c, t, r);
          case AppStyle.miku:
            _duet(canvas, c, t, r);
        }
      case Fx.all:
        switch (look) {
          case AppStyle.tiger:
            _waves(canvas, c, t, outer, 1);
            _arc(canvas, c, r, t * 1.6, math.pi * 2, Palette.orange, 1.6);
          case AppStyle.zarya:
            _dawn(canvas, c, t, r);
            _rays(canvas, c, t, r, outer);
          case AppStyle.curse:
            _teeth(canvas, c, t, r, outer);
            _slashes(canvas, c, t, r, outer);
          case AppStyle.miku:
            _equaliser(canvas, c, t, r, outer);
            _arc(canvas, c, r, t * 1.6, math.pi * 2, Palette.second, 1.6);
        }
    }
  }

  static Offset _dir(double a) => Offset(math.cos(a), math.sin(a));

  /// A steady random number in 0..1 for a pair of integers.
  static double _rnd(int i, int k) {
    final x = math.sin(i * 12.9898 + k * 78.233) * 43758.5453;
    return x - x.floorToDouble();
  }

  /// Tiger, smart: three sparks circle the button, looking for the best way.
  void _orbits(Canvas canvas, Offset c, double t, double r) {
    for (var i = 0; i < 3; i++) {
      final a = t * 0.9 + i * 2 * math.pi / 3;
      _arc(canvas, c, r, a - 0.9, 0.9, Palette.orange, 2.4);
      drawMote(canvas, Mote.spark, c + _dir(a) * r, 4, Palette.orangeLight, 1);
    }
  }

  /// Заря, smart: an anomaly detector sweeps the ring, and finds light up
  /// where it passes.
  void _radar(Canvas canvas, Offset c, double t, double r, double outer) {
    final a = t * 1.7;
    final band = outer - 4 - radius;
    final mid = radius + band / 2;
    final rect = Rect.fromCircle(center: c, radius: mid);
    canvas.drawArc(
      rect,
      a - 1.3,
      1.3,
      false,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = band
        ..shader = SweepGradient(
          startAngle: 0,
          endAngle: 1.3,
          colors: [Palette.orange.withValues(alpha: 0), Palette.orange.withValues(alpha: 0.5)],
          transform: GradientRotation(a - 1.3),
        ).createShader(rect),
    );
    canvas.drawLine(
      c + _dir(a) * radius,
      c + _dir(a) * (outer - 4),
      Paint()
        ..strokeWidth = 2
        ..strokeCap = StrokeCap.round
        ..color = Palette.orangeLight,
    );
    // Range rings.
    for (final rr in [r, outer - 6]) {
      canvas.drawCircle(
        c,
        rr,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1
          ..color = Palette.orange.withValues(alpha: 0.25),
      );
    }
    // Finds: each lights up as the sweep passes it and slowly fades.
    final turn = (a / (2 * math.pi)).floor();
    for (var i = 0; i < 6; i++) {
      final at = _rnd(i, turn) * 2 * math.pi;
      final since = (a - at) % (2 * math.pi);
      final alpha = (1 - since / 3).clamp(0.0, 1.0);
      if (alpha == 0) continue;
      final d = radius + 6 + (band - 10) * _rnd(i + 9, turn);
      drawMote(canvas, Mote.spark, c + _dir(at) * d, 2.4, Palette.second, alpha);
    }
  }

  /// Заря, basic: a compass bezel turns slowly; a white needle points the
  /// direct way and a green one the VPN's, the opposite way.
  void _compass(Canvas canvas, Offset c, double t, double r, double outer) {
    const n = 48;
    final tick = Paint()
      ..strokeWidth = 1.6
      ..strokeCap = StrokeCap.round;
    for (var i = 0; i < n; i++) {
      final a = t * 0.12 + i * 2 * math.pi / n;
      final long = i % 6 == 0;
      tick.color = Palette.orange.withValues(alpha: long ? 0.8 : 0.35);
      canvas.drawLine(c + _dir(a) * (r + 2), c + _dir(a) * (r + (long ? 10 : 5)), tick);
    }
    final swing = -math.pi / 2 + 0.45 * math.sin(t * 0.9) + 0.12 * math.sin(t * 3.1);
    void needle(double a, Color color) {
      final d = _dir(a), side = _dir(a + math.pi / 2);
      final path = Path()
        ..moveTo(c.dx + d.dx * (outer - 2), c.dy + d.dy * (outer - 2))
        ..lineTo(c.dx + d.dx * (r + 3) + side.dx * 6, c.dy + d.dy * (r + 3) + side.dy * 6)
        ..lineTo(c.dx + d.dx * (r + 3) - side.dx * 6, c.dy + d.dy * (r + 3) - side.dy * 6)
        ..close();
      canvas.drawPath(
        path,
        Paint()
          ..color = color.withValues(alpha: 0.5)
          ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 4),
      );
      canvas.drawPath(path, Paint()..color = color);
    }

    needle(swing, Palette.shine);
    needle(swing + math.pi, Palette.orange);
  }

  /// Заря, all: the dawn glow breathes around the button.
  void _dawn(Canvas canvas, Offset c, double t, double r) {
    canvas.drawCircle(
      c,
      r + 4,
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = 6
        ..color = Palette.second.withValues(alpha: 0.25 + 0.2 * math.sin(t * 1.2))
        ..maskFilter = const MaskFilter.blur(BlurStyle.normal, 6),
    );
  }

  /// Король проклятий, smart: invisible blades cut across the ring in
  /// crossing pairs, one after another.
  void _cleave(Canvas canvas, Offset c, double t, double r, double outer) {
    const cycle = 0.7;
    for (var i = 0; i < 3; i++) {
      final time = t / cycle + i / 3;
      final k = time.floor();
      final p = time - k;
      final a = _rnd(i, k) * 2 * math.pi;
      final grow = (p / 0.18).clamp(0.0, 1.0);
      final alpha = p < 0.18 ? 1.0 : (1 - (p - 0.18) / 0.82);
      final at = c + _dir(a) * (r + (outer - r) * 0.35);
      for (final cross in [-0.4, 0.4]) {
        drawMote(
          canvas,
          Mote.slash,
          at,
          5.2 * grow + 0.5,
          cross < 0 ? Palette.orange : Palette.orangeLight,
          alpha,
          angle: a + math.pi / 2 + cross,
        );
      }
    }
  }

  /// Король проклятий, basic: a vow binds the button: two chains turn
  /// against each other, held by four seals.
  void _binding(Canvas canvas, Offset c, double t, double r) {
    for (final (rr, speed, n) in [(r, 0.5, 22), (r + 12, -0.35, 28)]) {
      for (var i = 0; i < n; i++) {
        final a = t * speed + i * 2 * math.pi / n;
        final upright = i.isEven;
        canvas.save();
        canvas.translate(c.dx + math.cos(a) * rr, c.dy + math.sin(a) * rr);
        canvas.rotate(a + math.pi / 2);
        canvas.drawRRect(
          RRect.fromRectAndRadius(
            Rect.fromCenter(center: Offset.zero, width: upright ? 9 : 7, height: upright ? 4.5 : 2.2),
            const Radius.circular(2.2),
          ),
          Paint()
            ..style = PaintingStyle.stroke
            ..strokeWidth = 1.5
            ..color = (rr == r ? Palette.orange : Palette.orangeDeep).withValues(alpha: 0.9),
        );
        canvas.restore();
      }
    }
    for (var k = 0; k < 4; k++) {
      final a = math.pi / 4 + k * math.pi / 2;
      final pulse = 0.55 + 0.45 * math.sin(t * 2.4 + k * math.pi / 2);
      drawMote(canvas, Mote.flake, c + _dir(a) * (r + 6), 3.4, Palette.orangeLight, pulse, angle: a);
    }
  }

  /// Король проклятий, all: the domain's teeth rise around the button.
  void _teeth(Canvas canvas, Offset c, double t, double r, double outer) {
    const n = 30;
    final paint = Paint()..color = Palette.orangeDeep.withValues(alpha: 0.85);
    for (var i = 0; i < n; i++) {
      final a = i * 2 * math.pi / n + t * 0.1;
      final len = 5 + (outer - r - 10) * 0.6 * (0.5 + 0.5 * math.sin(t * 2.2 + i * 2.4)).abs();
      final d = _dir(a), side = _dir(a + math.pi / 2);
      final base = c + d * (r + 1);
      canvas.drawPath(
        Path()
          ..moveTo(base.dx + side.dx * 3.5, base.dy + side.dy * 3.5)
          ..lineTo(base.dx + d.dx * len, base.dy + d.dy * len)
          ..lineTo(base.dx - side.dx * 3.5, base.dy - side.dy * 3.5)
          ..close(),
        paint,
      );
    }
  }

  /// Mike, smart: a voice's waveform around the button, tuned as it goes.
  void _autotune(Canvas canvas, Offset c, double t, double r, double outer) {
    final amp = (outer - r - 8) * 0.45;
    for (final (color, phase, scale) in [(Palette.orange, 0.0, 1.0), (Palette.second, 1.7, 0.6)]) {
      final path = Path();
      for (var k = 0; k <= 180; k++) {
        final a = k / 180 * 2 * math.pi;
        final env = 0.55 + 0.45 * math.sin(a * 3 - t * 2 + phase);
        final rr = r + 6 + amp * scale * env * math.sin(a * 10 + t * 6 + phase);
        final p = c + _dir(a) * rr;
        k == 0 ? path.moveTo(p.dx, p.dy) : path.lineTo(p.dx, p.dy);
      }
      canvas.drawPath(
        path,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = 2
          ..strokeJoin = StrokeJoin.round
          ..color = color.withValues(alpha: 0.9),
      );
    }
  }

  /// Mike, basic: a duet: two lights run around the stage's lamps the
  /// opposite ways, teal and pink.
  void _duet(Canvas canvas, Offset c, double t, double r) {
    const n = 28;
    final one = (t * 7) % n, two = n - (t * 7) % n;
    double near(double head, int i) {
      final d = (i - head).abs();
      return math.exp(-math.min(d, n - d) / 1.6);
    }

    for (var i = 0; i < n; i++) {
      final at = c + _dir(i * 2 * math.pi / n - math.pi / 2) * (r + 8);
      final a1 = near(one, i), a2 = near(two, i);
      canvas.drawCircle(at, 2.4, Paint()..color = Palette.white.withValues(alpha: 0.22));
      if (a1 > 0.05) drawMote(canvas, Mote.spark, at, 2.2 + a1 * 2, Palette.orange, a1);
      if (a2 > 0.05) drawMote(canvas, Mote.spark, at, 2.2 + a2 * 2, Palette.second, a2);
    }
  }

  /// Waves leave the button every 0.6 s and fade as they grow.
  void _waves(Canvas canvas, Offset c, double t, double outer, double strength) {
    for (var i = 0; i < 3; i++) {
      final p = ((t / 1.8) + i / 3) % 1.0;
      final rr = radius + p * (outer - radius);
      canvas.drawCircle(
        c,
        rr,
        Paint()
          ..style = PaintingStyle.stroke
          ..strokeWidth = 2.2 * (1 - p) + 0.4
          ..color = Palette.orange.withValues(alpha: 0.7 * (1 - p) * strength),
      );
    }
  }

  /// Dawn: rays turn slowly around the button, breathing in and out.
  void _rays(Canvas canvas, Offset c, double t, double r, double outer) {
    const n = 16;
    for (var i = 0; i < n; i++) {
      final a = t * 0.25 + i * 2 * math.pi / n;
      final len = (outer - r - 6) * (0.45 + 0.35 * math.sin(t * 1.4 + i * 1.7)) * (i.isEven ? 1 : 0.6);
      final dir = Offset(math.cos(a), math.sin(a));
      final from = c + dir * (r + 6);
      final paint = Paint()
        ..strokeWidth = 2
        ..strokeCap = StrokeCap.round
        ..shader = LinearGradient(colors: [Palette.second.withValues(alpha: 0.8), Palette.second.withValues(alpha: 0)])
            .createShader(Rect.fromPoints(from, from + dir * len));
      canvas.drawLine(from, from + dir * len, paint);
    }
  }

  /// A domain: slashes flash all around the button and fade, each in its
  /// own place and time.
  void _slashes(Canvas canvas, Offset c, double t, double r, double outer) {
    const n = 14;
    for (var i = 0; i < n; i++) {
      final cycle = 0.9 + (i % 5) * 0.17;
      final p = ((t + i * 0.37) / cycle) % 1.0;
      final round = ((t + i * 0.37) / cycle).floor();
      // A new place on each flash.
      final seed = math.sin((i * 12.9898 + round * 78.233)) * 43758.5453;
      final rnd = seed - seed.floorToDouble();
      final a = rnd * 2 * math.pi;
      final at = c + Offset(math.cos(a), math.sin(a)) * (r + 4 + (outer - r - 14) * ((rnd * 7) % 1.0));
      final alpha = p < 0.15 ? p / 0.15 : (1 - p) / 0.85;
      drawMote(canvas, Mote.slash, at, 2.6, i.isEven ? Palette.orange : Palette.orangeLight, alpha, angle: a + 0.9);
    }
  }

  /// A stage: bars around the button jump to the beat, in two colours.
  void _equaliser(Canvas canvas, Offset c, double t, double r, double outer) {
    const n = 48;
    final paint = Paint()
      ..strokeWidth = 3
      ..strokeCap = StrokeCap.round;
    for (var i = 0; i < n; i++) {
      final a = i * 2 * math.pi / n - math.pi / 2;
      final beat = math.pow(math.sin(t * 2.6 * math.pi / 2).abs(), 3).toDouble();
      final level = 0.25 + 0.45 * (0.5 + 0.5 * math.sin(t * 3.1 + i * 0.9)) * (0.6 + 0.4 * beat);
      final len = (outer - r - 8) * level;
      final dir = Offset(math.cos(a), math.sin(a));
      paint.color = (i.isEven ? Palette.orange : Palette.second).withValues(alpha: 0.85);
      canvas.drawLine(c + dir * (r + 5), c + dir * (r + 5 + len), paint);
    }
  }

  /// An arc that fades in from its tail to a bright head.
  void _arc(Canvas canvas, Offset c, double r, double start, double sweep, Color color, double width) {
    final rect = Rect.fromCircle(center: c, radius: r);
    final full = sweep >= math.pi * 2 - 0.01;
    final paint = Paint()
      ..style = PaintingStyle.stroke
      ..strokeWidth = width
      ..strokeCap = StrokeCap.round;
    if (full) {
      paint.color = color.withValues(alpha: 0.35);
      canvas.drawCircle(c, r, paint);
      return;
    }
    paint.shader = SweepGradient(
      startAngle: 0,
      endAngle: sweep,
      colors: [color.withValues(alpha: 0), color],
      transform: GradientRotation(start),
    ).createShader(rect);
    canvas.drawArc(rect, start, sweep, false, paint);
  }

  @override
  bool shouldRepaint(_RingPainter old) =>
      old.fx != fx || old.radius != radius || old.light != light || old.look != look;
}
