import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:archive/archive.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:path/path.dart' as p;
import 'package:path_provider/path_provider.dart';

import 'app_build.dart';

/// Windows 版自动更新。
///
/// 流程：查 GitHub 最新 Release → 比对版本 → 下载 ZIP → 解压到临时目录 →
/// 由批处理在进程退出后覆盖程序目录并重启。
///
/// 可以安全覆盖的前提：用户数据（追剧、下载记录、设置）保存在系统的
/// Application Support 目录，不在程序目录内，替换程序文件不影响它们。
///
/// 不在进程内直接覆盖：运行中的 exe 与已加载的 DLL 无法被替换，必须等进程
/// 退出，因此把覆盖与重启交给分离的批处理完成。
class WindowsUpdater {
  WindowsUpdater({HttpClient? client}) : _client = client ?? HttpClient() {
    _client.connectionTimeout = const Duration(seconds: 15);
  }

  static const _owner = 'yinzaiqwq';
  static const _repo = 'guoapp-exo';

  final HttpClient _client;

  /// 查询是否有更新。非 Windows 或已是最新时返回 null。
  Future<UpdateInfo?> check() async {
    if (!Platform.isWindows) return null;
    final info = await PackageInfo.fromPlatform();
    final current = parseVersion(info.version, info.buildNumber);
    if (current == null) return null;
    final latest = await latestRelease();
    if (latest == null) return null;
    return latest.version.compareTo(current) > 0 ? latest : null;
  }

  /// 查最新 Release 并解析出与本变体匹配的更新包。
  Future<UpdateInfo?> latestRelease() async {
    final uri = Uri.https(
      'api.github.com',
      '/repos/$_owner/$_repo/releases/latest',
    );
    final request = await _client.getUrl(uri);
    // GitHub 要求带 User-Agent，缺失会返回 403
    request.headers.set(HttpHeaders.userAgentHeader, 'guoapp-exo-updater');
    request.headers.set(
      HttpHeaders.acceptHeader,
      'application/vnd.github+json',
    );
    final response = await request.close();
    if (response.statusCode != HttpStatus.ok) {
      throw HttpException('查询更新失败（HTTP ${response.statusCode}）');
    }
    final body = await response.transform(utf8.decoder).join();
    final json = jsonDecode(body);
    if (json is! Map) return null;

    final tag = json['tag_name'] as String? ?? '';
    final version = parseTag(tag);
    if (version == null) return null;

    // 资产名形如 hongguojian-0.2.66+90-windows-x64.zip
    final prefix = '$appSlug-';
    const suffix = '-windows-x64.zip';
    for (final row in (json['assets'] as List? ?? const [])) {
      if (row is! Map) continue;
      final name = row['name'] as String? ?? '';
      if (!name.startsWith(prefix) || !name.endsWith(suffix)) continue;
      final url = row['browser_download_url'] as String? ?? '';
      if (url.isEmpty) continue;
      return UpdateInfo(
        version: version,
        tag: tag,
        name: name,
        url: url,
        notes: (json['body'] as String? ?? '').trim(),
        size: (row['size'] as num?)?.toInt() ?? 0,
      );
    }
    return null;
  }

  /// 下载更新包；已存在且大小一致时直接复用。
  Future<File> download(
    UpdateInfo update, {
    void Function(int received, int total)? onProgress,
  }) async {
    final temp = await getTemporaryDirectory();
    final target = File(p.join(temp.path, 'updates', update.name));
    await target.parent.create(recursive: true);
    if (await target.exists()) {
      final length = await target.length();
      if (length > 0 && (update.size <= 0 || length == update.size)) {
        return target;
      }
      await target.delete();
    }

    final request = await _client.getUrl(Uri.parse(update.url));
    request.headers.set(HttpHeaders.userAgentHeader, 'guoapp-exo-updater');
    final response = await request.close();
    if (response.statusCode != HttpStatus.ok) {
      throw HttpException('下载更新包失败（HTTP ${response.statusCode}）');
    }
    final total = update.size > 0 ? update.size : response.contentLength;
    var received = 0;
    final sink = target.openWrite();
    try {
      await for (final chunk in response) {
        sink.add(chunk);
        received += chunk.length;
        onProgress?.call(received, total);
      }
    } finally {
      await sink.close();
    }
    if (total > 0 && received != total) {
      await target.delete().catchError((_) => target);
      throw const HttpException('更新包下载不完整，请重试');
    }
    return target;
  }

  /// 解压并安排替换：写出批处理并分离执行。
  ///
  /// 调用方应在此之后提示用户程序即将重启。
  Future<void> apply(File archive) async {
    final executable = Platform.resolvedExecutable;
    final installDir = p.dirname(executable);
    final temp = await getTemporaryDirectory();
    final staging = Directory(p.join(temp.path, 'updates', 'staging'));
    if (await staging.exists()) await staging.delete(recursive: true);
    await staging.create(recursive: true);

    await _extract(archive, staging);

    // 解压结果必须含 exe，否则说明包不完整，宁可不更新
    if (!await File(p.join(staging.path, '$appSlug.exe')).exists()) {
      throw const HttpException('更新包内容不完整，已取消更新');
    }

    final script = File(p.join(temp.path, 'updates', 'apply_update.bat'));
    await script.writeAsString(_script(installDir, staging.path, executable));
    await Process.start(
      'cmd.exe',
      ['/c', script.path],
      mode: ProcessStartMode.detached,
      runInShell: true,
    );
  }

  /// 替换脚本：等主进程退出后覆盖，再重启。
  ///
  /// 用 ping 延时而非 timeout —— 无控制台时 timeout 会直接失败。
  /// 覆盖重试若干次，给刚退出的进程留出释放文件句柄的时间。
  String _script(String installDir, String staging, String executable) {
    final buffer = StringBuffer()
      ..writeln('@echo off')
      ..writeln('chcp 65001 >nul')
      ..writeln('ping 127.0.0.1 -n 4 >nul');
    for (var attempt = 0; attempt < 10; attempt++) {
      buffer
        ..writeln('copy /y "$staging\\*" "$installDir\\" >nul 2>&1')
        ..writeln('if not errorlevel 1 goto :done')
        ..writeln('ping 127.0.0.1 -n 3 >nul');
    }
    buffer
      ..writeln(':done')
      ..writeln('start "" "$executable"')
      ..writeln('rmdir /s /q "$staging" >nul 2>&1')
      ..writeln('del "%~f0"');
    return buffer.toString();
  }

  /// 解压 ZIP 到目标目录，拒绝越界路径。
  Future<void> _extract(File archive, Directory target) async {
    final bytes = await archive.readAsBytes();
    final decoded = ZipDecoder().decodeBytes(bytes);
    var files = 0;
    for (final entry in decoded) {
      if (!entry.isFile) continue;
      final name = entry.name.replaceAll('\\', '/');
      if (name.startsWith('/') || name.contains('../') || name.contains(':')) {
        throw HttpException('更新包包含非法路径：$name');
      }
      final destination = File(p.join(target.path, name));
      await destination.parent.create(recursive: true);
      await destination.writeAsBytes(entry.content as List<int>);
      files++;
    }
    if (files == 0) throw const HttpException('更新包无法解析');
  }
}

/// 可比较的版本号：先比语义版本，再比构建号。
class AppVersion implements Comparable<AppVersion> {
  const AppVersion(this.major, this.minor, this.patch, this.build);

  final int major, minor, patch, build;

  @override
  int compareTo(AppVersion other) {
    if (major != other.major) return major.compareTo(other.major);
    if (minor != other.minor) return minor.compareTo(other.minor);
    if (patch != other.patch) return patch.compareTo(other.patch);
    return build.compareTo(other.build);
  }

  @override
  String toString() => '$major.$minor.$patch+$build';
}

/// 解析 pubspec 里的 `0.2.66+90`。
AppVersion? parseVersion(String name, String build) {
  final match = RegExp(r'^(\d+)\.(\d+)\.(\d+)').firstMatch(name.trim());
  if (match == null) return null;
  return AppVersion(
    int.parse(match.group(1)!),
    int.parse(match.group(2)!),
    int.parse(match.group(3)!),
    int.tryParse(build.trim()) ?? 0,
  );
}

/// 解析 Release tag `app-v0.2.66-90`。
AppVersion? parseTag(String tag) {
  final match = RegExp(
    r'^app-v(\d+)\.(\d+)\.(\d+)-(\d+)$',
  ).firstMatch(tag.trim());
  if (match == null) return null;
  return AppVersion(
    int.parse(match.group(1)!),
    int.parse(match.group(2)!),
    int.parse(match.group(3)!),
    int.parse(match.group(4)!),
  );
}

class UpdateInfo {
  const UpdateInfo({
    required this.version,
    required this.tag,
    required this.name,
    required this.url,
    required this.notes,
    required this.size,
  });

  final AppVersion version;
  final String tag;
  final String name;
  final String url;
  final String notes;
  final int size;

  String get label => version.toString();

  String get sizeLabel => size <= 0
      ? ''
      : size < 1024 * 1024
      ? '${(size / 1024).toStringAsFixed(0)} KB'
      : '${(size / 1024 / 1024).toStringAsFixed(1)} MB';
}
