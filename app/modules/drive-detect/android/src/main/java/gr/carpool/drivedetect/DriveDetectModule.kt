package gr.carpool.drivedetect

import expo.modules.kotlin.Promise
import expo.modules.kotlin.exception.Exceptions
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition

class DriveDetectModule : Module() {
  private val context
    get() = appContext.reactContext ?: throw Exceptions.ReactContextLost()

  override fun definition() = ModuleDefinition {
    Name("DriveDetect")

    // Resolves once Play services has accepted or refused the watch. The
    // notification's words come from the app, in the language in use.
    AsyncFunction("start") { title: String, body: String, promise: Promise ->
      Watch.start(context, title, body) { ok -> promise.resolve(ok) }
    }
    AsyncFunction("stop") { Watch.stop(context) }
    AsyncFunction("isOn") { Watch.isOn(context) }
    AsyncFunction("setWindows") { windows: String -> Watch.setWindows(context, windows) }
  }
}
