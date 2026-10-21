import Intents

final class IntentHandler: INExtension, INStartCallIntentHandling {
    override func handler(for intent: INIntent) -> Any {
        self
    }

    func confirm(intent: INStartCallIntent, completion: @escaping @Sendable (INStartCallIntentResponse) -> Void) {
        completion(INStartCallIntentResponse(code: .ready, userActivity: nil))
    }

    func handle(intent: INStartCallIntent, completion: @escaping @Sendable (INStartCallIntentResponse) -> Void) {
        completion(INStartCallIntentResponse(code: .continueInApp, userActivity: nil))
    }
}
