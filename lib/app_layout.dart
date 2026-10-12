import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

export 'app_build.dart';

const appVersion = '0.2.65';

ThemeData televisionTheme(ThemeData theme) {
  final colors = theme.colorScheme;
  final focusSide = WidgetStateProperty.resolveWith<BorderSide?>(
    (states) => states.contains(WidgetState.focused)
        ? BorderSide(color: colors.primary, width: 3)
        : null,
  );
  final focusBackground = WidgetStateProperty.resolveWith<Color?>(
    (states) =>
        states.contains(WidgetState.focused) ? colors.primaryContainer : null,
  );
  final button = ButtonStyle(
    side: focusSide,
    minimumSize: const WidgetStatePropertyAll(Size(52, 48)),
    textStyle: const WidgetStatePropertyAll(TextStyle(fontSize: 17)),
  );
  return theme.copyWith(
    focusColor: colors.primaryContainer,
    progressIndicatorTheme: const ProgressIndicatorThemeData(
      color: Color(0xFFFF765F),
      linearTrackColor: Colors.white24,
    ),
    iconButtonTheme: IconButtonThemeData(
      style: button.copyWith(backgroundColor: focusBackground),
    ),
    filledButtonTheme: FilledButtonThemeData(style: button),
    outlinedButtonTheme: OutlinedButtonThemeData(style: button),
    textButtonTheme: TextButtonThemeData(
      style: button.copyWith(backgroundColor: focusBackground),
    ),
    inputDecorationTheme: theme.inputDecorationTheme.copyWith(
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(16),
        borderSide: BorderSide(color: colors.primary, width: 3),
      ),
    ),
  );
}

class AppDevice {
  const AppDevice({this.television = false, this.version = appVersion});
  final bool television;
  final String version;
  static const channel = MethodChannel('duanju/device');

  static Future<AppDevice> detect({
    AppDevice fallback = const AppDevice(),
  }) async {
    if (defaultTargetPlatform != TargetPlatform.android) {
      return const AppDevice();
    }
    try {
      final data = await channel
          .invokeMapMethod<String, dynamic>('deviceInfo')
          .timeout(const Duration(seconds: 2));
      final television = data?['television'];
      if (television is! bool) return fallback;
      final version = data?['version'];
      return AppDevice(
        television: television,
        version: version is String && version.isNotEmpty
            ? version
            : fallback.version,
      );
    } on PlatformException {
      return fallback;
    } on MissingPluginException {
      return fallback;
    } on TimeoutException {
      return fallback;
    }
  }

  static Future<double> getBrightness() async {
    if (defaultTargetPlatform != TargetPlatform.android) return 0.5;
    try {
      final value = await channel.invokeMethod<double>('getBrightness');
      return (value ?? 0.5).clamp(0.0, 1.0);
    } catch (_) {
      return 0.5;
    }
  }

  static Future<void> setBrightness(double brightness) async {
    if (defaultTargetPlatform != TargetPlatform.android) return;
    try {
      await channel.invokeMethod('setBrightness', {'brightness': brightness.clamp(0.01, 1.0)});
    } catch (_) {}
  }

  static Future<void> resetBrightness() async {
    if (defaultTargetPlatform != TargetPlatform.android) return;
    try {
      await channel.invokeMethod('resetBrightness');
    } catch (_) {}
  }

  /// 用原生 ExoPlayer Activity 全屏播放（SurfaceView 直通，不走 Flutter 合成）。
  ///
  /// [decryptionKey] 为 CENC 内容的 AES-128 密钥（十六进制）。非空时原生侧会先
  /// 逐样本解密再交给解码器；明文播放传空字符串。
  ///
  /// [episodes] 为分集标题，供原生侧自绘选集面板；[danmaku] 为弹幕载荷
  /// （每行 `时间毫秒\t文本`），两者都为空时不显示对应 UI。
  ///
  /// 返回播放结束时的进度；用户中途返回时 [NativePlaybackResult.completed] 为 false。
  /// 失败（含原生播放器解码错误）会抛 [NativePlaybackFailure]。
  /// 向正在运行的原生播放器推送弹幕载荷（每行 `时间毫秒\t文本`）。
  ///
  /// 弹幕首次拉取需要服务器向上游取数，因此不随播放器一起传入，而是在
  /// 起播后异步推送，避免阻塞画面。原生播放器未运行时静默忽略。
  static Future<void> updateNativeDanmaku(String payload) async {
    if (defaultTargetPlatform != TargetPlatform.android) return;
    if (payload.isEmpty) return;
    try {
      await channel.invokeMethod('updateNativeDanmaku', {'danmaku': payload});
    } catch (_) {
      // 播放器可能已关闭
    }
  }

  static Future<NativePlaybackResult> openNativePlayer({
    required String url,
    String title = '播放',
    String referer = '',
    String decryptionKey = '',
    List<String> episodes = const [],
    int episodeIndex = 0,
    String danmaku = '',
    List<int> qualities = const [],
    int quality = 0,
    double speed = 1,
    Duration position = Duration.zero,
  }) async {
    if (defaultTargetPlatform != TargetPlatform.android) {
      throw const NativePlaybackFailure('原生播放器仅支持 Android');
    }
    final dynamic data = await channel.invokeMethod<dynamic>('openNativePlayer', {
      'url': url,
      'title': title,
      'referer': referer,
      'key': decryptionKey,
      'episodes': episodes,
      'episodeIndex': episodeIndex,
      'danmaku': danmaku,
      'qualities': qualities,
      'quality': quality,
      'speed': speed,
      'positionMs': position.inMilliseconds,
    });
    if (data is Map) {
      return NativePlaybackResult(
        position: Duration(milliseconds: (data['positionMs'] as num?)?.toInt() ?? 0),
        completed: data['completed'] == true,
        episodeIndex: (data['episodeIndex'] as num?)?.toInt(),
        quality: (data['quality'] as num?)?.toInt(),
        speed: (data['speed'] as num?)?.toDouble(),
      );
    }
    return const NativePlaybackResult(position: Duration.zero, completed: false);
  }
}

class NativePlaybackResult {
  const NativePlaybackResult({
    required this.position,
    required this.completed,
    this.episodeIndex,
    this.quality,
    this.speed,
  });
  final Duration position;
  final bool completed;

  /// 用户在原生选集中切换的分集下标；未切换时为 null。
  final int? episodeIndex;

  /// 用户在原生清晰度面板中选择的档位；未切换时为 null。
  final int? quality;

  /// 用户调整后的倍速；未调整时为 null。
  final double? speed;
}

class NativePlaybackFailure implements Exception {
  const NativePlaybackFailure(this.message);
  final String message;
  @override
  String toString() => message;
}

class AppLayout extends InheritedWidget {
  const AppLayout({
    super.key,
    required this.television,
    this.version = appVersion,
    required super.child,
  });
  final bool television;
  final String version;

  static bool isTelevision(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<AppLayout>()?.television ??
      false;
  static String versionOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<AppLayout>()?.version ??
      appVersion;

  @override
  bool updateShouldNotify(AppLayout oldWidget) =>
      television != oldWidget.television || version != oldWidget.version;
}
