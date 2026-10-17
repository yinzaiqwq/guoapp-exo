import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/server_downloads.dart';
import 'package:duanju_app/server_downloads_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'fixtures.dart';

class _ServerDownloadRepository extends FixtureRepository {
  var list = const ServerDownloadList(
    dramas: [
      ServerDownloadDrama(
        folder: 'synthetic-show',
        title: '合成短剧',
        seriesId: 'series-100',
        downloaded: 1,
        episodes: [
          ServerDownloadEpisode(index: 1, filename: 'episode-1.mp4'),
        ],
      ),
    ],
  );
  final deletions = <String>[];

  @override
  Future<ServerDownloadList> serverDownloads() async => list;

  @override
  Future<String> deleteServerDownload(
    String folder, {
    String episode = '',
  }) async {
    deletions.add('$folder:$episode');
    list = const ServerDownloadList();
    return '已删除';
  }
}

void main() {
  test('server download list keeps series and episode playback identity', () {
    final list = ServerDownloadList.fromJson({
      'dramas': [
        {
          'folder': 'synthetic-show',
          'title': '合成短剧',
          'seriesId': 'series-100',
          'episodes': [
            {
              'index': 1,
              'filename': 'episode-1.mp4',
              'sizeHuman': '1 MB',
              'hasDanmaku': true,
            },
          ],
        },
      ],
    });

    expect(list.dramas.single.seriesId, 'series-100');
    expect(list.dramas.single.episodes.single.filename, 'episode-1.mp4');
    expect(list.dramas.single.episodes.single.hasDanmaku, isTrue);
  });

  testWidgets('server download screen exposes playback and whole deletion', (
    tester,
  ) async {
    tester.view.physicalSize = const Size(1080, 1920);
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    SharedPreferences.setMockInitialValues({});
    final repository = _ServerDownloadRepository();
    final store = LocalStore(await SharedPreferences.getInstance());
    addTearDown(store.dispose);

    await tester.pumpWidget(
      MaterialApp(
        home: ServerDownloadsScreen(repository: repository, store: store),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.byTooltip('从第 1 集播放'), findsOneWidget);
    expect(find.byTooltip('删除整部'), findsOneWidget);
    await tester.tap(find.byTooltip('删除整部'));
    await tester.pumpAndSettle();
    expect(find.text('删除服务器上的 《合成短剧》全部？'), findsOneWidget);
    await tester.tap(find.text('确认删除'));
    await tester.pumpAndSettle();

    expect(repository.deletions, ['synthetic-show:']);
    expect(find.text('服务器上还没有已下载的剧集'), findsOneWidget);
  });
}
