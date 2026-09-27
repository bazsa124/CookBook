# kotlinx.serialization keeps its generated serializers via annotations.
-keepattributes *Annotation*, InnerClasses
-dontnote kotlinx.serialization.**
-keepclassmembers class dev.cookbook.** {
    *** Companion;
}
-keepclasseswithmembers class dev.cookbook.** {
    kotlinx.serialization.KSerializer serializer(...);
}
