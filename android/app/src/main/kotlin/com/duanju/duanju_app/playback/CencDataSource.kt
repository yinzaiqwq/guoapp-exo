package com.duanju.duanju_app.playback

import android.net.Uri
import androidx.media3.common.C
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.BaseDataSource
import androidx.media3.datasource.DataSource
import androidx.media3.datasource.DataSpec
import java.io.IOException

/**
 * 面向加密 MP4 的 ExoPlayer 数据源工厂。
 *
 * 底层沿用 Media3 的 HTTP 数据源，因此 Range、重定向、连接池与超时策略和普通
 * 播放完全一致；读到的密文在返回给解码器之前由 [CencStream] 就地解密。
 *
 * 索引只在首次打开时建立，之后的 seek 复用同一份，因此拖动进度不会重复解析
 * moov；不缓存整个文件，也不建立监听端口。
 *
 * 索引建立放在数据源的 [CencDataSource.open] 而不是 [createDataSource] 里，
 * 因为前者抛 IOException 会被 ExoPlayer 当作 IO 错误上报并触发重试/降级。
 */
@UnstableApi
class CencDataSourceFactory(
    private val upstreamFactory: DataSource.Factory,
    private val url: String,
    private val referer: String,
    private val key: ByteArray,
) : DataSource.Factory {

    @Volatile
    private var prepared: CencStream? = null

    @Volatile
    private var failure: IOException? = null

    override fun createDataSource(): DataSource = CencDataSource(this)

    internal fun openUpstream(): DataSource = upstreamFactory.createDataSource()

    internal fun contentKey(): ByteArray = key

    @Throws(IOException::class)
    internal fun preparedStream(): CencStream {
        prepared?.let { return it }
        synchronized(this) {
            prepared?.let { return it }
            failure?.let { throw it }
            return try {
                CencStream.prepare(url, referer).also { prepared = it }
            } catch (error: Exception) {
                val wrapped = error as? IOException ?: IOException("视频索引解析失败", error)
                failure = wrapped
                throw wrapped
            }
        }
    }
}

/**
 * 单个读取通道：把上游 HTTP 数据源读到的密文解密后再交给解码器。
 * 每次 open 都新建一个，索引与密钥来自工厂。
 */
@UnstableApi
internal class CencDataSource(
    private val factory: CencDataSourceFactory,
) : BaseDataSource(true) {
    private var upstream: DataSource? = null
    private var stream: CencStream? = null
    private var position = 0L
    private var remaining = 0L
    private var opened = false
    private var uri: Uri? = null

    override fun open(dataSpec: DataSpec): Long {
        transferInitializing(dataSpec)
        uri = dataSpec.uri
        // 索引缺失或损坏时在这里抛 IOException，交由 ExoPlayer 统一处理
        val prepared = factory.preparedStream()
        val start = dataSpec.position
        if (start < 0 || start > prepared.total) throw IOException("读取位置超出视频长度")
        remaining = if (dataSpec.length == C.LENGTH_UNSET.toLong()) {
            prepared.total - start
        } else {
            minOf(dataSpec.length, prepared.total - start)
        }
        position = start
        val target = factory.openUpstream()
        upstream = target
        stream = prepared
        // 原样交给上游：position/length/flags 都已是我们想要的语义，
        // 读取量在 read() 里按 remaining 收敛
        val reported = target.open(dataSpec)
        // 上游给出的长度更小时以它为准，避免读出资源尾部之外的内容
        if (reported != C.LENGTH_UNSET.toLong() && reported < remaining) remaining = reported
        opened = true
        transferStarted(dataSpec)
        return remaining
    }

    override fun read(buffer: ByteArray, offset: Int, length: Int): Int {
        if (length == 0) return 0
        if (remaining == 0L) return C.RESULT_END_OF_INPUT
        val target = upstream ?: return C.RESULT_END_OF_INPUT
        val prepared = stream ?: return C.RESULT_END_OF_INPUT
        val read = target.read(buffer, offset, minOf(length.toLong(), remaining).toInt())
        if (read == C.RESULT_END_OF_INPUT) return read
        if (read > 0) {
            prepared.decryptInto(buffer, offset, read, position, factory.contentKey())
            position += read
            remaining -= read
            bytesTransferred(read)
        }
        return read
    }

    override fun getUri(): Uri? = uri

    override fun close() {
        uri = null
        stream = null
        val target = upstream
        upstream = null
        if (opened) {
            opened = false
            try {
                target?.close()
            } finally {
                transferEnded()
            }
        } else {
            target?.close()
        }
    }
}
