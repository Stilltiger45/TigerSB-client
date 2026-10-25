import 'dart:ui';

import 'package:flutter/material.dart';

/// Light liquid glass: blurs what is behind, with a soft sheen from the top
/// left and a thin bright edge.
class Glass extends StatelessWidget {
  const Glass({
    super.key,
    required this.child,
    this.radius = 24,
    this.padding = EdgeInsets.zero,
    this.tint,
    this.blur = 16,
  });

  final Widget child;
  final double radius;
  final EdgeInsetsGeometry padding;

  /// A colour mixed into the glass, for a warm glow under it.
  final Color? tint;
  final double blur;

  @override
  Widget build(BuildContext context) {
    final r = BorderRadius.circular(radius);
    return ClipRRect(
      borderRadius: r,
      child: BackdropFilter(
        filter: ImageFilter.blur(sigmaX: blur, sigmaY: blur),
        child: CustomPaint(
          painter: _GlassPainter(radius: radius, tint: tint),
          child: Padding(padding: padding, child: child),
        ),
      ),
    );
  }
}

class _GlassPainter extends CustomPainter {
  _GlassPainter({required this.radius, this.tint});

  final double radius;
  final Color? tint;

  @override
  void paint(Canvas canvas, Size size) {
    final rect = Offset.zero & size;
    final rr = RRect.fromRectAndRadius(rect, Radius.circular(radius));
    // Body: a little white, more at the top, like light falling on glass.
    canvas.drawRRect(
      rr,
      Paint()
        ..shader = LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [
            Color.alphaBlend((tint ?? Colors.transparent).withValues(alpha: 0.06), const Color(0x17FFFFFF)),
            Color.alphaBlend((tint ?? Colors.transparent).withValues(alpha: 0.02), const Color(0x05FFFFFF)),
          ],
        ).createShader(rect),
    );
    // Sheen: a soft highlight across the upper part.
    canvas.drawRRect(
      rr,
      Paint()
        ..shader = RadialGradient(
          center: const Alignment(-0.6, -1.2),
          radius: 1.2,
          colors: const [Color(0x22FFFFFF), Color(0x00FFFFFF)],
        ).createShader(rect),
    );
    // Edge: bright at the top left, fading towards the bottom right.
    canvas.drawRRect(
      rr.deflate(0.5),
      Paint()
        ..style = PaintingStyle.stroke
        ..strokeWidth = 1
        ..shader = const LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [Color(0x59FFFFFF), Color(0x14FFFFFF), Color(0x26FFFFFF)],
          stops: [0, 0.55, 1],
        ).createShader(rect),
    );
  }

  @override
  bool shouldRepaint(_GlassPainter old) => old.radius != radius || old.tint != tint;
}
