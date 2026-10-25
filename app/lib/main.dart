import 'package:flutter/material.dart';

import 'app.dart';
import 'core.dart';
import 'licenses.dart';

void main() {
  // PlatformCore registers a channel handler, which needs the binding.
  WidgetsFlutterBinding.ensureInitialized();
  registerLicenses();
  runApp(TigerApp(core: PlatformCore()));
}
