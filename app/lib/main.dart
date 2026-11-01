import 'dart:io';

import 'package:flutter/material.dart';

import 'app.dart';
import 'core.dart';
import 'desktop_core.dart';
import 'licenses.dart';
import 'ui/palette.dart';

Future<void> main(List<String> args) async {
  // PlatformCore registers a channel handler, which needs the binding.
  WidgetsFlutterBinding.ensureInitialized();
  registerLicenses();
  final Core core = Platform.isWindows ? DesktopCore(args) : PlatformCore();
  // The saved theme and style come before the first frame, so a light app
  // does not flash dark, nor a styled one orange.
  try {
    lightTheme.value = await core.lightTheme();
    appStyle.value = AppStyle.byName(await core.style());
  } catch (_) {
    // Dark and standard, as before the user chose.
  }
  // Started with Windows: the VPN comes back on if the user left it on.
  if (core is DesktopCore && args.contains('--autostart')) core.resume();
  runApp(TigerApp(core: core));
}
