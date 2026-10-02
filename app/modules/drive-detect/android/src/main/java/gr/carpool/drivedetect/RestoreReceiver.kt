package gr.carpool.drivedetect

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/**
 * A reboot or an update forgets the watch; put it back if the driver had it
 * on. The receiver is kept alive until Play services has answered, which it
 * does asynchronously.
 */
class RestoreReceiver : BroadcastReceiver() {
  override fun onReceive(context: Context, intent: Intent) {
    if (!Watch.isOn(context)) return
    val pending = goAsync()
    Watch.register(context) { pending.finish() }
  }
}
