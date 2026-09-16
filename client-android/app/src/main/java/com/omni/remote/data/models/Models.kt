package com.omni.remote.data.models

import com.google.gson.annotations.SerializedName

data class WindowInfo(
    @SerializedName("id") val id: String,
    @SerializedName("title") val title: String,
    @SerializedName("class") val className: String? = null
)

data class ProcessInfo(
    @SerializedName("pid") val pid: Int,
    @SerializedName("name") val name: String,
    @SerializedName("cpu") val cpu: Double,
    @SerializedName("memory") val memory: Double
)

data class TelemetryMetric(
    @SerializedName("device_id") val deviceId: String,
    @SerializedName("cpu_usage_pct") val cpuUsagePct: Double,
    @SerializedName("ram_usage_pct") val ramUsagePct: Double,
    @SerializedName("ram_used_bytes") val ramUsedBytes: Long,
    @SerializedName("disk_usage_pct") val diskUsagePct: Double,
    @SerializedName("battery_pct") val batteryPct: Double?,
    @SerializedName("is_charging") val isCharging: Boolean?,
    @SerializedName("network_name") val networkName: String?,
    @SerializedName("public_ip") val publicIp: String?,
    @SerializedName("uptime_seconds") val uptimeSeconds: Long?,
    @SerializedName("open_windows") val openWindows: List<WindowInfo> = emptyList(),
    @SerializedName("top_processes") val topProcesses: List<ProcessInfo> = emptyList(),
    @SerializedName("recorded_at") val recordedAt: String
)

data class DeviceSystemInfo(
    @SerializedName("device_id") val deviceId: String,
    @SerializedName("cpu_model") val cpuModel: String,
    @SerializedName("cpu_cores") val cpuCores: Int,
    @SerializedName("ram_total_bytes") val ramTotalBytes: Long,
    @SerializedName("disk_total_bytes") val diskTotalBytes: Long,
    @SerializedName("os_version") val osVersion: String,
    @SerializedName("kernel_version") val kernelVersion: String,
    @SerializedName("arch") val arch: String,
    @SerializedName("ip_address") val ipAddress: String,
    @SerializedName("mac_address") val macAddress: String,
    @SerializedName("timezone") val timezone: String,
    @SerializedName("agent_version") val agentVersion: String,
    @SerializedName("public_ip") val publicIp: String? = null,
    @SerializedName("network_name") val networkName: String? = null,
    @SerializedName("uptime_seconds") val uptimeSeconds: Long? = null,
    @SerializedName("control_mode") val controlMode: String? = null,
    @SerializedName("updated_at") val updatedAt: String
)

data class Device(
    @SerializedName("id") val id: String,
    @SerializedName("name") val name: String,
    @SerializedName("hostname") val hostname: String,
    @SerializedName("os") val os: String = "android",
    @SerializedName("platform") val platform: String,
    @SerializedName("status") val status: String = "online"
)

data class DeviceEvent(
    @SerializedName("device_id") val deviceId: String,
    @SerializedName("event_type") val eventType: String,
    @SerializedName("severity") val severity: String = "info",
    @SerializedName("message") val message: String,
    @SerializedName("metadata") val metadata: Map<String, Any>? = null,
    @SerializedName("created_at") val createdAt: String
)

data class Command(
    @SerializedName("id") val id: String,
    @SerializedName("device_id") val deviceId: String,
    @SerializedName("type") val type: String,
    @SerializedName("payload") val payload: Map<String, Any> = emptyMap(),
    @SerializedName("status") val status: String = "pending",
    @SerializedName("exit_code") val exitCode: Int = 0,
    @SerializedName("output") val output: String = "",
    @SerializedName("error") val error: String? = null
)

data class WSMessage(
    @SerializedName("type") val type: String,
    @SerializedName("device_id") val deviceId: String? = null,
    @SerializedName("command_id") val commandId: String? = null,
    @SerializedName("payload") val payload: Any? = null
)

data class NotificationAction(
    @SerializedName("title") val title: String,
    @SerializedName("index") val index: Int
)

data class NotificationRecord(
    @SerializedName("device_id") val deviceId: String,
    @SerializedName("external_id") val externalId: String,
    @SerializedName("package_name") val packageName: String,
    @SerializedName("app_name") val appName: String? = null,
    @SerializedName("title") val title: String = "",
    @SerializedName("text") val text: String = "",
    @SerializedName("sub_text") val subText: String? = null,
    @SerializedName("category") val category: String? = null,
    @SerializedName("is_ongoing") val isOngoing: Boolean = false,
    @SerializedName("is_clearable") val isClearable: Boolean = true,
    @SerializedName("actions") val actions: List<NotificationAction> = emptyList(),
    @SerializedName("posted_at") val postedAt: String,
    @SerializedName("received_at") val receivedAt: String
)

data class SmsMessage(
    @SerializedName("device_id") val deviceId: String,
    @SerializedName("external_id") val externalId: String,
    @SerializedName("direction") val direction: String = "inbound",
    @SerializedName("address") val address: String,
    @SerializedName("body") val body: String,
    @SerializedName("person") val person: String? = null,
    @SerializedName("read") val read: Boolean = false,
    @SerializedName("timestamp") val timestamp: String,
    @SerializedName("received_at") val receivedAt: String
)
