package servify.sdk.android

import android.app.Activity
import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.RectF
import android.view.View
import android.view.ViewGroup
import android.widget.FrameLayout
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.launch
import servify.sdk.android.model.SenderType
import kotlin.math.max

/**
 * 入口编排（平台规格 §4.2 show/hide / D8 三形态）：
 * 浮钮挂宿主 DecorView（宿主可完全自绘入口而不调 show，浮钮只是默认入口）；
 * 面板（抽屉/全屏）随 UI 刀接线。所有 View 更新 post 到主线程。
 */
internal class EntryOrchestrator(
    private val chat: ServifyChat,
    private val primaryColor: Int?,
    private val scope: CoroutineScope,
) {

    private var hostView: ViewGroup? = null
    private var button: FloatingButtonView? = null
    private var badgeJob: Job? = null
    /** 面板打开状态（UI 刀前的占位：attach 即视为面板关闭，浮钮常驻）。 */
    private var panelOpen = false

    /** 浮钮点击回调：UI 刀接到面板打开动作。 */
    var onButtonTap: (() -> Unit)? = null

    fun attach(activity: Activity) {
        val decor = activity.window.decorView as? ViewGroup ?: return
        if (hostView === decor && button != null) return // 已挂同一窗口
        release() // activity 重建等场景：先清旧挂载再重挂
        hostView = decor
        button = FloatingButtonView(activity, primaryColor).also { view ->
            view.setOnClickListener {
                panelOpen = !panelOpen
                onButtonTap?.invoke()
            }
            decor.addView(view, defaultParams())
        }
        startBadge()
    }

    /** 收起面板（hide()：连接保持，浮钮保留）。面板实装前仅翻状态位。 */
    fun detachPanel() {
        panelOpen = false
    }

    /** 销毁清理：摘除浮钮、取消订阅。 */
    fun release() {
        badgeJob?.cancel()
        badgeJob = null
        button?.let { hostView?.removeView(it) }
        button = null
        hostView = null
        panelOpen = false
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

    private fun defaultParams(): FrameLayout.LayoutParams {
        val margin = (16 * viewDensityScale()).toInt()
        return FrameLayout.LayoutParams(dp(56), dp(56)).apply {
            gravity = android.view.Gravity.END or android.view.Gravity.BOTTOM
            rightMargin = margin
            bottomMargin = margin
        }
    }

    private fun viewDensityScale(): Float = button?.resources?.displayMetrics?.density ?: 1f

    private fun dp(v: Int): Int = max(1, (v * viewDensityScale()).toInt())
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
        color = Color.WHITE
        style = Paint.Style.STROKE
        strokeWidth = dp(2f)
    }
    private val badgePaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = BADGE_BG
    }
    private val badgeTextPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = Color.WHITE
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
