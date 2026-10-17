import 'dart:async';
import 'dart:io';
import 'dart:math';

/// 扫码搜索的一次性局域网输入服务。
///
/// 电视/投影上用遥控器打字很痛苦，这里让手机扫码后输入：电视端起一个临时
/// HTTP 服务，把带 token 的地址渲染成二维码，手机扫码打开表单、提交后回传。
///
/// 安全边界：
/// - 只绑定局域网地址，跳过回环与常见虚拟网卡
/// - 二维码中的随机 token 一次性消费，服务有存活上限
/// - 拒绝 Transfer-Encoding
/// - 页面不引用任何外部资源，响应带 CSP / no-store
class ScanSearchServer {
  ScanSearchServer._(this._server, this._token, this._deadline);

  final HttpServer _server;
  final String _token;
  final DateTime _deadline;
  final _completer = Completer<String?>();
  StreamSubscription<HttpRequest>? _subscription;
  Timer? _expiry;
  var _consumed = false;
  var _closed = false;

  /// 手机要访问的地址，用于生成二维码。
  String get url => 'http://${_server.address.address}:${_server.port}/$_token';

  /// 提交的查询词；超时、取消或未提交时返回 null。
  Future<String?> get result => _completer.future;

  /// 创建服务并开始监听。找不到可用的局域网地址时返回 null。
  static Future<ScanSearchServer?> start({
    Duration ttl = const Duration(minutes: 5),
    InternetAddress? address,
  }) async {
    final bindAddress = address?.address ?? await _localAddress();
    if (bindAddress == null) return null;
    final HttpServer server;
    try {
      server = await HttpServer.bind(bindAddress, 0, shared: false);
    } on SocketException {
      return null;
    }
    final instance = ScanSearchServer._(
      server,
      _randomToken(),
      DateTime.now().add(ttl),
    );
    instance._serve();
    return instance;
  }

  /// 取一个站点本地地址，优先常见家用网段，跳过虚拟网卡。
  static Future<String?> _localAddress() async {
    try {
      final interfaces = await NetworkInterface.list(
        type: InternetAddressType.IPv4,
        includeLoopback: false,
        includeLinkLocal: false,
      );
      final candidates = <(int, String)>[];
      for (final interface in interfaces) {
        final name = interface.name.toLowerCase();
        final virtual =
            name.contains('docker') ||
            name.contains('veth') ||
            name.contains('vmnet') ||
            name.contains('vbox') ||
            name.startsWith('tun') ||
            name.startsWith('tap');
        for (final address in interface.addresses) {
          final value = address.address;
          if (address.isLoopback || value.startsWith('169.254.')) continue;
          candidates.add((virtual ? 100 + _rank(value) : _rank(value), value));
        }
      }
      if (candidates.isEmpty) return null;
      candidates.sort((a, b) => a.$1.compareTo(b.$1));
      return candidates.first.$2;
    } catch (_) {
      return null;
    }
  }

  /// 家用网段排序：192.168 < 10 < 172.16-31 < 其它。
  static int _rank(String address) {
    if (address.startsWith('192.168.')) return 0;
    if (address.startsWith('10.')) return 1;
    if (address.startsWith('172.')) {
      final parts = address.split('.');
      final second = parts.length > 1 ? int.tryParse(parts[1]) : null;
      if (second != null && second >= 16 && second <= 31) return 2;
    }
    return 3;
  }

  static String _randomToken() {
    final random = Random.secure();
    return List<int>.generate(
      16,
      (_) => random.nextInt(256),
    ).map((b) => b.toRadixString(16).padLeft(2, '0')).join();
  }

  bool get _expired => DateTime.now().isAfter(_deadline);

  /// HttpServer 是单订阅流，只能监听一次；超时由定时器收口。
  void _serve() {
    _subscription = _server.listen(
      (request) => unawaited(_handle(request)),
      onError: (_) {},
      cancelOnError: false,
    );
    final remaining = _deadline.difference(DateTime.now());
    _expiry = Timer(
      remaining.isNegative ? Duration.zero : remaining,
      () => unawaited(close()),
    );
  }

  Future<void> _handle(HttpRequest request) async {
    final response = request.response;
    response.headers.set('Cache-Control', 'no-store');
    response.headers.set('X-Content-Type-Options', 'nosniff');
    response.headers.set('Referrer-Policy', 'no-referrer');
    response.headers.set(
      'Content-Security-Policy',
      "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; "
          'frame-ancestors \'none\'; base-uri \'none\'',
    );

    if (_consumed || _expired) {
      await _respond(response, HttpStatus.gone, '二维码已失效，请在电视上重新打开');
      return;
    }
    final path = request.uri.path;
    if (path != '/$_token' && path != '/$_token/submit') {
      await _respond(response, HttpStatus.notFound, '页面不存在');
      return;
    }
    if (request.method == 'GET') {
      await _respond(response, HttpStatus.ok, _form());
      return;
    }
    if (request.method != 'POST') {
      await _respond(response, HttpStatus.methodNotAllowed, '不支持的操作');
      return;
    }

    if (request.headers.value(HttpHeaders.transferEncodingHeader) != null) {
      await _respond(response, HttpStatus.badRequest, '请求格式错误');
      return;
    }
    final contentType = request.headers.contentType;
    if (contentType == null ||
        contentType.mimeType != 'application/x-www-form-urlencoded') {
      await _respond(response, HttpStatus.badRequest, '请求格式错误');
      return;
    }

    final body = <int>[];
    await for (final chunk in request) {
      body.addAll(chunk);
      if (body.length > 4096) {
        await _respond(response, HttpStatus.requestEntityTooLarge, '输入内容过长');
        return;
      }
    }
    final fields = Uri.splitQueryString(String.fromCharCodes(body));
    final query = fields['query']?.trim() ?? '';
    final invalid =
        query.isEmpty ||
        query.length > 80 ||
        query.runes.any((r) => r < 0x20 || r == 0x7f);
    if (invalid) {
      await _respond(response, HttpStatus.badRequest, '请输入 1—80 个字符的剧名');
      return;
    }

    _consumed = true;
    await _respond(
      response,
      HttpStatus.ok,
      '<h1>已发送到电视</h1><p>请回到电视查看搜索结果。</p>',
    );
    if (!_completer.isCompleted) _completer.complete(query);
    await close();
  }

  Future<void> _respond(
    HttpResponse response,
    int status,
    String message,
  ) async {
    response.statusCode = status;
    response.headers.contentType = ContentType.html;
    response.write(_page(message));
    await response.close();
  }

  String _form() =>
      '''
<h1>在电视上搜索</h1>
<p>输入剧名，电视会按当前选择的站源搜索。</p>
<form method="post" action="/$_token/submit">
  <input name="query" maxlength="80" required autofocus placeholder="输入剧名或关键词" autocomplete="off">
  <button>发送到电视</button>
</form>
<p>页面仅在电视的扫码窗口打开期间有效，最长 5 分钟。</p>''';

  String _page(String body) => '''
<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>扫码搜索</title>
<style>
body{font:18px/1.6 sans-serif;background:#10131b;color:#f4f5f8;max-width:520px;margin:48px auto;padding:0 20px}
h1{font-size:22px}
input,button{box-sizing:border-box;width:100%;font:inherit;padding:16px;margin:12px 0;border-radius:8px;border:0}
input{background:#1c2130;color:#f4f5f8}
button{background:#ff634c;color:#fff}
p{color:#a8b0c0}
</style></head><body>$body</body></html>''';

  /// 关闭服务并释放端口；未提交时让 [result] 返回 null。
  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    _expiry?.cancel();
    _expiry = null;
    if (!_completer.isCompleted) _completer.complete(null);
    try {
      await _subscription?.cancel();
    } catch (_) {}
    _subscription = null;
    try {
      await _server.close(force: true);
    } catch (_) {}
  }
}
