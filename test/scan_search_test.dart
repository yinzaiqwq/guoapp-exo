import 'dart:convert';
import 'dart:io';

import 'package:duanju_app/scan_search.dart';
import 'package:flutter_test/flutter_test.dart';

Future<String> sendRawRequest(int port, String request) async {
  final socket = await Socket.connect(InternetAddress.loopbackIPv4, port);
  socket.add(utf8.encode(request));
  await socket.flush();
  return utf8.decoder.bind(socket).join();
}

void main() {
  test('扫码搜索兼容省略端口的手机浏览器 Host 和 Origin', () async {
    final server = await ScanSearchServer.start(
      address: InternetAddress.loopbackIPv4,
    );
    expect(server, isNotNull);
    addTearDown(() => server!.close());
    final uri = Uri.parse(server!.url);

    final page = await sendRawRequest(
      uri.port,
      'GET ${uri.path} HTTP/1.1\r\n'
      'Host: ${uri.host}\r\n'
      'Connection: close\r\n\r\n',
    );
    expect(page, contains('200 OK'));
    expect(page, contains('在电视上搜索'));

    final wrongToken = await sendRawRequest(
      uri.port,
      'GET /not-the-token HTTP/1.1\r\n'
      'Host: servicewechat.com\r\n'
      'Connection: close\r\n\r\n',
    );
    expect(wrongToken, contains('404 Not Found'));

    final body = 'query=${Uri.encodeQueryComponent('丑小鸭')}';
    final submitted = await sendRawRequest(
      uri.port,
      'POST ${uri.path}/submit HTTP/1.1\r\n'
      'Host: ${uri.host}\r\n'
      'Origin: http://${uri.host}\r\n'
      'Content-Type: application/x-www-form-urlencoded\r\n'
      'Content-Length: ${utf8.encode(body).length}\r\n'
      'Connection: close\r\n\r\n'
      '$body',
    );
    expect(submitted, contains('200 OK'));
    expect(await server.result, '丑小鸭');
  });

  test('扫码浏览器改写 Host 和 Origin 时仍可使用一次性二维码', () async {
    final server = await ScanSearchServer.start(
      address: InternetAddress.loopbackIPv4,
    );
    expect(server, isNotNull);
    addTearDown(() => server!.close());
    final uri = Uri.parse(server!.url);

    final page = await sendRawRequest(
      uri.port,
      'GET ${uri.path} HTTP/1.1\r\n'
      'Host: servicewechat.com\r\n'
      'Connection: close\r\n\r\n',
    );
    expect(page, contains('200 OK'));
    expect(page, contains('在电视上搜索'));

    final body = 'query=${Uri.encodeQueryComponent('丑小鸭')}';
    final submitted = await sendRawRequest(
      uri.port,
      'POST ${uri.path}/submit HTTP/1.1\r\n'
      'Host: servicewechat.com\r\n'
      'Origin: https://servicewechat.com\r\n'
      'Content-Type: application/x-www-form-urlencoded\r\n'
      'Content-Length: ${utf8.encode(body).length}\r\n'
      'Connection: close\r\n\r\n'
      '$body',
    );
    expect(submitted, contains('200 OK'));
    expect(await server.result, '丑小鸭');
  });
}
