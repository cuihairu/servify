package servify.sdk.android

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFailsWith
import kotlin.test.assertNull
import kotlin.test.assertFalse

class ServifyConfigTest {

    @Test
    fun acceptsHttpsApiUrl() {
        val config = ServifyConfig(apiUrl = "https://chat.example.com")
        assertEquals("https://chat.example.com", config.apiUrl)
    }

    @Test
    fun acceptsWssApiUrl() {
        val config = ServifyConfig(apiUrl = "wss://chat.example.com")
        assertEquals("wss://chat.example.com", config.apiUrl)
    }

    @Test
    fun rejectsPlaintextHttpApiUrlAtConstruction() {
        val error = assertFailsWith<IllegalArgumentException> {
            ServifyConfig(apiUrl = "http://chat.example.com")
        }
        assertEquals(
            "apiUrl must use https:// or wss:// (got: http://chat.example.com)",
            error.message,
        )
    }

    @Test
    fun rejectsPlaintextWsApiUrlAtConstruction() {
        assertFailsWith<IllegalArgumentException> {
            ServifyConfig(apiUrl = "ws://chat.example.com")
        }
    }

    @Test
    fun rejectsEmptyApiUrl() {
        assertFailsWith<IllegalArgumentException> {
            ServifyConfig(apiUrl = "")
        }
    }

    @Test
    fun defaultsMatchFrozenSurface() {
        val config = ServifyConfig(apiUrl = "https://chat.example.com")
        assertNull(config.guestToken)
        assertEquals("在线客服", config.branding.title)
        assertNull(config.branding.primaryColor)
        assertEquals("您好，请问有什么可以帮您？", config.branding.welcomeText)
        assertEquals(PresentationStyle.Drawer, config.presentationStyle)
        assertFalse(config.loggingEnabled)
    }
}
