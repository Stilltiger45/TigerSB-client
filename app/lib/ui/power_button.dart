import 'dart:math' as math;
import 'dart:ui';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';

import 'fx.dart';
import 'palette.dart';

/// The big round button. Off it is clear glass; on it glows orange, and the
/// ring around it moves in the way of the routing mode:
/// - smart: three sparks circle the button, looking for the best way;
/// - basic: a white and an orange arc turn against each other, direct and VPN;
/// - all: waves run out of the button, everything goes through the tunnel.
/// The style changes the sparks' shape and what runs out in the all mode:
/// sun rays, slashes of a domain or an equaliser.
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

    switch (fx) {
      case Fx.idle:
        break;
      case Fx.connecting:
        _arc(canvas, c, r, t * 4.2, 1.6, Palette.orangeLight, 3);
      case Fx.smart:
        for (var i = 0; i < 3; i++) {
          final a = t * 0.9 + i * 2 * math.pi / 3;
          _arc(canvas, c, r, a - 0.9, 0.9, Palette.orange, 2.4);
          final p = c + Offset(math.cos(a), math.sin(a)) * r;
          // Heads point along the way they fly.
          final angle = look.mote == Mote.note ? 0.0 : a + math.pi / 2;
          drawMote(canvas, look.mote, p, look.mote == Mote.spark ? 4 : 3, Palette.orangeLight, 1, angle: angle);
        }
      case Fx.basic:
        _arc(canvas, c, r, t * 0.8, 1.9, Palette.white, 2.4);
        _arc(canvas, c, r, -t * 0.8 + math.pi, 1.9, Palette.orange, 2.4);
      case Fx.all:
        final outer = size.width / 2;
        switch (look) {
          case AppStyle.tiger:
            _waves(canvas, c, t, outer, 1);
          case AppStyle.zarya:
            _waves(canvas, c, t, outer, 0.6);
            _rays(canvas, c, t, r, outer);
          case AppStyle.curse:
            _waves(canvas, c, t, outer, 0.35);
            _slashes(canvas, c, t, r, outer);
          case AppStyle.miku:
            _equaliser(canvas, c, t, r, outer);
        }
        _arc(canvas, c, r, t * 1.6, math.pi * 2, Palette.orange, 1.6);
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
