package app.unison.bridge;

/** Names, versions and enums that both the app and the keyboard share. */
public final class BridgeContract {

    /** Package of the app that hosts the bridge service. */
    public static final String PACKAGE = "app.unison";

    /** Action of the intent that binds the bridge service. */
    public static final String ACTION_BIND = "app.unison.bridge.BIND";

    /** Class of the bridge service in {@link #PACKAGE}. */
    public static final String SERVICE_CLASS = "app.unison.service.BridgeService";

    /** Signature permission that the bridge service requires from its callers. */
    public static final String PERMISSION = "app.unison.permission.BRIDGE";

    /**
     * Version of the contract this artifact describes.
     *
     * A caller compares it with {@code protocolVersion()} of the bound service and does not use
     * methods it does not know.
     */
    public static final int PROTOCOL_VERSION = 1;

    /** Kind of an item whose content has no known type. */
    public static final int KIND_UNSPECIFIED = 0;

    /** Kind of a text item. */
    public static final int KIND_TEXT = 1;

    /** Kind of an image item. */
    public static final int KIND_IMAGE = 2;

    /** Kind of an item that carries one or more files. */
    public static final int KIND_FILES = 3;

    private BridgeContract() {
    }
}
