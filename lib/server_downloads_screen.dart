import 'dart:async';

import 'package:flutter/material.dart';

import 'core_bridge.dart';
import 'local_store.dart';
import 'models.dart';
import 'player_screen.dart';
import 'server_downloads.dart';

/// 服务器端已下载内容的管理界面。
///
/// 这里管理的是自建红果TV 服务磁盘上的副本，与设备本地下载无关：
/// 删除只释放服务器空间，之后仍可重新下载或直接在线播放。
class ServerDownloadsScreen extends StatefulWidget {
  const ServerDownloadsScreen({
    super.key,
    required this.repository,
    required this.store,
  });

  final AppRepository repository;
  final LocalStore store;

  @override
  State<ServerDownloadsScreen> createState() => _ServerDownloadsScreenState();
}

class _ServerDownloadsScreenState extends State<ServerDownloadsScreen> {
  ServerDownloadList? _list;
  String? _error;
  bool _loading = true;
  bool _busy = false;
  bool _opening = false;
  final _expanded = <String>{};

  @override
  void initState() {
    super.initState();
    unawaited(_load());
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final list = await widget.repository.serverDownloads();
      if (!mounted) return;
      setState(() {
        _list = list;
        _loading = false;
      });
    } catch (error) {
      if (!mounted) return;
      setState(() {
        _error = error is AppFailure ? error.message : '读取服务器下载失败';
        _loading = false;
      });
    }
  }

  Future<void> _delete(ServerDownloadDrama drama, {int? episode}) async {
    final label = episode == null ? '《${drama.title}》全部' : '第 $episode 集';
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: Text('删除服务器上的 $label？'),
        content: const Text('只删除服务器磁盘上的副本，本机下载不受影响。'),
        actions: [
          TextButton(
            onPressed: () => Navigator.pop(context, false),
            child: const Text('取消'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context, true),
            child: const Text('确认删除'),
          ),
        ],
      ),
    );
    if (confirmed != true || _busy || !mounted) return;

    setState(() => _busy = true);
    try {
      final message = await widget.repository.deleteServerDownload(
        drama.folder,
        episode: episode?.toString() ?? '',
      );
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(message)));
      await _load();
    } catch (error) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text(error is AppFailure ? error.message : '删除失败')),
      );
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _play(ServerDownloadDrama drama, int initialIndex) async {
    if (_opening || initialIndex < 0 || initialIndex >= drama.episodes.length) {
      return;
    }
    _opening = true;
    final episodes = [
      for (final episode in drama.episodes)
        Episode({
          'id': episode.filename,
          'title': '第 ${episode.index} 集',
          'currentEpisode': episode.index,
        }, episode.index),
    ];
    final seriesId = drama.seriesId.isEmpty
        ? 'server-download/${Uri.encodeComponent(drama.folder)}'
        : drama.seriesId;
    final detail = DramaDetail(
      Drama(
        id: 'hongguotv:$seriesId',
        source: 'hongguotv',
        sourceId: seriesId,
        title: drama.title,
        episodes: drama.totalEpisodes > 0
            ? drama.totalEpisodes
            : episodes.length,
      ),
      episodes,
    );
    final watched =
        widget.store.watched(detail.drama.id) ??
        widget.store.mediaWatched('server-download:${drama.folder}');
    final position =
        watched?.episode == episodes[initialIndex].number &&
            watched?.finished == false
        ? watched!.position
        : 0.0;
    try {
      await Navigator.of(context).push(
        MaterialPageRoute<void>(
          builder: (_) => PlayerScreen(
            detail: detail,
            initialIndex: initialIndex,
            initialPosition: position,
            repository: widget.repository,
            store: widget.store,
            allowOnlineFallback: false,
            planResolver: (index) => widget.repository.serverDownloadPlayback(
              drama.folder,
              drama.episodes[index].filename,
            ),
          ),
        ),
      );
    } finally {
      _opening = false;
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        title: const Text('服务器下载'),
        actions: [
          IconButton(
            tooltip: '刷新',
            onPressed: _busy ? null : () => unawaited(_load()),
            icon: const Icon(Icons.refresh_rounded),
          ),
        ],
      ),
      body: _buildBody(theme),
    );
  }

  Widget _buildBody(ThemeData theme) {
    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }
    if (_error != null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(
                Icons.cloud_off_rounded,
                size: 48,
                color: theme.colorScheme.onSurfaceVariant,
              ),
              const SizedBox(height: 16),
              Text(_error!, textAlign: TextAlign.center),
              const SizedBox(height: 20),
              FilledButton(
                onPressed: () => unawaited(_load()),
                child: const Text('重试'),
              ),
            ],
          ),
        ),
      );
    }
    final list = _list;
    if (list == null || list.isEmpty) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.cloud_done_rounded,
              size: 48,
              color: theme.colorScheme.onSurfaceVariant,
            ),
            const SizedBox(height: 16),
            const Text('服务器上还没有已下载的剧集'),
          ],
        ),
      );
    }

    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.symmetric(vertical: 8),
        children: [
          if (list.totalSize.isNotEmpty || list.diskFree.isNotEmpty)
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 8, 16, 12),
              child: Text(
                [
                  if (list.totalSize.isNotEmpty) '共占用 ${list.totalSize}',
                  if (list.diskFree.isNotEmpty) '服务器剩余 ${list.diskFree}',
                ].join(' · '),
                style: TextStyle(
                  fontSize: 13,
                  color: theme.colorScheme.onSurfaceVariant,
                ),
              ),
            ),
          for (final drama in list.dramas) _buildDrama(theme, drama),
        ],
      ),
    );
  }

  Widget _buildDrama(ThemeData theme, ServerDownloadDrama drama) {
    final expanded = _expanded.contains(drama.folder);
    return Card(
      margin: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
      clipBehavior: Clip.antiAlias,
      child: Column(
        children: [
          ListTile(
            leading: const Icon(Icons.movie_rounded),
            title: Text(
              drama.title,
              maxLines: 2,
              overflow: TextOverflow.ellipsis,
            ),
            subtitle: Text(
              [
                if (drama.downloaded > 0) '${drama.downloaded} 集',
                if (drama.size.isNotEmpty) drama.size,
                if (drama.updatedTime.isNotEmpty) drama.updatedTime,
              ].join(' · '),
              style: const TextStyle(fontSize: 12),
            ),
            trailing: Row(
              mainAxisSize: MainAxisSize.min,
              children: [
                if (drama.episodes.isNotEmpty)
                  IconButton(
                    tooltip: '从第 1 集播放',
                    onPressed: _opening
                        ? null
                        : () => unawaited(_play(drama, 0)),
                    icon: const Icon(Icons.play_circle_outline_rounded),
                  ),
                if (drama.episodes.isNotEmpty)
                  IconButton(
                    tooltip: expanded ? '收起分集' : '展开分集',
                    onPressed: () => setState(() {
                      if (expanded) {
                        _expanded.remove(drama.folder);
                      } else {
                        _expanded.add(drama.folder);
                      }
                    }),
                    icon: Icon(
                      expanded
                          ? Icons.expand_less_rounded
                          : Icons.expand_more_rounded,
                    ),
                  ),
                IconButton(
                  tooltip: '删除整部',
                  onPressed: _busy ? null : () => unawaited(_delete(drama)),
                  icon: Icon(
                    Icons.delete_outline_rounded,
                    color: theme.colorScheme.error,
                  ),
                ),
              ],
            ),
            onTap: drama.episodes.isEmpty
                ? null
                : () => setState(() {
                    if (expanded) {
                      _expanded.remove(drama.folder);
                    } else {
                      _expanded.add(drama.folder);
                    }
                  }),
          ),
          if (expanded && drama.episodes.isNotEmpty)
            Container(
              width: double.infinity,
              color: theme.colorScheme.surfaceContainerHighest.withValues(
                alpha: .3,
              ),
              padding: const EdgeInsets.symmetric(vertical: 4),
              child: Column(
                children: [
                  for (final episode in drama.episodes)
                    ListTile(
                      dense: true,
                      leading: const Icon(Icons.play_circle_outline, size: 20),
                      title: Text(
                        '第 ${episode.index} 集',
                        style: const TextStyle(fontSize: 14),
                      ),
                      subtitle: Text(
                        [
                          if (episode.size.isNotEmpty) episode.size,
                          if (episode.hasDanmaku) '含弹幕',
                        ].join(' · '),
                        style: const TextStyle(fontSize: 11),
                      ),
                      trailing: IconButton(
                        tooltip: '删除本集',
                        onPressed: _busy
                            ? null
                            : () => unawaited(
                                _delete(drama, episode: episode.index),
                              ),
                        icon: Icon(
                          Icons.delete_outline_rounded,
                          size: 20,
                          color: theme.colorScheme.error,
                        ),
                      ),
                      onTap: _opening
                          ? null
                          : () => unawaited(
                              _play(drama, drama.episodes.indexOf(episode)),
                            ),
                    ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}
