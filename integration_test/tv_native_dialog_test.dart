import 'package:duanju_app/app_layout.dart';
import 'package:duanju_app/main.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/server_downloads.dart';
import 'package:duanju_app/server_downloads_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../test/fixtures.dart';

class _Repository extends FixtureRepository {
  @override
  Future<ServerDownloadList> serverDownloads() async =>
      const ServerDownloadList(
        dramas: [
          ServerDownloadDrama(
            folder: 'synthetic',
            title: '合成短剧',
            downloaded: 1,
            episodes: [
              ServerDownloadEpisode(index: 1, filename: 'synthetic.mp4'),
            ],
          ),
        ],
      );
}

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();
  testWidgets(
    'physical back dismisses once and native settings are reachable at TV density',
    (tester) async {
      expect(const bool.fromEnvironment('DISABLE_REMOTE_IMAGES'), isTrue);
      SharedPreferences.setMockInitialValues({});
      final store = LocalStore(await SharedPreferences.getInstance());
      final repository = _Repository();
      await tester.pumpWidget(
        DuanjuApp(repository: repository, store: store, television: true),
      );
      await tester.pumpAndSettle();
      final context = tester.element(find.byType(Scaffold).first);
      Navigator.of(context).push(
        MaterialPageRoute<void>(
          builder: (_) =>
              ServerDownloadsScreen(repository: repository, store: store),
        ),
      );
      await tester.pumpAndSettle();
      await tester.tap(find.byTooltip('删除整部'));
      await tester.pumpAndSettle();
      expect(find.byType(AlertDialog), findsOneWidget);
      debugPrint('TV_PHYSICAL_BACK_READY');
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(seconds: 5)),
      );
      await tester.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(find.byType(ServerDownloadsScreen), findsOneWidget);
      debugPrint('TV_PHYSICAL_BACK_PASS');
      debugPrint('TV_NATIVE_SETTINGS_READY');
      final result = await tester.runAsync(
        () => AppDevice.openNativePlayer(
          url: 'file:///data/local/tmp/danmaku-test.mp4',
          title: '合成弹幕设置验证',
          episodes: ['合成第1集', '合成第2集'],
          danmaku: '1000\t合成测试弹幕\n1000\t合成测试弹幕二',
        ).timeout(const Duration(minutes: 3)),
      );
      expect(result, isNotNull);
      debugPrint('TV_NATIVE_SETTINGS_RETURNED');
      debugPrint('TV_NATIVE_DIALOG_END_READY');
      final ended = await tester.runAsync(
        () => AppDevice.openNativePlayer(
          url: 'file:///data/local/tmp/danmaku-test.mp4',
          title: '合成结束清理验证',
          episodes: ['合成第1集'],
          position: const Duration(seconds: 113),
        ).timeout(const Duration(seconds: 30)),
      );
      expect(ended!.completed, isTrue);
      debugPrint('TV_NATIVE_DIALOG_END_PASS');
      await tester.pumpWidget(const SizedBox.shrink());
      store.dispose();
    },
  );
}
