package servify.sdk.android

import android.app.Activity
import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RectF
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.widget.FrameLayout
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.ComposeView
import androidx.lifecycle.findViewTreeLifecycleOwner
import androidx.lifecycle.setViewTreeLifecycleOwner
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import servify.sdk.android.model.SenderType
import servify.sdk.android.ui.ChatPanel
import servify.sdk.android.ui.ChatThemeDefaults
import servify.sdk.android.ui.LocalServifyPrimary
import servify.sdk.android.ui.PanelStyle
import servify.sdk.android.ui.resolvePanelStyle
import kotlin.math.max

/**
 * 入口编排（平台规格 §4.2 show/hide / D8 三形态）：
 * 浮钮挂宿主 DecorView（宿主可完全自绘入口而不调 show，浮钮只是默认入口）；
 * 面板 = decorView 上的全屏 FrameLayout（scrim + 底部面板），主体经 [ChatPanel] 渲染，
 * 形态（抽屉 ≤85% 屏高 / 全屏，小屏自动升级）由 [resolvePanelStyle] 决定。
 * hide() 只摘面板、连接保持；所有 View 更新 post 到主线程。
 */
internal class EntryOrchestrator(
    private val chat: ServifyChat,
    private val config: ServifyConfig,
    private val scope: CoroutineScope,
) {

    private var hostView: ViewGroup? = null
    private var button: FloatingButtonView? = null
    private var badgeJob: Job? = null
    private var panelView: FrameLayout? = null

    /** 浮钮点击回调：缺省行为为打开面板，宿主/测试可覆盖。 */
    var onButtonTap: (() -> Unit)? = null

    fun attach(activity: Activity) {
        val decor = activity.window.decorView as? ViewGroup ?: return
        if (hostView === decor && button != null) return // 已挂同一窗口
        release() // activity 重建等场景：先清旧挂载再重挂
        hostView = decor
        button = FloatingButtonView(activity, config.branding.primaryColor).also { view ->
            view.setOnClickListener {
                (onButtonTap ?: { openPanel(activity) }).invoke()
            }
            decor.addView(view, defaultParams())
        }
        startBadge()
    }

    /** 收起面板（hide()：连接保持，浮钮保留）。 */
    fun detachPanel() {
        panelView?.let { root ->
            root.post { (root.parent as? ViewGroup)?.removeView(root) }
        }
        panelView = null
    }

    /** 销毁清理：摘除浮钮与面板、取消订阅。 */
    fun release() {
        badgeJob?.cancel()
        badgeJob = null
        detachPanel()
        button?.let { hostView?.removeView(it) }
        button = null
        hostView = null
    }

    private fun startBadge() {
        badgeJob?.cancel()
        badgeJob = scope.launch {
            chat.events.unreadCount.collect { count ->
                val view = button ?: return@collect
                view.post { view.setUnread(count) }
            }
        }
    }

    private fun openPanel(activity: Activity) {
        if (panelView != null) return
        val decor = activity.window.decorView as? ViewGroup ?: return
        // 生命周期桥探测（零 androidx.activity 编译依赖）：宿主未桥接（decorView 与
        // content 都无 lifecycle owner，即非 ComponentActivity 且未手工桥接）→ 降级不挂面板。
        val owners = resolveOwners(activity) ?: return

        val root = FrameLayout(activity).apply {
            layoutParams = ViewGroup.LayoutParams(MATCH_PARENT, MATCH_PARENT)
        }
        // 点 scrim 收面板（V1 无拖拽手势）。
        val scrim = View(activity).apply {
            setBackgroundColor(BACKDROP_COLOR)
            setOnClickListener { detachPanel() }
        }
        root.addView(
            scrim,
            FrameLayout.LayoutParams(MATCH_PARENT, MATCH_PARENT),
        )

        val style = resolvePanelStyle(
            config.presentationStyle,
            activity.resources.configuration.screenHeightDp,
        )
        val panelHeight = if (style == PanelStyle.Drawer) {
            (decor.height * DRAWER_HEIGHT_FRACTION).toInt().coerceAtLeast(dp(activity, 240))
        } else {
            MATCH_PARENT
        }
        val panel = ComposeView(activity).apply {
            // 显式桥接（owner 已探测到，复制到面板视图自身，不依赖树上查找路径）。
            // savedstate owner 不桥接：savedstate 不在依赖白名单，面板未用 rememberSaveable。
            setViewTreeLifecycleOwner(owners)
            setContent {
                val primary = config.branding.primaryColor
                CompositionLocalProvider(
                    LocalServifyPrimary provides (
                        primary?.let { Color(it) } ?: ChatThemeDefaults.DefaultPrimary
                        ),
                ) {
                    ChatPanel(
                        chat = chat,
                        title = config.branding.title,
                        welcomeText = config.branding.welcomeText.takeIf { it.isNotBlank() },
                        onDismiss = { detachPanel() },
                        modifier = Modifier.fillMaxWidth().fillMaxHeight(),
                    )
                }
            }
        }
        root.addView(
            panel,
            FrameLayout.LayoutParams(MATCH_PARENT, panelHeight, Gravity.BOTTOM),
        )
        decor.addView(root, FrameLayout.LayoutParams(MATCH_PARENT, MATCH_PARENT))
        panelView = root
    }

    /**
     * 探测宿主的生命周期 owner：decorView → content 容器逐级向上找
     * （androidx.activity 的 ComponentActivity 会把自身桥到视图树上；纯 Activity 宿主没有）。
     * 返回 null = 宿主不支持 ComposeView 挂载。
     */
    private fun resolveOwners(activity: Activity): androidx.lifecycle.LifecycleOwner? {
        val decor = activity.window.decorView
        val content = activity.findViewById<View>(android.R.id.content)
        return decor.findViewTreeLifecycleOwner()
            ?: content?.findViewTreeLifecycleOwner()
    }

    private fun defaultParams(): FrameLayout.LayoutParams {
        val density = button?.resources?.displayMetrics?.density ?: 1f
        val margin = (16 * density).toInt()
        return FrameLayout.LayoutParams(dp(56), dp(56)).apply {
            gravity = Gravity.END or Gravity.BOTTOM
            rightMargin = margin
            bottomMargin = margin
        }
    }

    private fun viewDensityScale(): Float = button?.resources?.displayMetrics?.density ?: 1f

    private fun dp(v: Int): Int = max(1, (v * viewDensityScale()).toInt())

    private companion object {
        const val DRAWER_HEIGHT_FRACTION = 0.85f
        const val BACKDROP_COLOR = 0x52000000

        fun dp(activity: Activity, v: Int): Int =
            max(1, (v * activity.resources.displayMetrics.density).toInt())

        const val MATCH_PARENT = ViewGroup.LayoutParams.MATCH_PARENT
    }
}

/**
 * 浮动按钮（D8：56dp 圆形悬浮，默认右下角；角标 = 抽屉收起时的未读计数）。
 * 纯 framework 自绘（零资源/零依赖）：圆形主色底 + 白色气泡图标 + 红色角标。
 */
internal class FloatingButtonView(
    context: Context,
    primaryColor: Int?,
) : View(context) {

    private var unread = 0

    private val circlePaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = primaryColor ?: DEFAULT_PRIMARY
    }
    private val bubblePaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = android.graphics.Color.WHITE
        style = Paint.Style.STROKE
        strokeWidth = dp(2f)
    }
    private val badgePaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = BADGE_BG
    }
    private val badgeTextPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = android.graphics.Color.WHITE
        textSize = dp(11f)
        textAlign = Paint.Align.CENTER
        isFakeBoldText = true
    }

    fun setUnread(count: Int) {
        val next = max(0, count)
        if (next != unread) {
            unread = next
            invalidate()
        }
    }

    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        val size = width.toFloat()
        val c = size / 2f
        canvas.drawCircle(c, c, c, circlePaint)

        // 白色气泡图标：圆角矩形主体 + 左下小尾巴
        val bodyLeft = dp(16f)
        val bodyTop = dp(18f)
        val bodyRight = size - dp(16f)
        val bodyBottom = size - dp(22f)
        val radius = dp(8f)
        canvas.drawRoundRect(
            RectF(bodyLeft, bodyTop, bodyRight, bodyBottom),
            radius, radius, bubblePaint,
        )
        canvas.drawCircle(dp(22f), (bodyTop + bodyBottom) / 2f, dp(1.5f), bubblePaint)
        canvas.drawCircle(c, (bodyTop + bodyBottom) / 2f, dp(1.5f), bubblePaint)
        canvas.drawCircle(size - dp(22f), (bodyTop + bodyBottom) / 2f, dp(1.5f), bubblePaint)

        if (unread > 0) {
            drawBadge(canvas, size)
        }
    }

    private fun drawBadge(canvas: Canvas, size: Float) {
        val label = if (unread > MAX_BADGE) "$MAX_BADGE+" else unread.toString()
        val textWidth = badgeTextPaint.measureText(label)
        val badgeRadius = max(dp(10f), (textWidth + dp(8f)) / 2f)
        val cx = size - badgeRadius
        val cy = badgeRadius
        canvas.drawCircle(cx, cy, badgeRadius, badgePaint)
        val textY = cy - (badgeTextPaint.descent() + badgeTextPaint.ascent()) / 2f
        canvas.drawText(label, cx, textY, badgeTextPaint)
    }

    private fun dp(v: Float): Float = v * resources.displayMetrics.density

    companion object {
        const val MAX_BADGE = 99
        val DEFAULT_PRIMARY: Int = 0xFF2563EB.toInt()
        val BADGE_BG: Int = 0xFFEF4444.toInt()
    }
}
