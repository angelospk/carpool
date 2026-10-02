package gr.carpool.drivedetect

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import com.google.android.gms.location.ActivityRecognition
import com.google.android.gms.location.ActivityTransition
import com.google.android.gms.location.ActivityTransitionRequest
import com.google.android.gms.location.DetectedActivity
import org.json.JSONArray

// Noticing that the phone is in a car, with nothing sent anywhere.
//
// Google Play services tells the app when the phone starts moving in a
// vehicle, even with the app closed. In a vehicle is not driving — it may be
// a bus — so the phone only says something when that happens inside one of
// the driver's own departure windows, which the app writes here whenever it
// loads them. Then it shows one local notification that opens the app on
// that departure, where «Φεύγω» is. No position, no network: the decision is
// made on the phone. See specs/DRIVING-NOW.md, phase 3.
internal object Watch {
  private const val PREFS = "carpool.driveDetect"
  private const val KEY_ON = "on"
  private const val KEY_WINDOWS = "windows"
  private const val KEY_TITLE = "title"
  private const val KEY_BODY = "body"
  private const val KEY_LAST = "lastWindowStart"
  private const val CHANNEL = "driving"
  private const val NOTIFICATION_ID = 7301

  private fun prefs(context: Context) = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

  fun permitted(context: Context): Boolean =
    Build.VERSION.SDK_INT < Build.VERSION_CODES.Q ||
      ContextCompat.checkSelfPermission(context, Manifest.permission.ACTIVITY_RECOGNITION) ==
      PackageManager.PERMISSION_GRANTED

  private fun pendingIntent(context: Context): PendingIntent {
    val intent = Intent(context, VehicleReceiver::class.java)
    // Mutable: Play services writes the transition into the intent's extras.
    val flags = PendingIntent.FLAG_UPDATE_CURRENT or
      (if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) PendingIntent.FLAG_MUTABLE else 0)
    return PendingIntent.getBroadcast(context, 0, intent, flags)
  }

  /** Registers for "started moving in a vehicle". Returns false without permission. */
  fun register(context: Context): Boolean {
    if (!permitted(context)) return false
    val request = ActivityTransitionRequest(
      listOf(
        ActivityTransition.Builder()
          .setActivityType(DetectedActivity.IN_VEHICLE)
          .setActivityTransition(ActivityTransition.ACTIVITY_TRANSITION_ENTER)
          .build()
      )
    )
    try {
      ActivityRecognition.getClient(context).requestActivityTransitionUpdates(request, pendingIntent(context))
    } catch (e: SecurityException) {
      return false
    }
    return true
  }

  fun start(context: Context, title: String, body: String): Boolean {
    prefs(context).edit().putBoolean(KEY_ON, true).putString(KEY_TITLE, title).putString(KEY_BODY, body).apply()
    return register(context)
  }

  fun stop(context: Context) {
    prefs(context).edit().putBoolean(KEY_ON, false).apply()
    if (permitted(context)) {
      try {
        ActivityRecognition.getClient(context).removeActivityTransitionUpdates(pendingIntent(context))
      } catch (e: SecurityException) {
        // Permission withdrawn: there is nothing registered to remove.
      }
    }
  }

  fun isOn(context: Context): Boolean = prefs(context).getBoolean(KEY_ON, false)

  /** [[startMs, endMs, "occurrence id"], ...]: when a car journey means one of the driver's departures. */
  fun setWindows(context: Context, windows: String) {
    prefs(context).edit().putString(KEY_WINDOWS, windows).apply()
  }

  /** Called when the phone has started moving in a vehicle. */
  fun onVehicle(context: Context, now: Long) {
    if (!isOn(context)) return
    val p = prefs(context)
    val windows = try { JSONArray(p.getString(KEY_WINDOWS, "[]")) } catch (e: Exception) { JSONArray() }
    for (i in 0 until windows.length()) {
      val w = windows.optJSONArray(i) ?: continue
      val start = w.optLong(0)
      val end = w.optLong(1)
      val occurrence = w.optString(2)
      if (now < start || now > end || occurrence.isEmpty()) continue
      // Once per departure: a stop at the lights and off again is not news.
      if (p.getLong(KEY_LAST, 0L) == start) return
      p.edit().putLong(KEY_LAST, start).apply()
      notify(context, occurrence, p.getString(KEY_TITLE, "") ?: "", p.getString(KEY_BODY, "") ?: "")
      return
    }
  }

  private fun notify(context: Context, occurrence: String, title: String, body: String) {
    val manager = NotificationManagerCompat.from(context)
    if (!manager.areNotificationsEnabled()) return
    if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
      val system = context.getSystemService(NotificationManager::class.java)
      if (system.getNotificationChannel(CHANNEL) == null) {
        system.createNotificationChannel(NotificationChannel(CHANNEL, title, NotificationManager.IMPORTANCE_DEFAULT))
      }
    }
    val open = Intent(Intent.ACTION_VIEW, Uri.parse("carpool://trip/" + Uri.encode(occurrence)))
      .setPackage(context.packageName)
      .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    val tap = PendingIntent.getActivity(
      context, 0, open, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
    )
    val notification = NotificationCompat.Builder(context, CHANNEL)
      .setSmallIcon(context.applicationInfo.icon)
      .setContentTitle(title)
      .setContentText(body)
      .setContentIntent(tap)
      .setAutoCancel(true)
      .build()
    try {
      manager.notify(NOTIFICATION_ID, notification)
    } catch (e: SecurityException) {
      // Notifications refused since the check above.
    }
  }
}
