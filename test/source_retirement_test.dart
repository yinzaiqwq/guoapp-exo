import 'dart:convert';

import 'package:duanju_app/local_store.dart';
import 'package:duanju_app/models.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  test(
    'retired Hongguo is not selectable and defaults migrate to Hongguo TV',
    () async {
      SharedPreferences.setMockInitialValues({'source': 'hongguo'});
      final store = LocalStore(await SharedPreferences.getInstance());
      expect(SourceSite.values.any((site) => site.id == 'hongguo'), isFalse);
      expect(store.sources.map((site) => site.id), ['hongguotv']);
      expect(store.source, 'hongguotv');
      expect(SourceSite.byId('unknown').id, 'hongguotv');
      await expectLater(store.setSource('hongguo'), throwsStateError);
      store.dispose();
    },
  );

  test(
    'retiring the source preserves existing history and favorites',
    () async {
      const drama = Drama(id: 'hongguo:123', source: 'hongguo', title: '合成旧记录');
      final history = jsonEncode([
        WatchEntry(
          drama: drama,
          episode: 2,
          position: 23,
          duration: 120,
          updatedAt: DateTime(2026, 10, 18),
        ).toJson(),
      ]);
      SharedPreferences.setMockInitialValues({
        'source': 'hongguo',
        'favorites': jsonEncode([drama.toJson()]),
        'history': history,
      });
      final preferences = await SharedPreferences.getInstance();
      final store = LocalStore(preferences);
      expect(store.source, 'hongguotv');
      expect(store.watched(drama.id)!.position, 23);
      expect(store.isFavorite(drama.id), isTrue);
      expect(store.history.single.drama.id, drama.id);
      expect(preferences.getString('history'), history);
      store.dispose();
    },
  );
}
