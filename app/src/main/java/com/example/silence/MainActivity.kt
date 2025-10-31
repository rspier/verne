package com.example.silence

import android.content.ComponentName
import androidx.appcompat.app.AppCompatActivity
import android.os.Bundle
import android.widget.Button
import androidx.media3.common.MediaItem
import androidx.media3.session.MediaController
import androidx.media3.session.SessionToken
import com.google.common.util.concurrent.MoreExecutors

class MainActivity : AppCompatActivity() {

    private var isPlaying = false
    private lateinit var mediaController: MediaController

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)

        val playPauseButton: Button = findViewById(R.id.play_pause_button)
        playPauseButton.setOnClickListener {
            if (isPlaying) {
                mediaController.pause()
                playPauseButton.text = "Play"
            } else {
                val silenceMediaItem = MediaItem.Builder()
                    .setMediaId("silence")
                    .build()
                mediaController.setMediaItem(silenceMediaItem)
                mediaController.prepare()
                mediaController.play()
                playPauseButton.text = "Pause"
            }
            isPlaying = !isPlaying
        }

        val sessionToken = SessionToken(this, ComponentName(this, SilenceMediaBrowserService::class.java))
        val controllerFuture = MediaController.Builder(this, sessionToken).buildAsync()
        controllerFuture.addListener(
            {
                mediaController = controllerFuture.get()
            },
            MoreExecutors.directExecutor()
        )
    }
}