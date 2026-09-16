package com.omni.remote.services

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.GestureDescription
import android.graphics.Path
import android.graphics.Rect
import android.os.Bundle
import android.util.Log
import android.view.accessibility.AccessibilityEvent
import android.view.accessibility.AccessibilityNodeInfo
import com.google.gson.Gson
import com.omni.remote.data.models.Command

class OmniAccessibilityService : AccessibilityService() {

    private val gson = Gson()

    override fun onServiceConnected() {
        super.onServiceConnected()
        instance = this
        Log.i(TAG, "OmniAccessibilityService connected and ready.")
    }

    override fun onAccessibilityEvent(event: AccessibilityEvent?) {
        // Monitoreo pasivo de eventos de ventana si es necesario
    }

    override fun onInterrupt() {
        Log.w(TAG, "OmniAccessibilityService interrupted.")
    }

    override fun onDestroy() {
        super.onDestroy()
        if (instance == this) {
            instance = null
        }
        Log.i(TAG, "OmniAccessibilityService destroyed.")
    }

    fun executeCommand(command: Command, callback: (exitCode: Int, output: String, error: String?) -> Unit) {
        Log.d(TAG, "Executing command: ${command.type} with payload: ${command.payload}")

        when (command.type) {
            "click", "gui_click" -> {
                val x = (command.payload["x"] as? Number)?.toFloat() ?: 0f
                val y = (command.payload["y"] as? Number)?.toFloat() ?: 0f
                performClick(x, y, callback)
            }

            "swipe" -> {
                val x1 = (command.payload["x1"] as? Number)?.toFloat() ?: 0f
                val y1 = (command.payload["y1"] as? Number)?.toFloat() ?: 0f
                val x2 = (command.payload["x2"] as? Number)?.toFloat() ?: 0f
                val y2 = (command.payload["y2"] as? Number)?.toFloat() ?: 0f
                val duration = (command.payload["duration_ms"] as? Number)?.toLong() ?: 300L
                performSwipe(x1, y1, x2, y2, duration, callback)
            }

            "set_text", "gui_type" -> {
                val text = command.payload["text"] as? String ?: ""
                val targetId = command.payload["target_id"] as? String
                performSetText(text, targetId, callback)
            }

            "key", "gui_key" -> {
                val key = command.payload["key"] as? String ?: ""
                performKeyAction(key, callback)
            }

            "get_tree", "gui_window" -> {
                performGetTree(callback)
            }

            else -> {
                callback(1, "", "Unsupported command type for Android: ${command.type}")
            }
        }
    }

    private fun performClick(x: Float, y: Float, callback: (Int, String, String?) -> Unit) {
        val path = Path().apply {
            moveTo(x, y)
        }
        val stroke = GestureDescription.StrokeDescription(path, 0, 50)
        val gesture = GestureDescription.Builder().addStroke(stroke).build()

        dispatchGesture(gesture, object : GestureResultCallback() {
            override fun onCompleted(gestureDescription: GestureDescription?) {
                Log.d(TAG, "Click gesture completed at ($x, $y)")
                callback(0, "Clicked at ($x, $y)", null)
            }

            override fun onCancelled(gestureDescription: GestureDescription?) {
                Log.w(TAG, "Click gesture cancelled at ($x, $y)")
                callback(1, "", "Click gesture was cancelled")
            }
        }, null)
    }

    private fun performSwipe(
        x1: Float, y1: Float,
        x2: Float, y2: Float,
        durationMs: Long,
        callback: (Int, String, String?) -> Unit
    ) {
        val path = Path().apply {
            moveTo(x1, y1)
            lineTo(x2, y2)
        }
        val stroke = GestureDescription.StrokeDescription(path, 0, durationMs.coerceAtLeast(100L))
        val gesture = GestureDescription.Builder().addStroke(stroke).build()

        dispatchGesture(gesture, object : GestureResultCallback() {
            override fun onCompleted(gestureDescription: GestureDescription?) {
                Log.d(TAG, "Swipe gesture completed from ($x1, $y1) to ($x2, $y2)")
                callback(0, "Swiped from ($x1, $y1) to ($x2, $y2)", null)
            }

            override fun onCancelled(gestureDescription: GestureDescription?) {
                Log.w(TAG, "Swipe gesture cancelled")
                callback(1, "", "Swipe gesture was cancelled")
            }
        }, null)
    }

    private fun performSetText(text: String, targetId: String?, callback: (Int, String, String?) -> Unit) {
        val root = rootInActiveWindow
        if (root == null) {
            callback(1, "", "No active window found")
            return
        }

        var targetNode: AccessibilityNodeInfo? = null
        if (!targetId.isNullOrEmpty()) {
            val matching = root.findAccessibilityNodeInfosByViewId(targetId)
            targetNode = matching.firstOrNull()
        }

        if (targetNode == null) {
            targetNode = root.findFocus(AccessibilityNodeInfo.FOCUS_INPUT)
        }

        if (targetNode == null) {
            callback(1, "", "No focused or matching editable text field found")
            return
        }

        val arguments = Bundle().apply {
            putCharSequence(AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE, text)
        }
        val success = targetNode.performAction(AccessibilityNodeInfo.ACTION_SET_TEXT, arguments)

        if (success) {
            callback(0, "Text set successfully: $text", null)
        } else {
            callback(1, "", "Failed to set text on targeted node")
        }
    }

    private fun performKeyAction(key: String, callback: (Int, String, String?) -> Unit) {
        val action = when (key.lowercase()) {
            "back" -> GLOBAL_ACTION_BACK
            "home" -> GLOBAL_ACTION_HOME
            "recents", "app_switch" -> GLOBAL_ACTION_RECENTS
            "notifications" -> GLOBAL_ACTION_NOTIFICATIONS
            "quick_settings" -> GLOBAL_ACTION_QUICK_SETTINGS
            "power_dialog" -> GLOBAL_ACTION_POWER_DIALOG
            "lock_screen" -> GLOBAL_ACTION_LOCK_SCREEN
            else -> null
        }

        if (action == null) {
            callback(1, "", "Unknown key or global action: $key")
            return
        }

        val success = performGlobalAction(action)
        if (success) {
            callback(0, "Global action performed: $key", null)
        } else {
            callback(1, "", "Failed to perform global action: $key")
        }
    }

    private fun performGetTree(callback: (Int, String, String?) -> Unit) {
        val root = rootInActiveWindow
        if (root == null) {
            callback(1, "", "No active window available")
            return
        }

        try {
            val tree = serializeNode(root)
            val json = gson.toJson(tree)
            callback(0, json, null)
        } catch (e: Exception) {
            callback(1, "", "Error serializing accessibility tree: ${e.message}")
        }
    }

    private fun serializeNode(node: AccessibilityNodeInfo): Map<String, Any?> {
        val bounds = Rect()
        node.getBoundsInScreen(bounds)

        val children = mutableListOf<Map<String, Any?>>()
        for (i in 0 until node.childCount) {
            node.getChild(i)?.let { child ->
                children.add(serializeNode(child))
            }
        }

        return mapOf(
            "class" to (node.className?.toString() ?: ""),
            "id" to (node.viewIdResourceName ?: ""),
            "text" to (node.text?.toString() ?: ""),
            "content_description" to (node.contentDescription?.toString() ?: ""),
            "clickable" to node.isClickable,
            "editable" to node.isEditable,
            "focused" to node.isFocused,
            "bounds" to mapOf(
                "left" to bounds.left,
                "top" to bounds.top,
                "right" to bounds.right,
                "bottom" to bounds.bottom,
                "width" to bounds.width(),
                "height" to bounds.height()
            ),
            "children" to children
        )
    }

    companion object {
        private const val TAG = "OmniAccessibility"

        @Volatile
        var instance: OmniAccessibilityService? = null
            private set
    }
}
