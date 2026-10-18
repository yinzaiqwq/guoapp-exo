import 'dart:async';

import 'package:duanju_app/app_layout.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/player_screen.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'player_fixtures.dart';

class TranscodeRepository extends RouteRepository {
  TranscodeRepository(this.local, {this.serverDownloaded = false});
  final bool local;
  final bool serverDownloaded;

  @override
  PlaybackPlan plan(bool alternate) => PlaybackPlan(
    url: local ? '/synthetic/local.mp4' : 'http://127.0.0.1/synthetic.mp4',
    local: local,
    serverDownloaded: serverDownloaded,
    quality: 1080,
    qualities: const [1080, 720],
  );
}

void main() {
  for (final scenario in [
    (
      source: 'hongguotv',
      local: false,
      direct: false,
      server: false,
      transcode: true,
    ),
    (
      source: 'hongguotv',
      local: true,
      direct: false,
      server: false,
      transcode: false,
    ),
    (
      source: 'hongguo',
      local: false,
      direct: false,
      server: false,
      transcode: false,
    ),
    (
      source: 'hongguotv',
      local: false,
      direct: true,
      server: true,
      transcode: false,
    ),
    (
      source: 'hongguotv',
      local: false,
      direct: false,
      server: true,
      transcode: false,
    ),
  ]) {
    testWidgets('native transcode wait scope $scenario', (tester) async {
      debugDefaultTargetPlatformOverride = TargetPlatform.android;
      SharedPreferences.setMockInitialValues({});
      final store = LocalStore(await SharedPreferences.getInstance());
      final repository = TranscodeRepository(
        scenario.local,
        serverDownloaded: scenario.server,
      );
      final detail = await repository.detail(
        Drama(
          id: '${scenario.source}:100',
          source: scenario.source,
          title: '合成剧',
        ),
      );
      final result = Completer<Map<String, Object>>();
      final calls = <Map>[];
      final messenger =
          TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
      messenger.setMockMethodCallHandler(AppDevice.channel, (call) async {
        if (call.method != 'openNativePlayer') return null;
        calls.add(call.arguments as Map);
        return result.future;
      });
      try {
        await tester.pumpWidget(
          MaterialApp(
            home: PlayerScreen(
              detail: detail,
              initialIndex: 0,
              repository: repository,
              store: store,
              nativePlayback: true,
              planResolver: scenario.direct
                  ? (_) async => repository.plan(false)
                  : null,
              playerFactory: () => Player(platformPlayer: ScriptedPlayer()),
              videoBuilder: (controls) => controls,
            ),
          ),
        );
        for (var i = 0; i < 50; i++) {
          await tester.pump(const Duration(milliseconds: 10));
        }
        expect(calls, hasLength(1));
        expect(calls.single['serverTranscode'], scenario.transcode);
        expect(calls.single['quality'], 1080);
        expect(tester.takeException(), isNull);
      } finally {
        result.complete({'positionMs': 0, 'completed': false});
        for (var i = 0; i < 20; i++) {
          await tester.pump(const Duration(milliseconds: 10));
        }
        await tester.pumpWidget(const SizedBox.shrink());
        messenger.setMockMethodCallHandler(AppDevice.channel, null);
        debugDefaultTargetPlatformOverride = null;
        store.dispose();
      }
    });
  }
}
