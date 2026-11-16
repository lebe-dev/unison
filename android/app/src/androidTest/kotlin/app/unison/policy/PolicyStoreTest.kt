package app.unison.policy

import android.content.Context
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.assertEquals
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith

@RunWith(AndroidJUnit4::class)
class PolicyStoreTest {
    private val context: Context = InstrumentationRegistry.getInstrumentation().targetContext

    @Before
    fun clear() {
        PolicyStore(context).reset()
    }

    @Test
    fun defaultsAreLoadedWithoutStoredValues() {
        assertEquals(PolicyDefaults.config, PolicyStore(context).load())
    }

    @Test
    fun storedTableIsLoadedBack() {
        val store = PolicyStore(context)
        val changed = PolicyDefaults.config
            .with(
                PolicyCondition.ROAMING,
                PolicyDefaults.config[PolicyCondition.ROAMING]
                    .with(PolicyKind.IMAGE, PolicyCell(PolicyMode.CONFIRM, PolicySize.ofMegabytes(3))),
            )
            .copy(relayMaxSizeBytes = PolicySize.ofMegabytes(5))

        store.save(changed)

        assertEquals(changed, PolicyStore(context).load())
    }

    @Test
    fun resetDropsTheStoredTable() {
        val store = PolicyStore(context)
        store.save(PolicyDefaults.config.copy(relayMaxSizeBytes = PolicySize.ofMegabytes(7)))

        assertEquals(PolicyDefaults.config, store.reset())
        assertEquals(PolicyDefaults.config, store.load())
    }

    @Test
    fun brokenValuesFallBackToDefaults() {
        context.getSharedPreferences("unison.policy", Context.MODE_PRIVATE)
            .edit()
            .putString(PolicyKeys.mode(PolicyCondition.METERED, PolicyKind.TEXT), "unknown")
            .putLong(PolicyKeys.maxSize(PolicyCondition.METERED, PolicyKind.TEXT), -1)
            .putLong(PolicyKeys.RELAY_MAX_SIZE, 0)
            .commit()

        assertEquals(PolicyDefaults.config, PolicyStore(context).load())
    }
}
