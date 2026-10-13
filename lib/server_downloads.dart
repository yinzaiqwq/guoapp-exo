/// 服务器端（自建红果TV 服务）已下载内容的清单模型。
///
/// 与设备本地下载相互独立：删除服务器副本只释放服务器磁盘，不影响本机文件，
/// 之后仍可重新下载或直接在线播放。
class ServerDownloadEpisode {
  const ServerDownloadEpisode({
    required this.index,
    required this.filename,
    this.size = '',
    this.hasDanmaku = false,
  });

  final int index;
  final String filename;
  final String size;
  final bool hasDanmaku;

  factory ServerDownloadEpisode.fromJson(Map<String, dynamic> json) =>
      ServerDownloadEpisode(
        index: (json['index'] as num?)?.toInt() ?? 0,
        filename: json['filename'] as String? ?? '',
        size: json['sizeHuman'] as String? ?? '',
        hasDanmaku: json['hasDanmaku'] == true,
      );
}

class ServerDownloadDrama {
  const ServerDownloadDrama({
    required this.folder,
    required this.title,
    this.totalEpisodes = 0,
    this.downloaded = 0,
    this.size = '',
    this.updatedTime = '',
    this.episodes = const [],
  });

  /// 服务器上的目录名，删除时用它定位。
  final String folder;
  final String title;
  final int totalEpisodes;
  final int downloaded;
  final String size;
  final String updatedTime;
  final List<ServerDownloadEpisode> episodes;

  factory ServerDownloadDrama.fromJson(Map<String, dynamic> json) =>
      ServerDownloadDrama(
        folder: json['folder'] as String? ?? '',
        title: json['title'] as String? ?? '',
        totalEpisodes: (json['totalEpisodes'] as num?)?.toInt() ?? 0,
        downloaded: (json['downloaded'] as num?)?.toInt() ?? 0,
        size: json['sizeHuman'] as String? ?? '',
        updatedTime: json['updatedTime'] as String? ?? '',
        episodes: [
          for (final row in json['episodes'] as List? ?? const [])
            ServerDownloadEpisode.fromJson(
              Map<String, dynamic>.from(row as Map),
            ),
        ],
      );
}

class ServerDownloadList {
  const ServerDownloadList({
    this.dramas = const [],
    this.totalSize = '',
    this.diskFree = '',
    this.diskUsed = '',
  });

  final List<ServerDownloadDrama> dramas;
  final String totalSize;
  final String diskFree;
  final String diskUsed;

  bool get isEmpty => dramas.isEmpty;

  factory ServerDownloadList.fromJson(Map<String, dynamic> json) =>
      ServerDownloadList(
        dramas: [
          for (final row in json['dramas'] as List? ?? const [])
            ServerDownloadDrama.fromJson(
              Map<String, dynamic>.from(row as Map),
            ),
        ],
        totalSize: json['totalSize'] as String? ?? '',
        diskFree: json['diskFree'] as String? ?? '',
        diskUsed: json['diskUsed'] as String? ?? '',
      );
}
