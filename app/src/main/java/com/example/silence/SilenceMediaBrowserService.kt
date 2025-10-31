package com.example.silence

import android.os.Bundle
import androidx.media3.common.MediaItem
import androidx.media3.common.MediaMetadata
import androidx.media3.session.LibraryResult
import androidx.media3.session.MediaLibraryService
import androidx.media3.session.MediaLibraryService.LibraryParams
import androidx.media3.session.MediaSession
import com.google.common.collect.ImmutableList
import androidx.media3.exoplayer.ExoPlayer
import com.google.common.util.concurrent.Futures
import com.google.common.util.concurrent.ListenableFuture

class SilenceMediaBrowserService : MediaLibraryService() {

    private lateinit var player: ExoPlayer
    private var mediaLibrarySession: MediaLibrarySession? = null

    override fun onCreate() {
        super.onCreate()
        player = ExoPlayer.Builder(this).build()
        mediaLibrarySession =
            MediaLibrarySession.Builder(this, player, object : MediaLibrarySession.Callback {
                override fun onGetLibraryRoot(
                    session: MediaLibrarySession,
                    browser: MediaSession.ControllerInfo,
                    params: LibraryParams?
                ): ListenableFuture<LibraryResult<MediaItem>> {
                    return Futures.immediateFuture(LibraryResult.ofItem(getRootItem(), params))
                }

                override fun onGetChildren(
                    session: MediaLibrarySession,
                    browser: MediaSession.ControllerInfo,
                    parentId: String,
                    page: Int,
                    pageSize: Int,
                    params: LibraryParams?
                ): ListenableFuture<LibraryResult<ImmutableList<MediaItem>>> {
                    val children = if (parentId == "root") {
                        getSilenceItem()
                    } else {
                        ImmutableList.of()
                    }
                    return Futures.immediateFuture(LibraryResult.ofItemList(children, params))
                }
            }).build()
    }

    override fun onGetSession(controllerInfo: MediaSession.ControllerInfo): MediaLibrarySession? {
        return mediaLibrarySession
    }

    private fun getRootItem(): MediaItem {
        val metadata = MediaMetadata.Builder()
            .setTitle("Silence")
            .setIsPlayable(false)
            .setIsBrowsable(true)
            .build()
        return MediaItem.Builder()
            .setMediaId("root")
            .setMediaMetadata(metadata)
            .build()
    }

    private fun getSilenceItem(): ImmutableList<MediaItem> {
        val metadata = MediaMetadata.Builder()
            .setTitle("Silence")
            .setArtist("System")
            .setIsPlayable(true)
            .setIsBrowsable(false)
            .build()
        val mediaItem = MediaItem.Builder()
            .setMediaId("silence")
            .setUri("android.resource://$packageName/${R.raw.silence}")
            .setMediaMetadata(metadata)
            .build()
        return ImmutableList.of(mediaItem)
    }
}
