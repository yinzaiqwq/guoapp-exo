import json
import subprocess
import tempfile
import threading
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from hongguotv_quality_support import install_hongguotv_quality, upgraded_source


class HttpError(Exception):
    def __init__(self, status_code, detail):
        self.status_code = status_code
        self.detail = detail
        super().__init__(detail)


class App:
    def get(self, path):
        return lambda handler: handler


class QualitySupportTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.tracks = [
            {'video_meta': {'definition': '1080p', 'vwidth': 1080, 'vheight': 1920}},
            {'video_meta': {'definition': '720p', 'vwidth': 720, 'vheight': 1280}},
        ]
        self.namespace = {
            'app': App(), 'HTTPException': HttpError, 'H264_DIR': self.directory.name,
            'STREAM_CACHE': self.directory.name,
            'ODL': SimpleNamespace(_video_model=lambda vid: {'video_list': self.tracks}),
            '_ensure_decrypted_original': lambda vid, quality: 'original.mp4',
            '_downloaded_index': lambda: {}, '_dec_lock': lambda key: threading.Lock(),
            'FileResponse': lambda path, **kwargs: path,
        }
        self.support = install_hongguotv_quality(self.namespace)

    @staticmethod
    def probe(width, height, codec='hevc'):
        return SimpleNamespace(stdout=json.dumps({'streams': [
            {'width': width, 'height': height, 'codec_name': codec},
        ]}))

    def test_preserves_portrait_landscape_and_never_upscales(self):
        size = self.support['target_size']
        self.assertEqual(size(1080, 1920, 1080), (1080, 1920))
        self.assertEqual(size(1920, 1080, 1080), (1920, 1080))
        self.assertEqual(size(1080, 1920, 720), (720, 1280))
        self.assertEqual(size(720, 1280, 1080), (720, 1280))
        self.assertEqual(size(480, 854, 360), (360, 640))

    def test_lists_only_confirmed_real_qualities(self):
        self.tracks.extend([
            {'video_meta': {'definition': '1080p', 'vwidth': 406, 'vheight': 720}},
            {'video_meta': {'definition': '540p'}},
        ])
        result = self.support['stream_qualities']('123')
        self.assertEqual(result['profile'], 'short-edge-v2')
        self.assertEqual([item['quality'] for item in result['items']], [1080, 720])
        self.assertEqual(result['items'][0]['width'], 1080)
        self.tracks.clear()
        self.assertEqual(self.support['stream_qualities']('123'), result)

    def test_empty_metadata_is_not_invented(self):
        self.tracks.clear()
        with self.assertRaises(HttpError) as raised:
            self.support['stream_qualities']('456')
        self.assertEqual(raised.exception.status_code, 502)

    def test_cache_is_versioned_and_ids_are_validated(self):
        path = self.support['h264_path']('123', '1080p', 1080)
        self.assertTrue(path.endswith('123_1080p_shortedge-v2_1080p.mp4'))
        with self.assertRaises(HttpError):
            self.support['h264_path']('../escape', 'best', 1080)
        with self.assertRaises(HttpError):
            self.support['h264_path']('123', 'best', 9999)

    def test_low_resolution_download_is_not_reused_for_1080(self):
        self.namespace['_downloaded_index'] = lambda: {'123': 'old720.mp4'}
        with patch.object(subprocess, 'run', side_effect=[self.probe(720, 1280), self.probe(1080, 1920)]):
            self.assertEqual(self.support['ensure_decrypted']('123', '1080p'), 'original.mp4')

    def test_low_resolution_original_is_rejected(self):
        with patch.object(subprocess, 'run', return_value=self.probe(720, 1280)):
            with self.assertRaises(HttpError) as raised:
                self.support['ensure_decrypted']('123', '1080p')
        self.assertEqual(raised.exception.status_code, 409)

    def test_h264_original_can_be_reused_without_transcode(self):
        with patch.object(subprocess, 'run', return_value=self.probe(1080, 1920, 'h264')) as run:
            self.assertEqual(self.support['ensure_h264']('source.mp4', '123', '1080p', 1080), 'source.mp4')
            self.assertEqual(run.call_count, 1)

    def test_transcode_verifies_actual_output_before_publishing(self):
        calls = []

        def run(command, **kwargs):
            calls.append(command)
            if command[0] == 'ffmpeg':
                Path(command[-1]).write_bytes(b'synthetic')
                return SimpleNamespace()
            return self.probe(1080, 1920, 'h264' if '.tmp.mp4' in command[-1] else 'hevc')

        with patch.object(subprocess, 'run', side_effect=run):
            result = self.support['ensure_h264']('source.mp4', '123', '1080p', 1080)
        command = next(command for command in calls if command[0] == 'ffmpeg')
        self.assertEqual(command[command.index('-vf') + 1], 'scale=1080:1920')
        self.assertTrue(Path(result).is_file())

    def test_wrong_output_is_not_published(self):
        def run(command, **kwargs):
            if command[0] == 'ffmpeg':
                Path(command[-1]).write_bytes(b'synthetic')
                return SimpleNamespace()
            if '.tmp.mp4' in command[-1]:
                return self.probe(406, 720, 'h264')
            return self.probe(1080, 1920)

        with patch.object(subprocess, 'run', side_effect=run):
            with self.assertRaises(HttpError):
                self.support['ensure_h264']('source.mp4', '123', '1080p', 1080)
        self.assertEqual(list(Path(self.directory.name).iterdir()), [])

    def test_patch_preserves_other_code_and_is_idempotent(self):
        source = 'if codec == "h264" and not _is_downloaded(path):\n    pass\n\nif __name__ == "__main__":\n    pass\n'
        upgraded = upgraded_source(source)
        self.assertIn('if codec == "h264":', upgraded)
        self.assertEqual(upgraded_source(upgraded), upgraded)
        with self.assertRaises(ValueError):
            upgraded_source('unrelated = 1\n')

    def test_updates_previously_installed_patch(self):
        source = 'def install_hongguotv_quality(namespace):\n    return {}\n\ninstall_hongguotv_quality(globals())\n\nif __name__ == "__main__":\n    pass\n'
        updated = upgraded_source(source)
        self.assertIn("@app.get('/stream/cache')", updated)
        self.assertEqual(updated.count('def install_hongguotv_quality('), 1)
        self.assertEqual(updated, upgraded_source(updated))

    def cache_probe(self, width=1920, height=1080, codec='hevc'):
        result = json.loads(self.probe(width, height, codec).stdout)
        result['format'] = {'duration': '100.0'}
        return SimpleNamespace(stdout=json.dumps(result))

    def test_reads_legacy_best_cache_without_downloading_or_transcoding(self):
        path = Path(self.directory.name) / '123_best.mp4'
        path.write_bytes(b'synthetic')
        self.namespace['_ensure_decrypted_original'] = lambda *args: self.fail('must not download')
        with patch.object(subprocess, 'run', return_value=self.cache_probe()) as run:
            info = self.support['stream_cache']('123', 1080)
            self.assertTrue(info['hit'])
            self.assertNotIn('path', info)
            self.assertEqual(info['quality'], 1080)
            self.assertEqual(self.support['stream_cache_file']('123', info['cacheId']), str(path))
            self.assertEqual(self.support['ensure_decrypted']('123', '1080p'), str(path))
            self.assertTrue(all(call.args[0][0] == 'ffprobe' for call in run.call_args_list))

    def test_missing_partial_empty_and_other_video_are_not_hits(self):
        for name, data in [('123_best.mp4', b''), ('123_best.mp4.tmp.mp4', b'x'),
                           ('123_best.mp4.enc', b'x'), ('1234_best.mp4', b'x')]:
            (Path(self.directory.name) / name).write_bytes(data)
        with patch.object(subprocess, 'run', side_effect=AssertionError('must not probe unrelated or partial files')):
            self.assertFalse(self.support['stream_cache']('123', 1080)['hit'])

    def test_validates_dimensions_not_legacy_cache_filename(self):
        (Path(self.directory.name) / '123_best_1080p.mp4').write_bytes(b'synthetic')
        with patch.object(subprocess, 'run', return_value=self.cache_probe(406, 720, 'h264')):
            self.assertFalse(self.support['stream_cache']('123', 1080)['hit'])
            info = self.support['stream_cache']('123', 0)
            self.assertEqual(info['quality'], 360)

    def test_changed_or_deleted_cache_is_not_silently_replaced(self):
        path = Path(self.directory.name) / '123_best.mp4'
        path.write_bytes(b'synthetic')
        with patch.object(subprocess, 'run', return_value=self.cache_probe()):
            info = self.support['stream_cache']('123', 1080)
            path.unlink()
            with self.assertRaises(HttpError) as raised:
                self.support['stream_cache_file']('123', info['cacheId'])
            self.assertEqual(raised.exception.status_code, 404)
            with self.assertRaises(HttpError):
                self.support['stream_cache_file']('../escape', info['cacheId'])
            with self.assertRaises(HttpError):
                self.support['stream_cache_file']('123', '../escape')

    def test_corrupt_cache_and_missing_duration_are_not_hits(self):
        (Path(self.directory.name) / '123_best.mp4').write_bytes(b'synthetic')
        with patch.object(subprocess, 'run', side_effect=subprocess.CalledProcessError(1, 'ffprobe')):
            self.assertFalse(self.support['stream_cache']('123')['hit'])
        with patch.object(subprocess, 'run', return_value=self.probe(1920, 1080)):
            self.assertFalse(self.support['stream_cache']('123')['hit'])

    def test_writing_cache_is_skipped(self):
        path = Path(self.directory.name) / '123_best.mp4'
        path.write_bytes(b'synthetic')
        active = threading.Lock()
        active.acquire()
        namespace = dict(self.namespace, _dec_lock=lambda key: active)
        support = install_hongguotv_quality(namespace)
        try:
            with patch.object(subprocess, 'run', side_effect=AssertionError('must not probe writing file')):
                self.assertFalse(support['stream_cache']('123')['hit'])
        finally:
            active.release()


if __name__ == '__main__':
    unittest.main()
