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

  static const _labels = {Mode.auto: 'Умная', Mode.basic: 'Базовая', Mode.all: 'Все'};

  @override
  Widget build(BuildContext context) {
    final modes = Mode.values;
    final i = modes.indexOf(mode);
    return Glass(
      radius: 30,
      padding: const EdgeInsets.all(5),
      child: SizedBox(
        height: 46,
        child: Stack(
          children: [
            AnimatedAlign(
              duration: const Duration(milliseconds: 380),
              curve: Curves.easeOutBack,
              alignment: Alignment(-1 + 2 * i / (modes.length - 1), 0),
              child: FractionallySizedBox(
                widthFactor: 1 / modes.length,
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
                for (final m in modes)
                  Expanded(
                    child: Semantics(
                      selected: m == mode,
                      button: true,
                      child: GestureDetector(
                        behavior: HitTestBehavior.opaque,
                        onTap: () => onChanged(m),
                        child: Center(
                          child: AnimatedDefaultTextStyle(
                            duration: const Duration(milliseconds: 250),
                            style: TextStyle(
                              fontSize: 15,
                              fontWeight: m == mode ? FontWeight.w700 : FontWeight.w500,
                              color: m == mode ? Palette.ink : Palette.muted,
                            ),
                            child: Text(_labels[m]!),
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
