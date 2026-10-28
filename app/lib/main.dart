import 'package:flutter/material.dart';

import 'app.dart';
import 'core.dart';
import 'licenses.dart';
import 'ui/palette.dart';

Future<void> main() async {
  // PlatformCore registers a channel handler, which needs the binding.
  WidgetsFlutterBinding.ensureInitialized();
  registerLicenses();
  final core = PlatformCore();
  // The saved theme comes before the first frame, so a light app does not
  // flash dark.
  try {
    lightTheme.value = await core.lightTheme();
  } catch (_) {
    // Dark, as before the user chose.
  }
  runApp(TigerApp(core: core));
}
