-keep class io.nekohasekai.libSwipt.** { *; }
-keep class go.** { *; }
-keepclasseswithmembernames class * {
    native <methods>;
}
-keep class io.nekohasekai.swipt.device.CoreProcess {
    public static android.os.ParcelFileDescriptor connect(java.lang.String);
    public static void close();
}
-keep class io.nekohasekai.swipt.device.ShellHelper {
    public static void main(java.lang.String[]);
}
-keep class io.nekohasekai.swipt.privilege.CoreService {
    public <init>(android.content.Context);
}
-keep class io.nekohasekai.swipt.privilege.CoreRootService {
    public <init>();
}
-keep class expo.modules.ExpoModulesPackageList {
    public *;
}
