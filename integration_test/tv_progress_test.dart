import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/downloads_screen.dart';
import 'package:duanju_app/models.dart';
import 'package:duanju_app/player_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:media_kit/media_kit.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../test/fixtures.dart';
import '../test/downloads_test.dart' show DownloadRepository;
import '../test/player_fixtures.dart';
import '../test/remote_test_helpers.dart';

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('TV collection delete accepts Android remote confirmation', (
    tester,
  ) async {
    SharedPreferences.setMockInitialValues({});
    final store = LocalStore(await SharedPreferences.getInstance());
    final repository = DownloadRepository()
      ..jobs = [
        DownloadRepository().job(1, 'completed'),
        DownloadRepository().job(2, 'paused'),
      ];
    await tester.pumpWidget(
      televisionHost(
        child: DownloadsScreen(repository: repository, store: store),
      ),
    );
    await tester.pumpAndSettle();
    final label = find.text('删除整部');
    for (var i = 0; i < 20; i++) {
      if (Focus.of(tester.element(label)).hasFocus) break;
      await tester.sendKeyEvent(LogicalKeyboardKey.tab);
      await tester.pump();
    }
    expect(Focus.of(tester.element(label)).hasFocus, isTrue);
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowLeft);
    await tester.pump();
    await tester.sendKeyEvent(LogicalKeyboardKey.arrowRight);
    await tester.pump();
    expect(Focus.of(tester.element(label)).hasFocus, isTrue);
    debugPrint('TV_DELETE_REMOTE_READY');
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(seconds: 4)),
    );
    await tester.pumpAndSettle();
    expect(find.textContaining('共 2 项'), findsOneWidget);
    debugPrint('TV_DELETE_CONFIRM_READY');
    await tester.runAsync(
      () => Future<void>.delayed(const Duration(seconds: 4)),
    );
    await tester.pumpAndSettle();
    expect(repository.commands, containsAll(['remove:1', 'remove:2']));
    expect(repository.jobs, isEmpty);
    debugPrint('TV_DELETE_REMOTE_PASS');
    await tester.pumpWidget(const SizedBox.shrink());
    store.dispose();
  });

  testWidgets('TV native progress persists and is passed back on reopen', (
    tester,
  ) async {
    expect(const bool.fromEnvironment('DISABLE_REMOTE_IMAGES'), isTrue);
    SharedPreferences.setMockInitialValues({});
    final preferences = await SharedPreferences.getInstance();
    final repository = RouteRepository();
    final detail = await repository.detail(FixtureRepository.free);
    var store = LocalStore(preferences);

    Future<void> play(double position) async {
      final player = ScriptedPlayer();
      await tester.pumpWidget(
        televisionHost(
          child: PlayerScreen(
            detail: detail,
            initialIndex: 1,
            initialPosition: position,
            repository: repository,
            store: store,
            planResolver: (_) async => const PlaybackPlan(
              url: '/data/local/tmp/danmaku-test.mp4',
              local: true,
            ),
            playerFactory: () => Player(platformPlayer: player),
            videoBuilder: (controls) => controls,
          ),
        ),
      );
      await tester.pump();
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(seconds: 12)),
      );
      await tester.pump();
      final entry = store.watched(detail.drama.id)!;
      expect(entry.episode, 2);
      expect(entry.position, greaterThan(position + 2));
      expect(entry.duration, greaterThan(120));
      debugPrint('TV_NATIVE_BACK');
      await tester.runAsync(
        () => Future<void>.delayed(const Duration(seconds: 4)),
      );
      await tester.pump();
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pump();
    }

    await play(20);
    final saved = store.watched(detail.drama.id)!.position;
    expect(store.history.first.drama.id, detail.drama.id);
    store.dispose();
    store = LocalStore(preferences);
    expect(store.watched(detail.drama.id)!.position, saved);
    await play(saved);
    expect(store.watched(detail.drama.id)!.position, greaterThan(saved + 2));
    debugPrint('TV_NATIVE_RESUME_PASS');
    store.dispose();
  });
}
