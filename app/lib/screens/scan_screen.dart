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

  /// A link the core reads, or a WireGuard / AmneziaWG config as the
  /// AmneziaWG apps put it into a QR code.
  bool _looksLikeKey(String s) =>
      RegExp(
        r'^(tigersb|https|vless|vmess|trojan|ss|hysteria2|hy2|tuic|mierus|mieru|naive\+https|naive\+quic|wireguard|wg|awg|vpn)://',
        caseSensitive: false,
      ).hasMatch(s) ||
      s.toLowerCase().contains('[interface]');

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
