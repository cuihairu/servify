package servify.sdk.android

/** 会话页展示形态（D8）：小屏自动升级全屏。 */
enum class PresentationStyle { Drawer, Fullscreen }

/** 会话页品牌化配置。 */
data class Branding(
    val title: String = "在线客服",
    /** ARGB 主色；null = 平台默认主色（UI 层自行取默认主题色）。 */
    val primaryColor: Int? = null,
    val welcomeText: String = "您好，请问有什么可以帮您？",
    /**
     * 离线提示（V1 品牌基础配置四件套收口，M3）：disconnected 终态（握手失败/重连耗尽）
     * 时追加的系统提示行；null = 不展示（默认，接入方零改动）。
     */
    val offlineText: String? = null,
)

/**
 * 接入配置（平台规格 §4.1，V1 冻结面）。
 *
 * apiUrl 强制加密协议：非 https/wss 值构造期直接报错（对应错误码 config_invalid）。
 * M3 项（pushTokenProvider）随 M3 刀进入冻结面，避免无消费面的预留字段。
 */
data class ServifyConfig(
    val apiUrl: String,
    val guestToken: String? = null,
    val branding: Branding = Branding(),
    val presentationStyle: PresentationStyle = PresentationStyle.Drawer,
    val loggingEnabled: Boolean = false,
) {
    init {
        require(apiUrl.startsWith("https://") || apiUrl.startsWith("wss://")) {
            "apiUrl must use https:// or wss:// (got: ${if (apiUrl.isEmpty()) "<empty>" else apiUrl})"
        }
    }
}
