import argparse
import ast
import hashlib
import inspect
import os
import shutil
from datetime import datetime, timezone
from pathlib import Path


def install_hongguotv_quality(namespace):
    import json
    import os
    import re
    import subprocess
    import threading
    import time

    app = namespace['app']
    http_error = namespace['HTTPException']
    output_directory = namespace['H264_DIR']
    original_decrypted = namespace['_ensure_decrypted_original']
    locks = namespace['_dec_lock']
    slots = threading.BoundedSemaphore(2)
    supported = (360, 480, 540, 720, 1080)
    cache = {}
    cache_lock = threading.Lock()

    def video_dimensions(path):
        result = subprocess.run(
            ['ffprobe', '-v', 'error', '-select_streams', 'v:0',
             '-show_entries', 'stream=codec_name,width,height', '-of', 'json', path],
            check=True, capture_output=True, text=True, timeout=20,
        )
        streams = json.loads(result.stdout).get('streams') or []
        if not streams:
            raise http_error(422, '视频没有可读取的画面轨道')
        stream = streams[0]
        width, height = int(stream.get('width') or 0), int(stream.get('height') or 0)
        if min(width, height) <= 0:
            raise http_error(422, '视频分辨率无效')
        return width, height, stream.get('codec_name') or ''

    def target_size(width, height, quality):
        ratio = min(1.0, quality / min(width, height))
        return max(2, int(width * ratio) // 2 * 2), max(2, int(height * ratio) // 2 * 2)

    def validate_vid(vid):
        vid = str(vid)
        if not re.fullmatch(r'[0-9]{1,64}', vid):
            raise http_error(400, '视频标识无效')
        return vid

    def h264_path(vid, quality, height=720):
        vid = validate_vid(vid)
        if int(height) not in supported:
            raise http_error(400, '不支持的转码档位')
        safe_quality = re.sub(r'[^\w]', '', str(quality)) or 'best'
        return os.path.join(output_directory, f'{vid}_{safe_quality}_shortedge-v2_{int(height)}p.mp4')

    def ensure_decrypted(vid, quality='best'):
        validate_vid(vid)
        requested = re.sub(r'\D', '', str(quality))
        requested = int(requested) if requested else 0
        candidate = namespace['_downloaded_index']().get(str(vid))
        if candidate:
            try:
                width, height, _ = video_dimensions(candidate)
                if min(width, height) >= requested:
                    return candidate
            except Exception:
                pass
        path = original_decrypted(vid, quality)
        width, height, _ = video_dimensions(path)
        if requested and min(width, height) < requested:
            raise http_error(409, f'实际源视频仅 {width}×{height}，无法提供 {requested}P')
        return path

    def ensure_h264(src_path, vid, quality, height=720):
        out = h264_path(vid, quality, height)
        width, source_height, codec = video_dimensions(src_path)
        size = target_size(width, source_height, int(height))
        if codec == 'h264' and size == (width, source_height):
            return src_path
        os.makedirs(output_directory, exist_ok=True)

        def cached():
            if not os.path.isfile(out) or os.path.getsize(out) <= 0:
                return False
            try:
                return video_dimensions(out) == (*size, 'h264')
            except Exception:
                return False

        with locks(out):
            if cached():
                return out
            temporary = out + '.tmp.mp4'
            with slots:
                command = [
                    'ffmpeg', '-y', '-loglevel', 'error', '-i', src_path,
                    '-c:v', 'libx264', '-preset', 'veryfast', '-crf', '20', '-threads', '2',
                    '-vf', f'scale={size[0]}:{size[1]}',
                    '-c:a', 'aac', '-b:a', '128k', '-movflags', '+faststart', temporary,
                ]
                try:
                    subprocess.run(command, check=True, capture_output=True, timeout=2400)
                    if video_dimensions(temporary) != (*size, 'h264'):
                        raise http_error(500, '转码文件的实际尺寸或编码不符')
                    os.replace(temporary, out)
                except FileNotFoundError:
                    raise http_error(500, '服务器缺少 ffmpeg 或 ffprobe')
                except subprocess.CalledProcessError:
                    raise http_error(500, '高清视频转码失败')
                finally:
                    if os.path.isfile(temporary):
                        os.remove(temporary)
            return out

    def stream_transcode(src_path, out_path, vid, quality, height=720):
        return namespace['FileResponse'](
            ensure_h264(src_path, vid, quality, height), media_type='video/mp4',
        )

    @app.get('/stream/qualities')
    def stream_qualities(vid: str):
        vid = validate_vid(vid)
        with cache_lock:
            previous = cache.get(vid)
            if previous and time.monotonic() - previous[0] < 300:
                return previous[1]
        model = namespace['ODL']._video_model(vid)
        tracks = model.get('video_list') if isinstance(model, dict) else model
        items = {}
        for track in tracks or []:
            meta = track.get('video_meta') or {}
            digits = re.sub(r'\D', '', str(meta.get('definition') or ''))
            quality = int(digits) if digits else 0
            width, height = int(meta.get('vwidth') or 0), int(meta.get('vheight') or 0)
            if quality not in supported or min(width, height) < quality:
                continue
            items[quality] = {'quality': quality, 'width': width, 'height': height}
        if not items:
            raise http_error(502, '站源未返回可确认分辨率的视频档位')
        result = {'profile': 'short-edge-v2', 'items': [items[q] for q in sorted(items, reverse=True)]}
        with cache_lock:
            if len(cache) >= 1024:
                cache.pop(next(iter(cache)))
            cache[vid] = (time.monotonic(), result)
        return result

    namespace['_h264_path'] = h264_path
    namespace['_ensure_decrypted'] = ensure_decrypted
    namespace['_ensure_h264'] = ensure_h264
    namespace['_stream_transcode'] = stream_transcode
    return {'target_size': target_size, 'stream_qualities': stream_qualities,
            'video_dimensions': video_dimensions, 'ensure_h264': ensure_h264,
            'ensure_decrypted': ensure_decrypted, 'h264_path': h264_path}


def upgraded_source(source):
    activation = '\ninstall_hongguotv_quality(globals())\n'
    if activation in source:
        return source
    anchor = 'if __name__ == "__main__":'
    bypass = 'if codec == "h264" and not _is_downloaded(path):'
    if source.count(anchor) != 1 or source.count(bypass) != 1:
        raise ValueError('服务端源码结构不匹配，未修改')
    source = source.replace(bypass, 'if codec == "h264":')
    source = source.replace(anchor, inspect.getsource(install_hongguotv_quality) + activation + '\n' + anchor)
    ast.parse(source)
    return source


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('server', type=Path)
    parser.add_argument('--expected-sha256', required=True)
    parser.add_argument('--apply', action='store_true')
    options = parser.parse_args()
    path = options.server.resolve(strict=True)
    if options.server.is_symlink() or path.name != 'server.py':
        raise SystemExit('必须指向非符号链接的 server.py')
    original = path.read_bytes()
    if hashlib.sha256(original).hexdigest() != options.expected_sha256.lower():
        raise SystemExit('服务端文件已变化，未修改')
    replacement = upgraded_source(original.decode('utf-8')).encode('utf-8')
    if not options.apply:
        print('结构及语法检查通过；未修改服务端')
        return
    if replacement == original:
        print('已安装相同补丁，未重复修改')
        return
    stamp = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ')
    backup = path.with_name(path.name + '.before-1080-' + stamp)
    if backup.exists():
        raise SystemExit('备份路径已存在，未修改')
    shutil.copy2(path, backup)
    if backup.read_bytes() != original:
        raise SystemExit('备份校验失败，未修改')
    with path.open('r+b') as stream:
        stream.write(replacement)
        stream.truncate()
        stream.flush()
        os.fsync(stream.fileno())
    if path.read_bytes() != replacement:
        raise SystemExit('写入校验失败，请从备份恢复')
    print('备份：' + str(backup))
    print('新 SHA256：' + hashlib.sha256(replacement).hexdigest())


if __name__ == '__main__':
    main()
