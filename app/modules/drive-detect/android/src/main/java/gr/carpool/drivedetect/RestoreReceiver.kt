package gr.carpool.drivedetect

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/** A reboot or an update forgets the watch; put it back if the driver had it on. */
class RestoreReceiver : BroadcastReceiver() {
  override fun onReceive(context: Context, intent: Intent) {
    if (Watch.isOn(context)) Watch.register(context)
  }
}
