import 'package:flutter/material.dart';

import '../core.dart';
import 'glass.dart';
import 'palette.dart';

/// Glass pill with the three routing modes and an orange thumb that slides
/// to the chosen one.
class ModeSwitch extends StatelessWidget {
  const ModeSwitch({super.key, required this.mode, required this.onChanged});

  final Mode mode;
  final ValueChanged<Mode> onChanged;

  @override
  Widget build(BuildContext context) => Segments(
    labels: const {Mode.auto: 'Умная', Mode.basic: 'Базовая', Mode.all: 'Все'},
    value: mode,
    onChanged: onChanged,
  );
}

/// Glass pill with a few choices and an orange thumb that slides to the
/// chosen one.
class Segments<T> extends StatelessWidget {
  const Segments({super.key, required this.labels, required this.value, required this.onChanged, this.height = 46});

  /// The choices in order, with their labels.
  final Map<T, String> labels;
  final T value;
  final ValueChanged<T> onChanged;
  final double height;

  @override
  Widget build(BuildContext context) {
    final values = labels.keys.toList();
    final i = values.indexOf(value);
    return Glass(
      radius: 30,
      padding: const EdgeInsets.all(5),
      child: SizedBox(
        height: height,
        child: Stack(
          children: [
            AnimatedAlign(
              duration: const Duration(milliseconds: 380),
              curve: Curves.easeOutBack,
              alignment: Alignment(-1 + 2 * i / (values.length - 1), 0),
              child: FractionallySizedBox(
                widthFactor: 1 / values.length,
                heightFactor: 1,
                child: DecoratedBox(
                  decoration: BoxDecoration(
                    gradient: Palette.fire,
                    borderRadius: BorderRadius.circular(26),
                    boxShadow: [BoxShadow(color: Palette.orange.withValues(alpha: 0.45), blurRadius: 18)],
                  ),
                ),
              ),
            ),
            Row(
              children: [
                for (final v in values)
                  Expanded(
                    child: Semantics(
                      selected: v == value,
                      button: true,
                      child: GestureDetector(
                        behavior: HitTestBehavior.opaque,
                        onTap: () => onChanged(v),
                        child: Center(
                          child: AnimatedDefaultTextStyle(
                            duration: const Duration(milliseconds: 250),
                            style: TextStyle(
                              fontSize: 15,
                              fontWeight: v == value ? FontWeight.w700 : FontWeight.w500,
                              color: v == value ? Palette.ink : Palette.muted,
                            ),
                            child: Text(labels[v]!),
                          ),
                        ),
                      ),
                    ),
                  ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
