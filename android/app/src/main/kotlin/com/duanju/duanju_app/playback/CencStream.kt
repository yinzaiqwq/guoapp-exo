package com.duanju.duanju_app.playback

import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL

/**
 * 播放前拉取 MP4 头部（ftyp + moov）并建立 CENC 样本索引。
 *
 * 取流走本机 Go 代理（已按 Range 转发上游密文），这里只做一次 256 KiB
 * 的头部读取；moov 超出该窗口时再补一次 Range 请求。索引建好后由
 * [CencDataSource] 在 ExoPlayer 读入的缓冲区上就地解密。
 */
internal class CencStream private constructor(
    private val index: CencIndex,
    val total: Long,
) {
    fun decryptInto(buffer: ByteArray, offset: Int, count: Int, position: Long, key: ByteArray) {
        index.decryptInto(buffer, offset, count, position, key)
    }

    companion object {
        private const val HEAD_WINDOW = 256L * 1024
        private const val MAX_RANGE = 8L * 1024 * 1024
        private val CONTENT_RANGE = Regex("bytes (\\d+)-(\\d+)/(\\d+)")

        fun prepare(url: String, referer: String): CencStream {
            val first = fetch(url, referer, 0L, HEAD_WINDOW - 1)
            val end = CencIndex.headerEnd(first.bytes, first.total)
            val header = if (end > first.bytes.size) {
                first.bytes + fetch(url, referer, first.bytes.size.toLong(), end - 1L).bytes
            } else {
                first.bytes.copyOf(end)
            }
            val index = CencIndex.parse(header)
            if (index.hasSampleBeyond(first.total)) throw IOException("视频样本超出资源大小")
            return CencStream(index, first.total)
        }

        private class Fetched(val bytes: ByteArray, val total: Long)

        private fun fetch(url: String, referer: String, start: Long, end: Long): Fetched {
            if (start < 0 || end < start || end - start >= MAX_RANGE) throw IOException("视频分段范围无效")
            val connection = (URL(url).openConnection() as HttpURLConnection).apply {
                requestMethod = "GET"
                connectTimeout = 15_000
                readTimeout = 30_000
                instanceFollowRedirects = true
                setRequestProperty("Range", "bytes=$start-$end")
                setRequestProperty("Accept-Encoding", "identity")
                if (referer.isNotEmpty()) setRequestProperty("Referer", referer)
            }
            try {
                val code = connection.responseCode
                if (code != HttpURLConnection.HTTP_PARTIAL) throw IOException("视频服务器不支持分段读取（HTTP $code）")
                val header = connection.getHeaderField("Content-Range")?.trim()
                    ?: throw IOException("视频分段响应异常")
                val match = CONTENT_RANGE.matchEntire(header) ?: throw IOException("视频分段响应异常")
                val (begin, finish, size) = match.destructured
                val total = size.toLong()
                val last = minOf(end, total - 1)
                if (total <= 0 || begin.toLong() != start || finish.toLong() != last) {
                    throw IOException("视频分段范围不匹配")
                }
                val expected = (last - start + 1).toInt()
                val bytes = ByteArray(expected)
                val input = connection.inputStream
                var offset = 0
                while (offset < bytes.size) {
                    val read = input.read(bytes, offset, bytes.size - offset)
                    if (read < 0) throw IOException("视频分段提前结束")
                    offset += read
                }
                return Fetched(bytes, total)
            } finally {
                connection.disconnect()
            }
        }
    }
}
