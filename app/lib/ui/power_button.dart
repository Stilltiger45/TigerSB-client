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
                      : const LinearGradient(
                          begin: Alignment.topLeft,
                          end: Alignment.bottomRight,
                          colors: [Color(0x26FFFFFF), Color(0x0AFFFFFF)],
                        ),
                  border: Border.all(color: Colors.white.withValues(alpha: on ? 0.35 : 0.22)),
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
                              child: const CircularProgressIndicator(strokeWidth: 3, color: Palette.orangeLight),
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
        ..color = Colors.white.withValues(alpha: 0.1),
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
          _spark(canvas, c + Offset(math.cos(a), math.sin(a)) * r, Palette.orangeLight, 4);
        }
      case Fx.basic:
        _arc(canvas, c, r, t * 0.8, 1.9, Palette.white, 2.4);
        _arc(canvas, c, r, -t * 0.8 + math.pi, 1.9, Palette.orange, 2.4);
      case Fx.all:
        // Waves leave the button every 1.2 s and fade as they grow.
        for (var i = 0; i < 3; i++) {
          final p = ((t / 1.8) + i / 3) % 1.0;
          final rr = radius + p * (size.width / 2 - radius);
          canvas.drawCircle(
            c,
            rr,
            Paint()
              ..style = PaintingStyle.stroke
              ..strokeWidth = 2.2 * (1 - p) + 0.4
              ..color = Palette.orange.withValues(alpha: 0.7 * (1 - p)),
          );
        }
        _arc(canvas, c, r, t * 1.6, math.pi * 2, Palette.orange, 1.6);
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

  void _spark(Canvas canvas, Offset p, Color color, double size) {
    canvas.drawCircle(
      p,
      size * 2.6,
      Paint()
        ..color = color.withValues(alpha: 0.45)
        ..maskFilter = MaskFilter.blur(BlurStyle.normal, size * 1.4),
    );
    canvas.drawCircle(p, size, Paint()..color = color);
  }

  @override
  bool shouldRepaint(_RingPainter old) => old.fx != fx || old.radius != radius;
}
