import 'dart:async';

import 'package:duanju_app/app_layout.dart';
import 'package:duanju_app/danmaku_models.dart';
import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/player_screen.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'player_fixtures.dart';

class CachedDanmakuRepository extends RouteRepository {
  CachedDanmakuRepository({this.paged = false});
  final bool paged;
  int danmakuCalls = 0;

  @override
  PlaybackPlan plan(bool alternate) => const PlaybackPlan(
    url: 'http://synthetic.test/stream/cache/file',
    serverDownloaded: true,
    session: 'synthetic-cache-session',
    danmakuId: '999',
    danmakuSource: 'hongguotv',
    quality: 1080,
    qualities: [1080],
  );

  @override
  Future<DanmakuPage> danmaku(
    PlaybackPlan plan, {
    required int startMs,
    required int durationMs,
  }) async {
    danmakuCalls++;
    if (paged) {
      return DanmakuPage(
        episodeId: plan.danmakuId,
        startMs: startMs,
        nextMs: startMs < 60000 ? startMs + 30000 : durationMs,
        items: [
          DanmakuItem(
            id: 'comment-$startMs',
            text: '合成缓存弹幕',
            timeMs: startMs + 500,
          ),
        ],
      );
    }
    return DanmakuPage(
      episodeId: plan.danmakuId,
      startMs: startMs,
      nextMs: durationMs,
      items: const [DanmakuItem(id: 'comment-1', text: '合成缓存弹幕', timeMs: 500)],
    );
  }
}

void main() {
  for (final paged in [false, true]) {
    testWidgets(
      'server cached playback pushes danmaku to native player paged=$paged',
      (tester) async {
        debugDefaultTargetPlatformOverride = TargetPlatform.android;
        SharedPreferences.setMockInitialValues({});
        final store = LocalStore(await SharedPreferences.getInstance());
        final repository = CachedDanmakuRepository(paged: paged);
        final detail = await repository.detail(
          const Drama(
            id: 'hongguotv:100',
            source: 'hongguotv',
            sourceId: '100',
            title: '合成缓存剧',
          ),
        );
        final pending = Completer<Map<String, Object>>();
        final opened = <Map>[];
        final payloads = <String>[];
        final messenger =
            TestDefaultBinaryMessengerBinding.instance.defaultBinaryMessenger;
        messenger.setMockMethodCallHandler(AppDevice.channel, (call) async {
          if (call.method == 'openNativePlayer') {
            opened.add(call.arguments as Map);
            return pending.future;
          }
          if (call.method == 'updateNativeDanmaku') {
            payloads.add((call.arguments as Map)['danmaku'] as String);
          }
          return null;
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
                playerFactory: () => Player(platformPlayer: ScriptedPlayer()),
                videoBuilder: (controls) => controls,
              ),
            ),
          );
          for (var i = 0; i < 50; i++) {
            await tester.pump(const Duration(milliseconds: 10));
          }
          expect(opened, hasLength(1));
          expect(opened.single['serverTranscode'], false);
          expect(repository.danmakuCalls, paged ? 3 : 1);
          expect(payloads, [
            paged
                ? '500\t合成缓存弹幕\n30500\t合成缓存弹幕\n60500\t合成缓存弹幕\n'
                : '500\t合成缓存弹幕\n',
          ]);
          expect(tester.takeException(), isNull);
        } finally {
          pending.complete({
            'positionMs': 23000,
            'durationMs': 120000,
            'completed': false,
          });
          for (var i = 0; i < 20; i++) {
            await tester.pump(const Duration(milliseconds: 10));
          }
          await tester.pumpWidget(const SizedBox.shrink());
          messenger.setMockMethodCallHandler(AppDevice.channel, null);
          debugDefaultTargetPlatformOverride = null;
          store.dispose();
        }
      },
    );
  }
}
