import Foundation

enum SettingsFixtureData {
    static let settings = #"""
{
  "schema": {
    "auth": [
      {
        "key": "mode",
        "label": "settings.schema.auth.mode",
        "control": "select",
        "options": [
          {
            "label": "settings.authNone",
            "value": "none"
          },
          {
            "label": "settings.authOTP",
            "value": "otp"
          },
          {
            "label": "settings.authOIDC",
            "value": "oidc"
          },
          {
            "label": "settings.authAccess",
            "value": "cloudflare_access"
          }
        ]
      },
      {
        "key": "sessionMaxDays",
        "label": "settings.schema.auth.sessionMaxDays",
        "control": "number",
        "min": 1,
        "max": 180,
        "required": true,
        "description": "settings.schema.auth.sessionMaxDaysDescription"
      },
      {
        "key": "otpChannels",
        "label": "settings.schema.auth.otpChannels",
        "control": "channelList"
      },
      {
        "key": "oidcIssuer",
        "label": "settings.schema.auth.oidcIssuer",
        "control": "text"
      },
      {
        "key": "oidcClientId",
        "label": "settings.schema.auth.oidcClientId",
        "control": "text"
      },
      {
        "key": "oidcClientSecret",
        "label": "settings.schema.auth.oidcClientSecret",
        "control": "password"
      },
      {
        "key": "oidcAllowedSubjects",
        "label": "settings.schema.auth.oidcAllowedSubjects",
        "control": "list"
      },
      {
        "key": "accessTeamDomain",
        "label": "settings.schema.auth.accessTeamDomain",
        "control": "text"
      },
      {
        "key": "accessAudience",
        "label": "settings.schema.auth.accessAudience",
        "control": "text"
      },
      {
        "key": "accessAllowedSubjects",
        "label": "settings.schema.auth.accessAllowedSubjects",
        "control": "list"
      }
    ],
    "proxy": [
      {
        "key": "listenAddress",
        "label": "settings.schema.proxy.listenAddress.label",
        "description": "settings.schema.proxy.listenAddress.description",
        "control": "text",
        "required": true
      },
      {
        "key": "httpPort",
        "label": "settings.schema.proxy.httpPort.label",
        "description": "settings.schema.proxy.httpPort.description",
        "control": "number",
        "required": true,
        "min": 1,
        "max": 65535
      },
      {
        "key": "socks5Port",
        "label": "settings.schema.proxy.socks5Port.label",
        "description": "settings.schema.proxy.socks5Port.description",
        "control": "number",
        "required": true,
        "min": 1,
        "max": 65535
      },
      {
        "key": "password",
        "label": "settings.schema.proxy.password.label",
        "description": "settings.schema.proxy.password.description",
        "control": "password",
        "required": false
      }
    ],
    "channels": [
      {
        "key": "telegram",
        "label": "settings.schema.channels.telegram.label",
        "description": "settings.schema.channels.telegram.description",
        "fields": [
          {
            "key": "endpoint",
            "label": "settings.schema.channels.telegram.endpoint.label",
            "description": "settings.schema.channels.telegram.endpoint.description",
            "control": "text",
            "required": false
          },
          {
            "key": "botToken",
            "label": "settings.schema.channels.telegram.botToken.label",
            "description": "settings.schema.channels.telegram.botToken.description",
            "control": "password",
            "required": true,
            "secret": true
          },
          {
            "key": "recipients",
            "label": "settings.schema.channels.telegram.recipients.label",
            "description": "settings.schema.channels.telegram.recipients.description",
            "control": "list",
            "required": true
          }
        ]
      },
      {
        "key": "bark",
        "label": "settings.schema.channels.bark.label",
        "description": "settings.schema.channels.bark.description",
        "fields": [
          {
            "key": "endpoint",
            "label": "settings.schema.channels.bark.endpoint.label",
            "description": "settings.schema.channels.bark.endpoint.description",
            "control": "text",
            "required": false
          },
          {
            "key": "recipients",
            "label": "settings.schema.channels.bark.recipients.label",
            "description": "settings.schema.channels.bark.recipients.description",
            "control": "list",
            "required": true
          }
        ]
      },
      {
        "key": "gotify",
        "label": "settings.schema.channels.gotify.label",
        "description": "settings.schema.channels.gotify.description",
        "fields": [
          {
            "key": "endpoint",
            "label": "settings.schema.channels.gotify.endpoint.label",
            "description": "settings.schema.channels.gotify.endpoint.description",
            "control": "text",
            "required": true
          },
          {
            "key": "recipients",
            "label": "settings.schema.channels.gotify.recipients.label",
            "description": "settings.schema.channels.gotify.recipients.description",
            "control": "list",
            "required": true
          },
          {
            "key": "priority",
            "label": "settings.schema.channels.gotify.priority.label",
            "description": "settings.schema.channels.gotify.priority.description",
            "control": "number",
            "required": false,
            "min": 0,
            "max": 10
          }
        ]
      },
      {
        "key": "sc3",
        "label": "settings.schema.channels.sc3.label",
        "description": "settings.schema.channels.sc3.description",
        "fields": [
          {
            "key": "endpoint",
            "label": "settings.schema.channels.sc3.endpoint.label",
            "description": "settings.schema.channels.sc3.endpoint.description",
            "control": "text",
            "required": true
          }
        ]
      },
      {
        "key": "lark",
        "label": "settings.schema.channels.lark.label",
        "description": "settings.schema.channels.lark.description",
        "fields": [
          {
            "key": "endpoint",
            "label": "settings.schema.channels.lark.endpoint.label",
            "description": "settings.schema.channels.lark.endpoint.description",
            "control": "text",
            "required": true
          }
        ]
      },
      {
        "key": "wecom",
        "label": "settings.schema.channels.wecom.label",
        "description": "settings.schema.channels.wecom.description",
        "fields": [
          {
            "key": "endpoint",
            "label": "settings.schema.channels.wecom.endpoint.label",
            "description": "settings.schema.channels.wecom.endpoint.description",
            "control": "text",
            "required": true
          }
        ]
      },
      {
        "key": "http",
        "label": "settings.schema.channels.http.label",
        "description": "settings.schema.channels.http.description",
        "fields": [
          {
            "key": "endpoint",
            "label": "settings.schema.channels.http.endpoint.label",
            "description": "settings.schema.channels.http.endpoint.description",
            "control": "text",
            "required": true
          },
          {
            "key": "headers",
            "label": "settings.schema.channels.http.headers.label",
            "description": "settings.schema.channels.http.headers.description",
            "control": "keyValue",
            "required": false
          }
        ]
      },
      {
        "key": "email",
        "label": "settings.schema.channels.email.label",
        "description": "settings.schema.channels.email.description",
        "fields": [
          {
            "key": "smtpHost",
            "label": "settings.schema.channels.email.smtpHost.label",
            "description": "settings.schema.channels.email.smtpHost.description",
            "control": "text",
            "required": true
          },
          {
            "key": "smtpPort",
            "label": "settings.schema.channels.email.smtpPort.label",
            "description": "settings.schema.channels.email.smtpPort.description",
            "control": "number",
            "required": true,
            "min": 1,
            "max": 65535
          },
          {
            "key": "smtpUsername",
            "label": "settings.schema.channels.email.smtpUsername.label",
            "description": "settings.schema.channels.email.smtpUsername.description",
            "control": "text",
            "required": false
          },
          {
            "key": "smtpPassword",
            "label": "settings.schema.channels.email.smtpPassword.label",
            "description": "settings.schema.channels.email.smtpPassword.description",
            "control": "password",
            "required": false,
            "secret": true
          },
          {
            "key": "from",
            "label": "settings.schema.channels.email.from.label",
            "description": "settings.schema.channels.email.from.description",
            "control": "text",
            "required": true
          },
          {
            "key": "recipients",
            "label": "settings.schema.channels.email.recipients.label",
            "description": "settings.schema.channels.email.recipients.description",
            "control": "list",
            "required": true
          },
          {
            "key": "tlsPolicy",
            "label": "settings.schema.channels.email.tlsPolicy.label",
            "description": "settings.schema.channels.email.tlsPolicy.description",
            "control": "select",
            "required": false,
            "options": [
              {
                "label": "settings.schema.channels.email.tlsPolicy.options.mandatory",
                "value": "mandatory"
              },
              {
                "label": "settings.schema.channels.email.tlsPolicy.options.opportunistic",
                "value": "opportunistic"
              },
              {
                "label": "settings.schema.channels.email.tlsPolicy.options.none",
                "value": "none"
              }
            ]
          },
          {
            "key": "ssl",
            "label": "settings.schema.channels.email.ssl.label",
            "description": "settings.schema.channels.email.ssl.description",
            "control": "switch",
            "required": false
          }
        ]
      }
    ]
  },
  "values": {
    "auth": {
      "oidcIssuer": "",
      "oidcClientId": "",
      "oidcClientSecret": "",
      "oidcAllowedSubjects": [],
      "accessTeamDomain": "",
      "accessAudience": "",
      "accessAllowedSubjects": [],
      "mode": "none",
      "sessionMaxDays": 30,
      "otpChannels": [
        "telegram"
      ]
    },
    "proxy": {
      "listenAddress": "127.0.0.1",
      "httpPort": 8080,
      "socks5Port": 1080,
      "password": ""
    },
    "channels": {
      "telegram": {
        "botToken": "fixture-bot",
        "recipients": [
          "123456"
        ]
      }
    },
    "notifications": {
      "channels": [
        "telegram"
      ]
    }
  }
}
"""#
}
