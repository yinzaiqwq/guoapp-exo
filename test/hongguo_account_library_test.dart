import 'dart:async';

import 'package:duanju_app/core_bridge.dart';
import 'package:duanju_app/hongguo_account_screen.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';
import 'remote_test_helpers.dart';

class AccountLibraryRepository extends FixtureRepository {
  final accountRequests = <String>[];
  final pageItems = <String, List<Map<String, dynamic>>>{
    'likes:0': [row('101', '合成点赞一号')],
    'favorites:0': [row('201', '合成收藏一号')],
    'favorites:20': [row('202', '合成收藏二号')],
    'history:0': [
      row(
        '301',
        '合成账号历史',
        history: {
          'episodeIndex': 2,
          'positionSeconds': 37,
          'watchedAt': 1795190400000,
        },
      ),
    ],
  };
  final failures = <String, int>{};
  final morePages = <String>{};
  final wrongOffsets = <String, int>{};
  final totals = <String, int>{};
  final delayedPages = <String, Completer<void>>{};

  static Map<String, dynamic> row(
    String id,
    String title, {
    Map<String, dynamic>? history,
  }) => {
    'series': {
      'seriesId': id,
      'title': title,
      'intro': '',
      'cover': '',
      'episodeCount': 12,
    },
    'playable': true,
    'note': '',
    'history': ?history,
  };

  @override
  bool get supportsHongguoAccount => true;

  @override
  Future<Map<String, dynamic>> hongguoAccount(
    String command, {
    String sessionId = '',
    int offset = 0,
    String? profileId,
  }) async {
    accountRequests.add('$command:$offset');
    if (command == 'status') {
      return {'authenticated': true, 'displayName': '合成账号', 'persistent': true};
    }
    if (command == 'logout') return {};
    final page = '$command:$offset';
    final pending = delayedPages[page];
    if (pending != null) await pending.future;
    if ((failures[page] ?? 0) > 0) {
      failures[page] = failures[page]! - 1;
      throw AppFailure('合成片单读取失败', code: 'hongguo_library_unavailable');
    }
    return {
      'items': pageItems[page] ?? <Map<String, dynamic>>[],
      'offset': wrongOffsets[page] ?? offset,
      'nextOffset': offset + 20,
      'hasMore': morePages.contains(page),
      'unsupportedCount': 0,
      if (totals.containsKey(page)) 'total': totals[page],
    };
  }
}

void main() {
  Future<LocalStore> createStore() async {
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);
    return store;
  }

  void viewport(WidgetTester tester, {bool television = false}) {
    tester.view.physicalSize = television
        ? const Size(960, 540)
        : const Size(800, 1000);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
  }

  Future<void> openAccount(
    WidgetTester tester,
    AccountLibraryRepository repository,
    LocalStore store, {
    bool television = false,
  }) async {
    viewport(tester, television: television);
    final screen = HongguoAccountScreen(repository: repository, store: store);
    await tester.pumpWidget(
      television ? televisionHost(child: screen) : MaterialApp(home: screen),
    );
    await tester.pumpAndSettle();
  }

  Future<void> selectLibrary(WidgetTester tester, String command) async {
    final chip = find.byKey(ValueKey('hongguo-account-$command'));
    await tester.ensureVisible(chip);
    await tester.tap(chip);
    await tester.pumpAndSettle();
  }

  Future<void> press(WidgetTester tester, String label) async {
    final button = find.text(label);
    await tester.ensureVisible(button);
    await tester.tap(button);
    await tester.pumpAndSettle();
  }

  testWidgets('account libraries retain distinct loaded lists and pagination', (
    tester,
  ) async {
    final repository = AccountLibraryRepository()..morePages.add('favorites:0');
    final store = await createStore();
    await openAccount(tester, repository, store);
    expect(repository.accountRequests, ['status:0', 'likes:0']);
    expect(find.text('合成点赞一号'), findsOneWidget);
    await selectLibrary(tester, 'favorites');
    expect(find.text('合成收藏一号'), findsOneWidget);
    expect(find.text('合成点赞一号'), findsNothing);
    await press(tester, '加载更多收藏');
    expect(repository.accountRequests.last, 'favorites:20');
    await selectLibrary(tester, 'history');
    expect(find.text('合成账号历史'), findsOneWidget);
    expect(find.text('合成收藏一号'), findsNothing);
    await selectLibrary(tester, 'likes');
    expect(find.text('合成点赞一号'), findsOneWidget);
    expect(repository.accountRequests.where((value) => value == 'likes:0'), [
      'likes:0',
    ]);
    await selectLibrary(tester, 'favorites');
    expect(
      repository.accountRequests.where((value) => value == 'favorites:0'),
      ['favorites:0'],
    );
    expect(find.textContaining('我的收藏'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('a failed favorites refresh preserves the account and old rows', (
    tester,
  ) async {
    final repository = AccountLibraryRepository();
    final store = await createStore();
    await openAccount(tester, repository, store);
    await selectLibrary(tester, 'favorites');
    repository.failures['favorites:0'] = 1;
    await press(tester, '刷新片单');
    expect(find.text('合成片单读取失败'), findsOneWidget);
    expect(find.text('合成收藏一号'), findsOneWidget);
    expect(find.text('退出红果账号'), findsOneWidget);
    expect(find.text('获取登录二维码'), findsNothing);
    repository.pageItems['favorites:0'] = [
      AccountLibraryRepository.row('203', '合成收藏刷新成功'),
    ];
    await press(tester, '重试读取');
    expect(find.text('合成收藏刷新成功'), findsOneWidget);
    expect(find.text('合成片单读取失败'), findsNothing);
    await selectLibrary(tester, 'likes');
    expect(find.text('合成点赞一号'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets(
    'a failed first history read can be retried without signing out',
    (tester) async {
      final repository = AccountLibraryRepository()..failures['history:0'] = 1;
      final store = await createStore();
      await openAccount(tester, repository, store);
      await selectLibrary(tester, 'history');
      expect(find.text('合成片单读取失败'), findsOneWidget);
      expect(find.text('退出红果账号'), findsOneWidget);
      await press(tester, '重试读取');
      expect(find.text('合成账号历史'), findsOneWidget);
      expect(find.text('合成片单读取失败'), findsNothing);
      expect(
        repository.accountRequests.where((value) => value == 'history:0'),
        ['history:0', 'history:0'],
      );
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );

  testWidgets('cloud reads leave local follow and watch progress unchanged', (
    tester,
  ) async {
    final repository = AccountLibraryRepository();
    final store = await createStore();
    const local = Drama(
      id: 'hongguotv:901',
      source: 'hongguotv',
      sourceId: '901',
      title: '合成本机记录',
      episodes: 12,
    );
    await store.toggleFavorite(local);
    await store.saveWatch(
      WatchEntry(
        drama: local,
        episode: 7,
        position: 43,
        duration: 60,
        updatedAt: DateTime.utc(2026, 11, 20),
      ),
    );
    final beforeWatch = store.history.single.toJson();
    final beforeFollow = store.following(local.id)!.toJson();
    await openAccount(tester, repository, store);
    await selectLibrary(tester, 'favorites');
    await selectLibrary(tester, 'history');
    expect(store.history, hasLength(1));
    expect(store.history.single.toJson(), beforeWatch);
    expect(store.following(local.id)!.toJson(), beforeFollow);
    expect(store.isFavorite('hongguotv:201'), isFalse);
    expect(store.watched('hongguotv:301'), isNull);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('late favorites results do not replace the selected history', (
    tester,
  ) async {
    final repository = AccountLibraryRepository();
    final pending = Completer<void>();
    repository.delayedPages['favorites:0'] = pending;
    final store = await createStore();
    await openAccount(tester, repository, store);
    final favorites = find.byKey(const ValueKey('hongguo-account-favorites'));
    await tester.ensureVisible(favorites);
    await tester.tap(favorites);
    await tester.pump();
    expect(repository.accountRequests.last, 'favorites:0');
    await selectLibrary(tester, 'history');
    expect(find.text('合成账号历史'), findsOneWidget);
    pending.complete();
    await tester.pumpAndSettle();
    expect(find.text('合成账号历史'), findsOneWidget);
    expect(find.text('合成收藏一号'), findsNothing);
    await selectLibrary(tester, 'favorites');
    expect(find.text('合成收藏一号'), findsOneWidget);
    expect(
      repository.accountRequests.where((value) => value == 'favorites:0'),
      ['favorites:0'],
    );
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('invalid pagination retains its previous rows and retry offset', (
    tester,
  ) async {
    final repository = AccountLibraryRepository()
      ..morePages.add('favorites:0')
      ..wrongOffsets['favorites:20'] = 999;
    final store = await createStore();
    await openAccount(tester, repository, store);
    await selectLibrary(tester, 'favorites');
    await press(tester, '加载更多收藏');
    expect(find.textContaining('已有列表已保留'), findsOneWidget);
    expect(find.text('合成收藏一号'), findsOneWidget);
    expect(find.text('合成收藏二号'), findsNothing);
    repository.wrongOffsets.remove('favorites:20');
    await press(tester, '重试读取');
    expect(
      repository.accountRequests.where((value) => value == 'favorites:20'),
      ['favorites:20', 'favorites:20'],
    );
    expect(find.text('合成收藏二号'), findsOneWidget);
    expect(find.textContaining('已有列表已保留'), findsNothing);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets('a later page can omit the previously reported total', (
    tester,
  ) async {
    final repository = AccountLibraryRepository()
      ..morePages.add('favorites:0')
      ..totals['favorites:0'] = 42;
    final store = await createStore();
    await openAccount(tester, repository, store);
    await selectLibrary(tester, 'favorites');
    expect(find.textContaining('共 42 条'), findsOneWidget);
    await press(tester, '加载更多收藏');
    expect(find.textContaining('共 42 条'), findsOneWidget);
    expect(tester.takeException(), isNull);
    await tester.pumpWidget(const SizedBox.shrink());
  });

  testWidgets(
    'a successful refresh without a total clears the previously reported total',
    (tester) async {
      final repository = AccountLibraryRepository()
        ..morePages.add('favorites:0')
        ..totals['favorites:0'] = 42;
      final store = await createStore();
      await openAccount(tester, repository, store);
      await selectLibrary(tester, 'favorites');
      expect(find.textContaining('共 42 条'), findsOneWidget);
      await press(tester, '加载更多收藏');
      expect(find.textContaining('共 42 条'), findsOneWidget);
      repository.totals.remove('favorites:0');
      await press(tester, '刷新片单');
      expect(find.textContaining('共 42 条'), findsNothing);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );

  testWidgets(
    'history shows the one-based episode and seconds without playing',
    (tester) async {
      final repository = AccountLibraryRepository();
      final store = await createStore();
      await openAccount(tester, repository, store);
      await selectLibrary(tester, 'history');
      expect(find.text('手机观看到第 3 集 · 0:37'), findsOneWidget);
      expect(repository.detailCalls, 0);
      expect(store.history, isEmpty);
      expect(tester.takeException(), isNull);
      await tester.pumpWidget(const SizedBox.shrink());
    },
  );
  testWidgets('television layout exposes every library without overflow', (
    tester,
  ) async {
    final repository = AccountLibraryRepository();
    final store = await createStore();
    await openAccount(tester, repository, store, television: true);
    for (final command in ['likes', 'favorites', 'history']) {
      await selectLibrary(tester, command);
      final selected = tester.widget<ChoiceChip>(
        find.byKey(ValueKey('hongguo-account-$command')),
      );
      expect(selected.selected, isTrue);
      expect(selected.onSelected, isNotNull);
      expect(tester.takeException(), isNull);
    }
    await tester.pumpWidget(const SizedBox.shrink());
  });
}
