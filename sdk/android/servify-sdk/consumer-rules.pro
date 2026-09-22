# Servify Android SDK —— 消费方 R8 规则（AGP 自动并入宿主）。
# kotlinx.serialization：编译期生成的 serializer 与 Companion 经反射查找，R8 下需 keep。
# （协议/模型类均在 servify.sdk.android.** 包下：shared/protocol、shared/core 源直接编入本 AAR。）

-keepattributes *Annotation*, InnerClasses, Signature

-dontnote kotlinx.serialization.**

-keep,includedescriptorclasses class servify.sdk.android.**$$serializer { *; }
-keepclassmembers class servify.sdk.android.** {
    *** Companion;
}
-keepclasseswithmembers class servify.sdk.android.** {
    kotlinx.serialization.KSerializer serializer(...);
}
