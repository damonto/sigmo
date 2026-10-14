import Foundation

enum AuthenticationFixture {
    static func methods(required: Bool) -> String {
        required ? #"{"version":1,"mode":"otp","methods":["otp"]}"# : #"{"version":1,"mode":"none","methods":[]}"#
    }
    static let challenge = #"{"id":"fixture-challenge","expiresAt":"2033-05-18T03:33:20Z"}"#
    static func tokens(_ access: String) -> String {
        #"{"accessToken":"\#(access)","refreshToken":"refresh-\#(access)","expiresAt":"2033-05-18T03:33:20Z","sessionExpiresAt":"2033-05-19T03:33:20Z"}"#
    }
    static func validCode(_ request: URLRequest) -> Bool {
        guard let body = request.httpBody, let fields = try? JSONDecoder().decode([String: String].self, from: body) else { return false }
        return fields["code"] == "123456" && fields["challengeId"] == "fixture-challenge" && fields["client"] == "native"
    }
}
