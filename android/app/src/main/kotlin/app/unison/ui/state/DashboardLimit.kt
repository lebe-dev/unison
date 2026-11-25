package app.unison.ui.state

/** Number of newest items each tab of the dashboard keeps. */
object DashboardLimit {
    /** Numbers of items the settings offer. */
    val OPTIONS = listOf(6, 12, 24, 48)

    /** Number of items a tab shows when the user picked none. */
    const val DEFAULT = 12

    /** The given value when the settings offer it, the default otherwise. */
    fun normalize(value: Int): Int = if (value in OPTIONS) value else DEFAULT
}
