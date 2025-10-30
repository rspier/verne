package com.example.silence

import android.widget.Button
import androidx.test.core.app.ActivityScenario
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class MainActivityTest {

    @Test
    fun playPauseButton_initialText_isPlay() {
        val scenario = ActivityScenario.launch(MainActivity::class.java)
        scenario.onActivity { activity ->
            val playPauseButton: Button = activity.findViewById(R.id.play_pause_button)
            assertEquals("Play", playPauseButton.text.toString())
        }
    }
}
