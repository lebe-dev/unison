package app.unison.policy

import unison.local.v1.Local

/** Table row the core picks for a network state. */
fun conditionOf(network: Local.NetworkState?): PolicyCondition? {
    if (network == null || !network.online) {
        return null
    }
    return when {
        network.dataSaver -> PolicyCondition.DATA_SAVER
        network.roaming -> PolicyCondition.ROAMING
        network.metered -> PolicyCondition.METERED
        else -> PolicyCondition.UNMETERED
    }
}
