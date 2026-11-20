package com.duanju.duanju_app

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.AtomicFile
import android.util.Base64
import java.io.File
import java.security.KeyStore
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

object HongguoAccountVault {
    private const val alias = "guoapp.hongguo.accounts.v1"

    @Synchronized
    fun storageKey(context: Context): String {
        val location = File(context.noBackupFilesDir, "hongguo-account-key")
        val file = AtomicFile(location)
        val store = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        val key = if (store.containsAlias(alias)) {
            store.getKey(alias, null) as SecretKey
        } else {
            if (location.exists() || File(location.path + ".bak").exists()) {
                error("账号保护密钥已失效")
            }
            KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore").apply {
                init(KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                    .setKeySize(256)
                    .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                    .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                    .build())
            }.generateKey()
        }
        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        val material: ByteArray
        if (location.exists() || File(location.path + ".bak").exists()) {
            val encoded = file.openRead().use { input ->
                val buffer = ByteArray(61)
                var count = 0
                while (count < buffer.size) {
                    val read = input.read(buffer, count, buffer.size - count)
                    if (read < 0) break
                    count += read
                }
                require(count == 60 && input.read() == -1)
                buffer.copyOf(count)
            }
            cipher.init(Cipher.DECRYPT_MODE, key, GCMParameterSpec(128, encoded.copyOfRange(0, 12)))
            cipher.updateAAD(alias.toByteArray(Charsets.UTF_8))
            material = cipher.doFinal(encoded.copyOfRange(12, encoded.size))
            require(material.size == 32)
        } else {
            material = ByteArray(32).also { SecureRandom().nextBytes(it) }
            cipher.init(Cipher.ENCRYPT_MODE, key)
            cipher.updateAAD(alias.toByteArray(Charsets.UTF_8))
            val encoded = cipher.iv + cipher.doFinal(material)
            require(encoded.size == 60)
            val stream = file.startWrite()
            try {
                stream.write(encoded)
                file.finishWrite(stream)
            } catch (error: Throwable) {
                file.failWrite(stream)
                material.fill(0)
                throw error
            }
        }
        return try { Base64.encodeToString(material, Base64.NO_WRAP) } finally { material.fill(0) }
    }
}
