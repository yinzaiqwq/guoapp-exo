package com.duanju.duanju_app.playback

import java.io.IOException
import java.math.BigInteger
import java.util.Arrays
import javax.crypto.Cipher
import javax.crypto.spec.IvParameterSpec
import javax.crypto.spec.SecretKeySpec

/**
 * ISO BMFF + CENC(cenc-aes-ctr) 索引与样本级解密。
 *
 * 红果媒体是 MP4：moov 内的 saiz/saio 记录每个样本（或子样本）的 IV，
 * 载荷以 AES-128-CTR 加密。CTR 不改变字节长度，所以可以在 ExoPlayer
 * 读入的缓冲区上就地解密，Range/seek 语义与明文文件完全一致。
 *
 * 头部同时改写：把 encv/enca 还原成 frma 中记录的原始编码，并把 sinf
 * 置为 free。这样解密后的 MP4 会被解码器当作普通 AVC/HEVC 轨道解析，
 * 而不是等待 MediaDrm 的加密轨道。
 */
internal class CencIndex private constructor(
    private val header: ByteArray,
    private val samples: List<Sample>,
) {
    data class Sample(
        val start: Long,
        val end: Long,
        val iv: ByteArray,
        val encryptedOffset: Long,
    )

    /** 索引中是否存在超出资源大小的样本，用于识别被截断的响应。 */
    fun hasSampleBeyond(total: Long): Boolean = samples.any { it.end > total }

    /**
     * 就地解密 [buffer] 中从文件偏移 [position] 开始的 [count] 字节。
     * 与头部重叠的部分会用改写后的头部覆盖。
     */
    fun decryptInto(buffer: ByteArray, offset: Int, count: Int, position: Long, key: ByteArray) {
        if (count <= 0) return
        val start = position
        val end = position + count

        if (start < header.size) {
            val from = start.toInt()
            val to = minOf(end, header.size.toLong()).toInt()
            if (to > from) header.copyInto(buffer, offset, from, to)
        }
        if (key.size != 16 || samples.isEmpty()) return

        // 二分定位第一个 end > start 的样本
        var low = 0
        var high = samples.size
        while (low < high) {
            val middle = (low + high) ushr 1
            if (samples[middle].end <= start) low = middle + 1 else high = middle
        }

        // 一次读取通常跨多个样本，Cipher 复用后每次 init 重置计数器
        val cipher = Cipher.getInstance("AES/CTR/NoPadding")
        val spec = SecretKeySpec(key, "AES")

        for (index in low until samples.size) {
            val sample = samples[index]
            if (sample.start >= end) break
            val begin = maxOf(start, sample.start)
            val stop = minOf(end, sample.end)
            if (begin >= stop) continue

            // 样本内的字节位置决定 CTR 计数器与块内偏移
            val relative = begin - sample.start + sample.encryptedOffset
            val skip = (relative % 16).toInt()
            val counter = (BigInteger(1, sample.iv) + BigInteger.valueOf(relative / 16)).and(MAX_COUNTER)
            val encoded = counter.toByteArray()
            val iv = ByteArray(16)
            encoded.copyInto(iv, maxOf(0, 16 - encoded.size), maxOf(0, encoded.size - 16), encoded.size)
            cipher.init(Cipher.DECRYPT_MODE, spec, IvParameterSpec(iv))

            val target = offset + (begin - start).toInt()
            val length = (stop - begin).toInt()
            if (skip == 0) {
                cipher.doFinal(buffer, target, length, buffer, target)
            } else {
                // 前置 skip 个 0 把密钥流对齐到块内偏移，输出时再丢弃这部分
                val plain = cipher.doFinal(ByteArray(skip) + buffer.copyOfRange(target, target + length))
                plain.copyInto(buffer, target, skip, skip + length)
            }
        }
    }

    companion object {
        private const val MAX_INDEX = 200_000
        private const val MAX_HEADER = 8 * 1024 * 1024
        private val MAX_COUNTER: BigInteger = BigInteger.ONE.shiftLeft(128).subtract(BigInteger.ONE)

        /**
         * 返回 moov 结束处的偏移；红果媒体要求索引位于文件开头。
         *
         * ftyp 与 moov 之间可能夹着 free / skip / wide 等填充 box，因此按
         * 顶层 box 逐个前进，而不是假设 moov 紧跟在 ftyp 之后。
         */
        fun headerEnd(first: ByteArray, total: Long): Int {
            if (first.size < 16) throw IOException("视频容器头部过短")
            if (String(first, 4, 4, Charsets.US_ASCII) != "ftyp") throw IOException("不支持的视频容器")
            val moov = topLevelMoov(first)
            val end = moov.end.toLong()
            if (end > total || end > MAX_HEADER.toLong()) throw IOException("视频索引大小异常")
            return end.toInt()
        }

        /** 在顶层 box 中定位 moov；中间只允许 ftyp / free / skip / wide 填充。 */
        private fun topLevelMoov(data: ByteArray): Box {
            var at = 0
            while (true) {
                val current = box(data, at, data.size) ?: throw IOException("视频索引不完整")
                when (current.type) {
                    "moov" -> return current
                    "ftyp", "free", "skip", "wide" -> at = current.end
                    else -> throw IOException("视频索引不在文件开头")
                }
            }
        }

        fun parse(header: ByteArray): CencIndex {
            val ftyp = box(header, 0, header.size) ?: throw IOException("视频容器头部无效")
            if (ftyp.type != "ftyp") throw IOException("不支持的视频容器")
            // 与 headerEnd 使用同一套定位规则，避免两处对填充 box 的处理不一致
            val moov = topLevelMoov(header)

            val patched = header.copyOf()
            val samples = mutableListOf<Sample>()

            var trackAt = moov.body
            while (trackAt < moov.end) {
                val track = box(header, trackAt, moov.end) ?: throw IOException("视频轨道格式异常")
                trackAt = track.end
                if (track.type != "trak") continue

                val stbl = find(header, "stbl", track.body, track.end) ?: continue
                val saiz = find(header, "saiz", stbl.body, stbl.end) ?: continue
                val saio = find(header, "saio", stbl.body, stbl.end) ?: throw IOException("视频加密索引缺失")
                val stsz = find(header, "stsz", stbl.body, stbl.end) ?: throw IOException("样本索引缺失")
                val stsc = find(header, "stsc", stbl.body, stbl.end) ?: throw IOException("块索引缺失")
                val chunks = find(header, "stco", stbl.body, stbl.end)
                    ?: find(header, "co64", stbl.body, stbl.end)
                    ?: throw IOException("块偏移缺失")

                val sampleCount = count(header, stsz.body + 8, stsz.end)
                val defaultSize = u32(header, stsz.body + 4)
                if (defaultSize == 0L && stsz.body + 12 + sampleCount.toLong() * 4 > stsz.end) {
                    throw IOException("样本索引截断")
                }
                val sizes = LongArray(sampleCount) {
                    if (defaultSize > 0) defaultSize else u32(header, stsz.body + 12 + it * 4)
                }

                val chunkCount = count(header, chunks.body + 4, chunks.end)
                val offsetWidth = if (chunks.type == "co64") 8 else 4
                if (chunks.body + 8 + chunkCount.toLong() * offsetWidth > chunks.end) throw IOException("块偏移截断")
                val offsets = LongArray(chunkCount) {
                    val at = chunks.body + 8 + it * offsetWidth
                    if (offsetWidth == 8) i64(header, at) else u32(header, at)
                }

                val entryCount = count(header, stsc.body + 4, stsc.end)
                if (entryCount == 0 || stsc.body + 8 + entryCount.toLong() * 12 > stsc.end) {
                    throw IOException("块索引截断")
                }
                val entries = (0 until entryCount).map {
                    u32(header, stsc.body + 8 + it * 12) to u32(header, stsc.body + 12 + it * 12)
                }

                // saiz/saio 的 flags 位 0 会插入 aux_info_type 与参数两个字段
                val sizeBase = saiz.body + 4 + (if ((i32(header, saiz.body) and 1) != 0) 8 else 0)
                if (sizeBase + 5 > saiz.end) throw IOException("辅助索引截断")
                val auxDefault = header[sizeBase].toInt() and 0xff
                val auxCount = count(header, sizeBase + 1, saiz.end)
                if (auxCount != sampleCount || (auxDefault == 0 && sizeBase + 5 + sampleCount > saiz.end)) {
                    throw IOException("辅助索引数量不一致")
                }
                val auxSizes = IntArray(sampleCount) {
                    if (auxDefault > 0) auxDefault else (header[sizeBase + 5 + it].toInt() and 0xff)
                }

                val offsetBase = saio.body + 4 + (if ((i32(header, saio.body) and 1) != 0) 8 else 0)
                val pointerWidth = if ((header[saio.body].toInt() and 0xff) == 1) 8 else 4
                if (count(header, offsetBase, saio.end) != 1 || offsetBase + 4 + pointerWidth > saio.end) {
                    throw IOException("不支持的辅助索引布局")
                }
                var aux = if (pointerWidth == 8) i64(header, offsetBase + 4) else u32(header, offsetBase + 4)
                val auxEnd = aux + auxSizes.sumOf { it.toLong() }
                if (aux < 0 || auxEnd > header.size) throw IOException("辅助数据超出索引范围")

                var sampleIndex = 0
                var entry = 0
                for (chunk in offsets.indices) {
                    while (entry + 1 < entries.size && entries[entry + 1].first <= (chunk + 1).toLong()) entry++
                    val chunkSamples = entries[entry].second
                    if (chunkSamples > (sampleCount - sampleIndex).toLong() || entries[entry].first > (chunk + 1).toLong()) {
                        throw IOException("样本与块数量不一致")
                    }
                    var position = offsets[chunk]
                    var pending = chunkSamples.toInt()
                    while (pending > 0) {
                        val length = sizes[sampleIndex]
                        val auxSize = auxSizes[sampleIndex]
                        // 先校验长度与辅助数据范围，再读取 IV，避免畸形索引越界
                        if (position < 0 || length <= 0 || position > Long.MAX_VALUE - length || auxSize < 8) {
                            throw IOException("样本索引异常")
                        }
                        if (aux + auxSize > header.size) throw IOException("辅助数据超出索引范围")
                        val iv = ByteArray(16)
                        header.copyInto(iv, 0, aux.toInt(), aux.toInt() + 8)

                        if (auxSize == 8) {
                            samples.add(Sample(position, position + length, iv, 0L))
                        } else {
                            if (auxSize < 10) throw IOException("不支持的加密样本格式")
                            val at = aux.toInt()
                            val subs = ((header[at + 8].toInt() and 0xff) shl 8) or (header[at + 9].toInt() and 0xff)
                            if (auxSize != 10 + subs * 6) throw IOException("加密子样本索引异常")
                            var cursor = position
                            var encrypted = 0L
                            for (sub in 0 until subs) {
                                val subAt = at + 10 + sub * 6
                                val clear = ((header[subAt].toInt() and 0xff) shl 8) or (header[subAt + 1].toInt() and 0xff)
                                val size = u32(header, subAt + 2)
                                cursor += clear
                                if (cursor + size > position + length) throw IOException("加密子样本越界")
                                if (size > 0) samples.add(Sample(cursor, cursor + size, iv, encrypted))
                                encrypted += size
                                cursor += size
                            }
                            if (cursor != position + length) throw IOException("加密子样本长度异常")
                        }

                        position += length
                        aux += auxSize
                        sampleIndex++
                        pending--
                    }
                }
                if (sampleIndex != sampleCount) throw IOException("视频样本不完整")

                // 还原原始编码并移除 sinf，让明文 MP4 按普通轨道解析
                var scan = stbl.body
                while (scan + 8 <= stbl.end) {
                    val sinf = find(header, "sinf", scan, stbl.end) ?: break
                    val frma = find(header, "frma", sinf.body, sinf.end) ?: throw IOException("原始编码格式缺失")
                    if (frma.body + 4 > frma.end) throw IOException("原始编码格式无效")
                    val original = header.copyOfRange(frma.body, frma.body + 4)
                    var entryAt = sinf.start - 1
                    while (entryAt >= stbl.body) {
                        val type = String(header, entryAt, 4, Charsets.US_ASCII)
                        if (type == "encv" || type == "enca") {
                            original.copyInto(patched, entryAt)
                            break
                        }
                        entryAt--
                    }
                    FREE.copyInto(patched, sinf.start + 4)
                    Arrays.fill(patched, sinf.body, sinf.end, 0.toByte())
                    scan = sinf.end
                }
            }

            if (samples.isEmpty()) throw IOException("视频加密索引不可用")
            val sorted = samples.sortedBy { it.start }
            for (index in 1 until sorted.size) {
                if (sorted[index - 1].end > sorted[index].start) throw IOException("视频样本重叠")
            }
            return CencIndex(patched, sorted)
        }

        private val FREE = "free".toByteArray(Charsets.US_ASCII)

        private class Box(val start: Int, val end: Int, val body: Int, val type: String)

        private fun u32(data: ByteArray, at: Int): Long =
            ((data[at].toLong() and 0xff) shl 24) or
                ((data[at + 1].toLong() and 0xff) shl 16) or
                ((data[at + 2].toLong() and 0xff) shl 8) or
                (data[at + 3].toLong() and 0xff)

        private fun i32(data: ByteArray, at: Int): Int =
            ((data[at].toInt() and 0xff) shl 24) or
                ((data[at + 1].toInt() and 0xff) shl 16) or
                ((data[at + 2].toInt() and 0xff) shl 8) or
                (data[at + 3].toInt() and 0xff)

        private fun i64(data: ByteArray, at: Int): Long {
            var value = 0L
            for (step in 0 until 8) value = (value shl 8) or (data[at + step].toLong() and 0xff)
            return value
        }

        private fun box(data: ByteArray, at: Int, limit: Int): Box? {
            if (at < 0 || at + 8 > limit) return null
            var size = u32(data, at)
            var head = 8
            if (size == 1L) {
                if (at + 16 > limit) return null
                size = i64(data, at + 8)
                head = 16
            }
            if (size == 0L) size = (limit - at).toLong()
            if (size < head || size > (limit - at).toLong()) return null
            return Box(at, at + size.toInt(), at + head, String(data, at + 4, 4, Charsets.US_ASCII))
        }

        private fun find(data: ByteArray, type: String, start: Int, limit: Int): Box? {
            val code = type[0].code
            var at = start
            while (at + 8 <= limit) {
                if ((data[at + 4].toInt() and 0xff) == code) {
                    val candidate = box(data, at, limit)
                    if (candidate != null && candidate.type == type) return candidate
                }
                at++
            }
            return null
        }

        private fun count(data: ByteArray, at: Int, limit: Int): Int {
            if (at + 4 > limit) throw IOException("索引越界")
            val value = u32(data, at)
            if (value > MAX_INDEX) throw IOException("索引数量过大")
            return value.toInt()
        }
    }
}
