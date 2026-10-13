import 'package:media_kit/media_kit.dart';

/// 按需初始化 media_kit（libmpv）。
///
/// libmpv 体积大、加载慢：电视端实测会阻塞主线程约 6.7 秒（Skipped 401
/// frames）。它只在播放时才需要，因此从启动路径移到这里，由播放页在构造
/// Player 之前调用。重复调用只生效一次。
bool _ready = false;

void ensureMediaKit() {
  if (_ready) return;
  _ready = true;
  MediaKit.ensureInitialized();
}
