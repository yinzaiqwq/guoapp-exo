import 'dart:async';

import 'package:duanju_app/app_layout.dart';
import 'package:duanju_app/downloads_screen.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/player_screen.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'downloads_test.dart' show DownloadRepository;
import 'fixtures.dart';
import 'player_fixtures.dart';
import 'remote_test_helpers.dart';

void main() {
  Future<void> pumpOperations(WidgetTester tester) async {
    for (var i = 0; i < 50; i++) {
      await tester.pump(const Duration(milliseconds: 10));
    }
  }

  for (final outcome in ['exit', 'episode', 'completed', 'failure']) {
    testWidgets('native route $outcome preserves navigation and history', (
      tester,
    ) async {
      debugDefaultTargetPlatformOverride = TargetPlatform.android;
      SharedPreferences.setMockInitialValues({});
      final store = LocalStore(await SharedPreferences.getInstance());
      final repository = RouteRepository();
      final detail = await repository.detail(FixtureRepository.free);
      final pending = <Completer<Map<String, Object>>>[];
      final messenger =
          TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
      messenger.setMockMethodCallHandler(AppDevice.channel, (call) async {
        if (call.method != 'openNativePlayer') return null;
        final result = Completer<Map<String, Object>>();
        pending.add(result);
        return result.future;
      });
      try {
        await tester.pumpWidget(
          MaterialApp(
            home: Builder(
              builder: (context) => Scaffold(
                body: TextButton(
                  onPressed: () => Navigator.of(context).push(
                    MaterialPageRoute<void>(
                      builder: (_) => PlayerScreen(
                        detail: detail,
                        initialIndex: 0,
                        repository: repository,
                        store: store,
                        nativePlayback: true,
                        playerFactory: () =>
                            Player(platformPlayer: ScriptedPlayer()),
                        videoBuilder: (controls) => controls,
                      ),
                    ),
                  ),
                  child: const Text('合成剧集列表'),
                ),
              ),
            ),
          ),
        );
        await tester.tap(find.text('合成剧集列表'));
        await pumpOperations(tester);
        expect(pending, hasLength(1));
        if (outcome == 'failure') {
          pending.first.completeError(const NativePlaybackFailure('合成解码失败'));
        } else {
          pending.first.complete({
            'positionMs': 23000,
            'durationMs': 120000,
            'completed': outcome == 'completed',
            if (outcome == 'episode') 'episodeIndex': 1,
          });
        }
        await pumpOperations(tester);
        if (outcome == 'exit') {
          expect(find.byType(PlayerScreen), findsNothing);
          expect(find.text('合成剧集列表'), findsOneWidget);
          expect(store.watched(detail.drama.id)!.position, 23);
        } else if (outcome == 'failure') {
          expect(find.byType(PlayerScreen), findsOneWidget);
          expect(find.textContaining('合成解码失败'), findsWidgets);
          expect(find.text('正在准备播放'), findsNothing);
        } else {
          expect(find.byType(PlayerScreen), findsOneWidget);
          expect(pending, hasLength(2));
          expect(repository.requestedEpisodes.last, 2);
          pending.last.complete({
            'positionMs': 5000,
            'durationMs': 120000,
            'completed': false,
          });
          await pumpOperations(tester);
          expect(find.byType(PlayerScreen), findsNothing);
          expect(store.watched(detail.drama.id)!.episode, 2);
        }
        expect(tester.takeException(), isNull);
      } finally {
        for (final result in pending) {
          if (!result.isCompleted) {
            result.complete({'positionMs': 0, 'completed': false});
          }
        }
        await tester.pumpWidget(const SizedBox.shrink());
        await pumpOperations(tester);
        messenger.setMockMethodCallHandler(AppDevice.channel, null);
        debugDefaultTargetPlatformOverride = null;
        store.dispose();
      }
    });
  }

  testWidgets(
    'TV download search releases focus with down and back only dismisses confirmation',
    (tester) async {
      tester.view.physicalSize = const Size(1920, 1080);
      tester.view.devicePixelRatio = 2;
      addTearDown(tester.view.resetPhysicalSize);
      addTearDown(tester.view.resetDevicePixelRatio);
      SharedPreferences.setMockInitialValues({});
      final store = LocalStore(await SharedPreferences.getInstance());
      final repository = DownloadRepository()
        ..jobs = [DownloadRepository().job(1, 'completed')];
      await tester.pumpWidget(
        televisionHost(
          child: DownloadsScreen(repository: repository, store: store),
        ),
      );
      await tester.pumpAndSettle();
      final input = find.byType(TextField).first;
      await tester.tap(input);
      await tester.pumpAndSettle();
      expect(tester.widget<TextField>(input).focusNode!.hasFocus, isTrue);
      await tester.sendKeyEvent(LogicalKeyboardKey.arrowDown);
      await tester.pumpAndSettle();
      expect(tester.widget<TextField>(input).focusNode!.hasFocus, isFalse);
      await tester.tap(find.text('删除整部'));
      await tester.pumpAndSettle();
      expect(find.byType(AlertDialog), findsOneWidget);
      await tester.binding.handlePopRoute();
      await tester.pumpAndSettle();
      expect(find.byType(AlertDialog), findsNothing);
      expect(find.byType(DownloadsScreen), findsOneWidget);
      expect(repository.commands, isEmpty);
      await tester.pumpWidget(const SizedBox.shrink());
      store.dispose();
    },
  );
}
