package content

import "github.com/damonto/sigmo/internal/pkg/locale"

// phrases is every piece of notification copy in one language. Fields ending
// in From, To or For are fmt patterns with exactly one %s, so that each
// language can put the name where its grammar wants it.
type phrases struct {
	Login            string
	VerificationCode string

	IncomingSMS     string
	OutgoingSMS     string
	IncomingSMSFrom string
	OutgoingSMSTo   string
	EmptySMS        string
	// SMS and calls get separate direction labels because Chinese words them
	// differently, even though English says From and To for both.
	Sender    string
	Recipient string

	IncomingCall     string
	OutgoingCall     string
	IncomingCallFrom string
	OutgoingCallTo   string
	Caller           string
	Callee           string

	Reminder      string
	ReminderFor   string
	EmptyReminder string
	Profile       string
	ICCID         string

	Modem string
	Time  string
}

var english = phrases{
	Login:            "Sigmo Login",
	VerificationCode: "Verification code",

	IncomingSMS:     "Incoming SMS",
	OutgoingSMS:     "Outgoing SMS",
	IncomingSMSFrom: "Incoming SMS from %s",
	OutgoingSMSTo:   "Outgoing SMS to %s",
	EmptySMS:        "(empty message)",
	Sender:          "From",
	Recipient:       "To",

	IncomingCall:     "Incoming Call",
	OutgoingCall:     "Outgoing Call",
	IncomingCallFrom: "Incoming Call from %s",
	OutgoingCallTo:   "Outgoing Call to %s",
	Caller:           "From",
	Callee:           "To",

	Reminder:      "Reminder",
	ReminderFor:   "Reminder: %s",
	EmptyReminder: "(empty reminder)",
	Profile:       "Profile",
	ICCID:         "ICCID",

	Modem: "Modem",
	Time:  "Time",
}

// chinese keeps "Modem", "Profile" and "ICCID" in English and reuses the web
// push wording, so a notification reads like the web UI it points to.
var chinese = phrases{
	Login:            "Sigmo 登录",
	VerificationCode: "验证码",

	IncomingSMS:     "新短信",
	OutgoingSMS:     "已发送短信",
	IncomingSMSFrom: "来自 %s 的新短信",
	OutgoingSMSTo:   "发给 %s 的短信",
	EmptySMS:        "（空短信）",
	Sender:          "发件人",
	Recipient:       "收件人",

	IncomingCall:     "来电",
	OutgoingCall:     "去电",
	IncomingCallFrom: "来自 %s 的来电",
	OutgoingCallTo:   "拨给 %s 的电话",
	Caller:           "主叫",
	Callee:           "被叫",

	Reminder:      "提醒",
	ReminderFor:   "提醒：%s",
	EmptyReminder: "（空提醒）",
	Profile:       "Profile",
	ICCID:         "ICCID",

	Modem: "Modem",
	Time:  "时间",
}

func phrasesFor(lang locale.Tag) phrases {
	if lang == locale.Chinese {
		return chinese
	}
	return english
}
