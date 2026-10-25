import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

/// Scans a QR code with a key and returns its text.
class ScanScreen extends StatefulWidget {
  const ScanScreen({super.key});

  @override
  State<ScanScreen> createState() => _ScanScreenState();
}

class _ScanScreenState extends State<ScanScreen> {
  bool _done = false;

  bool _looksLikeKey(String s) =>
      RegExp(r'^(tigersb|https|vless|vmess|trojan|ss)://', caseSensitive: false).hasMatch(s);

  void _onDetect(BarcodeCapture capture) {
    if (_done) return;
    for (final b in capture.barcodes) {
      final s = b.rawValue?.trim();
      if (s != null && _looksLikeKey(s)) {
        _done = true;
        Navigator.of(context).pop(s);
        return;
      }
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Наведите на QR-код')),
      body: MobileScanner(onDetect: _onDetect),
    );
  }
}
