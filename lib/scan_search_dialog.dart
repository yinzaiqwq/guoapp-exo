import 'dart:async';

import 'package:flutter/material.dart';
import 'package:qr/qr.dart';

import 'scan_search.dart';

/// 二维码绘制。用 qr 包生成矩阵后自绘，避免引入带 Flutter 版本约束的
/// 二维码组件库。
class QrCodeView extends StatelessWidget {
  const QrCodeView({
    super.key,
    required this.data,
    this.size = 220,
    this.foreground = Colors.black,
    this.background = Colors.white,
  });

  final String data;
  final double size;
  final Color foreground;
  final Color background;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: size,
      height: size,
      child: CustomPaint(
        painter: _QrPainter(
          data: data,
          foreground: foreground,
          background: background,
        ),
      ),
    );
  }
}

class _QrPainter extends CustomPainter {
  _QrPainter({
    required this.data,
    required this.foreground,
    required this.background,
  });

  final String data;
  final Color foreground;
  final Color background;

  @override
  void paint(Canvas canvas, Size size) {
    final QrImage image;
    try {
      image = QrImage(
        QrCode(
          payload: QrPayload.fromString(data),
          errorCorrectLevel: QrErrorCorrectLevel.medium,
        ),
      );
    } catch (_) {
      // 内容过长等情况：画一块底色，避免抛出中断渲染
      canvas.drawRect(
        Offset.zero & size,
        Paint()..color = background,
      );
      return;
    }

    canvas.drawRect(Offset.zero & size, Paint()..color = background);
    final modules = image.moduleCount;
    if (modules <= 0) return;
    // 留出静默区，否则部分扫码器识别困难
    const quiet = 4;
    final total = modules + quiet * 2;
    final cell = size.width / total;
    final paint = Paint()..color = foreground;
    for (var row = 0; row < modules; row++) {
      for (var col = 0; col < modules; col++) {
        if (!image.isDark(row, col)) continue;
        canvas.drawRect(
          Rect.fromLTWH(
            (col + quiet) * cell,
            (row + quiet) * cell,
            cell + 0.5, // 相邻模块间留极细重叠，避免出现白缝
            cell + 0.5,
          ),
          paint,
        );
      }
    }
  }

  @override
  bool shouldRepaint(_QrPainter old) =>
      old.data != data ||
      old.foreground != foreground ||
      old.background != background;
}

/// 扫码搜索对话框：显示二维码，手机扫码输入后回传查询词。
///
/// 关闭对话框或超时会释放端口；已提交则返回查询词。
class ScanSearchDialog extends StatefulWidget {
  const ScanSearchDialog({super.key, required this.title});

  final String title;

  @override
  State<ScanSearchDialog> createState() => _ScanSearchDialogState();
}

class _ScanSearchDialogState extends State<ScanSearchDialog> {
  ScanSearchServer? _server;
  String _status = '正在准备…';
  var _started = false;

  @override
  void initState() {
    super.initState();
    unawaited(_start());
  }

  Future<void> _start() async {
    if (_started) return;
    _started = true;
    final server = await ScanSearchServer.start();
    if (!mounted) {
      await server?.close();
      return;
    }
    if (server == null) {
      setState(() => _status = '未找到可用的局域网地址，请确认已连接 Wi-Fi');
      return;
    }
    setState(() {
      _server = server;
      _status = '用手机扫码输入';
    });
    final query = await server.result;
    if (!mounted) return;
    if (query == null) {
      // 超时或取消
      if (mounted) Navigator.of(context).pop();
      return;
    }
    Navigator.of(context).pop(query);
  }

  @override
  void dispose() {
    unawaited(_server?.close());
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final colors = Theme.of(context).colorScheme;
    final server = _server;
    return AlertDialog(
      title: Text(widget.title),
      content: SizedBox(
        width: 300,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (server != null)
              Container(
                padding: const EdgeInsets.all(12),
                decoration: BoxDecoration(
                  color: Colors.white,
                  borderRadius: BorderRadius.circular(12),
                ),
                child: QrCodeView(data: server.url),
              )
            else
              const SizedBox(
                height: 220,
                child: Center(child: CircularProgressIndicator()),
              ),
            const SizedBox(height: 16),
            Text(
              _status,
              style: TextStyle(fontSize: 15, color: colors.onSurfaceVariant),
              textAlign: TextAlign.center,
            ),
            if (server != null) ...[
              const SizedBox(height: 8),
              SelectableText(
                server.url,
                style: TextStyle(fontSize: 12, color: colors.onSurfaceVariant),
                textAlign: TextAlign.center,
              ),
            ],
          ],
        ),
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('取消'),
        ),
      ],
    );
  }
}
