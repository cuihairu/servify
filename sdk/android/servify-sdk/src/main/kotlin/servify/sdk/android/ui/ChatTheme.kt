package servify.sdk.android.ui

import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color

/**
 * 会话页主题常量（D8 品牌注入的最小面：主色经 Branding 配置进入渲染层）。
 * 不引 material/material3（依赖白名单禁入）——色板/形状在组件内自绘。
 */
object ChatThemeDefaults {
    /** 未配置 branding.primaryColor 时的默认主色（品牌蓝）。 */
    val DefaultPrimary: Color = Color(0xFF2563EB)

    val PageBackground: Color = Color(0xFFF8FAFC)
    val SurfaceWhite: Color = Color.White
    val TextPrimary: Color = Color(0xFF0F172A)
    val TextSecondary: Color = Color(0xFF64748B)
    val BubbleCustomer: Color = Color(0xFF2563EB)
    val BubbleOther: Color = Color.White
    val Divider: Color = Color(0xFFE2E8F0)
    val HandoffBadge: Color = Color(0xFFFEF3C7)
}

/** 主色注入（CompositionLocal，避免层层传参）。 */
val LocalServifyPrimary = staticCompositionLocalOf { ChatThemeDefaults.DefaultPrimary }
