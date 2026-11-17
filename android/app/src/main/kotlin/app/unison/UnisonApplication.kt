package app.unison

import android.app.Application
import app.unison.diagnostics.Diagnostics

/** Application that starts the error reporting of the build. */
class UnisonApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        Diagnostics.start(this)
    }
}
