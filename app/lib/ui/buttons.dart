import 'package:flutter/material.dart';

import 'glass.dart';
import 'palette.dart';

/// The main action: an orange pill that glows.
class FireButton extends StatelessWidget {
  const FireButton({super.key, required this.label, required this.icon, this.onPressed, this.busy = false});

  final String label;
  final IconData icon;
  final VoidCallback? onPressed;
  final bool busy;

  @override
  Widget build(BuildContext context) {
    final enabled = onPressed != null;
    return AnimatedOpacity(
      duration: const Duration(milliseconds: 200),
      opacity: enabled || busy ? 1 : 0.5,
      child: DecoratedBox(
        decoration: BoxDecoration(
          gradient: Palette.fire,
          borderRadius: BorderRadius.circular(28),
          boxShadow: [
            BoxShadow(color: Palette.orange.withValues(alpha: 0.4), blurRadius: 24, offset: const Offset(0, 6)),
          ],
        ),
        child: Material(
          type: MaterialType.transparency,
          child: InkWell(
            borderRadius: BorderRadius.circular(28),
            onTap: onPressed,
            child: SizedBox(
              height: 56,
              child: Row(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  if (busy)
                    const SizedBox.square(
                      dimension: 18,
                      child: CircularProgressIndicator(strokeWidth: 2, color: Palette.ink),
                    )
                  else
                    Icon(icon, color: Palette.ink, size: 22),
                  const SizedBox(width: 10),
                  Text(
                    label,
                    style: const TextStyle(
                      color: Palette.ink,
                      fontSize: 16,
                      fontWeight: FontWeight.w700,
                      letterSpacing: 0.3,
                    ),
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

/// A secondary action on glass.
class GlassButton extends StatelessWidget {
  const GlassButton({super.key, required this.label, required this.icon, this.onPressed});

  final String label;
  final IconData icon;
  final VoidCallback? onPressed;

  @override
  Widget build(BuildContext context) {
    return Opacity(
      opacity: onPressed == null ? 0.5 : 1,
      child: Glass(
        radius: 24,
        child: Material(
          type: MaterialType.transparency,
          child: InkWell(
            onTap: onPressed,
            child: SizedBox(
              height: 50,
              child: Row(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  Icon(icon, size: 20, color: Palette.orangeLight),
                  const SizedBox(width: 8),
                  Text(
                    label,
                    style: TextStyle(color: Palette.white, fontSize: 15, fontWeight: FontWeight.w600),
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

/// A round glass button for the top bar.
class GlassIcon extends StatelessWidget {
  const GlassIcon({super.key, required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) => SizedBox.square(
    dimension: 44,
    child: Glass(radius: 22, child: Center(child: child)),
  );
}
