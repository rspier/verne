package url.receiver

import android.content.Intent
import android.net.Uri
import android.widget.TextView
import androidx.test.core.app.ActivityScenario
import androidx.test.core.app.ApplicationProvider.getApplicationContext
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.Shadows
import org.robolectric.shadows.ShadowToast

@RunWith(AndroidJUnit4::class)
class MainActivityTest {

    @Test
    fun `when valid url is shared, it should open the browser with the modified url`() {
        val intent = Intent(Intent.ACTION_SEND).apply {
            type = "text/plain"
            putExtra(Intent.EXTRA_TEXT, "https://www.google.com")
        }

        val scenario = ActivityScenario.launch<MainActivity>(intent)
        scenario.onActivity { activity ->
            val shadowActivity = Shadows.shadowOf(activity)
            val nextStartedActivity = shadowActivity.nextStartedActivity
            assertNotNull("Expected an activity to be started", nextStartedActivity)
            assertEquals(Intent.ACTION_VIEW, nextStartedActivity.action)
            assertEquals("https://archive.is/newest/https://www.google.com", nextStartedActivity.data.toString())
            assertTrue("Expected the activity to finish", activity.isFinishing)
        }
    }

    @Test
    fun `when invalid url is shared, it should show an error toast`() {
        val intent = Intent(Intent.ACTION_SEND).apply {
            type = "text/plain"
            putExtra(Intent.EXTRA_TEXT, "not a valid url")
        }

        val scenario = ActivityScenario.launch<MainActivity>(intent)
        scenario.onActivity { activity ->
            assertEquals("Invalid URL", ShadowToast.getTextOfLatestToast())
            assertTrue("Expected the activity to finish", activity.isFinishing)
        }
    }

    @Test
    fun `when app is launched directly, it should show the main layout`() {
        val intent = Intent(getApplicationContext(), MainActivity::class.java)
        val scenario = ActivityScenario.launch<MainActivity>(intent)
        scenario.onActivity { activity ->
            val textView = activity.findViewById<TextView>(R.id.welcome_text)
            assertNotNull(textView)
            assertEquals("Welcome to URL Receiver!", textView.text)
        }
    }
}
