import 'dart:async';

import 'package:flutter/material.dart';

import 'core_bridge.dart';
import 'detail_screen.dart';
import 'local_store.dart';
import 'models.dart';
import 'scan_search_dialog.dart';
import 'widgets.dart';

class HongguoAccountScreen extends StatefulWidget {
  const HongguoAccountScreen({
    super.key,
    required this.repository,
    required this.store,
  });

  final AppRepository repository;
  final LocalStore store;

  @override
  State<HongguoAccountScreen> createState() => _HongguoAccountScreenState();
}

class _HongguoAccountScreenState extends State<HongguoAccountScreen>
    with WidgetsBindingObserver {
  late final int _profileEpoch;
  late final String _profileId;
  Timer? _pollTimer;
  Timer? _countdownTimer;
  int _generation = 0;
  int _pollFailures = 0;
  bool _invalidated = false;
  bool _foreground = true;
  bool _busy = false;
  bool _polling = false;
  bool _loadingLikes = false;
  bool _statusLoaded = false;
  bool _storageError = false;
  bool _authenticated = false;
  bool _persistent = false;
  bool _identityVerified = false;
  bool _sessionExpired = false;
  bool _likesLoaded = false;
  bool _hasMore = false;
  bool _retryLikesRefresh = true;
  int _nextOffset = 0;
  int _unsupportedCount = 0;
  String _displayName = '';
  String _sessionId = '';
  String _qrText = '';
  String _phase = 'idle';
  String? _accountError;
  String? _likesError;
  String _qrMessage = '';
  DateTime? _expiresAt;
  DateTime? _syncedAt;
  List<_LikedSeries> _items = const [];

  bool get _active =>
      mounted &&
      !_invalidated &&
      !widget.store.locked &&
      widget.store.profileEpoch == _profileEpoch &&
      widget.store.profile.id == _profileId;

  int get _remainingSeconds {
    final remaining = _expiresAt?.difference(DateTime.now()).inSeconds ?? 0;
    return remaining < 0 ? 0 : remaining;
  }

  bool get _qrEnded =>
      const {'expired', 'cancelled', 'error'}.contains(_phase);

  @override
  void initState() {
    super.initState();
    _profileEpoch = widget.store.profileEpoch;
    _profileId = widget.store.profile.id;
    widget.store.addListener(_storeChanged);
    WidgetsBinding.instance.addObserver(this);
    unawaited(_loadStatus());
  }

  @override
  void didUpdateWidget(covariant HongguoAccountScreen oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (oldWidget.store != widget.store) {
      oldWidget.store.removeListener(_storeChanged);
      widget.store.addListener(_storeChanged);
      _invalidate();
    }
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    _foreground = state == AppLifecycleState.resumed;
    if (!_foreground) {
      _pollTimer?.cancel();
    } else if (_active && _sessionId.isNotEmpty && !_qrEnded) {
      if (_identityVerified) {
        unawaited(_commit());
      } else {
        _schedulePoll(1000);
      }
    }
  }

  @override
  void dispose() {
    _generation++;
    _pollTimer?.cancel();
    _countdownTimer?.cancel();
    widget.store.removeListener(_storeChanged);
    WidgetsBinding.instance.removeObserver(this);
    _cancelRemote(_sessionId);
    super.dispose();
  }

  void _storeChanged() {
    if (!_active && !_invalidated) _invalidate();
  }

  void _invalidate() {
    if (!mounted || _invalidated) return;
    final session = _sessionId;
    _generation++;
    _pollTimer?.cancel();
    _countdownTimer?.cancel();
    setState(() {
      _invalidated = true;
      _authenticated = false;
      _items = const [];
      _qrText = '';
      _sessionId = '';
      _displayName = '';
    });
    _cancelRemote(session);
  }

  void _cancelRemote(String session) {
    if (session.isEmpty) return;
    unawaited(
      widget.repository
          .hongguoAccount('cancel', sessionId: session, profileId: _profileId)
          .then<void>((_) {}, onError: (Object _) {}),
    );
  }

  bool _current(int generation) => _active && generation == _generation;

  String _errorText(Object error, String fallback) =>
      error is AppFailure ? error.message : fallback;

  Future<void> _loadStatus() async {
    if (!_active || _busy) return;
    final generation = ++_generation;
    setState(() {
      _busy = true;
      _accountError = null;
    });
    try {
      final result = await widget.repository.hongguoAccount('status');
      if (!_current(generation)) return;
      setState(() {
        _applyAccount(result);
        _statusLoaded = true;
      });
    } catch (error) {
      if (_current(generation)) {
        setState(() => _accountError = _errorText(error, '读取账号状态失败，请重试'));
      }
    } finally {
      if (_current(generation)) setState(() => _busy = false);
    }
    if (_current(generation) && _authenticated) {
      await _loadLikes(refresh: true);
    }
  }

  void _applyAccount(Map<String, dynamic> result) {
    _authenticated = result['authenticated'] == true;
    _sessionExpired = result['sessionExpired'] == true;
    _persistent = result['persistent'] == true;
    _storageError = result['storageError'] == true;
    final name = result['displayName'];
    _displayName = name is String && name.isNotEmpty ? name : '红果账号';
    if (_storageError) _accountError = '保存的红果登录信息无法读取，请清除后重新登录';
  }

  Future<void> _start() async {
    if (!_active || _busy || _authenticated || _storageError || _sessionExpired) {
      return;
    }
    final previous = _sessionId;
    final generation = ++_generation;
    _pollTimer?.cancel();
    _countdownTimer?.cancel();
    _cancelRemote(previous);
    setState(() {
      _busy = true;
      _accountError = null;
      _sessionId = '';
      _qrText = '';
      _phase = 'creating';
      _qrMessage = '';
      _identityVerified = false;
      _pollFailures = 0;
    });
    try {
      final result = await widget.repository.hongguoAccount('start');
      final session = result['sessionId'];
      if (!_current(generation)) {
        if (session is String) _cancelRemote(session);
        return;
      }
      final text = result['qrText'];
      if (session is! String ||
          session.isEmpty ||
          text is! String ||
          text.isEmpty) {
        throw const FormatException('二维码未就绪');
      }
      setState(() {
        _sessionId = session;
        _qrText = text;
        _applyQR(result);
      });
      _startCountdown();
      _schedulePoll(_retryAfter(result));
    } catch (error) {
      if (_current(generation)) {
        setState(() {
          _phase = 'error';
          _accountError = _errorText(error, '获取二维码失败，请重试');
        });
      }
    } finally {
      if (_current(generation)) setState(() => _busy = false);
    }
  }

  int _retryAfter(Map<String, dynamic> result) =>
      intValue(result['retryAfterMs']).clamp(1500, 15000).toInt();

  void _applyQR(Map<String, dynamic> result) {
    final phase = result['phase'];
    _phase = phase is String ? phase : 'waiting-for-scan';
    _identityVerified = result['identityVerified'] == true;
    final message = result['message'];
    _qrMessage = message is String ? message : '';
    if (!_identityVerified) {
      _expiresAt = DateTime.now().add(
        Duration(seconds: intValue(result['expiresIn'])),
      );
    }
  }

  void _startCountdown() {
    _countdownTimer?.cancel();
    _countdownTimer = Timer.periodic(const Duration(seconds: 1), (_) {
      if (!_active || _qrEnded || _identityVerified || _sessionId.isEmpty) {
        _countdownTimer?.cancel();
      } else if (_remainingSeconds <= 0 && !_polling) {
        _pollTimer?.cancel();
        _countdownTimer?.cancel();
        setState(() {
          _phase = 'expired';
          _qrMessage = '二维码已过期，请重新获取';
        });
      } else {
        setState(() {});
      }
    });
  }

  void _schedulePoll(int milliseconds) {
    _pollTimer?.cancel();
    if (!_active ||
        !_foreground ||
        _sessionId.isEmpty ||
        _qrEnded ||
        _identityVerified) {
      return;
    }
    _pollTimer = Timer(
      Duration(milliseconds: milliseconds),
      () => unawaited(_poll()),
    );
  }

  Future<void> _poll() async {
    if (!_active || !_foreground || _sessionId.isEmpty || _qrEnded) return;
    if (_polling) {
      _schedulePoll(1500);
      return;
    }
    final generation = _generation;
    final session = _sessionId;
    _polling = true;
    try {
      final result = await widget.repository.hongguoAccount(
        'poll',
        sessionId: session,
      );
      if (!_current(generation) || session != _sessionId) return;
      setState(() {
        _applyQR(result);
        _accountError = null;
        _pollFailures = 0;
      });
      if (_identityVerified) {
        _countdownTimer?.cancel();
        if (_foreground) await _commit();
      } else {
        _schedulePoll(_retryAfter(result));
      }
    } catch (error) {
      if (!_current(generation) || session != _sessionId) return;
      _pollFailures++;
      setState(() {
        _accountError = _pollFailures >= 3
            ? '扫码状态查询已暂停，请检查连接后继续查询'
            : '扫码状态查询暂时中断，正在重试（$_pollFailures/3）';
      });
      if (_pollFailures < 3) _schedulePoll(3000 * _pollFailures);
    } finally {
      _polling = false;
    }
  }

  Future<void> _commit() async {
    if (!_active || _busy || !_identityVerified || _sessionId.isEmpty) return;
    final generation = _generation;
    final session = _sessionId;
    _pollTimer?.cancel();
    setState(() {
      _busy = true;
      _accountError = null;
    });
    var loggedIn = false;
    try {
      final result = await widget.repository.hongguoAccount(
        'commit',
        sessionId: session,
      );
      if (!_current(generation)) return;
      if (result['authenticated'] != true) {
        throw const FormatException('账号尚未完成登录');
      }
      setState(() {
        _applyAccount(result);
        _statusLoaded = true;
        _sessionId = '';
        _qrText = '';
        _phase = 'idle';
        _identityVerified = false;
        _sessionExpired = false;
        _items = const [];
        _likesLoaded = false;
        _likesError = null;
        _syncedAt = null;
        _hasMore = false;
        _unsupportedCount = 0;
      });
      loggedIn = true;
    } catch (error) {
      if (_current(generation)) {
        setState(() => _accountError = _errorText(error, '账号已确认，保存登录失败，请重试'));
      }
    } finally {
      if (_current(generation)) setState(() => _busy = false);
    }
    if (loggedIn && _current(generation)) await _loadLikes(refresh: true);
  }

  void _cancel() {
    if (!_active || _busy) return;
    final session = _sessionId;
    _generation++;
    _pollTimer?.cancel();
    _countdownTimer?.cancel();
    setState(() {
      _sessionId = '';
      _qrText = '';
      _phase = 'idle';
      _identityVerified = false;
      _accountError = null;
      _qrMessage = '';
    });
    _cancelRemote(session);
  }

  Future<void> _logout() async {
    if (!_active || _busy || _loadingLikes) return;
    final generation = ++_generation;
    setState(() {
      _busy = true;
      _accountError = null;
    });
    try {
      await widget.repository.hongguoAccount('logout');
      if (!_current(generation)) return;
      setState(() {
        _authenticated = false;
        _storageError = false;
        _displayName = '';
        _sessionExpired = false;
        _likesLoaded = false;
        _items = const [];
        _likesError = null;
        _syncedAt = null;
        _hasMore = false;
        _nextOffset = 0;
        _unsupportedCount = 0;
      });
    } catch (error) {
      if (_current(generation)) {
        setState(() => _accountError = _errorText(error, '退出登录失败，请重试'));
      }
    } finally {
      if (_current(generation)) setState(() => _busy = false);
    }
  }

  Future<void> _loadLikes({required bool refresh}) async {
    if (!_active ||
        !_authenticated ||
        _busy ||
        _loadingLikes ||
        _sessionExpired ||
        (!refresh && !_hasMore)) {
      return;
    }
    final generation = _generation;
    final offset = refresh ? 0 : _nextOffset;
    setState(() {
      _loadingLikes = true;
      _likesError = null;
      _retryLikesRefresh = refresh;
    });
    try {
      final result = await widget.repository.hongguoAccount(
        'likes',
        offset: offset,
      );
      if (!_current(generation)) return;
      final rows = result['items'];
      final next = intValue(result['nextOffset']);
      final more = result['hasMore'] == true;
      if (rows is! List ||
          intValue(result['offset']) != offset ||
          (more && (next <= offset || next > 2000))) {
        throw const FormatException('点赞列表分页异常');
      }
      final incoming = rows.map(_LikedSeries.fromJson).toList();
      final merged = <String, _LikedSeries>{
        if (!refresh) for (final item in _items) item.drama.sourceId: item,
        for (final item in incoming) item.drama.sourceId: item,
      };
      setState(() {
        _items = merged.values.toList();
        _likesLoaded = true;
        _hasMore = more;
        _nextOffset = next;
        _unsupportedCount =
            (refresh ? 0 : _unsupportedCount) +
            intValue(result['unsupportedCount']);
        _syncedAt = DateTime.now();
      });
    } catch (error) {
      if (_current(generation)) {
        setState(() {
          _sessionExpired =
              error is AppFailure && error.code == 'hongguo_session_expired';
          _likesError = _errorText(error, '读取点赞片单失败，已有列表已保留，请重试');
        });
      }
    } finally {
      if (_current(generation)) setState(() => _loadingLikes = false);
    }
  }

  void _open(_LikedSeries item) {
    if (!_active ||
        !item.playable ||
        !widget.store.allowsSource(SourceSite.hongguotv.id)) {
      return;
    }
    unawaited(
      Navigator.of(context).push<void>(
        MaterialPageRoute(
          builder: (_) => DetailScreen(
            drama: item.drama,
            repository: widget.repository,
            store: widget.store,
          ),
        ),
      ),
    );
  }

  Widget _message(String text, {bool error = false}) => Padding(
    padding: const EdgeInsets.symmetric(vertical: 8),
    child: Text(
      text,
      style: TextStyle(
        color: error ? Theme.of(context).colorScheme.error : null,
      ),
    ),
  );

  Widget _accountCard() {
    final colors = Theme.of(context).colorScheme;
    return Card(
      child: Padding(
        padding: const EdgeInsets.all(20),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                CircleAvatar(
                  backgroundColor: colors.primaryContainer,
                  child: Icon(
                    _authenticated
                        ? Icons.person_rounded
                        : Icons.person_outline_rounded,
                    color: colors.onPrimaryContainer,
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Text(
                    _authenticated ? _displayName : '登录红果账号',
                    style: Theme.of(context).textTheme.titleLarge,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 12),
            Text(
              _authenticated
                  ? '账号仅关联当前本地用户“${widget.store.profile.name}”。'
                  : '使用手机红果 App 扫码并确认，即可读取自己点赞的剧集。',
            ),
            if (_authenticated && !_persistent)
              _message('当前设备仅在本次运行中保留登录，关闭应用后需要重新扫码。'),
            if (_sessionExpired)
              _message('登录已过期。请退出后重新扫码，已读取的片单不会因网络错误清除。', error: true),
            if (_accountError != null) _message(_accountError!, error: true),
            if (_busy) ...[
              const SizedBox(height: 12),
              const LinearProgressIndicator(),
            ],
            const SizedBox(height: 12),
            if (!_statusLoaded && !_busy)
              FilledButton.icon(
                onPressed: _loadStatus,
                icon: const Icon(Icons.refresh_rounded),
                label: const Text('重试读取账号'),
              )
            else if (_storageError || _authenticated || _sessionExpired)
              OutlinedButton.icon(
                onPressed: _busy || _loadingLikes ? null : _logout,
                icon: const Icon(Icons.logout_rounded),
                label: Text(
                  _storageError
                      ? '清除本地登录'
                      : _sessionExpired
                      ? '退出后重新登录'
                      : '退出红果账号',
                ),
              )
            else if (_qrText.isEmpty)
              FilledButton.icon(
                onPressed: _busy ? null : _start,
                icon: const Icon(Icons.qr_code_rounded),
                label: const Text('获取登录二维码'),
              )
            else
              _qrPanel(),
          ],
        ),
      ),
    );
  }

  Widget _qrPanel() => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Center(
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 280),
          child: AspectRatio(
            aspectRatio: 1,
            child: Semantics(
              label: '红果账号登录二维码，请使用手机红果 App 扫描',
              image: true,
              child: Opacity(
                opacity: _qrEnded ? 0.25 : 1,
                child: QrCodeView(data: _qrText),
              ),
            ),
          ),
        ),
      ),
      const SizedBox(height: 12),
      Text(
        _identityVerified
            ? (_busy ? '账号已确认，正在完成登录' : '账号已确认，请重试完成登录')
            : _phase == 'waiting-for-confirmation'
            ? '已扫码，请在手机上确认登录'
            : _qrEnded
            ? (_qrMessage.isNotEmpty ? _qrMessage : '二维码已失效，请重新获取')
            : '请使用手机红果 App 扫码 · 剩余 $_remainingSeconds 秒',
      ),
      if (_qrMessage.isNotEmpty && !_qrEnded && !_identityVerified)
        _message(_qrMessage),
      const SizedBox(height: 12),
      Wrap(
        spacing: 12,
        runSpacing: 8,
        children: [
          if (_identityVerified && !_busy)
            FilledButton(
              onPressed: _commit,
              child: const Text('重试完成登录'),
            )
          else if (_qrEnded)
            FilledButton(
              onPressed: _busy ? null : _start,
              child: const Text('重新获取二维码'),
            )
          else if (_pollFailures >= 3)
            FilledButton(
              onPressed: _busy
                  ? null
                  : () {
                      setState(() {
                        _pollFailures = 0;
                        _accountError = null;
                      });
                      unawaited(_poll());
                    },
              child: const Text('继续查询'),
            ),
          TextButton(
            onPressed: _busy ? null : _cancel,
            child: const Text('取消登录'),
          ),
        ],
      ),
    ],
  );

  Widget _likesHeader() => Padding(
    padding: const EdgeInsets.fromLTRB(4, 20, 4, 12),
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Wrap(
          spacing: 16,
          runSpacing: 8,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            Text(
              '我的点赞 · ${_items.length} 部',
              style: Theme.of(context).textTheme.titleLarge,
            ),
            OutlinedButton.icon(
              onPressed: _busy || _loadingLikes || _sessionExpired
                  ? null
                  : () => _loadLikes(refresh: true),
              icon: const Icon(Icons.refresh_rounded),
              label: const Text('刷新片单'),
            ),
          ],
        ),
        const SizedBox(height: 8),
        const Text('片单来自红果账号；点击剧集后，通过已配置的红果TV服务播放。'),
        if (_syncedAt != null)
          _message(
            '本次更新 ${_syncedAt!.hour.toString().padLeft(2, '0')}:'
            '${_syncedAt!.minute.toString().padLeft(2, '0')}',
          ),
        if (_unsupportedCount > 0)
          _message('已略过 $_unsupportedCount 条非剧集或暂不支持的点赞内容。'),
        if (!widget.store.allowsSource(SourceSite.hongguotv.id))
          _message('当前本地用户未开启红果TV权限，片单可查看，播放入口暂不可用。'),
        if (_likesError != null) ...[
          _message(_likesError!, error: true),
          if (!_sessionExpired)
            TextButton.icon(
              onPressed: _loadingLikes
                  ? null
                  : () => _loadLikes(refresh: _retryLikesRefresh),
              icon: const Icon(Icons.refresh_rounded),
              label: const Text('重试读取'),
            ),
        ],
        if (_loadingLikes) const LinearProgressIndicator(),
        if (_likesLoaded && _items.isEmpty && !_loadingLikes)
          _message(
            _unsupportedCount > 0
                ? '当前已读取的点赞中没有可展示的剧集。'
                : '当前已读取的点赞片单为空。可先在红果 App 点赞剧集，再刷新。',
          ),
      ],
    ),
  );

  Widget _likedTile(_LikedSeries item) {
    final enabled =
        item.playable &&
        widget.store.allowsSource(SourceSite.hongguotv.id);
    return Card(
      margin: const EdgeInsets.only(bottom: 10),
      child: InkWell(
        borderRadius: BorderRadius.circular(12),
        onTap: enabled ? () => _open(item) : null,
        child: Padding(
          padding: const EdgeInsets.all(12),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              SizedBox(
                width: 72,
                height: 100,
                child: DramaCover(
                  drama: item.drama,
                  repository: widget.repository,
                  radius: 8,
                ),
              ),
              const SizedBox(width: 14),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      item.drama.title,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    if (item.drama.description.isNotEmpty) ...[
                      const SizedBox(height: 6),
                      Text(
                        item.drama.description,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: Theme.of(context).textTheme.bodySmall,
                      ),
                    ],
                    const SizedBox(height: 8),
                    Text(
                      item.playable
                          ? (enabled ? '在红果TV中打开' : '当前用户暂无播放权限')
                          : item.note.isNotEmpty
                          ? item.note
                          : '此剧当前不可播放',
                      style: Theme.of(context).textTheme.labelMedium,
                    ),
                  ],
                ),
              ),
              if (enabled)
                const Padding(
                  padding: EdgeInsets.only(top: 4),
                  child: Icon(Icons.chevron_right_rounded),
                ),
            ],
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) => Scaffold(
    appBar: AppBar(title: const Text('红果账号与点赞')),
    body: _invalidated || widget.store.locked
        ? Center(
            child: Padding(
              padding: const EdgeInsets.all(24),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                children: [
                  const Icon(Icons.lock_outline_rounded, size: 40),
                  const SizedBox(height: 12),
                  const Text('本地用户已切换或锁定，请返回后重新进入账号页面。'),
                  const SizedBox(height: 12),
                  FilledButton(
                    onPressed: () => Navigator.of(context).maybePop(),
                    child: const Text('返回'),
                  ),
                ],
              ),
            ),
          )
        : Center(
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 880),
              child: ListView.builder(
                padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
                itemCount: _authenticated ? _items.length + 3 : 1,
                itemBuilder: (context, index) {
                  if (index == 0) return _accountCard();
                  if (index == 1) return _likesHeader();
                  final itemIndex = index - 2;
                  if (itemIndex < _items.length) {
                    final item = _items[itemIndex];
                    return KeyedSubtree(
                      key: ValueKey(item.drama.id),
                      child: _likedTile(item),
                    );
                  }
                  if (_hasMore) {
                    return Padding(
                      padding: const EdgeInsets.symmetric(vertical: 12),
                      child: OutlinedButton(
                        onPressed: _loadingLikes || _busy || _sessionExpired
                            ? null
                            : () => _loadLikes(refresh: false),
                        child: const Text('加载更多点赞'),
                      ),
                    );
                  }
                  if (_likesLoaded && _items.isNotEmpty) {
                    return const Padding(
                      padding: EdgeInsets.all(12),
                      child: Center(child: Text('已显示全部可读取的点赞剧集')),
                    );
                  }
                  return const SizedBox.shrink();
                },
              ),
            ),
          ),
  );
}

class _LikedSeries {
  const _LikedSeries({
    required this.drama,
    required this.playable,
    required this.note,
  });

  final Drama drama;
  final bool playable;
  final String note;

  factory _LikedSeries.fromJson(Object? value) {
    if (value is! Map || value['series'] is! Map) {
      throw const FormatException('点赞剧集格式异常');
    }
    final series = value['series'] as Map;
    final id = series['seriesId'];
    final title = series['title'];
    if (id is! String ||
        !RegExp(r'^[0-9]{1,30}$').hasMatch(id) ||
        title is! String ||
        title.trim().isEmpty) {
      throw const FormatException('点赞剧集资料不完整');
    }
    return _LikedSeries(
      drama: Drama(
        id: 'hongguotv:$id',
        source: SourceSite.hongguotv.id,
        sourceId: id,
        title: title,
        description: series['intro'] is String ? series['intro'] as String : '',
        cover: series['cover'] is String ? series['cover'] as String : '',
        episodes: intValue(series['episodeCount']),
      ),
      playable: value['playable'] == true,
      note: value['note'] is String ? value['note'] as String : '',
    );
  }
}
