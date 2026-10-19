import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/ranking_models.dart';
import 'package:duanju_app/rankings_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'remote_test_helpers.dart';
import 'fixtures.dart';

class TvRankingRepository extends FixtureRepository {
  String requested = '';

  @override
  Future<List<RankingBoard>> rankingBoards() async => const [
    RankingBoard(id: 'hongguotv-hot', source: 'hongguotv', name: '漫剧热播榜'),
  ];

  @override
  Future<RankingPage> rankings(
    String board, {
    int page = 1,
    bool force = false,
  }) async {
    requested = board;
    return RankingPage(
      items: [
        for (var rank = 1; rank <= 99; rank++)
          RankingItem(
            rank,
            Drama(
              id: 'hongguotv:$rank',
              source: 'hongguotv',
              title: '合成榜单剧 $rank',
            ),
          ),
      ],
    );
  }
}

void main() {
  test(
    'TV ranking IDs authorize the TV source rather than the removed source',
    () {
      for (final key in ['recommend', 'hot', 'new']) {
        expect(RankingBoard.sourceForID('hongguotv-$key'), 'hongguotv');
      }
    },
  );

  testWidgets('TV rankings remain reachable and scroll using the remote', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(960, 540);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    final repository = TvRankingRepository();
    await tester.pumpWidget(
      televisionHost(
        child: RankingsScreen(
          repository: repository,
          store: store,
          initialGroup: 'hongguotv',
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(repository.requested, 'hongguotv-hot');
    focusRemote(tester, find.byKey(const ValueKey('rank-1-hongguotv:1')));
    await tester.pumpAndSettle();
    for (var step = 0; step < 14; step++) {
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
      await tester.pumpAndSettle();
    }
    expect(
      FocusManager.instance.primaryFocus?.debugLabel,
      'remote-15-hongguotv:15',
    );
    expect(find.text('合成榜单剧 15'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
