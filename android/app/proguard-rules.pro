# Keep AIDL bridge interfaces and parcelables exposed to external clients.
-keep class app.unison.bridge.** { *; }

# WorkManager creates the worker from its class name.
-keep class app.unison.updater.UpdateCheckWorker { *; }

# Protobuf-javalite accesses generated message fields via reflection.
-keepclassmembers class * extends com.google.protobuf.GeneratedMessageLite {
    <fields>;
}

# ML Kit loads ComponentRegistrar implementations by class name from manifest meta-data.
-keep class * implements com.google.firebase.components.ComponentRegistrar { *; }
-keep class com.google.mlkit.vision.barcode.internal.BarcodeRegistrar { *; }
-keep class com.google.mlkit.vision.common.internal.VisionCommonRegistrar { *; }
-keep class com.google.mlkit.common.internal.CommonComponentRegistrar { *; }

# Strip verbose and debug log calls from release builds.
-assumenosideeffects class android.util.Log {
    public static int v(...);
    public static int d(...);
}
