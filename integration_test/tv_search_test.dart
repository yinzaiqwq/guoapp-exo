import 'dart:io';

import 'package:duanju_app/core_bridge.dart';
import 'package:duanju_app/catalog_sort.dart';
import 'package:duanju_app/home_screen.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/remote_widgets.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../test/remote_test_helpers.dart';

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  for (final release in ['', 'finished', 'ongoing']) {
    testWidgets('TV Chinese search with release filter "$release"', (
      tester,
    ) async {
      expect(const bool.fromEnvironment('DISABLE_REMOTE_IMAGES'), isTrue);
      SharedPreferences.setMockInitialValues({});
      final store = LocalStore(await SharedPreferences.getInstance());
      final repository = NativeRepository(background: true)..access = store;
      await tester.runAsync(repository.initialize);
      final result = await tester.runAsync(
        () => repository.catalog('hongguotv', query: '月亮'),
      );
      expect(result, isNotNull);
      final titles = result!.items.map((drama) => drama.title).toList();
      debugPrint('TV_SEARCH_NATIVE_COUNT=${titles.length}');
      expect(titles.any((title) => title.contains('假装在月亮心口撒个娇第二季')), isTrue);
      expect(titles.any((title) => title.contains('假装在月亮心口撒个娇第三季')), isTrue);
      await store.setCatalogView(CatalogView(release: release));
      await tester.pumpWidget(
        televisionHost(
          child: HomeScreen(repository: repository, store: store),
        ),
      );
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(seconds: 5)),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('搜索'));
      await tester.pumpAndSettle();
      expect(find.byType(TelevisionSearchDialog), findsOneWidget);
      if (release.isEmpty) {
        await tester.tap(find.text('手机扫码输入'));
        await tester.pump();
        await tester.runAsync(
          () => Future<void>.delayed(const Duration(seconds: 1)),
        );
        await tester.pumpAndSettle();
        final address = tester
            .widgetList<SelectableText>(find.byType(SelectableText))
            .map((text) => text.data ?? '')
            .firstWhere((text) => text.startsWith('http://'));
        await tester.runAsync(() async {
          final client = HttpClient();
          try {
            final uri = Uri.parse('$address/submit');
            final request = await client.postUrl(uri);
            request.headers.contentType = ContentType(
              'application',
              'x-www-form-urlencoded',
              charset: 'utf-8',
            );
            request.headers.set('Origin', uri.origin);
            final body = Uri(queryParameters: {'query': '月亮'}).query;
            request.contentLength = body.length;
            request.write(body);
            final response = await request.close();
            expect(response.statusCode, HttpStatus.ok);
            await response.drain<void>();
          } finally {
            client.close(force: true);
          }
        });
      } else {
        Navigator.of(
          tester.element(find.byType(TelevisionSearchDialog)),
        ).pop('月亮');
      }
      await tester.pump();
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(seconds: 8)),
      );
      await tester.pumpAndSettle();
      if (release.isNotEmpty) {
        expect(find.text('结果被筛选条件隐藏'), findsOneWidget);
        final clear = find.text('清除完结状态筛选');
        for (var step = 0; step < 40; step++) {
          if (Focus.of(tester.element(clear)).hasFocus) break;
          await tester.sendKeyEvent(LogicalKeyboardKey.tab);
          await tester.pump();
        }
        expect(Focus.of(tester.element(clear)).hasFocus, isTrue);
        await tester.sendKeyEvent(LogicalKeyboardKey.select);
        await tester.pumpAndSettle();
        expect(store.catalogView.release, isEmpty);
        final restored = LocalStore(await SharedPreferences.getInstance());
        expect(restored.catalogView.release, isEmpty);
        restored.dispose();
      }
      expect(find.textContaining('假装在月亮心口撒个娇'), findsWidgets);
      expect(find.text('没有找到匹配的短剧'), findsNothing);
      debugPrint('TV_SEARCH_CHINESE_UI_PASS');
      await tester.pumpWidget(const SizedBox.shrink());
      store.dispose();
    });
  }
}
