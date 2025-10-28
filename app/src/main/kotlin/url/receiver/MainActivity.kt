package url.receiver

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.util.Patterns
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity

class MainActivity : AppCompatActivity() {

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        handleIntent(intent)
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        handleIntent(intent)
    }

    private fun handleIntent(intent: Intent) {
        if (intent.action == Intent.ACTION_SEND && intent.type == "text/plain") {
            val url = extractUrl(intent)
            if (url != null) {
                val modifiedUrl = "https://archive.is/newest/$url"
                val browserIntent = Intent(Intent.ACTION_VIEW, Uri.parse(modifiedUrl))
                startActivity(browserIntent)
            } else {
                val sharedText = intent.getStringExtra(Intent.EXTRA_TEXT)
                Toast.makeText(this, "Invalid URL: $sharedText", Toast.LENGTH_SHORT).show()
            }
            finish()
        } else {
            setContentView(R.layout.activity_main)
        }
    }

    private fun extractUrl(intent: Intent): String? {
        intent.dataString?.let {
            if (Patterns.WEB_URL.matcher(it).matches()) {
                return it
            }
        }

        intent.getStringExtra(Intent.EXTRA_TEXT)?.let {
            for (word in it.split(" ", "\n")) {
                if (Patterns.WEB_URL.matcher(word).matches()) {
                    return word
                }
            }
        }

        return null
    }
}
