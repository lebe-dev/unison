package app.unison

import android.app.Application
import app.unison.diagnostics.Diagnostics
import app.unison.diagnostics.Journal

/** Application that starts the error reporting of the build. */
class UnisonApplication : Application() {
    override fun onCreate() {
        super.onCreate()
        Journal.init(this)
        Diagnostics.start(this)
    }
}
