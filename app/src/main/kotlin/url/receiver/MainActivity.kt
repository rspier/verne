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
            val sharedText = intent.getStringExtra(Intent.EXTRA_TEXT)
            if (sharedText != null && Patterns.WEB_URL.matcher(sharedText).matches()) {
                val modifiedUrl = "https://archive.is/newest/$sharedText"
                val browserIntent = Intent(Intent.ACTION_VIEW, Uri.parse(modifiedUrl))
                startActivity(browserIntent)
            } else {
                Toast.makeText(this, "Invalid URL", Toast.LENGTH_SHORT).show()
            }
            finish()
        } else {
            setContentView(R.layout.activity_main)
        }
    }
}
