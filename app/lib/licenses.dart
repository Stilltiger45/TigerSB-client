import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

/// Adds the licenses of TigerSB itself and of the Go libraries inside its core
/// to the ones Flutter collects for its own packages, so the licenses screen
/// lists everything the app is built from. The texts come from
/// assets/licenses.json, which scripts/licensetexts writes.
void registerLicenses() => LicenseRegistry.addLicense(_coreLicenses);

Stream<LicenseEntry> _coreLicenses() async* {
  final raw = await rootBundle.loadString('assets/licenses.json');
  for (final e in jsonDecode(raw) as List) {
    yield LicenseEntryWithLineBreaks(List<String>.from(e['packages'] as List), e['text'] as String);
  }
}
