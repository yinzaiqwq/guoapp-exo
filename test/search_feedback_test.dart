import 'package:duanju_app/catalog_sort.dart';
import 'package:duanju_app/core_bridge.dart';
import 'package:duanju_app/home_screen.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/remote_widgets.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';
import 'remote_test_helpers.dart';

class SearchFeedbackRepository extends FixtureRepository {
  SearchFeedbackRepository({this.empty = false, this.searchFails = false});
  final bool empty;
  final bool searchFails;
  final queries = <String>[];

  @override
  Future<CatalogPage> catalog(
    String source, {
    int page = 1,
    String query = '',
    String category = '',
    bool force = false,
  }) async {
    queries.add(query);
    if (query.isNotEmpty && searchFails) throw AppFailure('合成搜索失败');
    return CatalogPage(
      empty
          ? []
          : const [
              Drama(id: 'hongguotv:1', source: 'hongguotv', title: '合成月亮第二季'),
              Drama(id: 'hongguotv:2', source: 'hongguotv', title: '合成月亮第三季'),
            ],
      hasMore: false,
    );
  }
}

void main() {
  for (final scenario in ['empty', 'filtered', 'failure']) {
    testWidgets('TV search feedback distinguishes $scenario', (tester) async {
      tester.view.physicalSize = const Size(1920, 1080);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      SharedPreferences.setMockInitialValues({});
      final store = testStore(await SharedPreferences.getInstance());
      if (scenario == 'filtered') {
        await store.setCatalogView(const CatalogView(release: 'finished'));
      }
      final repository = SearchFeedbackRepository(
        empty: scenario == 'empty',
        searchFails: scenario == 'failure',
      );
      await tester.pumpWidget(
        televisionHost(
          child: HomeScreen(repository: repository, store: store),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('搜索'));
      await tester.pumpAndSettle();
      Navigator.of(
        tester.element(find.byType(TelevisionSearchDialog)),
      ).pop('月亮');
      await tester.pumpAndSettle();
      expect(repository.queries.last, '月亮');
      if (scenario == 'failure') {
        expect(find.text('暂时无法加载'), findsOneWidget);
        expect(find.textContaining('合成搜索失败'), findsOneWidget);
        expect(find.text('没有找到匹配的短剧'), findsNothing);
        expect(find.textContaining('已载入 0 部'), findsNothing);
      } else {
        expect(find.textContaining('站源：红果TV · 关键词：月亮'), findsOneWidget);
        if (scenario == 'empty') {
          expect(find.text('没有找到匹配的短剧'), findsOneWidget);
          expect(find.textContaining('已载入 0 部'), findsOneWidget);
          expect(find.text('结果被筛选条件隐藏'), findsNothing);
        } else {
          expect(find.text('结果被筛选条件隐藏'), findsOneWidget);
          expect(find.textContaining('已载入 2 部'), findsOneWidget);
          expect(find.text('没有找到匹配的短剧'), findsNothing);
          await tester.tap(find.text('清除完结状态筛选'));
          await tester.pumpAndSettle();
          expect(find.text('合成月亮第二季'), findsOneWidget);
          expect(find.text('合成月亮第三季'), findsOneWidget);
          expect(store.catalogView.release, isEmpty);
        }
      }
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
      store.dispose();
    });
  }
}
