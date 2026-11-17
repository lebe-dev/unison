package app.unison

import android.content.Context
import androidx.annotation.PluralsRes
import androidx.annotation.StringRes
import androidx.test.core.app.ApplicationProvider

/** Target context of the test, whose resources follow the locale of the device. */
val testContext: Context get() = ApplicationProvider.getApplicationContext()

/** Text of a string resource in the locale of the device. */
fun testString(@StringRes id: Int, vararg args: Any): String =
    testContext.getString(id, *args)

/** Text of a plural resource in the locale of the device. */
fun testQuantity(@PluralsRes id: Int, quantity: Int, vararg args: Any): String =
    testContext.resources.getQuantityString(id, quantity, *args)
