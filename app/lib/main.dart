import 'package:flutter/material.dart';

import 'app.dart';
import 'core.dart';

void main() {
  // PlatformCore registers a channel handler, which needs the binding.
  WidgetsFlutterBinding.ensureInitialized();
  runApp(TigerApp(core: PlatformCore()));
}
