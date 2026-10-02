package gr.carpool.drivedetect

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import com.google.android.gms.location.ActivityTransition
import com.google.android.gms.location.ActivityTransitionResult
import com.google.android.gms.location.DetectedActivity

/** Play services' word that the phone started moving in a vehicle. */
class VehicleReceiver : BroadcastReceiver() {
  override fun onReceive(context: Context, intent: Intent) {
    val result = ActivityTransitionResult.extractResult(intent) ?: return
    val entered = result.transitionEvents.any {
      it.activityType == DetectedActivity.IN_VEHICLE &&
        it.transitionType == ActivityTransition.ACTIVITY_TRANSITION_ENTER
    }
    if (entered) Watch.onVehicle(context, System.currentTimeMillis())
  }
}
